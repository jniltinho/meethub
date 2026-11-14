package tlsutil_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jniltinho/meethub/internal/tlsutil"
	"github.com/jniltinho/meethub/internal/tlsutil/tlstest"
)

// serve starts an HTTPS server with cfg and returns its URL rewritten to host (which resolves to it).
func serve(t *testing.T, cfg *tls.Config) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	srv.TLS = cfg
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// get requests the server as name, trusting only the certificates in roots (nil: the system's), and returns the
// status and how many certificates the server sent.
func get(t *testing.T, srv *httptest.Server, name string, roots *x509.CertPool) (status, sent int, err error) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: name, MinVersion: tls.VersionTLS12},
	}}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, len(resp.TLS.PeerCertificates), nil
}

func poolOf(t *testing.T, file string) *x509.CertPool {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(b) {
		t.Fatalf("no certificate in %s", file)
	}
	return pool
}

func TestSelfSigned(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "tls")
	cert, key := filepath.Join(dir, "selfsigned.crt"), filepath.Join(dir, "selfsigned.key")
	now := time.Now()
	hosts := []string{"chat.example.com"}

	created, err := tlsutil.EnsureSelfSigned(cert, key, hosts, now)
	if err != nil || !created {
		t.Fatalf("first start: created %v, %v", created, err)
	}
	if info, err := os.Stat(key); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the key must be readable by its owner only: %v %v", info, err)
	}
	cfg, first, err := tlsutil.Load(cert, key)
	if err != nil {
		t.Fatal(err)
	}

	// kept across restarts: the same fingerprint, so a device that trusted it keeps trusting it
	if created, err := tlsutil.EnsureSelfSigned(cert, key, hosts, now.Add(24*time.Hour)); err != nil || created {
		t.Fatalf("second start: created %v, %v", created, err)
	}
	if _, again, _ := tlsutil.Load(cert, key); again != first {
		t.Fatalf("the fingerprint changed across a restart: %s then %s", first, again)
	}

	// served: trusted once the certificate itself is trusted, under the public name, localhost and the loopback
	srv := serve(t, cfg)
	for _, name := range []string{"chat.example.com", "localhost", "127.0.0.1"} {
		if status, _, err := get(t, srv, name, poolOf(t, cert)); err != nil || status != http.StatusOK {
			t.Errorf("%s with the certificate trusted: %d, %v", name, status, err)
		}
	}
	if _, _, err := get(t, srv, "chat.example.com", nil); err == nil {
		t.Error("a client that does not trust the self-signed certificate must refuse it")
	}
	if _, _, err := get(t, srv, "other.example.com", poolOf(t, cert)); err == nil {
		t.Error("the certificate must not be valid for a name it does not carry")
	}

	// a restored copy with loose permissions is tightened again, without a new certificate
	if err := os.Chmod(key, 0o644); err != nil { //nolint:gosec // the loose mode is what the test repairs
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // idem
		t.Fatal(err)
	}
	if created, err := tlsutil.EnsureSelfSigned(cert, key, hosts, now); err != nil || created {
		t.Fatalf("loose permissions: created %v, %v", created, err)
	}
	for file, want := range map[string]os.FileMode{dir: 0o700, key: 0o600, cert: 0o600} {
		if info, err := os.Stat(file); err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: %v %v, want %o", file, info.Mode().Perm(), err, want)
		}
	}

	// replaced when the public name changes, before it expires, and when it is not valid yet (it was made while
	// the clock was ahead)
	other := []string{"team.example.org"}
	if created, err := tlsutil.EnsureSelfSigned(cert, key, other, now); err != nil || !created {
		t.Fatalf("another public name: created %v, %v", created, err)
	}
	if created, err := tlsutil.EnsureSelfSigned(cert, key, other, now.Add(800*24*time.Hour)); err != nil || !created {
		t.Fatalf("25 days before the end: created %v, %v", created, err)
	}
	if created, err := tlsutil.EnsureSelfSigned(cert, key, other, now); err != nil || !created {
		t.Fatalf("a certificate from the future: created %v, %v", created, err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// Several servers starting on the same data directory end with one pair that matches.
func TestSelfSignedAtOnce(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "tls")
	cert, key := filepath.Join(dir, "selfsigned.crt"), filepath.Join(dir, "selfsigned.key")
	var wg sync.WaitGroup
	made := make(chan bool, 8)
	for range 8 {
		wg.Go(func() {
			created, err := tlsutil.EnsureSelfSigned(cert, key, []string{"chat.example.com"}, time.Now())
			if err != nil {
				t.Error(err)
			}
			made <- created
		})
	}
	wg.Wait()
	close(made)
	n := 0
	for created := range made {
		if created {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d of 8 starts generated a certificate, want exactly one", n)
	}
	if _, _, err := tlsutil.Load(cert, key); err != nil {
		t.Errorf("the pair left behind does not load: %v", err)
	}
}

func TestSignedChain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	authority := tlstest.NewAuthority(t)
	certFile, keyFile := authority.Issue(t, dir, "chat.example.com")

	cfg, fingerprint, err := tlsutil.Load(certFile, keyFile)
	if err != nil || len(fingerprint) != 64 {
		t.Fatalf("load: %q, %v", fingerprint, err)
	}
	srv := serve(t, cfg)

	// a client that trusts the authority, and nothing else, accepts the server: the whole chain is served
	status, sent, err := get(t, srv, "chat.example.com", authority.Pool())
	if err != nil || status != http.StatusOK {
		t.Fatalf("with the authority trusted: %d, %v", status, err)
	}
	if sent != 2 {
		t.Errorf("the server sent %d certificates, want the leaf and the authority", sent)
	}
	if _, _, err := get(t, srv, "localhost", authority.Pool()); err == nil {
		t.Error("a signed certificate for chat.example.com must not be accepted as localhost")
	}
	if _, _, err := get(t, srv, "chat.example.com", tlstest.NewAuthority(t).Pool()); err == nil {
		t.Error("a client that trusts another authority must refuse the server")
	}

	// a key that does not belong to the certificate is refused at startup, not at the first request
	otherCert, otherKey := authority.Issue(t, t.TempDir(), "chat.example.com")
	if _, _, err := tlsutil.Load(certFile, otherKey); err == nil {
		t.Error("a certificate with another certificate's key must not load")
	}
	if _, _, err := tlsutil.Load(otherCert, filepath.Join(dir, "missing.pem")); err == nil || !strings.Contains(err.Error(), "missing.pem") {
		t.Errorf("a missing key file must be named: %v", err)
	}
}
