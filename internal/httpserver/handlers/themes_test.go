package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jniltinho/meethub/internal/httpserver"
	"github.com/jniltinho/meethub/internal/httpserver/handlers"
)

func TestListThemes(t *testing.T) {
	t.Parallel()
	s := httpserver.New(httpserver.Options{})
	s.API().GET("/themes", handlers.ListThemes)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/themes", nil))
	var body struct {
		Themes []struct {
			ID, Scheme string
			Default    bool
		}
		Default string
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if len(body.Themes) != 22 || body.Default != "meethub-dark" || !body.Themes[0].Default {
		t.Fatalf("themes: %+v", body)
	}
}
