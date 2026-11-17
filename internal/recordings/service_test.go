package recordings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/livekit/protocol/livekit"
	"github.com/oklog/ulid/v2"
	"github.com/redis/go-redis/v9"

	"github.com/jniltinho/meethub/internal/chat"
	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/files"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/meetings"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/testenv"
)

var bg = context.Background()

// events keeps what was published.
type events struct {
	mu   sync.Mutex
	list []chat.Event
}

func (e *events) Publish(_ context.Context, evs ...chat.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, evs...)
}

func (e *events) named(name string) []chat.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []chat.Event
	for _, ev := range e.list {
		if ev.Name == name {
			out = append(out, ev)
		}
	}
	return out
}

// rooms is LiveKit's room API in memory.
type rooms struct {
	mu      sync.Mutex
	removed []string
}

func (r *rooms) ListRooms(context.Context) ([]string, error) { return nil, nil }
func (r *rooms) ListParticipants(context.Context, string) ([]meetings.LiveParticipant, error) {
	return nil, nil
}

func (r *rooms) RemoveParticipant(_ context.Context, room, identity string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removed = append(r.removed, room+"/"+identity)
	return nil
}

func (r *rooms) wasRemoved(identity string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.removed {
		if strings.HasSuffix(x, "/"+identity) {
			return true
		}
	}
	return false
}

type world struct {
	db               *database.DB
	chat             *chat.Service
	meet             *meetings.Service
	ev               *events
	rooms            *rooms
	store            files.Storage
	owner, ana, bob  chat.Subject
	public, private  *chat.Channel
	dir              string
	ff               *FFmpeg
	captured         chan string // the end of each fake capture
	captureStartedAt chan string
}

func newWorld(t *testing.T, db *database.DB) *world {
	t.Helper()
	if _, err := org.Create(bg, db.Gorm, "Acme"); err != nil {
		t.Fatal(err)
	}
	users := &org.Users{DB: db, Hasher: identity.NewHasher(identity.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1}, 8, time.Second)}
	mk := func(name, role string) chat.Subject {
		m, err := users.Create(bg, org.SystemActor, org.NewUser{Email: name + "@acme.io", DisplayName: name, Role: role, Password: "a long password!"})
		if err != nil {
			t.Fatal(err)
		}
		return chat.Subject{UserID: m.ID, OrgRole: role}
	}
	w := &world{db: db, ev: &events{}, rooms: &rooms{}, dir: t.TempDir(), captured: make(chan string, 8),
		captureStartedAt: make(chan string, 8)}
	w.chat = &chat.Service{DB: db, Publisher: w.ev}
	w.owner, w.ana, w.bob = mk("owner", org.RoleOwner), mk("ana", org.RoleMember), mk("bob", org.RoleMember)
	w.meet = &meetings.Service{DB: db, Chat: w.chat, Publisher: w.ev, URL: "ws://livekit:7880", APIKey: "k",
		APISecret: "a-test-secret-of-at-least-32-bytes!!"}
	team, err := w.chat.CreateTeam(bg, w.owner, chat.NewTeam{Name: "eng", DisplayName: "Eng"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []chat.Subject{w.ana, w.bob} {
		if err := w.chat.AddTeamMember(bg, w.owner, team.ID, s.UserID); err != nil {
			t.Fatal(err)
		}
	}
	if w.public, err = w.chat.CreateChannel(bg, w.ana, team.ID, chat.NewChannel{Name: "projects", DisplayName: "Projects", Type: chat.ChannelOpen}); err != nil {
		t.Fatal(err)
	}
	if err := w.chat.JoinChannel(bg, w.bob, w.public.ID); err != nil {
		t.Fatal(err)
	}
	if w.private, err = w.chat.CreateChannel(bg, w.ana, team.ID, chat.NewChannel{Name: "board", DisplayName: "Board", Type: chat.ChannelPrivate}); err != nil {
		t.Fatal(err)
	}
	if w.store, err = files.NewLocal(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		w.ff = &FFmpeg{Path: p}
	}
	return w
}

// join puts a user in a channel's room (LiveKit's webhook).
func (w *world) join(t *testing.T, channelID string, sub chat.Subject) {
	t.Helper()
	room, err := w.meet.RoomName(bg, channelID)
	if err != nil {
		t.Fatal(err)
	}
	ev := &livekit.WebhookEvent{Event: "participant_joined", Room: &livekit.Room{Name: room},
		Participant: &livekit.ParticipantInfo{Identity: sub.UserID, Sid: "PA_" + sub.UserID[20:]}}
	if err := w.meet.HandleWebhook(bg, ev); err != nil {
		t.Fatal(err)
	}
}

// service is one server of recordings on the volume dir; its captures write nothing and wait to be stopped.
func (w *world) service(t *testing.T, dir string, cfg config.Recordings, rdb *redis.Client) *Service {
	t.Helper()
	s := &Service{DB: w.db, Chat: w.chat, Meetings: w.meet, Rooms: w.rooms, Storage: w.store, Redis: rdb, Config: cfg,
		Dir: dir, FFmpeg: w.ff, Lease: 3 * time.Second, Renew: 500 * time.Millisecond, APIURL: "http://livekit:7880",
		APIKey: "k", Secret: "a-test-secret-of-at-least-32-bytes!!"}
	s.Capturer = func(ctx context.Context, c *Capture, stop <-chan string) (CaptureResult, error) {
		w.captureStartedAt <- filepath.Base(c.Dir)
		var end string
		select {
		case end = <-stop:
		case <-ctx.Done():
			end = EndShutdown
			if r := endCause(""); errors.As(context.Cause(ctx), &r) {
				end = string(r)
			}
		}
		w.captured <- end
		return CaptureResult{End: end, Ended: time.Now()}, nil
	}
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	return s
}

func defaultConfig() config.Recordings {
	return config.Recordings{Enabled: true, Mode: config.RecordAudio, VideoFormat: config.FormatWebM, Start: config.RecordAdmins,
		MaxMinutes: 1, MaxBytes: 1 << 20, MaxConcurrent: 2}
}

func (w *world) run(t *testing.T, s *Service) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(bg)
	done := make(chan struct{})
	go func() {
		_ = s.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return cancel
}

func waitFor(t *testing.T, what string, within time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not within %s", what, within)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (w *world) row(t *testing.T, id string) Recording {
	t.Helper()
	var r Recording
	if err := w.db.Gorm.Where("id = ?", id).Take(&r).Error; err != nil {
		t.Fatal(err)
	}
	r.trim()
	return r
}

func TestStartPermissions(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t, w.dir, defaultConfig(), nil)
		w.run(t, s)
		w.join(t, w.public.ID, w.ana)
		w.join(t, w.public.ID, w.bob)

		// a member who is not an administrator may not record by default
		if _, err := s.Start(bg, w.bob, w.public.ID); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("bob: %v", err)
		}
		// someone who is not in the room may not start it
		if _, err := s.Start(bg, w.owner, w.public.ID); err == nil {
			t.Fatal("the owner is not in the room")
		}
		// a team or system administrator who is not a member of the private channel: 404
		if _, err := s.Start(bg, w.owner, w.private.ID); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("owner on #board: %v", err)
		}
		// while the capture directory is being changed nothing starts: the confirmation may still undo it
		s.Frozen = func() bool { return true }
		if _, err := s.Start(bg, w.ana, w.public.ID); !errors.Is(err, ErrFrozen) {
			t.Fatalf("start during a change of recordings.dir: %v", err)
		}
		s.Frozen = nil
		v, err := s.Start(bg, w.ana, w.public.ID)
		if err != nil || v.Status != StatusRecording || v.StartedBy != w.ana.UserID {
			t.Fatalf("ana: %+v %v", v, err)
		}
		w.started(t)
		// the room shows it, to every member
		room, err := w.meet.Room(bg, w.bob, w.public.ID)
		if err != nil || room.Recording == nil || room.Recording.StartedBy != w.ana.UserID || room.Recording.Stopping {
			t.Fatalf("room: %+v %v", room, err)
		}
		up := w.ev.named("meeting_updated")
		last := up[len(up)-1]
		if rv, _ := last.Data["recording"].(*meetings.RecordingView); rv == nil || rv.ID != v.ID {
			t.Fatalf("meeting_updated without the recording: %+v", last.Data)
		}
		// one per room
		if _, err := s.Start(bg, w.ana, w.public.ID); !errors.Is(err, ErrActive) {
			t.Fatalf("second start: %v", err)
		}
		// the start is audited
		var n int64
		db.Gorm.Table("audit_events").Where("action = ? AND target_id = ?", "meeting.recording_started", w.public.ID).Count(&n)
		if n != 1 {
			t.Fatalf("audit events: %d", n)
		}

		// in a conversation any member records; with start = members any member does
		dm, err := w.chat.OpenDirect(bg, w.ana, w.bob.UserID)
		if err != nil {
			t.Fatal(err)
		}
		w.join(t, dm.ID, w.bob)
		if _, err := s.Start(bg, w.bob, dm.ID); err != nil {
			t.Fatalf("conversation: %v", err)
		}
		w.started(t)
		cfg := defaultConfig()
		cfg.Start, cfg.MaxConcurrent = config.RecordMembers, 3
		s.Config = cfg
		w.join(t, w.private.ID, w.ana)
		if _, err := s.Start(bg, w.ana, w.private.ID); err != nil {
			t.Fatalf("members: %v", err)
		}
		w.started(t)
		// a fourth capture is beyond max_concurrent
		other, err := w.chat.CreateChannel(bg, w.ana, w.public.TeamID, chat.NewChannel{Name: "extra", DisplayName: "Extra", Type: chat.ChannelOpen})
		if err != nil {
			t.Fatal(err)
		}
		w.join(t, other.ID, w.ana)
		if _, err := s.Start(bg, w.ana, other.ID); !errors.Is(err, ErrBusy) {
			t.Fatalf("beyond max_concurrent: %v", err)
		}
	})
}

func TestStartBusyWithoutSpace(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		cfg := defaultConfig()
		cfg.MaxBytes = 1 << 60 // no volume holds it
		s := w.service(t, w.dir, cfg, nil)
		w.join(t, w.public.ID, w.ana)
		if _, err := s.Start(bg, w.ana, w.public.ID); !errors.Is(err, ErrBusy) {
			t.Fatalf("volume nearly full: %v", err)
		}
		var n int64
		db.Gorm.Model(&Recording{}).Count(&n)
		if n != 0 {
			t.Fatal("nothing must be recorded")
		}
	})
}

// TestAdmissionFillsTheVolume: recordings are admitted while the volume's free space covers every one's budget and
// composition reserve; the third of a volume that holds two and a half is refused, and so is a start while four
// compositions wait.
func TestAdmissionFillsTheVolume(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		free, err := freeBytes(w.dir)
		if err != nil {
			t.Fatal(err)
		}
		cfg := defaultConfig()
		cfg.MaxConcurrent = 5
		probe := &Service{Config: cfg}
		cfg.MaxBytes = int64(free)*2/5 - (probe.Reserve() - cfg.MaxBytes) // each needs 40 % of the volume
		s := w.service(t, w.dir, cfg, nil)
		var channels []string
		for i := range 3 {
			ch, err := w.chat.CreateChannel(bg, w.ana, w.public.TeamID, chat.NewChannel{Name: fmt.Sprintf("fill-%d", i),
				DisplayName: fmt.Sprintf("Fill %d", i), Type: chat.ChannelOpen})
			if err != nil {
				t.Fatal(err)
			}
			w.join(t, ch.ID, w.ana)
			channels = append(channels, ch.ID)
		}
		for i, ch := range channels {
			_, err := s.Start(bg, w.ana, ch)
			switch {
			case i < 2 && err != nil:
				t.Fatalf("recording %d: %v", i+1, err)
			case i == 2 && !errors.Is(err, ErrBusy):
				t.Fatalf("the third recording on a volume for two and a half: %v", err)
			}
			if err == nil {
				w.started(t)
			}
		}
		// a full composition backlog refuses new recordings too
		s2 := w.service(t, filepath.Join(w.dir, "other"), defaultConfig(), nil)
		for range maxWaiting {
			s2.waiting <- &Recording{}
		}
		w.join(t, w.private.ID, w.ana)
		if _, err := s2.Start(bg, w.ana, w.private.ID); !errors.Is(err, ErrBusy) {
			t.Fatalf("with a full backlog: %v", err)
		}
	})
}

// TestStopThroughAnotherServer: replica A records, the stop reaches replica B; A's recorder leaves within seconds
// (the bus) and only then the room shows no recording.
func TestStopThroughAnotherServer(t *testing.T) {
	addr := testenv.RedisAddr(t)
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		rdb := redis.NewClient(&redis.Options{Addr: addr})
		t.Cleanup(func() { _ = rdb.Close() })
		a := w.service(t, filepath.Join(w.dir, "a"), defaultConfig(), rdb)
		b := w.service(t, filepath.Join(w.dir, "b"), defaultConfig(), rdb)
		a.Renew = time.Minute // only the bus can stop it quickly
		w.run(t, a)
		w.run(t, b)
		time.Sleep(200 * time.Millisecond) // the subscriptions
		w.join(t, w.public.ID, w.ana)
		v, err := a.Start(bg, w.ana, w.public.ID)
		if err != nil {
			t.Fatal(err)
		}
		w.started(t)
		before := time.Now()
		if err := b.Stop(bg, w.bob, w.public.ID); !errors.Is(err, chat.ErrForbidden) {
			t.Fatalf("bob may not stop it: %v", err)
		}
		if err := b.Stop(bg, w.ana, w.public.ID); err != nil {
			t.Fatal(err)
		}
		room, _ := w.meet.Room(bg, w.ana, w.public.ID)
		if room.Recording == nil || !room.Recording.Stopping {
			t.Fatalf("stopping: %+v", room.Recording)
		}
		select {
		case end := <-w.captured:
			if end != EndStopped {
				t.Fatalf("ended %q", end)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("A's recorder did not stop through the bus")
		}
		if took := time.Since(before); took > 15*time.Second {
			t.Fatalf("stopped after %s", took)
		}
		waitFor(t, "the room without a recording", 5*time.Second, func() bool {
			r, _ := w.meet.Room(bg, w.ana, w.public.ID)
			return r.Recording == nil
		})
		if st := w.row(t, v.ID).Status; st != StatusProcessing && st != StatusReady && st != StatusFailed {
			t.Fatalf("status %s", st)
		}
	})
}

// TestLeaseLost: the owner cannot renew (its row was taken over, as the sweeper does when the database was out of
// reach): it stops recording without being told.
func TestLeaseLost(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t, w.dir, defaultConfig(), nil)
		w.run(t, s)
		w.join(t, w.public.ID, w.ana)
		v, err := s.Start(bg, w.ana, w.public.ID)
		if err != nil {
			t.Fatal(err)
		}
		w.started(t)
		if err := db.Gorm.Model(&Recording{}).Where("id = ?", v.ID).Update("generation", 7).Error; err != nil {
			t.Fatal(err)
		}
		select {
		case end := <-w.captured:
			if end != EndLease {
				t.Fatalf("ended %q", end)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("the recorder kept recording on a lease it could not renew")
		}
	})
}

// TestDatabasePartition: the owner loses the database (its own connection closes) while LiveKit is still there: it
// stops recording without being told, and another server's sweep then ends the recording for the room.
func TestDatabasePartition(t *testing.T) {
	dbtest.EachEmpty(t, func(t *testing.T, set database.Settings) {
		db := dbtest.OpenEmpty(t, set)
		m, err := database.NewMigrator(db)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Up(bg); err != nil {
			t.Fatal(err)
		}
		w := newWorld(t, db)
		owner := w.service(t, filepath.Join(w.dir, "owner"), defaultConfig(), nil)
		ownDB, err := database.Open(bg, set) // the owner's own connection to the same database
		if err != nil {
			t.Fatal(err)
		}
		owner.DB = ownDB
		w.run(t, owner)
		w.join(t, w.public.ID, w.ana)
		v, err := owner.Start(bg, w.ana, w.public.ID)
		if err != nil {
			t.Fatal(err)
		}
		w.started(t)
		_ = ownDB.Close() // the partition: every renewal fails from now on
		select {
		case end := <-w.captured:
			if end != EndLease {
				t.Fatalf("ended %q", end)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the owner kept recording without the database")
		}
		other := w.service(t, filepath.Join(w.dir, "other"), defaultConfig(), nil)
		later := time.Now().Add(time.Minute)
		other.Now = func() time.Time { return later }
		if err := other.Sweep(bg); err != nil {
			t.Fatal(err)
		}
		if st := w.row(t, v.ID).Status; st != StatusInterrupted {
			t.Fatalf("status %s", st)
		}
		if room, _ := w.meet.Room(bg, w.ana, w.public.ID); room.Recording != nil {
			t.Fatal("the room still shows the recording")
		}
	})
}

// TestSweeper: a server that vanished: within the lease and one sweep, the room no longer shows the recording,
// its recorder is removed; after 24 hours without its volume the recording fails and the channel is told.
func TestSweeper(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		gone := w.service(t, filepath.Join(w.dir, "gone"), defaultConfig(), nil)
		gone.Capturer = func(ctx context.Context, _ *Capture, _ <-chan string) (CaptureResult, error) {
			<-ctx.Done() // the server "vanishes": it never renews (its loops do not run)
			return CaptureResult{End: EndShutdown}, nil
		}
		w.join(t, w.public.ID, w.ana)
		v, err := gone.Start(bg, w.ana, w.public.ID)
		if err != nil {
			t.Fatal(err)
		}
		other := w.service(t, filepath.Join(w.dir, "other"), defaultConfig(), nil)
		now := time.Now()
		other.Now = func() time.Time { return now.Add(time.Minute) }
		if err := other.Sweep(bg); err != nil {
			t.Fatal(err)
		}
		r := w.row(t, v.ID)
		if r.Status != StatusInterrupted || r.Generation != 2 {
			t.Fatalf("after the sweep: %+v", r)
		}
		if !w.rooms.wasRemoved(RecorderIdentity(v.ID)) {
			t.Fatal("the recorder was not removed from the room")
		}
		room, _ := w.meet.Room(bg, w.ana, w.public.ID)
		if room.Recording != nil {
			t.Fatal("the room still shows the recording")
		}
		other.Now = func() time.Time { return now.Add(25 * time.Hour) }
		if err := other.Sweep(bg); err != nil {
			t.Fatal(err)
		}
		r = w.row(t, v.ID)
		if r.Status != StatusFailed || r.Error != FailCaptureLost || r.PostID == "" {
			t.Fatalf("after 24 hours: %+v", r)
		}
		posts := w.ev.named("posted")
		if p, _ := posts[len(posts)-1].Data["post"].(chat.PostView); p.Type != chat.PostTypeRecordingFailed || p.UserID != w.ana.UserID {
			t.Fatalf("failure post: %+v", posts[len(posts)-1].Data)
		}
	})
}

func newULID() string { return ulid.Make().String() }

// TestStaleAttemptPostsNothing: an attempt whose generation another server bumped neither fails the recording nor
// posts; a client cannot take a recording's post key either.
func TestStaleAttemptPostsNothing(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t, w.dir, defaultConfig(), nil)
		r := w.recordingRow(t, s, StatusProcessing, true)
		db.Gorm.Model(&Recording{}).Where("id = ?", r.ID).Update("generation", 2)
		s.fail(bg, &r, "ffmpeg_failed") // r still has generation 1
		var n int64
		db.Gorm.Model(&chat.Post{}).Where("pending_post_id = ?", "recording:"+r.ID).Count(&n)
		if n != 0 || w.row(t, r.ID).Status != StatusProcessing {
			t.Fatalf("a stale attempt failed the recording: %d posts, %+v", n, w.row(t, r.ID))
		}
		_, err := w.chat.CreatePost(bg, w.ana, chat.NewPost{ChannelID: w.public.ID, Message: "x", PendingPostID: "recording:" + r.ID})
		if !errors.Is(err, chat.ErrInvalidInput) {
			t.Fatalf("a client took the recording's key: %v", err)
		}
	})
}

// started waits for a capture to start.
func (w *world) started(t *testing.T) {
	t.Helper()
	select {
	case <-w.captureStartedAt:
	case <-time.After(10 * time.Second):
		t.Fatal("the capture did not start")
	}
}

// recordingRow inserts a recording in a state, as if a server had captured it.
func (w *world) recordingRow(t *testing.T, s *Service, status string, ended bool) Recording {
	t.Helper()
	room, _ := w.meet.RoomName(bg, w.public.ID)
	start := time.Now().Add(-time.Minute)
	r := Recording{ID: newULID(), ChannelID: w.public.ID, Room: room, StartedBy: w.ana.UserID, Mode: "audio",
		Status: status, StartedAt: start.UnixMilli(), Volume: s.volume, Generation: 1, FileID: newULID(),
		CreatedAt: start.UnixMilli(), UpdatedAt: start.UnixMilli()}
	if ended {
		r.EndedAt, r.DurationMS = start.UnixMilli()+3000, 3000
	}
	if err := w.db.Gorm.Create(&r).Error; err != nil {
		t.Fatal(err)
	}
	return r
}

// TestPublishOnce: the recording is posted once, attributed to its starter, without clearing her unread count or
// notifying anyone; a member reads its file, someone outside the channel does not; a repeat posts nothing.
func TestPublishOnce(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t, w.dir, defaultConfig(), nil)
		r := w.recordingRow(t, s, StatusProcessing, true)
		// Bob posts: Ana has an unread message
		if _, err := w.chat.CreatePost(bg, w.bob, chat.NewPost{ChannelID: w.public.ID, Message: "hello @ana", PendingPostID: "p1"}); err != nil {
			t.Fatal(err)
		}
		unread := func() int64 {
			var m struct{ MsgCount, MentionCount int64 }
			db.Gorm.Table("channel_members").Select("msg_count, mention_count").
				Where("channel_id = ? AND user_id = ?", w.public.ID, w.ana.UserID).Scan(&m)
			var total int64
			db.Gorm.Table("channels").Select("total_msg_count").Where("id = ?", w.public.ID).Scan(&total)
			return total - m.MsgCount
		}
		before := unread()
		out := filepath.Join(t.TempDir(), "recording.m4a")
		if err := os.WriteFile(out, []byte("not really audio"), 0o600); err != nil {
			t.Fatal(err)
		}
		o := &Output{Path: out, Name: "recording.m4a", MimeType: "audio/mp4"}
		if err := s.publish(bg, &r, o, false); err != nil {
			t.Fatal(err)
		}
		if err := s.publish(bg, &r, o, false); err != nil { // a repeat after the commit (the row is ready): nothing
			t.Fatal(err)
		}
		var posts []chat.Post
		db.Gorm.Where("channel_id = ? AND type = ?", w.public.ID, chat.PostTypeRecording).Find(&posts)
		if len(posts) != 1 || strings.TrimSpace(posts[0].UserID) != w.ana.UserID {
			t.Fatalf("posts: %+v", posts)
		}
		if after := unread(); after != before+1 {
			t.Fatalf("Ana's unread went from %d to %d: posting must not mark the channel read", before, after)
		}
		row := w.row(t, r.ID)
		if row.Status != StatusReady || row.PostID != strings.TrimSpace(posts[0].ID) {
			t.Fatalf("row %+v", row)
		}
		if _, err := w.chat.ReadableFile(bg, w.bob, r.FileID); err != nil {
			t.Fatalf("a member reads the recording: %v", err)
		}
		if _, err := w.chat.ReadableFile(bg, w.owner, r.FileID); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("outside the channel: %v", err)
		}
		posted := w.ev.named("posted")
		p, _ := posted[len(posted)-1].Data["post"].(chat.PostView)
		if p.Props["recording_id"] != r.ID || p.Metadata == nil || len(p.Metadata.Files) != 1 {
			t.Fatalf("post %+v", p)
		}
		if m, _ := posted[len(posted)-1].Data["mentions"].([]string); len(m) != 0 {
			t.Fatalf("mentions %v", m)
		}
	})
}

// TestPublishArchivedAndStarterGone: the result still belongs in an archived channel, to a starter who left.
func TestPublishArchivedAndStarterGone(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t, w.dir, defaultConfig(), nil)
		r := w.recordingRow(t, s, StatusProcessing, true)
		r.StartedBy = w.bob.UserID
		db.Gorm.Model(&Recording{}).Where("id = ?", r.ID).Update("started_by", w.bob.UserID)
		if err := w.chat.RemoveChannelMember(bg, w.ana, w.public.ID, w.bob.UserID); err != nil {
			t.Fatal(err)
		}
		if err := w.chat.SetChannelArchived(bg, w.ana, w.public.ID, true); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(t.TempDir(), "recording.m4a")
		_ = os.WriteFile(out, []byte("x"), 0o600)
		if err := s.publish(bg, &r, &Output{Path: out, Name: "recording.m4a", MimeType: "audio/mp4"}, false); err != nil {
			t.Fatal(err)
		}
		if st := w.row(t, r.ID).Status; st != StatusReady {
			t.Fatalf("status %s", st)
		}
	})
}

// TestSupersededAttempt: an attempt whose generation was bumped (another server adopted it) publishes nothing and
// leaves no object.
func TestSupersededAttempt(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		s := w.service(t, w.dir, defaultConfig(), nil)
		r := w.recordingRow(t, s, StatusProcessing, true)
		db.Gorm.Model(&Recording{}).Where("id = ?", r.ID).Update("generation", 2)
		out := filepath.Join(t.TempDir(), "recording.m4a")
		_ = os.WriteFile(out, []byte("x"), 0o600)
		if err := s.publish(bg, &r, &Output{Path: out, Name: "recording.m4a", MimeType: "audio/mp4"}, false); err != nil {
			t.Fatal(err)
		}
		var n int64
		db.Gorm.Model(&chat.Post{}).Where("type = ?", chat.PostTypeRecording).Count(&n)
		if n != 0 {
			t.Fatal("a superseded attempt posted")
		}
		if _, err := w.store.Open(bg, storageKey(&r, "recording.m4a")); !errors.Is(err, files.ErrNoObject) {
			t.Fatalf("its object is left: %v", err)
		}
	})
}

// TestAdoptCutCapture: a server killed while capturing comes back with the same volume: the capture up to the cut
// is posted, partial.
func TestAdoptCutCapture(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		if w.ff == nil {
			t.Skip("no ffmpeg")
		}
		s := w.service(t, w.dir, defaultConfig(), nil)
		r := w.recordingRow(t, s, StatusRecording, false) // its lease long expired
		dir := filepath.Join(w.dir, r.ID)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		tone(t, w.ff, dir, "t001-001.ogg", 440, 1500)
		writeTrack(t, dir, "t001.jsonl", Header{Kind: KindAudio, Participant: w.ana.UserID, Name: "ana"},
			Segment{File: "t001-001.ogg", PosMS: 500, DurMS: 1500})
		w.run(t, s) // adoption at startup
		waitFor(t, "the adopted recording posted", 60*time.Second, func() bool {
			return w.row(t, r.ID).Status == StatusReady
		})
		row := w.row(t, r.ID)
		if row.Generation != 2 || row.DurationMS != 2000 || !strings.Contains(row.PartialReasons, PartialCut) {
			t.Fatalf("row %+v", row)
		}
		var n int64
		db.Gorm.Model(&chat.Post{}).Where("type = ?", chat.PostTypeRecording).Count(&n)
		if n != 1 {
			t.Fatalf("%d posts", n)
		}
	})
}

// TestAdoptInterruptedComposition: a server stopped while composing (the sweeper made the row interrupted an hour
// ago) comes back with the same volume: it composes again and posts the recording once, complete.
func TestAdoptInterruptedComposition(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		if w.ff == nil {
			t.Skip("no ffmpeg")
		}
		s := w.service(t, w.dir, defaultConfig(), nil)
		r := w.recordingRow(t, s, StatusInterrupted, true)
		db.Gorm.Model(&Recording{}).Where("id = ?", r.ID).Updates(map[string]any{"generation": 3,
			"updated_at": time.Now().Add(-time.Hour).UnixMilli()})
		dir := filepath.Join(w.dir, r.ID)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		tone(t, w.ff, dir, "t001-001.ogg", 440, 1000)
		writeTrack(t, dir, "t001.jsonl", Header{Kind: KindAudio, Participant: w.ana.UserID, Name: "ana"},
			Segment{File: "t001-001.ogg", PosMS: 1000, DurMS: 1000})
		_ = os.WriteFile(filepath.Join(dir, "p00.ogg"), []byte("a half-written intermediate"), 0o600)
		w.run(t, s)
		waitFor(t, "the recording posted", 60*time.Second, func() bool { return w.row(t, r.ID).Status == StatusReady })
		row := w.row(t, r.ID)
		if row.Generation != 4 || row.PartialReasons != "" || row.DurationMS != 3000 {
			t.Fatalf("row %+v", row)
		}
		var n int64
		db.Gorm.Model(&chat.Post{}).Where("type = ?", chat.PostTypeRecording).Count(&n)
		if n != 1 {
			t.Fatalf("%d posts", n)
		}
		// the capture directory goes once the recording is ready
		s.cleanDirs(bg)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("capture directory kept: %v", err)
		}
	})
}

// TestRetention: a recording older than retention_days loses its file and its message says it expired.
func TestRetention(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		w := newWorld(t, db)
		cfg := defaultConfig()
		cfg.RetentionDays = 30
		s := w.service(t, w.dir, cfg, nil)
		r := w.recordingRow(t, s, StatusProcessing, true)
		out := filepath.Join(t.TempDir(), "recording.m4a")
		_ = os.WriteFile(out, []byte("x"), 0o600)
		if err := s.publish(bg, &r, &Output{Path: out, Name: "recording.m4a", MimeType: "audio/mp4"}, false); err != nil {
			t.Fatal(err)
		}
		if err := w.chat.SetChannelArchived(bg, w.ana, w.public.ID, true); err != nil { // archived channels too
			t.Fatal(err)
		}
		s.Now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }
		// what a retention would delete is counted, and nothing goes while a settings change awaits its confirmation
		for days, want := range map[int64]int64{0: 0, 7: 1, 30: 1, 60: 0} {
			if n, err := s.Expiring(bg, days); err != nil || n != want {
				t.Fatalf("expiring in %d days: %d %v", days, n, err)
			}
		}
		pending := true
		s.Hold = func() bool { return pending }
		if err := s.Expire(bg); err != nil || w.row(t, r.ID).ExpiredAt != 0 {
			t.Fatalf("expired during a pending change: %v", err)
		}
		pending = false
		if err := s.Expire(bg); err != nil {
			t.Fatal(err)
		}
		row := w.row(t, r.ID)
		if row.ExpiredAt == 0 {
			t.Fatal("not expired")
		}
		if _, err := w.chat.ReadableFile(bg, w.ana, r.FileID); !errors.Is(err, chat.ErrNotFound) {
			t.Fatalf("its file is still there: %v", err)
		}
		edited := w.ev.named("post_edited")
		if len(edited) == 0 {
			t.Fatal("the message was not updated")
		}
		if p, _ := edited[len(edited)-1].Data["post"].(chat.PostView); p.Props["expired"] != true {
			t.Fatalf("message props %+v", p.Props)
		}
	})
}
