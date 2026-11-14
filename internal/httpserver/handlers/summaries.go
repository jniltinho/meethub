package handlers

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/meethub/internal/ai"
	"github.com/jniltinho/meethub/internal/httpserver"
	"github.com/jniltinho/meethub/internal/summaries"
)

// registerSummaries adds the routes of meeting summaries (add-meeting-summaries): the organization's setting and
// its connection test, and a recording's summary.
func (a *API) registerSummaries(in *echo.Group) {
	in.GET("/admin/summaries", a.getSummariesSetting)
	in.PUT("/admin/summaries", a.setSummariesSetting)
	in.POST("/admin/summaries/test", a.testSummaries)
	in.GET("/recordings/:id/summary", a.getSummary)
	in.POST("/recordings/:id/summary", a.requestSummary)
}

// summaryMapper maps the AI client's failures (by class, never the provider's words) for the setting's test.
func summaryMapper(err error) *httpserver.Error {
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
