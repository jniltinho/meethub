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

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/org"
)

var (
	owner = org.Actor{UserID: "01J00000000000000000000OWN", Role: org.RoleOwner, ClientIP: "10.0.0.1"}
	admin = org.Actor{UserID: "01J00000000000000000000ADM", Role: org.RoleAdmin}
)

const secretValue = "hunter2-smtp-password"

// newService is a server whose applier is the rig's (same files), with the environment env.
func newService(t *testing.T, db *database.DB, r *rig, env map[string]string) *Service {
	t.Helper()
	cfg, err := config.LoadWith(r.config, func(k string) (string, bool) { v, ok := env[k]; return v, ok }, os.ReadFile)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{DB: db.Gorm, Config: r.config, Requests: r.a.Requests, State: r.a.State, Running: cfg, BootID: "boot-0",
		Host: "host-a", Lookup: func(k string) (string, bool) { v, ok := env[k]; return v, ok }}
	// the applier announced itself (it runs at boot)
	if err := os.MkdirAll(r.a.State, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(r.a.State, PresenceFile), Presence{Config: r.config, Requests: r.a.Requests}, 0o640); err != nil {
		t.Fatal(err)
	}
	return s
}

func raw(m map[string]string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, v := range m {
		out[k] = json.RawMessage(v)
	}
	return out
}

func actions(t *testing.T, db *database.DB) []string {
	t.Helper()
	page, err := audit.List(context.Background(), db.Gorm, audit.Filter{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for i := len(page.Events) - 1; i >= 0; i-- {
		out = append(out, page.Events[i].Action)
	}
	return out
}

func noRequest(t *testing.T, r *rig) {
	t.Helper()
	for _, n := range []string{RequestFile, PendingFile} {
		if _, err := os.Lstat(filepath.Join(r.a.Requests, n)); err == nil {
			t.Fatalf("%s was written", n)
		}
	}
}

func TestServiceRefusals(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		ctx := context.Background()
		r := newRig(t)
		s := newService(t, db, r, map[string]string{"MEETHUB_FILES_MAX_SIZE_BYTES": "10485760"})
		if _, err := s.Get(ctx, admin, "s"); !errors.Is(err, org.ErrForbidden) {
			t.Fatalf("admin get: %v", err)
		}
		if _, err := s.Request(ctx, admin, "s", raw(map[string]string{"log.level": `"warn"`}), nil); !errors.Is(err, org.ErrForbidden) {
			t.Fatalf("admin put: %v", err)
		}
		if _, err := s.Request(ctx, owner, "s", raw(map[string]string{"smtp.host": `"mail.example.com"`}), nil); !errors.Is(err, config.ErrNotEditable) ||
			!strings.Contains(err.Error(), "smtp.host") {
			t.Fatalf("smtp.host: %v", err)
		}
		var ve *config.AdminValueError
		if _, err := s.Request(ctx, owner, "s", raw(map[string]string{"session.idle_timeout": `"1s"`}), nil); !errors.As(err, &ve) ||
			!strings.Contains(err.Error(), "at least 5m") {
			t.Fatalf("bounds: %v", err)
		}
		var le *LockedError
		if _, err := s.Request(ctx, owner, "s", raw(map[string]string{"files.max_size_bytes": "104857600"}), nil); !errors.As(err, &le) ||
			le.Env != "MEETHUB_FILES_MAX_SIZE_BYTES" {
			t.Fatalf("locked: %v", err)
		}
		if _, err := s.Request(ctx, owner, "s", nil, []string{"files.max_size_bytes"}); !errors.As(err, &le) {
			t.Fatalf("restoring a locked key: %v", err)
		}
		// valid key by key, refused as a whole configuration (as at startup)
		var ie *InvalidConfigError
		if _, err := s.Request(ctx, owner, "s", raw(map[string]string{"session.absolute_lifetime": `"2h"`}), nil); !errors.As(err, &ie) ||
			!strings.Contains(err.Error(), "session.absolute_lifetime") {
			t.Fatalf("whole configuration: %v", err)
		}
		if _, err := s.Request(ctx, owner, "s", nil, nil); !errors.Is(err, ErrNothingToChange) {
			t.Fatalf("nothing: %v", err)
		}
		noRequest(t, r)

		// without an applier on this host: read-only
		_ = os.Remove(filepath.Join(r.a.State, PresenceFile))
		v, err := s.Get(ctx, owner, "s")
		if err != nil || v.Applier.Available || v.Applier.Reason != "no_applier" {
			t.Fatalf("view %+v %v", v.Applier, err)
		}
		if _, err := s.Request(ctx, owner, "s", raw(map[string]string{"log.level": `"warn"`}), nil); !errors.Is(err, ErrReadOnly) {
			t.Fatalf("read-only: %v", err)
		}
		bare := &Service{DB: db.Gorm, Running: config.Default()}
		if v, err := bare.Get(ctx, owner, "s"); err != nil || v.Applier.Reason != "no_file" {
			t.Fatalf("no file: %+v %v", v, err)
		}
		noRequest(t, r)
		if got := actions(t, db); len(got) != 0 {
			t.Fatalf("audited %v", got)
		}
	})
}

func TestServiceView(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		r := newRig(t)
		s := newService(t, db, r, map[string]string{
			"MEETHUB_SMTP_PASSWORD": secretValue, "MEETHUB_SMTP_HOST": "mail.example.com", "MEETHUB_SMTP_USERNAME": "u",
			"MEETHUB_SMTP_FROM": "m@example.com", "MEETHUB_LOG_LEVEL": "warn",
		})
		v, err := s.Get(context.Background(), owner, "s")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), secretValue) {
			t.Fatal("the view carries the SMTP password")
		}
		byKey := map[string]KeyView{}
		for _, k := range v.Keys {
			byKey[k.Key] = k
		}
		if k := byKey["files.max_size_bytes"]; k.Source != "file" || k.Value != int64(52428800) || k.Default != int64(52428800) || k.Locked {
			t.Fatalf("max_size_bytes %+v", k)
		}
		if k := byKey["log.level"]; k.Source != "environment" || !k.Locked || k.Env != "MEETHUB_LOG_LEVEL" || k.Value != "warn" {
			t.Fatalf("log.level %+v", k)
		}
		if k := byKey["session.idle_timeout"]; k.Source != "default" || k.Value != "168h" || k.Min != int64(5*time.Minute) {
			t.Fatalf("idle_timeout %+v", k)
		}
		ro := map[string]ReadOnlyView{}
		for _, f := range v.ReadOnly {
			ro[f.Key] = f
		}
		if f := ro["smtp.password"]; !f.Secret || !f.Set || f.Value != nil || f.Reason != "secret" {
			t.Fatalf("smtp.password %+v", f)
		}
		if f := ro["setup.token"]; !f.Secret || f.Set {
			t.Fatalf("setup.token %+v", f)
		}
		for key, reason := range map[string]string{"smtp.host": "destination", "http.trusted_proxies": "trust",
			"http.public_url": "lockout", "database.driver": "data", "oidc.auto_create": "irreversible", "ai.timeout": "other"} {
			if ro[key].Reason != reason {
				t.Errorf("%s: reason %q, want %q", key, ro[key].Reason, reason)
			}
		}
		if _, ok := ro["files.max_per_post"]; ok {
			t.Fatal("an editable key is in the read-only list")
		}
		if !v.Applier.Available || v.Pending != nil || v.Host != "host-a" {
			t.Fatalf("view %+v", v)
		}
	})
}

// The whole path with the applier: request, the restart, "Keep these settings", the result and its audit.
func TestRequestApplyConfirm(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		ctx := context.Background()
		r := newRig(t)
		r.a.ConfirmWindow = 5 * time.Second
		s := newService(t, db, r, nil)
		v, err := s.Request(ctx, owner, "sess-1", raw(map[string]string{"files.max_size_bytes": "104857600"}), []string{"files.max_per_post"})
		if err != nil {
			t.Fatal(err)
		}
		if v.Pending == nil || v.Pending.Phase != "waiting" || !v.Pending.Mine || v.Pending.Keys["files.max_size_bytes"].Old != float64(52428800) {
			t.Fatalf("pending %+v", v.Pending)
		}
		if _, err := s.Request(ctx, owner, "sess-1", raw(map[string]string{"log.level": `"warn"`}), nil); !errors.Is(err, ErrPending) {
			t.Fatalf("second request: %v", err)
		}
		id := v.Pending.ID
		if err := s.Confirm(ctx, owner, "sess-1", id); !errors.Is(err, ErrNotConfirmable) {
			t.Fatalf("confirmed before the restart: %v", err)
		}

		done := make(chan error, 1)
		go func() { done <- r.a.Run(ctx) }()
		var st *State
		for st, _ = ReadState(r.a.State); st == nil || st.Phase != PhaseConfirming; st, _ = ReadState(r.a.State) {
			time.Sleep(time.Millisecond)
		}
		// the process before the restart, another session, another owner: refused
		if err := s.Confirm(ctx, owner, "sess-1", id); !errors.Is(err, ErrNotConfirmable) {
			t.Fatalf("old process: %v", err)
		}
		s.BootID = st.BootID // now this is the process the applier restarted
		other := org.Actor{UserID: "01J00000000000000000000OW2", Role: org.RoleOwner}
		for _, bad := range []error{
			s.Confirm(ctx, owner, "sess-2", id),
			s.Confirm(ctx, other, "sess-1", id),
			s.Confirm(ctx, owner, "sess-1", "01J00000000000000000000XXX"),
			s.Confirm(ctx, admin, "sess-1", id),
		} {
			if bad == nil {
				t.Fatal("a confirmation by someone else was accepted")
			}
		}
		v, _ = s.Get(ctx, owner, "sess-1")
		if v.Pending == nil || v.Pending.Phase != "confirming" || !v.Pending.CanConfirm || v.Pending.SecondsLeft <= 0 {
			t.Fatalf("pending %+v", v.Pending)
		}
		if v, _ := s.Get(ctx, owner, "sess-2"); v.Pending.CanConfirm {
			t.Fatal("another session can confirm")
		}
		if err := s.Confirm(ctx, owner, "sess-1", id); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		v, _ = s.Get(ctx, owner, "sess-1")
		if v.Pending != nil || v.LastResult == nil || v.LastResult.Outcome != Applied || v.LastResult.ID != id {
			t.Fatalf("after: %+v %+v", v.Pending, v.LastResult)
		}
		want := strings.Replace(strings.Replace(baseConfig, "52428800", "104857600", 1), "max_per_post = 10", "max_per_post = 10", 1)
		if r.file() != want {
			t.Fatalf("config\n%s", r.file())
		}
		if got := strings.Join(actions(t, db), ","); got != audit.ActionSettingsRequested+","+audit.ActionSettingsApplied {
			t.Fatalf("audit %s", got)
		}
		page, _ := audit.List(ctx, db.Gorm, audit.Filter{Action: audit.ActionSettingsRequested})
		d := page.Events[0].Details
		if m, _ := d["files.max_size_bytes"].(map[string]any); m["old"] != float64(52428800) || m["new"] != float64(104857600) {
			t.Fatalf("details %v", d)
		}
		if *page.Events[0].ActorID != owner.UserID {
			t.Fatal("actor")
		}
		s.Reconcile(ctx) // once only
		if n := len(actions(t, db)); n != 2 {
			t.Fatalf("audited again: %d", n)
		}
	})
}

// Rolled back and refused results are recorded too; a request no applier takes is withdrawn.
func TestResultsAudited(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		ctx := context.Background()
		r := newRig(t)
		s := newService(t, db, r, nil)
		now := time.Now()
		s.Now = func() time.Time { return now }
		if _, err := s.Request(ctx, owner, "s", raw(map[string]string{"files.max_per_post": "3"}), nil); err != nil {
			t.Fatal(err)
		}
		r.run(ctx) // nobody confirms
		s.Reconcile(ctx)
		if _, err := s.Request(ctx, owner, "s", raw(map[string]string{"log.level": `"warn"`}), nil); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(r.config, []byte(baseConfig+"# a hand edit\n"), 0o644)
		r.run(ctx) // stale
		s.Reconcile(ctx)
		if _, err := s.Request(ctx, owner, "s", raw(map[string]string{"log.level": `"warn"`}), nil); err != nil {
			t.Fatal(err)
		}
		s.Reconcile(ctx)
		if v, _ := s.Get(ctx, owner, "s"); v.Pending == nil || v.Pending.Phase != "waiting" {
			t.Fatal("withdrawn too early")
		}
		now = now.Add(2 * time.Minute) // the applier never ran
		s.Reconcile(ctx)
		if v, _ := s.Get(ctx, owner, "s"); v.Pending != nil {
			t.Fatal("not withdrawn")
		}
		noRequest(t, r)
		want := []string{audit.ActionSettingsRequested, audit.ActionSettingsRolledBack, audit.ActionSettingsRequested,
			audit.ActionSettingsRefused, audit.ActionSettingsRequested, audit.ActionSettingsRefused}
		if got := actions(t, db); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("audit %v", got)
		}
		page, _ := audit.List(ctx, db.Gorm, audit.Filter{Action: audit.ActionSettingsRefused, Limit: 1})
		if !strings.Contains(page.Events[0].Details["reason"].(string), "not running") {
			t.Fatalf("details %v", page.Events[0].Details)
		}
	})
}
