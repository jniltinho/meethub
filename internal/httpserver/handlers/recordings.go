package handlers

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/meethub/internal/recordings"
)

// registerRecordings adds the routes of meeting recordings (add-meeting-recordings, design D12).
func (a *API) registerRecordings(in *echo.Group) {
	in.POST("/channels/:id/meeting/recording", a.startRecording)
	in.DELETE("/channels/:id/meeting/recording", a.stopRecording)
	in.GET("/channels/:id/meeting/recordings", a.listRecordings)
}

func (a *API) recordingsOrUnavailable() error {
	if !a.Recordings.Enabled() {
		return recordings.ErrUnavailable
	}
	return nil
}

func (a *API) startRecording(c *echo.Context) error {
	if err := a.recordingsOrUnavailable(); err != nil {
		return err
	}
	v, err := a.Recordings.Start(c.Request().Context(), subject(c), c.Param("id"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, v)
}

func (a *API) stopRecording(c *echo.Context) error {
	if err := a.recordingsOrUnavailable(); err != nil {
		return err
	}
	if err := a.Recordings.Stop(c.Request().Context(), subject(c), c.Param("id")); err != nil {
		return err
	}
	return c.NoContent(http.StatusAccepted)
}

func (a *API) listRecordings(c *echo.Context) error {
	if err := a.recordingsOrUnavailable(); err != nil {
		return err
	}
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit == 0 {
		limit = 20
	}
	list, err := a.Recordings.List(c.Request().Context(), subject(c), c.Param("id"), c.QueryParam("before"), limit)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"recordings": list})
}
