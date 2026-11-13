package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/testenv"
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
	for _, c := range []string{"healthcheck", "migrate", "server", "user", "version"} {
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

func TestServerInvalidConfig(t *testing.T) {
	t.Setenv("MEETHUB_DATABASE_DRIVER", "oracle")
	t.Setenv("MEETHUB_HTTP_ADDR", "abc")
	code, _, errOut := execute(t, "server")
	if code != 1 || !strings.Contains(errOut, "database.driver") || !strings.Contains(errOut, "http.addr") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestServerStartsAndStops(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("MEETHUB_APP_DATA_DIR", t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	t.Setenv("MEETHUB_HTTP_ADDR", addr)
	t.Setenv("MEETHUB_REDIS_ADDR", testenv.RedisAddr(t))

	c := &cli{in: strings.NewReader(""), out: io.Discard, errOut: io.Discard}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.runServer(ctx, false) }()

	var resp *http.Response
	for range 100 {
		resp, err = http.Get("http://" + addr + "/api/v1/ready") //nolint:noctx // test client
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server never answered: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ready = %d", resp.StatusCode)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("runServer: %v", err)
	}
}

// executeIn runs the CLI with stdin, keeping the current directory.
func executeIn(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestUserCommands(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("MEETHUB_APP_DATA_DIR", t.TempDir())
	t.Setenv("MEETHUB_LOG_LEVEL", "error")
	if code, _, errOut := executeIn(t, "", "migrate", "up"); code != 0 {
		t.Fatal(errOut)
	}
	// first user rules
	if code, _, errOut := executeIn(t, "a long password!\n", "user", "create", "--email", "m@acme.io", "--name", "M", "--role", "member", "--password-stdin"); code != 1 || !strings.Contains(errOut, "first user must be an owner") {
		t.Fatalf("first member: %d %q", code, errOut)
	}
	if code, _, errOut := executeIn(t, "a long password!\n", "user", "create", "--email", "o@acme.io", "--name", "O", "--role", "owner"); code != 1 || !strings.Contains(errOut, "organization name") {
		t.Fatalf("first owner without org: %d %q", code, errOut)
	}
	// no terminal and no --password-stdin
	if code, _, errOut := executeIn(t, "", "user", "create", "--email", "o@acme.io", "--name", "O", "--role", "owner", "--org-name", "Acme"); code != 1 || !strings.Contains(errOut, "--password-stdin") {
		t.Fatalf("no terminal: %d %q", code, errOut)
	}
	code, out, errOut := executeIn(t, "a long password!\n", "user", "create", "--email", "ana@acme.io", "--name", "Ana", "--role", "owner", "--org-name", "Acme", "--password-stdin")
	if code != 0 || !strings.Contains(errOut, `initialized organization "Acme" (slug acme)`) || !strings.Contains(out, "as owner") {
		t.Fatalf("initialize: %d %q %q", code, out, errOut)
	}
	if code, _, errOut := executeIn(t, "another long pass\n", "user", "create", "--email", "bob@acme.io", "--name", "Bob", "--password-stdin"); code != 0 {
		t.Fatalf("second user: %q", errOut)
	}
	code, out, _ = executeIn(t, "", "user", "list", "--json")
	if code != 0 || strings.Contains(out, "argon2") || strings.Contains(out, "password") {
		t.Fatalf("list json: %q", out)
	}
	var users []map[string]any
	if json.Unmarshal([]byte(out), &users) != nil || len(users) != 2 {
		t.Fatalf("list: %q", out)
	}
	if code, out, errOut := executeIn(t, "a third long password\n", "user", "set-password", "--email", "BOB@acme.io", "--password-stdin"); code != 0 || !strings.Contains(out, "sessions were ended") {
		t.Fatalf("set-password: %d %q %q", code, out, errOut)
	}
}
