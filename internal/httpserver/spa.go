package httpserver

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v5"
)

// ThemeResolver returns the organization's default theme id and its color
// scheme ("light" or "dark"), rendered into index.html for the first paint.
type ThemeResolver func(ctx context.Context) (id, scheme string)

// DefaultThemeResolver is used until an organization exists.
func DefaultThemeResolver(context.Context) (string, string) { return "meethub-dark", "dark" }

const notBuiltPage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>MeetHub</title></head>
<body><h1>MeetHub</h1><p>The frontend is not built into this binary. Build it with <code>make build</code>.</p></body></html>`

// spaHandler serves the embedded SPA: real files as they are (hashed assets
// with long-lived caching), and index.html for any other path so client-side
// routes survive a reload.
func spaHandler(dist fs.FS, themes ThemeResolver) echo.HandlerFunc {
	index, _ := fs.ReadFile(dist, "index.html")
	return func(c *echo.Context) error {
		r := c.Request()
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			return echo.ErrMethodNotAllowed
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && name != "index.html" {
			if data, err := fs.ReadFile(dist, name); err == nil {
				h := c.Response().Header()
				if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
					h.Set(echo.HeaderContentType, ct)
				}
				if strings.HasPrefix(name, "assets/") {
					h.Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					h.Set("Cache-Control", "no-cache")
				}
				return c.Blob(http.StatusOK, h.Get(echo.HeaderContentType), data)
			} else if !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if path.Ext(name) != "" { // a missing file, not a client route
				return echo.ErrNotFound
			}
		}
		c.Response().Header().Set("Cache-Control", "no-cache")
		if index == nil {
			return c.HTML(http.StatusOK, notBuiltPage)
		}
		id, scheme := themes(r.Context())
		page := bytes.ReplaceAll(index, []byte("%MEETHUB_THEME%"), []byte(id))
		page = bytes.ReplaceAll(page, []byte("%MEETHUB_SCHEME%"), []byte(scheme))
		return c.HTMLBlob(http.StatusOK, page)
	}
}
