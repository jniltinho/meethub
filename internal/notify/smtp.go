package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"slices"
	"strings"
	"time"

	"github.com/jniltinho/meethub/internal/config"
)

// Sender delivers one e-mail.
type Sender interface {
	Send(ctx context.Context, to string, msg []byte) error
}

// SMTPSender sends through the configured SMTP server with the standard
// library (design D3): STARTTLS or implicit TLS, PLAIN or LOGIN
// authentication, 30 seconds for the whole exchange.
type SMTPSender struct {
	Config config.SMTP
	// TLS overrides the TLS settings (tests trust their own certificate); nil: the system roots.
	TLS     *tls.Config
	Timeout time.Duration // default 30s
}

// ErrNoStartTLS is returned when the server does not offer STARTTLS in the starttls mode.
var ErrNoStartTLS = errors.New("smtp: the server does not offer STARTTLS")

// From is the sender's address of the From header.
func (s *SMTPSender) From() string { return s.Config.From }

// Send delivers msg (headers and body) to one address.
func (s *SMTPSender) Send(ctx context.Context, to string, msg []byte) error {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cfg := s.tlsConfig()
	d := &net.Dialer{}
	var (
		conn net.Conn
		err  error
	)
	if s.Config.TLS == config.SMTPTLS {
		conn, err = (&tls.Dialer{NetDialer: d, Config: cfg}).DialContext(ctx, "tcp", s.Config.Addr())
	} else {
		conn, err = d.DialContext(ctx, "tcp", s.Config.Addr())
	}
	if err != nil {
		return fmt.Errorf("smtp: connect: %w", err)
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, s.Config.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp: greeting: %w", err)
	}
	defer func() { _ = c.Close() }()
	if s.Config.TLS == config.SMTPStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return ErrNoStartTLS
		}
		if err := c.StartTLS(cfg); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
	}
	if s.Config.Username != "" {
		if err := c.Auth(s.auth(c)); err != nil {
			return fmt.Errorf("smtp: authentication: %w", err)
		}
	}
	from, err := mail.ParseAddress(s.Config.From)
	if err != nil {
		return fmt.Errorf("smtp: from: %w", err)
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("smtp: MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("smtp: RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: end of data: %w", err)
	}
	return c.Quit()
}

func (s *SMTPSender) tlsConfig() *tls.Config {
	if s.TLS != nil {
		c := s.TLS.Clone()
		if c.ServerName == "" {
			c.ServerName = s.Config.Host
		}
		return c
	}
	return &tls.Config{ServerName: s.Config.Host, MinVersion: tls.VersionTLS12}
}

// auth picks PLAIN when offered, else LOGIN.
func (s *SMTPSender) auth(c *smtp.Client) smtp.Auth {
	_, mechs := c.Extension("AUTH")
	if slices.Contains(strings.Fields(strings.ToUpper(mechs)), "PLAIN") || mechs == "" {
		return plainAuth{user: s.Config.Username, pass: s.Config.Password}
	}
	return &loginAuth{user: s.Config.Username, pass: s.Config.Password}
}

// plainAuth is AUTH PLAIN (RFC 4616). Unlike smtp.PlainAuth it also allows the
// tls = "none" mode, which configuration allows only towards loopback and
// private hosts.
type plainAuth struct{ user, pass string }

func (a plainAuth) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "PLAIN", []byte("\x00" + a.user + "\x00" + a.pass), nil
}

func (plainAuth) Next(_ []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("smtp: unexpected PLAIN challenge")
	}
	return nil, nil
}

// loginAuth is AUTH LOGIN (the username, then the password, each on request).
type loginAuth struct {
	user, pass string
	step       int
}

func (a *loginAuth) Start(*smtp.ServerInfo) (string, []byte, error) { return "LOGIN", nil, nil }

func (a *loginAuth) Next(_ []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	a.step++
	switch a.step {
	case 1:
		return []byte(a.user), nil
	case 2:
		return []byte(a.pass), nil
	}
	return nil, errors.New("smtp: unexpected LOGIN challenge")
}
