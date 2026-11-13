package handlers_test

import (
	"os"
	"strings"
	"testing"

	"github.com/jniltinho/meethub/internal/database/dbtest"
)

// TestEveryRouteDocumented fails when a /api/v1 route is missing from docs/api.md.
func TestEveryRouteDocumented(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/api.md")
	if err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, dbtest.Open(t, "sqlite"), false)
	n := 0
	for _, r := range e.srv.Routes() {
		method, path, _ := strings.Cut(r, " ")
		if !strings.HasPrefix(path, "/api/v1/") {
			continue
		}
		n++
		if !strings.Contains(string(doc), "`"+method+" "+path+"`") {
			t.Errorf("route %s is not documented in docs/api.md", r)
		}
	}
	if n < 15 {
		t.Fatalf("only %d routes found", n)
	}
}
