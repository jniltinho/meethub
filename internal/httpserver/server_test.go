package httpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/meethub/internal/httpserver"
)

var dist = fstest.MapFS{
	"index.html":           {Data: []byte(`<html data-theme="%MEETHUB_THEME%" style="color-scheme: %MEETHUB_SCHEME%"></html>`)},
	"assets/app-abc123.js": {Data: []byte("console.log(1)")},
	"favicon.svg":          {Data: []byte("<svg/>")},
}

func newServer(t *testing.T, mutate ...func(*httpserver.Options)) *httpserver.Server {
	t.Helper()
	o := httpserver.Options{Dist: dist, BodyLimitBytes: 1024}
	for _, m := range mutate {
		m(&o)
	}
	s := httpserver.New(o)
	// test-only routes
	s.API().POST("/echo", func(c *echo.Context) error {
		var body map[string]any
		if err := c.Bind(&body); err != nil {
			return httpserver.ErrBadRequest
		}
		return c.JSON(http.StatusOK, body)
	})
	s.API().GET("/ip", func(c *echo.Context) error { return c.String(http.StatusOK, c.RealIP()) })
	return s
}

func do(t *testing.T, s *httpserver.Server, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("not a JSON error (%d): %q", w.Code, w.Body.String())
	}
	if body.Error.Message == "" {
		t.Fatalf("error without message: %q", w.Body.String())
	}
	return body.Error.Code
}

func TestAPIErrors(t *testing.T) {
	t.Parallel()
	s := newServer(t)
	tests := []struct {
		name       string
		req        *http.Request
		wantStatus int
		wantCode   string
	}{
		{"unknown API route", httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil), 404, "not_found"},
		{"wrong method", httptest.NewRequest(http.MethodDelete, "/api/v1/health", nil), 405, "method_not_allowed"},
		{"malformed JSON", jsonReq(`{"a":`), 400, "bad_request"},
		{"oversized body", jsonReq(`{"a":"` + strings.Repeat("x", 2048) + `"}`), 413, "payload_too_large"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := do(t, s, tt.req)
			if w.Code != tt.wantStatus || errorCode(t, w) != tt.wantCode {
				t.Fatalf("got %d %q", w.Code, w.Body.String())
			}
		})
	}
}

func jsonReq(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/echo", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	for name, hsts := range map[string]bool{"http": false, "https": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := do(t, newServer(t, func(o *httpserver.Options) { o.SecureHeaders = hsts }), httptest.NewRequest(http.MethodGet, "/", nil))
			h := w.Header()
			if h.Get("Content-Security-Policy") != httpserver.ContentSecurityPolicy ||
				h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "same-origin" {
				t.Fatalf("headers: %v", h)
			}
			if got := h.Get("Strict-Transport-Security") != ""; got != hsts {
				t.Fatalf("HSTS present = %v, want %v", got, hsts)
			}
		})
	}
}

func TestConnectSources(t *testing.T) {
	t.Parallel()
	w := do(t, newServer(t, func(o *httpserver.Options) {
		o.ConnectSources = []string{"wss://meet.example.com", "https://meet.example.com"}
	}), httptest.NewRequest(http.MethodGet, "/", nil))
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "connect-src 'self' wss://meet.example.com https://meet.example.com;") ||
		!strings.Contains(csp, "default-src 'self';") {
		t.Fatalf("csp = %q", csp)
	}
}

func TestClientIP(t *testing.T) {
	t.Parallel()
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	tests := []struct {
		name, remote, xff, want string
		trusted                 []netip.Prefix
	}{
		{"forged header ignored", "203.0.113.7:5000", "1.2.3.4", "203.0.113.7", nil},
		{"trusted proxy", "10.0.0.5:5000", "203.0.113.9", "203.0.113.9", trusted},
		{"right-most untrusted hop", "10.0.0.5:5000", "1.1.1.1, 203.0.113.9, 10.0.0.3", "203.0.113.9", trusted},
		{"untrusted peer with trusted config", "198.51.100.1:5000", "1.2.3.4", "198.51.100.1", trusted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := newServer(t, func(o *httpserver.Options) { o.TrustedProxies = tt.trusted })
			r := httptest.NewRequest(http.MethodGet, "/api/v1/ip", nil)
			r.RemoteAddr = tt.remote
			r.Header.Set("X-Forwarded-For", tt.xff)
			if got := do(t, s, r).Body.String(); got != tt.want {
				t.Fatalf("client IP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSPA(t *testing.T) {
	t.Parallel()
	s := newServer(t, func(o *httpserver.Options) {
		o.Themes = func(context.Context) (string, string) { return "classic-blue", "light" }
	})
	t.Run("deep link reload renders index with the theme", func(t *testing.T) {
		w := do(t, s, httptest.NewRequest(http.MethodGet, "/settings/appearance", nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `data-theme="classic-blue"`) ||
			!strings.Contains(w.Body.String(), "color-scheme: light") || w.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("got %d %q %v", w.Code, w.Body.String(), w.Header())
		}
	})
	t.Run("hashed asset is immutable", func(t *testing.T) {
		w := do(t, s, httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", nil))
		if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") ||
			!strings.Contains(w.Header().Get("Content-Type"), "javascript") {
			t.Fatalf("got %d %v", w.Code, w.Header())
		}
	})
	t.Run("missing file is 404, not index", func(t *testing.T) {
		if w := do(t, s, httptest.NewRequest(http.MethodGet, "/assets/nope.js", nil)); w.Code != 404 {
			t.Fatalf("got %d", w.Code)
		}
	})
	t.Run("frontend not built", func(t *testing.T) {
		empty := newServer(t, func(o *httpserver.Options) { o.Dist = fstest.MapFS{".gitkeep": {}} })
		w := do(t, empty, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "not built") {
			t.Fatalf("got %d %q", w.Code, w.Body.String())
		}
	})
}

func TestHealthAndReady(t *testing.T) {
	t.Parallel()
	failing := errors.New("migrations pending")
	s := newServer(t, func(o *httpserver.Options) {
		o.ReadyChecks = []httpserver.Check{
			{Name: "database", Check: func(context.Context) error { return nil }},
			{Name: "redis", Check: func(context.Context) error { return failing }},
		}
	})
	if w := do(t, s, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)); w.Code != 200 {
		t.Fatalf("health = %d", w.Code)
	}
	w := do(t, s, httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil))
	var body struct {
		Status string
		Checks map[string]string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 503 || body.Checks["redis"] != "migrations pending" || body.Checks["database"] != "ok" {
		t.Fatalf("ready = %d %+v", w.Code, body)
	}
	ok := newServer(t, func(o *httpserver.Options) {
		o.ReadyChecks = []httpserver.Check{{Name: "database", Check: func(context.Context) error { return nil }}}
	})
	if w := do(t, ok, httptest.NewRequest(http.MethodGet, "/api/v1/ready", nil)); w.Code != 200 {
		t.Fatalf("ready = %d", w.Code)
	}
}

func TestRequestLogHasNoSecrets(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	s := newServer(t, func(o *httpserver.Options) { o.Logger = slog.New(slog.NewJSONHandler(&buf, nil)) })
	r := jsonReq(`{"password":"hunter2-secret"}`)
	r.URL.RawQuery = "token=abc-secret"
	r.Header.Set("Cookie", "meethub_session=cookie-secret")
	do(t, s, r)
	line := buf.String()
	if !strings.Contains(line, `"path":"/api/v1/echo"`) || !strings.Contains(line, `"status":200`) {
		t.Fatalf("log line: %q", line)
	}
	for _, secret := range []string{"hunter2-secret", "abc-secret", "cookie-secret"} {
		if strings.Contains(line, secret) {
			t.Fatalf("log contains %q: %q", secret, line)
		}
	}
}

func TestGracefulShutdown(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	s := httpserver.New(httpserver.Options{Addr: "127.0.0.1:0", ShutdownTimeout: 5 * time.Second})
	s.API().GET("/slow", func(c *echo.Context) error {
		close(started)
		time.Sleep(300 * time.Millisecond)
		return c.String(http.StatusOK, "done")
	})
	ctx, cancel := context.WithCancel(context.Background())
	addrCh := make(chan net.Addr, 1)
	runErr := make(chan error, 1)
	go func() { runErr <- s.Run(ctx, func(a net.Addr) { addrCh <- a }) }()
	addr := <-addrCh

	respCh := make(chan string, 1)
	go func() {
		resp, err := http.Get("http://" + addr.String() + "/api/v1/slow") //nolint:noctx // test client
		if err != nil {
			respCh <- "error: " + err.Error()
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		respCh <- string(b)
	}()
	<-started
	cancel() // SIGTERM equivalent while the request is in flight

	if got := <-respCh; got != "done" {
		t.Fatalf("in-flight request: %q", got)
	}
	if err := <-runErr; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := net.DialTimeout("tcp", addr.String(), 200*time.Millisecond); err == nil {
		t.Fatal("server still accepts connections after shutdown")
	}
}
