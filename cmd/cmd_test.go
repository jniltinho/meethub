package cmd

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// execute runs the CLI in-process from an empty directory (no config.toml).
func execute(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Chdir(t.TempDir())
	var out, errOut bytes.Buffer
	code = run(args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestHelpListsCommands(t *testing.T) {
	code, out, _ := execute(t, "--help")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, c := range []string{"healthcheck", "migrate", "version"} {
		if !strings.Contains(out, c) {
			t.Errorf("help does not list %q:\n%s", c, out)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, errOut := execute(t, "frobnicate")
	if code != 1 || !strings.Contains(errOut, `unknown command "frobnicate"`) {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestVersionWithoutConfig(t *testing.T) {
	t.Setenv("MEETHUB_APP_ENV", "invalid") // would fail validation if version loaded the config
	code, out, errOut := execute(t, "version")
	if code != 0 || !strings.HasPrefix(out, "meethub dev (commit none") {
		t.Fatalf("exit %d, out %q, err %q", code, out, errOut)
	}
	code, out, _ = execute(t, "version", "--json")
	var v map[string]string
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil || v["version"] != "dev" {
		t.Fatalf("json version: exit %d %q", code, out)
	}
}

func TestHealthcheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/health" {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1") // must be ignored
	t.Setenv("MEETHUB_HTTP_ADDR", ":"+port)      // wildcard → 127.0.0.1
	if code, out, errOut := execute(t, "healthcheck"); code != 0 {
		t.Fatalf("server up: exit %d %q %q", code, out, errOut)
	}

	srv.Close()
	if code, _, _ := execute(t, "healthcheck"); code != 1 {
		t.Fatalf("server down: exit %d, want 1", code)
	}
}

func TestHealthURL(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		":8080":        "http://127.0.0.1:8080/api/v1/health",
		"0.0.0.0:8080": "http://127.0.0.1:8080/api/v1/health",
		"[::]:8080":    "http://[::1]:8080/api/v1/health",
		"10.1.2.3:80":  "http://10.1.2.3:80/api/v1/health",
	} {
		if got, err := healthURL(in); err != nil || got != want {
			t.Errorf("healthURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestMigrateFreshInstall(t *testing.T) {
	t.Setenv("MEETHUB_APP_DATA_DIR", t.TempDir())
	t.Setenv("MEETHUB_LOG_LEVEL", "error")
	if code, out, errOut := execute(t, "migrate", "status"); code != 0 || !strings.Contains(out, "pending") {
		t.Fatalf("status before: exit %d %q %q", code, out, errOut)
	}
	if code, out, errOut := execute(t, "migrate", "up"); code != 0 || !strings.Contains(out, "applied") {
		t.Fatalf("up: exit %d %q %q", code, out, errOut)
	}
	code, out, _ := execute(t, "migrate", "status", "--json")
	var st []struct{ Applied bool }
	if code != 0 || json.Unmarshal([]byte(out), &st) != nil || len(st) == 0 {
		t.Fatalf("status json: exit %d %q", code, out)
	}
	for _, s := range st {
		if !s.Applied {
			t.Fatalf("pending after up: %q", out)
		}
	}
	if code, out, _ := execute(t, "migrate", "up"); code != 0 || !strings.Contains(out, "nothing pending") {
		t.Fatalf("second up: %q", out)
	}
}
