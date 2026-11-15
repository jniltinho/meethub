package summaries_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/oklog/ulid/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/jniltinho/meethub/internal/ai"
	"github.com/jniltinho/meethub/internal/ai/aitest"
	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/files"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/recordings"
	"github.com/jniltinho/meethub/internal/summaries"
	"github.com/jniltinho/meethub/internal/testenv"
)

var bg = context.Background()

const testKey = "sk-test-key-never-logged"

// queue records what was enqueued.
type queue struct {
	mu  sync.Mutex
	ids []string
}

func (q *queue) Enqueue(_ context.Context, t *asynq.Task, _ ...asynq.Option) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ids = append(q.ids, string(t.Payload()))
	return nil
}

func (q *queue) count(id string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, x := range q.ids {
		if x == id {
			n++
		}
	}
	return n
}

type world struct {
	db                           *database.DB
	chat                         *chat.Service
	store                        files.Storage
	rec                          *recordings.Service
	fake                         *aitest.Server
	q                            *queue
	logs                         *bytes.Buffer
	logMu                        *sync.Mutex
	owner, admin, ana, bob, carl chat.Subject
	ch                           *chat.Channel
	ff                           *recordings.FFmpeg
	// answers maps a piece's content to the transcription answer (default: no speech)
	mu      sync.Mutex
	answers map[string]func() aitest.Answer
}

type lockedWriter struct {
	mu *sync.Mutex
	b  *bytes.Buffer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func newWorld(t *testing.T, db *database.DB) *world {
	t.Helper()
	if _, err := org.Create(bg, db.Gorm, "Acme"); err != nil {
		t.Fatal(err)
	}
	users := &org.Users{DB: db, Hasher: identity.NewHasher(identity.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1}, 8, time.Second)}
	mk := func(name, role string) chat.Subject {
		m, err := users.Create(bg, org.SystemActor, org.NewUser{Email: name + "@acme.io", DisplayName: strings.ToUpper(name[:1]) + name[1:] + " Silva",
			Role: role, Password: "a long password!"})
		if err != nil {
			t.Fatal(err)
		}
		return chat.Subject{UserID: m.ID, OrgRole: role}
	}
	w := &world{db: db, q: &queue{}, logs: &bytes.Buffer{}, logMu: &sync.Mutex{}, answers: map[string]func() aitest.Answer{}}
	w.chat = &chat.Service{DB: db}
	w.owner, w.admin, w.ana, w.bob, w.carl = mk("owner", org.RoleOwner), mk("admin", org.RoleAdmin), mk("ana", org.RoleMember),
		mk("bruno", org.RoleMember), mk("carla", org.RoleMember)
	team, err := w.chat.CreateTeam(bg, w.owner, chat.NewTeam{Name: "eng", DisplayName: "Eng"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []chat.Subject{w.ana, w.bob, w.carl} {
		if err := w.chat.AddTeamMember(bg, w.owner, team.ID, s.UserID); err != nil {
			t.Fatal(err)
		}
	}
	if w.ch, err = w.chat.CreateChannel(bg, w.ana, team.ID, chat.NewChannel{Name: "projects", DisplayName: "Projects", Type: chat.ChannelPrivate}); err != nil {
		t.Fatal(err)
	}
	if err := w.chat.AddChannelMember(bg, w.ana, w.ch.ID, w.bob.UserID); err != nil {
		t.Fatal(err)
	}
	if w.store, err = files.NewLocal(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		w.ff = &recordings.FFmpeg{Path: p}
	}
	w.rec = &recordings.Service{DB: db, Chat: w.chat, Storage: w.store, FFmpeg: w.ff, Dir: t.TempDir(),
		Config: config.Recordings{Enabled: true, Start: config.RecordAdmins, RetentionDays: 30}}
	w.fake = aitest.New()
	t.Cleanup(w.fake.Close)
	w.fake.Transcribe = func(c aitest.Call) aitest.Answer {
		w.mu.Lock()
		f := w.answers[string(c.Audio)]
		w.mu.Unlock()
		if f == nil {
			return aitest.Segments([3]any{0.0, 10.0, "[BLANK_AUDIO]"})
		}
		return f()
	}
	return w
}

func (w *world) service(t *testing.T) *summaries.Service {
	t.Helper()
	cfg := config.AI{APIKey: testKey, TranscriptionModel: "whisper", SummaryModel: "llm", Timeout: config.Duration(10 * time.Second),
		MaxUploadBytes: 20_000_000, ContextTokens: 32000, MaxOutputTokens: 8000}
	return &summaries.Service{DB: w.db, Chat: w.chat, Recordings: w.rec, Storage: w.store, Config: cfg, FFmpeg: w.ff,
		Dir: t.TempDir(), Queue: w.q, Watch: 100 * time.Millisecond, Claim: 2 * time.Second,
		Speech: &ai.Client{BaseURL: w.fake.URL(), Key: testKey}, Writer: &ai.Client{BaseURL: w.fake.URL(), Key: testKey},
		Logger: slog.New(slog.NewTextHandler(lockedWriter{w.logMu, w.logs}, &slog.HandlerOptions{Level: slog.LevelDebug}))}
}

func (w *world) logText() string {
	w.logMu.Lock()
	defer w.logMu.Unlock()
	return w.logs.String()
}

func (w *world) setting(t *testing.T, v string) {
	t.Helper()
	if err := w.db.Gorm.Model(&org.Organization{}).Where("singleton = 1").Update("meeting_summaries", v).Error; err != nil {
		t.Fatal(err)
	}
}

func (w *world) answer(tag string, f func() aitest.Answer) {
	w.mu.Lock()
	w.answers[tag] = f
	w.mu.Unlock()
}

// piece is one summary source: whose, where on the recording (ms), and its content (what the fake answers to).
type piece struct {
	who         chat.Subject
	offset, dur int64
	tag         string
}

// recording makes a ready recording of #projects started by Ana under policy, with its sources.
func (w *world) recording(t *testing.T, policy string, pieces ...piece) *recordings.Recording {
	t.Helper()
	now := time.Now()
	rec := &recordings.Recording{ID: ulid.Make().String(), ChannelID: w.ch.ID, Room: "room", StartedBy: w.ana.UserID, Mode: "audio",
		Status: recordings.StatusReady, StartedAt: now.Add(-20 * time.Minute).UnixMilli(), EndedAt: now.Add(-time.Minute).UnixMilli(),
		Volume: "v", Generation: 1, FileID: ulid.Make().String(), CreatedAt: now.UnixMilli(), UpdatedAt: now.UnixMilli(),
		SummaryPolicy: policy, SummarySource: recordings.SourceNone, SummaryStatus: recordings.SummaryNone}
	if policy != org.SummariesOff {
		rec.SummarySource, rec.SummarySourceExpiresAt = recordings.SourceAvailable, rec.EndedAt+recordings.SourceKeep.Milliseconds()
	}
	rec.DurationMS = rec.EndedAt - rec.StartedAt
	if err := w.db.Gorm.Create(rec).Error; err != nil {
		t.Fatal(err)
	}
	p, _, err := w.chat.PublishSystemPost(bg, chat.SystemPost{ChannelID: w.ch.ID, UserID: w.ana.UserID, Type: chat.PostTypeRecording,
		Key: "recording:" + rec.ID, Props: map[string]any{"recording_id": rec.ID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec.PostID = p.ID
	if err := w.db.Gorm.Model(rec).Update("post_id", p.ID).Error; err != nil {
		t.Fatal(err)
	}
	for i, pc := range pieces {
		o := &recordings.Object{RecordingID: rec.ID, Generation: 1, Kind: recordings.ObjectSource, Speaker: pc.who.UserID,
			OffsetMS: pc.offset, DurationMS: pc.dur, ObjectKey: fmt.Sprintf("summaries/%s/g1/sources/p%02d-%03d.ogg", rec.ID, i, i)}
		if err := w.rec.Register(bg, o); err != nil {
			t.Fatal(err)
		}
		if err := w.rec.PutObject(bg, o, []byte(pc.tag)); err != nil {
			t.Fatal(err)
		}
	}
	return rec
}

func (w *world) row(t *testing.T, id string) recordings.Recording {
	t.Helper()
	var r recordings.Recording
	if err := w.db.Gorm.Where("id = ?", id).Take(&r).Error; err != nil {
		t.Fatal(err)
	}
	r.Trim()
	return r
}

func (w *world) objects(t *testing.T, id string) int64 {
	t.Helper()
	var n int64
	if err := w.db.Gorm.Model(&recordings.Object{}).Where("recording_id = ?", id).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// replies are the thread's replies to the recording's message.
func (w *world) replies(t *testing.T, rec *recordings.Recording) []chat.PostView {
	t.Helper()
	posts, _, err := w.chat.Thread(bg, w.ana, rec.PostID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []chat.PostView
	for _, p := range posts {
		if p.ID != rec.PostID {
			out = append(out, p)
		}
	}
	return out
}

func typed(posts []chat.PostView, typ string) []chat.PostView {
	var out []chat.PostView
	for _, p := range posts {
		if p.Type == typ {
			out = append(out, p)
		}
	}
	return out
}

func said(start, end float64, text string) func() aitest.Answer {
	return func() aitest.Answer { return aitest.Segments([3]any{start, end, text}) }
}

func needFFmpeg(t *testing.T, w *world) {
	t.Helper()
	if w.ff == nil {
		t.Skip("no ffmpeg in PATH (make test mounts the image's own)")
	}
}

func TestSetting(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t)
		owner := org.Actor{UserID: w.owner.UserID, Role: org.RoleOwner}
		admin := org.Actor{UserID: w.admin.UserID, Role: org.RoleAdmin}
		if _, err := s.SetSetting(bg, admin, org.SummariesAutomatic); !errors.Is(err, org.ErrForbidden) {
			t.Fatalf("admin: %v", err)
		}
		if v, err := s.GetSetting(bg, owner); err != nil || v.Value != org.SummariesOff || !v.Available {
			t.Fatalf("default: %+v %v", v, err)
		}
		// not configured: unavailable, and reads off
		bare := w.service(t)
		bare.Config.APIKey = ""
		if v, err := bare.GetSetting(bg, owner); err != nil || v.Available || v.Value != org.SummariesOff {
			t.Fatalf("not configured: %+v %v", v, err)
		}
		if _, err := bare.SetSetting(bg, owner, org.SummariesManual); !errors.Is(err, summaries.ErrNotConfigured) {
			t.Fatalf("not configured: %v", err)
		}
		needFFmpeg(t, w)
		// a transcription without timing is refused, and the setting stays off
		w.fake.Transcribe = func(aitest.Call) aitest.Answer { return aitest.Answer{Body: `{"text":"hello"}`} }
		if _, err := s.SetSetting(bg, owner, org.SummariesAutomatic); ai.ClassOf(err) != ai.ClassUnsupported {
			t.Fatalf("untimed: %v", err)
		}
		// a refused key
		w.fake.Transcribe = func(aitest.Call) aitest.Answer {
			return aitest.Answer{Status: http.StatusUnauthorized, Body: `{"error":"` + testKey + `"}`}
		}
		if _, err := s.SetSetting(bg, owner, org.SummariesAutomatic); ai.ClassOf(err) != ai.ClassUnauthorized || strings.Contains(err.Error(), testKey) {
			t.Fatalf("key: %v", err)
		}
		// unreachable
		down := w.service(t)
		down.Speech = &ai.Client{BaseURL: "http://127.0.0.1:1/v1", Key: testKey}
		if _, err := down.SetSetting(bg, owner, org.SummariesAutomatic); ai.ClassOf(err) != ai.ClassUnavailable {
			t.Fatalf("unreachable: %v", err)
		}
		if v, _ := s.Setting(bg); v != org.SummariesOff {
			t.Fatalf("after failed tests: %s", v)
		}
		if n := w.fake.Count("/chat/completions"); n != 0 {
			t.Fatalf("chat asked %d times before the transcription passed", n)
		}
		// passing: on, audited with both values; the test sent a generated sample and one fixed line
		w.fake.Transcribe = nil
		if v, err := s.SetSetting(bg, owner, org.SummariesAutomatic); err != nil || v.Value != org.SummariesAutomatic {
			t.Fatalf("on: %+v %v", v, err)
		}
		calls := w.fake.Calls()
		last := calls[len(calls)-1]
		if last.User != "ok" || last.MaxToks != 8000 || calls[len(calls)-2].Fields["response_format"] != "verbose_json" ||
			calls[len(calls)-2].Auth != "Bearer "+testKey {
			t.Fatalf("test requests: %+v", calls[len(calls)-2:])
		}
		evs, err := audit.List(bg, db.Gorm, audit.Filter{Action: audit.ActionMeetingSummariesChanged})
		if err != nil || len(evs.Events) != 1 || evs.Events[0].Details["old"] != "off" || evs.Events[0].Details["new"] != "automatic" ||
			evs.Events[0].ActorID == nil || *evs.Events[0].ActorID != w.owner.UserID {
			t.Fatalf("audit: %+v %v", evs, err)
		}
		// automatic → manual needs no new test
		before := len(w.fake.Calls())
		if _, err := s.SetSetting(bg, owner, org.SummariesManual); err != nil || len(w.fake.Calls()) != before {
			t.Fatalf("manual: %v", err)
		}
		if strings.Contains(w.logText(), testKey) {
			t.Fatal("the key is in the logs")
		}
	})
}

func TestSummary(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t)
		w.setting(t, org.SummariesManual)
		// Ana at minute nine, Bruno right after; a pause of silence from Ana's microphone in between
		rec := w.recording(t, org.SummariesManual,
			piece{w.ana, 540_000, 10_000, "ana-1"}, piece{w.bob, 543_000, 8_000, "bob-1"}, piece{w.ana, 600_000, 5_000, "ana-2"})
		w.answer("ana-1", said(0.5, 2.5, " we ship on Friday"))
		w.answer("bob-1", said(1.0, 3.0, "the tests are not done"))
		w.answer("ana-2", said(0.0, 5.0, "[BLANK_AUDIO]"))
		// Bruno may not ask; Ana (the channel's administrator) may, without being in any room
		if _, err := s.Request(bg, w.bob, rec.ID); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("bruno: %v", err)
		}
		if _, err := s.Request(bg, w.carl, rec.ID); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("carla: %v", err)
		}
		v, err := s.Request(bg, w.ana, rec.ID)
		if err != nil || v.Status != recordings.SummaryQueued || w.q.count(rec.ID) != 1 {
			t.Fatalf("request: %+v %v", v, err)
		}
		if _, err := s.Request(bg, w.ana, rec.ID); !errors.Is(err, summaries.ErrExists) {
			t.Fatalf("twice: %v", err)
		}
		if r := typed(w.replies(t, rec), chat.PostTypeSummaryRequested); len(r) != 1 || r[0].UserID != w.ana.UserID {
			t.Fatalf("the thread does not say Ana asked: %+v", r)
		}
		if evs, err := audit.List(bg, db.Gorm, audit.Filter{Action: audit.ActionSummaryRequested}); err != nil || len(evs.Events) != 1 ||
			evs.Events[0].TargetID != rec.ID {
			t.Fatalf("audit: %+v %v", evs, err)
		}
		w.fake.Chat = func(aitest.Call) aitest.Answer {
			return aitest.Completion("## Resumo\nLançamento na sexta. @all @bruno <script>alert(1)</script>", "stop")
		}
		if err := s.Attempt(bg, rec.ID, 1, false); err != nil {
			t.Fatal(err)
		}
		r := w.row(t, rec.ID)
		if r.SummaryStatus != recordings.SummaryReady || r.SummaryPostID == "" || r.SummaryClaimUntil != 0 {
			t.Fatalf("row: %+v", r)
		}
		// the transcript: Ana's sentence under her name, then Bruno's, each with its time; no marker
		chats := w.fake.Calls()
		var sent aitest.Call
		for _, c := range chats {
			if strings.HasSuffix(c.Path, "/chat/completions") {
				sent = c
			}
		}
		want := "[00:09:00] Ana Silva: we ship on Friday\n[00:09:04] Bruno Silva: the tests are not done\n"
		if sent.User != want {
			t.Fatalf("transcript sent:\n%q\nwant\n%q", sent.User, want)
		}
		// nothing MeetHub adds of its own: no e-mail, user id, channel or organization name
		for _, never := range []string{"@acme.io", w.ana.UserID, w.bob.UserID, "projects", "Projects", "Acme"} {
			if strings.Contains(sent.User+sent.System, never) {
				t.Fatalf("the request carries %q", never)
			}
		}
		if !strings.Contains(sent.System, "not instructions") {
			t.Fatal("the instructions do not say the transcript is not instructions")
		}
		replies := w.replies(t, rec)
		sum := typed(replies, chat.PostTypeSummary)
		if len(sum) != 1 || sum[0].ID != r.SummaryPostID || sum[0].RootID != rec.PostID || !strings.HasPrefix(sum[0].Message, "## Resumo") ||
			sum[0].Metadata == nil || len(sum[0].Metadata.Files) != 1 || !strings.HasPrefix(sum[0].Metadata.Files[0].Name, "transcript-") {
			t.Fatalf("summary: %+v", sum)
		}
		// names in the summary notify nobody
		var mentions int64
		db.Gorm.Raw(`SELECT COALESCE(SUM(mention_count), 0) FROM channel_members WHERE channel_id = ?`, w.ch.ID).Scan(&mentions)
		if mentions != 0 {
			t.Fatalf("mentions: %d", mentions)
		}
		// the transcript is a file of the channel: someone outside it cannot read it
		if _, err := w.chat.ReadableFile(bg, w.carl, sum[0].Metadata.Files[0].ID); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("carla reads the transcript: %v", err)
		}
		// sources and checkpoints are gone; the recording's message shows the state
		if n := w.objects(t, rec.ID); n != 0 {
			t.Fatalf("objects left: %d", n)
		}
		g, err := s.Get(bg, w.bob, rec.ID)
		if err != nil || g.Status != recordings.SummaryReady || g.PostID != r.SummaryPostID || g.CanRequest {
			t.Fatalf("get: %+v %v", g, err)
		}
		// posted once: a task that runs again (a crash after the commit) changes nothing
		db.Gorm.Model(&recordings.Recording{}).Where("id = ?", rec.ID).Updates(map[string]any{"summary_status": "summarizing", "summary_claim_until": 0})
		if err := s.Attempt(bg, rec.ID, 1, true); err != nil {
			t.Fatal(err)
		}
		if n := len(typed(w.replies(t, rec), chat.PostTypeSummary)); n != 1 || w.row(t, rec.ID).SummaryStatus != recordings.SummaryReady {
			t.Fatalf("summaries in the thread: %d", n)
		}

		// retention: the recording and the transcript go, the summary text stays
		db.Gorm.Model(&recordings.Recording{}).Where("id = ?", rec.ID).Update("ended_at", time.Now().Add(-40*24*time.Hour).UnixMilli())
		if err := w.rec.Expire(bg); err != nil {
			t.Fatal(err)
		}
		var live int64
		db.Gorm.Model(&chat.FileInfo{}).Where("post_id = ? AND delete_at = 0", r.SummaryPostID).Count(&live)
		after := typed(w.replies(t, rec), chat.PostTypeSummary)
		if live != 0 || len(after) != 1 || !strings.HasPrefix(after[0].Message, "## Resumo") || after[0].Props["recording_expired"] != true {
			t.Fatalf("retention: files %d, %+v", live, after)
		}
	})
}

func TestRequestRules(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t)
		w.setting(t, org.SummariesManual)
		// made while summaries were off: never summarizable
		old := w.recording(t, org.SummariesOff)
		if _, err := s.Request(bg, w.ana, old.ID); !errors.Is(err, summaries.ErrSourceUnavailable) {
			t.Fatalf("made under off: %v", err)
		}
		if g, err := s.Get(bg, w.ana, old.ID); err != nil || g.CanRequest || g.Source != recordings.SourceNone {
			t.Fatalf("get: %+v %v", g, err)
		}
		// eight days old, never summarized
		aged := w.recording(t, org.SummariesManual)
		db.Gorm.Model(&recordings.Recording{}).Where("id = ?", aged.ID).Update("summary_source_expires_at", time.Now().Add(-24*time.Hour).UnixMilli())
		if _, err := s.Request(bg, w.ana, aged.ID); !errors.Is(err, summaries.ErrSourceExpired) {
			t.Fatalf("expired: %v", err)
		}
		// with the setting off nothing is accepted
		fresh := w.recording(t, org.SummariesManual)
		w.setting(t, org.SummariesOff)
		if _, err := s.Request(bg, w.ana, fresh.ID); !errors.Is(err, summaries.ErrOff) {
			t.Fatalf("off: %v", err)
		}
		if n := len(w.fake.Calls()); n != 0 {
			t.Fatalf("%d requests to the AI service", n)
		}
	})
}

func TestRetryReusesCheckpoints(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t)
		w.setting(t, org.SummariesManual)
		var ps []piece
		for i := range 10 {
			tag := fmt.Sprintf("p%d", i)
			ps = append(ps, piece{w.ana, int64(i) * 60_000, 50_000, tag})
			w.answer(tag, said(1, 2, fmt.Sprintf("sentence %d", i)))
		}
		failing := true
		w.answer("p9", func() aitest.Answer {
			if failing {
				return aitest.Answer{Status: http.StatusServiceUnavailable, Body: `{"error":"busy"}`}
			}
			return aitest.Segments([3]any{1.0, 2.0, "sentence 9"})
		})
		rec := w.recording(t, org.SummariesManual, ps...)
		if _, err := s.Request(bg, w.ana, rec.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Attempt(bg, rec.ID, 1, false); err == nil {
			t.Fatal("the first attempt should ask to be tried again")
		}
		if r := w.row(t, rec.ID); r.SummaryStatus != recordings.SummaryTranscribing || r.SummaryClaimUntil != 0 {
			t.Fatalf("after a retryable failure: %+v", r)
		}
		if n := w.fake.Count("/audio/transcriptions"); n != 10 {
			t.Fatalf("transcriptions: %d", n)
		}
		failing = false
		if err := s.Attempt(bg, rec.ID, 2, false); err != nil {
			t.Fatal(err)
		}
		if n := w.fake.Count("/audio/transcriptions"); n != 11 {
			t.Fatalf("the retry sent %d pieces, want only the tenth", n-10)
		}
		if w.row(t, rec.ID).SummaryStatus != recordings.SummaryReady {
			t.Fatal("not ready")
		}
	})
}

func TestFailures(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t)
		w.setting(t, org.SummariesManual)
		start := func(tag string) *recordings.Recording {
			rec := w.recording(t, org.SummariesManual, piece{w.ana, 0, 30_000, tag})
			if _, err := s.Request(bg, w.ana, rec.ID); err != nil {
				t.Fatal(err)
			}
			return rec
		}
		// a wrong key fails at once, without retries, and appears nowhere
		w.answer("k", func() aitest.Answer { return aitest.Answer{Status: 401, Body: `{"error":"` + testKey + `"}`} })
		rec := start("k")
		if err := s.Attempt(bg, rec.ID, 1, false); err != nil {
			t.Fatalf("a wrong key is not retried: %v", err)
		}
		if r := w.row(t, rec.ID); r.SummaryStatus != recordings.SummaryFailed || r.SummaryError != "ai_unauthorized" {
			t.Fatalf("wrong key: %+v", r)
		}
		// the service down: retried, then failed with Try again offered
		w.answer("down", func() aitest.Answer { return aitest.Answer{Status: 503} })
		rec = start("down")
		if err := s.Attempt(bg, rec.ID, 1, false); err == nil {
			t.Fatal("503 must be retried")
		}
		if err := s.Attempt(bg, rec.ID, 4, true); err != nil {
			t.Fatal(err)
		}
		g, err := s.Get(bg, w.ana, rec.ID)
		if err != nil || g.Status != recordings.SummaryFailed || g.Error != "ai_unavailable" || !g.CanRequest {
			t.Fatalf("down: %+v %v", g, err)
		}
		// Try again: a new request on a failed summary
		if _, err := s.Request(bg, w.ana, rec.ID); err != nil {
			t.Fatalf("try again: %v", err)
		}
		// an error that echoes the content: only its class and status reach the logs
		echo := "Ana said the launch slips to Monday"
		w.answer("echo", said(0, 3, echo))
		w.fake.Chat = func(aitest.Call) aitest.Answer {
			return aitest.Answer{Status: 400, Body: `{"error":"bad input: ` + echo + `"}`}
		}
		rec = start("echo")
		if err := s.Attempt(bg, rec.ID, 1, false); err != nil {
			t.Fatal(err)
		}
		if r := w.row(t, rec.ID); r.SummaryError != "ai_rejected" {
			t.Fatalf("echo: %+v", r)
		}
		// cut short twice: asked once more, then refused
		before := w.fake.Count("/chat/completions")
		w.fake.Chat = func(aitest.Call) aitest.Answer { return aitest.Completion("## Summary\nhalf", "length") }
		rec = start("echo")
		if err := s.Attempt(bg, rec.ID, 1, false); err != nil {
			t.Fatal(err)
		}
		if r := w.row(t, rec.ID); r.SummaryStatus != recordings.SummaryFailed || r.SummaryError != "ai_rejected" ||
			w.fake.Count("/chat/completions")-before != 2 {
			t.Fatalf("truncated: %+v after %d", r, w.fake.Count("/chat/completions")-before)
		}
		// an answer without its Summary section is not posted either
		w.fake.Chat = func(aitest.Call) aitest.Answer { return aitest.Completion("Sure! Here are the minutes.", "stop") }
		rec = start("echo")
		if err := s.Attempt(bg, rec.ID, 1, false); err != nil {
			t.Fatal(err)
		}
		if r := w.row(t, rec.ID); r.SummaryError != "ai_rejected" || len(typed(w.replies(t, rec), chat.PostTypeSummary)) != 0 {
			t.Fatalf("no section: %+v", r)
		}
		logs := w.logText()
		for _, never := range []string{testKey, echo, "launch"} {
			if strings.Contains(logs, never) {
				t.Fatalf("the logs carry %q:\n%s", never, logs)
			}
		}
	})
}

// TestArchivedChannel: a recording of a channel archived since follows the same rules: summarized and posted.
func TestArchivedChannel(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t)
		w.setting(t, org.SummariesManual)
		rec := w.recording(t, org.SummariesManual, piece{w.ana, 0, 10_000, "a"})
		w.answer("a", said(0, 2, "closing words"))
		if err := w.chat.SetChannelArchived(bg, w.ana, w.ch.ID, true); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Request(bg, w.ana, rec.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Attempt(bg, rec.ID, 1, true); err != nil {
			t.Fatal(err)
		}
		if r := w.row(t, rec.ID); r.SummaryStatus != recordings.SummaryReady {
			t.Fatalf("archived: %+v", r)
		}
	})
}

func TestNoSpeech(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t)
		w.setting(t, org.SummariesManual)
		rec := w.recording(t, org.SummariesManual, piece{w.ana, 0, 60_000, "quiet"}, piece{w.bob, 0, 60_000, "quiet2"})
		w.answer("quiet2", func() aitest.Answer { return aitest.Segments() })
		if _, err := s.Request(bg, w.ana, rec.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Attempt(bg, rec.ID, 1, true); err != nil {
			t.Fatal(err)
		}
		if n := w.fake.Count("/chat/completions"); n != 0 {
			t.Fatalf("the summary model was asked %d times", n)
		}
		if r := typed(w.replies(t, rec), chat.PostTypeSummaryNoSpeech); len(r) != 1 || w.row(t, rec.ID).SummaryStatus != recordings.SummaryReady {
			t.Fatalf("no-speech note: %+v", r)
		}
	})
}

func TestLongSummaryAndBudget(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t)
		w.setting(t, org.SummariesManual)
		// a small model: (1556 − 256 − 1000) × 2 = 600 bytes per request
		s.Config.ContextTokens, s.Config.MaxOutputTokens = 1556, 256
		var ps []piece
		for i := range 12 {
			who := w.ana
			if i%2 == 1 {
				who = w.bob
			}
			tag := fmt.Sprintf("t%d", i)
			ps = append(ps, piece{who, int64(i) * 20_000, 15_000, tag})
			w.answer(tag, said(0.5, 9, strings.Repeat(fmt.Sprintf("point %d ", i), 8)))
		}
		long := "## Summary\n" + strings.Repeat("word ", 2500) + "\n\n## Decisions\n" + strings.Repeat("- ship it\n", 400)
		var mu sync.Mutex
		var sizes []int
		w.fake.Chat = func(c aitest.Call) aitest.Answer {
			mu.Lock()
			sizes = append(sizes, len(c.User))
			mu.Unlock()
			if strings.Contains(c.System, "combine") {
				return aitest.Completion(long, "stop")
			}
			return aitest.Completion("## Summary\npart", "stop")
		}
		rec := w.recording(t, org.SummariesManual, ps...)
		if _, err := s.Request(bg, w.ana, rec.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Attempt(bg, rec.ID, 1, true); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		if len(sizes) < 3 {
			t.Fatalf("not reduced in parts: %v", sizes)
		}
		for _, n := range sizes {
			if n > 600 {
				t.Fatalf("a request of %d bytes over the budget: %v", n, sizes)
			}
		}
		mu.Unlock()
		// over a message: cut at a section, attached whole
		sum := typed(w.replies(t, rec), chat.PostTypeSummary)
		if len(sum) != 1 || sum[0].Props["truncated"] != true || strings.Contains(sum[0].Message, "## Decisions") ||
			len(sum[0].Metadata.Files) != 2 || sum[0].Metadata.Files[1].Name != "summary.md" {
			t.Fatalf("long summary: %d files, props %+v", len(sum[0].Metadata.Files), sum[0].Props)
		}

		// a request refused for its length: the part size is halved once, then ai_too_long
		w.fake.Chat = func(aitest.Call) aitest.Answer {
			return aitest.Answer{Status: 400, Body: `{"error":"this model's maximum context length is exceeded"}`}
		}
		rec = w.recording(t, org.SummariesManual, ps...)
		if _, err := s.Request(bg, w.ana, rec.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Attempt(bg, rec.ID, 1, false); err != nil {
			t.Fatal(err)
		}
		if r := w.row(t, rec.ID); r.SummaryError != "ai_too_long" {
			t.Fatalf("too long: %+v", r)
		}
	})
}

func TestSwitchOff(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		a, b := w.service(t), w.service(t) // two servers; no bus: the 5-second re-read (here 100 ms) alone
		w.setting(t, org.SummariesManual)
		var ps []piece
		for i := range 10 {
			tag := fmt.Sprintf("s%d", i)
			ps = append(ps, piece{w.ana, int64(i) * 60_000, 50_000, tag})
			w.answer(tag, func() aitest.Answer {
				time.Sleep(150 * time.Millisecond)
				return aitest.Segments([3]any{1.0, 2.0, "x"})
			})
		}
		rec := w.recording(t, org.SummariesManual, ps...)
		queued := w.recording(t, org.SummariesManual, piece{w.ana, 0, 10_000, "s0"})
		for _, r := range []*recordings.Recording{rec, queued} {
			if _, err := a.Request(bg, w.ana, r.ID); err != nil {
				t.Fatal(err)
			}
		}
		done := make(chan error, 1)
		go func() { done <- a.Attempt(bg, rec.ID, 1, false) }()
		deadline := time.Now().Add(10 * time.Second)
		for w.fake.Count("/audio/transcriptions") < 3 && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if _, err := b.SetSetting(bg, org.Actor{UserID: w.owner.UserID, Role: org.RoleOwner}, org.SummariesOff); err != nil {
			t.Fatal(err)
		}
		at := w.fake.Count("/audio/transcriptions")
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("still running 10 s after the switch")
		}
		if n := w.fake.Count("/audio/transcriptions"); n > at+1 || n >= 10 {
			t.Fatalf("%d pieces sent, %d when switched off", n, at)
		}
		for _, id := range []string{rec.ID, queued.ID} {
			if r := w.row(t, id); r.SummaryStatus != recordings.SummaryCancelled || r.SummaryError != "cancelled_by_setting" {
				t.Fatalf("%s: %+v", id, r)
			}
		}
		// the queued one never starts; turned on again, nothing restarts on its own
		before := len(w.fake.Calls())
		if err := a.Attempt(bg, queued.ID, 1, true); err != nil || len(w.fake.Calls()) != before {
			t.Fatalf("queued after off: %v", err)
		}
		w.setting(t, org.SummariesManual)
		n := w.q.count(rec.ID)
		if err := a.Reconcile(bg); err != nil || w.q.count(rec.ID) != n {
			t.Fatalf("reconciled a cancelled summary: %v", err)
		}
	})
}

func TestReconcileAndDeletion(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t)
		w.setting(t, org.SummariesAutomatic)
		// queued under automatic, its enqueue lost
		lost := w.recording(t, org.SummariesAutomatic, piece{w.ana, 0, 10_000, "a"})
		db.Gorm.Model(&recordings.Recording{}).Where("id = ?", lost.ID).Update("summary_status", recordings.SummaryQueued)
		failed := w.recording(t, org.SummariesAutomatic)
		db.Gorm.Model(&recordings.Recording{}).Where("id = ?", failed.ID).Update("summary_status", recordings.SummaryFailed)
		if err := s.Reconcile(bg); err != nil {
			t.Fatal(err)
		}
		if w.q.count(lost.ID) != 1 || w.q.count(failed.ID) != 0 {
			t.Fatalf("reconcile: %v", w.q.ids)
		}

		// the recording's message deleted while its summary is written: not posted, sources removed
		release := make(chan struct{})
		var once sync.Once
		w.answer("slow", func() aitest.Answer {
			<-release
			return aitest.Segments([3]any{1.0, 2.0, "hello"})
		})
		rec := w.recording(t, org.SummariesAutomatic, piece{w.ana, 0, 10_000, "slow"}, piece{w.ana, 20_000, 10_000, "slow"})
		db.Gorm.Model(&recordings.Recording{}).Where("id = ?", rec.ID).Update("summary_status", recordings.SummaryQueued)
		done := make(chan error, 1)
		go func() { done <- s.Attempt(bg, rec.ID, 1, false) }()
		for w.fake.Count("/audio/transcriptions") == 0 {
			time.Sleep(10 * time.Millisecond)
		}
		if err := w.chat.DeletePost(bg, w.ana, rec.PostID); err != nil {
			t.Fatal(err)
		}
		once.Do(func() { close(release) })
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if n := w.fake.Count("/chat/completions"); n != 0 {
			t.Fatalf("asked the summary model %d times for a deleted recording", n)
		}
		if r := w.row(t, rec.ID); r.SummaryStatus != recordings.SummaryCancelled || r.SummaryPostID != "" {
			t.Fatalf("deleted: %+v", r)
		}
		if n := w.objects(t, rec.ID); n != 0 {
			t.Fatalf("objects left: %d", n)
		}

		// the daily cleanup: sources of a recording that can no longer be summarized, and of expired ones
		aged := w.recording(t, org.SummariesManual, piece{w.ana, 0, 10_000, "a"})
		db.Gorm.Model(&recordings.Recording{}).Where("id = ?", aged.ID).Update("summary_source_expires_at", time.Now().Add(-time.Hour).UnixMilli())
		// a crash between registration and upload leaves a row without its object: removed all the same
		stray := &recordings.Object{RecordingID: aged.ID, Generation: 1, Kind: recordings.ObjectSource, ObjectKey: "summaries/" + aged.ID + "/g1/sources/stray.ogg"}
		if err := w.rec.Register(bg, stray); err != nil {
			t.Fatal(err)
		}
		if err := s.Cleanup(bg); err != nil {
			t.Fatal(err)
		}
		if n := w.objects(t, aged.ID); n != 0 || w.row(t, aged.ID).SummarySource != recordings.SourceExpired {
			t.Fatalf("aged: %d objects, %+v", n, w.row(t, aged.ID))
		}
		if n := w.objects(t, lost.ID); n != 1 {
			t.Fatalf("an active summary lost its sources: %d", n)
		}
	})
}

// TestOffAndOnAgain: switched off and on again between two re-reads (no bus), the running attempt still stops: the
// check before every request sees its summary cancelled.
func TestOffAndOnAgain(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		a, b := w.service(t), w.service(t)
		a.Watch = time.Hour
		w.setting(t, org.SummariesManual)
		var ps []piece
		for i := range 10 {
			ps = append(ps, piece{w.ana, int64(i) * 60_000, 50_000, "slow"})
		}
		w.answer("slow", func() aitest.Answer {
			time.Sleep(100 * time.Millisecond)
			return aitest.Segments([3]any{1.0, 2.0, "x"})
		})
		rec := w.recording(t, org.SummariesManual, ps...)
		if _, err := a.Request(bg, w.ana, rec.ID); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- a.Attempt(bg, rec.ID, 1, false) }()
		for w.fake.Count("/audio/transcriptions") < 2 {
			time.Sleep(10 * time.Millisecond)
		}
		if _, err := b.SetSetting(bg, org.Actor{UserID: w.owner.UserID, Role: org.RoleOwner}, org.SummariesOff); err != nil {
			t.Fatal(err)
		}
		w.setting(t, org.SummariesManual) // on again at once (the test is skipped here: the fake would pass it)
		at := w.fake.Count("/audio/transcriptions")
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if n := w.fake.Count("/audio/transcriptions"); n > at+1 {
			t.Fatalf("%d pieces sent after the switch, at %d", n, at)
		}
		if r := w.row(t, rec.ID); r.SummaryStatus != recordings.SummaryCancelled {
			t.Fatalf("row: %+v", r)
		}
	})
}

// TestOneAtATime: a second attempt on the same summary does nothing while the first holds it; an attempt whose
// summary was taken over stops before its next request and posts nothing.
func TestOneAtATime(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		a, b := w.service(t), w.service(t)
		a.Watch = time.Hour
		w.setting(t, org.SummariesManual)
		release := make(chan struct{})
		w.answer("held", func() aitest.Answer {
			<-release
			return aitest.Segments([3]any{1.0, 2.0, "x"})
		})
		rec := w.recording(t, org.SummariesManual, piece{w.ana, 0, 10_000, "held"}, piece{w.ana, 20_000, 10_000, "next"})
		w.answer("next", said(1, 2, "y"))
		if _, err := a.Request(bg, w.ana, rec.ID); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- a.Attempt(bg, rec.ID, 1, false) }()
		for w.fake.Count("/audio/transcriptions") < 1 {
			time.Sleep(10 * time.Millisecond)
		}
		if err := b.Attempt(bg, rec.ID, 1, false); err != nil || w.fake.Count("/audio/transcriptions") != 1 {
			t.Fatalf("a second attempt ran: %v", err)
		}
		// taken over (as after a lost claim): the first attempt stops at its next check
		db.Gorm.Model(&recordings.Recording{}).Where("id = ?", rec.ID).Update("summary_generation", gorm.Expr("summary_generation + 1"))
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if n := w.fake.Count("/audio/transcriptions"); n != 1 || len(typed(w.replies(t, rec), chat.PostTypeSummary)) != 0 {
			t.Fatalf("the stale attempt went on: %d requests", n)
		}
	})
}

// TestRecovery: a checkpoint left pending by a crash does not block the retry; two participants who share a display
// name stay two speakers; a summary whose sources expired while it waited ends without a request.
func TestRecovery(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t)
		w.setting(t, org.SummariesManual)
		db.Gorm.Exec(`UPDATE users SET display_name = ? WHERE id = ?`, "Ana Silva", w.bob.UserID)
		rec := w.recording(t, org.SummariesManual, piece{w.ana, 0, 10_000, "a1"}, piece{w.bob, 3_000, 10_000, "b1"})
		w.answer("a1", said(0, 2, "first"))
		w.answer("b1", said(0, 2, "second"))
		pending := &recordings.Object{RecordingID: rec.ID, Generation: 1, Kind: recordings.ObjectCheckpoint, Speaker: w.ana.UserID,
			ObjectKey: fmt.Sprintf("summaries/%s/g1/sources/p00-000.json", rec.ID)}
		if err := w.rec.Register(bg, pending); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Request(bg, w.ana, rec.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.Attempt(bg, rec.ID, 1, true); err != nil {
			t.Fatal(err)
		}
		if r := w.row(t, rec.ID); r.SummaryStatus != recordings.SummaryReady {
			t.Fatalf("after a pending checkpoint: %+v", r)
		}
		var sent string
		for _, c := range w.fake.Calls() {
			if strings.HasSuffix(c.Path, "/chat/completions") {
				sent = c.User
			}
		}
		if want := "[00:00:00] Ana Silva: first\n[00:00:03] Ana Silva: second\n"; sent != want {
			t.Fatalf("same names merged:\n%q", sent)
		}

		aged := w.recording(t, org.SummariesManual, piece{w.ana, 0, 10_000, "a1"})
		if _, err := s.Request(bg, w.ana, aged.ID); err != nil {
			t.Fatal(err)
		}
		db.Gorm.Model(&recordings.Recording{}).Where("id = ?", aged.ID).Update("summary_source_expires_at", time.Now().Add(-time.Minute).UnixMilli())
		before := len(w.fake.Calls())
		if err := s.Attempt(bg, aged.ID, 1, false); err != nil || len(w.fake.Calls()) != before {
			t.Fatalf("expired while queued: %v", err)
		}
		if r := w.row(t, aged.ID); r.SummaryStatus != recordings.SummaryFailed || r.SummaryError != "summary_source_expired" {
			t.Fatalf("expired: %+v", r)
		}
		// and the daily cleanup ends one that waits
		waiting := w.recording(t, org.SummariesManual, piece{w.ana, 0, 10_000, "a1"})
		db.Gorm.Model(&recordings.Recording{}).Where("id = ?", waiting.ID).Updates(map[string]any{"summary_status": recordings.SummaryQueued,
			"summary_source_expires_at": time.Now().Add(-time.Minute).UnixMilli()})
		if err := s.Cleanup(bg); err != nil {
			t.Fatal(err)
		}
		if r := w.row(t, waiting.ID); r.SummaryStatus != recordings.SummaryFailed || r.SummarySource != recordings.SourceExpired {
			t.Fatalf("cleanup: %+v", r)
		}
	})
}

// TestSwitchOffBus: with the bus, the other server stops at once, long before its own re-read.
func TestSwitchOffBus(t *testing.T) {
	addr := testenv.RedisAddr(t)
	db := dbtest.Open(t, "sqlite")
	w := newWorld(t, db)
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer func() { _ = rdb.Close() }()
	a, b := w.service(t), w.service(t)
	a.Redis, b.Redis, a.Watch = rdb, rdb, time.Hour
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	go func() { _ = a.Run(ctx) }()
	w.setting(t, org.SummariesManual)
	var ps []piece
	for i := range 10 {
		ps = append(ps, piece{w.ana, int64(i) * 60_000, 50_000, "slow"})
	}
	w.answer("slow", func() aitest.Answer {
		time.Sleep(100 * time.Millisecond)
		return aitest.Segments([3]any{1.0, 2.0, "x"})
	})
	rec := w.recording(t, org.SummariesManual, ps...)
	if _, err := a.Request(bg, w.ana, rec.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // a's subscription is up
	done := make(chan error, 1)
	go func() { done <- a.Attempt(bg, rec.ID, 1, false) }()
	for w.fake.Count("/audio/transcriptions") < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := b.SetSetting(bg, org.Actor{UserID: w.owner.UserID, Role: org.RoleOwner}, org.SummariesOff); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the bus did not stop the other server")
	}
	if n := w.fake.Count("/audio/transcriptions"); n >= 10 {
		t.Fatalf("%d pieces sent", n)
	}
}
