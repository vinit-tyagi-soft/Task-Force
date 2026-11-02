package httpapi

import (
	"bytes"
	"strings"
	"testing"
)

func TestMetricsHistogram(t *testing.T) {
	m := newHTTPMetrics()
	m.observe(200, 0.2)
	m.observe(500, 6)
	var buf bytes.Buffer
	m.write(&buf, 1, 2, true)
	body := buf.String()
	for _, want := range []string{
		`taskforce_http_requests_total{status="200"} 1`,
		`taskforce_http_requests_total{status="500"} 1`,
		`taskforce_http_request_duration_seconds_bucket{le="0.05"} 0`,
		`taskforce_http_request_duration_seconds_bucket{le="0.25"} 1`,
		`taskforce_http_request_duration_seconds_bucket{le="+Inf"} 2`,
		`taskforce_http_request_duration_seconds_count 2`,
		`taskforce_webhook_deliveries{status="pending"} 1`,
		`taskforce_webhook_deliveries{status="failed"} 2`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s\n%s", want, body)
		}
	}
	if strings.Contains(body, "/api/") || strings.Contains(body, "user") {
		t.Fatalf("metrics leaked a path or identity:\n%s", body)
	}
}
