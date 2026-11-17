package sealed

import (
	"bytes"
	"errors"
	"testing"
)

func TestSeal(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	text, err := Seal(key, "ai_api_key", "a-test-key")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Open(key, "ai_api_key", text); err != nil || got != "a-test-key" {
		t.Fatalf("open: %q %v", got, err)
	}
	if again, _ := Seal(key, "ai_api_key", "a-test-key"); again == text {
		t.Fatal("the nonce is not random")
	}
	for name, open := range map[string]func() (string, error){
		"another column": func() (string, error) { return Open(key, "ai_transcription_api_key", text) },
		"another key":    func() (string, error) { return Open(bytes.Repeat([]byte{8}, 32), "ai_api_key", text) },
		"not sealed":     func() (string, error) { return Open(key, "ai_api_key", "a-test-key") },
	} {
		if got, err := open(); !errors.Is(err, ErrSealed) || got != "" {
			t.Errorf("%s: %q %v", name, got, err)
		}
	}
	if _, err := Seal(nil, "x", "y"); err == nil {
		t.Fatal("sealed without a key")
	}
}
