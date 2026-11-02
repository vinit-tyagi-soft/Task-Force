package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Prometheus-style histogram buckets, in seconds. Counts are cumulative.
var latencyBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

// httpMetrics counts requests in memory. It has no path labels, so task ids
// and search text cannot show up in a scrape.
type httpMetrics struct {
	mu       sync.Mutex
	byStatus map[int]uint64
	bucket   []uint64
	sum      float64
	count    uint64
}

func newHTTPMetrics() *httpMetrics {
	return &httpMetrics{byStatus: map[int]uint64{}, bucket: make([]uint64, len(latencyBuckets))}
}

func (m *httpMetrics) observe(status int, seconds float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byStatus[status]++
	m.count++
	m.sum += seconds
	for i, bound := range latencyBuckets {
		if seconds <= bound {
			m.bucket[i]++
		}
	}
}

func (m *httpMetrics) write(w io.Writer, pending, failed int, haveWebhook bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fmt.Fprintln(w, "# HELP taskforce_http_requests_total HTTP responses by status code, excluding health and metrics scrapes.")
	fmt.Fprintln(w, "# TYPE taskforce_http_requests_total counter")
	for _, status := range []int{200, 201, 202, 204, 400, 401, 403, 404, 409, 412, 422, 500} {
		if m.byStatus[status] == 0 {
			continue
		}
		fmt.Fprintf(w, "taskforce_http_requests_total{status=%q} %d\n", strconv.Itoa(status), m.byStatus[status])
	}
	fmt.Fprintln(w, "# HELP taskforce_http_request_duration_seconds HTTP request duration, excluding health and metrics scrapes.")
	fmt.Fprintln(w, "# TYPE taskforce_http_request_duration_seconds histogram")
	for i, bound := range latencyBuckets {
		fmt.Fprintf(w, "taskforce_http_request_duration_seconds_bucket{le=%q} %d\n", strconv.FormatFloat(bound, 'f', -1, 64), m.bucket[i])
	}
	fmt.Fprintf(w, "taskforce_http_request_duration_seconds_bucket{le=\"+Inf\"} %d\n", m.count)
	fmt.Fprintf(w, "taskforce_http_request_duration_seconds_sum %s\n", strconv.FormatFloat(m.sum, 'f', -1, 64))
	fmt.Fprintf(w, "taskforce_http_request_duration_seconds_count %d\n", m.count)
	if !haveWebhook {
		return
	}
	fmt.Fprintln(w, "# HELP taskforce_webhook_deliveries Webhook deliveries by status. Counts only.")
	fmt.Fprintln(w, "# TYPE taskforce_webhook_deliveries gauge")
	fmt.Fprintf(w, "taskforce_webhook_deliveries{status=%q} %d\n", "pending", pending)
	fmt.Fprintf(w, "taskforce_webhook_deliveries{status=%q} %d\n", "failed", failed)
}

func skipTelemetry(path string) bool {
	switch path {
	case "/healthz", "/readyz", "/metrics", "/openapi.json":
		return true
	default:
		return false
	}
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Pool.Ping(ctx); err != nil {
		s.log.Error("readiness failed", "error", err)
		writeJSON(w, r, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) serveMetrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	var pending, failed int
	err := s.store.Pool.QueryRow(ctx, `SELECT pending, failed FROM tf_webhook_stats()`).Scan(&pending, &failed)
	if err != nil {
		s.log.Error("webhook stats failed", "error", err)
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	s.metrics.write(w, pending, failed, err == nil)
}
