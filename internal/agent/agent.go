package agent

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
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
	rng     *rand.Rand

	// Backlog metrics: full directory Stat is expensive; throttle on OutcomeMore.
	backlogMinInterval time.Duration
	lastBacklogAt      time.Time
	lastBacklogBytes   int64
	lastBacklogAge     time.Duration
}

const defaultBacklogMinInterval = 5 * time.Second

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
		metrics:            registry,
		log:                log,
		now:                time.Now,
		sleep:              sleepCtx,
		rng:                rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // backoff jitter only
		backlogMinInterval: defaultBacklogMinInterval,
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
	forceBacklog := true
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

		a.refreshSpoolBacklog(forceBacklog)
		res := a.drainer.Drain(admitCtx, pushCtx)
		a.logScanIssues()
		forceBacklog = res.Outcome != OutcomeMore
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
			// failStreak was just incremented; attempt is 0-based. Retry-After is a floor.
			backoff = delivery.Backoff(failStreak-1, res.RetryAfter, time.Second, 5*time.Minute, a.rng)
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

// refreshSpoolBacklog updates spool_* gauges. force=false skips the full directory
// Stat walk when called again within backlogMinInterval (OutcomeMore tight loop),
// publishing QueueLen() with the last known bytes/age instead.
func (a *Agent) refreshSpoolBacklog(force bool) {
	if a.metrics == nil || a.drainer == nil || a.drainer.Source == nil {
		return
	}
	interval := a.backlogMinInterval
	if interval <= 0 {
		interval = defaultBacklogMinInterval
	}
	now := a.now()
	if !force && !a.lastBacklogAt.IsZero() && now.Sub(a.lastBacklogAt) < interval {
		a.metrics.SetSpoolBacklog(a.drainer.Source.QueueLen(), a.lastBacklogBytes, a.lastBacklogAge)
		return
	}
	b := a.drainer.Source.Backlog()
	if b.Unreadable {
		a.log.Error("spool directory unreadable; backlog metrics not updated")
		return
	}
	a.lastBacklogAt = now
	a.lastBacklogBytes = b.Bytes
	a.lastBacklogAge = b.OldestAge
	a.metrics.SetSpoolBacklog(b.Files, b.Bytes, b.OldestAge)
}

// logScanIssues reports non-benign shard/file scan errors collected during Drain,
// independent of metrics. Call after every Drain so --once still surfaces them.
func (a *Agent) logScanIssues() {
	if a.drainer == nil || a.drainer.Source == nil {
		return
	}
	if issues := a.drainer.Source.TakeScanIssues(); len(issues) > 0 {
		a.log.Error("spool scan partial failure", "issues", issues)
	}
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
