package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/httpserver"
)

// ssoFailed is where a refused single sign-on lands: the login page says only that it could not be completed.
const ssoFailed = "/login?sso=failed"

// oidcCookie binds the state of a single sign-on to the browser that started it (no login CSRF).
func (a *API) oidcCookie() string {
	if a.SecureCookies {
		return "__Host-meethub_oidc"
	}
	return "meethub_oidc"
}

func (a *API) setOIDCCookie(c *echo.Context, value string, maxAge int) {
	c.SetCookie(&http.Cookie{ //nolint:gosec // Secure follows the public URL, as the session cookie
		Name: a.oidcCookie(), Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: a.SecureCookies, SameSite: http.SameSiteLaxMode, // Lax: the provider's redirect is a top-level GET
	})
}

// oidcStart serves GET /auth/oidc/start: a redirect to the provider.
func (a *API) oidcStart(c *echo.Context) error {
	if a.OIDC == nil {
		return httpserver.ErrNotFound
	}
	if err := a.oidcLimit(c, "start:"); err != nil {
		return err
	}
	authURL, state, err := a.OIDC.Start(c.Request().Context())
	if err != nil {
		a.OIDC.Logf(c.Request().Context(), "single sign-on start", err)
		return c.Redirect(http.StatusFound, ssoFailed)
	}
	a.setOIDCCookie(c, state, int((10 * time.Minute).Seconds()))
	return c.Redirect(http.StatusFound, authURL)
}

// oidcCallback serves GET /auth/oidc/callback: the provider's answer; a session and the app, or the login page.
func (a *API) oidcCallback(c *echo.Context) error {
	if a.OIDC == nil {
		return httpserver.ErrNotFound
	}
	if err := a.oidcLimit(c, "callback:"); err != nil { // before any refusal is audited
		return err
	}
	browser := ""
	if ck, err := c.Cookie(a.oidcCookie()); err == nil {
		browser = ck.Value
	}
	a.setOIDCCookie(c, "", -1)
	q := c.Request().URL.Query()
	res, err := a.OIDC.Callback(c.Request().Context(), q.Get("state"), browser, q.Get("code"), c.RealIP(), c.Request().UserAgent())
	if err != nil {
		if !errors.Is(err, auth.ErrSSOFailed) { // a refusal is already audited and logged
			a.OIDC.Logf(c.Request().Context(), "single sign-on callback", err)
		}
		return c.Redirect(http.StatusFound, ssoFailed)
	}
	a.setCookie(c, res.Token, res.Session.ExpiresAt)
	return c.Redirect(http.StatusFound, "/")
}

// oidcLimit allows 30 starts and 30 callbacks a minute per client address.
func (a *API) oidcLimit(c *echo.Context, kind string) error {
	if a.OIDCLimit == nil {
		return nil
	}
	if ok, wait := a.OIDCLimit.Allow(c.Request().Context(), kind+c.RealIP()); !ok {
		e := httpserver.NewError(http.StatusTooManyRequests, "rate_limited", "Too many attempts. Try again later.")
		e.Headers = map[string]string{"Retry-After": strconv.Itoa(int(wait.Seconds() + 1))}
		return e
	}
	return nil
}
