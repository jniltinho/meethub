package auth

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestSafeCauseDropsTheBody(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("exchange: %w", &oauth2.RetrieveError{
		Response: &http.Response{StatusCode: 500}, Body: []byte(`{"id_token":"eyJthrowaway"}`),
	})
	if got := safeCause(err); strings.Contains(got, "eyJ") || !strings.Contains(got, "500") {
		t.Fatalf("logged %q", got)
	}
}
