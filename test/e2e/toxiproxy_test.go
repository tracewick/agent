//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	envPyroscopeURL      = "PYROSCOPE_URL"
	envPyroscopeToxicURL = "PYROSCOPE_TOXIC_URL"
	envToxiproxyURL      = "TOXIPROXY_URL"
	proxyPyroscope       = "pyroscope"
)

func standEnv() (pyroscope, toxic, toxiAPI string, ok bool) {
	pyroscope = strings.TrimSpace(os.Getenv(envPyroscopeURL))
	toxic = strings.TrimSpace(os.Getenv(envPyroscopeToxicURL))
	toxiAPI = strings.TrimSpace(os.Getenv(envToxiproxyURL))
	ok = pyroscope != "" && toxic != "" && toxiAPI != ""
	return pyroscope, toxic, toxiAPI, ok
}

// toxiproxy is a minimal client for the stand API (parity with main-repo Toxiproxy.php).
type toxiproxy struct {
	api string
	hc  *http.Client
}

func newToxiproxy(api string) *toxiproxy {
	return &toxiproxy{
		api: strings.TrimRight(api, "/"),
		hc:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (t *toxiproxy) reset() error {
	return t.req(http.MethodPost, "/reset", nil)
}

func (t *toxiproxy) addToxic(proxy, typ, stream string, attrs map[string]int) error {
	body := map[string]any{
		"name":       typ + "_" + stream,
		"type":       typ,
		"stream":     stream,
		"attributes": attrs,
	}
	return t.req(http.MethodPost, "/proxies/"+proxy+"/toxics", body)
}

func (t *toxiproxy) req(method, path string, body any) error {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, t.api+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := t.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("toxiproxy %s %s: %s %s", method, path, res.Status, payload)
	}
	return nil
}
