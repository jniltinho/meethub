package config_test

import (
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/config"
)

type env map[string]string

func (e env) lookup(k string) (string, bool) { v, ok := e[k]; return v, ok }

// files maps paths to contents; a missing path reads as fs.ErrNotExist.
type files map[string]string

func (f files) read(p string) ([]byte, error) {
	if s, ok := f[p]; ok {
		return []byte(s), nil
	}
	return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
}

func load(t *testing.T, path string, e env, f files) (*config.Config, error) {
	t.Helper()
	return config.LoadWith(path, e.lookup, f.read)
}

func TestLoadPrecedence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		path  string
		env   env
		files files
		check func(t *testing.T, c *config.Config)
	}{
		{
			name: "defaults without a file",
			check: func(t *testing.T, c *config.Config) {
				if c.Database.Driver != "sqlite" || c.HTTP.Addr != ":8080" || c.Redis.Addr != "localhost:6379" || c.Redis.DB != 0 {
					t.Fatalf("unexpected defaults: %+v", c)
				}
				if got := c.SQLitePath(); got != "data/meethub.db" {
					t.Fatalf("SQLitePath = %q", got)
				}
			},
		},
		{
			name:  "environment overrides the file",
			files: files{"config.toml": "[http]\naddr = \":8080\"\n"},
			env:   env{"MEETHUB_HTTP_ADDR": ":9090"},
			check: func(t *testing.T, c *config.Config) {
				if c.HTTP.Addr != ":9090" {
					t.Fatalf("addr = %q", c.HTTP.Addr)
				}
			},
		},
		{
			name:  "file over defaults, second default path",
			files: files{"/etc/meethub/config.toml": "[log]\nlevel = \"debug\"\n[session]\nidle_timeout = \"1h\"\n"},
			check: func(t *testing.T, c *config.Config) {
				if c.Log.Level != "debug" || c.Session.IdleTimeout.Std() != time.Hour {
					t.Fatalf("file not applied: %+v", c)
				}
			},
		},
		{
			name: "env types: list, int, duration",
			env: env{
				"MEETHUB_HTTP_TRUSTED_PROXIES":  "10.0.0.0/8, 192.168.1.1",
				"MEETHUB_REDIS_DB":              "3",
				"MEETHUB_HTTP_SHUTDOWN_TIMEOUT": "5s",
			},
			check: func(t *testing.T, c *config.Config) {
				if len(c.HTTP.TrustedProxies) != 2 || c.Redis.DB != 3 || c.HTTP.ShutdownTimeout.Std() != 5*time.Second {
					t.Fatalf("env not parsed: %+v", c.HTTP)
				}
				if p := c.TrustedProxyPrefixes(); len(p) != 2 || p[1].Bits() != 32 {
					t.Fatalf("prefixes = %v", p)
				}
			},
		},
		{
			name:  "secret from _FILE (Docker secret)",
			env:   env{"MEETHUB_DATABASE_PASSWORD_FILE": "/run/secrets/db"},
			files: files{"/run/secrets/db": "s3cret\n"},
			check: func(t *testing.T, c *config.Config) {
				if c.Database.Password != "s3cret" {
					t.Fatalf("password = %q", c.Database.Password)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := load(t, tt.path, tt.env, tt.files)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			tt.check(t, c)
		})
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		path  string
		env   env
		files files
		want  []string // substrings that must all appear
	}{
		{
			name:  "password in the file",
			files: files{"config.toml": "[database]\npassword = \"x\"\n"},
			want:  []string{"database.password", "MEETHUB_DATABASE_PASSWORD", "MEETHUB_DATABASE_PASSWORD_FILE"},
		},
		{
			name: "both secret forms set",
			env:  env{"MEETHUB_DATABASE_PASSWORD": "a", "MEETHUB_DATABASE_PASSWORD_FILE": "/x"},
			want: []string{"MEETHUB_DATABASE_PASSWORD and MEETHUB_DATABASE_PASSWORD_FILE are both set"},
		},
		{
			name: "unknown file driver and bad limits",
			env:  env{"MEETHUB_FILES_DRIVER": "ftp", "MEETHUB_FILES_MAX_SIZE_BYTES": "0", "MEETHUB_FILES_MAX_PER_POST": "0"},
			want: []string{"files.driver", `"local" or "s3"`, "files.max_size_bytes", "files.max_per_post"},
		},
		{
			name: "s3 without endpoint, bucket or credentials",
			env:  env{"MEETHUB_FILES_DRIVER": "s3"},
			want: []string{"files.s3", "endpoint and bucket", "MEETHUB_FILES_S3_ACCESS_KEY", "MEETHUB_FILES_S3_SECRET_KEY"},
		},
		{
			name:  "s3 secret in the file",
			files: files{"config.toml": "[files.s3]\nsecret_key = \"x\"\n"},
			want:  []string{"files.s3.secret_key", "MEETHUB_FILES_S3_SECRET_KEY"},
		},
		{
			name: "several errors reported at once",
			env:  env{"MEETHUB_DATABASE_DRIVER": "oracle", "MEETHUB_HTTP_ADDR": "abc"},
			want: []string{"database.driver", `"sqlite", "mariadb" or "postgres"`, "http.addr"},
		},
		{
			name: "production requires https",
			env:  env{"MEETHUB_APP_ENV": "production"},
			want: []string{"http.public_url", "https://"},
		},
		{
			name:  "unknown key in file",
			files: files{"config.toml": "[http]\nport = 1\n"},
			want:  []string{"unknown settings"},
		},
		{
			name: "explicit path missing",
			path: "/nope.toml",
			want: []string{"read config file"},
		},
		{
			name: "bad trusted proxy",
			env:  env{"MEETHUB_HTTP_TRUSTED_PROXIES": "not-an-ip"},
			want: []string{"http.trusted_proxies", "not-an-ip"},
		},
		{
			name: "bad integer",
			env:  env{"MEETHUB_REDIS_DB": "x"},
			want: []string{"MEETHUB_REDIS_DB", "not an integer"},
		},
		{
			name:  "livekit secret in the file",
			files: files{"config.toml": "[livekit]\napi_secret = \"x\"\n"},
			want:  []string{"livekit.api_secret", "MEETHUB_LIVEKIT_API_SECRET", "MEETHUB_LIVEKIT_API_SECRET_FILE"},
		},
		{
			name: "livekit url without credentials",
			env:  env{"MEETHUB_LIVEKIT_URL": "ws://localhost:7880"},
			want: []string{"livekit.api_key", "livekit.api_secret"},
		},
		{
			name: "livekit short secret",
			env:  env{"MEETHUB_LIVEKIT_URL": "ws://localhost:7880", "MEETHUB_LIVEKIT_API_KEY": "k", "MEETHUB_LIVEKIT_API_SECRET": "short"},
			want: []string{"livekit.api_secret", "32 bytes"},
		},
		{
			name: "livekit credentials without url",
			env:  env{"MEETHUB_LIVEKIT_API_KEY": "k", "MEETHUB_LIVEKIT_API_SECRET": strings.Repeat("s", 32)},
			want: []string{"livekit.url"},
		},
		{
			name: "livekit http url",
			env:  env{"MEETHUB_LIVEKIT_URL": "http://localhost:7880", "MEETHUB_LIVEKIT_API_KEY": "k", "MEETHUB_LIVEKIT_API_SECRET": strings.Repeat("s", 32)},
			want: []string{"livekit.url", "ws(s)"},
		},
		{
			name: "livekit insecure in production",
			env: env{
				"MEETHUB_APP_ENV": "production", "MEETHUB_HTTP_PUBLIC_URL": "https://chat.example.com",
				"MEETHUB_LIVEKIT_URL": "ws://meet.example.com", "MEETHUB_LIVEKIT_API_KEY": "k",
				"MEETHUB_LIVEKIT_API_SECRET": strings.Repeat("s", 32),
			},
			want: []string{"livekit.url", "wss://"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := load(t, tt.path, tt.env, tt.files)
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}

func TestLiveKitSettings(t *testing.T) {
	t.Parallel()
	secret := strings.Repeat("s", 32)
	tests := []struct {
		name    string
		env     env
		enabled bool
		server  string
	}{
		{name: "off by default"},
		{
			name:    "api url derived from wss",
			env:     env{"MEETHUB_LIVEKIT_URL": "wss://meet.example.com", "MEETHUB_LIVEKIT_API_KEY": "k", "MEETHUB_LIVEKIT_API_SECRET": secret},
			enabled: true, server: "https://meet.example.com",
		},
		{
			name: "explicit api url",
			env: env{
				"MEETHUB_LIVEKIT_URL": "ws://localhost:7880", "MEETHUB_LIVEKIT_API_URL": "http://livekit:7880",
				"MEETHUB_LIVEKIT_API_KEY": "k", "MEETHUB_LIVEKIT_API_SECRET": secret,
			},
			enabled: true, server: "http://livekit:7880",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := load(t, "", tt.env, nil)
			if err != nil {
				t.Fatal(err)
			}
			if c.LiveKit.Enabled() != tt.enabled || (tt.enabled && c.LiveKit.ServerURL() != tt.server) {
				t.Fatalf("enabled=%v server=%q", c.LiveKit.Enabled(), c.LiveKit.ServerURL())
			}
			if o := c.LiveKit.BrowserOrigins(); tt.enabled && (len(o) != 2 || o[1][:4] != "http") {
				t.Fatalf("origins = %v", o)
			}
			if r := c.Redacted(); tt.enabled && (r.LiveKit.APISecret != "***" || r.LiveKit.APIKey != "***") {
				t.Fatalf("not redacted: %+v", r.LiveKit)
			}
		})
	}
}

func TestRedactedHidesSecrets(t *testing.T) {
	t.Parallel()
	c, err := load(t, "", env{"MEETHUB_DATABASE_PASSWORD": "pw", "MEETHUB_SETUP_TOKEN": "tok"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := c.Redacted()
	if r.Database.Password != "***" || r.Setup.Token != "***" || r.Redis.Password != "" {
		t.Fatalf("redacted = %+v %+v", r.Database, r.Setup)
	}
	if c.Database.Password != "pw" {
		t.Fatal("Redacted modified the original")
	}
}

func TestExampleParsesAndValidates(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../config.toml.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := load(t, "config.toml.example", nil, files{"config.toml.example": string(data)}); err != nil {
		t.Fatalf("example: %v", err)
	}
}
