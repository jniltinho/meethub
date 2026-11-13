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
