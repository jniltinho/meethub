// Package config loads MeetHub's settings: defaults, then a TOML file, then
// MEETHUB_* environment variables. Secrets are accepted only from the
// environment (or from a file named by the variable with a _FILE suffix) and
// are never part of the TOML file, logs or API responses.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Environment names accepted in app.env.
const (
	EnvDevelopment = "development"
	EnvProduction  = "production"
)

// Database drivers accepted in database.driver.
const (
	DriverSQLite   = "sqlite"
	DriverMariaDB  = "mariadb"
	DriverPostgres = "postgres"
)

// Config is the complete server configuration.
type Config struct {
	App      App      `toml:"app"`
	HTTP     HTTP     `toml:"http"`
	Database Database `toml:"database"`
	Redis    Redis    `toml:"redis"`
	Session  Session  `toml:"session"`
	Setup    Setup    `toml:"setup"`
	Log      Log      `toml:"log"`
	Files    Files    `toml:"files"`
}

// App holds general settings.
type App struct {
	Env     string `toml:"env"`      // development or production
	DataDir string `toml:"data_dir"` // SQLite database, setup token
}

// HTTP holds the web server settings.
type HTTP struct {
	Addr            string   `toml:"addr"`
	PublicURL       string   `toml:"public_url"`      // canonical origin, e.g. https://chat.example.com
	TrustedProxies  []string `toml:"trusted_proxies"` // CIDRs or IPs whose X-Forwarded-* headers are honored
	BodyLimitBytes  int64    `toml:"body_limit_bytes"`
	ShutdownTimeout Duration `toml:"shutdown_timeout"`
}

// Database selects and reaches the one database MeetHub uses.
type Database struct {
	Driver         string   `toml:"driver"` // sqlite, mariadb or postgres
	Path           string   `toml:"path"`   // SQLite file; default <data_dir>/meethub.db
	Host           string   `toml:"host"`
	Port           int      `toml:"port"`
	Name           string   `toml:"name"`
	User           string   `toml:"user"`
	Params         string   `toml:"params"` // extra driver parameters, e.g. sslmode=require
	Password       string   `toml:"password" secret:"true"`
	ConnectTimeout Duration `toml:"connect_timeout"`
	MaxOpenConns   int      `toml:"max_open_conns"`
}

// Redis reaches the Redis server used by background jobs and rate limits.
type Redis struct {
	Addr           string   `toml:"addr"`
	DB             int      `toml:"db"`
	Username       string   `toml:"username"`
	Password       string   `toml:"password" secret:"true"`
	ConnectTimeout Duration `toml:"connect_timeout"`
}

// Session sets the lifetime of login sessions.
type Session struct {
	IdleTimeout      Duration `toml:"idle_timeout"`
	AbsoluteLifetime Duration `toml:"absolute_lifetime"`
}

// Setup holds the first-run setup token (secret).
type Setup struct {
	Token string `toml:"token" secret:"true"`
}

// Log configures the structured logger.
type Log struct {
	Level  string `toml:"level"`  // debug, info, warn, error
	Format string `toml:"format"` // json or text
}

// File storage drivers.
const (
	FilesLocal = "local"
	FilesS3    = "s3"
)

// Files configures attachments: where their bytes live and the upload limits.
type Files struct {
	Driver       string      `toml:"driver"`         // local (a directory) or s3 (an S3-compatible bucket)
	MaxSizeBytes int64       `toml:"max_size_bytes"` // per file
	MaxPerPost   int         `toml:"max_per_post"`   // files a message may carry
	Local        FilesLocalT `toml:"local"`
	S3           FilesS3T    `toml:"s3"`
}

// FilesLocalT is the local driver's directory.
type FilesLocalT struct {
	Dir string `toml:"dir"` // default <data_dir>/files
}

// FilesS3T is the S3 driver's bucket; the credentials come only from the environment.
type FilesS3T struct {
	Endpoint  string `toml:"endpoint"` // host[:port], e.g. rustfs:9000 or s3.amazonaws.com
	Bucket    string `toml:"bucket"`
	Region    string `toml:"region"`
	UseSSL    bool   `toml:"use_ssl"`
	AccessKey string `toml:"access_key" secret:"true"`
	SecretKey string `toml:"secret_key" secret:"true"`
}

// Duration is a time.Duration written as a Go duration string ("15s", "168h").
type Duration time.Duration

// UnmarshalText parses a duration string.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// MarshalText formats the duration.
func (d Duration) MarshalText() ([]byte, error) { return []byte(time.Duration(d).String()), nil }

// Std returns the value as a time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

// Default returns the built-in defaults.
func Default() *Config {
	return &Config{
		App: App{Env: EnvDevelopment, DataDir: "./data"},
		HTTP: HTTP{
			Addr:            ":8080",
			PublicURL:       "http://localhost:8080",
			BodyLimitBytes:  1 << 20,
			ShutdownTimeout: Duration(15 * time.Second),
		},
		Database: Database{
			Driver:         DriverSQLite,
			Host:           "localhost",
			Name:           "meethub",
			User:           "meethub",
			ConnectTimeout: Duration(30 * time.Second),
			MaxOpenConns:   20,
		},
		Redis:   Redis{Addr: "localhost:6379", ConnectTimeout: Duration(30 * time.Second)},
		Session: Session{IdleTimeout: Duration(7 * 24 * time.Hour), AbsoluteLifetime: Duration(30 * 24 * time.Hour)},
		Log:     Log{Level: "info", Format: "json"},
		Files: Files{
			Driver: FilesLocal, MaxSizeBytes: 50 << 20, MaxPerPost: 10,
			S3: FilesS3T{Region: "us-east-1", UseSSL: true},
		},
	}
}

// FilesDir is the local driver's directory: files.local.dir or <data_dir>/files.
func (c *Config) FilesDir() string {
	if c.Files.Local.Dir != "" {
		return c.Files.Local.Dir
	}
	return filepath.Join(c.App.DataDir, "files")
}

// Production reports whether app.env is production.
func (c *Config) Production() bool { return c.App.Env == EnvProduction }

// SQLitePath is the SQLite database file: database.path or <data_dir>/meethub.db.
func (c *Config) SQLitePath() string {
	if c.Database.Path != "" {
		return c.Database.Path
	}
	return filepath.Join(c.App.DataDir, "meethub.db")
}

// DatabasePort is database.port or the driver's default port.
func (c *Config) DatabasePort() int {
	if c.Database.Port != 0 {
		return c.Database.Port
	}
	if c.Database.Driver == DriverPostgres {
		return 5432
	}
	return 3306
}

// PublicOrigin is the scheme://host[:port] of http.public_url.
func (c *Config) PublicOrigin() string {
	u, err := url.Parse(c.HTTP.PublicURL)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// SecureCookies reports whether cookies must be Secure (public URL is HTTPS).
func (c *Config) SecureCookies() bool { return strings.HasPrefix(c.HTTP.PublicURL, "https://") }

// TrustedProxyPrefixes parses http.trusted_proxies (validated by Validate).
func (c *Config) TrustedProxyPrefixes() []netip.Prefix {
	out := make([]netip.Prefix, 0, len(c.HTTP.TrustedProxies))
	for _, s := range c.HTTP.TrustedProxies {
		if p, err := parsePrefix(s); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// Validate checks every setting and returns all problems joined.
func (c *Config) Validate() error {
	var errs []error
	bad := func(key, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", key, fmt.Sprintf(format, args...)))
	}

	if !slices.Contains([]string{EnvDevelopment, EnvProduction}, c.App.Env) {
		bad("app.env", "must be %q or %q, got %q", EnvDevelopment, EnvProduction, c.App.Env)
	}
	if c.App.DataDir == "" {
		bad("app.data_dir", "must not be empty")
	}

	if _, _, err := net.SplitHostPort(c.HTTP.Addr); err != nil {
		bad("http.addr", "must be host:port or :port, got %q", c.HTTP.Addr)
	}
	if u, err := url.Parse(c.HTTP.PublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		bad("http.public_url", "must be an absolute http(s) URL, got %q", c.HTTP.PublicURL)
	} else if c.Production() && u.Scheme != "https" {
		bad("http.public_url", "must use https:// when app.env is production")
	}
	for _, p := range c.HTTP.TrustedProxies {
		if _, err := parsePrefix(p); err != nil {
			bad("http.trusted_proxies", "%q is not an IP address or CIDR", p)
		}
	}
	if c.HTTP.BodyLimitBytes <= 0 {
		bad("http.body_limit_bytes", "must be positive")
	}
	if c.HTTP.ShutdownTimeout <= 0 {
		bad("http.shutdown_timeout", "must be positive")
	}

	switch c.Database.Driver {
	case DriverSQLite:
	case DriverMariaDB, DriverPostgres:
		if c.Database.Host == "" {
			bad("database.host", "is required for %s", c.Database.Driver)
		}
		if c.Database.Name == "" {
			bad("database.name", "is required for %s", c.Database.Driver)
		}
		if c.Database.User == "" {
			bad("database.user", "is required for %s", c.Database.Driver)
		}
	default:
		bad("database.driver", "must be %q, %q or %q, got %q", DriverSQLite, DriverMariaDB, DriverPostgres, c.Database.Driver)
	}
	if c.Database.Port < 0 || c.Database.Port > 65535 {
		bad("database.port", "must be between 0 and 65535")
	}
	if c.Database.ConnectTimeout <= 0 {
		bad("database.connect_timeout", "must be positive")
	}
	if c.Database.MaxOpenConns <= 0 {
		bad("database.max_open_conns", "must be positive")
	}

	if _, _, err := net.SplitHostPort(c.Redis.Addr); err != nil {
		bad("redis.addr", "must be host:port, got %q", c.Redis.Addr)
	}
	if c.Redis.DB < 0 {
		bad("redis.db", "must not be negative")
	}
	if c.Redis.ConnectTimeout <= 0 {
		bad("redis.connect_timeout", "must be positive")
	}

	if c.Session.IdleTimeout <= 0 {
		bad("session.idle_timeout", "must be positive")
	}
	if c.Session.AbsoluteLifetime < c.Session.IdleTimeout {
		bad("session.absolute_lifetime", "must not be shorter than session.idle_timeout")
	}

	switch c.Files.Driver {
	case FilesLocal:
	case FilesS3:
		if c.Files.S3.Endpoint == "" || c.Files.S3.Bucket == "" {
			bad("files.s3", "endpoint and bucket are required for the s3 driver")
		}
		if c.Files.S3.AccessKey == "" || c.Files.S3.SecretKey == "" {
			bad("files.s3", "the credentials are required: set %sFILES_S3_ACCESS_KEY and %sFILES_S3_SECRET_KEY", EnvPrefix, EnvPrefix)
		}
	default:
		bad("files.driver", "must be %q or %q, got %q", FilesLocal, FilesS3, c.Files.Driver)
	}
	if c.Files.MaxSizeBytes <= 0 {
		bad("files.max_size_bytes", "must be positive")
	}
	if c.Files.MaxPerPost <= 0 || c.Files.MaxPerPost > 50 {
		bad("files.max_per_post", "must be between 1 and 50")
	}

	if !slices.Contains([]string{"debug", "info", "warn", "error"}, c.Log.Level) {
		bad("log.level", "must be debug, info, warn or error, got %q", c.Log.Level)
	}
	if c.Log.Format != "json" && c.Log.Format != "text" {
		bad("log.format", "must be json or text, got %q", c.Log.Format)
	}
	return errors.Join(errs...)
}

func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}
