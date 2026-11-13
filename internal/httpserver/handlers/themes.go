// Package handlers holds the /api/v1 endpoint handlers. Each file serves one
// resource; handlers call the domain packages and return their errors, which
// the server maps to API errors in one place (Mappers).
package handlers

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/jniltinho/meethub/internal/theme"
)

// ListThemes serves GET /api/v1/themes: the theme catalog (public).
func ListThemes(c *echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{"themes": theme.All(), "default": theme.Default})
}
