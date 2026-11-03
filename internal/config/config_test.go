package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/config"
)

func TestLoadRequiredAndDefaults(t *testing.T) {
	t.Parallel()
	env := envMap{
		"TRACEWICK_SPOOL_DIR":          "/tmp/spool",
		"TRACEWICK_PYROSCOPE_URL":      "http://pyroscope:4040",
		"TRACEWICK_PYROSCOPE_USER":     "u",
		"TRACEWICK_PYROSCOPE_PASSWORD": "p",
	}
	cfg, err := config.Load([]string{"--reader-id=agent-1", "--once"}, env.get)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SpoolDir != "/tmp/spool" || cfg.PyroscopeURL != "http://pyroscope:4040" || !cfg.Once {
		t.Fatalf("%#v", cfg)
	}
	if cfg.ReaderID != "agent-1" || cfg.AggregationWindowSec != 15 {
		t.Fatalf("%#v", cfg)
	}
}

func TestLoadFlagsOverrideEnv(t *testing.T) {
	t.Parallel()
	env := envMap{"TRACEWICK_SPOOL_DIR": "/env", "TRACEWICK_PYROSCOPE_URL": "http://env"}
	cfg, err := config.Load([]string{
		"--spool-dir=/flag",
		"--pyroscope-url=http://flag",
		"--aggregation-window", "10",
		"--sleep=1s",
	}, env.get)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SpoolDir != "/flag" || cfg.PyroscopeURL != "http://flag" || cfg.AggregationWindowSec != 10 {
		t.Fatalf("%#v", cfg)
	}
}

func TestLoadRejectsPasswordOnArgv(t *testing.T) {
	t.Parallel()
	_, err := config.Load([]string{
		"--spool-dir=/s", "--pyroscope-url=http://x", "--pyroscope-password=secret",
	}, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "TRACEWICK_PYROSCOPE_PASSWORD") {
		t.Fatalf("%v", err)
	}
}

func TestLoadRejectsURLUserinfo(t *testing.T) {
	t.Parallel()
	_, err := config.Load([]string{
		"--spool-dir=/s", "--pyroscope-url=http://user:pass@pyroscope:4040",
	}, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("%v", err)
	}
}

func TestLoadTLSCA(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load([]string{
		"--spool-dir=/s", "--pyroscope-url=http://x", "--tls-ca=/etc/ssl/tracewick-ca.pem",
	}, func(string) string { return "" })
	if err != nil || cfg.TLSCAFile != "/etc/ssl/tracewick-ca.pem" {
		t.Fatalf("%v %#v", err, cfg)
	}
	cfg2, err := config.Load([]string{
		"--spool-dir", "/s", "--pyroscope-url", "http://x", "--tls-ca", "/ca.pem",
	}, func(string) string { return "" })
	if err != nil || cfg2.TLSCAFile != "/ca.pem" {
		t.Fatalf("%v %#v", err, cfg2)
	}
	env := envMap{
		"TRACEWICK_SPOOL_DIR":     "/s",
		"TRACEWICK_PYROSCOPE_URL": "http://x",
		"TRACEWICK_TLS_CA":        "/env-ca.pem",
	}
	cfg3, err := config.Load(nil, env.get)
	if err != nil || cfg3.TLSCAFile != "/env-ca.pem" {
		t.Fatalf("%v %#v", err, cfg3)
	}
}

func TestLoadValidation(t *testing.T) {
	t.Parallel()
	empty := func(string) string { return "" }
	if _, err := config.Load(nil, empty); err == nil {
		t.Fatal("spool required")
	}
	if _, err := config.Load([]string{"--spool-dir=/s"}, empty); err == nil {
		t.Fatal("url required")
	}
	if _, err := config.Load([]string{"--spool-dir=/s", "--pyroscope-url=http://x", "--reader-id=bad id"}, empty); err == nil {
		t.Fatal("reader id")
	}
	if _, err := config.Load([]string{"--spool-dir=/s", "--pyroscope-url=http://x", "--aggregation-window=3"}, empty); err == nil {
		t.Fatal("window")
	}
	if _, err := config.Load([]string{"--help"}, empty); !config.IsHelp(err) {
		t.Fatalf("%v", err)
	}
	env := envMap{"TRACEWICK_PYROSCOPE_USER": "u"}
	_, err := config.Load([]string{"--spool-dir=/s", "--pyroscope-url=http://x"}, env.get)
	if err == nil || !strings.Contains(err.Error(), "PASSWORD") {
		t.Fatalf("auth pair: %v", err)
	}
	usage := config.Usage()
	if !strings.Contains(usage, "TRACEWICK_PYROSCOPE_PASSWORD") ||
		!strings.Contains(usage, "--metrics-listen") ||
		!strings.Contains(usage, "--tls-ca") {
		t.Fatal("usage")
	}
	if _, err := config.Load([]string{"--spool-dir"}, empty); err == nil {
		t.Fatal("missing value")
	}
	if _, err := config.Load([]string{"--nope"}, empty); err == nil {
		t.Fatal("unknown")
	}
	if _, err := config.Load([]string{"--spool-dir=/s", "--pyroscope-url=http://x", "--sleep=-1s"}, empty); err == nil {
		t.Fatal("neg sleep")
	}
}

func TestLoadSeparateFlags(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load([]string{
		"--spool-dir", "/s",
		"--pyroscope-url", "http://x",
		"--pyroscope-user", "u",
		"--pyroscope-tenant", "t",
		"--reader-id", "r1",
		"--sleep", "2s",
		"--metrics-listen", "127.0.0.1:9090",
	}, envMap{"TRACEWICK_PYROSCOPE_PASSWORD": "secret"}.get)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PyroscopeUser != "u" || cfg.PyroscopeTenant != "t" || cfg.ReaderID != "r1" || cfg.Sleep != 2*time.Second {
		t.Fatalf("%#v", cfg)
	}
	if cfg.MetricsListen != "127.0.0.1:9090" {
		t.Fatalf("metrics listen %#v", cfg)
	}
}

func TestLoadEqualsFormsAndErrors(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load([]string{
		"--spool-dir=/s",
		"--pyroscope-url=http://x",
		"--pyroscope-user=u",
		"--pyroscope-tenant=ten",
		"--aggregation-window=12",
		"--sleep=3s",
		"--metrics-listen=:0",
	}, envMap{"TRACEWICK_PYROSCOPE_PASSWORD": "x"}.get)
	if err != nil || cfg.AggregationWindowSec != 12 || cfg.PyroscopeTenant != "ten" || cfg.MetricsListen != ":0" {
		t.Fatalf("%v %#v", err, cfg)
	}
	empty := func(string) string { return "" }
	cases := []struct {
		name string
		args []string
		help bool
	}{
		{"bad window", []string{"--spool-dir=/s", "--pyroscope-url=http://x", "--aggregation-window=nope"}, false},
		{"bad window sep", []string{"--spool-dir=/s", "--pyroscope-url=http://x", "--aggregation-window", "nope"}, false},
		{"bad sleep", []string{"--spool-dir=/s", "--pyroscope-url=http://x", "--sleep=nope"}, false},
		{"bad sleep sep", []string{"--spool-dir=/s", "--pyroscope-url=http://x", "--sleep", "nope"}, false},
		{"spool missing", []string{"--pyroscope-url", "http://x"}, false},
		{"url needs value", []string{"--spool-dir=/s", "--pyroscope-url"}, false},
		{"user needs value", []string{"--spool-dir=/s", "--pyroscope-url=http://x", "--pyroscope-user"}, false},
		{"tenant needs value", []string{"--spool-dir=/s", "--pyroscope-url=http://x", "--pyroscope-tenant"}, false},
		{"reader needs value", []string{"--spool-dir=/s", "--pyroscope-url=http://x", "--reader-id"}, false},
		{"window needs value", []string{"--spool-dir=/s", "--pyroscope-url=http://x", "--aggregation-window"}, false},
		{"sleep needs value", []string{"--spool-dir=/s", "--pyroscope-url=http://x", "--sleep"}, false},
		{"metrics-listen needs value", []string{"--spool-dir=/s", "--pyroscope-url=http://x", "--metrics-listen"}, false},
		{"tls-ca needs value", []string{"--spool-dir=/s", "--pyroscope-url=http://x", "--tls-ca"}, false},
		{"help", []string{"-h"}, true},
	}
	for _, tc := range cases {
		_, loadErr := config.Load(tc.args, empty)
		if tc.help {
			if !config.IsHelp(loadErr) {
				t.Fatalf("%s: %v", tc.name, loadErr)
			}
			continue
		}
		if loadErr == nil {
			t.Fatalf("%s: want error", tc.name)
		}
	}
}

func TestLoadMetricsListenEnv(t *testing.T) {
	t.Parallel()
	envListen := envMap{
		"TRACEWICK_SPOOL_DIR":      "/s",
		"TRACEWICK_PYROSCOPE_URL":  "http://x",
		"TRACEWICK_METRICS_LISTEN": "0.0.0.0:9100",
	}
	cfgEnv, err := config.Load(nil, envListen.get)
	if err != nil || cfgEnv.MetricsListen != "0.0.0.0:9100" {
		t.Fatalf("env listen %v %#v", err, cfgEnv)
	}
}

type envMap map[string]string

func (e envMap) get(k string) string { return e[k] }
