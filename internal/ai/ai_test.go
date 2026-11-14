package ai_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/ai"
	"github.com/jniltinho/meethub/internal/ai/aitest"
)

const echoed = "Ana said the launch is on Friday"

func TestCalls(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		transcribe func(aitest.Call) aitest.Answer
		chat       func(aitest.Call) aitest.Answer
		class      string // "": success
	}{
		{name: "success"},
		{name: "wrong key", class: ai.ClassUnauthorized,
			transcribe: func(aitest.Call) aitest.Answer { return aitest.Answer{Status: 401, Body: `{"error":"bad key"}`} },
			chat:       func(aitest.Call) aitest.Answer { return aitest.Answer{Status: 403, Body: `{"error":"bad key"}`} }},
		{name: "rate limited", class: ai.ClassUnavailable,
			transcribe: func(aitest.Call) aitest.Answer { return aitest.Answer{Status: 429} },
			chat:       func(aitest.Call) aitest.Answer { return aitest.Answer{Status: 429} }},
		{name: "server error", class: ai.ClassUnavailable,
			transcribe: func(aitest.Call) aitest.Answer { return aitest.Answer{Status: 503} },
			chat:       func(aitest.Call) aitest.Answer { return aitest.Answer{Status: 500} }},
		{name: "unknown model", class: ai.ClassUnsupported,
			transcribe: func(aitest.Call) aitest.Answer { return aitest.Answer{Status: 404} },
			chat:       func(aitest.Call) aitest.Answer { return aitest.Answer{Status: 404} }},
		{name: "error echoing the input", class: ai.ClassRejected,
			transcribe: func(aitest.Call) aitest.Answer {
				return aitest.Answer{Status: 422, Body: `{"error":"` + echoed + `"}`}
			},
			chat: func(aitest.Call) aitest.Answer { return aitest.Answer{Status: 400, Body: `{"error":"` + echoed + `"}`} }},
		{name: "too long", class: ai.ClassTooLong,
			transcribe: func(aitest.Call) aitest.Answer { return aitest.Answer{Status: 413} },
			chat: func(aitest.Call) aitest.Answer {
				return aitest.Answer{Status: 400, Body: `{"error":"prompt exceeds the context window: ` + echoed + `"}`}
			}},
		{name: "oversized answer", class: ai.ClassRejected,
			transcribe: func(aitest.Call) aitest.Answer { return aitest.Answer{Body: strings.Repeat("x", ai.MaxAnswer+10)} },
			chat:       func(aitest.Call) aitest.Answer { return aitest.Answer{Body: strings.Repeat("x", ai.MaxAnswer+10)} }},
		{name: "an error in a 200", class: ai.ClassRejected,
			transcribe: func(aitest.Call) aitest.Answer { return aitest.Answer{Body: `{"error":"failed to read audio data"}`} },
			chat:       func(aitest.Call) aitest.Answer { return aitest.Answer{Body: `{"choices":[]}`} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := aitest.New()
			defer f.Close()
			f.Transcribe, f.Chat = tt.transcribe, tt.chat
			c := &ai.Client{BaseURL: f.URL(), Key: "test-key", Timeout: 5 * time.Second}
			segs, terr := c.Transcribe(context.Background(), ai.TranscribeRequest{Model: "m", Name: "a.ogg", Audio: []byte("OggS")})
			text, cerr := c.Chat(context.Background(), ai.ChatRequest{Model: "m", System: "s", User: echoed, MaxTokens: 100})
			for _, err := range []error{terr, cerr} {
				if tt.class == "" {
					if err != nil {
						t.Fatalf("error: %v", err)
					}
					continue
				}
				if ai.ClassOf(err) != tt.class {
					t.Fatalf("class = %q (%v), want %q", ai.ClassOf(err), err, tt.class)
				}
				if strings.Contains(err.Error(), "Ana") || strings.Contains(err.Error(), "test-key") {
					t.Fatalf("the error carries content: %v", err)
				}
			}
			if tt.class == "" && (len(segs) != 1 || segs[0].End != 2 || text == "") {
				t.Fatalf("answers: %+v %q", segs, text)
			}
		})
	}
}

func TestTimingAndTruncation(t *testing.T) {
	t.Parallel()
	f := aitest.New()
	defer f.Close()
	c := &ai.Client{BaseURL: f.URL(), Key: "k"}
	f.Transcribe = func(aitest.Call) aitest.Answer { return aitest.Answer{Body: `{"text":"no timing"}`} }
	if _, err := c.Transcribe(context.Background(), ai.TranscribeRequest{Audio: []byte("x")}); ai.ClassOf(err) != ai.ClassUnsupported {
		t.Fatalf("untimed: %v", err)
	}
	f.Transcribe = func(aitest.Call) aitest.Answer { return aitest.Answer{Body: `{"segments":[{"text":"a"}]}`} }
	if _, err := c.Transcribe(context.Background(), ai.TranscribeRequest{Audio: []byte("x")}); ai.ClassOf(err) != ai.ClassUnsupported {
		t.Fatalf("segments without times: %v", err)
	}
	f.Transcribe = func(aitest.Call) aitest.Answer { return aitest.Segments() }
	if segs, err := c.Transcribe(context.Background(), ai.TranscribeRequest{Audio: []byte("x"), Language: "pt"}); err != nil || len(segs) != 0 {
		t.Fatalf("no speech: %v %v", segs, err)
	}
	if got := f.Calls()[2].Fields; got["language"] != "pt" || got["response_format"] != "verbose_json" {
		t.Fatalf("fields: %v", got)
	}
	f.Chat = func(aitest.Call) aitest.Answer { return aitest.Completion("## Summary\npartial", "length") }
	if _, err := c.Chat(context.Background(), ai.ChatRequest{}); !errors.Is(err, ai.ErrTruncated) {
		t.Fatalf("truncated: %v", err)
	}
	f.Chat = func(aitest.Call) aitest.Answer {
		return aitest.Answer{Body: `{"choices":[{"finish_reason":"stop","message":{"content":null}}]}`}
	}
	if _, err := c.Chat(context.Background(), ai.ChatRequest{}); !errors.Is(err, ai.ErrEmpty) {
		t.Fatalf("empty: %v", err)
	}
}

func TestNoRedirectAndTimeout(t *testing.T) {
	t.Parallel()
	other := aitest.New() // where the redirect points: it must never be reached
	defer other.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.Server.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	c := &ai.Client{BaseURL: redirect.URL + "/v1", Key: "k"}
	if _, err := c.Chat(context.Background(), ai.ChatRequest{}); ai.ClassOf(err) != ai.ClassUnavailable {
		t.Fatalf("redirect: %v", err)
	}
	if len(other.Calls()) != 0 {
		t.Fatal("the redirect was followed")
	}

	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer slow.Close()
	defer close(release)
	c = &ai.Client{BaseURL: slow.URL, Key: "k", Timeout: 200 * time.Millisecond}
	start := time.Now()
	if _, err := c.Chat(context.Background(), ai.ChatRequest{}); ai.ClassOf(err) != ai.ClassUnavailable || time.Since(start) > 3*time.Second {
		t.Fatalf("slow: %v after %s", err, time.Since(start))
	}
	// stopped by the caller: the caller's cause, not a service failure
	ctx, cancel := context.WithCancelCause(context.Background())
	stop := errors.New("switched off")
	time.AfterFunc(100*time.Millisecond, func() { cancel(stop) })
	c.Timeout = 0
	if _, err := c.Chat(ctx, ai.ChatRequest{}); !errors.Is(err, stop) {
		t.Fatalf("cancelled: %v", err)
	}
}
