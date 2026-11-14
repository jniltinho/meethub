package handlers

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

type loginTwoFactorRequest struct {
	Ticket string `json:"ticket"`
	Code   string `json:"code"`
}

// loginTwoFactor serves POST /auth/login/two-factor: the second step of a login.
func (a *API) loginTwoFactor(c *echo.Context) error {
	var req loginTwoFactorRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	res, err := a.Auth.LoginTwoFactor(c.Request().Context(), req.Ticket, req.Code, c.RealIP(), c.Request().UserAgent())
	if err != nil {
		return err
	}
	return a.respondSession(c, http.StatusOK, res)
}

func (a *API) twoFactor(c *echo.Context) error {
	st, err := a.Auth.TwoFactor(c.Request().Context(), principal(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, st)
}

func (a *API) beginTwoFactor(c *echo.Context) error {
	e, err := a.Auth.BeginTwoFactor(c.Request().Context(), principal(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, e)
}

type codeRequest struct {
	Code string `json:"code"`
}

func (a *API) confirmTwoFactor(c *echo.Context) error {
	var req codeRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	codes, err := a.Auth.ConfirmTwoFactor(c.Request().Context(), principal(c), req.Code, c.RealIP())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"recovery_codes": codes})
}

func (a *API) newRecoveryCodes(c *echo.Context) error {
	var req codeRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	codes, err := a.Auth.RegenerateRecoveryCodes(c.Request().Context(), principal(c), req.Code, c.RealIP())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"recovery_codes": codes})
}

type disableTwoFactorRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

func (a *API) disableTwoFactor(c *echo.Context) error {
	var req disableTwoFactorRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if err := a.Auth.DisableTwoFactor(c.Request().Context(), principal(c), req.Password, req.Code, c.RealIP()); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) resetTwoFactor(c *echo.Context) error {
	if err := a.Users.ResetTwoFactor(c.Request().Context(), principal(c).Actor(c.RealIP()), c.Param("id")); err != nil {
		return err
	}
	a.sessionsChanged(c.Request().Context())
	return c.NoContent(http.StatusNoContent)
}
