// Package cli implements the tracewick-agent command entrypoints.
package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/tracewick/agent/internal/agent"
	"github.com/tracewick/agent/internal/config"
	"github.com/tracewick/agent/internal/delivery"
	"github.com/tracewick/agent/internal/metrics"
	"github.com/tracewick/agent/internal/version"
)

// Run executes the CLI with the given args. stdout/stderr are injectable for tests.
// runCtx is cancelled on SIGTERM/SIGINT for the `run` command.
func Run(args []string, stdout, stderr io.Writer) error {
	return RunContext(context.Background(), args, stdout, stderr)
}

// RunContext is like Run but uses parent as the root context (tests inject cancel).
func RunContext(parent context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 1 && (args[0] == "-version" || args[0] == "--version" || args[0] == "version") {
		_, _ = fmt.Fprintln(stdout, version.String())
		return nil
	}

	if len(args) >= 1 && args[0] == "run" {
		return runAgent(parent, args[1:], stdout, stderr)
	}

	if len(args) >= 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		_, _ = fmt.Fprintf(stdout, "tracewick-agent %s\n\n", version.String())
		_, _ = io.WriteString(stdout, "Commands:\n  version\n  run [options]\n\n")
		_, _ = io.WriteString(stdout, config.Usage())
		return nil
	}

	_, _ = fmt.Fprintf(stderr, "tracewick-agent %s — early development; not production-ready\n", version.String())
	_, _ = fmt.Fprintln(stderr, "usage: tracewick-agent version | run [options] | help")
	return nil
}

func runAgent(parent context.Context, args []string, stdout, stderr io.Writer) error {
	cfg, err := config.Load(args, nil)
	if err != nil {
		if config.IsHelp(err) {
			_, _ = io.WriteString(stdout, config.Usage())
			return nil
		}
		return err
	}

	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	reg := metrics.New()
	a, err := agent.New(&cfg, log, nil, reg)
	if err != nil {
		return err
	}

	metricsSrv, err := metrics.Listen(cfg.MetricsListen, reg)
	if err != nil {
		return err
	}
	if metricsSrv != nil {
		defer func() {
			shCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = metricsSrv.Shutdown(shCtx)
		}()
		_, _ = fmt.Fprintf(stderr, "metrics listening on http://%s/metrics\n", metricsSrv.Addr())
	}

	ctx, stop := delivery.Notify(parent)
	defer stop()
	workCtx, cancelGrace := delivery.WithGrace(ctx, cfg.ShutdownGrace)
	defer cancelGrace()

	_, _ = fmt.Fprintf(stderr, "tracewick-agent %s starting (spool=%s reader=%s)\n", version.String(), cfg.SpoolDir, cfg.ReaderID)
	return a.Run(workCtx)
}
