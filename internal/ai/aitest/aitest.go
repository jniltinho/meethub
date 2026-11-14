// Package aitest is a fake OpenAI-compatible server for tests: nothing in the test suites talks to a real AI
// service.
package aitest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Answer is a status and a body to answer with.
type Answer struct {
	Status int
	Body   string
}

// Call is a request the fake received.
type Call struct {
	Path    string
	Auth    string
	Fields  map[string]string // multipart fields of a transcription
	Audio   []byte            // its file
	System  string            // a chat's instructions
	User    string            // a chat's content
	MaxToks int
}

// Server is the fake. Transcribe and Chat decide each answer (nil: a timed segment, a summary).
type Server struct {
	*httptest.Server
	Transcribe func(c Call) Answer
	Chat       func(c Call) Answer

	mu    sync.Mutex
	calls []Call
}

// New starts a fake; it is closed when the test ends through t.Cleanup by the caller (Close).
func New() *Server {
	s := &Server{}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// URL is the base URL to configure (…/v1).
func (s *Server) URL() string { return s.Server.URL + "/v1" }

// Calls returns the requests received so far.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// Count is the number of requests received on a path suffix ("/audio/transcriptions", "/chat/completions").
func (s *Server) Count(suffix string) int {
	n := 0
	for _, c := range s.Calls() {
		if strings.HasSuffix(c.Path, suffix) {
			n++
		}
	}
	return n
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	c := Call{Path: r.URL.Path, Auth: r.Header.Get("Authorization")}
	var a Answer
	switch {
	case strings.HasSuffix(r.URL.Path, "/audio/transcriptions"):
		if err := r.ParseMultipartForm(64 << 20); err == nil {
			c.Fields = map[string]string{}
			for k, v := range r.MultipartForm.Value {
				c.Fields[k] = v[0]
			}
			if f, _, err := r.FormFile("file"); err == nil {
				c.Audio, _ = io.ReadAll(f)
				_ = f.Close()
			}
		}
		s.record(c)
		if s.Transcribe != nil {
			a = s.Transcribe(c)
		} else {
			a = Segments([3]any{0.5, 2.0, "Hello there."})
		}
	case strings.HasSuffix(r.URL.Path, "/chat/completions"):
		var body struct {
			MaxTokens int `json:"max_tokens"`
			Messages  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		c.MaxToks = body.MaxTokens
		for _, m := range body.Messages {
			if m.Role == "system" {
				c.System = m.Content
			} else {
				c.User += m.Content
			}
		}
		s.record(c)
		if s.Chat != nil {
			a = s.Chat(c)
		} else {
			a = Completion("## Summary\nThey talked.", "stop")
		}
	default:
		a = Answer{Status: http.StatusNotFound, Body: `{"error":"not found"}`}
	}
	if a.Status == 0 {
		a.Status = http.StatusOK
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(a.Status)
	_, _ = io.WriteString(w, a.Body)
}

func (s *Server) record(c Call) {
	s.mu.Lock()
	s.calls = append(s.calls, c)
	s.mu.Unlock()
}

// Segments is a verbose_json answer with segments of {start, end, text}.
func Segments(segs ...[3]any) Answer {
	type seg struct {
		Start any    `json:"start"`
		End   any    `json:"end"`
		Text  string `json:"text"`
	}
	out := struct {
		Text     string `json:"text"`
		Segments []seg  `json:"segments"`
	}{Segments: []seg{}}
	for _, s := range segs {
		out.Segments = append(out.Segments, seg{s[0], s[1], fmt.Sprint(s[2])})
		out.Text += fmt.Sprint(s[2])
	}
	b, _ := json.Marshal(out)
	return Answer{Body: string(b)}
}

// Completion is a chat answer with content and a finish reason.
func Completion(content, finish string) Answer {
	b, _ := json.Marshal(map[string]any{"choices": []map[string]any{{
		"finish_reason": finish, "message": map[string]any{"role": "assistant", "content": content},
	}}})
	return Answer{Body: string(b)}
}
