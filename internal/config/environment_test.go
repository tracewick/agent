package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/config"
)

func TestOperationalEnvironment(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(nil, envMap{
		"TRACEWICK_SPOOL_DIR": "/spool", "TRACEWICK_PYROSCOPE_URL": "http://localhost:4040",
		"TRACEWICK_READER_ID": "env-reader", "TRACEWICK_AGGREGATION_WINDOW": "10",
		"TRACEWICK_SLEEP": "250ms", "TRACEWICK_SHUTDOWN_GRACE": "0s", "TRACEWICK_MAX_REJECTED_FILES": "0",
		"TRACEWICK_MAX_STACKS": "8", "TRACEWICK_MAX_FRAMES": "16", "TRACEWICK_MAX_STRINGS": "32",
		"TRACEWICK_MAX_HELD_DECODED_BYTES": "65536", "TRACEWICK_MAX_IN_FLIGHT_BYTES": "131072",
	}.get)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReaderID != "env-reader" || cfg.AggregationWindowSec != 10 || cfg.Sleep != 250*time.Millisecond || cfg.ShutdownGrace != 0 || cfg.MaxRejectedFiles != 0 {
		t.Fatalf("unexpected operational settings: %#v", cfg)
	}
	limits := cfg.DeliveryLimits()
	if limits.MaxStacks != 8 || limits.MaxFrames != 16 || limits.MaxStrings != 32 || limits.MaxHeldDecodedBytes != 65536 || limits.MaxInFlightBytes != 131072 {
		t.Fatalf("limits: %#v", limits)
	}
}

func TestOperationalEnvironmentPrecedenceAndValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ flag, variable, valid, invalid string }{
		{"reader-id", "TRACEWICK_READER_ID", "reader", "a/b"},
		{"aggregation-window", "TRACEWICK_AGGREGATION_WINDOW", "10", "9"},
		{"sleep", "TRACEWICK_SLEEP", "250ms", "bad"},
		{"shutdown-grace", "TRACEWICK_SHUTDOWN_GRACE", "0s", "-1s"},
		{"max-rejected-files", "TRACEWICK_MAX_REJECTED_FILES", "0", "100001"},
		{"max-stacks", "TRACEWICK_MAX_STACKS", "10", "-1"},
		{"max-frames", "TRACEWICK_MAX_FRAMES", "20", "-1"},
		{"max-strings", "TRACEWICK_MAX_STRINGS", "30", "-1"},
		{"max-held-decoded-bytes", "TRACEWICK_MAX_HELD_DECODED_BYTES", "65536", "9223372036854775808"},
		{"max-in-flight-bytes", "TRACEWICK_MAX_IN_FLIGHT_BYTES", "131072", "-1"},
	} {
		t.Run(tc.flag, func(t *testing.T) {
			required := []string{"--spool-dir=/spool", "--pyroscope-url=http://localhost:4040"}
			baseline, err := config.Load(append(required, "--"+tc.flag+"="+tc.valid), envMap{}.get)
			if err != nil {
				t.Fatal(err)
			}
			fromEnv, err := config.Load(required, envMap{tc.variable: tc.valid}.get)
			if err != nil || fromEnv != baseline {
				t.Fatalf("env mismatch: %v %#v", err, fromEnv)
			}
			for _, args := range [][]string{
				append(required, "--"+tc.flag+"="+tc.valid),
				append(required, "--"+tc.flag, tc.valid),
			} {
				actual, loadErr := config.Load(args, envMap{tc.variable: tc.invalid}.get)
				if loadErr != nil || actual != baseline {
					t.Fatalf("CLI override: %v %#v", loadErr, actual)
				}
			}
			if _, loadErr := config.Load(required, envMap{tc.variable: tc.invalid}.get); loadErr == nil {
				t.Fatal("invalid env accepted")
			}
			empty, err := config.Load(required, envMap{tc.variable: ""}.get)
			defaults, defaultErr := config.Load(required, envMap{}.get)
			if err != nil || defaultErr != nil || empty != defaults {
				t.Fatalf("empty env: %v %v", err, defaultErr)
			}
			explicit, err := config.Load(append(required, "--"+tc.flag+"="), envMap{tc.variable: tc.valid}.get)
			if tc.flag == "reader-id" {
				if err != nil || explicit.ReaderID == tc.valid || explicit.ReaderID == "" {
					t.Fatalf("reader reset: %v", err)
				}
			} else if err == nil {
				t.Fatal("empty numeric/duration CLI accepted")
			}
			if _, err := config.Load([]string{"--help"}, envMap{tc.variable: tc.invalid}.get); !config.IsHelp(err) {
				t.Fatalf("help blocked: %v", err)
			}
		})
	}
}

func TestExplicitEmptyStringsOverrideEnvironment(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load([]string{"--spool-dir=/spool", "--pyroscope-url=http://localhost:4040", "--pyroscope-tenant=", "--metrics-listen=", "--tls-ca=", "--pyroscope-user="}, envMap{
		"TRACEWICK_PYROSCOPE_TENANT": "tenant", "TRACEWICK_METRICS_LISTEN": ":9090", "TRACEWICK_TLS_CA": "/ca.pem", "TRACEWICK_PYROSCOPE_USER": "user",
	}.get)
	if err != nil || cfg.PyroscopeTenant != "" || cfg.MetricsListen != "" || cfg.TLSCAFile != "" || cfg.PyroscopeUser != "" {
		t.Fatalf("clear settings: %v %#v", err, cfg)
	}
	_, err = config.Load([]string{"--spool-dir=/spool", "--pyroscope-url=http://localhost:4040", "--pyroscope-user="}, envMap{"TRACEWICK_PYROSCOPE_USER": "u", "TRACEWICK_PYROSCOPE_PASSWORD": "secret"}.get)
	if err == nil || !strings.Contains(err.Error(), "authentication") {
		t.Fatalf("cleared auth user: %v", err)
	}
	for _, flag := range []string{"--spool-dir=", "--pyroscope-url="} {
		_, err = config.Load([]string{flag}, envMap{"TRACEWICK_SPOOL_DIR": "/spool", "TRACEWICK_PYROSCOPE_URL": "http://localhost:4040"}.get)
		if err == nil {
			t.Fatalf("empty required value accepted: %s", flag)
		}
	}
}

func TestZeroEnvironmentBudgetsKeepDeliveryDefaults(t *testing.T) {
	t.Parallel()
	args := []string{"--spool-dir=/spool", "--pyroscope-url=http://localhost:4040"}
	baseline, err := config.Load(args, envMap{}.get)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(args, envMap{
		"TRACEWICK_MAX_STACKS": "0", "TRACEWICK_MAX_FRAMES": "0", "TRACEWICK_MAX_STRINGS": "0",
		"TRACEWICK_MAX_HELD_DECODED_BYTES": "0", "TRACEWICK_MAX_IN_FLIGHT_BYTES": "0", "TRACEWICK_SLEEP": "0s",
	}.get)
	if err != nil || cfg.DeliveryLimits() != baseline.DeliveryLimits() || cfg.Sleep != 0 {
		t.Fatalf("zero overrides: %v %#v", err, cfg)
	}
}
