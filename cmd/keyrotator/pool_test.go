package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// Each key place draws from its own pool, and each pool rotates on its own:
// the default pool takes turns in order while the one-key partner pool stays put.
// A pinned key replaces the pick for its own pool only.
func TestProxyUsesOneKeyPerPool(t *testing.T) {
	type sent struct{ main, partner string }
	var got []sent
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, sent{r.Header.Get("x-api-key"), r.URL.Query().Get("partner_key")})
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	app.store.AddProvider(&Provider{ID: "p", Name: "p", BaseURL: upstream.URL, Enabled: true, Strategy: "round_robin",
		Inject: []InjectRule{
			{Enabled: true, In: "header", Name: "x-api-key", Source: "key"},
			{Enabled: true, In: "query", Name: "partner_key", Source: "key", Pool: "Partner"},
		},
		Keys: []*Key{
			{ID: "a1", Label: "a1", Value: "main-1", Enabled: true, Status: statusOK},
			{ID: "a2", Label: "a2", Value: "main-2", Enabled: true, Status: statusOK},
			{ID: "b1", Label: "b1", Value: "partner-1", Enabled: true, Status: statusOK, Pool: "partner"},
		},
	})

	call := func(pin string) string {
		req := httptest.NewRequest(http.MethodPost, "/p/p/chat", bytes.NewBufferString(`{}`))
		if pin != "" {
			req.Header.Set("X-Rotator-Key", pin)
		}
		rec := httptest.NewRecorder()
		app.handleProxy(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
		}
		return rec.Header().Get("X-Rotator-Key")
	}
	call("")
	call("")
	if used := call("a2"); used != "a2, b1" {
		t.Errorf("X-Rotator-Key = %q, want both keys used", used)
	}

	want := []sent{{"main-1", "partner-1"}, {"main-2", "partner-1"}, {"main-2", "partner-1"}}
	if len(got) != len(want) {
		t.Fatalf("upstream saw %d requests, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("request %d sent %+v, want %+v", i+1, got[i], want[i])
		}
	}
}
