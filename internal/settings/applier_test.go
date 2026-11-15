package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

const baseConfig = "[files]                      # attachments\nmax_size_bytes = 52428800    # per file: 50 MiB\nmax_per_post = 10\n"

// fakeServer restarts instantly; it is ready unless notReady says no to the file it would run with.
type fakeServer struct {
	mu       sync.Mutex
	config   string
	boot     int
	restarts int
	notReady func(config string) bool
	onRest   func()
}

func (f *fakeServer) Restart(context.Context) error {
	f.mu.Lock()
	f.boot++
	f.restarts++
	cb := f.onRest
	f.mu.Unlock()
	if cb != nil {
		cb()
	}
	return nil
}

func (f *fakeServer) BootID(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, _ := os.ReadFile(f.config)
	if f.notReady != nil && f.notReady(string(b)) {
		return "", errors.New("503 not ready")
	}
	return fmt.Sprintf("boot-%d", f.boot), nil
}

func (f *fakeServer) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.restarts }

type rig struct {
	t      *testing.T
	a      *Applier
	srv    *fakeServer
	config string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	root := t.TempDir()
	cfg := filepath.Join(root, "config.toml")
	if err := os.WriteFile(cfg, []byte(baseConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	reqs := filepath.Join(root, "data", "settings")
	if err := os.MkdirAll(reqs, 0o750); err != nil {
		t.Fatal(err)
	}
	srv := &fakeServer{config: cfg}
	a := &Applier{Config: cfg, Requests: reqs, State: filepath.Join(root, StateDirName), Server: srv,
		ReadyTimeout: 300 * time.Millisecond, ConfirmWindow: 300 * time.Millisecond, Poll: 5 * time.Millisecond}
	return &rig{t: t, a: a, srv: srv, config: cfg}
}

func (r *rig) file() string {
	b, err := os.ReadFile(r.config)
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

// request leaves a request for keys made from the current file; it returns its id.
func (r *rig) request(keys map[string]string) string {
	r.t.Helper()
	req := Request{ID: ulid.Make().String(), UserID: "u1", CreatedAt: r.a.now().UTC(), ConfigSHA256: sha([]byte(r.file())),
		Keys: map[string]json.RawMessage{}}
	for k, v := range keys {
		req.Keys[k] = json.RawMessage(v)
	}
	r.write(req)
	return req.ID
}

func (r *rig) write(req Request) {
	r.t.Helper()
	if req.CreatedAt.IsZero() {
		req.CreatedAt = r.a.now().UTC()
	}
	if err := writeJSON(filepath.Join(r.a.Requests, RequestFile), req, 0o640); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) run(ctx context.Context) {
	r.t.Helper()
	if err := r.a.Run(ctx); err != nil {
		r.t.Fatal(err)
	}
}

// confirmWhenAsked plays the owner: once the applier waits, it confirms that request and that process.
func (r *rig) confirmWhenAsked(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ctx.Err() == nil {
			if st, _ := ReadState(r.a.State); st != nil && st.Phase == PhaseConfirming {
				_ = writeJSON(filepath.Join(r.a.Requests, ConfirmFile), Confirm{ID: st.ID, BootID: st.BootID}, 0o640)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	return done
}

func (r *rig) result(id string) Result {
	r.t.Helper()
	rs, err := ReadResults(r.a.State)
	if err != nil {
		r.t.Fatal(err)
	}
	res := lastResult(rs, id)
	if res == nil {
		r.t.Fatalf("no result for %q in %+v", id, rs)
	}
	return *res
}

func (r *rig) noState() {
	r.t.Helper()
	if st, err := ReadState(r.a.State); st != nil || err != nil {
		r.t.Fatalf("state left: %+v %v", st, err)
	}
}

func TestAppliedAndKept(t *testing.T) {
	r := newRig(t)
	r.a.ConfirmWindow = 5 * time.Second
	id := r.request(map[string]string{"files.max_size_bytes": "104857600"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := r.confirmWhenAsked(ctx)
	r.run(ctx)
	<-done
	if res := r.result(id); res.Outcome != Applied || strings.Join(res.Keys, ",") != "files.max_size_bytes" {
		t.Fatalf("result %+v", res)
	}
	want := strings.Replace(baseConfig, "52428800", "104857600", 1)
	if got := r.file(); got != want {
		t.Fatalf("config.toml\n%s", got)
	}
	if b, _ := os.ReadFile(filepath.Join(r.a.State, BackupFile)); string(b) != baseConfig {
		t.Fatalf("backup %q", b)
	}
	if r.srv.count() != 1 {
		t.Fatalf("restarts %d", r.srv.count())
	}
	if _, err := os.Lstat(filepath.Join(r.a.Requests, RequestFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the request was left in the mailbox")
	}
	r.noState()
	if p, _ := ReadPresence(r.a.State); p == nil || p.Config != r.config || p.Requests != r.a.Requests {
		t.Fatalf("presence %+v", p)
	}
}

func TestNotReadyRollsBack(t *testing.T) {
	r := newRig(t)
	r.srv.notReady = func(c string) bool { return strings.Contains(c, "max_per_post = 3") }
	id := r.request(map[string]string{"files.max_per_post": "3"})
	r.run(context.Background())
	res := r.result(id)
	if res.Outcome != RolledBack || !strings.Contains(res.Reason, "did not become ready") {
		t.Fatalf("result %+v", res)
	}
	if r.file() != baseConfig || r.srv.count() != 2 {
		t.Fatalf("restarts %d, config\n%s", r.srv.count(), r.file())
	}
	r.noState()
}

func TestNotConfirmedRollsBack(t *testing.T) {
	r := newRig(t)
	id := r.request(map[string]string{"files.max_per_post": "3"})
	// a confirmation for another process (the old one) does not count
	r.srv.onRest = func() {
		_ = writeJSON(filepath.Join(r.a.Requests, ConfirmFile), Confirm{ID: id, BootID: "boot-0"}, 0o640)
	}
	r.run(context.Background())
	if res := r.result(id); res.Outcome != RolledBack || !strings.Contains(res.Reason, "not confirmed") {
		t.Fatalf("result %+v", res)
	}
	if r.file() != baseConfig || r.srv.count() != 2 {
		t.Fatalf("restarts %d, config\n%s", r.srv.count(), r.file())
	}

	// the same request again: refused without a restart, although the file's hash matches again
	r.write(Request{ID: id, UserID: "u1", ConfigSHA256: sha([]byte(baseConfig)),
		Keys: map[string]json.RawMessage{"files.max_per_post": json.RawMessage("3")}})
	r.srv.onRest = nil
	r.run(context.Background())
	if res := r.result(id); res.Outcome != Refused || !strings.Contains(res.Reason, "replayed") {
		t.Fatalf("replay %+v", res)
	}
	if r.srv.count() != 2 || r.file() != baseConfig {
		t.Fatalf("the replay restarted (%d) or changed the file", r.srv.count())
	}
}

func TestRefusals(t *testing.T) {
	tests := []struct {
		name   string
		keys   map[string]string
		mutate func(r *rig, req *Request)
		reason string
	}{
		{name: "a forged key", keys: map[string]string{"database.host": `"evil.example.com"`}, reason: "not editable"},
		{name: "a secret", keys: map[string]string{"smtp.password": `"x"`}, reason: "not editable"},
		{name: "out of bounds", keys: map[string]string{"session.idle_timeout": `"1s"`}, reason: "at least 5m"},
		{name: "no key", keys: map[string]string{}, reason: "no setting"},
		{name: "stale", keys: map[string]string{"files.max_per_post": "3"}, reason: "stale",
			mutate: func(r *rig, _ *Request) {
				_ = os.WriteFile(r.config, []byte(baseConfig+"# a hand edit\n"), 0o644)
			}},
		{name: "a bad hash", keys: map[string]string{"files.max_per_post": "3"}, reason: "malformed",
			mutate: func(_ *rig, req *Request) { req.ConfigSHA256 = "../../etc/passwd" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			req := Request{ID: ulid.Make().String(), ConfigSHA256: sha([]byte(baseConfig)), Keys: map[string]json.RawMessage{}}
			for k, v := range tt.keys {
				req.Keys[k] = json.RawMessage(v)
			}
			if tt.mutate != nil {
				tt.mutate(r, &req)
			}
			before := r.file()
			r.write(req)
			r.run(context.Background())
			if res := r.result(req.ID); res.Outcome != Refused || !strings.Contains(res.Reason, tt.reason) {
				t.Fatalf("result %+v", res)
			}
			if r.file() != before || r.srv.count() != 0 {
				t.Fatalf("restarts %d, config\n%s", r.srv.count(), r.file())
			}
			if _, err := os.Stat(filepath.Join(r.a.State, BackupFile)); err == nil {
				t.Fatal("a backup was written")
			}
		})
	}
}

func TestMalformedRequests(t *testing.T) {
	for name, body := range map[string]string{
		"not JSON":         "{",
		"an unknown field": `{"id":"01J00000000000000000000001","keys":{},"path":"/etc/shadow"}`,
		"a path as its id": `{"id":"../../config.toml","keys":{"log.level":"\"warn\""}}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			if err := os.WriteFile(filepath.Join(r.a.Requests, RequestFile), []byte(body), 0o640); err != nil {
				t.Fatal(err)
			}
			r.run(context.Background())
			if res := r.result(""); res.Outcome != Refused || !strings.Contains(res.Reason, "malformed") {
				t.Fatalf("result %+v", res)
			}
			if r.file() != baseConfig || r.srv.count() != 0 {
				t.Fatal("changed something")
			}
			if _, err := os.Stat(filepath.Join(r.a.State, "..", "config.toml.bak")); err == nil {
				t.Fatal("wrote a path from the request")
			}
		})
	}
}

func TestLinksInTheMailbox(t *testing.T) {
	r := newRig(t)
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the request itself is a link (to a valid request elsewhere): refused, the link removed, its target untouched
	elsewhere := filepath.Join(t.TempDir(), "req.json")
	b, _ := json.Marshal(Request{ID: ulid.Make().String(), ConfigSHA256: sha([]byte(baseConfig)),
		Keys: map[string]json.RawMessage{"log.level": json.RawMessage(`"warn"`)}})
	_ = os.WriteFile(elsewhere, b, 0o644)
	if err := os.Symlink(elsewhere, filepath.Join(r.a.Requests, RequestFile)); err != nil {
		t.Fatal(err)
	}
	r.run(context.Background())
	if res := r.result(""); res.Outcome != Refused || !strings.Contains(res.Reason, "regular file") {
		t.Fatalf("result %+v", res)
	}
	if _, err := os.Lstat(filepath.Join(r.a.Requests, RequestFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the link stayed")
	}
	if got, _ := os.ReadFile(elsewhere); string(got) != string(b) || r.file() != baseConfig {
		t.Fatal("followed the link")
	}

	// every name the applier writes or removes, planted as a link in the mailbox: the victim stays
	for _, n := range []string{ResultsFile, StateFile, PresenceFile, ConsumedFile, BackupFile, ConfirmFile, PendingFile} {
		_ = os.Symlink(victim, filepath.Join(r.a.Requests, n))
	}
	r.a.ConfirmWindow = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := r.request(map[string]string{"log.level": `"warn"`})
	done := r.confirmWhenAsked(ctx)
	r.run(ctx)
	<-done
	if res := r.result(id); res.Outcome != Applied {
		t.Fatalf("result %+v", res)
	}
	if got, _ := os.ReadFile(victim); string(got) != "keep" {
		t.Fatalf("the victim was written: %q", got)
	}

	// a mailbox that is itself a link is not read
	r2 := newRig(t)
	real := t.TempDir()
	_ = os.Remove(r2.a.Requests)
	_ = os.Symlink(real, r2.a.Requests)
	_ = writeJSON(filepath.Join(real, RequestFile), Request{ID: ulid.Make().String(), ConfigSHA256: sha([]byte(baseConfig)),
		Keys: map[string]json.RawMessage{"log.level": json.RawMessage(`"warn"`)}}, 0o640)
	r2.run(context.Background())
	if res := r2.result(""); res.Outcome != Refused {
		t.Fatalf("linked mailbox: %+v", res)
	}
	if r2.file() != baseConfig || r2.srv.count() != 0 {
		t.Fatal("applied through a linked mailbox")
	}
}

func TestOneAtATime(t *testing.T) {
	r := newRig(t)
	r.a.ConfirmWindow = 5 * time.Second
	first := r.request(map[string]string{"files.max_per_post": "3"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errs := make(chan error, 2)
	go func() { errs <- r.a.Run(ctx) }()
	// while the first waits for its confirmation, a second request made from the same file arrives and its run
	// waits for the lock
	for st, _ := ReadState(r.a.State); st == nil || st.Phase != PhaseConfirming; st, _ = ReadState(r.a.State) {
		time.Sleep(time.Millisecond)
	}
	second := Request{ID: ulid.Make().String(), ConfigSHA256: sha([]byte(baseConfig)),
		Keys: map[string]json.RawMessage{"log.level": json.RawMessage(`"warn"`)}}
	r.write(second)
	go func() { errs <- r.a.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Lstat(filepath.Join(r.a.Requests, RequestFile)); err != nil {
		t.Fatal("the second run did not wait for the lock")
	}
	st, _ := ReadState(r.a.State)
	_ = writeJSON(filepath.Join(r.a.Requests, ConfirmFile), Confirm{ID: st.ID, BootID: st.BootID}, 0o640)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if r.result(first).Outcome != Applied {
		t.Fatal("first not applied")
	}
	if res := r.result(second.ID); res.Outcome != Refused || !strings.Contains(res.Reason, "stale") {
		t.Fatalf("second %+v", res)
	}
	if !strings.Contains(r.file(), "max_per_post = 3") || strings.Contains(r.file(), "warn") || r.srv.count() != 1 {
		t.Fatalf("restarts %d, config\n%s", r.srv.count(), r.file())
	}
}

func TestHourlyLimit(t *testing.T) {
	r := newRig(t)
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	r.a.Now = func() time.Time { now = now.Add(time.Second); return now }
	r.a.ConfirmWindow = time.Nanosecond // each one rolls back at once: it still counts
	for i := range 3 {
		id := r.request(map[string]string{"files.max_per_post": fmt.Sprint(i + 2)})
		r.run(context.Background())
		if r.result(id).Outcome != RolledBack {
			t.Fatalf("apply %d: %+v", i, r.result(id))
		}
	}
	id := r.request(map[string]string{"files.max_per_post": "9"})
	r.run(context.Background())
	if res := r.result(id); res.Outcome != Refused || !strings.Contains(res.Reason, "too many") {
		t.Fatalf("fourth %+v", res)
	}
	if r.srv.count() != 6 {
		t.Fatalf("the fourth restarted: %d", r.srv.count())
	}
	now = now.Add(time.Hour)
	id = r.request(map[string]string{"files.max_per_post": "9"})
	r.run(context.Background())
	if r.result(id).Outcome != RolledBack {
		t.Fatal("still limited an hour later")
	}
}

// A stop at every step: the state and the files exactly as a stop there leaves them, then the applier's start.
func TestRecoveryAfterAStop(t *testing.T) {
	newFile := strings.Replace(baseConfig, "max_per_post = 10", "max_per_post = 3", 1)
	tests := []struct {
		phase      string
		config     string // what config.toml holds at the stop
		backup     bool
		outcome    string
		wantConfig string
		restarts   int
	}{
		{PhaseBackup, baseConfig, false, RolledBack, baseConfig, 0},
		{PhaseBackup, baseConfig, true, RolledBack, baseConfig, 0},
		{PhaseReplacing, baseConfig, true, RolledBack, baseConfig, 0},
		{PhaseReplacing, newFile, true, RolledBack, baseConfig, 1},
		{PhaseRestarting, newFile, true, RolledBack, baseConfig, 1},
		{PhaseConfirming, newFile, true, RolledBack, baseConfig, 1},
		{PhaseRollingBack, newFile, true, RolledBack, baseConfig, 1},
		{PhaseRollingBack, baseConfig, true, RolledBack, baseConfig, 1},
		{PhaseConfirmed, newFile, true, Applied, newFile, 0},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/backup=%v", tt.phase, tt.backup), func(t *testing.T) {
			r := newRig(t)
			if err := os.MkdirAll(r.a.State, 0o750); err != nil {
				t.Fatal(err)
			}
			id := ulid.Make().String()
			st := State{ID: id, Phase: tt.phase, Keys: []string{"files.max_per_post"}, OldSHA256: sha([]byte(baseConfig)),
				NewSHA256: sha([]byte(newFile)), BootID: "boot-0"}
			if err := writeJSON(filepath.Join(r.a.State, StateFile), st, 0o640); err != nil {
				t.Fatal(err)
			}
			if tt.backup {
				_ = os.WriteFile(filepath.Join(r.a.State, BackupFile), []byte(baseConfig), 0o640)
			}
			_ = os.WriteFile(r.config, []byte(tt.config), 0o644)
			r.srv.boot = 0
			r.run(context.Background()) // the request file is gone: the state alone decides
			if res := r.result(id); res.Outcome != tt.outcome {
				t.Fatalf("result %+v", res)
			}
			if r.file() != tt.wantConfig || r.srv.count() != tt.restarts {
				t.Fatalf("restarts %d, config\n%s", r.srv.count(), r.file())
			}
			r.noState()
		})
	}
}

// The unit is stopped (a package upgrade) while the applier waits: it undoes the change before it exits.
func TestStoppedWhileWaiting(t *testing.T) {
	r := newRig(t)
	r.a.ConfirmWindow = time.Minute
	id := r.request(map[string]string{"files.max_per_post": "3"})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for st, _ := ReadState(r.a.State); st == nil || st.Phase != PhaseConfirming; st, _ = ReadState(r.a.State) {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	r.run(ctx)
	if res := r.result(id); res.Outcome != RolledBack || !strings.Contains(res.Reason, "interrupted") {
		t.Fatalf("result %+v", res)
	}
	if r.file() != baseConfig || r.srv.count() != 2 {
		t.Fatalf("restarts %d", r.srv.count())
	}
}

// A FIFO (or anything but a regular file) in the mailbox neither blocks the applier nor is carried out.
func TestFIFOInTheMailbox(t *testing.T) {
	r := newRig(t)
	if _, err := takeNoFollow(r.a.Requests, RequestFile, takenFile, maxRequestBytes); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty mailbox: %v", err)
	}
	fifo := filepath.Join(r.a.Requests, RequestFile)
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip(err)
	}
	done := make(chan error, 1)
	go func() { done <- r.a.Run(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a FIFO in the mailbox blocked the applier")
	}
	if res := r.result(""); res.Outcome != Refused {
		t.Fatalf("result %+v", res)
	}
}
