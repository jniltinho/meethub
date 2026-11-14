// Package ai is a small client of an OpenAI-compatible API (add-meeting-summaries, design D7): timed speech to text
// (POST /audio/transcriptions) and chat completions (POST /chat/completions). It follows no redirect, caps every
// answer, and reduces a failure to a class and a status code: a provider's error body, which may repeat what was
// sent, never leaves this package.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Failure classes (the codes a recording shows: ai_<class>).
const (
	ClassUnauthorized = "unauthorized" // 401, 403: the key was refused
	ClassUnavailable  = "unavailable"  // unreachable, timed out, redirected, 429, 5xx: worth retrying
	ClassRejected     = "rejected"     // another 4xx, an error in a 200, an empty, cut or malformed answer
	ClassTooLong      = "too_long"     // the request does not fit the model
	ClassUnsupported  = "unsupported"  // 404, or an answer without the timing summaries need
)

// MaxAnswer is how much of an answer is read.
const MaxAnswer = 1 << 20

// Error is a failed call: its class and the status code (0 when there was no answer).
type Error struct {
	Class  string
	Status int
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("ai: %s (status %d)", e.Class, e.Status)
	}
	return "ai: " + e.Class
}

// Code is the error's code as the API and the recording show it.
func (e *Error) Code() string { return "ai_" + e.Class }

// Retryable reports whether trying again later may succeed.
func (e *Error) Retryable() bool { return e.Class == ClassUnavailable }

// ClassOf is err's class ("" when it is not an *Error).
func ClassOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Class
	}
	return ""
}

// Client calls one service.
type Client struct {
	BaseURL string // e.g. https://api.openai.com/v1
	Key     string
	Timeout time.Duration // per request
	HTTP    *http.Client  // nil: one that follows no redirect
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// do sends a request and returns the capped body of a 200; any other outcome is an *Error.
func (c *Client) do(ctx context.Context, path, contentType string, body []byte) ([]byte, error) {
	parent := ctx
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Class: ClassRejected}
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		if parent.Err() != nil {
			return nil, context.Cause(parent) // stopped by the caller: not the service's failure
		}
		return nil, &Error{Class: ClassUnavailable}
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxAnswer+1))
	if err != nil {
		return nil, &Error{Class: ClassUnavailable, Status: resp.StatusCode}
	}
	switch s := resp.StatusCode; {
	case s == http.StatusOK:
		if len(data) > MaxAnswer {
			return nil, &Error{Class: ClassRejected, Status: s}
		}
		return data, nil
	case s == http.StatusUnauthorized || s == http.StatusForbidden:
		return nil, &Error{Class: ClassUnauthorized, Status: s}
	case s == http.StatusNotFound:
		return nil, &Error{Class: ClassUnsupported, Status: s}
	case s == http.StatusRequestEntityTooLarge:
		return nil, &Error{Class: ClassTooLong, Status: s}
	case s == http.StatusTooManyRequests || s >= 500 || (s >= 300 && s < 400):
		return nil, &Error{Class: ClassUnavailable, Status: s}
	case s == http.StatusBadRequest && lengthComplaint(data):
		return nil, &Error{Class: ClassTooLong, Status: s}
	default:
		return nil, &Error{Class: ClassRejected, Status: s}
	}
}

// lengthComplaint reports whether an error body speaks of the request's length (the body itself is never kept).
func lengthComplaint(body []byte) bool {
	b := bytes.ToLower(body)
	for _, w := range []string{"context", "too long", "maximum length", "max_tokens", "token limit", "too many tokens"} {
		if bytes.Contains(b, []byte(w)) {
			return true
		}
	}
	return false
}

// Segment is a timed passage of a transcription, in seconds from the start of the audio.
type Segment struct {
	Start float64
	End   float64
	Text  string
}

// TranscribeRequest is one piece of audio to transcribe.
type TranscribeRequest struct {
	Model    string
	Language string // "": the service detects it
	Name     string // the file name sent, e.g. piece.ogg
	Audio    []byte
}

// Transcribe asks for timed segments (verbose_json). An answer without a segments list, or with an error member,
// is refused: ClassUnsupported for the first, ClassRejected for the second.
func (c *Client) Transcribe(ctx context.Context, in TranscribeRequest) ([]Segment, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fields := [][2]string{{"model", in.Model}, {"response_format", "verbose_json"}, {"timestamp_granularities[]", "segment"}}
	if in.Language != "" {
		fields = append(fields, [2]string{"language", in.Language})
	}
	for _, f := range fields {
		if err := w.WriteField(f[0], f[1]); err != nil {
			return nil, &Error{Class: ClassRejected}
		}
	}
	fw, err := w.CreateFormFile("file", in.Name)
	if err == nil {
		_, err = fw.Write(in.Audio)
	}
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		return nil, &Error{Class: ClassRejected}
	}
	data, err := c.do(ctx, "/audio/transcriptions", w.FormDataContentType(), buf.Bytes())
	if err != nil {
		return nil, err
	}
	return parseTranscription(data)
}

func parseTranscription(data []byte) ([]Segment, error) {
	var out struct {
		Error    json.RawMessage `json:"error"`
		Segments *[]struct {
			Start *float64 `json:"start"`
			End   *float64 `json:"end"`
			Text  string   `json:"text"`
		} `json:"segments"`
	}
	if json.Unmarshal(data, &out) != nil {
		return nil, &Error{Class: ClassRejected, Status: http.StatusOK}
	}
	if len(out.Error) > 0 && string(out.Error) != "null" { // whisper.cpp answers its errors with a 200 (design D3)
		return nil, &Error{Class: ClassRejected, Status: http.StatusOK}
	}
	if out.Segments == nil {
		return nil, &Error{Class: ClassUnsupported, Status: http.StatusOK}
	}
	segs := make([]Segment, 0, len(*out.Segments))
	for _, s := range *out.Segments {
		if s.Start == nil || s.End == nil || *s.End < *s.Start || *s.Start < 0 {
			return nil, &Error{Class: ClassUnsupported, Status: http.StatusOK}
		}
		segs = append(segs, Segment{Start: *s.Start, End: *s.End, Text: s.Text})
	}
	return segs, nil
}

// ChatRequest is one completion: fixed instructions and the content they apply to.
type ChatRequest struct {
	Model     string
	System    string
	User      string
	MaxTokens int
}

// ErrTruncated and ErrEmpty are the answers the summary cannot use (both of ClassRejected).
var (
	ErrTruncated = &Error{Class: ClassRejected, Status: http.StatusOK}
	ErrEmpty     = &Error{Class: ClassRejected, Status: http.StatusOK}
)

// Chat returns the answer's text. An answer cut by its output limit (finish_reason "length") is ErrTruncated —
// reasoning models may spend the whole budget before answering (design D3) —, one without text ErrEmpty.
func (c *Client) Chat(ctx context.Context, in ChatRequest) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model": in.Model, "max_tokens": in.MaxTokens, "temperature": 0.2,
		"messages": []map[string]string{{"role": "system", "content": in.System}, {"role": "user", "content": in.User}},
	})
	if err != nil {
		return "", &Error{Class: ClassRejected}
	}
	data, err := c.do(ctx, "/chat/completions", "application/json", body)
	if err != nil {
		return "", err
	}
	var out struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content *string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &out) != nil || len(out.Choices) == 0 {
		return "", &Error{Class: ClassRejected, Status: http.StatusOK}
	}
	ch := out.Choices[0]
	text := ""
	if ch.Message.Content != nil {
		text = strings.TrimSpace(*ch.Message.Content)
	}
	switch {
	case ch.FinishReason == "length":
		return "", ErrTruncated
	case text == "":
		return "", ErrEmpty
	}
	return text, nil
}

// Status is the status code of err's answer, as text for logs ("" without one).
func Status(err error) string {
	var e *Error
	if errors.As(err, &e) && e.Status != 0 {
		return strconv.Itoa(e.Status)
	}
	return ""
}
