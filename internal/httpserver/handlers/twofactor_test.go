package handlers_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // RFC 6238
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/testenv"
)

// totp is what an authenticator app shows for secret now.
func totp(secret string, now time.Time) string {
	key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(now.Unix()/30)) //nolint:gosec // positive
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1_000_000)
}

func TestTwoFactorAPI(t *testing.T) {
	e := newEnv(t, dbtest.Open(t, "sqlite"), false)
	ana := e.setupOwner()

	// without MEETHUB_SECRET_KEY the routes answer 503, everything else works
	if r := ana.do("GET", "/me/two-factor", nil); r.Code != 503 || r.code() != "two_factor_unavailable" {
		t.Fatalf("no key: %d %s", r.Code, r.Body.String())
	}
	rdb := redis.NewClient(&redis.Options{Addr: testenv.RedisAddr(t)})
	t.Cleanup(func() { _ = rdb.Close() })
	clock := time.Now() // the sessions of setup were created now
	e.api.Auth.Redis, e.api.Auth.SecretKey = rdb, bytes.Repeat([]byte{0x42}, 32)
	e.api.Auth.Now = func() time.Time { return clock }

	if r := ana.do("GET", "/me/two-factor", nil); r.Code != 200 || r.body["enabled"] != false {
		t.Fatalf("status: %d %s", r.Code, r.Body.String())
	}
	r := ana.do("POST", "/me/two-factor", map[string]any{})
	secret, _ := r.body["secret"].(string)
	if r.Code != 200 || secret == "" || r.body["qr_code"] == nil {
		t.Fatalf("begin: %d %s", r.Code, r.Body.String())
	}
	if r := ana.do("POST", "/me/two-factor/confirm", map[string]string{"code": "000000"}); r.Code != 400 || r.code() != "invalid_code" {
		t.Fatalf("wrong confirmation: %d %s", r.Code, r.Body.String())
	}
	r = ana.do("POST", "/me/two-factor/confirm", map[string]string{"code": totp(secret, clock)})
	if codes, _ := r.body["recovery_codes"].([]any); r.Code != 200 || len(codes) != 10 {
		t.Fatalf("confirm: %d %s", r.Code, r.Body.String())
	}
	clock = clock.Add(30 * time.Second)
	if r := ana.do("GET", "/admin/users", nil); !strings.Contains(r.Body.String(), `"two_factor":true`) {
		t.Fatalf("the user list does not show two-factor: %s", r.Body.String())
	}

	// the password answers a ticket and no cookie; the code sets the cookie
	c := e.client()
	r = c.do("POST", "/auth/login", map[string]string{"email": "ana@acme.io", "password": "a long password!"})
	ticket, _ := r.body["ticket"].(string)
	if r.Code != 200 || r.body["two_factor_required"] != true || ticket == "" || c.cookie != nil || r.body["csrf_token"] != nil {
		t.Fatalf("first step: %d %s", r.Code, r.Body.String())
	}
	if r := c.do("POST", "/auth/login/two-factor", map[string]string{"ticket": ticket, "code": "999999"}); r.Code != 400 || r.code() != "invalid_code" || c.cookie != nil {
		t.Fatalf("wrong code: %d %s", r.Code, r.Body.String())
	}
	if r := c.do("POST", "/auth/login/two-factor", map[string]string{"ticket": ticket, "code": totp(secret, clock)}); r.Code != 200 || c.cookie == nil {
		t.Fatalf("second step: %d %s", r.Code, r.Body.String())
	}
	if r := c.do("POST", "/auth/login/two-factor", map[string]string{"ticket": ticket, "code": totp(secret, clock)}); r.Code != 400 || r.code() != "two_factor_expired" {
		t.Fatalf("ticket reused: %d %s", r.Code, r.Body.String())
	}

	// an admin resets Bruno's two-factor (owners only by owners), his sessions end
	brunoID := ana.createUser("bruno@acme.io", "admin")
	bruno := e.login("bruno@acme.io", "a long password!")
	if r := bruno.do("DELETE", "/admin/users/"+ana.mustID()+"/two-factor", nil); r.Code != 403 {
		t.Fatalf("admin resets an owner: %d", r.Code)
	}
	if r := ana.do("DELETE", "/admin/users/"+ana.mustID()+"/two-factor", nil); r.Code != 403 {
		t.Fatalf("own reset from the administration: %d", r.Code)
	}
	if r := ana.do("DELETE", "/admin/users/"+brunoID+"/two-factor", nil); r.Code != 204 {
		t.Fatalf("reset: %d %s", r.Code, r.Body.String())
	}
	if r := bruno.do("GET", "/me", nil); r.Code != 401 {
		t.Fatalf("Bruno's session survived the reset: %d", r.Code)
	}
}

func (c *client) mustID() string {
	c.e.t.Helper()
	r := c.do("GET", "/me", nil)
	user, _ := r.body["user"].(map[string]any)
	id, _ := user["id"].(string)
	if id == "" {
		c.e.t.Fatalf("me: %d %s", r.Code, r.Body.String())
	}
	return id
}
