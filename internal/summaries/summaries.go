// Package summaries writes the summary of a recorded meeting with an OpenAI-compatible AI service
// (add-meeting-summaries): the organization's Meeting summaries setting and its connection test, requests, the
// queued task that transcribes each participant's audio, merges it on the meeting's timeline and asks the model for
// the summary, and the lifecycle of what it derives. Nothing of a meeting leaves the server while the setting is off.
package summaries

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hibiken/asynq"
	"github.com/oklog/ulid/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/ai"
	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/files"
	"github.com/jniltinho/meethub/internal/jobs"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/recordings"
)

// Errors of the summaries API.
var (
	ErrNotConfigured     = errors.New("an AI service must be configured first")
	ErrOff               = errors.New("meeting summaries are off")
	ErrSourceUnavailable = errors.New("this recording cannot be summarized")
	ErrSourceExpired     = errors.New("this recording is too old to be summarized")
	ErrExists            = errors.New("this recording's summary is already queued, being written or written")
	ErrNotReady          = errors.New("the recording is not ready")
	ErrFFmpeg            = errors.New("the ffmpeg in use cannot make summary sources (it needs the silencedetect and ametadata filters)")
)

// Timings (design D2, D5).
const (
	TaskName     = "summaries:write"
	Queue        = "meethub:summaries"
	WatchEvery   = 5 * time.Second // the setting and the claim, while a summary runs
	ClaimFor     = 30 * time.Second
	MaxAttempts  = 4
	InFlight     = 4 // AI requests at once per server
	offChannel   = "meethub:summaries:off"
	codeSetting  = "cancelled_by_setting"
	codeDeleted  = "recording_deleted"
	codeInternal = "summary_failed"
	codeExpired  = "summary_source_expired"
)

// Service is the summaries of one server.
type Service struct {
	DB         *database.DB
	Chat       *chat.Service
	Recordings *recordings.Service // the objects' inventory, the storage and the recording role
	Storage    files.Storage
	Config     config.AI
	SecretKey  []byte     // MEETHUB_SECRET_KEY: seals the keys entered in Administration; nil: none can be stored
	Speech     *ai.Client // speech to text
	Writer     *ai.Client // the summary
	FFmpeg     *recordings.FFmpeg
	Dir        string // where the connection test's sample is made
	Queue      jobs.Queue
	Redis      *redis.Client // the "off" message between servers; nil: the 5-second re-read alone
	Logger     *slog.Logger
	Now        func() time.Time
	// Watch overrides WatchEvery and Claim ClaimFor (tests).
	Watch, Claim time.Duration

	once    sync.Once
	keys    atomic.Pointer[keys] // the AI keys in use, re-read at every entry point
	slots   chan struct{}
	mu      sync.Mutex
	running map[string]context.CancelCauseFunc
}

// Available reports whether summaries can be offered: both models and the keys are set (from the environment or
// entered in Administration, as last read) and recordings are on.
func (s *Service) Available() bool {
	if s == nil || s.Recordings == nil || !s.Recordings.Enabled() {
		return false
	}
	k := s.current()
	return s.Config.TranscriptionModel != "" && s.Config.SummaryModel != "" && k.main != "" && k.speech != ""
}

func (s *Service) init() {
	s.once.Do(func() {
		s.slots = make(chan struct{}, InFlight)
		s.running = map[string]context.CancelCauseFunc{}
	})
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.DiscardHandler)
}

func (s *Service) watchEvery() time.Duration {
	if s.Watch > 0 {
		return s.Watch
	}
	return WatchEvery
}

func (s *Service) claimFor() time.Duration {
	if s.Claim > 0 {
		return s.Claim
	}
	return ClaimFor
}

// Setting is the Meeting summaries setting as it applies: off whenever summaries are not available.
func (s *Service) Setting(ctx context.Context) (string, error) {
	if s.refresh(ctx); !s.Available() {
		return org.SummariesOff, nil
	}
	var v []string
	if err := s.DB.Gorm.WithContext(ctx).Model(&org.Organization{}).Pluck("meeting_summaries", &v).Error; err != nil {
		return "", fmt.Errorf("meeting summaries setting: %w", err)
	}
	if len(v) == 0 || !org.ValidSummaries(v[0]) {
		return org.SummariesOff, nil
	}
	return v[0], nil
}

// stored is the setting as the organization left it, whatever this server can do with it.
func (s *Service) stored(ctx context.Context) (string, error) {
	var v []string
	if err := s.DB.Gorm.WithContext(ctx).Model(&org.Organization{}).Pluck("meeting_summaries", &v).Error; err != nil {
		return "", fmt.Errorf("meeting summaries setting: %w", err)
	}
	if len(v) == 0 || !org.ValidSummaries(v[0]) {
		return org.SummariesOff, nil
	}
	return v[0], nil
}

// Policy is Setting for recordings: an error reads as off (nothing is sent on a doubt).
func (s *Service) Policy(ctx context.Context) string {
	v, err := s.Setting(ctx)
	if err != nil {
		return org.SummariesOff
	}
	return v
}

// SettingView is the setting as Administration shows it.
type SettingView struct {
	Value     string `json:"meeting_summaries"`
	Available bool   `json:"available"` // AI is configured and recordings are on
}

// GetSetting returns the setting to someone with manage_system.
func (s *Service) GetSetting(ctx context.Context, actor org.Actor) (*SettingView, error) {
	if err := actor.Require(org.PermManageSystem); err != nil {
		return nil, err
	}
	v, err := s.Setting(ctx)
	if err != nil {
		return nil, err
	}
	return &SettingView{Value: v, Available: s.Available()}, nil
}

// SetSetting changes the setting (design D2): turning it on runs the connection test first; turning it off
// cancels the queued and running summaries, on every server. Audited with both values.
func (s *Service) SetSetting(ctx context.Context, actor org.Actor, value string) (*SettingView, error) {
	if err := actor.Require(org.PermManageSystem); err != nil {
		return nil, err
	}
	if !org.ValidSummaries(value) {
		return nil, chat.ErrInvalidInput
	}
	if s.refresh(ctx); !s.Available() {
		return nil, ErrNotConfigured
	}
	old, err := s.Setting(ctx)
	if err != nil {
		return nil, err
	}
	if old == value {
		return &SettingView{Value: value, Available: true}, nil
	}
	if old == org.SummariesOff {
		if err := s.Test(ctx, actor); err != nil {
			return nil, err
		}
	}
	var cancelled []recordings.Recording
	err = s.DB.Gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := org.LockOrganization(ctx, tx, s.DB.Dialect); err != nil {
			return err
		}
		if value != org.SummariesOff {
			// a key removed since the test, on any server, wins: summaries never turn on without their keys
			if ok, err := s.keysPresent(ctx, tx); err != nil {
				return err
			} else if !ok {
				return ErrNotConfigured
			}
		}
		var err error
		cancelled, err = s.store(ctx, tx, actor, old, value)
		return err
	})
	if err != nil {
		return nil, err
	}
	if value == org.SummariesOff {
		s.afterOff(ctx, cancelled)
	}
	return &SettingView{Value: value, Available: true}, nil
}

// store writes the setting in tx, under the organization's lock, cancelling the queued and running summaries
// when it is off, and audits the change with both values. It returns what it cancelled, for afterOff.
func (s *Service) store(ctx context.Context, tx *gorm.DB, actor org.Actor, old, value string) ([]recordings.Recording, error) {
	var cancelled []recordings.Recording
	if err := tx.Model(&org.Organization{}).Where("singleton = 1").
		Updates(map[string]any{"meeting_summaries": value, "updated_at": time.Now().UTC()}).Error; err != nil {
		return nil, fmt.Errorf("meeting summaries setting: %w", err)
	}
	if value == org.SummariesOff {
		if err := tx.Where("summary_status IN ?", recordings.SummaryActive).Find(&cancelled).Error; err != nil {
			return nil, err
		}
		if err := tx.Model(&recordings.Recording{}).Where("summary_status IN ?", recordings.SummaryActive).
			Updates(map[string]any{"summary_status": recordings.SummaryCancelled, "summary_error": codeSetting,
				"summary_claim_until": 0}).Error; err != nil {
			return nil, fmt.Errorf("cancel summaries: %w", err)
		}
	}
	return cancelled, audit.Record(ctx, tx, actorEvent(actor, audit.ActionMeetingSummariesChanged, "organization", "",
		map[string]any{"old": old, "new": value}))
}

// afterOff stops what runs here, tells the other servers, and announces each cancelled summary.
func (s *Service) afterOff(ctx context.Context, cancelled []recordings.Recording) {
	s.stopAll()
	if s.Redis != nil {
		if err := s.Redis.Publish(context.WithoutCancel(ctx), offChannel, "off").Err(); err != nil {
			s.logger().WarnContext(ctx, "summaries: off message not sent; servers stop at their next re-read", "error", err)
		}
	}
	for i := range cancelled {
		cancelled[i].Trim()
		cancelled[i].SummaryStatus, cancelled[i].SummaryError = recordings.SummaryCancelled, codeSetting
		s.announce(context.WithoutCancel(ctx), &cancelled[i])
	}
}

func actorEvent(a org.Actor, action, targetType, targetID string, details map[string]any) audit.Event {
	e := audit.Event{Action: action, TargetType: targetType, TargetID: targetID, Details: details, ClientIP: a.ClientIP}
	if !a.System && a.UserID != "" {
		id := a.UserID
		e.ActorID = &id
	}
	return e
}

// testSample is the connection test's audio: a generated tone, encoded like a summary source (design D3).
func (s *Service) testSample(ctx context.Context) ([]byte, error) {
	if s.FFmpeg == nil {
		return nil, ErrNotConfigured
	}
	dir, err := os.MkdirTemp(s.Dir, "summary-test-")
	if err != nil {
		return nil, fmt.Errorf("connection test: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	out := filepath.Join(dir, "sample.ogg")
	// the filters that cut summary sources run too: an ffmpeg without them cannot make any
	if err := s.FFmpeg.Run(ctx, dir, "sample", time.Minute, "-f", "lavfi", "-i", "sine=f=440:sample_rate=16000:d=3",
		"-af", "silencedetect=noise=-40dB:d=0.3,ametadata=mode=print:file=meta.txt",
		"-ac", "1", "-ar", "16000", "-c:a", "libopus", "-b:a", "24k", out); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFFmpeg, err)
	}
	return os.ReadFile(out) //nolint:gosec // our own temporary directory
}

// Test checks the AI service with a generated sample and a one-line request (design D3): both must answer, and
// the transcription must be timed. It returns an *ai.Error on failure. Only the sample and a fixed line are sent.
func (s *Service) Test(ctx context.Context, actor org.Actor) error {
	if err := actor.Require(org.PermManageSystem); err != nil {
		return err
	}
	if s.refresh(ctx); !s.Available() {
		return ErrNotConfigured
	}
	sample, err := s.testSample(ctx)
	if err != nil {
		return err
	}
	if _, err := s.Speech.Transcribe(ctx, ai.TranscribeRequest{Model: s.Config.TranscriptionModel, Name: "sample.ogg",
		Audio: sample, Language: s.Config.Language}); err != nil {
		s.logger().InfoContext(ctx, "summaries: connection test failed", "step", "transcription", "class", ai.ClassOf(err), "status", ai.Status(err))
		return err
	}
	if _, err := s.Writer.Chat(ctx, ai.ChatRequest{Model: s.Config.SummaryModel, System: "Reply with the single word: ok",
		User: "ok", MaxTokens: s.Config.MaxOutputTokens}); err != nil {
		s.logger().InfoContext(ctx, "summaries: connection test failed", "step", "summary", "class", ai.ClassOf(err), "status", ai.Status(err))
		return err
	}
	return nil
}

// View is a recording's summary as its readers see it.
type View struct {
	RecordingID string `json:"recording_id"`
	Status      string `json:"status"` // none, queued, transcribing, summarizing, ready, failed, cancelled
	Error       string `json:"error,omitempty"`
	Source      string `json:"source"` // none, available, expired
	Policy      string `json:"policy"`
	PostID      string `json:"post_id,omitempty"`
	CanRequest  bool   `json:"can_request"` // the caller may ask for it now
}

// load reads a recording and checks that sub may read its channel.
func (s *Service) load(ctx context.Context, sub chat.Subject, id string) (*recordings.Recording, bool, error) {
	var rec recordings.Recording
	if err := s.DB.Gorm.WithContext(ctx).Where("id = ?", id).Take(&rec).Error; err != nil {
		if database.IsNotFound(err) {
			return nil, false, chat.ErrNotFound
		}
		return nil, false, fmt.Errorf("recording: %w", err)
	}
	rec.Trim()
	members := s.Recordings.Config.Start == config.RecordMembers
	_, may, err := s.Chat.RecordingAccess(ctx, sub, rec.ChannelID, members)
	if err != nil {
		return nil, false, err
	}
	gone, err := recordings.Gone(ctx, s.DB.Gorm, rec.ID)
	if err != nil {
		return nil, false, err
	}
	if gone && rec.ExpiredAt == 0 { // its message was deleted (an expired one still shows its state)
		return nil, false, chat.ErrNotFound
	}
	return &rec, may, nil
}

// sourceState is the recording's sources as the request sees them now.
func (s *Service) sourceState(rec *recordings.Recording) string {
	if rec.SummarySource == recordings.SourceAvailable && (rec.ExpiredAt != 0 || s.now().UnixMilli() > rec.SummarySourceExpiresAt) {
		return recordings.SourceExpired
	}
	return rec.SummarySource
}

func (s *Service) view(ctx context.Context, rec *recordings.Recording, may bool) *View {
	v := &View{RecordingID: rec.ID, Status: rec.SummaryStatus, Error: rec.SummaryError, Source: s.sourceState(rec),
		Policy: rec.SummaryPolicy, PostID: rec.SummaryPostID}
	setting := s.Policy(ctx)
	v.CanRequest = may && setting != org.SummariesOff && rec.Status == recordings.StatusReady &&
		v.Source == recordings.SourceAvailable && requestable(rec.SummaryStatus)
	return v
}

func requestable(status string) bool {
	return status == recordings.SummaryNone || status == recordings.SummaryFailed || status == recordings.SummaryCancelled
}

// Get returns a recording's summary state to its channel's readers.
func (s *Service) Get(ctx context.Context, sub chat.Subject, id string) (*View, error) {
	if s.refresh(ctx); !s.Available() {
		return nil, ErrNotConfigured
	}
	rec, may, err := s.load(ctx, sub, id)
	if err != nil {
		return nil, err
	}
	return s.view(ctx, rec, may), nil
}

// Request queues a recording's summary for someone who holds its channel's recording role (design D5): the thread
// says who asked, and the request is audited.
func (s *Service) Request(ctx context.Context, sub chat.Subject, id string) (*View, error) {
	if s.refresh(ctx); !s.Available() {
		return nil, ErrNotConfigured
	}
	rec, may, err := s.load(ctx, sub, id)
	if err != nil {
		return nil, err
	}
	if !may {
		return nil, chat.ErrForbidden
	}
	if setting := s.Policy(ctx); setting == org.SummariesOff {
		return nil, ErrOff
	}
	switch {
	case rec.Status != recordings.StatusReady || rec.PostID == "":
		return nil, ErrNotReady
	case s.sourceState(rec) == recordings.SourceExpired:
		return nil, ErrSourceExpired
	case rec.SummarySource != recordings.SourceAvailable:
		return nil, ErrSourceUnavailable
	case !requestable(rec.SummaryStatus):
		return nil, ErrExists
	}
	busy := false
	_, _, err = s.Chat.PublishSystemPost(ctx, chat.SystemPost{
		ChannelID: rec.ChannelID, UserID: sub.UserID, RootID: rec.PostID, Type: chat.PostTypeSummaryRequested,
		Key: "summary:req:" + ulid.Make().String(), Props: map[string]any{"recording_id": rec.ID},
	}, func(tx *gorm.DB, _ *chat.PostView) error {
		// the permission again, under the channel's lock: removed or demoted meanwhile, the request is refused
		may, err := chat.MayRecordTx(ctx, tx, sub, rec.ChannelID, s.Recordings.Config.Start == config.RecordMembers)
		if err != nil {
			return err
		}
		if !may {
			return chat.ErrForbidden
		}
		r := tx.Model(&recordings.Recording{}).Where("id = ? AND summary_status IN ? AND summary_source = ? AND expired_at = 0",
			rec.ID, []string{recordings.SummaryNone, recordings.SummaryFailed, recordings.SummaryCancelled}, recordings.SourceAvailable).
			Updates(map[string]any{"summary_status": recordings.SummaryQueued, "summary_error": "", "summary_claim_until": 0})
		if r.Error == nil && r.RowsAffected != 1 {
			busy = true
			return ErrExists
		}
		if r.Error != nil {
			return r.Error
		}
		uid := sub.UserID
		return audit.Record(ctx, tx, audit.Event{ActorID: &uid, Action: audit.ActionSummaryRequested,
			TargetType: "recording", TargetID: rec.ID, Details: map[string]any{"channel_id": rec.ChannelID}})
	})
	if busy {
		return nil, ErrExists
	}
	if err != nil {
		return nil, err
	}
	rec.SummaryStatus, rec.SummaryError = recordings.SummaryQueued, ""
	s.announce(ctx, rec)
	s.Enqueue(context.WithoutCancel(ctx), rec.ID, ulid.Make().String())
	return s.view(ctx, rec, may), nil
}

// Enqueue queues the summary task of a recording; suffix makes its task id unique ("": the reconciler's stable id,
// so a summary waiting for a worker is not queued twice). A lost enqueue is caught by the reconciler.
func (s *Service) Enqueue(ctx context.Context, id, suffix string) {
	if s.Queue == nil {
		return
	}
	tid := TaskName + ":" + id
	if suffix != "" {
		tid += ":" + suffix
	}
	err := s.Queue.Enqueue(ctx, asynq.NewTask(TaskName, []byte(id)), asynq.Queue(Queue), asynq.TaskID(tid))
	if err != nil && !errors.Is(err, asynq.ErrTaskIDConflict) {
		s.logger().WarnContext(ctx, "summaries: not queued; the reconciler queues it again", "recording_id", id, "error", err)
	}
}

// announce writes the summary's state on the recording's message, so every reader sees it change.
func (s *Service) announce(ctx context.Context, rec *recordings.Recording) {
	if rec.PostID == "" {
		return
	}
	summary := map[string]any{"status": rec.SummaryStatus, "source": rec.SummarySource}
	if rec.SummaryError != "" {
		summary["error"] = rec.SummaryError
	}
	if rec.SummaryPostID != "" {
		summary["post_id"] = rec.SummaryPostID
	}
	if err := s.Chat.UpdateSystemPostProps(ctx, rec.PostID, map[string]any{"summary": summary}, nil); err != nil &&
		!errors.Is(err, chat.ErrNotFound) {
		s.logger().WarnContext(ctx, "summaries: state not announced", "recording_id", rec.ID, "error", err)
	}
}

// Run listens for "off" from the other servers until ctx ends.
func (s *Service) Run(ctx context.Context) error {
	s.init()
	if s.Redis == nil {
		<-ctx.Done()
		return nil
	}
	sub := s.Redis.Subscribe(ctx, offChannel)
	defer func() { _ = sub.Close() }()
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-ch:
			if !ok {
				return nil
			}
			s.stopAll()
		}
	}
}

// errSwitchedOff, errClaimLost and recordings.ErrGone end a running summary.
var (
	errSwitchedOff   = errors.New("meeting summaries were switched off")
	errClaimLost     = errors.New("the summary is no longer this task's")
	errSourceExpired = errors.New("the summary sources expired")
)

func (s *Service) stopAll() {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cancel := range s.running {
		cancel(errSwitchedOff)
	}
}

// codeOf is the failure code a recording shows for err.
func codeOf(err error) string {
	var e *ai.Error
	if errors.As(err, &e) {
		return e.Code()
	}
	return codeInternal
}

// trimmed lowercases nothing; it keeps a display name to one line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
