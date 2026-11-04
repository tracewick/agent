package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tracewick/agent/internal/aggregate"
	"github.com/tracewick/agent/internal/delivery"
	"github.com/tracewick/agent/internal/metrics"
	"github.com/tracewick/agent/internal/pprof"
	"github.com/tracewick/agent/internal/pyroscope"
	"github.com/tracewick/agent/internal/spool"
)

// Outcome is the result of one Drainer pass.
type Outcome int

const (
	OutcomeEmpty Outcome = iota
	OutcomeMore          // claim budget hit; caller should drain again without sleeping
	OutcomeFailed
	OutcomePaused // auth/endpoint; release held claims and back off
)

func (o Outcome) String() string {
	switch o {
	case OutcomeEmpty:
		return "empty"
	case OutcomeMore:
		return "more"
	case OutcomeFailed:
		return "failed"
	case OutcomePaused:
		return "paused"
	default:
		return fmt.Sprintf("outcome(%d)", int(o))
	}
}

// Result summarizes one Drainer pass.
type Result struct {
	Outcome    Outcome
	Delivered  int
	Rejected   int
	Failure    string
	PauseFor   time.Duration // set when OutcomePaused
	RetryAfter time.Duration // lower bound for Agent backoff on OutcomeFailed (Retry-After / 0)
}

// Drainer runs one delivery pass over the spool (SpoolDrainer domain role).
type Drainer struct {
	Source             *spool.Source
	Client             *pyroscope.Client
	Window             *aggregate.Window
	Gate               *aggregate.CardinalityGate
	Ownership          delivery.Ownership
	Limits             delivery.Limits
	MaxFiles           int
	MaxBatchInputBytes int64
	HTTPTimeout        time.Duration
	Log                *slog.Logger
	Now                func() time.Time
	Metrics            *metrics.Registry // optional; nil-safe
}

// Drain claims a batch, groups by ADR-12 key+window, merges, encodes pprof, pushes,
// then ack/release/reject using delivery.Classify (agent policy, not PHP reject-on-401).
// admitCtx and pushCtx may be the same; on shutdown admit cancels first so no new claims start
// while an in-flight push may still use pushCtx until grace expires.
func (d *Drainer) Drain(admitCtx, pushCtx context.Context) Result {
	if pushCtx == nil {
		pushCtx = admitCtx
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	maxFiles := d.MaxFiles
	if maxFiles <= 0 {
		maxFiles = 256
	}
	maxBytes := d.MaxBatchInputBytes
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}

	batch, hitBudget, err := d.claimBatch(admitCtx, maxFiles, maxBytes)
	if err != nil {
		return Result{Outcome: OutcomeFailed, Failure: err.Error()}
	}
	if len(batch) == 0 {
		return Result{Outcome: OutcomeEmpty}
	}

	var batchBytes int64
	for _, c := range batch {
		batchBytes += int64(len(c.Bytes))
	}
	d.Metrics.AddClaimed(int64(len(batch)))
	d.Metrics.ObserveBatch(len(batch), batchBytes)

	held := append([]*spool.Claimed(nil), batch...)
	defer func() { d.releaseAll(held) }()

	type item struct {
		claimed *spool.Claimed
		file    *spool.File
		profile *pprof.Profile
	}
	items := make([]item, 0, len(batch))
	members := make([]aggregate.Member, 0, len(batch))
	rejected := 0
	decodeLim := d.Limits.DecodeLimits()
	maxHeld := d.Limits.MaxHeldProfiles
	if maxHeld <= 0 {
		maxHeld = delivery.DefaultMaxHeldProfiles
	}

	for _, claimed := range batch {
		if d.Ownership.Exhausted(claimed.ClaimedAt, d.Now()) {
			d.Log.Warn("ownership budget exhausted before parse; will release at end of pass", "file", claimed.Name)
			continue
		}
		if limErr := d.Limits.AcceptDecoded(len(claimed.Bytes)); limErr != nil {
			_ = d.Source.Reject(claimed)
			held = withoutClaim(held, claimed)
			rejected++
			d.Metrics.AddRejected(1)
			d.Log.Warn("reject oversized claim", "file", claimed.Name, "err", limErr)
			continue
		}
		file, parseErr := spool.Parse(claimed.Bytes)
		if parseErr != nil {
			_ = d.Source.Reject(claimed)
			held = withoutClaim(held, claimed)
			rejected++
			d.Metrics.AddRejected(1)
			d.Log.Warn("reject unreadable spool", "file", claimed.Name, "err", parseErr)
			continue
		}
		// Decode once: isolate corrupt members and reuse Profile in pushGroup.
		if len(items) >= maxHeld {
			d.Log.Warn("held profile budget reached; remaining claims released", "max", maxHeld)
			break
		}
		profile, decErr := pprof.FromSpoolFileLimited(file, decodeLim)
		if decErr != nil {
			_ = d.Source.Reject(claimed)
			held = withoutClaim(held, claimed)
			rejected++
			d.Metrics.AddRejected(1)
			d.Log.Warn("reject undecodable compact-stacks", "file", claimed.Name, "err", decErr)
			continue
		}
		key, keyErr := aggregate.KeyFromFile(file)
		if keyErr != nil {
			_ = d.Source.Reject(claimed)
			held = withoutClaim(held, claimed)
			rejected++
			d.Metrics.AddRejected(1)
			d.Log.Warn("reject bad aggregation key", "file", claimed.Name, "err", keyErr)
			continue
		}
		hint := aggregate.AggregationKeyHintFromName(claimed.Name)
		if hint != "" && hint != key.Hash() {
			_ = d.Source.Reject(claimed)
			held = withoutClaim(held, claimed)
			rejected++
			d.Metrics.AddRejected(1)
			d.Log.Warn("reject name hint / header hash mismatch", "file", claimed.Name, "hint", hint, "header", key.Hash())
			continue
		}
		items = append(items, item{claimed: claimed, file: file, profile: profile})
		members = append(members, aggregate.Member{File: file, Name: claimed.Name, HintHash: hint})
	}

	if len(members) == 0 {
		if hitBudget || len(batch) >= maxFiles {
			return Result{Outcome: OutcomeMore, Rejected: rejected}
		}
		return Result{Outcome: OutcomeEmpty, Rejected: rejected}
	}

	groups, err := aggregate.GroupBy(d.Window, members)
	if err != nil {
		d.Log.Error("groupby failed", "err", err)
		return Result{Outcome: OutcomeFailed, Rejected: rejected, Failure: err.Error()}
	}

	byFile := make(map[*spool.File]item, len(items))
	for _, it := range items {
		byFile[it.file] = it
	}

	delivered := 0
	posts := 0
	for _, g := range groups {
		claimedFiles := make([]*spool.Claimed, 0, len(g.Files))
		profiles := make([]*pprof.Profile, 0, len(g.Files))
		for _, f := range g.Files {
			if it, ok := byFile[f]; ok {
				claimedFiles = append(claimedFiles, it.claimed)
				profiles = append(profiles, it.profile)
			}
		}
		if len(claimedFiles) == 0 {
			continue
		}

		for _, c := range claimedFiles {
			if d.Ownership.Exhausted(c.ClaimedAt, d.Now()) || !d.Ownership.WithinHTTP(c.ClaimedAt, d.Now(), d.HTTPTimeout) {
				d.Log.Warn("ownership budget exhausted before push", "file", c.Name)
				return Result{Outcome: OutcomeFailed, Delivered: delivered, Rejected: rejected, Failure: "ownership budget exhausted"}
			}
		}

		decision := d.pushGroup(pushCtx, &g, profiles)
		posts++
		switch decision.Action {
		case delivery.ActionAck:
			for _, c := range claimedFiles {
				if err := d.Source.Acknowledge(c); err != nil {
					d.Log.Error("ack failed", "file", c.Name, "err", err)
				}
				held = withoutClaim(held, c)
				delivered++
			}
			d.Metrics.AddDelivered(int64(len(claimedFiles)))
		case delivery.ActionReject:
			for _, c := range claimedFiles {
				_ = d.Source.Reject(c)
				held = withoutClaim(held, c)
				rejected++
			}
			d.Metrics.AddRejected(int64(len(claimedFiles)))
			d.Log.Warn("rejected group", "reason", decision.Reason, "n", len(claimedFiles))
		case delivery.ActionPauseAuth, delivery.ActionPauseEndpoint:
			d.Log.Error("pausing delivery", "action", decision.Action.String(), "reason", decision.Reason)
			d.setAggRatio(len(members), posts)
			return Result{
				Outcome:   OutcomePaused,
				Delivered: delivered,
				Rejected:  rejected,
				Failure:   decision.Reason,
				PauseFor:  time.Minute,
			}
		case delivery.ActionShrinkBatch:
			if delivery.For413(len(claimedFiles)) == delivery.ActionReject {
				for _, c := range claimedFiles {
					_ = d.Source.Reject(c)
					held = withoutClaim(held, c)
					rejected++
				}
				d.Metrics.AddRejected(int64(len(claimedFiles)))
				d.Log.Warn("413 on single-profile group; rejected", "reason", decision.Reason)
				continue
			}
			d.MaxBatchInputBytes = maxInt64(maxBytes/2, 1)
			d.Metrics.AddRetried(int64(len(claimedFiles)))
			d.Log.Warn("413/oversize; shrink next batch", "bytes", d.MaxBatchInputBytes, "reason", decision.Reason)
			d.setAggRatio(len(members), posts)
			return Result{Outcome: OutcomeFailed, Delivered: delivered, Rejected: rejected, Failure: decision.Reason}
		default: // retry / transport → release held (defer) and end pass; Agent backs off
			d.Metrics.AddRetried(int64(len(claimedFiles)))
			d.Log.Warn("push failed; releasing held claims", "action", decision.Action.String(), "reason", decision.Reason)
			d.setAggRatio(len(members), posts)
			return Result{
				Outcome:    OutcomeFailed,
				Delivered:  delivered,
				Rejected:   rejected,
				Failure:    decision.Reason,
				RetryAfter: decision.RetryAfter,
			}
		}
	}

	d.setAggRatio(len(members), posts)
	if hitBudget || len(batch) >= maxFiles {
		return Result{Outcome: OutcomeMore, Delivered: delivered, Rejected: rejected}
	}
	return Result{Outcome: OutcomeEmpty, Delivered: delivered, Rejected: rejected}
}

func (d *Drainer) setAggRatio(profiles, posts int) {
	if posts <= 0 {
		d.Metrics.SetAggregationRatio(0)
		return
	}
	d.Metrics.SetAggregationRatio(float64(profiles) / float64(posts))
}

func (d *Drainer) pushGroup(ctx context.Context, g *aggregate.Group, profiles []*pprof.Profile) delivery.Decision {
	if len(profiles) == 0 {
		return delivery.Decision{Action: delivery.ActionReject, Reason: "empty profile group"}
	}
	merged, err := (pprof.Merger{
		MaxStacks:  d.Limits.MaxStacks,
		MaxFrames:  d.Limits.MaxFrames,
		MaxStrings: d.Limits.MaxStrings,
	}).Merge(profiles)
	if err != nil {
		return delivery.Decision{Action: delivery.ActionReject, Reason: err.Error()}
	}

	labels := g.Key.Labels
	if d.Gate != nil {
		labels = d.Gate.LimitLabels(g.Key.Application, g.Key.Labels, g.Key.EventType, g.Key.PeriodNs)
	}
	raw, err := pprof.EncodeLimited(merged, d.Limits.EncodeLimits())
	if err != nil {
		if errors.Is(err, pprof.ErrEncodeBudget) {
			// Oversized aggregate of many files → shrink/split; single profile → reject.
			if len(g.Files) <= 1 {
				return delivery.Decision{Action: delivery.ActionReject, Reason: err.Error()}
			}
			return delivery.Decision{Action: delivery.ActionShrinkBatch, Reason: err.Error()}
		}
		return delivery.Decision{Action: delivery.ActionReject, Reason: err.Error()}
	}

	pushCtx := ctx
	if d.HTTPTimeout > 0 {
		var cancel context.CancelFunc
		pushCtx, cancel = context.WithTimeout(ctx, d.HTTPTimeout)
		defer cancel()
	}
	d.Metrics.IncInflight()
	start := d.Now()
	err = d.Client.Push(pushCtx, &pyroscope.Profile{
		Application: g.Key.Application,
		EventType:   g.Key.EventType,
		Labels:      labels,
		RawProfile:  raw,
	})
	d.Metrics.ObserveUpload(d.Now().Sub(start))
	d.Metrics.DecInflight()
	return delivery.Classify(err)
}

func (d *Drainer) claimBatch(ctx context.Context, maxFiles int, maxBytes int64) ([]*spool.Claimed, bool, error) {
	batch := make([]*spool.Claimed, 0, maxFiles)
	var nbytes int64
	hitBudget := false
	for i := 0; i < maxFiles; i++ {
		if err := ctx.Err(); err != nil {
			d.releaseAll(batch)
			return nil, false, err
		}
		claimed, err := d.Source.Claim()
		if err != nil {
			d.releaseAll(batch)
			return nil, false, err
		}
		if claimed == nil {
			break
		}
		size := int64(len(claimed.Bytes))
		if len(batch) > 0 && nbytes+size > maxBytes {
			_ = d.Source.Release(claimed)
			hitBudget = true
			break
		}
		batch = append(batch, claimed)
		nbytes += size
		if nbytes >= maxBytes {
			hitBudget = true
			break
		}
	}
	return batch, hitBudget, nil
}

func (d *Drainer) releaseAll(held []*spool.Claimed) {
	for _, c := range held {
		if c == nil {
			continue
		}
		if err := d.Source.Release(c); err != nil {
			d.Log.Warn("release failed", "file", c.Name, "err", err)
		}
	}
}

func withoutClaim(held []*spool.Claimed, remove *spool.Claimed) []*spool.Claimed {
	out := held[:0]
	for _, c := range held {
		if c != remove {
			out = append(out, c)
		}
	}
	return out
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
