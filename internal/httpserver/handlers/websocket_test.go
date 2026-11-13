package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/database/dbtest"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/realtime"
	"github.com/jniltinho/meethub/internal/session"
)

type liveSessions struct {
	db    *database.DB
	store session.Store
}

func (l liveSessions) Live(ctx context.Context, ids []string, now time.Time) (map[string]bool, error) {
	return l.store.Live(ctx, l.db.Gorm, ids, identity.StatusActive, now)
}

func (l liveSessions) Touch(ctx context.Context, id string, now time.Time) error {
	return l.store.TouchID(ctx, l.db.Gorm, id, now)
}

func dialWS(t *testing.T, url string, c *client, origin string) (*websocket.Conn, int) {
	t.Helper()
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	if c != nil && c.cookie != nil {
		h.Set("Cookie", c.cookie.Name+"="+c.cookie.Value)
	}
	ws, resp, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{HTTPHeader: h})
	code := 0
	if resp != nil {
		code = resp.StatusCode
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
	if err != nil {
		return nil, code
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	return ws, code
}

// TestWebSocketEndsWithTheSession: the upgrade needs a session and the app's
// origin; disabling the user closes the open socket at once (database check,
// no Redis involved).
func TestWebSocketEndsWithTheSession(t *testing.T) {
	dbtest.Each(t, func(t *testing.T, db *database.DB) {
		e := newEnv(t, db, false)
		hub := realtime.NewHub(realtime.Options{Version: "test", Sessions: liveSessions{db: db, store: e.api.Auth.Sessions}})
		e.api.Realtime = hub
		e.api.SessionsChanged = func(context.Context) { hub.Recheck() }
		rctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { _ = hub.RunRevalidation(rctx) }()
		srv := httptest.NewServer(e.srv.Handler())
		defer srv.Close()
		url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/websocket"

		owner := e.setupOwner()
		memberID := owner.createUser("mem@acme.io", "member")
		member := e.login("mem@acme.io", "a long password!")

		if _, code := dialWS(t, url, nil, origin); code != http.StatusUnauthorized {
			t.Fatalf("without a session: %d", code)
		}
		if _, code := dialWS(t, url, member, "http://evil.test"); code != http.StatusForbidden {
			t.Fatalf("foreign origin: %d", code)
		}
		ws, code := dialWS(t, url, member, origin)
		if ws == nil {
			t.Fatalf("upgrade: %d", code)
		}
		if _, msg, err := ws.Read(t.Context()); err != nil || !strings.Contains(string(msg), `"hello"`) {
			t.Fatalf("hello: %s %v", msg, err)
		}
		start := time.Now()
		if r := owner.do("PATCH", "/admin/users/"+memberID, map[string]any{"status": "disabled"}); r.Code != 200 {
			t.Fatalf("disable: %d %s", r.Code, r.Body.String())
		}
		rctx2, cancel2 := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel2()
		_, _, err := ws.Read(rctx2)
		if websocket.CloseStatus(err) != websocket.StatusPolicyViolation || time.Since(start) > 5*time.Second {
			t.Fatalf("socket of a disabled user: %v after %s", err, time.Since(start))
		}
	})
}
