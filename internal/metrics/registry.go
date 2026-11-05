package metrics

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// Registry holds the §3.4 delivery instruments. Safe for concurrent use.
// Counters are process-lifetime totals; gauges reflect the latest observation.
type Registry struct {
	claimed   atomic.Int64
	delivered atomic.Int64
	retried   atomic.Int64
	rejected  atomic.Int64

	inflight atomic.Int64

	spoolFiles     atomic.Int64
	spoolBytes     atomic.Int64
	spoolOldestSec atomic.Uint64 // float64 bits

	batchFiles atomic.Int64
	batchBytes atomic.Int64
	aggRatio   atomic.Uint64 // float64 bits

	uploadMu    sync.Mutex
	uploadSum   float64
	uploadCount uint64
}

// New returns an empty Registry.
func New() *Registry { return &Registry{} }

// AddClaimed increments profiles_claimed_total.
func (r *Registry) AddClaimed(n int64) {
	if r == nil || n == 0 {
		return
	}
	r.claimed.Add(n)
}

// AddDelivered increments profiles_delivered_total.
func (r *Registry) AddDelivered(n int64) {
	if r == nil || n == 0 {
		return
	}
	r.delivered.Add(n)
}

// AddRetried increments profiles_retried_total (release+retry / backend path).
func (r *Registry) AddRetried(n int64) {
	if r == nil || n == 0 {
		return
	}
	r.retried.Add(n)
}

// AddRejected increments profiles_rejected_total.
func (r *Registry) AddRejected(n int64) {
	if r == nil || n == 0 {
		return
	}
	r.rejected.Add(n)
}

// IncInflight increments inflight_uploads.
func (r *Registry) IncInflight() {
	if r == nil {
		return
	}
	r.inflight.Add(1)
}

// DecInflight decrements inflight_uploads (floors at zero).
func (r *Registry) DecInflight() {
	if r == nil {
		return
	}
	for {
		cur := r.inflight.Load()
		if cur <= 0 {
			return
		}
		if r.inflight.CompareAndSwap(cur, cur-1) {
			return
		}
	}
}

// ObserveUpload records one Push latency sample.
func (r *Registry) ObserveUpload(d time.Duration) {
	if r == nil || d < 0 {
		return
	}
	r.uploadMu.Lock()
	r.uploadSum += d.Seconds()
	r.uploadCount++
	r.uploadMu.Unlock()
}

// ObserveBatch sets batch_files / batch_bytes gauges for the last claim batch.
func (r *Registry) ObserveBatch(files int, bytes int64) {
	if r == nil {
		return
	}
	r.batchFiles.Store(int64(files))
	r.batchBytes.Store(bytes)
}

// SetAggregationRatio sets aggregation_ratio (input profiles / POSTs in a pass).
func (r *Registry) SetAggregationRatio(ratio float64) {
	if r == nil {
		return
	}
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 {
		ratio = 0
	}
	r.aggRatio.Store(math.Float64bits(ratio))
}

// SetSpoolBacklog updates spool_files / spool_bytes / spool_oldest_age_seconds.
func (r *Registry) SetSpoolBacklog(files int, bytes int64, oldestAge time.Duration) {
	if r == nil {
		return
	}
	r.spoolFiles.Store(int64(files))
	r.spoolBytes.Store(bytes)
	sec := oldestAge.Seconds()
	if sec < 0 || math.IsNaN(sec) || math.IsInf(sec, 0) {
		sec = 0
	}
	r.spoolOldestSec.Store(math.Float64bits(sec))
}

// Snapshot is a point-in-time copy for tests and exposition.
type Snapshot struct {
	Claimed             int64
	Delivered           int64
	Retried             int64
	Rejected            int64
	Inflight            int64
	SpoolFiles          int64
	SpoolBytes          int64
	SpoolOldestAgeSec   float64
	BatchFiles          int64
	BatchBytes          int64
	AggregationRatio    float64
	UploadDurationSum   float64
	UploadDurationCount uint64
}

// Snapshot returns the current values.
func (r *Registry) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{}
	}
	r.uploadMu.Lock()
	sum, count := r.uploadSum, r.uploadCount
	r.uploadMu.Unlock()
	return Snapshot{
		Claimed:             r.claimed.Load(),
		Delivered:           r.delivered.Load(),
		Retried:             r.retried.Load(),
		Rejected:            r.rejected.Load(),
		Inflight:            r.inflight.Load(),
		SpoolFiles:          r.spoolFiles.Load(),
		SpoolBytes:          r.spoolBytes.Load(),
		SpoolOldestAgeSec:   math.Float64frombits(r.spoolOldestSec.Load()),
		BatchFiles:          r.batchFiles.Load(),
		BatchBytes:          r.batchBytes.Load(),
		AggregationRatio:    math.Float64frombits(r.aggRatio.Load()),
		UploadDurationSum:   sum,
		UploadDurationCount: count,
	}
}
