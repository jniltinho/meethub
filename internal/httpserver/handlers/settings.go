package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/auth"
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
	g.GET("/reauth", a.reauthNeeds)
	g.GET("/retention", a.retentionPreview)
	in.GET("/admin/diagnosis", a.adminDiagnosis, requirePermission(org.PermManageSystem))
}

func (a *API) diagnose(c *echo.Context) error {
	if a.Diagnosis == nil {
		return httpserver.NewError(http.StatusServiceUnavailable, "unavailable", "The diagnosis is not available.")
	}
	return c.JSON(http.StatusOK, map[string]any{"items": a.Diagnosis.Run(c.Request().Context())})
}

// adminDiagnosis shows the state of the server's parts to an owner (add-setup-wizard, design D2).
func (a *API) adminDiagnosis(c *echo.Context) error { return a.diagnose(c) }

// setupDiagnosis shows it before the first account exists, to who holds the setup token.
func (a *API) setupDiagnosis(c *echo.Context) error {
	var req struct {
		SetupToken string `json:"setup_token"`
	}
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if err := a.Auth.CheckSetupToken(c.Request().Context(), req.SetupToken); err != nil {
		return err
	}
	return a.diagnose(c)
}

// reauthNeeds says what a sensitive change will ask of this owner: the password, and a code with two-factor on.
func (a *API) reauthNeeds(c *echo.Context) error {
	p := principal(c)
	on, err := a.Auth.TwoFactorEnabled(c.Request().Context(), p.User.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]bool{"password": p.User.PasswordHash != "", "code": on})
}

// retentionPreview counts the recordings a retention of ?days= would delete, for the confirmation dialog.
func (a *API) retentionPreview(c *echo.Context) error {
	days, err := strconv.ParseInt(c.QueryParam("days"), 10, 64)
	if err != nil || days < 0 || days > 3650 {
		return httpserver.NewError(http.StatusBadRequest, "invalid_input", "Days must be a number from 0 to 3650.")
	}
	var n int64
	if a.Recordings != nil {
		if n, err = a.Recordings.Expiring(c.Request().Context(), days); err != nil {
			return err
		}
	}
	return c.JSON(http.StatusOK, map[string]int64{"recordings": n})
}

// reauth checks the password (and the code) a sensitive change carries (edit-ai-and-recording-settings, design
// D3) before anything is validated or written; keys are what the change names, for the audit of a failure.
func (a *API) reauth(c *echo.Context, password, code string, keys []string) error {
	p, ctx := principal(c), c.Request().Context()
	err := a.Auth.Reauthenticate(ctx, p, password, code, c.RealIP())
	switch {
	case err == nil:
		return nil
	case errors.Is(err, auth.ErrReauthRequired):
		return httpserver.NewError(http.StatusForbidden, "reauth_required",
			"This change needs your password, and a two-factor code when two-factor is on.")
	case errors.Is(err, auth.ErrNoPassword):
		return httpserver.NewError(http.StatusForbidden, "reauth_unavailable",
			"Your account has no password: change this setting on the server.")
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrInvalidCode), errors.Is(err, auth.ErrTwoFactorOff):
		uid, ev := p.User.ID, audit.Event{Action: audit.ActionSettingsReauthFailed, TargetType: "settings",
			Details: map[string]any{"keys": keys}, ClientIP: c.RealIP()}
		ev.OrgID, ev.ActorID = &p.Membership.OrgID, &uid
		if aerr := audit.Record(context.WithoutCancel(ctx), a.DB.Gorm, ev); aerr != nil {
			return aerr
		}
		// 403, not the login's 401: the session is fine, the proof was not
		return httpserver.NewError(http.StatusForbidden, "reauth_failed", "The password or the code is wrong.")
	}
	return err
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
		Changes  map[string]json.RawMessage `json:"changes"`
		Restore  []string                   `json:"restore"`
		Password string                     `json:"password"`
		Code     string                     `json:"code"`
	}
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if sensitive := config.SensitiveKeys(append(slices.Collect(maps.Keys(req.Changes)), req.Restore...)); len(sensitive) > 0 {
		if err := a.reauth(c, req.Password, req.Code, sensitive); err != nil {
			return err
		}
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
