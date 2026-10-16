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

// TestUnreachableAPIHidesKeysAndSparesThem points a provider at an address
// where nothing listens, with the key in the URL query. The client must get a
// 502 that names the API but never the key, and no key may be marked as
// failing: the API being down is not the keys' fault.
func TestUnreachableAPIHidesKeysAndSparesThem(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	base := down.URL
	down.Close() // nothing listens here any more

	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	keys := []*Key{
		{ID: "k1", Label: "a", Value: "secret-key-one", Enabled: true, Status: statusOK},
		{ID: "k2", Label: "b", Value: "secret-key-two", Enabled: true, Status: statusOK},
	}
	app.store.AddProvider(&Provider{
		ID: "p1", Name: "test", BaseURL: base, Enabled: true, Strategy: "round_robin",
		Inject: []InjectRule{{Enabled: true, In: "query", Name: "api_key", Source: "key"}},
		Keys:   keys,
	})

	rec := httptest.NewRecorder()
	app.handleProxy(rec, httptest.NewRequest(http.MethodGet, "/p/p1/search?q=x", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "could not reach "+base) {
		t.Errorf("error should name the API: %s", body)
	}
	if strings.Contains(body, "secret-key") {
		t.Fatalf("error leaked a key: %s", body)
	}
	for _, l := range app.logs.snapshot() {
		if strings.Contains(l.Msg, "secret-key") {
			t.Fatalf("activity log leaked a key: %s", l.Msg)
		}
	}
	for _, k := range keys {
		if k.Status != statusOK || k.CooldownUntil != 0 || k.LastError != "" {
			t.Errorf("key %s was marked: status=%s cooldown=%d error=%q", k.Label, k.Status, k.CooldownUntil, k.LastError)
		}
	}

	// The health check must not mark the key or show its value either.
	p := app.store.Provider("p1")
	code, msg := app.testAndRecord(p, keys[0])
	if code != 0 || strings.Contains(msg, "secret-key") || keys[0].Status != statusOK {
		t.Errorf("health check: code=%d msg=%q status=%s", code, msg, keys[0].Status)
	}
}
