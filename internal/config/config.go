// Package config loads MeetHub's settings: defaults, then a TOML file, then
// MEETHUB_* environment variables. Secrets are accepted only from the
// environment (or from a file named by the variable with a _FILE suffix) and
// are never part of the TOML file, logs or API responses.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/netip"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
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
	LiveKit  LiveKit  `toml:"livekit"`
	SMTP     SMTP     `toml:"smtp"`
	// Recordings configures meeting recordings (add-meeting-recordings); off by default.
	Recordings Recordings `toml:"recordings"`
	// Notifications configures what notification e-mails carry.
	Notifications Notifications `toml:"notifications"`
	// Secret holds the key that encrypts stored secrets (authenticator apps).
	Secret Secret `toml:"secret"`
	// Auth says which logins are offered; OIDC is the single sign-on provider.
	Auth Auth `toml:"auth"`
	OIDC OIDC `toml:"oidc"`
}

// Auth selects the login methods.
type Auth struct {
	PasswordLogin bool `toml:"password_login"` // false: only single sign-on (setup and the CLI still work)
}

// OIDC is one OpenID Connect provider for single sign-on; an empty issuer: none.
type OIDC struct {
	Issuer         string   `toml:"issuer"` // https:// URL; the redirect URI is <public_url>/api/v1/auth/oidc/callback
	ClientID       string   `toml:"client_id"`
	ClientSecret   string   `toml:"client_secret" secret:"true"`
	Name           string   `toml:"name"`            // the button: "Sign in with <name>"
	Scopes         []string `toml:"scopes"`          // openid is always asked for
	AutoCreate     bool     `toml:"auto_create"`     // create unknown people with a verified e-mail
	AllowedDomains []string `toml:"allowed_domains"` // e-mail domains auto_create accepts; empty: any
	DefaultRole    string   `toml:"default_role"`    // member or guest
}

// Enabled reports whether single sign-on is configured.
func (o OIDC) Enabled() bool { return o.Issuer != "" }

// SecretKeyBytes is the length of secret.key once decoded.
const SecretKeyBytes = 32

// Secret holds MEETHUB_SECRET_KEY: 32 random bytes in base64; empty: two-factor authentication is unavailable.
type Secret struct {
	Key string `toml:"key" secret:"true"`
}

// Bytes is the decoded key; nil when unset or invalid (Validate rejects an invalid one).
func (s Secret) Bytes() []byte {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s.Key))
	if err != nil || len(b) != SecretKeyBytes {
		return nil
	}
	return b
}

// SMTP TLS modes.
const (
	SMTPStartTLS = "starttls" // plain connection upgraded with STARTTLS (port 587)
	SMTPTLS      = "tls"      // implicit TLS (port 465)
	SMTPNone     = "none"     // no encryption: loopback or private hosts only when a password is set
)

// SMTP reaches the mail server for notification e-mails; an empty host: no e-mail.
type SMTP struct {
	Host     string `toml:"host"`
	Port     int    `toml:"port"` // default 587 (starttls), 465 (tls) or 25 (none)
	Username string `toml:"username"`
	Password string `toml:"password" secret:"true"`
	From     string `toml:"from"` // the sender address, e.g. "MeetHub <meethub@example.com>"
	TLS      string `toml:"tls"`
}

// Enabled reports whether e-mail is configured.
func (s SMTP) Enabled() bool { return s.Host != "" }

// Addr is host:port with the TLS mode's default port.
func (s SMTP) Addr() string {
	port := s.Port
	if port == 0 {
		port = map[string]int{SMTPTLS: 465, SMTPNone: 25}[s.TLS]
		if port == 0 {
			port = 587
		}
	}
	return net.JoinHostPort(s.Host, strconv.Itoa(port))
}

// Notifications configures notification e-mails.
type Notifications struct {
	EmailExcerpt bool `toml:"email_excerpt"` // e-mails carry the start of messages (default true)
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
	// CertFile and KeyFile make the server speak HTTPS itself: a PEM chain from any authority, or self-signed
	CertFile string `toml:"cert_file"`
	KeyFile  string `toml:"key_file"`
	// SelfSigned, without CertFile, serves HTTPS with a certificate generated under <data_dir>/tls
	SelfSigned bool `toml:"self_signed"`
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

// LiveKit reaches the LiveKit server used by meetings; all empty: meetings are disabled.
type LiveKit struct {
	URL       string `toml:"url"`     // WebSocket URL the browsers use, e.g. wss://meet.example.com
	APIURL    string `toml:"api_url"` // HTTP URL the server uses; default: url with ws→http, wss→https
	APIKey    string `toml:"api_key" secret:"true"`
	APISecret string `toml:"api_secret" secret:"true"`
}

// Enabled reports whether meetings are configured.
func (l LiveKit) Enabled() bool { return l.URL != "" }

// ServerURL is api_url, or url with its WebSocket scheme turned into HTTP.
func (l LiveKit) ServerURL() string {
	if l.APIURL != "" {
		return l.APIURL
	}
	if rest, ok := strings.CutPrefix(l.URL, "ws"); ok {
		return "http" + rest
	}
	return l.URL
}

// BrowserOrigins are the origins browsers reach LiveKit at, in both schemes (WebSocket signalling and the
// HTTP validation request), for the CSP connect-src; nil when disabled.
func (l LiveKit) BrowserOrigins() []string {
	u, err := url.Parse(l.URL)
	if err != nil || u.Host == "" {
		return nil
	}
	httpScheme := map[string]string{"ws": "http", "wss": "https"}[u.Scheme]
	return []string{u.Scheme + "://" + u.Host, httpScheme + "://" + u.Host}
}

// Log configures the structured logger.
type Log struct {
	Level  string `toml:"level"`  // debug, info, warn, error
	Format string `toml:"format"` // json or text
}

// Recording modes, formats and who may record.
const (
	RecordAudio   = "audio"   // an .m4a of the mixed voices
	RecordVideo   = "video"   // a composed video with the mixed voices
	FormatWebM    = "webm"    // VP8 + Opus, what MeetHub's own ffmpeg produces
	FormatMP4     = "mp4"     // H.264 + AAC: needs an ffmpeg with libx264, supplied by the operator
	RecordAdmins  = "admins"  // channel administrators (and team administrators and manage_system holders who are members)
	RecordMembers = "members" // any member of the channel
)

// Recordings configures meeting recordings.
type Recordings struct {
	Enabled       bool   `toml:"enabled"`
	Mode          string `toml:"mode"`           // audio or video
	VideoFormat   string `toml:"video_format"`   // webm or mp4 (video mode)
	Start         string `toml:"start"`          // admins or members
	MaxMinutes    int    `toml:"max_minutes"`    // a recording stops at this length
	MaxBytes      int64  `toml:"max_bytes"`      // and at this size of its captures
	MaxConcurrent int    `toml:"max_concurrent"` // captures per server
	RetentionDays int    `toml:"retention_days"` // 0: recordings are kept
	FFmpegPath    string `toml:"ffmpeg_path"`    // the ffmpeg executable (a name looked up in PATH, or a path)
	Dir           string `toml:"dir"`            // the capture volume; default <data_dir>/recordings
}

// RecordingsDir is recordings.dir or <data_dir>/recordings.
func (c *Config) RecordingsDir() string {
	if c.Recordings.Dir != "" {
		return c.Recordings.Dir
	}
	return filepath.Join(c.App.DataDir, "recordings")
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
		SMTP:          SMTP{TLS: SMTPStartTLS},
		Auth:          Auth{PasswordLogin: true},
		OIDC:          OIDC{Name: "single sign-on", Scopes: []string{"openid", "email", "profile"}, DefaultRole: "member"},
		Notifications: Notifications{EmailExcerpt: true},
		Recordings: Recordings{
			Mode: RecordAudio, VideoFormat: FormatWebM, Start: RecordAdmins, MaxMinutes: 240, MaxBytes: 4 << 30,
			MaxConcurrent: 2, FFmpegPath: "ffmpeg",
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

// PublicHost returns the host of the public URL, without its port.
func (c *Config) PublicHost() string {
	u, err := url.Parse(c.HTTP.PublicURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// TLSFiles returns the certificate and the key the server serves HTTPS with: the configured pair, or the
// self-signed one under <data_dir>/tls. Both are empty when it speaks plain HTTP (TLS, if any, is a proxy's).
func (c *Config) TLSFiles() (cert, key string) {
	switch {
	case c.HTTP.CertFile != "":
		return c.HTTP.CertFile, c.HTTP.KeyFile
	case c.HTTP.SelfSigned:
		dir := filepath.Join(c.App.DataDir, "tls")
		return filepath.Join(dir, "selfsigned.crt"), filepath.Join(dir, "selfsigned.key")
	}
	return "", ""
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
	if (c.HTTP.CertFile == "") != (c.HTTP.KeyFile == "") {
		bad("http.cert_file", "and http.key_file must be set together")
	}
	if cert, _ := c.TLSFiles(); cert != "" && !c.SecureCookies() {
		bad("http.public_url", "must use https:// when the server serves TLS (http.cert_file or http.self_signed)")
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

	if lk := c.LiveKit; lk.URL != "" || lk.APIURL != "" || lk.APIKey != "" || lk.APISecret != "" {
		if u, err := url.Parse(lk.URL); err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
			bad("livekit.url", "must be an absolute ws(s) URL, got %q", lk.URL)
		} else if c.Production() && u.Scheme != "wss" {
			bad("livekit.url", "must use wss:// when app.env is production")
		}
		if u, err := url.Parse(lk.ServerURL()); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			bad("livekit.api_url", "must be an absolute http(s) URL, got %q", lk.APIURL)
		}
		if lk.APIKey == "" {
			bad("livekit.api_key", "is required with livekit.url (MEETHUB_LIVEKIT_API_KEY)")
		}
		if len(lk.APISecret) < 32 {
			bad("livekit.api_secret", "must be at least 32 bytes (MEETHUB_LIVEKIT_API_SECRET)")
		}
	}

	if r := c.Recordings; r.Enabled {
		if !c.LiveKit.Enabled() {
			bad("recordings.enabled", "needs meetings: set livekit.url and its key")
		}
		if r.Mode != RecordAudio && r.Mode != RecordVideo {
			bad("recordings.mode", "must be %q or %q, got %q", RecordAudio, RecordVideo, r.Mode)
		}
		if r.VideoFormat != FormatWebM && r.VideoFormat != FormatMP4 {
			bad("recordings.video_format", "must be %q or %q, got %q", FormatWebM, FormatMP4, r.VideoFormat)
		}
		if r.Start != RecordAdmins && r.Start != RecordMembers {
			bad("recordings.start", "must be %q or %q, got %q", RecordAdmins, RecordMembers, r.Start)
		}
		if r.MaxMinutes < 1 || r.MaxMinutes > 24*60 {
			bad("recordings.max_minutes", "must be between 1 and 1440")
		}
		if r.MaxBytes < 1<<20 {
			bad("recordings.max_bytes", "must be at least 1 MiB")
		}
		if r.MaxConcurrent < 1 {
			bad("recordings.max_concurrent", "must be positive")
		}
		if r.FFmpegPath == "" {
			bad("recordings.ffmpeg_path", "is required with recordings.enabled")
		}
	}
	if c.Recordings.RetentionDays < 0 {
		bad("recordings.retention_days", "must not be negative")
	}

	if sm := c.SMTP; sm.Host != "" || sm.Username != "" || sm.Password != "" || sm.From != "" {
		if sm.Host == "" {
			bad("smtp.host", "is required with the other smtp settings")
		}
		if a, err := mail.ParseAddress(sm.From); err != nil || a.Address == "" {
			bad("smtp.from", "must be the sender's e-mail address, got %q", sm.From)
		}
		if !slices.Contains([]string{SMTPStartTLS, SMTPTLS, SMTPNone}, sm.TLS) {
			bad("smtp.tls", "must be starttls, tls or none, got %q", sm.TLS)
		} else if sm.TLS == SMTPNone && sm.Password != "" && !privateHost(sm.Host) {
			bad("smtp.tls", "none would send the password in clear to %s: use starttls or tls", sm.Host)
		}
		if sm.Port < 0 || sm.Port > 65535 {
			bad("smtp.port", "must be between 1 and 65535")
		}
		if sm.Password != "" && sm.Username == "" {
			bad("smtp.username", "is required with MEETHUB_SMTP_PASSWORD")
		}
	}

	if o := c.OIDC; o.Issuer != "" || o.ClientID != "" || o.ClientSecret != "" {
		if u, err := url.Parse(o.Issuer); err != nil || u.Scheme != "https" || u.Host == "" {
			bad("oidc.issuer", "must be an absolute https:// URL, got %q", o.Issuer)
		}
		if o.ClientID == "" {
			bad("oidc.client_id", "is required with oidc.issuer")
		}
		if o.ClientSecret == "" {
			bad("oidc.client_secret", "is required with oidc.issuer (%sOIDC_CLIENT_SECRET)", EnvPrefix)
		}
		if strings.TrimSpace(o.Name) == "" {
			bad("oidc.name", "must not be empty")
		}
	}
	if r := c.OIDC.DefaultRole; r != "member" && r != "guest" {
		bad("oidc.default_role", "must be member or guest, got %q", r)
	}
	for _, d := range c.OIDC.AllowedDomains {
		if d == "" || strings.ContainsAny(d, "@ /") {
			bad("oidc.allowed_domains", "%q is not a domain", d)
		}
	}
	if !c.Auth.PasswordLogin && !c.OIDC.Enabled() {
		bad("auth.password_login", "can be false only with single sign-on (oidc.issuer)")
	}

	if c.Secret.Key != "" && c.Secret.Bytes() == nil { // never the value: it is a secret
		bad("secret.key", "must be %d random bytes in base64 (%sSECRET_KEY)", SecretKeyBytes, EnvPrefix)
	}

	if !slices.Contains([]string{"debug", "info", "warn", "error"}, c.Log.Level) {
		bad("log.level", "must be debug, info, warn or error, got %q", c.Log.Level)
	}
	if c.Log.Format != "json" && c.Log.Format != "text" {
		bad("log.format", "must be json or text, got %q", c.Log.Format)
	}
	return errors.Join(errs...)
}

// privateHost reports whether host is localhost or a loopback or private address (where a password may travel
// without TLS).
func privateHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && (a.IsLoopback() || a.IsPrivate())
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
