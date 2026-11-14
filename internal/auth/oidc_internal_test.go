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
		Response: &http.Response{StatusCode: http.StatusInternalServerError}, Body: []byte(`{"id_token":"eyJthrowaway"}`),
	})
	if got := safeCause(err); strings.Contains(got, "eyJ") || !strings.Contains(got, "500") {
		t.Fatalf("logged %q", got)
	}
	long := fmt.Errorf("oidc discovery: 502 Bad Gateway: %s", strings.Repeat("x", 1000)+"SENTINEL")
	if got := safeCause(long); strings.Contains(got, "SENTINEL") || len(got) > 200 {
		t.Fatalf("logged %d bytes", len(got))
	}
}
