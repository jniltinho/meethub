package meetings

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testSecret = "check-test-secret-0123456789abcdef0123456789"

func TestCheckAPIFailures(t *testing.T) {
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"unauthenticated","msg":"invalid token"}`))
	}))
	defer refusing.Close()
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	for _, tc := range []struct {
		name, url, want string
	}{
		{"wrong secret", refusing.URL, "the key was refused"},
		{"unreachable", closed.URL, "unreachable:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := Check(context.Background(), tc.url, "check-key", testSecret)
			if len(res) != 1 || res[0].OK || !strings.Contains(res[0].Detail, tc.want) {
				t.Fatalf("got %+v, want one failed result with %q", res, tc.want)
			}
			if strings.Contains(res[0].Detail, testSecret) || strings.Contains(res[0].Target, testSecret) {
				t.Fatal("the secret was printed")
			}
		})
	}
}

func TestTurnAddr(t *testing.T) {
	for _, tc := range []struct {
		url, api, want string
		moved          bool
	}{
		{"turn:203.0.113.5:3478?transport=udp", "http://livekit:7880", "203.0.113.5:3478", false},
		{"turn:127.0.0.1:3478?transport=udp", "http://livekit:7880", "livekit:3478", true},
		{"turn:127.0.0.1:3478?transport=udp", "http://127.0.0.1:7880", "127.0.0.1:3478", false},
		{"turns:turn.example.com:443?transport=tcp", "http://livekit:7880", "turn.example.com:443", false},
	} {
		got, moved, err := turnAddr(tc.url, tc.api)
		if err != nil || got != tc.want || moved != tc.moved {
			t.Errorf("turnAddr(%q, %q) = %q, %v, %v; want %q, %v", tc.url, tc.api, got, moved, err, tc.want, tc.moved)
		}
	}
	if _, _, err := turnAddr("turn:nohost", "http://livekit:7880"); err == nil {
		t.Error("a URL without a port was accepted")
	}
}
