package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/httpserver"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/settings"
)

// registerSettings adds Administration → Settings (add-admin-settings) for manage_system.
func (a *API) registerSettings(in *echo.Group) {
	g := in.Group("/admin/settings", requirePermission(org.PermManageSystem))
	g.GET("", a.getSettings)
	g.PUT("", a.requestSettings)
	g.POST("/confirm", a.confirmSettings)
}

// settingsMapper maps the settings errors, keeping their messages: they name the key (never capitalized), the bound
// or the variable.
func settingsMapper(err error) *httpserver.Error {
	var ve *config.AdminValueError
	var le *settings.LockedError
	var ie *settings.InvalidConfigError
	switch {
	case errors.Is(err, config.ErrNotEditable):
		return httpserver.NewError(http.StatusBadRequest, "setting_not_editable", err.Error()+".")
	case errors.As(err, &ve), errors.As(err, &ie):
		return httpserver.NewError(http.StatusBadRequest, "invalid_setting", err.Error()+".")
	case errors.As(err, &le):
		return httpserver.NewError(http.StatusConflict, "setting_locked", err.Error()+".")
	case errors.Is(err, config.ErrUnusualFile):
		return httpserver.NewError(http.StatusConflict, "settings_file_unusual", capitalize(config.ErrUnusualFile.Error())+".")
	case errors.Is(err, settings.ErrReadOnly):
		return httpserver.NewError(http.StatusConflict, "settings_read_only", capitalize(err.Error())+".")
	case errors.Is(err, settings.ErrPending):
		return httpserver.NewError(http.StatusConflict, "settings_pending", capitalize(err.Error())+".")
	case errors.Is(err, settings.ErrNotConfirmable):
		return httpserver.NewError(http.StatusConflict, "settings_not_confirmable", capitalize(err.Error())+".")
	case errors.Is(err, settings.ErrNothingToChange):
		return httpserver.NewError(http.StatusBadRequest, "invalid_input", "Nothing to change.")
	case errors.Is(err, settings.ErrTooMany):
		e := httpserver.NewError(http.StatusTooManyRequests, "settings_rate_limited", capitalize(err.Error())+".")
		e.Headers = map[string]string{"Retry-After": "3600"}
		return e
	}
	return nil
}

func (a *API) settingsOrUnavailable() error {
	if a.Settings == nil {
		return settings.ErrReadOnly
	}
	return nil
}

func (a *API) getSettings(c *echo.Context) error {
	if err := a.settingsOrUnavailable(); err != nil {
		return err
	}
	p := principal(c)
	v, err := a.Settings.Get(c.Request().Context(), p.Actor(c.RealIP()), p.Session.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, v)
}

func (a *API) requestSettings(c *echo.Context) error {
	if err := a.settingsOrUnavailable(); err != nil {
		return err
	}
	var req struct {
		Changes map[string]json.RawMessage `json:"changes"`
		Restore []string                   `json:"restore"`
	}
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	p := principal(c)
	v, err := a.Settings.Request(c.Request().Context(), p.Actor(c.RealIP()), p.Session.ID, req.Changes, req.Restore)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, v)
}

func (a *API) confirmSettings(c *echo.Context) error {
	if err := a.settingsOrUnavailable(); err != nil {
		return err
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	p := principal(c)
	if err := a.Settings.Confirm(c.Request().Context(), p.Actor(c.RealIP()), p.Session.ID, req.ID); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}
