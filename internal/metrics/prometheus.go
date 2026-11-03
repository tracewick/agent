package metrics

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Handler serves Prometheus text exposition for the Registry on GET /metrics
// (and any path — callers typically mount it at /metrics only).
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		if req.Method == http.MethodHead {
			return
		}
		_ = r.WritePrometheus(w)
	})
}

// WritePrometheus writes the OpenMetrics/Prometheus 0.0.4 text format.
func (r *Registry) WritePrometheus(w io.Writer) error {
	s := r.Snapshot()
	var b strings.Builder
	writeGauge(&b, "spool_files", "Unclaimed spool files currently on disk.", float64(s.SpoolFiles))
	writeGauge(&b, "spool_bytes", "Total bytes of unclaimed spool files on disk.", float64(s.SpoolBytes))
	writeGauge(&b, "spool_oldest_age_seconds", "Age in seconds of the oldest unclaimed spool file.", s.SpoolOldestAgeSec)
	writeCounter(&b, "profiles_claimed_total", "Spool profiles claimed for delivery.", float64(s.Claimed))
	writeCounter(&b, "profiles_delivered_total", "Spool profiles acknowledged after Pyroscope 2xx.", float64(s.Delivered))
	writeCounter(&b, "profiles_retried_total", "Spool profiles released for retry after backend failure.", float64(s.Retried))
	writeCounter(&b, "profiles_rejected_total", "Spool profiles moved to rejected/ or deleted as bad data.", float64(s.Rejected))
	writeSummary(&b, "upload_duration_seconds", "Pyroscope Push latency in seconds.", s.UploadDurationSum, s.UploadDurationCount)
	writeGauge(&b, "batch_files", "Files in the last claim batch.", float64(s.BatchFiles))
	writeGauge(&b, "batch_bytes", "Bytes in the last claim batch.", float64(s.BatchBytes))
	writeGauge(&b, "inflight_uploads", "In-flight Pyroscope Push requests.", float64(s.Inflight))
	writeGauge(&b, "aggregation_ratio", "Profiles per Push in the last drain pass (POST reduction).", s.AggregationRatio)
	_, err := io.WriteString(w, b.String())
	return err
}

func writeGauge(b *strings.Builder, name, help string, v float64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s gauge\n%s %s\n", name, help, name, name, formatFloat(v))
}

func writeCounter(b *strings.Builder, name, help string, v float64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s counter\n%s %s\n", name, help, name, name, formatFloat(v))
}

func writeSummary(b *strings.Builder, name, help string, sum float64, count uint64) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s summary\n%s_sum %s\n%s_count %d\n",
		name, help, name, name, formatFloat(sum), name, count)
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
