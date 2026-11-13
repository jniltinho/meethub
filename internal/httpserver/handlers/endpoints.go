package handlers

import (
	"net/http"
	"slices"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/meethub/internal/audit"
	"github.com/jniltinho/meethub/internal/auth"
	"github.com/jniltinho/meethub/internal/httpserver"
	"github.com/jniltinho/meethub/internal/identity"
	"github.com/jniltinho/meethub/internal/org"
	"github.com/jniltinho/meethub/internal/theme"
)

// bootstrap serves GET /bootstrap: what an anonymous page needs, nothing more.
func (a *API) bootstrap(c *echo.Context) error {
	ctx := c.Request().Context()
	initialized, err := a.Auth.Initialized(ctx)
	if err != nil {
		return err
	}
	def := theme.Default
	if initialized {
		if o, err := org.Get(ctx, a.DB.Gorm); err == nil {
			def = o.DefaultTheme
		}
	}
	return c.JSON(http.StatusOK, map[string]any{
		"setup_required": !initialized,
		"default_theme":  def,
		"languages":      Languages,
	})
}

type setupRequest struct {
	SetupToken       string `json:"setup_token"`
	OrganizationName string `json:"organization_name"`
	DisplayName      string `json:"display_name"`
	Email            string `json:"email"`
	Password         string `json:"password"`
}

func (a *API) setup(c *echo.Context) error {
	var req setupRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	res, err := a.Auth.Setup(c.Request().Context(), req.SetupToken, auth.InitInput{
		OrganizationName: req.OrganizationName, DisplayName: req.DisplayName, Email: req.Email, Password: req.Password,
	}, c.RealIP(), c.Request().UserAgent())
	if err != nil {
		return err
	}
	return a.respondSession(c, http.StatusCreated, res)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *API) login(c *echo.Context) error {
	var req loginRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	res, err := a.Auth.Login(c.Request().Context(), req.Email, req.Password, c.RealIP(), c.Request().UserAgent())
	if err != nil {
		return err
	}
	return a.respondSession(c, http.StatusOK, res)
}

// respondSession sets the new session cookie and answers with the profile.
func (a *API) respondSession(c *echo.Context, status int, res *auth.LoginResult) error {
	p, err := a.Auth.Authenticate(c.Request().Context(), res.Token)
	if err != nil {
		return err
	}
	a.setCookie(c, res.Token, res.Session.ExpiresAt)
	body, err := a.profile(c, p)
	if err != nil {
		return err
	}
	return c.JSON(status, body)
}

func (a *API) logout(c *echo.Context) error {
	if err := a.Auth.Logout(c.Request().Context(), principal(c), c.RealIP()); err != nil {
		return err
	}
	a.clearCookie(c)
	return c.NoContent(http.StatusNoContent)
}

// profileBody is the shape of /me (and of login/setup responses).
type profileBody struct {
	User         *identity.User       `json:"user"`
	Organization *org.Organization    `json:"organization"`
	Role         string               `json:"role"`
	Permissions  []org.Permission     `json:"permissions"`
	Preferences  identity.Preferences `json:"preferences"`
	CSRFToken    string               `json:"csrf_token"`
}

func (a *API) profile(c *echo.Context, p *auth.Principal) (*profileBody, error) {
	ctx := c.Request().Context()
	o, err := org.Get(ctx, a.DB.Gorm)
	if err != nil {
		return nil, err
	}
	prefs, err := a.store.Preferences(ctx, a.DB.Gorm, p.User.ID)
	if err != nil {
		return nil, err
	}
	return &profileBody{
		User: p.User, Organization: o, Role: p.Membership.Role, Permissions: org.Permissions(p.Membership.Role),
		Preferences: prefs, CSRFToken: p.CSRFToken(),
	}, nil
}

func (a *API) me(c *echo.Context) error {
	body, err := a.profile(c, principal(c))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, body)
}

type updateMeRequest struct {
	DisplayName *string `json:"display_name"`
	Preferences *struct {
		Theme    *string `json:"theme"`
		Language *string `json:"language"`
	} `json:"preferences"`
}

func (a *API) updateMe(c *echo.Context) error {
	var req updateMeRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	p := principal(c)
	if req.Preferences != nil {
		t, l := req.Preferences.Theme, req.Preferences.Language
		if t != nil && *t != "" && !theme.Valid(*t) {
			return ErrInvalidTheme
		}
		if l != nil && *l != "" && !slices.Contains(Languages, *l) {
			return ErrInvalidLanguage
		}
		if err := a.store.UpdatePreferences(ctx, a.DB.Gorm, p.User.ID, t, l); err != nil {
			return err
		}
	}
	if req.DisplayName != nil {
		if err := a.store.SetDisplayName(ctx, a.DB.Gorm, p.User.ID, *req.DisplayName); err != nil {
			return err
		}
		u, err := a.store.ByID(ctx, a.DB.Gorm, p.User.ID)
		if err != nil {
			return err
		}
		p.User = u
	}
	return a.me(c)
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (a *API) changePassword(c *echo.Context) error {
	var req passwordRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	res, err := a.Auth.ChangePassword(c.Request().Context(), principal(c), req.CurrentPassword, req.NewPassword,
		c.RealIP(), c.Request().UserAgent())
	if err != nil {
		return err
	}
	return a.respondSession(c, http.StatusOK, res)
}

func (a *API) listUsers(c *echo.Context) error {
	page, err := a.Users.List(c.Request().Context(), principal(c).Actor(c.RealIP()), org.ListFilter{
		Role: c.QueryParam("role"), Status: c.QueryParam("status"), Query: c.QueryParam("q"),
		Limit: queryInt(c, "limit"), Cursor: c.QueryParam("cursor"),
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, page)
}

type createUserRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
	Password    string `json:"password"`
}

func (a *API) createUser(c *echo.Context) error {
	var req createUserRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	m, err := a.Users.Create(c.Request().Context(), principal(c).Actor(c.RealIP()), org.NewUser(req))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, m)
}

type updateUserRequest struct {
	DisplayName *string `json:"display_name"`
	Role        *string `json:"role"`
	Status      *string `json:"status"`
}

func (a *API) updateUser(c *echo.Context) error {
	var req updateUserRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	m, err := a.Users.Update(c.Request().Context(), principal(c).Actor(c.RealIP()), c.Param("id"), org.Patch(req))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, m)
}

type resetPasswordRequest struct {
	Password string `json:"password"`
}

func (a *API) resetPassword(c *echo.Context) error {
	var req resetPasswordRequest
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if err := a.Users.ResetPassword(c.Request().Context(), principal(c).Actor(c.RealIP()), c.Param("id"), req.Password); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) listAudit(c *echo.Context) error {
	f := audit.Filter{
		ActorID: c.QueryParam("actor"), Action: c.QueryParam("action"),
		Limit: queryInt(c, "limit"), Cursor: c.QueryParam("cursor"),
	}
	for key, dst := range map[string]*time.Time{"from": &f.From, "to": &f.To} {
		if v := c.QueryParam(key); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return httpserver.ErrInvalidInput
			}
			*dst = t
		}
	}
	page, err := audit.List(c.Request().Context(), a.DB.Gorm, f)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, page)
}
