// Package recordings records meetings (add-meeting-recordings): a hidden recorder captures a room's tracks on this
// server's capture volume while it holds the recording's lease, ffmpeg composes them when it ends, and the result
// is posted in the channel. Rows of meeting_recordings are owned through a lease and a generation (design D3);
// captures are found again by the volume's identity after a restart (D6).
package recordings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"io"

	"github.com/oklog/ulid/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/meetings"
	"github.com/jniltinho/meethub/internal/org"
)

// Statuses of a recording.
const (
	StatusRecording   = "recording"
	StatusProcessing  = "processing"
	StatusInterrupted = "interrupted"
	StatusReady       = "ready"
	StatusFailed      = "failed"
)

// Errors of the recordings API.
var (
	ErrUnavailable  = errors.New("recordings are not available")
	ErrBusy         = errors.New("this server cannot take another recording now")
	ErrActive       = errors.New("the room is already being recorded")
	ErrNotRecording = errors.New("the room is not being recorded")
	ErrNotInRoom    = errors.New("only someone in the meeting can start its recording")
)

// Failure classes stored on a failed recording (never ffmpeg's own output).
const (
	FailCaptureLost = "capture_lost"
	FailEmpty       = "nothing_captured"
)

// Lease timings (design D3) and housekeeping.
const (
	LeaseFor      = 30 * time.Second
	RenewEvery    = 10 * time.Second
	SweepEvery    = 30 * time.Second
	VolumeGrace   = 24 * time.Hour
	OrphanDirAge  = 7 * 24 * time.Hour
	maxWaiting    = 4
	stopChannel   = "meethub:recordings:stop"
	volumeFile    = ".volume"
	recorderToken = 24 * time.Hour // the connection outlives a token anyway; max_minutes is the limit
	shutdownWait  = 10 * time.Second
	startTimeout  = 30 * time.Second
	renewTimeout  = 5 * time.Second
)

// Recording is a row of meeting_recordings.
type Recording struct {
	ID              string `gorm:"primaryKey;size:26"`
	ChannelID       string `gorm:"size:26"`
	Room            string
	StartedBy       string `gorm:"size:26"`
	Mode            string
	Status          string
	PartialReasons  string
	StartedAt       int64
	EndedAt         int64
	DurationMS      int64 `gorm:"column:duration_ms"`
	Volume          string
	LeaseUntil      int64
	Generation      int64
	StopRequestedAt int64
	FileID          string `gorm:"size:26"`
	PostID          string `gorm:"size:26"`
	Error           string
	ExpiredAt       int64
	CreatedAt       int64 `gorm:"autoCreateTime:false"`
	UpdatedAt       int64 `gorm:"autoUpdateTime:false"`
	// add-meeting-summaries (design D2, D4, D5)
	SummaryPolicy          string // the Meeting summaries setting when the recording started
	SummarySource          string // none, available, expired
	SummarySourceExpiresAt int64
	SummaryStatus          string // none, queued, transcribing, summarizing, ready, failed, cancelled
	SummaryError           string
	SummaryClaimUntil      int64
	SummaryGeneration      int64
	SummaryPostID          string `gorm:"size:26"`
}

// TableName maps Recording to meeting_recordings.
func (Recording) TableName() string { return "meeting_recordings" }

func (r *Recording) trim() {
	r.ID, r.ChannelID, r.StartedBy = strings.TrimSpace(r.ID), strings.TrimSpace(r.ChannelID), strings.TrimSpace(r.StartedBy)
	r.FileID, r.PostID = strings.TrimSpace(r.FileID), strings.TrimSpace(r.PostID)
	r.SummaryPostID = strings.TrimSpace(r.SummaryPostID)
}

// Trim removes the padding of CHAR columns (MariaDB and PostgreSQL keep it).
func (r *Recording) Trim() { r.trim() }

// View is a recording as the API lists it.
type View struct {
	ID         string   `json:"id"`
	ChannelID  string   `json:"channel_id"`
	StartedBy  string   `json:"started_by"`
	StartedAt  int64    `json:"started_at"`
	EndedAt    int64    `json:"ended_at,omitempty"`
	DurationMS int64    `json:"duration_ms"`
	Mode       string   `json:"mode"`
	Status     string   `json:"status"`
	Partial    bool     `json:"partial"`
	Reasons    []string `json:"partial_reasons"`
	PostID     string   `json:"post_id,omitempty"`
	FileID     string   `json:"file_id,omitempty"`
	Expired    bool     `json:"expired"`
}

func (r *Recording) view() View {
	reasons := []string{}
	if r.PartialReasons != "" {
		reasons = strings.Split(r.PartialReasons, ",")
	}
	v := View{ID: r.ID, ChannelID: r.ChannelID, StartedBy: r.StartedBy, StartedAt: r.StartedAt, EndedAt: r.EndedAt,
		DurationMS: r.DurationMS, Mode: r.Mode, Status: r.Status, Partial: len(reasons) > 0, Reasons: reasons,
		PostID: r.PostID, Expired: r.ExpiredAt != 0}
	if r.Status == StatusReady && r.ExpiredAt == 0 {
		v.FileID = r.FileID
	}
	return v
}

// Rooms is what recordings need from LiveKit's room API.
type Rooms interface {
	RemoveParticipant(ctx context.Context, room, identity string) error
}

// Service starts, stops, captures, composes and publishes recordings, and keeps their rows honest.
type Service struct {
	DB       *database.DB
	Chat     *chat.Service
	Meetings *meetings.Service
	Rooms    Rooms
	Storage  FileStore
	Redis    *redis.Client // the stop message between replicas; nil: stops are seen at the next renewal
	Config   config.Recordings
	Dir      string // the capture volume
	APIURL   string // LiveKit, as the server reaches it
	APIKey   string
	Secret   string
	FFmpeg   *FFmpeg
	Logger   *slog.Logger
	Now      func() time.Time
	// Capturer runs a capture; nil: the LiveKit recorder (Capture.Run). Tests replace it.
	Capturer func(ctx context.Context, c *Capture, stop <-chan string) (CaptureResult, error)
	// Lease and Renew override LeaseFor and RenewEvery (tests).
	Lease, Renew time.Duration
	// Summaries (add-meeting-summaries): Policy is the Meeting summaries setting in force now ("off" without AI;
	// nil: always off); PieceBytes the largest summary source; OnQueued queues the summary of a recording that
	// became ready under "automatic" (the reconciler catches a lost one).
	Policy     func(ctx context.Context) string
	PieceBytes int64
	OnQueued   func(ctx context.Context, recordingID string)

	volume  string
	pending int // starts admitted and not launched yet
	mu      sync.Mutex
	active  map[string]*active // captures and compositions of this server
	waiting chan *Recording    // compositions waiting (at most maxWaiting)
	runCtx  context.Context
}

// FileStore is the attachments' storage as recordings use it (internal/files).
type FileStore interface {
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	Delete(ctx context.Context, key string) error
}

// active is a recording this server owns: capturing, or waiting for or in composition.
type active struct {
	rec     Recording
	cancel  context.CancelCauseFunc // ends its capture or composition
	stop    chan string             // a capture's stop requests
	capture *Capture
	compose bool // captured: waiting for or in composition
	lost    bool // its lease was lost while it waited
	// leaseUntil is the end of the last lease this server confirmed (zero: not renewed yet)
	leaseUntil time.Time
}

// Enabled reports whether recordings are on.
func (s *Service) Enabled() bool { return s != nil && s.Config.Enabled }

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// policy is the Meeting summaries setting in force now.
func (s *Service) policy(ctx context.Context) string {
	if s.Policy == nil {
		return org.SummariesOff
	}
	return s.Policy(ctx)
}

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.DiscardHandler)
}

// Prepare opens the capture volume (creating its identity once) and runs the startup probe (design D10).
func (s *Service) Prepare(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}
	if err := s.Open(); err != nil {
		return err
	}
	return Probe(ctx, s.FFmpeg, s.Dir, s.Config.Mode, s.Config.VideoFormat)
}

// Open opens the capture volume, creating its identity the first time.
func (s *Service) Open() error {
	abs, err := filepath.Abs(s.Dir) // ffmpeg runs inside the capture directories: every path must be absolute
	if err != nil {
		return &ProbeError{Setting: "recordings.dir", What: "cannot resolve " + s.Dir, err: err}
	}
	s.Dir = abs
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return &ProbeError{Setting: "recordings.dir", What: "cannot create " + s.Dir, err: err}
	}
	id, err := volumeID(s.Dir)
	if err != nil {
		return &ProbeError{Setting: "recordings.dir", What: "cannot write the volume identity in " + s.Dir, err: err}
	}
	s.volume = id
	s.active = map[string]*active{}
	s.waiting = make(chan *Recording, maxWaiting)
	return nil
}

func (s *Service) leaseFor() time.Duration {
	if s.Lease > 0 {
		return s.Lease
	}
	return LeaseFor
}

func (s *Service) renewEvery() time.Duration {
	if s.Renew > 0 {
		return s.Renew
	}
	return RenewEvery
}

// volumeID reads the capture volume's identity, creating it the first time.
func volumeID(dir string) (string, error) {
	path := filepath.Join(dir, volumeFile)
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) > 0 { //nolint:gosec // ours
		return strings.TrimSpace(string(b)), nil
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return id, nil
}

// subject of the server itself in audit events: no actor.
func record(ctx context.Context, tx *gorm.DB, actor, action, channelID string, details map[string]any) error {
	e := audit.Event{Action: action, TargetType: "channel", TargetID: channelID, Details: details}
	if actor != "" {
		e.ActorID = &actor
	}
	return audit.Record(ctx, tx, e)
}

// Start begins recording the channel's room for sub (design D7, D8): 201 with the recording, or one of the errors.
func (s *Service) Start(ctx context.Context, sub chat.Subject, channelID string) (*View, error) {
	if !s.Enabled() {
		return nil, ErrUnavailable
	}
	ch, may, err := s.Chat.RecordingAccess(ctx, sub, channelID, s.Config.Start == config.RecordMembers)
	if err != nil {
		return nil, err
	}
	if !may {
		return nil, chat.ErrForbidden
	}
	if ch.ArchivedAt != 0 {
		return nil, chat.ErrArchived
	}
	in, err := s.Meetings.InRoom(ctx, ch.ID, sub.UserID)
	if err != nil {
		return nil, err
	}
	if !in {
		return nil, ErrNotInRoom
	}
	if err := s.admit(); err != nil {
		return nil, err
	}
	launched := false
	defer func() {
		if !launched {
			s.unreserve()
		}
	}()
	room, err := s.Meetings.RoomName(ctx, ch.ID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	rec := Recording{
		ID: ulid.Make().String(), ChannelID: ch.ID, Room: room, StartedBy: sub.UserID, Mode: s.Config.Mode,
		Status: StatusRecording, StartedAt: now.UnixMilli(), Volume: s.volume, LeaseUntil: now.Add(s.leaseFor()).UnixMilli(),
		Generation: 1, FileID: ulid.Make().String(), CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
		SummaryPolicy: s.policy(ctx), SummarySource: SourceNone, SummaryStatus: SummaryNone,
	}
	// one recording per room: the room's lock is held while it checks and inserts. Past this point the request's
	// end must not stop a start that committed: it uses a context of its own, bounded.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), startTimeout)
	defer cancel()
	err = s.Meetings.Touch(ctx, ch.ID, func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&Recording{}).Where("channel_id = ? AND status = ?", ch.ID, StatusRecording).Count(&n).Error; err != nil {
			return fmt.Errorf("running recordings: %w", err)
		}
		if n > 0 {
			return ErrActive
		}
		if err := tx.Create(&rec).Error; err != nil {
			return fmt.Errorf("create recording: %w", err)
		}
		return record(ctx, tx, sub.UserID, audit.ActionRecordingStarted, ch.ID, map[string]any{"recording_id": rec.ID})
	})
	if err != nil {
		var n int64
		cerr := s.DB.Gorm.WithContext(ctx).Model(&Recording{}).Where("id = ?", rec.ID).Count(&n).Error
		if cerr == nil && n == 0 {
			return nil, err
		}
		// committed (or unknown: a capture without its row cannot renew a lease and stops at once), only the
		// announcement failed: the recording runs, and the next change announces the room
		s.logger().Warn("recordings: start not announced", "recording_id", rec.ID, "error", err)
	}
	s.launch(ctx, rec)
	launched = true
	v := rec.view()
	return &v, nil
}

// CanRecord reports whether sub may record the channel's meetings (false when recordings are off).
func (s *Service) CanRecord(ctx context.Context, sub chat.Subject, channelID string) bool {
	if !s.Enabled() {
		return false
	}
	_, may, err := s.Chat.RecordingAccess(ctx, sub, channelID, s.Config.Start == config.RecordMembers)
	return err == nil && may
}

// admit refuses a new recording beyond the concurrent captures, a full backlog or the volume's free space (D8),
// and otherwise reserves its place until unreserve (after launch or a failed start), so concurrent starts cannot
// all take the last one.
func (s *Service) admit() error {
	free, err := freeBytes(s.Dir)
	if err != nil {
		return fmt.Errorf("capture volume: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	capturing := s.pending
	for _, a := range s.active {
		if !a.compose {
			capturing++
		}
	}
	n := len(s.active) + s.pending
	if capturing >= s.Config.MaxConcurrent || len(s.waiting) >= maxWaiting ||
		free < uint64(n+1)*uint64(s.Reserve()) { //nolint:gosec // positive
		return ErrBusy
	}
	s.pending++
	return nil
}

func (s *Service) unreserve() {
	s.mu.Lock()
	s.pending--
	s.mu.Unlock()
}

// Reserve is what one recording may need on the capture volume: its capture budget and the peak of its
// composition (design D8).
func (s *Service) Reserve() int64 {
	rate := int64(1_800_000) // bit/s: the participants' tracks and the master while mixing
	if s.Config.Mode == config.RecordVideo {
		rate = 4_300_000 // segments, their concatenation, the final file and the master
	}
	return s.Config.MaxBytes + rate/8*int64(s.Config.MaxMinutes)*60
}

func freeBytes(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil //nolint:gosec // block size is positive
}

// Stop asks the room's recording to stop, through any server (design D3): the owner stops on the bus message or at
// its next renewal; the room is told "stopping" now and "stopped" once the recorder has left.
func (s *Service) Stop(ctx context.Context, sub chat.Subject, channelID string) error {
	if !s.Enabled() {
		return ErrUnavailable
	}
	ch, may, err := s.Chat.RecordingAccess(ctx, sub, channelID, s.Config.Start == config.RecordMembers)
	if err != nil {
		return err
	}
	var id string
	err = s.Meetings.Touch(ctx, ch.ID, func(tx *gorm.DB) error {
		var rec Recording
		if err := tx.Where("channel_id = ? AND status = ?", ch.ID, StatusRecording).Take(&rec).Error; err != nil {
			if database.IsNotFound(err) {
				return ErrNotRecording
			}
			return fmt.Errorf("running recording: %w", err)
		}
		rec.trim()
		if !may && rec.StartedBy != sub.UserID {
			return chat.ErrForbidden
		}
		id = rec.ID
		if rec.StopRequestedAt == 0 {
			if err := tx.Model(&Recording{}).Where("id = ?", rec.ID).
				Updates(map[string]any{"stop_requested_at": s.now().UnixMilli(), "updated_at": s.now().UnixMilli()}).Error; err != nil {
				return fmt.Errorf("stop recording: %w", err)
			}
		}
		return record(ctx, tx, sub.UserID, audit.ActionRecordingStopped, ch.ID, map[string]any{"recording_id": rec.ID})
	})
	if err != nil {
		return err
	}
	if s.Redis != nil {
		if err := s.Redis.Publish(context.WithoutCancel(ctx), stopChannel, id).Err(); err != nil {
			s.logger().WarnContext(ctx, "recordings: stop message not sent; the owner stops at its next renewal", "error", err)
		}
	}
	s.signalStop(id) // this server may be the owner
	return nil
}

func (s *Service) signalStop(id string) {
	s.mu.Lock()
	var stop chan string
	if a := s.active[id]; a != nil {
		stop = a.stop
	}
	s.mu.Unlock()
	if stop != nil {
		select {
		case stop <- EndStopped:
		default:
		}
	}
}

// List returns a channel's recordings to its readers, newest first, before the recording id cursor (paged).
func (s *Service) List(ctx context.Context, sub chat.Subject, channelID, before string, limit int) ([]View, error) {
	if !s.Enabled() {
		return nil, ErrUnavailable
	}
	ch, err := s.Chat.ReadableChannel(ctx, sub, channelID)
	if err != nil {
		return nil, err
	}
	limit = min(max(limit, 1), 100)
	q := s.DB.Gorm.WithContext(ctx).Where("channel_id = ?", ch.ID)
	if before != "" {
		q = q.Where("id < ?", before)
	}
	var rows []Recording
	if err := q.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list recordings: %w", err)
	}
	out := make([]View, len(rows))
	for i := range rows {
		rows[i].trim()
		out[i] = rows[i].view()
	}
	return out, nil
}

// Run owns this server's recordings until ctx ends: the stop messages, the composition worker, adoption of the
// volume's interrupted captures and the cleanup of finished capture directories. On return every capture has
// stopped and released its lease.
func (s *Service) Run(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}
	s.mu.Lock()
	s.runCtx = ctx
	s.mu.Unlock()
	var wg sync.WaitGroup
	if s.Redis != nil {
		wg.Go(func() { s.listen(ctx) })
	}
	wg.Go(func() { s.composeLoop(ctx) })
	wg.Go(func() { s.leaseLoop(ctx) })
	s.adopt(ctx)
	tick := time.NewTicker(SweepEvery)
	defer tick.Stop()
	for done := false; !done; {
		select {
		case <-ctx.Done():
			done = true
		case <-tick.C:
			s.adopt(ctx)
			s.cleanDirs(ctx)
		}
	}
	s.mu.Lock()
	for _, a := range s.active {
		if a.cancel != nil {
			a.cancel(endCause(EndShutdown))
		}
	}
	s.mu.Unlock()
	wg.Wait()
	// compositions still queued are left as they are: their leases lapse and the volume's next start adopts them
	for drained := false; !drained; {
		select {
		case rec := <-s.waiting:
			s.forget(rec.ID)
		default:
			drained = true
		}
	}
	s.waitCaptures(shutdownWait)
	return nil
}

// waitCaptures waits, at most limit, for the captures to stop and release their leases.
func (s *Service) waitCaptures(limit time.Duration) {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		n := 0
		for _, a := range s.active {
			if !a.compose {
				n++
			}
		}
		s.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// listen relays stop messages from other servers.
func (s *Service) listen(ctx context.Context) {
	sub := s.Redis.Subscribe(ctx, stopChannel)
	defer func() { _ = sub.Close() }()
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-ch:
			if !ok {
				return
			}
			s.signalStop(m.Payload)
		}
	}
}

// launch starts capturing rec on this server, with its lease; the capture outlives the request that started it
// and ends with the server (Run's context).
func (s *Service) launch(req context.Context, rec Recording) {
	ctx, cancel := context.WithCancelCause(context.WithoutCancel(req))
	a := &active{rec: rec, cancel: cancel, stop: make(chan string, 1), leaseUntil: time.UnixMilli(rec.LeaseUntil)}
	s.mu.Lock()
	s.active[rec.ID] = a
	s.pending-- // the start's reservation is now this capture
	stopWatch := func() bool { return false }
	if s.runCtx != nil {
		stopWatch = context.AfterFunc(s.runCtx, func() { cancel(endCause(EndShutdown)) }) //nolint:contextcheck // ends with the server
	}
	s.mu.Unlock()
	go func() {
		defer cancel(nil)
		defer stopWatch()
		s.capture(ctx, a)
	}()
}

func (s *Service) forget(id string) {
	s.mu.Lock()
	delete(s.active, id)
	s.mu.Unlock()
}

// capture records a's room until it ends, renewing the lease, then hands it to composition.
func (s *Service) capture(ctx context.Context, a *active) {
	rec := a.rec
	log := s.logger().With("recording_id", rec.ID, "channel_id", rec.ChannelID)
	dir := filepath.Join(s.Dir, rec.ID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		log.Error("recordings: capture directory", "error", err)
		s.release(ctx, rec, 0, nil)
		s.forget(rec.ID)
		return
	}
	token, err := RecorderToken(s.APIKey, s.Secret, rec.Room, rec.ID, recorderToken)
	if err != nil {
		log.Error("recordings: token", "error", err)
		s.release(ctx, rec, 0, nil)
		s.forget(rec.ID)
		return
	}
	c := &Capture{Dir: dir, URL: s.APIURL, Token: token, Mode: rec.Mode, Start: time.UnixMilli(rec.StartedAt),
		MaxBytes: s.Config.MaxBytes, MaxTime: time.Duration(s.Config.MaxMinutes) * time.Minute, Logger: log}
	a.capture = c
	run := s.Capturer
	if run == nil {
		run = func(ctx context.Context, c *Capture, stop <-chan string) (CaptureResult, error) {
			return c.Run(ctx, stop)
		}
	}
	res, err := run(ctx, c, a.stop)
	if err != nil {
		log.Error("recordings: the recorder could not join", "error", err)
		s.fail(context.WithoutCancel(ctx), &rec, "capture_failed")
		s.forget(rec.ID)
		return
	}
	log.Info("recordings: capture ended", "reason", res.End, "bytes", c.Bytes())
	switch res.End {
	case EndLease:
		// not ours any more: the sweeper and the volume's next adoption take it from here
		s.forget(rec.ID)
		return
	case EndShutdown:
		s.release(context.WithoutCancel(ctx), rec, res.Ended.UnixMilli(), res.Partial)
		s.forget(rec.ID)
		return
	}
	if s.toProcessing(context.WithoutCancel(ctx), &rec, res) {
		s.enqueue(&rec, log)
		return
	}
	s.forget(rec.ID)
}

// leaseLoop renews the lease of every recording this server owns (design D3), each on its own and bounded, and
// watches each capture's stop flag and its channel's archive. A renewal that fails stops that capture at once (fail
// closed); so does a lease about to lapse while renewals hang (a database out of reach).
func (s *Service) leaseLoop(ctx context.Context) {
	tick := time.NewTicker(s.renewEvery())
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		s.mu.Lock()
		owned := make([]*active, 0, len(s.active))
		for _, a := range s.active {
			owned = append(owned, a)
		}
		s.mu.Unlock()
		var wg sync.WaitGroup
		for _, a := range owned {
			s.mu.Lock()
			late := !a.leaseUntil.IsZero() && time.Now().After(a.leaseUntil.Add(-s.renewEvery()))
			cancel, id := a.cancel, a.rec.ID
			s.mu.Unlock()
			if late && cancel != nil {
				s.logger().Warn("recordings: lease about to lapse unrenewed, stopping", "recording_id", id)
				cancel(endCause(EndLease))
				continue
			}
			wg.Go(func() { s.renew(ctx, a) })
		}
		wg.Wait()
	}
}

func (s *Service) renew(ctx context.Context, a *active) {
	s.mu.Lock()
	rec, compose, stop := a.rec, a.compose, a.stop
	s.mu.Unlock()
	rctx, cancel := context.WithTimeout(ctx, min(renewTimeout, s.renewEvery()))
	defer cancel()
	now := s.now()
	res := s.DB.Gorm.WithContext(rctx).Model(&Recording{}).
		Where("id = ? AND generation = ? AND status IN ?", rec.ID, rec.Generation, []string{StatusRecording, StatusProcessing}).
		Updates(map[string]any{"lease_until": now.Add(s.leaseFor()).UnixMilli(), "updated_at": now.UnixMilli()})
	var row struct {
		StopRequestedAt int64
		ArchivedAt      int64
	}
	err := res.Error
	if err == nil && res.RowsAffected == 1 {
		err = s.DB.Gorm.WithContext(rctx).Raw(`SELECT r.stop_requested_at, c.archived_at FROM meeting_recordings r
			JOIN channels c ON c.id = r.channel_id WHERE r.id = ?`, rec.ID).Scan(&row).Error
	}
	if err != nil || res.RowsAffected != 1 {
		s.logger().Warn("recordings: lease not renewed, stopping", "recording_id", rec.ID, "error", err)
		s.mu.Lock()
		a.lost = true
		stopIt := a.cancel
		s.mu.Unlock()
		if stopIt != nil {
			stopIt(endCause(EndLease))
		}
		return
	}
	s.mu.Lock()
	a.leaseUntil = time.Now().Add(s.leaseFor())
	s.mu.Unlock()
	if compose || stop == nil {
		return
	}
	reason := ""
	switch {
	case row.StopRequestedAt != 0:
		reason = EndStopped
	case row.ArchivedAt != 0:
		reason = EndArchived
	}
	if reason != "" {
		select {
		case stop <- reason:
		default:
		}
	}
}

// release ends this server's hold on a recording at shutdown or after an error: it is interrupted with its end
// known (0: unknown, a cut), and the room stops showing it.
func (s *Service) release(ctx context.Context, rec Recording, ended int64, partial []string) {
	ctx, cancel := context.WithTimeout(ctx, shutdownWait)
	defer cancel()
	err := s.Meetings.Touch(ctx, rec.ChannelID, func(tx *gorm.DB) error {
		up := map[string]any{"status": StatusInterrupted, "lease_until": 0, "updated_at": s.now().UnixMilli()}
		if ended > 0 {
			up["ended_at"], up["duration_ms"] = ended, ended-rec.StartedAt
			up["partial_reasons"] = joinReasons("", partial)
		}
		return tx.Model(&Recording{}).Where("id = ? AND generation = ?", rec.ID, rec.Generation).Updates(up).Error
	})
	if err != nil {
		s.logger().Warn("recordings: release", "recording_id", rec.ID, "error", err)
	}
}

func joinReasons(have string, add []string) string {
	var all []string
	if have != "" {
		all = strings.Split(have, ",")
	}
	for _, r := range add {
		if r != "" && !slices.Contains(all, r) {
			all = append(all, r)
		}
	}
	slices.Sort(all)
	return strings.Join(all, ",")
}

// toProcessing records the capture's end and tells the room it stopped, once the recorder has left (design D3).
func (s *Service) toProcessing(ctx context.Context, rec *Recording, res CaptureResult) bool {
	ended := res.Ended.UnixMilli()
	reasons := joinReasons(rec.PartialReasons, res.Partial)
	changed := false
	err := s.Meetings.Touch(ctx, rec.ChannelID, func(tx *gorm.DB) error {
		r := tx.Model(&Recording{}).Where("id = ? AND generation = ? AND status = ?", rec.ID, rec.Generation, StatusRecording).
			Updates(map[string]any{"status": StatusProcessing, "ended_at": ended, "duration_ms": ended - rec.StartedAt,
				"partial_reasons": reasons, "lease_until": s.now().Add(s.leaseFor()).UnixMilli(), "updated_at": s.now().UnixMilli()})
		changed = r.RowsAffected == 1
		if r.Error == nil && changed && res.End != EndStopped {
			r.Error = record(ctx, tx, "", audit.ActionRecordingStopped, rec.ChannelID,
				map[string]any{"recording_id": rec.ID, "reason": res.End})
		}
		return r.Error
	})
	if err != nil {
		s.logger().Error("recordings: end of capture", "recording_id", rec.ID, "error", err)
		return false
	}
	rec.Status, rec.EndedAt, rec.DurationMS, rec.PartialReasons = StatusProcessing, ended, ended-rec.StartedAt, reasons
	return changed
}

// enqueue queues a composition on this server (design D8: one at a time, at most maxWaiting waiting); it stays
// owned (its lease renewed) while it waits.
func (s *Service) enqueue(rec *Recording, log *slog.Logger) {
	s.mu.Lock()
	a := s.active[rec.ID]
	if a == nil {
		a = &active{}
		s.active[rec.ID] = a
	}
	a.rec, a.compose, a.stop, a.lost = *rec, true, nil, false
	a.leaseUntil = time.Now().Add(s.leaseFor())
	s.mu.Unlock()
	select {
	case s.waiting <- rec:
	default: // admission keeps the backlog bounded; an adopted one beyond it waits for a later pass
		s.forget(rec.ID)
		log.Warn("recordings: composition backlog full, left for a later pass")
	}
}

// composeLoop runs one composition at a time.
func (s *Service) composeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case rec := <-s.waiting:
			s.composeOne(ctx, rec)
		}
	}
}

func (s *Service) composeOne(ctx context.Context, rec *Recording) {
	defer s.forget(rec.ID)
	log := s.logger().With("recording_id", rec.ID, "channel_id", rec.ChannelID)
	cctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	s.mu.Lock()
	a := s.active[rec.ID]
	lost := a == nil || a.lost
	if a != nil {
		a.cancel = cancel
	}
	s.mu.Unlock()
	if lost {
		return
	}
	dir := filepath.Join(s.Dir, rec.ID)
	c := &Composer{FFmpeg: s.FFmpeg, Mode: rec.Mode, Format: s.Config.VideoFormat}
	// summary sources only for a recording started under a policy that is still not off ("off" always wins)
	sources, sourcesFailed := rec.SummaryPolicy != "" && rec.SummaryPolicy != org.SummariesOff && s.policy(ctx) != org.SummariesOff, false
	if sources {
		c.Sources = s.sourcesFor(rec, &sourcesFailed)
	}
	out, err := c.Compose(cctx, dir, rec.DurationMS, s.names(ctx, dir))
	if cctx.Err() != nil {
		return // shutdown or lease lost: the row stays for adoption
	}
	if err != nil {
		var fe *FFmpegError
		class := "compose_failed"
		if errors.As(err, &fe) {
			class = fe.Class
		}
		log.Error("recordings: composition failed", "class", class)
		s.fail(ctx, rec, class)
		return
	}
	if err := s.publish(ctx, rec, out, sources && !sourcesFailed); err != nil {
		log.Error("recordings: publication", "error", err)
		return // the lease expires; the next adoption composes again
	}
	log.Info("recordings: posted", "duration_ms", rec.DurationMS)
}

// names are the display names of the participants, from the users table (their names now).
func (s *Service) names(ctx context.Context, dir string) map[string]string {
	tracks, err := LoadTracks(dir)
	if err != nil {
		return nil
	}
	var ids []string
	for _, t := range tracks {
		ids = append(ids, t.Participant)
	}
	var rows []struct {
		ID          string
		DisplayName string
	}
	if len(ids) == 0 || s.DB.Gorm.WithContext(ctx).Raw(`SELECT id, display_name FROM users WHERE id IN ?`, ids).Scan(&rows).Error != nil {
		return nil
	}
	out := map[string]string{}
	for _, r := range rows {
		out[strings.TrimSpace(r.ID)] = r.DisplayName
	}
	return out
}

// storageKey is the object of one composition attempt: it carries the generation, so a superseded attempt's late
// upload never overwrites the one that published (design D7).
func storageKey(rec *Recording, name string) string {
	return fmt.Sprintf("recordings/%s/%s/g%d/%s", rec.ChannelID, rec.ID, rec.Generation, name)
}

// publish uploads the file and posts it in one conditional transaction (design D7). With sources, the recording
// can be summarized, and under "automatic" its summary is queued in the same transaction (add-meeting-summaries).
func (s *Service) publish(ctx context.Context, rec *Recording, out *Output, sources bool) error {
	f, err := os.Open(out.Path)
	if err != nil {
		return fmt.Errorf("recording file: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("recording file: %w", err)
	}
	key := storageKey(rec, out.Name)
	err = s.Storage.Put(ctx, key, f, st.Size())
	_ = f.Close()
	if err != nil {
		return fmt.Errorf("store recording: %w", err)
	}
	now := s.now().UnixMilli()
	file := chat.FileInfo{
		ID: rec.FileID, CreatorID: rec.StartedBy, ChannelID: rec.ChannelID, Name: out.Name, SearchName: out.Name,
		Extension: strings.TrimPrefix(filepath.Ext(out.Name), "."), Size: st.Size(), MimeType: out.MimeType,
		Path: key, CreateAt: now, UpdateAt: now,
	}
	summary := map[string]any{"status": SummaryNone, "source": SourceNone}
	up := map[string]any{"status": StatusReady, "lease_until": 0, "updated_at": now}
	if sources {
		summary["source"] = SourceAvailable
		up["summary_source"] = SourceAvailable
		up["summary_source_expires_at"] = rec.EndedAt + SourceKeep.Milliseconds()
		if rec.SummaryPolicy == org.SummariesAutomatic && s.policy(ctx) != org.SummariesOff {
			summary["status"], up["summary_status"] = SummaryQueued, SummaryQueued
		}
	}
	props := map[string]any{"recording_id": rec.ID, "duration_ms": rec.DurationMS, "mode": rec.Mode,
		"partial": rec.PartialReasons != "", "partial_reasons": reasonsOf(rec.PartialReasons), "silent": out.Silent,
		"summary": summary}
	stale := false
	_, created, err := s.Chat.PublishSystemPost(ctx, chat.SystemPost{
		ChannelID: rec.ChannelID, UserID: rec.StartedBy, Type: chat.PostTypeRecording, Key: "recording:" + rec.ID,
		Props: props, Files: []chat.FileInfo{file},
	}, func(tx *gorm.DB, p *chat.PostView) error {
		up["post_id"] = p.ID
		r := tx.Model(&Recording{}).Where("id = ? AND generation = ? AND status = ?", rec.ID, rec.Generation, StatusProcessing).
			Updates(up)
		if r.Error == nil && r.RowsAffected != 1 {
			stale = true
			return errStale
		}
		return r.Error
	})
	if stale || err != nil {
		_ = s.Storage.Delete(context.WithoutCancel(ctx), key) // a superseded attempt leaves nothing
		if stale {
			return nil
		}
		return err
	}
	if !created { // published before (a crash after the commit): keep the object only if it is the published one
		var paths []string
		s.DB.Gorm.WithContext(ctx).Model(&chat.FileInfo{}).Where("id = ?", rec.FileID).Pluck("path", &paths)
		if len(paths) == 0 || paths[0] != key {
			_ = s.Storage.Delete(context.WithoutCancel(ctx), key)
		}
		return nil
	}
	// earlier attempts' objects, if any were left by a crash between upload and publication
	for g := int64(1); g <= rec.Generation; g++ {
		old := *rec
		old.Generation = g
		for _, name := range []string{"recording.m4a", "recording.webm", "recording.mp4"} {
			if g != rec.Generation || name != out.Name {
				_ = s.Storage.Delete(context.WithoutCancel(ctx), storageKey(&old, name))
			}
		}
	}
	if summary["status"] == SummaryQueued && s.OnQueued != nil {
		s.OnQueued(context.WithoutCancel(ctx), rec.ID)
	}
	return nil
}

var errStale = errors.New("the recording is no longer this attempt's")

func reasonsOf(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

// fail marks a recording failed and tells the channel (design D6).
func (s *Service) fail(ctx context.Context, rec *Recording, class string) {
	_, _, err := s.Chat.PublishSystemPost(ctx, chat.SystemPost{
		ChannelID: rec.ChannelID, UserID: rec.StartedBy, Type: chat.PostTypeRecordingFailed, Key: "recording:" + rec.ID,
		Props: map[string]any{"recording_id": rec.ID, "error": class},
	}, func(tx *gorm.DB, p *chat.PostView) error {
		r := tx.Model(&Recording{}).Where("id = ? AND generation = ? AND status IN ?", rec.ID, rec.Generation,
			[]string{StatusRecording, StatusProcessing, StatusInterrupted}).
			Updates(map[string]any{"status": StatusFailed, "error": class, "post_id": p.ID, "lease_until": 0,
				"updated_at": s.now().UnixMilli()})
		if r.Error == nil && r.RowsAffected != 1 {
			return errStale // another attempt owns it now: no failure post
		}
		return r.Error
	})
	if errors.Is(err, errStale) {
		return
	}
	if err != nil {
		s.logger().Error("recordings: failure post", "recording_id", rec.ID, "error", err)
		return
	}
	// the room stops showing it (a capture that failed was still "recording")
	if err := s.Meetings.Touch(ctx, rec.ChannelID, nil); err != nil {
		s.logger().Warn("recordings: room not announced", "recording_id", rec.ID, "error", err)
	}
	for g := int64(1); g <= rec.Generation; g++ { // whatever an attempt uploaded before the end
		old := *rec
		old.Generation = g
		for _, name := range []string{"recording.m4a", "recording.webm", "recording.mp4"} {
			_ = s.Storage.Delete(context.WithoutCancel(ctx), storageKey(&old, name))
		}
	}
}

// adopt takes the volume's recordings that no server works on (design D6): interrupted ones and this volume's own
// whose lease expired; a capture cut short is a recording up to the cut, marked partial.
func (s *Service) adopt(ctx context.Context) {
	now := s.now()
	var rows []Recording
	if err := s.DB.Gorm.WithContext(ctx).Where("volume = ? AND (status = ? OR (status IN ? AND lease_until < ?))",
		s.volume, StatusInterrupted, []string{StatusRecording, StatusProcessing}, now.UnixMilli()).
		Order("started_at").Find(&rows).Error; err != nil {
		s.logger().Warn("recordings: adoption", "error", err)
		return
	}
	for i := range rows {
		rec := rows[i]
		rec.trim()
		s.mu.Lock()
		_, busy := s.active[rec.ID]
		full := len(s.waiting) >= maxWaiting
		s.mu.Unlock()
		if busy || full {
			continue
		}
		dir := filepath.Join(s.Dir, rec.ID)
		if _, err := os.Stat(dir); err != nil {
			continue // not this volume's after all (a copy of its identity): leave it to the 24-hour rule
		}
		reasons := joinReasons(rec.PartialReasons, manifestReasons(dir))
		if rec.EndedAt == 0 { // cut short: up to the last durable media
			end := capturedUntil(dir)
			rec.EndedAt, rec.DurationMS = rec.StartedAt+end, end
			reasons = joinReasons(reasons, []string{PartialCut})
		}
		wasRecording := rec.Status == StatusRecording
		gen := rec.Generation + 1
		err := s.Meetings.Touch(ctx, rec.ChannelID, func(tx *gorm.DB) error {
			r := tx.Model(&Recording{}).Where("id = ? AND generation = ? AND (status = ? OR (status IN ? AND lease_until < ?))",
				rec.ID, rec.Generation, StatusInterrupted, []string{StatusRecording, StatusProcessing}, now.UnixMilli()).
				Updates(map[string]any{"status": StatusProcessing, "generation": gen, "ended_at": rec.EndedAt,
					"duration_ms": rec.DurationMS, "partial_reasons": reasons,
					"lease_until": now.Add(s.leaseFor()).UnixMilli(), "updated_at": now.UnixMilli()})
			if r.Error == nil && r.RowsAffected != 1 {
				return errStale
			}
			return r.Error
		})
		if err != nil {
			continue
		}
		if wasRecording && s.Rooms != nil {
			_ = s.Rooms.RemoveParticipant(ctx, rec.Room, RecorderIdentity(rec.ID))
		}
		rec.Generation, rec.Status, rec.PartialReasons = gen, StatusProcessing, reasons
		s.logger().Info("recordings: adopted", "recording_id", rec.ID, "generation", gen)
		s.enqueue(&rec, s.logger())
	}
}

// manifestReasons are the partial reasons a capture's manifests recorded, and timing still placed by arrival.
func manifestReasons(dir string) []string {
	tracks, err := LoadTracks(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, t := range tracks {
		out = append(out, t.Partial...)
		for _, sg := range t.Segments {
			if sg.Approx {
				out = append(out, PartialTimingApproximate)
			}
		}
	}
	return out
}

// capturedUntil is the end (ms from the start) of the last durable media of a capture.
func capturedUntil(dir string) int64 {
	tracks, err := LoadTracks(dir)
	if err != nil {
		return 0
	}
	var end int64
	for _, t := range tracks {
		for _, sg := range t.Segments {
			end = max(end, sg.PosMS+sg.DurMS)
		}
	}
	return end
}

// cleanDirs removes capture directories whose recording is ready or failed, or that have no recording and are
// older than OrphanDirAge — never one whose recording may still be composed.
func (s *Service) cleanDirs(ctx context.Context) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || len(e.Name()) != 26 {
			continue
		}
		var rec Recording
		err := s.DB.Gorm.WithContext(ctx).Select("status").Where("id = ?", e.Name()).Take(&rec).Error
		switch {
		case err == nil && (rec.Status == StatusReady || rec.Status == StatusFailed):
		case database.IsNotFound(err):
			info, ierr := e.Info()
			if ierr != nil || s.now().Sub(info.ModTime()) < OrphanDirAge {
				continue
			}
		default:
			continue
		}
		s.mu.Lock()
		_, busy := s.active[e.Name()]
		s.mu.Unlock()
		if !busy {
			_ = os.RemoveAll(filepath.Join(s.Dir, e.Name()))
		}
	}
}

// Sweep ends recordings whose server vanished (design D3), once across servers every SweepEvery: a lease expired
// makes the recording interrupted (its generation bumped, its recorder removed from the room); one interrupted
// for VolumeGrace without its volume coming back fails.
func (s *Service) Sweep(ctx context.Context) error {
	now := s.now()
	var rows []Recording
	if err := s.DB.Gorm.WithContext(ctx).Where("status IN ? AND lease_until < ?",
		[]string{StatusRecording, StatusProcessing}, now.UnixMilli()).Find(&rows).Error; err != nil {
		return fmt.Errorf("expired leases: %w", err)
	}
	var errs []error
	for i := range rows {
		rec := rows[i]
		rec.trim()
		claimed := false
		// conditional on the lease still expired: a renewal or a publication in between keeps the row
		err := s.Meetings.Touch(ctx, rec.ChannelID, func(tx *gorm.DB) error {
			r := tx.Model(&Recording{}).Where("id = ? AND generation = ? AND status IN ? AND lease_until < ?", rec.ID,
				rec.Generation, []string{StatusRecording, StatusProcessing}, now.UnixMilli()).
				Updates(map[string]any{"status": StatusInterrupted, "generation": rec.Generation + 1,
					"updated_at": now.UnixMilli()})
			claimed = r.RowsAffected == 1
			return r.Error
		})
		if err != nil {
			errs = append(errs, err)
			// the claim may have committed and only the announcement failed: evict only if it did
			var cur Recording
			claimed = s.DB.Gorm.WithContext(ctx).Select("status", "generation").Where("id = ?", rec.ID).Take(&cur).Error == nil &&
				cur.Status == StatusInterrupted && cur.Generation == rec.Generation+1
		}
		if claimed && s.Rooms != nil && rec.Status == StatusRecording {
			if err := s.Rooms.RemoveParticipant(ctx, rec.Room, RecorderIdentity(rec.ID)); err != nil {
				errs = append(errs, err)
			}
		}
	}
	var lost []Recording
	if err := s.DB.Gorm.WithContext(ctx).Where("status = ? AND updated_at < ?", StatusInterrupted,
		now.Add(-VolumeGrace).UnixMilli()).Find(&lost).Error; err != nil {
		return errors.Join(append(errs, fmt.Errorf("lost recordings: %w", err))...)
	}
	for i := range lost {
		lost[i].trim()
		s.fail(ctx, &lost[i], FailCaptureLost)
	}
	return errors.Join(errs...)
}

// Expire removes the files of recordings older than recordings.retention_days (in archived channels too); their
// messages say the recording expired.
func (s *Service) Expire(ctx context.Context) error {
	if s.Config.RetentionDays <= 0 {
		return nil
	}
	cut := s.now().Add(-time.Duration(s.Config.RetentionDays) * 24 * time.Hour).UnixMilli()
	var rows []Recording
	if err := s.DB.Gorm.WithContext(ctx).Where("status = ? AND expired_at = 0 AND ended_at < ?", StatusReady, cut).
		Limit(500).Find(&rows).Error; err != nil {
		return fmt.Errorf("expired recordings: %w", err)
	}
	var errs []error
	for i := range rows {
		rec := rows[i]
		rec.trim()
		now := s.now().UnixMilli()
		summaryPost := ""
		expire := func(tx *gorm.DB) error {
			if err := tx.Model(&chat.FileInfo{}).Where("id = ? AND delete_at = 0", rec.FileID).
				Updates(map[string]any{"delete_at": now, "update_at": now}).Error; err != nil {
				return err
			}
			// the summary stays as the minutes of the meeting, its transcript goes with the recording
			// (add-meeting-summaries, design D9); read here, under the channel's lock a summary is posted with, so a
			// summary posted a moment ago is not missed
			var cur Recording
			if err := tx.Select("summary_post_id", "summary_source").Where("id = ?", rec.ID).Take(&cur).Error; err != nil {
				return err
			}
			summaryPost = strings.TrimSpace(cur.SummaryPostID)
			if summaryPost != "" {
				if err := tx.Model(&chat.FileInfo{}).Where("post_id = ? AND name LIKE ? AND delete_at = 0", summaryPost, TranscriptPrefix+"%").
					Updates(map[string]any{"delete_at": now, "update_at": now}).Error; err != nil {
					return err
				}
				if err := chat.MergePropsTx(tx, summaryPost, map[string]any{"recording_expired": true}); err != nil {
					return err
				}
			}
			up := map[string]any{"expired_at": now, "updated_at": now}
			if cur.SummarySource == SourceAvailable {
				up["summary_source"] = SourceExpired
			}
			return tx.Model(&Recording{}).Where("id = ?", rec.ID).Updates(up).Error
		}
		var err error
		if rec.PostID != "" {
			err = s.Chat.UpdateSystemPostProps(ctx, rec.PostID, map[string]any{"expired": true}, expire)
		}
		if rec.PostID == "" || errors.Is(err, chat.ErrNotFound) {
			err = s.DB.Gorm.WithContext(ctx).Transaction(expire)
		}
		if err == nil && summaryPost != "" { // announce the note (already stored) to the channel
			if perr := s.Chat.UpdateSystemPostProps(ctx, summaryPost, map[string]any{"recording_expired": true}, nil); perr != nil &&
				!errors.Is(perr, chat.ErrNotFound) {
				s.logger().Warn("recordings: summary not marked expired", "recording_id", rec.ID, "error", perr)
			}
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Volume is this server's capture volume identity.
func (s *Service) Volume() string { return s.volume }

// Unfinished counts the recordings still being captured, prepared or waiting for their volume: what a server
// started with recordings off would leave as they are.
func (s *Service) Unfinished(ctx context.Context) (int64, error) {
	var n int64
	err := s.DB.Gorm.WithContext(ctx).Model(&Recording{}).
		Where("status IN ?", []string{StatusRecording, StatusProcessing, StatusInterrupted}).Count(&n).Error
	return n, err
}
