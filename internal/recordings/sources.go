package recordings

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

// Summary sources (add-meeting-summaries, design D4): each participant's voice re-encoded for speech and cut, in
// pauses, into pieces small enough for one transcription request, kept in the file storage and listed in
// recording_objects, where every object is registered before it is written.

// Kinds and states of recording_objects.
const (
	ObjectSource     = "source"     // a piece of one participant's audio
	ObjectCheckpoint = "checkpoint" // the transcription of a piece
	ObjectPending    = "pending"
	ObjectStored     = "stored"
)

// States of a recording's summary sources.
const (
	SourceNone      = "none"
	SourceAvailable = "available"
	SourceExpired   = "expired"
)

// States of a recording's summary (design D5); queued, transcribing and summarizing are active.
const (
	SummaryNone         = "none"
	SummaryQueued       = "queued"
	SummaryTranscribing = "transcribing"
	SummarySummarizing  = "summarizing"
	SummaryReady        = "ready"
	SummaryFailed       = "failed"
	SummaryCancelled    = "cancelled"
)

// SummaryActive are the states of a summary being written.
var SummaryActive = []string{SummaryQueued, SummaryTranscribing, SummarySummarizing}

// TranscriptPrefix starts the name of a summary's transcript file (recording retention removes it).
const TranscriptPrefix = "transcript-"

// Summary sources: how long they are kept and how they are cut.
const (
	SourceKeep      = 7 * 24 * time.Hour
	pieceMax        = 5 * time.Minute
	pieceRate       = 3000 // bytes per second: 24 kbit/s Opus
	minPause        = 300 * time.Millisecond
	pausePad        = 150 // ms of the pause kept on each side of a cut
	silenceNoise    = "-40dB"
	maxPieceRetries = 3 // a piece over the byte limit is halved at most this many times
)

// Object is a row of recording_objects.
type Object struct {
	ID          string `gorm:"primaryKey;size:26"`
	RecordingID string `gorm:"size:26"`
	Generation  int64
	ObjectKey   string
	Kind        string
	Speaker     string `gorm:"size:26"`
	OffsetMS    int64  `gorm:"column:offset_ms"`
	DurationMS  int64  `gorm:"column:duration_ms"`
	State       string
	CreateAt    int64 `gorm:"autoCreateTime:false"`
}

// TableName maps Object to recording_objects.
func (Object) TableName() string { return "recording_objects" }

// Trim removes the padding of CHAR columns.
func (o *Object) Trim() {
	o.ID, o.RecordingID, o.Speaker = strings.TrimSpace(o.ID), strings.TrimSpace(o.RecordingID), strings.TrimSpace(o.Speaker)
}

// ErrGone is returned when the recording was deleted or expired: nothing more may be written for it.
var ErrGone = errors.New("the recording was deleted or has expired")

// Gone reports whether a recording was deleted (its row or its message) or expired (design D9).
func Gone(ctx context.Context, db *gorm.DB, id string) (bool, error) {
	var rows []struct {
		ExpiredAt int64
		PostID    string
		DeleteAt  *int64
	}
	err := db.WithContext(ctx).Raw(`SELECT r.expired_at, r.post_id, p.delete_at FROM meeting_recordings r
		LEFT JOIN posts p ON p.id = r.post_id WHERE r.id = ?`, id).Scan(&rows).Error
	if err != nil {
		return false, fmt.Errorf("recording state: %w", err)
	}
	if len(rows) == 0 || rows[0].ExpiredAt != 0 {
		return true, nil
	}
	return strings.TrimSpace(rows[0].PostID) != "" && (rows[0].DeleteAt == nil || *rows[0].DeleteAt != 0), nil
}

func goneErr(gone bool, err error) error {
	if err == nil && gone {
		return ErrGone
	}
	return err
}

// Register inserts an object's row as pending; Store marks it stored once it is written. Writing is checked
// against the recording's deleted or expired mark before and after: an object written for a recording that went
// away in the meantime is removed with its row.
func (s *Service) Register(ctx context.Context, o *Object) error {
	if err := goneErr(Gone(ctx, s.DB.Gorm, o.RecordingID)); err != nil {
		return err
	}
	o.ID, o.State, o.CreateAt = ulid.Make().String(), ObjectPending, s.now().UnixMilli()
	if err := s.DB.Gorm.WithContext(ctx).Create(o).Error; err != nil {
		return fmt.Errorf("register object: %w", err)
	}
	return nil
}

// PutObject writes a registered object and marks it stored.
func (s *Service) PutObject(ctx context.Context, o *Object, data []byte) error {
	if err := s.Storage.Put(ctx, o.ObjectKey, bytes.NewReader(data), int64(len(data))); err != nil {
		return fmt.Errorf("store object: %w", err)
	}
	if err := goneErr(Gone(ctx, s.DB.Gorm, o.RecordingID)); err != nil {
		s.RemoveObject(context.WithoutCancel(ctx), o)
		return err
	}
	if err := s.DB.Gorm.WithContext(ctx).Model(&Object{}).Where("id = ?", o.ID).Update("state", ObjectStored).Error; err != nil {
		return fmt.Errorf("mark object stored: %w", err)
	}
	o.State = ObjectStored
	return nil
}

// RemoveObject deletes an object and then its row (a failed deletion keeps the row, for the next cleanup).
func (s *Service) RemoveObject(ctx context.Context, o *Object) {
	if err := s.Storage.Delete(ctx, o.ObjectKey); err != nil {
		return
	}
	_ = s.DB.Gorm.WithContext(ctx).Where("id = ?", o.ID).Delete(&Object{}).Error
}

// sourcesFor makes the sources of one participant's flattened track while rec is composed; any failure leaves the
// recording without sources (its summary unavailable) and is logged by class only.
func (s *Service) sourcesFor(rec *Recording, failed *bool) func(ctx context.Context, participant, track string, total int64) {
	return func(ctx context.Context, participant, track string, total int64) {
		if *failed {
			return
		}
		if err := s.makeSources(ctx, rec, participant, track, total); err != nil {
			*failed = true
			class := "sources_failed"
			var fe *FFmpegError
			if errors.As(err, &fe) {
				class = fe.Class
			}
			s.logger().Warn("recordings: summary sources not made", "recording_id", rec.ID, "class", class)
		}
	}
}

// pieceLimit is the longest piece, in ms, that stays well under a quarter of the upload limit at pieceRate.
func (s *Service) pieceLimit() int64 {
	byBytes := s.PieceBytes * 1000 / (2 * pieceRate) // twice the nominal rate: Opus may overshoot it on loud speech
	return max(min(pieceMax.Milliseconds(), byBytes), 10_000)
}

func (s *Service) makeSources(ctx context.Context, rec *Recording, participant, track string, total int64) error {
	dir := filepath.Dir(track)
	base := strings.TrimSuffix(filepath.Base(track), ".ogg")
	meta := filepath.Join(dir, base+"-silence.txt")
	defer removeAll(meta)
	limit := timeLimit(time.Duration(total) * time.Millisecond)
	if err := s.FFmpeg.Run(ctx, dir, base+"-silence", limit, "-i", track, "-af",
		fmt.Sprintf("silencedetect=noise=%s:d=%s,ametadata=mode=print:file=%s", silenceNoise,
			sec(minPause.Milliseconds()), filepath.Base(meta)), "-f", "null", "-"); err != nil {
		return err
	}
	silences, err := readSilences(meta, total)
	if err != nil {
		return err
	}
	for i, p := range cutPieces(silences, total, s.pieceLimit()) {
		if err := s.storePiece(ctx, rec, participant, track, fmt.Sprintf("%s-%03d", base, i), p, 0); err != nil {
			return err
		}
	}
	return nil
}

// storePiece encodes [from, to) of track for speech, registers it, uploads it and marks it stored; a piece over
// the byte limit is halved.
func (s *Service) storePiece(ctx context.Context, rec *Recording, participant, track, name string, p [2]int64, depth int) error {
	dir := filepath.Dir(track)
	out := filepath.Join(dir, name+".ogg")
	defer removeAll(out)
	if err := s.FFmpeg.Run(ctx, dir, name, timeLimit(time.Duration(p[1]-p[0])*time.Millisecond),
		"-ss", sec(p[0]), "-t", sec(p[1]-p[0]), "-i", track, "-ac", "1", "-ar", "16000", "-c:a", "libopus", "-b:a", "24k",
		"-vbr", "constrained", "-application", "voip", out); err != nil {
		return err
	}
	data, err := os.ReadFile(out) //nolint:gosec // this package's own capture directory
	if err != nil {
		return err
	}
	if int64(len(data)) > s.PieceBytes {
		if depth >= maxPieceRetries {
			return fmt.Errorf("piece of %d bytes over the limit", len(data))
		}
		mid := (p[0] + p[1]) / 2
		if err := s.storePiece(ctx, rec, participant, track, name+"a", [2]int64{p[0], mid}, depth+1); err != nil {
			return err
		}
		return s.storePiece(ctx, rec, participant, track, name+"b", [2]int64{mid, p[1]}, depth+1)
	}
	o := &Object{RecordingID: rec.ID, Generation: rec.Generation, Kind: ObjectSource, Speaker: participant,
		OffsetMS: p[0], DurationMS: p[1] - p[0],
		ObjectKey: fmt.Sprintf("summaries/%s/g%d/sources/%s.ogg", rec.ID, rec.Generation, name)}
	if err := s.Register(ctx, o); err != nil {
		return err
	}
	return s.PutObject(ctx, o, data)
}

// readSilences parses the silencedetect metadata ametadata printed: [start, end) ms of every pause.
func readSilences(path string, total int64) ([][2]int64, error) {
	f, err := os.Open(path) //nolint:gosec // this package's own capture directory
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out [][2]int64
	start := int64(-1)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		x, err := strconv.ParseFloat(v, 64)
		if err != nil {
			continue
		}
		ms := min(max(int64(x*1000), 0), total)
		switch k {
		case "lavfi.silence_start":
			start = ms
		case "lavfi.silence_end":
			if start >= 0 {
				out = append(out, [2]int64{start, ms})
			}
			start = -1
		}
	}
	if start >= 0 { // silent to the end
		out = append(out, [2]int64{start, total})
	}
	return out, sc.Err()
}

// cutPieces turns the pauses of a track of total ms into the pieces to transcribe, each at most maxLen ms: the
// stretches of speech, joined while they fit and cut inside the pauses between them (keeping pausePad ms of the
// pause on each side). A stretch of speech longer than maxLen is cut at maxLen, where a sentence may be split;
// long silences are not sent at all.
func cutPieces(silences [][2]int64, total, maxLen int64) [][2]int64 {
	var speech [][2]int64
	at := int64(0)
	for _, s := range silences {
		if s[0] > at {
			speech = append(speech, [2]int64{at, s[0]})
		}
		at = max(at, s[1])
	}
	if at < total {
		speech = append(speech, [2]int64{at, total})
	}
	var out [][2]int64
	prevEnd := int64(0) // where the previous piece ended: a piece never overlaps it
	for i := 0; i < len(speech); {
		from := max(speech[i][0]-pausePad, prevEnd)
		to := speech[i][1]
		j := i + 1
		for j < len(speech) && speech[j][1]+pausePad-from <= maxLen {
			to = speech[j][1]
			j++
		}
		if to-from > maxLen { // one stretch longer than a piece: cut mid-speech
			out = append(out, [2]int64{from, from + maxLen})
			prevEnd = from + maxLen
			speech[i][0] = prevEnd + pausePad // the rest of the stretch starts at the cut (pausePad undone below)
			continue
		}
		end := to + pausePad
		if j < len(speech) {
			end = min(end, (to+speech[j][0])/2)
		}
		end = min(end, total)
		out = append(out, [2]int64{from, end})
		prevEnd = end
		i = j
	}
	return out
}
