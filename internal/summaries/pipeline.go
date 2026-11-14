package summaries

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hibiken/asynq"
	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/ai"
	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/jobs"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/recordings"
)

// Task is the summary task type: its own queue and workers (design D5), attempts with the queue's backoff.
func (s *Service) Task(timeout time.Duration) jobs.TaskType {
	return jobs.TaskType{Name: TaskName, MaxAttempts: MaxAttempts, Timeout: timeout,
		Handler: func(ctx context.Context, t *asynq.Task) error { return s.Handle(ctx, string(t.Payload())) }}
}

// Handle runs one attempt of a recording's summary. It returns an error only to be tried again later; every
// outcome that ends the summary (posted, failed, cancelled) returns nil.
func (s *Service) Handle(ctx context.Context, id string) error {
	s.init()
	retried, _ := asynq.GetRetryCount(ctx)
	maxRetry, ok := asynq.GetMaxRetry(ctx)
	last := !ok || retried >= maxRetry
	setting, err := s.Setting(ctx)
	if err != nil {
		return err
	}
	if setting == org.SummariesOff {
		s.end(ctx, id, -1, recordings.SummaryCancelled, codeSetting)
		return nil
	}
	rec, gen, err := s.claim(ctx, id)
	if err != nil || rec == nil {
		return err // nothing to do (not active, or another task holds it), or the database: try later
	}
	log := s.logger().With("recording_id", id, "attempt", retried+1)
	wctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	s.mu.Lock()
	s.running[id] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.running, id)
		s.mu.Unlock()
	}()
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		s.watch(wctx, id, gen, cancel)
	}()
	err = s.write(wctx, rec, gen, log)
	cause := context.Cause(wctx)
	cancel(nil)
	<-watched
	if err == nil {
		return nil
	}
	if errors.Is(cause, errSwitchedOff) || errors.Is(err, errSwitchedOff) {
		log.Info("summaries: stopped, the setting is off")
		s.end(ctx, id, gen, recordings.SummaryCancelled, codeSetting)
		return nil
	}
	if errors.Is(cause, errClaimLost) {
		return nil
	}
	if errors.Is(cause, recordings.ErrGone) || errors.Is(err, recordings.ErrGone) {
		log.Info("summaries: the recording is gone")
		s.end(ctx, id, gen, recordings.SummaryCancelled, codeDeleted)
		s.removeObjects(context.WithoutCancel(ctx), id, nil)
		return nil
	}
	var ae *ai.Error
	retry := !errors.As(err, &ae) || ae.Retryable()
	log.Warn("summaries: attempt failed", "class", ai.ClassOf(err), "status", ai.Status(err), "retry", retry && !last)
	if retry && !last {
		s.release(context.WithoutCancel(ctx), id, gen)
		return errors.New("summary attempt failed: " + codeOf(err)) // the queue's backoff; no content in it
	}
	s.end(ctx, id, gen, recordings.SummaryFailed, codeOf(err))
	return nil
}

// claim takes the recording's summary for this task (a conditional update that bumps summary_generation): nil when
// it is not active or another task holds a live claim.
func (s *Service) claim(ctx context.Context, id string) (*recordings.Recording, int64, error) {
	var rec recordings.Recording
	if err := s.DB.Gorm.WithContext(ctx).Where("id = ?", id).Take(&rec).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	rec.Trim()
	now := s.now().UnixMilli()
	gen := rec.SummaryGeneration + 1
	r := s.DB.Gorm.WithContext(ctx).Model(&recordings.Recording{}).
		Where("id = ? AND summary_generation = ? AND summary_status IN ? AND summary_claim_until < ?", id,
			rec.SummaryGeneration, recordings.SummaryActive, now).
		Updates(map[string]any{"summary_generation": gen, "summary_claim_until": now + s.claimFor().Milliseconds()})
	if r.Error != nil || r.RowsAffected != 1 {
		return nil, 0, r.Error
	}
	rec.SummaryGeneration = gen
	return &rec, gen, nil
}

// watch renews the claim and re-reads the setting every WatchEvery while the summary runs (design D2): off, a
// lost claim or a recording gone stop it.
func (s *Service) watch(ctx context.Context, id string, gen int64, cancel context.CancelCauseFunc) {
	t := time.NewTicker(s.watchEvery())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		rctx, done := context.WithTimeout(ctx, s.watchEvery())
		setting, err := s.Setting(rctx)
		if err == nil && setting == org.SummariesOff {
			done()
			cancel(errSwitchedOff)
			return
		}
		now := s.now().UnixMilli()
		r := s.DB.Gorm.WithContext(rctx).Model(&recordings.Recording{}).
			Where("id = ? AND summary_generation = ? AND summary_status IN ?", id, gen, recordings.SummaryActive).
			Update("summary_claim_until", now+s.claimFor().Milliseconds())
		gone, gerr := recordings.Gone(rctx, s.DB.Gorm, id)
		done()
		switch {
		case err != nil || r.Error != nil || gerr != nil:
			// the database out of reach: what cannot be checked is not sent (fail closed)
			cancel(errClaimLost)
			return
		case r.RowsAffected != 1:
			cancel(errClaimLost)
			return
		case gone:
			cancel(recordings.ErrGone)
			return
		}
	}
}

// release ends this attempt's claim so the retry can take it.
func (s *Service) release(ctx context.Context, id string, gen int64) {
	_ = s.DB.Gorm.WithContext(ctx).Model(&recordings.Recording{}).
		Where("id = ? AND summary_generation = ?", id, gen).Update("summary_claim_until", 0).Error
}

// end moves an active summary to a terminal state (gen -1: whoever holds it) and announces it.
func (s *Service) end(ctx context.Context, id string, gen int64, status, code string) {
	ctx = context.WithoutCancel(ctx)
	q := s.DB.Gorm.WithContext(ctx).Model(&recordings.Recording{}).Where("id = ? AND summary_status IN ?", id, recordings.SummaryActive)
	if gen >= 0 {
		q = q.Where("summary_generation = ?", gen)
	}
	r := q.Updates(map[string]any{"summary_status": status, "summary_error": code, "summary_claim_until": 0})
	if r.Error != nil || r.RowsAffected != 1 {
		return
	}
	var rec recordings.Recording
	if s.DB.Gorm.WithContext(ctx).Where("id = ?", id).Take(&rec).Error == nil {
		rec.Trim()
		s.announce(ctx, &rec)
	}
}

// step moves the summary to an active state, conditional on this attempt's claim.
func (s *Service) step(ctx context.Context, rec *recordings.Recording, gen int64, status string) error {
	r := s.DB.Gorm.WithContext(ctx).Model(&recordings.Recording{}).
		Where("id = ? AND summary_generation = ? AND summary_status IN ?", rec.ID, gen, recordings.SummaryActive).
		Update("summary_status", status)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return errClaimLost
	}
	rec.SummaryStatus = status
	s.announce(ctx, rec)
	return nil
}

// allowed is checked before every request to the AI service (design D2): the setting, read now, and the
// recording's deleted or expired mark.
func (s *Service) allowed(ctx context.Context, id string) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	setting, err := s.Setting(ctx)
	if err != nil {
		return err
	}
	if setting == org.SummariesOff {
		return errSwitchedOff
	}
	gone, err := recordings.Gone(ctx, s.DB.Gorm, id)
	if err != nil {
		return err
	}
	if gone {
		return recordings.ErrGone
	}
	return nil
}

// slot takes one of this server's AI request slots.
func (s *Service) slot(ctx context.Context) (func(), error) {
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}

// write is one attempt: transcribe what is not checkpointed yet, merge, summarize, post.
func (s *Service) write(ctx context.Context, rec *recordings.Recording, gen int64, log *slog.Logger) error {
	if err := s.allowed(ctx, rec.ID); err != nil {
		return err
	}
	if err := s.step(ctx, rec, gen, recordings.SummaryTranscribing); err != nil {
		return err
	}
	var objs []recordings.Object
	if err := s.DB.Gorm.WithContext(ctx).Where("recording_id = ? AND generation = ? AND state = ?", rec.ID, rec.Generation,
		recordings.ObjectStored).Order("speaker, offset_ms").Find(&objs).Error; err != nil {
		return fmt.Errorf("summary sources: %w", err)
	}
	done := map[string]recordings.Object{}
	var pieces []recordings.Object
	for i := range objs {
		objs[i].Trim()
		switch objs[i].Kind {
		case recordings.ObjectSource:
			pieces = append(pieces, objs[i])
		case recordings.ObjectCheckpoint:
			done[strings.TrimSuffix(objs[i].ObjectKey, ".json")] = objs[i]
		}
	}
	var heard []heardPiece
	sent := 0
	for _, p := range pieces {
		segs, fresh, err := s.transcribe(ctx, rec, p, done)
		if err != nil {
			return err
		}
		if fresh {
			sent++
		}
		heard = append(heard, heardPiece{speaker: p.Speaker, offset: p.OffsetMS, end: p.OffsetMS + p.DurationMS, segs: segs})
	}
	names, err := s.names(ctx, heard)
	if err != nil {
		return err
	}
	lines := merge(heard, names)
	log.Info("summaries: transcribed", "pieces", len(pieces), "sent", sent, "passages", len(lines))
	if len(lines) == 0 {
		return s.post(ctx, rec, gen, "", nil) // no speech: no request to the summary model
	}
	if err := s.step(ctx, rec, gen, recordings.SummarySummarizing); err != nil {
		return err
	}
	summary, err := s.summarize(ctx, rec.ID, lines)
	if err != nil {
		return err
	}
	return s.post(ctx, rec, gen, summary, lines)
}

// checkpoint is a piece's stored transcription.
type checkpoint struct {
	Segments []ai.Segment `json:"segments"`
}

// transcribe returns a piece's segments: its checkpoint when one is stored, otherwise from the service, stored as
// a checkpoint (registered first) before the next request.
func (s *Service) transcribe(ctx context.Context, rec *recordings.Recording, p recordings.Object, done map[string]recordings.Object) ([]ai.Segment, bool, error) {
	if cp, ok := done[strings.TrimSuffix(p.ObjectKey, ".ogg")]; ok {
		var c checkpoint
		if data, err := s.read(ctx, cp.ObjectKey); err == nil && json.Unmarshal(data, &c) == nil {
			return c.Segments, false, nil
		}
		s.Recordings.RemoveObject(ctx, &cp) // unreadable: transcribe again
	}
	audio, err := s.read(ctx, p.ObjectKey)
	if err != nil {
		return nil, false, err
	}
	if err := s.allowed(ctx, rec.ID); err != nil {
		return nil, false, err
	}
	free, err := s.slot(ctx)
	if err != nil {
		return nil, false, err
	}
	segs, err := s.Speech.Transcribe(ctx, ai.TranscribeRequest{Model: s.Config.TranscriptionModel, Language: s.Config.Language,
		Name: "audio.ogg", Audio: audio})
	free()
	if err != nil {
		return nil, false, err
	}
	data, err := json.Marshal(checkpoint{Segments: segs})
	if err != nil {
		return nil, false, err
	}
	cp := &recordings.Object{RecordingID: rec.ID, Generation: rec.Generation, Kind: recordings.ObjectCheckpoint,
		Speaker: p.Speaker, OffsetMS: p.OffsetMS, DurationMS: p.DurationMS, ObjectKey: strings.TrimSuffix(p.ObjectKey, ".ogg") + ".json"}
	if err := s.Recordings.Register(ctx, cp); err != nil {
		return nil, false, err
	}
	if err := s.Recordings.PutObject(ctx, cp, data); err != nil {
		return nil, false, err
	}
	return segs, true, nil
}

func (s *Service) read(ctx context.Context, key string) ([]byte, error) {
	f, err := s.Storage.Open(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("summary object: %w", err)
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, 64<<20))
}

// names are the participants' display names (their names now; the username when they set none).
func (s *Service) names(ctx context.Context, heard []heardPiece) (map[string]string, error) {
	var ids []string
	for _, h := range heard {
		ids = append(ids, h.speaker)
	}
	out := map[string]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct{ ID, DisplayName, Username string }
	if err := s.DB.Gorm.WithContext(ctx).Raw(`SELECT id, display_name, username FROM users WHERE id IN ?`, ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("participants: %w", err)
	}
	for _, r := range rows {
		name := oneLine(r.DisplayName)
		if name == "" {
			name = r.Username
		}
		out[strings.TrimSpace(r.ID)] = name
	}
	return out, nil
}

// MessageLimit is the longest summary message; a longer summary is cut at a section and attached whole.
const MessageLimit = chat.PostMessageMaxRunes - 100

// post publishes the summary (or, without speech, the note that there is none) as a reply in the recording's
// thread in one conditional transaction with the deterministic key summary:<id> (design D5), then removes the
// sources and checkpoints.
func (s *Service) post(ctx context.Context, rec *recordings.Recording, gen int64, summary string, lines []line) error {
	if err := s.allowed(ctx, rec.ID); err != nil {
		return err
	}
	now := s.now()
	in := chat.SystemPost{ChannelID: rec.ChannelID, UserID: rec.StartedBy, RootID: rec.PostID, Key: "summary:" + rec.ID,
		Props: map[string]any{"recording_id": rec.ID}}
	var keys []string
	if len(lines) == 0 {
		in.Type = chat.PostTypeSummaryNoSpeech
	} else {
		in.Type, in.Message = chat.PostTypeSummary, summary
		files := []struct{ name, body, mime string }{
			{recordings.TranscriptPrefix + time.UnixMilli(rec.StartedAt).UTC().Format("2006-01-02-1504") + ".txt", transcriptText(lines), "text/plain"},
		}
		if utf8.RuneCountInString(summary) > MessageLimit {
			in.Message = cutAtSection(summary, MessageLimit)
			in.Props["truncated"] = true
			files = append(files, struct{ name, body, mime string }{"summary.md", summary, "text/markdown"})
		}
		for _, f := range files {
			key := fmt.Sprintf("summaries/%s/%s/s%d/%s", rec.ChannelID, rec.ID, gen, f.name)
			if err := s.Storage.Put(ctx, key, bytes.NewReader([]byte(f.body)), int64(len(f.body))); err != nil {
				return fmt.Errorf("store %s: %w", f.mime, err)
			}
			keys = append(keys, key)
			ext := strings.TrimPrefix(f.name[strings.LastIndex(f.name, "."):], ".")
			in.Files = append(in.Files, chat.FileInfo{ID: ulid.Make().String(), CreatorID: rec.StartedBy, ChannelID: rec.ChannelID,
				Name: f.name, SearchName: f.name, Extension: ext, Size: int64(len(f.body)), MimeType: f.mime, Path: key,
				CreateAt: now.UnixMilli(), UpdateAt: now.UnixMilli()})
		}
	}
	stale := false
	p, created, err := s.Chat.PublishSystemPost(ctx, in, func(tx *gorm.DB, p *chat.PostView) error {
		r := tx.Model(&recordings.Recording{}).Where("id = ? AND summary_generation = ? AND summary_status IN ? AND expired_at = 0",
			rec.ID, gen, recordings.SummaryActive).
			Updates(map[string]any{"summary_status": recordings.SummaryReady, "summary_error": "", "summary_post_id": p.ID,
				"summary_claim_until": 0})
		if r.Error == nil && r.RowsAffected != 1 {
			stale = true
			return errClaimLost
		}
		return r.Error
	})
	if stale || err != nil || !created {
		for _, k := range keys {
			_ = s.Storage.Delete(context.WithoutCancel(ctx), k)
		}
	}
	switch {
	case errors.Is(err, chat.ErrInvalidInput): // the recording's message was deleted: its thread is gone
		return recordings.ErrGone
	case stale:
		return errClaimLost
	case err != nil:
		return err
	}
	if !created { // posted before (a crash after the commit): make the row say so
		_ = s.DB.Gorm.WithContext(ctx).Model(&recordings.Recording{}).Where("id = ? AND summary_status IN ?", rec.ID, recordings.SummaryActive).
			Updates(map[string]any{"summary_status": recordings.SummaryReady, "summary_post_id": p.ID, "summary_claim_until": 0}).Error
	}
	rec.SummaryStatus, rec.SummaryError, rec.SummaryPostID = recordings.SummaryReady, "", p.ID
	s.announce(context.WithoutCancel(ctx), rec)
	s.removeObjects(context.WithoutCancel(ctx), rec.ID, nil)
	return nil
}

// removeObjects deletes a recording's derived objects (those keep accepts are kept).
func (s *Service) removeObjects(ctx context.Context, id string, keep func(recordings.Object) bool) {
	var objs []recordings.Object
	if err := s.DB.Gorm.WithContext(ctx).Where("recording_id = ?", id).Find(&objs).Error; err != nil {
		return
	}
	for i := range objs {
		objs[i].Trim()
		if keep == nil || !keep(objs[i]) {
			s.Recordings.RemoveObject(ctx, &objs[i])
		}
	}
}

// cutAtSection cuts a Markdown summary at the last section heading that keeps it within limit runes.
func cutAtSection(md string, limit int) string {
	runes := []rune(md)
	if len(runes) <= limit {
		return md
	}
	head := string(runes[:limit])
	if i := strings.LastIndex(head, "\n## "); i > 0 {
		return strings.TrimRight(head[:i], "\n")
	}
	if i := strings.LastIndex(head, "\n\n"); i > 0 {
		return strings.TrimRight(head[:i], "\n")
	}
	return head
}
