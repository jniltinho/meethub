package diagnosis

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jniltinho/meethub/internal/config"
)

func TestRun(t *testing.T) {
	ok := func(context.Context) error { return nil }
	down := func(context.Context) error { return errors.New("dial tcp 10.0.0.9:6379: refused") }
	bare := config.Default()
	bare.HTTP.PublicURL = "http://localhost:8080"
	full := config.Default()
	full.App.Env, full.HTTP.PublicURL = config.EnvProduction, "https://meet.example.com"
	full.Recordings.Enabled, full.SMTP.Host = true, "smtp.example.com"
	full.Secret.Key = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // test-only: 32 bytes
	full.SMTP.Password, full.AI.APIKey = "smtp-secret-value", "ai-secret-value"
	lan := config.Default()
	lan.HTTP.PublicURL = "http://10.0.0.5:8080"
	yes := func(context.Context) bool { return true }

	tests := []struct {
		name string
		svc  Service
		want map[string]string
	}{
		{"a fresh local install", Service{Config: bare, Checks: []Check{{"database", ok}, {"redis", ok}, {"storage", ok}}},
			map[string]string{"database": OK, "redis": OK, "storage": OK, "https": OK, "mode": Note, "meetings": Note,
				"recordings": Note, "email": Note, "secret_key": Warning, "ai": Note, "applier": Note}},
		{"everything set up", Service{Config: full, Checks: []Check{{"database", ok}, {"redis", ok}, {"storage", ok}},
			LiveKit: ok, Summaries: yes, Applier: func() bool { return true }},
			map[string]string{"database": OK, "redis": OK, "storage": OK, "https": OK, "mode": OK, "meetings": OK,
				"recordings": OK, "email": OK, "secret_key": OK, "ai": OK, "applier": OK}},
		{"redis down, LiveKit unreachable, plain HTTP on a LAN", Service{Config: lan,
			Checks: []Check{{"database", ok}, {"redis", down}, {"storage", ok}}, LiveKit: down},
			map[string]string{"redis": Problem, "https": Warning, "meetings": Warning}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := tt.svc.Run(context.Background())
			got := map[string]string{}
			for _, i := range items {
				got[i.ID] = i.Level
			}
			for id, want := range tt.want {
				if got[id] != want {
					t.Errorf("%s: %q, want %q", id, got[id], want)
				}
			}
			if len(items) != 11 || items[0].ID != "database" || items[0].Detail != "sqlite" {
				t.Fatalf("items %+v", items)
			}
			b, _ := json.Marshal(items)
			for _, secret := range []string{"secret-value", "10.0.0.9", "refused", "example.com", "MDEy"} {
				if strings.Contains(string(b), secret) {
					t.Fatalf("the diagnosis carries %q: %s", secret, b)
				}
			}
		})
	}
}
