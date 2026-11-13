// Package httpserver is MeetHub's HTTP layer on Echo v5: the /api/v1 JSON API,
// health and readiness probes, security headers, structured request logging,
// client-address extraction behind trusted proxies and the embedded SPA.
package httpserver

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

// ContentSecurityPolicy is the production policy for every response.
const ContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data: blob:; " +
	"font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// Check is a named readiness probe; it returns nil when the dependency is ready.
type Check struct {
	Name  string
	Check func(ctx context.Context) error
}

// Options configure a Server.
type Options struct {
	Addr            string
	SecureHeaders   bool // HSTS (the public URL is HTTPS)
	TrustedProxies  []netip.Prefix
	BodyLimitBytes  int64
	ShutdownTimeout time.Duration
	Logger          *slog.Logger
	Dist            fs.FS         // the built SPA (web.Dist())
	Themes          ThemeResolver // default theme rendered into index.html
	ReadyChecks     []Check
	ErrorMappers    []ErrorMapper
}

// Server is the configured Echo instance.
type Server struct {
	echo *echo.Echo
	opts Options
	api  *echo.Group
}

// New builds the server with its middleware and the always-present routes
// (probes, API not-found, SPA). Feature routes are added through API().
func New(o Options) *Server {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Themes == nil {
		o.Themes = DefaultThemeResolver
	}
	if o.BodyLimitBytes <= 0 {
		o.BodyLimitBytes = 1 << 20
	}
	if o.ShutdownTimeout <= 0 {
		o.ShutdownTimeout = 15 * time.Second
	}
	// The router's NotFoundHandler runs only when no route matches the path, so a
	// known path with another method still answers 405.
	notFound := func(*echo.Context) error { return ErrNotFound }
	if o.Dist != nil {
		spa := spaHandler(o.Dist, o.Themes)
		notFound = func(c *echo.Context) error {
			if strings.HasPrefix(c.Request().URL.Path, "/api/") {
				return ErrNotFound
			}
			return spa(c)
		}
	}
	e := echo.NewWithConfig(echo.Config{
		Logger:           o.Logger,
		Router:           echo.NewRouter(echo.RouterConfig{NotFoundHandler: notFound}),
		HTTPErrorHandler: errorHandler(o.Logger, o.ErrorMappers),
		IPExtractor:      clientIP(o.TrustedProxies),
	})
	e.SchemeExtractor = requestScheme(o.TrustedProxies)

	e.Use(
		middleware.RequestID(),
		middleware.Recover(),
		requestLogger(o.Logger),
		securityHeaders(o.SecureHeaders),
		middleware.BodyLimit(o.BodyLimitBytes),
	)

	s := &Server{echo: e, opts: o}
	s.api = e.Group("/api/v1")
	s.api.GET("/health", func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	s.api.GET("/ready", s.ready)
	return s
}

// API returns the /api/v1 route group for feature handlers.
func (s *Server) API() *echo.Group { return s.api }

// Handler returns the HTTP handler (for tests and custom listeners).
func (s *Server) Handler() http.Handler { return s.echo }

// Run serves until ctx is cancelled, then shuts down gracefully, letting
// in-flight requests finish within the shutdown timeout. onListen, when not
// nil, receives the bound address.
func (s *Server) Run(ctx context.Context, onListen func(net.Addr)) error {
	sc := echo.StartConfig{
		Address:          s.opts.Addr,
		HideBanner:       true,
		HidePort:         true,
		GracefulTimeout:  s.opts.ShutdownTimeout,
		ListenerAddrFunc: onListen,
		BeforeServeFunc: func(srv *http.Server) error {
			srv.ReadHeaderTimeout = 10 * time.Second
			srv.IdleTimeout = 120 * time.Second
			return nil
		},
	}
	s.opts.Logger.Info("http server listening", "addr", s.opts.Addr)
	err := sc.Start(ctx, s.echo)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// ready runs every readiness check and answers 200 or 503 with each result.
func (s *Server) ready(c *echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), 3*time.Second)
	defer cancel()
	results := make(map[string]string, len(s.opts.ReadyChecks))
	status, overall := http.StatusOK, "ready"
	for _, chk := range s.opts.ReadyChecks {
		if err := chk.Check(ctx); err != nil {
			results[chk.Name] = err.Error()
			status, overall = http.StatusServiceUnavailable, "not_ready"
			continue
		}
		results[chk.Name] = "ok"
	}
	return c.JSON(status, map[string]any{"status": overall, "checks": results})
}

// securityHeaders sets the CSP and the other hardening headers on every response.
func securityHeaders(hsts bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			h := c.Response().Header()
			h.Set("Content-Security-Policy", ContentSecurityPolicy)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("X-Frame-Options", "DENY")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			return next(c)
		}
	}
}

// requestLogger logs one structured line per request: no body, query, cookie
// or authorization header.
func requestLogger(logger *slog.Logger) echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogMethod:    true,
		LogURIPath:   true,
		LogStatus:    true,
		LogLatency:   true,
		LogRequestID: true,
		LogRemoteIP:  true,
		HandleError:  true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			level := slog.LevelInfo
			if v.Status >= http.StatusInternalServerError {
				level = slog.LevelError
			} else if strings.HasSuffix(v.URIPath, "/health") {
				level = slog.LevelDebug
			}
			logger.LogAttrs(c.Request().Context(), level, "http request",
				slog.String("method", v.Method),
				slog.String("path", v.URIPath),
				slog.Int("status", v.Status),
				slog.Float64("duration_ms", float64(v.Latency.Microseconds())/1000),
				slog.String("request_id", v.RequestID),
				slog.String("client_ip", v.RemoteIP),
			)
			return nil
		},
	})
}

// Routes lists every registered route as "METHOD /path" (Echo's internal
// not-found entries excluded).
func (s *Server) Routes() []string {
	var out []string
	for _, r := range s.echo.Router().Routes() {
		if r.Method == strings.ToUpper(r.Method) && !strings.Contains(r.Method, "_") {
			out = append(out, r.Method+" "+r.Path)
		}
	}
	return out
}
