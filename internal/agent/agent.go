package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/tracewick/agent/internal/aggregate"
	"github.com/tracewick/agent/internal/config"
	"github.com/tracewick/agent/internal/delivery"
	"github.com/tracewick/agent/internal/pyroscope"
	"github.com/tracewick/agent/internal/spool"
)

// Agent is the long-running sidecar loop: poll, pause, shutdown. Delivery passes
// go through Drainer.
type Agent struct {
	cfg     config.Config
	drainer *Drainer
	log     *slog.Logger
	now     func() time.Time
	sleep   func(context.Context, time.Duration) error
}

// New builds an Agent with a pooled HTTP client (connection reuse) and a Drainer.
func New(cfg *config.Config, log *slog.Logger, httpClient *http.Client) (*Agent, error) {
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
		Directory:    cfg.SpoolDir,
		ReaderID:     cfg.ReaderID,
		MaxFileBytes: limits.MaxDecodedBytes,
		StaleClaim:   cfg.StaleClaim,
	})
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = newHTTPClient(cfg.HTTPTimeout)
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
		},
		log:   log,
		now:   time.Now,
		sleep: sleepCtx,
	}, nil
}

// Run polls until ctx is cancelled, or finishes one productive cycle when cfg.Once.
func (a *Agent) Run(ctx context.Context) error {
	var pauseUntil time.Time
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := a.now()
		if !pauseUntil.IsZero() && now.Before(pauseUntil) {
			wait := pauseUntil.Sub(now)
			a.log.Info("delivery paused", "until", pauseUntil, "wait", wait)
			if err := a.sleep(ctx, wait); err != nil {
				return err
			}
			continue
		}

		res := a.drainer.Drain(ctx)
		switch res.Outcome {
		case OutcomePaused:
			pauseUntil = a.now().Add(res.PauseFor)
			a.log.Error("drain paused", "reason", res.Failure, "for", res.PauseFor)
			if a.cfg.Once {
				return fmt.Errorf("agent: drain %s: %s", res.Outcome, res.Failure)
			}
			continue // next iter sleeps until pauseUntil
		case OutcomeFailed:
			a.log.Warn("drain pass failed", "delivered", res.Delivered, "rejected", res.Rejected, "err", res.Failure)
		case OutcomeMore:
			a.log.Debug("more spool work remains", "delivered", res.Delivered, "rejected", res.Rejected)
		case OutcomeEmpty:
			a.log.Debug("spool idle", "delivered", res.Delivered, "rejected", res.Rejected)
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
		if res.Outcome == OutcomeFailed && backoff < time.Second {
			backoff = time.Second
		}
		if err := a.sleep(ctx, backoff); err != nil {
			return err
		}
	}
}

func newHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{Timeout: timeout, Transport: transport}
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
