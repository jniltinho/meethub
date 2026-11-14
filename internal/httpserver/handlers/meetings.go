package handlers

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/webhook"

	"github.com/jniltinho/meethub/internal/httpserver"
)

// Meetings errors.
var (
	ErrMeetingsUnavailable = httpserver.NewError(http.StatusServiceUnavailable, "meetings_unavailable", "Meetings are not configured.")
	ErrWebhookSignature    = httpserver.NewError(http.StatusUnauthorized, "invalid_signature", "The webhook signature is not valid.")
)

// webhookMaxBytes bounds a LiveKit webhook body (a few KiB in practice).
const webhookMaxBytes = 64 << 10

// registerMeetings adds the meetings routes (design D6): the LiveKit webhook on the bare API group (LiveKit is not a
// browser: its signature replaces the session and CSRF checks), the rest on the authenticated group.
func (a *API) registerMeetings(g, in *echo.Group) {
	g.POST("/meetings/webhook", a.meetingWebhook)
	in.GET("/meetings", a.listMeetings)
	in.GET("/channels/:id/meeting", a.channelMeeting)
	in.POST("/channels/:id/meeting/token", a.meetingToken)
}

func (a *API) meetingsOrUnavailable() error {
	if !a.Meetings.Enabled() {
		return ErrMeetingsUnavailable
	}
	return nil
}

func (a *API) listMeetings(c *echo.Context) error {
	rooms, err := a.Meetings.List(c.Request().Context(), subject(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"enabled": a.Meetings.Enabled(), "rooms": rooms})
}

func (a *API) channelMeeting(c *echo.Context) error {
	if err := a.meetingsOrUnavailable(); err != nil {
		return err
	}
	room, err := a.Meetings.Room(c.Request().Context(), subject(c), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, room)
}

func (a *API) meetingToken(c *echo.Context) error {
	if err := a.meetingsOrUnavailable(); err != nil {
		return err
	}
	p := principal(c)
	tok, err := a.Meetings.IssueToken(c.Request().Context(), subject(c), p.User.DisplayName, c.Param("id"))
	if err != nil {
		return err
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, tok)
}

// meetingWebhook receives LiveKit's events: the body must match the SHA-256 signed, with the API secret, in the
// Authorization header.
func (a *API) meetingWebhook(c *echo.Context) error {
	if err := a.meetingsOrUnavailable(); err != nil {
		return err
	}
	r := c.Request()
	r.Body = http.MaxBytesReader(c.Response(), r.Body, webhookMaxBytes)
	ev, err := webhook.ReceiveWebhookEvent(r, auth.NewSimpleKeyProvider(a.Meetings.APIKey, a.Meetings.APISecret))
	if err != nil {
		return ErrWebhookSignature
	}
	if err := a.Meetings.HandleWebhook(r.Context(), ev); err != nil {
		return err
	}
	return c.NoContent(http.StatusOK)
}
