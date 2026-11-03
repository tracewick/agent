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
	if !strings.Contains(config.Usage(), "TRACEWICK_PYROSCOPE_PASSWORD") {
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
	}, envMap{"TRACEWICK_PYROSCOPE_PASSWORD": "secret"}.get)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PyroscopeUser != "u" || cfg.PyroscopeTenant != "t" || cfg.ReaderID != "r1" || cfg.Sleep != 2*time.Second {
		t.Fatalf("%#v", cfg)
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
	}, envMap{"TRACEWICK_PYROSCOPE_PASSWORD": "x"}.get)
	if err != nil || cfg.AggregationWindowSec != 12 || cfg.PyroscopeTenant != "ten" {
		t.Fatalf("%v %#v", err, cfg)
	}
	empty := func(string) string { return "" }
	if _, err := config.Load([]string{"--spool-dir=/s", "--pyroscope-url=http://x", "--aggregation-window=nope"}, empty); err == nil {
		t.Fatal("bad window")
	}
	if _, err := config.Load([]string{"--spool-dir=/s", "--pyroscope-url=http://x", "--aggregation-window", "nope"}, empty); err == nil {
		t.Fatal("bad window sep")
	}
	if _, err := config.Load([]string{"--spool-dir=/s", "--pyroscope-url=http://x", "--sleep=nope"}, empty); err == nil {
		t.Fatal("bad sleep")
	}
	if _, err := config.Load([]string{"--spool-dir=/s", "--pyroscope-url=http://x", "--sleep", "nope"}, empty); err == nil {
		t.Fatal("bad sleep sep")
	}
	if _, err := config.Load([]string{"--pyroscope-url", "http://x"}, empty); err == nil {
		t.Fatal("spool missing with url flag")
	}
	if _, err := config.Load([]string{"--spool-dir=/s", "--pyroscope-url"}, empty); err == nil {
		t.Fatal("url needs value")
	}
	if _, err := config.Load([]string{"--spool-dir=/s", "--pyroscope-url=http://x", "--pyroscope-user"}, empty); err == nil {
		t.Fatal("user needs value")
	}
	if _, err := config.Load([]string{"--spool-dir=/s", "--pyroscope-url=http://x", "--pyroscope-tenant"}, empty); err == nil {
		t.Fatal("tenant needs value")
	}
	if _, err := config.Load([]string{"--spool-dir=/s", "--pyroscope-url=http://x", "--reader-id"}, empty); err == nil {
		t.Fatal("reader needs value")
	}
	if _, err := config.Load([]string{"--spool-dir=/s", "--pyroscope-url=http://x", "--aggregation-window"}, empty); err == nil {
		t.Fatal("window needs value")
	}
	if _, err := config.Load([]string{"--spool-dir=/s", "--pyroscope-url=http://x", "--sleep"}, empty); err == nil {
		t.Fatal("sleep needs value")
	}
	if _, err := config.Load([]string{"-h"}, empty); !config.IsHelp(err) {
		t.Fatal(err)
	}
}

type envMap map[string]string

func (e envMap) get(k string) string { return e[k] }
