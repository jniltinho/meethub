package config_test

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jniltinho/meethub/internal/config"
)

func TestEditAdmin(t *testing.T) {
	tests := []struct {
		name   string
		old    string
		values map[string]any
		want   string // "" with wantErr
		err    error
	}{
		{
			name:   "an existing key keeps its trailing comment",
			old:    "[files]                      # attachments\nmax_size_bytes = 52428800    # per file: 50 MiB\nmax_per_post = 10\n",
			values: map[string]any{"files.max_size_bytes": int64(104857600)},
			want:   "[files]                      # attachments\nmax_size_bytes = 104857600    # per file: 50 MiB\nmax_per_post = 10\n",
		},
		{
			name:   "a missing key goes after the last line of its table",
			old:    "[files]\nmax_per_post = 10\n\n[log]\nlevel = \"info\"\n",
			values: map[string]any{"files.max_size_bytes": int64(1 << 20)},
			want:   "[files]\nmax_per_post = 10\nmax_size_bytes = 1048576\n\n[log]\nlevel = \"info\"\n",
		},
		{
			name:   "a missing table is appended",
			old:    "[log]\nlevel = \"info\"\n",
			values: map[string]any{"session.idle_timeout": "30m", "oidc.name": "Acme ID"},
			want:   "[log]\nlevel = \"info\"\n\n[oidc]\nname = 'Acme ID'\n\n[session]\nidle_timeout = '30m'\n",
		},
		{
			name:   "no final newline",
			old:    "[log]\nlevel = \"info\"",
			values: map[string]any{"log.format": "text", "log.level": "warn"},
			want:   "[log]\nlevel = 'warn'\nformat = 'text'\n",
		},
		{
			name:   "an empty file",
			old:    "",
			values: map[string]any{"files.max_per_post": int64(3)},
			want:   "\n[files]\nmax_per_post = 3\n",
		},
		{
			name:   "arrays, durations and a '#' inside a string are kept",
			old:    "[oidc]\nscopes = [\"openid\", \"email\"] # asked for\nname = \"A # B\"   # the button\n[session]\nidle_timeout = \"168h\"\n",
			values: map[string]any{"oidc.name": "C", "session.idle_timeout": "24h"},
			want:   "[oidc]\nscopes = [\"openid\", \"email\"] # asked for\nname = 'C'   # the button\n[session]\nidle_timeout = '24h'\n",
		},
		{
			name:   "a key in a sub-table is not the table's",
			old:    "[files]\nmax_per_post = 2\n[files.local]\ndir = \"/srv\"\n",
			values: map[string]any{"files.max_per_post": int64(4)},
			want:   "[files]\nmax_per_post = 4\n[files.local]\ndir = \"/srv\"\n",
		},
		{
			name:   "CRLF line ends stay",
			old:    "[log]\r\nlevel = \"info\" # x\r\n",
			values: map[string]any{"log.level": "debug"},
			want:   "[log]\r\nlevel = 'debug' # x\r\n",
		},
		{
			name:   "a key inside a multi-line string is refused by the comparison",
			old:    "[oidc]\nname = \"\"\"\n[log]\nlevel = \"info\"\n\"\"\"\n",
			values: map[string]any{"log.level": "warn"},
			err:    config.ErrUnusualFile,
		},
		{
			name:   "a triple-quoted value is refused, its comment would be lost",
			old:    "[oidc]\nname = '''Acme's login''' # keep\n",
			values: map[string]any{"oidc.name": "Acme"},
			err:    config.ErrUnusualFile,
		},
		{
			name:   "a dotted key at the root is refused when a table must be added",
			old:    "log.level = \"info\"\n",
			values: map[string]any{"log.format": "text"},
			err:    config.ErrUnusualFile,
		},
		{
			name:   "a key outside the allowlist",
			old:    "",
			values: map[string]any{"database.host": "evil"},
			err:    config.ErrNotEditable,
		},
		{
			name:   "a value out of bounds",
			old:    "",
			values: map[string]any{"files.max_per_post": int64(51)},
			err:    &config.AdminValueError{},
		},
		{
			name:   "a file with a secret is refused",
			old:    "[smtp]\npassword = \"x\"\n",
			values: map[string]any{"log.level": "warn"},
			err:    errors.New("secrets"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.EditAdmin([]byte(tt.old), tt.values)
			if tt.err != nil {
				var ve *config.AdminValueError
				switch {
				case err == nil:
					t.Fatalf("want an error, got\n%s", got)
				case errors.As(tt.err, &ve):
					if !errors.As(err, &ve) {
						t.Fatalf("want a value error, got %v", err)
					}
				case errors.Is(tt.err, config.ErrNotEditable) || errors.Is(tt.err, config.ErrUnusualFile):
					if !errors.Is(err, tt.err) {
						t.Fatalf("want %v, got %v", tt.err, err)
					}
				case !strings.Contains(err.Error(), tt.err.Error()):
					t.Fatalf("want %q in %v", tt.err, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("got\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// The packaged file: one change touches one line, its comment included.
func TestEditAdminExample(t *testing.T) {
	old, err := os.ReadFile("../../config.toml.example")
	if err != nil {
		t.Fatal(err)
	}
	got, err := config.EditAdmin(old, map[string]any{"files.max_size_bytes": int64(100 << 20)})
	if err != nil {
		t.Fatal(err)
	}
	a, b := strings.Split(string(old), "\n"), strings.Split(string(got), "\n")
	if len(a) != len(b) {
		t.Fatalf("lines %d → %d", len(a), len(b))
	}
	var diff []string
	for i := range a {
		if a[i] != b[i] {
			diff = append(diff, b[i])
		}
	}
	if len(diff) != 1 || diff[0] != "max_size_bytes = 104857600    # per file: 50 MiB" {
		t.Fatalf("changed lines: %q", diff)
	}
}

func TestParseAdminValueBounds(t *testing.T) {
	tests := []struct {
		key, raw string
		want     any
		msg      string
	}{
		{"files.max_size_bytes", `1048576`, int64(1 << 20), ""},
		{"files.max_size_bytes", `1048575`, nil, "at least 1048576"},
		{"files.max_size_bytes", `2147483648`, int64(2 << 30), ""},
		{"files.max_size_bytes", `2147483649`, nil, "at most 2147483648"},
		{"files.max_size_bytes", `1.5`, nil, "whole number"},
		{"files.max_size_bytes", `"10"`, nil, "whole number"},
		{"files.max_per_post", `0`, nil, "at least 1"},
		{"files.max_per_post", `50`, int64(50), ""},
		{"files.max_per_post", `51`, nil, "at most 50"},
		{"session.idle_timeout", `"1s"`, nil, "at least 5m"},
		{"session.idle_timeout", `" 5m "`, "5m", ""},
		{"session.idle_timeout", `"2160h"`, "2160h", ""},
		{"session.idle_timeout", `"2161h"`, nil, "at most 2160h"},
		{"session.idle_timeout", `"7d"`, nil, "duration"},
		{"session.idle_timeout", `60`, nil, "duration"},
		{"session.absolute_lifetime", `"59m"`, nil, "at least 1h"},
		{"session.absolute_lifetime", `"8760h"`, "8760h", ""},
		{"session.absolute_lifetime", `"8761h"`, nil, "at most 8760h"},
		{"oidc.name", `""`, nil, "1 to 40"},
		{"oidc.name", `"  "`, nil, "1 to 40"},
		{"oidc.name", `"` + strings.Repeat("é", 40) + `"`, strings.Repeat("é", 40), ""},
		{"oidc.name", `"` + strings.Repeat("a", 41) + `"`, nil, "1 to 40"},
		{"oidc.name", `"a\nb"`, nil, "one line"},
		{"log.level", `"warn"`, "warn", ""},
		{"log.level", `"trace"`, nil, "one of debug, info, warn, error"},
		{"log.format", `"text"`, "text", ""},
		{"log.format", `"xml"`, nil, "one of json, text"},
	}
	for _, tt := range tests {
		t.Run(tt.key+"="+tt.raw, func(t *testing.T) {
			got, err := config.ParseAdminValue(tt.key, json.RawMessage(tt.raw))
			if tt.msg != "" {
				var ve *config.AdminValueError
				if !errors.As(err, &ve) || !strings.Contains(ve.Msg, tt.msg) || ve.Key != tt.key {
					t.Fatalf("want %q, got %v", tt.msg, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %v %v, want %v", got, err, tt.want)
			}
		})
	}
	if _, err := config.ParseAdminValue("smtp.host", json.RawMessage(`"x"`)); !errors.Is(err, config.ErrNotEditable) {
		t.Fatalf("smtp.host: %v", err)
	}
}

// Every default is within its own bounds (Restore default writes it).
func TestAdminDefaultsInBounds(t *testing.T) {
	def := config.Default()
	for _, k := range config.AdminKeys {
		v := def.AdminValue(k.Key)
		raw, _ := json.Marshal(v)
		if _, err := config.ParseAdminValue(k.Key, raw); err != nil {
			t.Errorf("%s default %v: %v", k.Key, v, err)
		}
	}
	if config.AdminKeys[0].Env() != "MEETHUB_FILES_MAX_SIZE_BYTES" {
		t.Fatal(config.AdminKeys[0].Env())
	}
}

func TestFieldsHideSecrets(t *testing.T) {
	cfg := config.Default()
	cfg.SMTP.Password = "hunter2-secret"
	for _, f := range cfg.Fields() {
		if f.Key == "smtp.password" && (f.Value != true || !f.Secret) {
			t.Fatalf("smtp.password: %+v", f)
		}
		if s, _ := json.Marshal(f.Value); strings.Contains(string(s), "hunter2") {
			t.Fatalf("%s shows the secret", f.Key)
		}
	}
	if config.FormatDuration(cfg.Session.IdleTimeout.Std()) != "168h" {
		t.Fatal(config.FormatDuration(cfg.Session.IdleTimeout.Std()))
	}
}
