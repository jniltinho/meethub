package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/jniltinho/meethub/internal/config"
)

// healthcheckTimeout bounds the whole probe.
const healthcheckTimeout = 3 * time.Second

func newHealthcheckCmd(c *cli) *cobra.Command {
	return &cobra.Command{
		Use:   "healthcheck",
		Short: "Probe the local server's liveness endpoint (for container health checks)",
		Long: "Requests /api/v1/health on the address the server listens on (http.addr; a wildcard host becomes\n" +
			"127.0.0.1) and exits 0 when it answers 200 within 3 seconds, 1 otherwise. Proxy settings are ignored.\n" +
			"When the server speaks HTTPS itself, the probe does too and accepts only the server's own certificate.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := c.config()
			if err != nil {
				return err
			}
			tlsConfig, err := healthTLS(cfg)
			if err != nil {
				return err
			}
			u, err := healthURL(cfg.HTTP.Addr, tlsConfig != nil)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), healthcheckTimeout)
			defer cancel()
			if err := probe(ctx, u, tlsConfig); err != nil {
				return err
			}
			_, err = fmt.Fprintln(c.out, "ok")
			return err
		},
	}
}

// healthTLS returns what the probe trusts when the server speaks HTTPS itself: exactly the certificate it is
// configured to serve, under the public URL's name (the probe connects to the loopback, which a certificate
// rarely names). Nil for a server on plain HTTP.
func healthTLS(cfg *config.Config) (*tls.Config, error) {
	cert, _ := cfg.TLSFiles()
	if cert == "" {
		return nil, nil
	}
	chain, err := os.ReadFile(cert) //nolint:gosec // the path is the install's own setting
	if err != nil {
		return nil, fmt.Errorf("health probe: %w", err)
	}
	block, _ := pem.Decode(chain) // the server's own certificate comes first
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("health probe: no certificate in %s", cert)
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("health probe: %s: %w", cert, err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return &tls.Config{
		RootCAs: pool, ServerName: cfg.PublicHost(), MinVersion: tls.VersionTLS12,
		// exactly that certificate: not another one for the same name, were it from the same authority
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 || !cs.PeerCertificates[0].Equal(leaf) {
				return errors.New("the server on this port does not serve the configured certificate")
			}
			return nil
		},
	}, nil
}

// healthURL turns a listen address into the local health URL.
func healthURL(addr string, https bool) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("http.addr %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
		if ip != nil && ip.To4() == nil {
			host = "::1"
		}
	}
	scheme := "http://"
	if https {
		scheme = "https://"
	}
	return scheme + net.JoinHostPort(host, port) + "/api/v1/health", nil
}

func probe(ctx context.Context, u string, tlsConfig *tls.Config) error {
	client := &http.Client{Transport: &http.Transport{Proxy: nil, TLSClientConfig: tlsConfig}} // ignore HTTP(S)_PROXY
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("health probe: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errors.New("health probe: status " + resp.Status)
	}
	return nil
}
