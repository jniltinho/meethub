package handlers_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/settings"
)

func TestSettingsAPI(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		e := newEnv(t, db, false)
		owner := e.setupOwner()
		owner.createUser("adm@acme.io", "admin")
		admin := e.login("adm@acme.io", "a long password!")

		dir := t.TempDir()
		file := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(file, []byte("[files]\nmax_per_post = 10 # files a message may carry\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		vars := map[string]string{"MEETHUB_FILES_MAX_SIZE_BYTES": "10485760", "MEETHUB_SMTP_PASSWORD": "hunter2-not-shown",
			"MEETHUB_SMTP_HOST": "localhost", "MEETHUB_SMTP_USERNAME": "u", "MEETHUB_SMTP_FROM": "m@acme.io"}
		lookup := func(k string) (string, bool) { v, ok := vars[k]; return v, ok }
		cfg, err := config.LoadWith(file, lookup, os.ReadFile)
		if err != nil {
			t.Fatal(err)
		}
		cfg.App.DataDir = filepath.Join(dir, "data")
		s := settings.New(db.Gorm, cfg, file, "boot-1")
		s.Lookup = lookup
		e.api.Settings = s

		if r := admin.do("GET", "/admin/settings", nil); r.Code != 403 {
			t.Fatalf("admin get: %d", r.Code)
		}
		if r := admin.do("PUT", "/admin/settings", map[string]any{"changes": map[string]any{"log.level": "warn"}}); r.Code != 403 {
			t.Fatalf("admin put: %d", r.Code)
		}
		r := owner.do("GET", "/admin/settings", nil)
		if r.Code != 200 || strings.Contains(r.Body.String(), "hunter2") || r.body["applier"].(map[string]any)["available"] != false {
			t.Fatalf("get: %d %s", r.Code, r.Body.String())
		}
		// no applier yet: read-only
		if r := owner.do("PUT", "/admin/settings", map[string]any{"changes": map[string]any{"log.level": "warn"}}); r.Code != 409 || r.code() != "settings_read_only" {
			t.Fatalf("read-only: %d %s", r.Code, r.Body.String())
		}
		state := filepath.Join(dir, settings.StateDirName)
		_ = os.MkdirAll(state, 0o750)
		presence, _ := json.Marshal(settings.Presence{Config: file, Requests: filepath.Join(dir, "data", "settings")})
		_ = os.WriteFile(filepath.Join(state, settings.PresenceFile), presence, 0o640)

		for _, tt := range []struct {
			body   map[string]any
			status int
			code   string
			msg    string
		}{
			{map[string]any{"changes": map[string]any{"smtp.host": "evil.example.com"}}, 400, "setting_not_editable", "smtp.host"},
			{map[string]any{"changes": map[string]any{"session.idle_timeout": "1s"}}, 400, "invalid_setting", "at least 5m"},
			{map[string]any{"changes": map[string]any{"files.max_size_bytes": 104857600}}, 409, "setting_locked", "MEETHUB_FILES_MAX_SIZE_BYTES"},
			{map[string]any{"changes": map[string]any{"log.level": "warn"}, "extra": 1}, 400, "invalid_input", ""},
		} {
			r := owner.do("PUT", "/admin/settings", tt.body)
			if r.Code != tt.status || r.code() != tt.code || !strings.Contains(r.Body.String(), tt.msg) {
				t.Fatalf("%v: %d %s", tt.body, r.Code, r.Body.String())
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "data", "settings", settings.RequestFile)); err == nil {
			t.Fatal("a refused change left a request")
		}
		r = owner.do("PUT", "/admin/settings", map[string]any{"changes": map[string]any{"files.max_per_post": 20}})
		if r.Code != 202 || r.body["pending"] == nil {
			t.Fatalf("put: %d %s", r.Code, r.Body.String())
		}
		if b, err := os.ReadFile(file); err != nil || !strings.Contains(string(b), "max_per_post = 10") {
			t.Fatal("the server wrote config.toml")
		}
		if r := owner.do("PUT", "/admin/settings", map[string]any{"restore": []string{"log.level"}}); r.Code != 409 || r.code() != "settings_pending" {
			t.Fatalf("second: %d %s", r.Code, r.Body.String())
		}
		id := r.body["pending"].(map[string]any)["id"].(string)
		if r := owner.do("POST", "/admin/settings/confirm", map[string]any{"id": id}); r.Code != 409 || r.code() != "settings_not_confirmable" {
			t.Fatalf("confirm before the restart: %d %s", r.Code, r.Body.String())
		}
		// without the CSRF token: refused before anything
		owner.csrf = "x"
		if r := owner.do("POST", "/admin/settings/confirm", map[string]any{"id": id}); r.Code != 403 || r.code() != "csrf_failed" {
			t.Fatalf("csrf: %d", r.Code)
		}
		evs := owner.do("GET", "/admin/audit?action=settings.requested", nil)
		if events, _ := evs.body["events"].([]any); len(events) != 1 {
			t.Fatalf("audit: %s", evs.Body.String())
		}
	})
}
