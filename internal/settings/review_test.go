package settings

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
)

// An old request is refused; refusals are not recorded, so they cannot push carried-out ids out of the list.
func TestExpiredAndUnrecordedRefusals(t *testing.T) {
	r := newRig(t)
	// the time is the id's own: a fresh created_at does not make an old id new
	r.write(Request{ID: newID(time.Now().Add(-2 * time.Hour)), CreatedAt: time.Now(), ConfigSHA256: sha([]byte(baseConfig)),
		Keys: map[string]json.RawMessage{"log.level": json.RawMessage(`"warn"`)}})
	r.run(context.Background())
	if rs, _ := ReadResults(r.a.State); len(rs) != 1 || !strings.Contains(rs[0].Reason, "expired") {
		t.Fatalf("results %+v", rs)
	}
	for range 3 {
		r.request(map[string]string{"database.host": `"x"`})
		r.run(context.Background())
	}
	if _, err := os.Stat(filepath.Join(r.a.State, ConsumedFile)); !errors.Is(err, os.ErrNotExist) || r.srv.count() != 0 {
		t.Fatalf("refusals were recorded as consumed (%v) or restarted", err)
	}
}

// An id refused once and then carried out: a stop in the middle is still undone (the old refusal is not its result).
func TestRefusedIDThenInterrupted(t *testing.T) {
	r := newRig(t)
	id := ulid.Make().String()
	r.write(Request{ID: id, ConfigSHA256: sha([]byte(baseConfig)), Keys: map[string]json.RawMessage{"database.host": json.RawMessage(`"x"`)}})
	r.run(context.Background())
	newFile := strings.Replace(baseConfig, "max_per_post = 10", "max_per_post = 3", 1)
	_ = writeJSON(filepath.Join(r.a.State, StateFile), State{ID: id, Phase: PhaseConfirming, StartedAt: time.Now().UTC(),
		OldSHA256: sha([]byte(baseConfig)), NewSHA256: sha([]byte(newFile))}, 0o640)
	_ = os.WriteFile(filepath.Join(r.a.State, BackupFile), []byte(baseConfig), 0o640)
	_ = os.WriteFile(r.config, []byte(newFile), 0o644)
	r.run(context.Background())
	if r.file() != baseConfig || r.result(id).Outcome != RolledBack {
		t.Fatalf("not undone: %+v\n%s", r.result(id), r.file())
	}
}

// A withdrawal whose result cannot be written yet is closed, with its own reason, at the next reconciliation.
func TestWithdrawalRetried(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		ctx := context.Background()
		r := newRig(t)
		s := newService(t, db, r, nil)
		now := time.Now()
		s.Now = func() time.Time { return now }
		if _, err := s.Request(ctx, owner, "s", raw(map[string]string{"log.level": `"warn"`}), nil); err != nil {
			t.Fatal(err)
		}
		blocker := filepath.Join(r.a.Requests, ServerResult)
		if err := os.MkdirAll(filepath.Join(blocker, "x"), 0o750); err != nil { // a rename over it fails
			t.Fatal(err)
		}
		now = now.Add(2 * time.Minute)
		s.Reconcile(ctx)
		if p := s.readPending(); p == nil || p.Withdrawn == "" {
			t.Fatalf("pending %+v", p)
		}
		_ = os.RemoveAll(blocker)
		s.Reconcile(ctx)
		v, _ := s.Get(ctx, owner, "s")
		if v.Pending != nil || v.LastResult == nil || !strings.Contains(v.LastResult.Reason, "not running") {
			t.Fatalf("after the retry: %+v %+v", v.Pending, v.LastResult)
		}
	})
}

// Recover (the package scripts) undoes an interrupted change and leaves a waiting request alone.
func TestRecoverOnly(t *testing.T) {
	r := newRig(t)
	newFile := strings.Replace(baseConfig, "max_per_post = 10", "max_per_post = 3", 1)
	_ = os.MkdirAll(r.a.State, 0o750)
	_ = writeJSON(filepath.Join(r.a.State, StateFile), State{ID: ulid.Make().String(), Phase: PhaseConfirming,
		OldSHA256: sha([]byte(baseConfig)), NewSHA256: sha([]byte(newFile))}, 0o640)
	_ = os.WriteFile(filepath.Join(r.a.State, BackupFile), []byte(baseConfig), 0o640)
	_ = os.WriteFile(r.config, []byte(newFile), 0o644)
	r.request(map[string]string{"log.level": `"warn"`})
	if err := r.a.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.file() != baseConfig {
		t.Fatalf("not undone:\n%s", r.file())
	}
	r.noState()
	if _, err := os.Lstat(filepath.Join(r.a.Requests, RequestFile)); err != nil {
		t.Fatal("the waiting request was taken")
	}
}

// Two changes at once: one request is published, the other is refused; a withdrawn request's result is shown.
func TestServiceOneAtATimeAndWithdrawnResult(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		ctx := context.Background()
		r := newRig(t)
		s := newService(t, db, r, nil)
		now := time.Now()
		s.Now = func() time.Time { return now }
		errs := make(chan error, 2)
		for _, v := range []string{"3", "4"} {
			go func() {
				_, err := s.Request(ctx, owner, "s", raw(map[string]string{"files.max_per_post": v}), nil)
				errs <- err
			}()
		}
		var ok, pending int
		for range 2 {
			switch err := <-errs; {
			case err == nil:
				ok++
			case errors.Is(err, ErrPending):
				pending++
			default:
				t.Fatal(err)
			}
		}
		if ok != 1 || pending != 1 {
			t.Fatalf("ok %d pending %d", ok, pending)
		}
		s.mu.Lock()
		now = now.Add(2 * time.Minute)
		s.mu.Unlock()
		v, _ := s.Get(ctx, owner, "s")
		if v.Pending != nil || v.LastResult == nil || v.LastResult.Outcome != Refused || !strings.Contains(v.LastResult.Reason, "not running") {
			t.Fatalf("after the withdrawal: %+v %+v", v.Pending, v.LastResult)
		}
	})
}
