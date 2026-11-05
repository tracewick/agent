//go:build e2e

package e2e_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestStandPyroscopeReady(t *testing.T) {
	pyroscope, toxic, toxiAPI, ok := standEnv()
	if !ok {
		t.Skipf("stand env unset (%s, %s, %s); run make stand-up", envPyroscopeURL, envPyroscopeToxicURL, envToxiproxyURL)
	}
	logf(t, "stand ready check pyroscope=%s toxic=%s toxiAPI=%s", pyroscope, toxic, toxiAPI)
	for _, u := range []string{pyroscope + "/ready", toxic + "/ready"} {
		assertReady(t, u)
		logf(t, "GET %s OK", u)
	}
	tx := newToxiproxy(toxiAPI)
	if err := tx.reset(); err != nil {
		t.Fatal(err)
	}
	logf(t, "toxiproxy reset OK")
}

func assertReady(t *testing.T, url string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode < 200 || res.StatusCode > 299 {
		t.Fatalf("%s: %s", url, res.Status)
	}
}
