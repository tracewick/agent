// Package httputil builds shared HTTP client pieces (TLS roots, pooled transport).
package httputil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// TLSOptions configures outbound TLS verification for the Pyroscope client.
type TLSOptions struct {
	// CAFile is an optional PEM file with extra root CAs (appended to the system pool when possible).
	CAFile string
}

// ClientTLSConfig returns a tls.Config with MinVersion TLS 1.2.
// Empty CAFile keeps system roots (nil Config lets crypto/tls use defaults).
func ClientTLSConfig(opts TLSOptions) (*tls.Config, error) {
	if opts.CAFile == "" {
		return &tls.Config{MinVersion: tls.VersionTLS12}, nil //nolint:gosec // MinVersion set; Roots = system
	}
	pem, err := os.ReadFile(opts.CAFile)
	if err != nil {
		return nil, fmt.Errorf("httputil: read TLS CA file: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("httputil: TLS CA file %q contains no certificates", opts.CAFile)
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
	}, nil
}

// NewHTTPClient returns a pooled HTTP client with optional custom CA.
func NewHTTPClient(timeout time.Duration, opts TLSOptions) (*http.Client, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	tlsCfg, err := ClientTLSConfig(opts)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig:       tlsCfg,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// Never follow redirects: a 302→200 HTML login page must not become a false ACK.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}
