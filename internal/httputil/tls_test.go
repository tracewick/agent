package httputil_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/httputil"
)

func TestNewHTTPClientSystemRoots(t *testing.T) {
	t.Parallel()
	c, err := httputil.NewHTTPClient(0, httputil.TLSOptions{})
	if err != nil || c == nil || c.Timeout <= 0 {
		t.Fatalf("%v %#v", err, c)
	}
	if c.CheckRedirect == nil {
		t.Fatal("CheckRedirect must refuse following redirects")
	}
}

func TestNewHTTPClientDoesNotFollowRedirect(t *testing.T) {
	t.Parallel()
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("login html"))
	}))
	t.Cleanup(final.Close)
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	t.Cleanup(redir.Close)

	client, err := httputil.NewHTTPClient(5*time.Second, httputil.TLSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(redir.URL) //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d want 302 (no follow)", resp.StatusCode)
	}
}

func TestNewHTTPClientCustomCA(t *testing.T) {
	t.Parallel()
	caPEM, certPEM, keyPEM := mustSelfSigned(t)

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil { //nolint:gosec // temp fixture
		t.Fatal(err)
	}

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}, MinVersion: tls.VersionTLS12} //nolint:gosec // test server
	srv.StartTLS()
	t.Cleanup(srv.Close)

	client, err := httputil.NewHTTPClient(5*time.Second, httputil.TLSOptions{CAFile: caFile})
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close() //nolint:errcheck // test cleanup
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", res.StatusCode)
	}

	// Without the custom CA the handshake must fail against this self-signed cert.
	bare, err := httputil.NewHTTPClient(2*time.Second, httputil.TLSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bare.Get(srv.URL + "/"); err == nil {
		t.Fatal("expected TLS verify failure without CA")
	}
}

func TestClientTLSConfigErrors(t *testing.T) {
	t.Parallel()
	if _, err := httputil.ClientTLSConfig(httputil.TLSOptions{CAFile: filepath.Join(t.TempDir(), "missing.pem")}); err == nil {
		t.Fatal("missing file")
	}
	bad := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(bad, []byte("not-a-cert"), 0o600); err != nil { //nolint:gosec // temp fixture
		t.Fatal(err)
	}
	if _, err := httputil.ClientTLSConfig(httputil.TLSOptions{CAFile: bad}); err == nil {
		t.Fatal("bad pem")
	}
}

func mustSelfSigned(t *testing.T) (caPEM, certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "tracewick-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, certPEM, keyPEM
}
