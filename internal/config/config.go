package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tracewick/agent/internal/aggregate"
	"github.com/tracewick/agent/internal/delivery"
	"github.com/tracewick/agent/internal/spool"
)

const (
	envSpoolDir           = "TRACEWICK_SPOOL_DIR"
	envPyroscopeURL       = "TRACEWICK_PYROSCOPE_URL"
	envPyroscopeUser      = "TRACEWICK_PYROSCOPE_USER"
	envPyroscopePassword  = "TRACEWICK_PYROSCOPE_PASSWORD" //nolint:gosec // env var name, not a secret
	envPyroscopeTenant    = "TRACEWICK_PYROSCOPE_TENANT"
	envMetricsListen      = "TRACEWICK_METRICS_LISTEN"
	envTLSCAFile          = "TRACEWICK_TLS_CA"
	defaultSleep          = 5 * time.Second
	defaultHTTPTimeout    = 10 * time.Second
	defaultMaxFiles       = 256
	defaultMaxBatchBytes  = 8 << 20 // matches PHP SpoolDrainer
	defaultMaxCardinality = 256
)

// Config is the runtime settings for one agent process.
type Config struct {
	SpoolDir             string
	PyroscopeURL         string
	PyroscopeUser        string
	PyroscopePassword    string
	PyroscopeTenant      string
	ReaderID             string
	AggregationWindowSec int
	Sleep                time.Duration
	StaleClaim           time.Duration
	OwnershipMargin      time.Duration
	MaxFilesPerPass      int
	MaxBatchInputBytes   int64
	HTTPTimeout          time.Duration
	ShutdownGrace        time.Duration
	MaxRejectedFiles     int // kept under spool rejected/; 0 deletes on reject
	MaxCardinality       int
	MetricsListen        string // optional host:port for Prometheus /metrics; empty disables
	TLSCAFile            string // optional PEM CA bundle for HTTPS to Pyroscope
	Once                 bool   // one drain pass then exit
}

// Load parses argv flags and environment. Password is read only from TRACEWICK_PYROSCOPE_PASSWORD.
func Load(args []string, getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	cfg := Config{
		SpoolDir:             getenv(envSpoolDir),
		PyroscopeURL:         getenv(envPyroscopeURL),
		PyroscopeUser:        getenv(envPyroscopeUser),
		PyroscopePassword:    getenv(envPyroscopePassword),
		PyroscopeTenant:      getenv(envPyroscopeTenant),
		MetricsListen:        getenv(envMetricsListen),
		TLSCAFile:            getenv(envTLSCAFile),
		AggregationWindowSec: aggregate.DefaultWindowSeconds,
		Sleep:                defaultSleep,
		StaleClaim:           delivery.DefaultStaleClaim,
		OwnershipMargin:      delivery.DefaultOwnershipMargin,
		MaxFilesPerPass:      defaultMaxFiles,
		MaxBatchInputBytes:   defaultMaxBatchBytes,
		HTTPTimeout:          defaultHTTPTimeout,
		ShutdownGrace:        delivery.DefaultShutdownGrace,
		MaxRejectedFiles:     spool.DefaultMaxRejectedFiles,
		MaxCardinality:       defaultMaxCardinality,
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			return Config{}, errHelp
		case arg == "--once":
			cfg.Once = true
		case strings.HasPrefix(arg, "--spool-dir="):
			cfg.SpoolDir = strings.TrimPrefix(arg, "--spool-dir=")
		case arg == "--spool-dir":
			v, err := needValue(args, &i, arg)
			if err != nil {
				return Config{}, err
			}
			cfg.SpoolDir = v
		case strings.HasPrefix(arg, "--pyroscope-url="):
			cfg.PyroscopeURL = strings.TrimPrefix(arg, "--pyroscope-url=")
		case arg == "--pyroscope-url":
			v, err := needValue(args, &i, arg)
			if err != nil {
				return Config{}, err
			}
			cfg.PyroscopeURL = v
		case strings.HasPrefix(arg, "--pyroscope-user="):
			cfg.PyroscopeUser = strings.TrimPrefix(arg, "--pyroscope-user=")
		case arg == "--pyroscope-user":
			v, err := needValue(args, &i, arg)
			if err != nil {
				return Config{}, err
			}
			cfg.PyroscopeUser = v
		case strings.HasPrefix(arg, "--pyroscope-tenant="):
			cfg.PyroscopeTenant = strings.TrimPrefix(arg, "--pyroscope-tenant=")
		case arg == "--pyroscope-tenant":
			v, err := needValue(args, &i, arg)
			if err != nil {
				return Config{}, err
			}
			cfg.PyroscopeTenant = v
		case strings.HasPrefix(arg, "--reader-id="):
			cfg.ReaderID = strings.TrimPrefix(arg, "--reader-id=")
		case arg == "--reader-id":
			v, err := needValue(args, &i, arg)
			if err != nil {
				return Config{}, err
			}
			cfg.ReaderID = v
		case strings.HasPrefix(arg, "--aggregation-window="):
			n, err := strconv.Atoi(strings.TrimPrefix(arg, "--aggregation-window="))
			if err != nil {
				return Config{}, fmt.Errorf("config: invalid --aggregation-window: %w", err)
			}
			cfg.AggregationWindowSec = n
		case arg == "--aggregation-window":
			v, err := needValue(args, &i, arg)
			if err != nil {
				return Config{}, err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return Config{}, fmt.Errorf("config: invalid --aggregation-window: %w", err)
			}
			cfg.AggregationWindowSec = n
		case strings.HasPrefix(arg, "--sleep="):
			d, err := time.ParseDuration(strings.TrimPrefix(arg, "--sleep="))
			if err != nil {
				return Config{}, fmt.Errorf("config: invalid --sleep: %w", err)
			}
			cfg.Sleep = d
		case arg == "--sleep":
			v, err := needValue(args, &i, arg)
			if err != nil {
				return Config{}, err
			}
			d, err := time.ParseDuration(v)
			if err != nil {
				return Config{}, fmt.Errorf("config: invalid --sleep: %w", err)
			}
			cfg.Sleep = d
		case strings.HasPrefix(arg, "--metrics-listen="):
			cfg.MetricsListen = strings.TrimPrefix(arg, "--metrics-listen=")
		case arg == "--metrics-listen":
			v, err := needValue(args, &i, arg)
			if err != nil {
				return Config{}, err
			}
			cfg.MetricsListen = v
		case strings.HasPrefix(arg, "--tls-ca="):
			cfg.TLSCAFile = strings.TrimPrefix(arg, "--tls-ca=")
		case arg == "--tls-ca":
			v, err := needValue(args, &i, arg)
			if err != nil {
				return Config{}, err
			}
			cfg.TLSCAFile = v
		case strings.HasPrefix(arg, "--shutdown-grace="):
			d, err := time.ParseDuration(strings.TrimPrefix(arg, "--shutdown-grace="))
			if err != nil {
				return Config{}, fmt.Errorf("config: invalid --shutdown-grace: %w", err)
			}
			cfg.ShutdownGrace = d
		case arg == "--shutdown-grace":
			v, err := needValue(args, &i, arg)
			if err != nil {
				return Config{}, err
			}
			d, err := time.ParseDuration(v)
			if err != nil {
				return Config{}, fmt.Errorf("config: invalid --shutdown-grace: %w", err)
			}
			cfg.ShutdownGrace = d
		case strings.HasPrefix(arg, "--max-rejected-files="):
			n, err := strconv.Atoi(strings.TrimPrefix(arg, "--max-rejected-files="))
			if err != nil {
				return Config{}, fmt.Errorf("config: invalid --max-rejected-files: %w", err)
			}
			cfg.MaxRejectedFiles = n
		case arg == "--max-rejected-files":
			v, err := needValue(args, &i, arg)
			if err != nil {
				return Config{}, err
			}
			n, err := strconv.Atoi(v)
			if err != nil {
				return Config{}, fmt.Errorf("config: invalid --max-rejected-files: %w", err)
			}
			cfg.MaxRejectedFiles = n
		case strings.HasPrefix(arg, "--pyroscope-password="), arg == "--pyroscope-password":
			return Config{}, errors.New("config: do not pass the Pyroscope password on the command line; set " + envPyroscopePassword)
		default:
			return Config{}, fmt.Errorf("config: unknown argument %q", arg)
		}
	}

	if cfg.SpoolDir == "" {
		return Config{}, errors.New("config: --spool-dir or TRACEWICK_SPOOL_DIR is required")
	}
	if cfg.PyroscopeURL == "" {
		return Config{}, errors.New("config: --pyroscope-url or TRACEWICK_PYROSCOPE_URL is required")
	}
	if err := rejectURLUserinfo(cfg.PyroscopeURL); err != nil {
		return Config{}, err
	}
	if cfg.ReaderID == "" {
		host, _ := os.Hostname()
		if host == "" {
			host = "agent"
		}
		cfg.ReaderID = fmt.Sprintf("%s-%d", host, os.Getpid())
	}
	if err := spool.ValidateReaderID(cfg.ReaderID); err != nil {
		return Config{}, fmt.Errorf("config: reader-id: %w", err)
	}
	if (cfg.PyroscopeUser == "") != (cfg.PyroscopePassword == "") {
		return Config{}, errors.New("config: basic authentication needs both TRACEWICK_PYROSCOPE_USER and TRACEWICK_PYROSCOPE_PASSWORD")
	}
	if _, err := aggregate.NewWindow(cfg.AggregationWindowSec); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	if cfg.Sleep < 0 {
		return Config{}, errors.New("config: --sleep must be non-negative")
	}
	if cfg.ShutdownGrace < 0 {
		return Config{}, errors.New("config: --shutdown-grace must be non-negative")
	}
	if cfg.MaxRejectedFiles < 0 || cfg.MaxRejectedFiles > 100_000 {
		return Config{}, errors.New("config: --max-rejected-files must be between 0 and 100000")
	}
	return cfg, nil
}

// Usage returns the help text for the run command.
func Usage() string {
	return strings.TrimSpace(`
Usage: tracewick-agent run --spool-dir=PATH --pyroscope-url=URL [options]

Options:
  --spool-dir=PATH              spool directory (env TRACEWICK_SPOOL_DIR)
  --pyroscope-url=URL           Pyroscope base URL (env TRACEWICK_PYROSCOPE_URL)
  --pyroscope-user=USER         basic auth user (env TRACEWICK_PYROSCOPE_USER)
  --pyroscope-tenant=ID         X-Scope-OrgID (env TRACEWICK_PYROSCOPE_TENANT)
  --reader-id=ID                claim reader id (default: host-pid)
  --aggregation-window=S        merge window 10–15 seconds (default: 15)
  --sleep=DURATION              idle poll delay (default: 5s)
  --shutdown-grace=DURATION     finish in-flight work after SIGTERM (default: 30s)
  --max-rejected-files=N        keep up to N files under rejected/ (default: 100; 0 deletes)
  --metrics-listen=ADDR         Prometheus /metrics bind (env TRACEWICK_METRICS_LISTEN; empty disables)
  --tls-ca=PATH                 extra PEM CA roots for HTTPS (env TRACEWICK_TLS_CA)
  --once                        deliver one pass and exit
  --help                        show this help

Password: set TRACEWICK_PYROSCOPE_PASSWORD in the environment (never argv).
Do not put credentials in --pyroscope-url; use TRACEWICK_PYROSCOPE_USER / TRACEWICK_PYROSCOPE_PASSWORD.
`) + "\n"
}

var errHelp = errors.New("help")

// IsHelp reports whether Load failed because the user asked for help.
func IsHelp(err error) bool { return errors.Is(err, errHelp) }

func needValue(args []string, i *int, flag string) (string, error) {
	if *i+1 >= len(args) {
		return "", fmt.Errorf("config: %s requires a value", flag)
	}
	*i++
	return args[*i], nil
}

func rejectURLUserinfo(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return nil // NewClient validates the URL shape
	}
	if u.User != nil {
		return errors.New("config: do not put credentials in --pyroscope-url; set TRACEWICK_PYROSCOPE_USER and TRACEWICK_PYROSCOPE_PASSWORD")
	}
	return nil
}
