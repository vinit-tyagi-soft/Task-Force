package httpapi_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHealthAndMetrics(t *testing.T) {
	e := setup(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		res, err := http.Get(e.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || !strings.Contains(string(body), `"ok"`) {
			t.Fatalf("%s: %d %s", path, res.StatusCode, body)
		}
	}
	res, err := http.Get(e.srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(body), `taskforce_webhook_deliveries{status="pending"}`) {
		t.Fatalf("metrics: %d %s", res.StatusCode, body)
	}
}
