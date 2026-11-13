package handlers

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/meethub/internal/httpserver"
	"github.com/jniltinho/meethub/internal/realtime"
)

// ErrUnavailable answers upgrades while the server cannot accept them.
var ErrUnavailable = httpserver.NewError(http.StatusServiceUnavailable, "unavailable", "Try again shortly.")

// websocket serves GET /websocket: the event stream of an authenticated
// session, only from the app's own origin (a cross-site page would otherwise
// ride the cookie).
func (a *API) websocket(c *echo.Context) error {
	if !realtime.SameOrigin(c.Request(), a.PublicOrigin) {
		return httpserver.ErrForbidden
	}
	if a.Realtime == nil {
		return ErrUnavailable
	}
	p := principal(c)
	err := a.Realtime.Serve(c.Response(), c.Request(), realtime.Identity{
		UserID: p.User.ID, OrgRole: p.Membership.Role, SessionID: p.Session.ID, SessionLastSeen: p.Session.LastSeenAt,
	})
	if errors.Is(err, realtime.ErrStopping) {
		return ErrUnavailable
	}
	return err
}
