package handlers_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	"github.com/redis/go-redis/v9"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/testenv"
)

// testProvider is an OpenID Connect provider in the test: discovery and keys from oidctest, a token endpoint that
// checks the PKCE verifier and answers the ID token the test sets.
type testProvider struct {
	srv  *httptest.Server
	priv *rsa.PrivateKey

	mu        sync.Mutex
	challenge string         // code_challenge of the last authorization request
	claims    map[string]any // the next ID token's claims
}

func newTestProvider(t *testing.T) *testProvider {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &testProvider{priv: priv}
	keys := &oidctest.Server{PublicKeys: []oidctest.PublicKey{{PublicKey: priv.Public(), KeyID: "k1", Algorithm: oidc.RS256}}}
	p.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			keys.ServeHTTP(w, r)
			return
		}
		_ = r.ParseForm()
		p.mu.Lock()
		defer p.mu.Unlock()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != p.challenge || r.PostForm.Get("code") != "the-code" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		b, _ := json.Marshal(p.claims)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "throwaway", "token_type": "Bearer", "expires_in": 60,
			"id_token": oidctest.SignIDToken(priv, "k1", oidc.RS256, string(b)),
		})
	}))
	keys.SetIssuer(p.srv.URL)
	t.Cleanup(p.srv.Close)
	return p
}

// claimsFor are valid claims for the authorization request at authURL; mutate changes them.
func (p *testProvider) claimsFor(t *testing.T, authURL string, mutate func(map[string]any)) {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("state") == "" || q.Get("nonce") == "" {
		t.Fatalf("authorization request without PKCE, state or nonce: %s", authURL)
	}
	c := map[string]any{
		"iss": p.srv.URL, "aud": "meethub", "sub": "sub-ana", "nonce": q.Get("nonce"),
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"email": "ana@acme.io", "email_verified": true, "name": "Ana from the provider",
	}
	if mutate != nil {
		mutate(c)
	}
	p.mu.Lock()
	p.challenge, p.claims = q.Get("code_challenge"), c
	p.mu.Unlock()
}

type ssoEnv struct {
	*env
	idp  *testProvider
	oidc *auth.OIDC
}

func newSSOEnv(t *testing.T) *ssoEnv {
	t.Helper()
	e := newEnv(t, dbtest.Open(t, "sqlite"), false)
	rdb := redis.NewClient(&redis.Options{Addr: testenv.RedisAddr(t)})
	t.Cleanup(func() { _ = rdb.Close() })
	e.api.Auth.Redis = rdb
	idp := newTestProvider(t)
	o := &auth.OIDC{
		Config: auth.OIDCConfig{
			Issuer: idp.srv.URL, ClientID: "meethub", ClientSecret: "throwaway",
			RedirectURL: origin + "/api/v1/auth/oidc/callback", Scopes: []string{"openid", "email"}, DefaultRole: "member",
		},
		Auth: e.api.Auth, Users: e.api.Users, HTTPClient: idp.srv.Client(),
	}
	e.api.OIDC, e.api.OIDCName = o, "Acme ID"
	e.setupOwner()
	return &ssoEnv{env: e, idp: idp, oidc: o}
}

// signIn runs start and callback in one browser; it returns the callback's answer.
func (s *ssoEnv) signIn(t *testing.T, c *client, mutate func(map[string]any), tamper func(state string) string) resp {
	t.Helper()
	r := c.do("GET", "/auth/oidc/start", nil)
	if r.Code != http.StatusFound {
		t.Fatalf("start: %d %s", r.Code, r.Body.String())
	}
	loc := r.Header().Get("Location")
	var state string
	for _, ck := range r.Result().Cookies() {
		if ck.Name == "meethub_oidc" {
			state = ck.Value
		}
	}
	if state == "" || !strings.HasPrefix(loc, s.idp.srv.URL+"/auth?") {
		t.Fatalf("start: no state cookie or wrong redirect %q", loc)
	}
	s.idp.claimsFor(t, loc, mutate)
	sent := state
	if tamper != nil {
		sent = tamper(state)
	}
	return c.do("GET", "/auth/oidc/callback?code=the-code&state="+url.QueryEscape(sent), nil, func(req *http.Request) {
		req.AddCookie(&http.Cookie{Name: "meethub_oidc", Value: state})
	})
}

func failed(r resp) bool {
	return r.Code == http.StatusFound && r.Header().Get("Location") == "/login?sso=failed"
}

func TestOIDCLogin(t *testing.T) {
	s := newSSOEnv(t)

	// the bootstrap offers both ways
	b := s.client().do("GET", "/bootstrap", nil)
	if sso, _ := b.body["sso"].(map[string]any); b.body["password_login"] != true || sso["name"] != "Acme ID" {
		t.Fatalf("bootstrap: %s", b.Body.String())
	}

	// Ana exists with the e-mail the provider asserts as verified
	c := s.client()
	r := s.signIn(t, c, nil, nil)
	if r.Code != http.StatusFound || r.Header().Get("Location") != "/" || c.cookie == nil {
		t.Fatalf("sign in: %d %v", r.Code, r.Header())
	}
	if me := c.do("GET", "/me", nil); me.Code != 200 {
		t.Fatalf("the new session: %d", me.Code)
	}
	ev, _ := audit.List(t.Context(), s.db.Gorm, audit.Filter{Action: audit.ActionOIDCLogin})
	if len(ev.Events) != 1 || ev.Events[0].Details["email"] != "ana@acme.io" {
		t.Fatalf("audit: %+v", ev.Events)
	}

	for name, tc := range map[string]struct {
		mutate func(map[string]any)
		tamper func(string) string
	}{
		"unknown state":      {tamper: func(string) string { return "not-a-state" }},
		"wrong audience":     {mutate: func(c map[string]any) { c["aud"] = "another-client" }},
		"expired token":      {mutate: func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() }},
		"wrong nonce":        {mutate: func(c map[string]any) { c["nonce"] = "another-nonce" }},
		"another issuer":     {mutate: func(c map[string]any) { c["iss"] = "https://evil.example" }},
		"unverified e-mail":  {mutate: func(c map[string]any) { c["email_verified"] = false }},
		"no email_verified":  {mutate: func(c map[string]any) { delete(c, "email_verified") }},
		"subject changed":    {mutate: func(c map[string]any) { c["sub"] = "someone-else" }},
		"unknown, no create": {mutate: func(c map[string]any) { c["email"], c["sub"] = "new@example.com", "sub-new" }},
	} {
		t.Run(name, func(t *testing.T) {
			bc := s.client()
			if r := s.signIn(t, bc, tc.mutate, tc.tamper); !failed(r) || bc.cookie != nil {
				t.Fatalf("not refused: %d %v", r.Code, r.Header())
			}
		})
	}
	var users int64
	s.db.Gorm.Raw(`SELECT COUNT(*) FROM users WHERE email_normalized = 'new@example.com'`).Scan(&users)
	if users != 0 {
		t.Fatal("an account was created with auto_create off")
	}
	refused, _ := audit.List(t.Context(), s.db.Gorm, audit.Filter{Action: audit.ActionOIDCRefused})
	if len(refused.Events) < 9 {
		t.Fatalf("refusals audited: %d", len(refused.Events))
	}
	for _, e := range refused.Events {
		for _, v := range e.Details {
			if strings.Contains(strconv.Quote(v.(string)), "eyJ") { // a JWT
				t.Fatal("a token in the audit log")
			}
		}
	}
}

func TestOIDCReplayedCallback(t *testing.T) {
	s := newSSOEnv(t)
	c := s.client()
	r := c.do("GET", "/auth/oidc/start", nil)
	var state string
	for _, ck := range r.Result().Cookies() {
		if ck.Name == "meethub_oidc" {
			state = ck.Value
		}
	}
	s.idp.claimsFor(t, r.Header().Get("Location"), nil)
	call := func(cookie string) resp {
		return s.client().do("GET", "/auth/oidc/callback?code=the-code&state="+url.QueryEscape(state), nil, func(req *http.Request) {
			req.AddCookie(&http.Cookie{Name: "meethub_oidc", Value: cookie})
		})
	}
	if r := call("another-browser"); !failed(r) { // a callback in another browser (login CSRF)
		t.Fatalf("state of another browser: %d", r.Code)
	}
	if r := call(state); r.Header().Get("Location") != "/" {
		t.Fatalf("first use: %d %v", r.Code, r.Header())
	}
	if r := call(state); !failed(r) {
		t.Fatalf("reused state: %d", r.Code)
	}
}

func TestOIDCProvisioning(t *testing.T) {
	s := newSSOEnv(t)
	s.oidc.Config.AutoCreate, s.oidc.Config.AllowedDomains = true, []string{"example.com"}
	newUser := func(email, sub string) func(map[string]any) {
		return func(c map[string]any) { c["email"], c["sub"] = email, sub }
	}
	if r := s.signIn(t, s.client(), newUser("someone@other.org", "sub-other"), nil); !failed(r) {
		t.Fatal("a domain not allowed got an account")
	}
	c := s.client()
	if r := s.signIn(t, c, newUser("new@example.com", "sub-new"), nil); r.Header().Get("Location") != "/" {
		t.Fatalf("auto-create: %d %v", r.Code, r.Header())
	}
	me := c.do("GET", "/me", nil)
	if me.body["role"] != "member" {
		t.Fatalf("role: %s", me.Body.String())
	}
	created, _ := audit.List(t.Context(), s.db.Gorm, audit.Filter{Action: audit.ActionUserCreated})
	var bySSO int
	for _, e := range created.Events {
		if e.Details["via"] == "oidc" {
			bySSO++
		}
	}
	if bySSO != 1 {
		t.Fatalf("user.created by single sign-on: %d", bySSO)
	}
	// the stored subject must match afterwards
	if r := s.signIn(t, s.client(), newUser("new@example.com", "sub-recycled"), nil); !failed(r) {
		t.Fatal("another subject entered the account")
	}
	// a disabled user is refused
	if err := s.db.Gorm.Exec(`UPDATE users SET status = 'disabled' WHERE email_normalized = 'new@example.com'`).Error; err != nil {
		t.Fatal(err)
	}
	if r := s.signIn(t, s.client(), newUser("new@example.com", "sub-new"), nil); !failed(r) {
		t.Fatal("a disabled user signed in")
	}
}

func TestPasswordLoginOff(t *testing.T) {
	s := newSSOEnv(t)
	s.api.NoPasswordLogin = true
	r := s.client().do("POST", "/auth/login", map[string]string{"email": "ana@acme.io", "password": "a long password!"})
	if r.Code != http.StatusForbidden || r.code() != "password_login_disabled" || r.Header().Get("Set-Cookie") != "" {
		t.Fatalf("password login: %d %s", r.Code, r.Body.String())
	}
	if b := s.client().do("GET", "/bootstrap", nil); b.body["password_login"] != false {
		t.Fatalf("bootstrap: %s", b.Body.String())
	}
	if r := s.signIn(t, s.client(), nil, nil); r.Header().Get("Location") != "/" {
		t.Fatal("single sign-on must still work")
	}
}

func TestOIDCOff(t *testing.T) {
	e := newEnv(t, dbtest.Open(t, "sqlite"), false)
	if r := e.client().do("GET", "/auth/oidc/start", nil); r.Code != http.StatusNotFound {
		t.Fatalf("start without a provider: %d", r.Code)
	}
	if b := e.client().do("GET", "/bootstrap", nil); b.body["sso"] != nil || b.body["password_login"] != true {
		t.Fatalf("bootstrap: %s", b.Body.String())
	}
}
