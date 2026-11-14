// Package tlsutil builds the server's own TLS setup: a certificate the install provides (http.cert_file and
// http.key_file — a chain signed by any authority, or a self-signed one) or, with http.self_signed, one generated
// on the first start and kept, so its fingerprint stays the same across restarts.
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	// selfSignedFor is how long a generated certificate lasts: the longest Apple's platforms accept
	selfSignedFor = 825 * 24 * time.Hour
	// renewBefore is how long before its end a generated certificate is replaced
	renewBefore = 30 * 24 * time.Hour
)

// Load returns the TLS configuration serving the certificate chain in certFile with the key in keyFile, and the
// SHA-256 fingerprint of the leaf certificate (what a browser shows).
func Load(certFile, keyFile string) (*tls.Config, string, error) {
	c, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(c.Certificate[0])
	return &tls.Config{
		Certificates: []tls.Certificate{c},
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2", "http/1.1"},
	}, hex.EncodeToString(sum[:]), nil
}

// EnsureSelfSigned leaves in certFile and keyFile a self-signed pair that names every host and is valid now and
// for more than 30 days, generating a new one when there is none or the present one is not; it reports whether
// it generated one (the fingerprint changed). The directory and the files end up readable by their owner only,
// also when they already existed. Two processes starting at once take turns: the second finds the first's pair.
func EnsureSelfSigned(certFile, keyFile string, hosts []string, now time.Time) (bool, error) {
	dir := filepath.Dir(certFile)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // a directory needs its search bit; owner only
		return false, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // inside the directory just secured
	if err != nil {
		return false, err
	}
	defer func() { _ = lock.Close() }() // closing releases the lock
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return false, fmt.Errorf("lock %s: %w", lock.Name(), err)
	}

	if covers(certFile, keyFile, hosts, now) {
		for _, f := range []string{certFile, keyFile} {
			if err := os.Chmod(f, 0o600); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	return true, generate(certFile, keyFile, hosts, now)
}

func covers(certFile, keyFile string, hosts []string, now time.Time) bool {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile) // also refuses a certificate with another one's key
	if err != nil {
		return false
	}
	c, err := x509.ParseCertificate(pair.Certificate[0])
	// not yet valid: it was made while the clock was ahead, and no client accepts it now
	if err != nil || now.Before(c.NotBefore) || c.NotAfter.Sub(now) < renewBefore {
		return false
	}
	for _, h := range hosts {
		if c.VerifyHostname(h) != nil {
			return false
		}
	}
	return true
}

// generate writes an ECDSA P-256 pair for hosts, localhost, this machine's name and its addresses.
func generate(certFile, keyFile string, hosts []string, now time.Time) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	names := append([]string{}, hosts...)
	names = append(names, "localhost", "127.0.0.1", "::1")
	if h, err := os.Hostname(); err == nil && h != "" {
		names = append(names, h)
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && !n.IP.IsLinkLocalUnicast() {
				names = append(names, n.IP.String())
			}
		}
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return err
	}
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: names[0], Organization: []string{"MeetHub (self-signed)"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(selfSignedFor),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	seen := map[string]bool{}
	for _, n := range names {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		if ip := net.ParseIP(n); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else {
			tpl.DNSNames = append(tpl.DNSNames, n)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	// each file appears whole or not at all; a stop between the two leaves a pair that does not match, which the
	// next start replaces
	if err := writeFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})); err != nil {
		return fmt.Errorf("write the key: %w", err)
	}
	if err := writeFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
		return fmt.Errorf("write the certificate: %w", err)
	}
	return nil
}

// writeFile puts data in name through a temporary file beside it (created with mode 0600) and a rename.
func writeFile(name string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(name), filepath.Base(name)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // a no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), name)
}
