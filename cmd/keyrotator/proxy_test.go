package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// TestProxyFailoverAndMetrics drives a real request through the proxy against
// a fake upstream that rejects one of two keys. Round robin tries the rejected
// key first, so the request must fail over to the good key, and the metrics
// must show exactly that.
func TestProxyFailoverAndMetrics(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer bad-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	app.store.AddProvider(&Provider{
		ID: "p1", Name: "test", BaseURL: upstream.URL, Enabled: true, Strategy: "round_robin",
		Inject: []InjectRule{{Enabled: true, In: "header", Name: "Authorization", Source: "key", Prefix: "Bearer "}},
		Keys: []*Key{
			{ID: "k1", Value: "bad-key", Enabled: true, Status: statusOK},
			{ID: "k2", Value: "good-key", Enabled: true, Status: statusOK},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/p/p1/chat/completions", bytes.NewBufferString(`{}`))
	rec := httptest.NewRecorder()
	app.handleProxy(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proxied status = %d, want 200 after failover; body=%s", rec.Code, rec.Body.String())
	}

	scrape := httptest.NewRecorder()
	app.handleMetrics(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	out := scrape.Body.String()

	for _, want := range []string{
		`keyrotator_requests_total{provider="p1",code="200"} 1`,
		`keyrotator_retries_total{provider="p1"} 1`,
		`keyrotator_keys{provider="p1",state="ok"} 1`,
		`keyrotator_providers 1`,
		`keyrotator_request_duration_seconds_count{provider="p1"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics missing %q\n--- output ---\n%s", want, out)
		}
	}
}
