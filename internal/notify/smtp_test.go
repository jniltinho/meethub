package notify_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/config"
	"github.com/jniltinho/meethub/internal/notify"
)

// fakeSMTP is a test SMTP server: EHLO, STARTTLS (or implicit TLS), AUTH PLAIN and LOGIN, MAIL, RCPT, DATA, QUIT.
type fakeSMTP struct {
	ln        net.Listener
	tls       *tls.Config
	implicit  bool // TLS from the first byte
	noStart   bool // does not offer STARTTLS
	user, pwd string
	mu        sync.Mutex
	mails     []string
	authed    []string
}

func newFakeSMTP(t *testing.T, implicit bool) (*fakeSMTP, *x509.CertPool) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	s := &fakeSMTP{tls: &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12},
		implicit: implicit, user: "meethub", pwd: "secret"}
	if implicit {
		s.ln, err = tls.Listen("tcp", "127.0.0.1:0", s.tls)
	} else {
		s.ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.ln.Close() })
	go s.serve()
	return s, pool
}

func (s *fakeSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *fakeSMTP) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.session(c)
	}
}

func (s *fakeSMTP) session(c net.Conn) {
	defer func() { _ = c.Close() }()
	r, w := bufio.NewReader(c), bufio.NewWriter(c)
	say := func(line string) { _, _ = w.WriteString(line + "\r\n"); _ = w.Flush() }
	secure := s.implicit
	say("220 localhost ESMTP fake")
	var data strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimRight(line, "\r\n")
		up := strings.ToUpper(cmd)
		switch {
		case strings.HasPrefix(up, "EHLO"):
			if !secure && !s.noStart {
				say("250-localhost")
				say("250-STARTTLS")
				say("250 AUTH PLAIN LOGIN")
			} else {
				say("250-localhost")
				say("250 AUTH PLAIN LOGIN")
			}
		case up == "STARTTLS":
			say("220 go ahead")
			tc := tls.Server(c, s.tls)
			if tc.Handshake() != nil {
				return
			}
			c, secure = tc, true
			r, w = bufio.NewReader(c), bufio.NewWriter(c)
		case strings.HasPrefix(up, "AUTH PLAIN"):
			b, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(cmd[len("AUTH PLAIN"):]))
			s.check(say, string(b) == "\x00"+s.user+"\x00"+s.pwd, "PLAIN")
		case strings.HasPrefix(up, "AUTH LOGIN"):
			say("334 VXNlcm5hbWU6")
			u, _ := r.ReadString('\n')
			say("334 UGFzc3dvcmQ6")
			p, _ := r.ReadString('\n')
			ub, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(u))
			pb, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(p))
			s.check(say, string(ub) == s.user && string(pb) == s.pwd, "LOGIN")
		case strings.HasPrefix(up, "MAIL FROM"), strings.HasPrefix(up, "RCPT TO"):
			say("250 ok")
		case up == "DATA":
			say("354 end with .")
			data.Reset()
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				data.WriteString(l)
			}
			s.mu.Lock()
			s.mails = append(s.mails, data.String())
			s.mu.Unlock()
			say("250 queued")
		case up == "QUIT":
			say("221 bye")
			return
		default:
			say("502 unknown")
		}
	}
}

func (s *fakeSMTP) check(say func(string), ok bool, mech string) {
	if ok {
		s.mu.Lock()
		s.authed = append(s.authed, mech)
		s.mu.Unlock()
		say("235 ok")
		return
	}
	say("535 bad credentials")
}

func (s *fakeSMTP) received() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.mails...)
}

func TestSMTPSender(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	msg := []byte("Subject: hi\r\n\r\nhello\r\n")
	sender := func(port int, mode string, pool *x509.CertPool, pwd string) *notify.SMTPSender {
		return &notify.SMTPSender{
			Config:  config.SMTP{Host: "127.0.0.1", Port: port, Username: "meethub", Password: pwd, From: "MeetHub <m@example.com>", TLS: mode},
			TLS:     &tls.Config{RootCAs: pool, ServerName: "localhost", MinVersion: tls.VersionTLS12},
			Timeout: 5 * time.Second,
		}
	}
	t.Run("starttls and authentication", func(t *testing.T) {
		t.Parallel()
		srv, pool := newFakeSMTP(t, false)
		if err := sender(srv.port(), config.SMTPStartTLS, pool, "secret").Send(ctx, "ana@example.com", msg); err != nil {
			t.Fatal(err)
		}
		if got := srv.received(); len(got) != 1 || !strings.Contains(got[0], "hello") || len(srv.authed) != 1 {
			t.Fatalf("received %q, auth %v", got, srv.authed)
		}
	})
	t.Run("implicit tls", func(t *testing.T) {
		t.Parallel()
		srv, pool := newFakeSMTP(t, true)
		if err := sender(srv.port(), config.SMTPTLS, pool, "secret").Send(ctx, "ana@example.com", msg); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("wrong password", func(t *testing.T) {
		t.Parallel()
		srv, pool := newFakeSMTP(t, false)
		err := sender(srv.port(), config.SMTPStartTLS, pool, "nope").Send(ctx, "ana@example.com", msg)
		if err == nil || len(srv.received()) != 0 || !strings.Contains(err.Error(), "535") || strings.Contains(err.Error(), "credentials") {
			t.Fatalf("sent with a wrong password, or the reply's text in the error: %v", err)
		}
	})
	t.Run("no starttls offered", func(t *testing.T) {
		t.Parallel()
		srv, pool := newFakeSMTP(t, false)
		srv.noStart = true
		if err := sender(srv.port(), config.SMTPStartTLS, pool, "secret").Send(ctx, "ana@example.com", msg); !errors.Is(err, notify.ErrNoStartTLS) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("an untrusted certificate", func(t *testing.T) {
		t.Parallel()
		srv, _ := newFakeSMTP(t, false)
		if err := sender(srv.port(), config.SMTPStartTLS, x509.NewCertPool(), "secret").Send(ctx, "ana@example.com", msg); err == nil {
			t.Fatal("sent over an untrusted certificate")
		}
	})
	t.Run("refused connection", func(t *testing.T) {
		t.Parallel()
		ln, _ := net.Listen("tcp", "127.0.0.1:0")
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		err := sender(port, config.SMTPStartTLS, nil, "secret").Send(ctx, "ana@example.com", msg)
		if err == nil || !strings.Contains(err.Error(), "connect") || strings.Contains(err.Error(), "127.0.0.1") {
			t.Fatalf("got %v", err)
		}
	})
}
