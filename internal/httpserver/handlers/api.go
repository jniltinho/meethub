package handlers

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/database"
	"github.com/jniltinho/meethub/internal/httpserver"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/ratelimit"
	"github.com/jniltinho/meethub/internal/session"
	"github.com/jniltinho/meethub/internal/theme"
)

// Languages the web app is translated to; "en" is the default.
var Languages = []string{"en", "pt-BR", "es"}

// API errors specific to these handlers.
var (
	ErrCSRF            = httpserver.NewError(http.StatusForbidden, "csrf_failed", "The request failed the CSRF check.")
	ErrUnauthenticated = httpserver.NewError(http.StatusUnauthorized, "unauthenticated", "You must log in.")
	ErrSessionExpired  = httpserver.NewError(http.StatusUnauthorized, "session_expired", "Your session expired. Log in again.")
	ErrInvalidTheme    = httpserver.NewError(http.StatusBadRequest, "invalid_theme", "Unknown theme.")
	ErrInvalidLanguage = httpserver.NewError(http.StatusBadRequest, "invalid_language", "Unsupported language.")
	ErrNotJSON         = httpserver.NewError(http.StatusUnsupportedMediaType, "unsupported_media_type", "The request must be JSON.")
)

// API serves the authentication, profile and administration endpoints.
type API struct {
	Auth          *auth.Service
	Users         *org.Users
	DB            *database.DB
	PublicOrigin  string // scheme://host[:port] of http.public_url
	SecureCookies bool   // HTTPS public URL: Secure + __Host- cookie

	store identity.Store
}

// CookieName is the session cookie's name for the scheme in use.
func (a *API) CookieName() string {
	if a.SecureCookies {
		return "__Host-meethub_session"
	}
	return "meethub_session"
}

const principalKey = "meethub.principal"

// Register adds every route of this package to the /api/v1 group.
func (a *API) Register(g *echo.Group) {
	g.GET("/themes", ListThemes)
	g.GET("/bootstrap", a.bootstrap)

	pub := g.Group("", a.sameOriginJSON)
	pub.POST("/auth/setup", a.setup)
	pub.POST("/auth/login", a.login)

	in := g.Group("", a.sameOriginJSON, a.authenticate, a.csrf)
	in.POST("/auth/logout", a.logout)
	in.GET("/me", a.me)
	in.PATCH("/me", a.updateMe)
	in.POST("/me/password", a.changePassword)

	users := in.Group("/admin/users", requirePermission(org.PermManageUsers))
	users.GET("", a.listUsers)
	users.POST("", a.createUser)
	users.PATCH("/:id", a.updateUser)
	users.POST("/:id/password", a.resetPassword)

	in.GET("/admin/audit", a.listAudit, requirePermission(org.PermReadAudits))
}

func unsafe(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

// sameOriginJSON makes state-changing requests prove they come from the app:
// JSON body and an Origin equal to the public origin (missing or "null" fails).
func (a *API) sameOriginJSON(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		r := c.Request()
		if !unsafe(r.Method) {
			return next(c)
		}
		if origin := r.Header.Get("Origin"); origin == "" || origin == "null" || !strings.EqualFold(origin, a.PublicOrigin) {
			return ErrCSRF
		}
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			return ErrNotJSON
		}
		return next(c)
	}
}

// authenticate loads the session's principal or answers 401.
func (a *API) authenticate(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		cookie, err := c.Cookie(a.CookieName())
		if err != nil || cookie.Value == "" {
			return ErrUnauthenticated
		}
		p, err := a.Auth.Authenticate(c.Request().Context(), cookie.Value)
		switch {
		case errors.Is(err, session.ErrExpired):
			a.clearCookie(c)
			return ErrSessionExpired
		case errors.Is(err, session.ErrInvalid):
			a.clearCookie(c)
			return ErrUnauthenticated
		case err != nil:
			return err
		}
		c.Set(principalKey, p)
		return next(c)
	}
}

// csrf requires X-CSRF-Token to match the session on state-changing requests.
func (a *API) csrf(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if unsafe(c.Request().Method) {
			want := principal(c).CSRFToken()
			got := c.Request().Header.Get("X-CSRF-Token")
			if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
				return ErrCSRF
			}
		}
		return next(c)
	}
}

func requirePermission(p org.Permission) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if !org.Can(principal(c).Membership.Role, p) {
				return httpserver.ErrForbidden
			}
			return next(c)
		}
	}
}

func principal(c *echo.Context) *auth.Principal {
	p, _ := c.Get(principalKey).(*auth.Principal)
	return p
}

func (a *API) setCookie(c *echo.Context, token string, expires time.Time) {
	c.SetCookie(&http.Cookie{ //nolint:gosec // Secure follows the public URL: always on with HTTPS, off only for http:// development
		Name: a.CookieName(), Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: a.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

func (a *API) clearCookie(c *echo.Context) {
	c.SetCookie(&http.Cookie{ //nolint:gosec // same attributes as setCookie
		Name: a.CookieName(), Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(0, 0),
		HttpOnly: true, Secure: a.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
}

// bindJSON decodes the body strictly: unknown fields are rejected.
func bindJSON(c *echo.Context, v any) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return httpserver.ErrBadRequest
		}
		return httpserver.ErrInvalidInput
	}
	if dec.More() {
		return httpserver.ErrBadRequest
	}
	return nil
}

func queryInt(c *echo.Context, key string) int {
	n, _ := strconv.Atoi(c.QueryParam(key))
	return n
}

// Mappers turn domain errors into API errors.
func Mappers() []httpserver.ErrorMapper {
	table := []struct {
		err    error
		status int
		code   string
	}{
		{auth.ErrInvalidCredentials, http.StatusUnauthorized, "invalid_credentials"},
		{auth.ErrInvalidSetupToken, http.StatusForbidden, "invalid_setup_token"},
		{auth.ErrAlreadyInitialized, http.StatusConflict, "already_initialized"},
		{org.ErrAlreadyExists, http.StatusConflict, "already_initialized"},
		{auth.ErrFirstUserOwner, http.StatusBadRequest, "invalid_input"},
		{identity.ErrWeakPassword, http.StatusBadRequest, "weak_password"},
		{identity.ErrPasswordTooLong, http.StatusBadRequest, "invalid_input"},
		{identity.ErrInvalidEmail, http.StatusBadRequest, "invalid_input"},
		{identity.ErrInvalidName, http.StatusBadRequest, "invalid_input"},
		{identity.ErrInvalidStatus, http.StatusBadRequest, "invalid_input"},
		{identity.ErrEmailTaken, http.StatusConflict, "email_taken"},
		{identity.ErrInvalidUsername, http.StatusBadRequest, "invalid_username"},
		{identity.ErrUsernameTaken, http.StatusConflict, "username_taken"},
		{identity.ErrUserNotFound, http.StatusNotFound, "not_found"},
		{org.ErrNotMember, http.StatusNotFound, "not_found"},
		{org.ErrInvalidRole, http.StatusBadRequest, "invalid_input"},
		{org.ErrInvalidOrgName, http.StatusBadRequest, "invalid_input"},
		{org.ErrForbidden, http.StatusForbidden, "forbidden"},
		{org.ErrLastOwner, http.StatusConflict, "last_owner"},
		{org.ErrBadCursor, http.StatusBadRequest, "invalid_input"},
		{audit.ErrBadCursor, http.StatusBadRequest, "invalid_input"},
	}
	return []httpserver.ErrorMapper{
		func(err error) *httpserver.Error {
			var rl *ratelimit.RateLimitedError
			if errors.As(err, &rl) {
				e := httpserver.NewError(http.StatusTooManyRequests, "rate_limited", "Too many failed attempts. Try again later.")
				e.Headers = map[string]string{"Retry-After": strconv.Itoa(int(rl.RetryAfter.Seconds() + 1))}
				return e
			}
			if errors.Is(err, identity.ErrBusy) {
				e := httpserver.NewError(http.StatusServiceUnavailable, "busy", "The server is busy. Try again shortly.")
				e.Headers = map[string]string{"Retry-After": "5"}
				return e
			}
			for _, m := range table {
				if errors.Is(err, m.err) {
					return httpserver.NewError(m.status, m.code, capitalize(m.err.Error())+".")
				}
			}
			return nil
		},
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ThemeResolver renders the organization's default theme into index.html.
func ThemeResolver(db *database.DB) httpserver.ThemeResolver {
	return func(ctx context.Context) (string, string) {
		id := theme.Default
		if o, err := org.Get(ctx, db.Gorm); err == nil && theme.Valid(o.DefaultTheme) {
			id = o.DefaultTheme
		}
		t, _ := theme.Lookup(id)
		return t.ID, t.Scheme
	}
}
