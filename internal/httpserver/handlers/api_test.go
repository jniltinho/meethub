package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/httpserver"
	"github.com/jniltinho/meethub/internal/httpserver/handlers"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/ratelimit"
	"github.com/jniltinho/meethub/internal/session"
)

const origin = "http://chat.test"

type env struct {
	t     *testing.T
	srv   *httpserver.Server
	api   *handlers.API
	db    *database.DB
	token string // setup token
}

func newEnv(t *testing.T, db *database.DB, secure bool) *env {
	t.Helper()
	hasher := identity.NewHasher(identity.Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1}, 8, time.Second)
	svc := &auth.Service{
		DB: db, Hasher: hasher, Limiter: ratelimit.NewLogins(nil, nil, 0), DataDir: t.TempDir(),
		Sessions: session.Store{Idle: time.Hour, Absolute: 24 * time.Hour},
	}
	token, _, err := svc.PrepareSetup(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	api := &handlers.API{Auth: svc, Users: &org.Users{DB: db, Hasher: hasher}, DB: db, PublicOrigin: origin, SecureCookies: secure}
	srv := httpserver.New(httpserver.Options{ErrorMappers: handlers.Mappers()})
	api.Register(srv.API())
	return &env{t: t, srv: srv, api: api, db: db, token: token}
}

// client is a browser: it keeps the session cookie and the CSRF token.
type client struct {
	e      *env
	cookie *http.Cookie
	csrf   string
	ip     string
}

type resp struct {
	*httptest.ResponseRecorder
	body map[string]any
}

func (r resp) code() string {
	if e, ok := r.body["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func (c *client) do(method, path string, body any, mutate ...func(*http.Request)) resp {
	c.e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	r := httptest.NewRequest(method, "/api/v1"+path, &buf)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", origin)
	if c.ip != "" {
		r.RemoteAddr = c.ip + ":1234"
	}
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	if c.csrf != "" {
		r.Header.Set("X-CSRF-Token", c.csrf)
	}
	for _, m := range mutate {
		m(r)
	}
	w := httptest.NewRecorder()
	c.e.srv.Handler().ServeHTTP(w, r)
	out := resp{ResponseRecorder: w}
	_ = json.Unmarshal(w.Body.Bytes(), &out.body)
	for _, ck := range w.Result().Cookies() {
		if ck.Name == c.e.api.CookieName() {
			if ck.MaxAge < 0 {
				c.cookie = nil
			} else {
				c.cookie = ck
			}
		}
	}
	if tok, ok := out.body["csrf_token"].(string); ok {
		c.csrf = tok
	}
	return out
}

func (e *env) client() *client { return &client{e: e} }

// setupOwner initializes the install and returns the logged-in owner.
func (e *env) setupOwner() *client {
	c := e.client()
	r := c.do("POST", "/auth/setup", map[string]string{
		"setup_token": e.token, "organization_name": "Acme", "display_name": "Ana",
		"email": "ana@acme.io", "password": "a long password!",
	})
	if r.Code != 201 {
		e.t.Fatalf("setup: %d %s", r.Code, r.Body.String())
	}
	return c
}

func (e *env) login(email, pw string) *client {
	c := e.client()
	if r := c.do("POST", "/auth/login", map[string]string{"email": email, "password": pw}); r.Code != 200 {
		e.t.Fatalf("login %s: %d %s", email, r.Code, r.Body.String())
	}
	return c
}

func (c *client) createUser(email, role string) string {
	c.e.t.Helper()
	r := c.do("POST", "/admin/users", map[string]string{"email": email, "display_name": email, "role": role, "password": "a long password!"})
	if r.Code != 201 {
		c.e.t.Fatalf("create %s: %d %s", email, r.Code, r.Body.String())
	}
	return r.body["id"].(string)
}

func TestSetupFlow(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		e := newEnv(t, db, false)
		anon := e.client()
		if r := anon.do("GET", "/bootstrap", nil); r.Code != 200 || r.body["setup_required"] != true || r.body["organization"] != nil {
			t.Fatalf("bootstrap: %s", r.Body.String())
		}
		if r := anon.do("POST", "/auth/setup", map[string]string{"setup_token": "nope"}); r.Code != 403 || r.code() != "invalid_setup_token" {
			t.Fatalf("bad token: %d %s", r.Code, r.Body.String())
		}
		owner := e.setupOwner()
		if owner.cookie == nil || owner.csrf == "" {
			t.Fatal("setup did not log the owner in")
		}
		if r := anon.do("POST", "/auth/setup", map[string]string{"setup_token": e.token}); r.Code != 409 || r.code() != "already_initialized" {
			t.Fatalf("second setup: %d %s", r.Code, r.Body.String())
		}
		if r := anon.do("GET", "/bootstrap", nil); r.body["setup_required"] != false {
			t.Fatal("bootstrap after setup")
		}
		me := owner.do("GET", "/me", nil)
		perms, _ := me.body["permissions"].([]any)
		if me.Code != 200 || me.body["role"] != "owner" || len(perms) != 6 {
			t.Fatalf("me: %s", me.Body.String())
		}
	})
}

func TestCookieAttributes(t *testing.T) {
	e := newEnv(t, dbtest.Open(t, "sqlite"), true)
	c := e.setupOwner()
	ck := c.cookie
	if ck.Name != "__Host-meethub_session" || !ck.Secure || !ck.HttpOnly || ck.SameSite != http.SameSiteLaxMode || ck.Path != "/" || ck.Domain != "" {
		t.Fatalf("cookie: %+v", ck)
	}
}

func TestLoginErrorsAndCSRF(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		e := newEnv(t, db, false)
		owner := e.setupOwner()

		unknown := e.client().do("POST", "/auth/login", map[string]string{"email": "who@acme.io", "password": "whatever pass 1"})
		wrong := e.client().do("POST", "/auth/login", map[string]string{"email": "ana@acme.io", "password": "whatever pass 1"})
		if unknown.Code != 401 || unknown.Body.String() != wrong.Body.String() || unknown.code() != "invalid_credentials" {
			t.Fatalf("login answers differ: %s / %s", unknown.Body.String(), wrong.Body.String())
		}
		cross := e.client().do("POST", "/auth/login", map[string]string{"email": "ana@acme.io", "password": "a long password!"},
			func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") })
		if cross.Code != 403 || cross.code() != "csrf_failed" {
			t.Fatalf("cross-origin login: %d %s", cross.Code, cross.Body.String())
		}
		noOrigin := e.client().do("POST", "/auth/login", map[string]string{}, func(r *http.Request) { r.Header.Del("Origin") })
		if noOrigin.Code != 403 {
			t.Fatalf("missing origin: %d", noOrigin.Code)
		}
		formPost := e.client().do("POST", "/auth/login", nil, func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") })
		if formPost.Code != 415 {
			t.Fatalf("form post: %d", formPost.Code)
		}
		saved := owner.csrf
		owner.csrf = ""
		if r := owner.do("PATCH", "/me", map[string]any{"display_name": "X"}); r.Code != 403 || r.code() != "csrf_failed" {
			t.Fatalf("missing csrf header: %d %s", r.Code, r.Body.String())
		}
		other := e.login("ana@acme.io", "a long password!")
		owner.csrf = other.csrf // a valid token, but of another session
		if r := owner.do("PATCH", "/me", map[string]any{"display_name": "X"}); r.Code != 403 {
			t.Fatalf("csrf of another session: %d", r.Code)
		}
		owner.csrf = saved
		if r := owner.do("PATCH", "/me", map[string]any{"display_name": "Ana S."}); r.Code != 200 {
			t.Fatalf("valid csrf: %d %s", r.Code, r.Body.String())
		}
		if r := e.client().do("GET", "/me", nil); r.Code != 401 || r.code() != "unauthenticated" {
			t.Fatalf("anonymous /me: %d %s", r.Code, r.Body.String())
		}
	})
}

func TestRateLimitedLogin(t *testing.T) {
	e := newEnv(t, dbtest.Open(t, "sqlite"), false)
	e.setupOwner()
	c := e.client()
	for range 5 {
		c.do("POST", "/auth/login", map[string]string{"email": "ana@acme.io", "password": "a wrong password"})
	}
	r := c.do("POST", "/auth/login", map[string]string{"email": "ana@acme.io", "password": "a long password!"})
	if r.Code != 429 || r.code() != "rate_limited" || r.Header().Get("Retry-After") == "" {
		t.Fatalf("6th attempt: %d %s %v", r.Code, r.Body.String(), r.Header())
	}
	if r := c.do("POST", "/auth/login", map[string]string{"email": "ana@acme.io", "password": strings.Repeat("x", 10_000)}); r.Code != 400 {
		t.Fatalf("oversized password: %d", r.Code)
	}
}

func TestAdminAPI(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		e := newEnv(t, db, false)
		owner := e.setupOwner()
		adminID := owner.createUser("adm@acme.io", "admin")
		memberID := owner.createUser("mem@acme.io", "member")
		admin := e.login("adm@acme.io", "a long password!")
		member := e.login("mem@acme.io", "a long password!")

		for _, path := range []string{"/admin/users", "/admin/audit"} {
			if r := member.do("GET", path, nil); r.Code != 403 || r.code() != "forbidden" {
				t.Fatalf("member %s: %d", path, r.Code)
			}
		}
		if r := member.do("POST", "/admin/users", map[string]string{"email": "x@acme.io"}); r.Code != 403 {
			t.Fatalf("member create: %d", r.Code)
		}
		if r := admin.do("POST", "/admin/users", map[string]string{"email": "o2@acme.io", "display_name": "O", "role": "owner", "password": "a long password!"}); r.Code != 403 {
			t.Fatalf("admin creating owner: %d", r.Code)
		}
		if r := admin.do("PATCH", "/admin/users/"+memberID, map[string]any{"email": "x@example.com"}); r.Code != 400 || r.code() != "invalid_input" {
			t.Fatalf("unknown field: %d %s", r.Code, r.Body.String())
		}
		if r := owner.do("PATCH", "/admin/users/"+adminID, map[string]any{"role": "member"}); r.Code != 200 || r.body["role"] != "member" {
			t.Fatalf("demote admin: %d %s", r.Code, r.Body.String())
		}
		// disabling ends the user's sessions
		if r := owner.do("PATCH", "/admin/users/"+memberID, map[string]any{"status": "disabled"}); r.Code != 200 {
			t.Fatalf("disable: %d %s", r.Code, r.Body.String())
		}
		if r := member.do("GET", "/me", nil); r.Code != 401 {
			t.Fatalf("disabled user's session: %d", r.Code)
		}
		ownerID := owner.do("GET", "/me", nil).body["user"].(map[string]any)["id"].(string)
		if r := owner.do("PATCH", "/admin/users/"+ownerID, map[string]any{"role": "admin"}); r.Code != 409 || r.code() != "last_owner" {
			t.Fatalf("last owner: %d %s", r.Code, r.Body.String())
		}
		list := owner.do("GET", "/admin/users?limit=2", nil)
		if users, _ := list.body["users"].([]any); list.Code != 200 || len(users) != 2 || list.body["next_cursor"] == nil {
			t.Fatalf("list: %s", list.Body.String())
		}
		ev := owner.do("GET", "/admin/audit?action="+audit.ActionRoleChanged, nil)
		if events, _ := ev.body["events"].([]any); ev.Code != 200 || len(events) != 1 {
			t.Fatalf("audit: %s", ev.Body.String())
		}
	})
}

func TestPreferencesAndPassword(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		e := newEnv(t, db, false)
		owner := e.setupOwner()
		if r := owner.do("PATCH", "/me", map[string]any{"preferences": map[string]string{"theme": "neon"}}); r.Code != 400 || r.code() != "invalid_theme" {
			t.Fatalf("invalid theme: %d %s", r.Code, r.Body.String())
		}
		if r := owner.do("PATCH", "/me", map[string]any{"username": "here"}); r.Code != 400 || r.code() != "invalid_username" {
			t.Fatalf("reserved username: %d %s", r.Code, r.Body.String())
		}
		if r := owner.do("PATCH", "/me", map[string]any{"username": "Ana.S"}); r.Code != 200 || r.body["user"].(map[string]any)["username"] != "ana.s" {
			t.Fatalf("set username: %d %s", r.Code, r.Body.String())
		}
		if r := owner.do("PATCH", "/me", map[string]any{"preferences": map[string]string{"language": "fr"}}); r.Code != 400 || r.code() != "invalid_language" {
			t.Fatalf("invalid language: %d %s", r.Code, r.Body.String())
		}
		r := owner.do("PATCH", "/me", map[string]any{"preferences": map[string]string{"theme": "classic-blue", "language": "es"}})
		prefs, _ := r.body["preferences"].(map[string]any)
		if r.Code != 200 || prefs["theme"] != "classic-blue" || prefs["language"] != "es" {
			t.Fatalf("preferences: %s", r.Body.String())
		}
		// the preference follows the user to another browser
		other := e.login("ana@acme.io", "a long password!")
		if p := other.do("GET", "/me", nil).body["preferences"].(map[string]any); p["theme"] != "classic-blue" {
			t.Fatalf("other browser: %v", p)
		}
		oldCookie := owner.cookie
		if r := owner.do("POST", "/me/password", map[string]string{"current_password": "a long password!", "new_password": "an even longer password"}); r.Code != 200 {
			t.Fatalf("change password: %d %s", r.Code, r.Body.String())
		}
		if owner.cookie == nil || owner.cookie.Value == oldCookie.Value {
			t.Fatal("session token not rotated")
		}
		if r := owner.do("GET", "/me", nil); r.Code != 200 {
			t.Fatalf("rotated session: %d", r.Code)
		}
		if r := other.do("GET", "/me", nil); r.Code != 401 {
			t.Fatalf("other session after password change: %d", r.Code)
		}
		if r := owner.do("POST", "/auth/logout", nil); r.Code != 204 || owner.cookie != nil {
			t.Fatalf("logout: %d", r.Code)
		}
	})
}
