package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/tracewick/agent/internal/aggregate"
	"github.com/tracewick/agent/internal/config"
	"github.com/tracewick/agent/internal/delivery"
	"github.com/tracewick/agent/internal/httputil"
	"github.com/tracewick/agent/internal/metrics"
	"github.com/tracewick/agent/internal/pyroscope"
	"github.com/tracewick/agent/internal/spool"
)

// Agent is the long-running sidecar loop: poll, pause, shutdown. Delivery passes
// go through Drainer.
type Agent struct {
	cfg     config.Config
	drainer *Drainer
	metrics *metrics.Registry
	log     *slog.Logger
	now     func() time.Time
	sleep   func(context.Context, time.Duration) error
}

// New builds an Agent with a pooled HTTP client (connection reuse) and a Drainer.
// metrics may be nil (instruments become no-ops).
func New(cfg *config.Config, log *slog.Logger, httpClient *http.Client, registry *metrics.Registry) (*Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("agent: config is required")
	}
	if log == nil {
		log = slog.Default()
	}
	window, err := aggregate.NewWindow(cfg.AggregationWindowSec)
	if err != nil {
		return nil, err
	}
	own, err := delivery.NewOwnership(cfg.StaleClaim, cfg.OwnershipMargin)
	if err != nil {
		return nil, err
	}
	limits := delivery.DefaultLimits()
	if vErr := limits.Validate(); vErr != nil {
		return nil, vErr
	}
	src, err := spool.NewSource(spool.Config{
		Directory:        cfg.SpoolDir,
		ReaderID:         cfg.ReaderID,
		MaxFileBytes:     limits.MaxDecodedBytes,
		StaleClaim:       cfg.StaleClaim,
		MaxRejectedFiles: cfg.MaxRejectedFiles,
	})
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient, err = httputil.NewHTTPClient(cfg.HTTPTimeout, httputil.TLSOptions{CAFile: cfg.TLSCAFile})
		if err != nil {
			return nil, err
		}
	}
	client, err := pyroscope.NewClient(&pyroscope.Config{
		ServerURL:     cfg.PyroscopeURL,
		TenantID:      cfg.PyroscopeTenant,
		BasicUser:     cfg.PyroscopeUser,
		BasicPassword: cfg.PyroscopePassword,
		HTTPClient:    httpClient,
	})
	if err != nil {
		return nil, err
	}
	gate, err := aggregate.NewCardinalityGate(aggregate.NewMemoryStore(), cfg.MaxCardinality)
	if err != nil {
		return nil, err
	}
	return &Agent{
		cfg: *cfg,
		drainer: &Drainer{
			Source:             src,
			Client:             client,
			Window:             window,
			Gate:               gate,
			Ownership:          own,
			Limits:             limits,
			MaxFiles:           cfg.MaxFilesPerPass,
			MaxBatchInputBytes: cfg.MaxBatchInputBytes,
			HTTPTimeout:        cfg.HTTPTimeout,
			Log:                log,
			Now:                time.Now,
			Metrics:            registry,
		},
		metrics: registry,
		log:     log,
		now:     time.Now,
		sleep:   sleepCtx,
	}, nil
}

// Metrics returns the registry passed to New (may be nil).
func (a *Agent) Metrics() *metrics.Registry { return a.metrics }

// Run polls until admitCtx is cancelled, or finishes one productive cycle when cfg.Once.
// admitCtx stops new claims immediately on SIGTERM; pushCtx may outlive admit for grace.
// When pushCtx is nil it defaults to admitCtx.
func (a *Agent) Run(admitCtx, pushCtx context.Context) error {
	if pushCtx == nil {
		pushCtx = admitCtx
	}
	var pauseUntil time.Time
	failStreak := 0
	for {
		if err := admitCtx.Err(); err != nil {
			return err
		}
		now := a.now()
		if !pauseUntil.IsZero() && now.Before(pauseUntil) {
			wait := pauseUntil.Sub(now)
			a.log.Info("delivery paused", "until", pauseUntil, "wait", wait)
			if err := a.sleep(admitCtx, wait); err != nil {
				return err
			}
			continue
		}

		a.refreshSpoolBacklog()
		res := a.drainer.Drain(admitCtx, pushCtx)
		switch res.Outcome {
		case OutcomePaused:
			failStreak = 0
			pauseUntil = a.now().Add(res.PauseFor)
			a.log.Error("drain paused", "reason", res.Failure, "for", res.PauseFor)
			if a.cfg.Once {
				return fmt.Errorf("agent: drain %s: %s", res.Outcome, res.Failure)
			}
			continue // next iter sleeps until pauseUntil
		case OutcomeFailed:
			failStreak++
			a.log.Warn("drain pass failed", "delivered", res.Delivered, "rejected", res.Rejected, "err", res.Failure)
		case OutcomeMore:
			if res.Delivered > 0 {
				failStreak = 0
			}
			a.logDrainPass(res, "more")
		case OutcomeEmpty:
			if res.Delivered > 0 {
				failStreak = 0
			}
			a.logDrainPass(res, "empty")
		}

		if a.cfg.Once {
			// Keep draining while claim budget says more work may remain.
			if res.Outcome == OutcomeMore {
				continue
			}
			if res.Outcome == OutcomeFailed {
				return fmt.Errorf("agent: drain %s: %s", res.Outcome, res.Failure)
			}
			return nil
		}
		if res.Outcome == OutcomeMore {
			continue // tight loop while backlog exists
		}
		backoff := a.cfg.Sleep
		if res.Outcome == OutcomeFailed {
			attempt := failStreak - 1
			if attempt < 0 {
				attempt = 0
			}
			backoff = delivery.Backoff(attempt, res.RetryAfter, time.Second, 5*time.Minute, nil)
		}
		if err := a.sleep(admitCtx, backoff); err != nil {
			return err
		}
	}
}

func (a *Agent) logDrainPass(res Result, outcome string) {
	// Idle empty polls stay Debug; any delivered/rejected work is Info so operators
	// (and e2e -v) see progress without drowning in sleep ticks.
	if res.Delivered == 0 && res.Rejected == 0 {
		a.log.Debug("spool idle", "outcome", outcome, "delivered", res.Delivered, "rejected", res.Rejected)
		return
	}
	a.log.Info("drain pass", "outcome", outcome, "delivered", res.Delivered, "rejected", res.Rejected)
}

func (a *Agent) refreshSpoolBacklog() {
	if a.metrics == nil || a.drainer == nil || a.drainer.Source == nil {
		return
	}
	b := a.drainer.Source.Backlog()
	if b.Unreadable {
		a.log.Error("spool directory unreadable; backlog metrics not updated")
		return
	}
	a.metrics.SetSpoolBacklog(b.Files, b.Bytes, b.OldestAge)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
