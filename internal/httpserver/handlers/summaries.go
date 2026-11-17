package handlers

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/meethub/internal/ai"
	"github.com/jniltinho/meethub/internal/httpserver"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/summaries"
)

// registerSummaries adds the routes of meeting summaries (add-meeting-summaries): the organization's setting and
// its connection test, and a recording's summary.
func (a *API) registerSummaries(in *echo.Group) {
	in.GET("/admin/summaries", a.getSummariesSetting)
	in.PUT("/admin/summaries", a.setSummariesSetting)
	in.POST("/admin/summaries/test", a.testSummaries)
	in.GET("/admin/summaries/keys", a.getSummaryKeys)
	in.PUT("/admin/summaries/keys", a.setSummaryKey)
	in.GET("/recordings/:id/summary", a.getSummary)
	in.POST("/recordings/:id/summary", a.requestSummary)
}

// summaryMapper maps the AI client's failures (by class, never the provider's words) for the setting's test.
func summaryMapper(err error) *httpserver.Error {
	var locked *summaries.KeyLockedError
	switch {
	case errors.As(err, &locked):
		return httpserver.NewError(http.StatusConflict, "setting_locked", locked.Error()+".")
	case errors.Is(err, summaries.ErrNoSecretKey):
		return httpserver.NewError(http.StatusServiceUnavailable, "secret_key_missing", capitalize(err.Error())+".")
	case errors.Is(err, summaries.ErrBadKey):
		return httpserver.NewError(http.StatusBadRequest, "invalid_input", capitalize(err.Error())+".")
	case errors.Is(err, summaries.ErrUnknownKey):
		return httpserver.NewError(http.StatusBadRequest, "invalid_input", "Unknown key.")
	}
	var e *ai.Error
	if !errors.As(err, &e) {
		return nil
	}
	msg := map[string]string{
		ai.ClassUnauthorized: "The AI service refused the key.",
		ai.ClassUnavailable:  "The AI service could not be reached.",
		ai.ClassRejected:     "The AI service refused the request or could not read the sample.",
		ai.ClassTooLong:      "The AI service refused the request as too long.",
		ai.ClassUnsupported:  "The AI service cannot give timed transcription, or a model or path is wrong.",
	}[e.Class]
	return httpserver.NewError(http.StatusUnprocessableEntity, e.Code(), msg)
}

func (a *API) summariesOrUnavailable() error {
	if a.Summaries == nil {
		return summaries.ErrNotConfigured
	}
	return nil
}

func (a *API) getSummariesSetting(c *echo.Context) error {
	if a.Summaries == nil { // read by owners even when nothing is configured: the page says why
		return c.JSON(http.StatusOK, summaries.SettingView{Value: "off"})
	}
	v, err := a.Summaries.GetSetting(c.Request().Context(), principal(c).Actor(c.RealIP()))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}

func (a *API) setSummariesSetting(c *echo.Context) error {
	if err := a.summariesOrUnavailable(); err != nil {
		return err
	}
	var req struct {
		Value string `json:"meeting_summaries"`
	}
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	v, err := a.Summaries.SetSetting(c.Request().Context(), principal(c).Actor(c.RealIP()), req.Value)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}

func (a *API) testSummaries(c *echo.Context) error {
	if err := a.summariesOrUnavailable(); err != nil {
		return err
	}
	if err := a.Summaries.Test(c.Request().Context(), principal(c).Actor(c.RealIP())); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) getSummary(c *echo.Context) error {
	if err := a.summariesOrUnavailable(); err != nil {
		return err
	}
	v, err := a.Summaries.Get(c.Request().Context(), subject(c), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}

func (a *API) requestSummary(c *echo.Context) error {
	if err := a.summariesOrUnavailable(); err != nil {
		return err
	}
	v, err := a.Summaries.Request(c.Request().Context(), subject(c), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, v)
}

// getSummaryKeys shows whether each AI key is set and where from, never a key.
func (a *API) getSummaryKeys(c *echo.Context) error {
	if err := a.summariesOrUnavailable(); err != nil {
		return err
	}
	v, err := a.Summaries.GetKeys(c.Request().Context(), principal(c).Actor(c.RealIP()))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}

// setSummaryKey stores an AI key entered in Administration, or removes it (an empty value), after the owner's
// password is checked again. The answer is the keys' state, never a key.
func (a *API) setSummaryKey(c *echo.Context) error {
	if err := a.summariesOrUnavailable(); err != nil {
		return err
	}
	var req struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	actor := principal(c).Actor(c.RealIP())
	if err := actor.Require(org.PermManageSystem); err != nil {
		return err
	}
	if err := a.reauth(c, req.Password, req.Code, []string{req.Key}); err != nil {
		return err
	}
	v, err := a.Summaries.SetKey(c.Request().Context(), actor, req.Key, req.Value)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}
