package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// Key groups and parts together: the default group sends an api key with its
// own session id, the partner group sends its key in the URL. Each request
// takes one key from each group, and a key's parts always travel together,
// even with random rotation. A key missing a part its group sends is skipped.
func TestKeyGroupsAndPartsTravelTogether(t *testing.T) {
	type sent struct{ key, session, partner string }
	var seen []sent
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, sent{r.Header.Get("x-api-key"), r.Header.Get("x-session-id"), r.URL.Query().Get("partner_key")})
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	session := func(v string) map[string]string { return map[string]string{"session_id": v} }
	app.store.AddProvider(&Provider{ID: "p", Name: "p", BaseURL: upstream.URL, Enabled: true, Strategy: "random",
		Inject: []InjectRule{
			{Enabled: true, In: "header", Name: "x-api-key", Source: "key"},
			{Enabled: true, In: "header", Name: "x-session-id", Source: "key", Part: "session_id"},
			{Enabled: true, In: "query", Name: "partner_key", Source: "key", Pool: "Partner"},
		},
		Keys: []*Key{
			{ID: "a", Label: "a", Value: "key-a", Parts: session("session-a"), Enabled: true, Status: statusOK},
			{ID: "b", Label: "b", Value: "key-b", Parts: session("session-b"), Enabled: true, Status: statusOK},
			{ID: "c", Label: "c", Value: "key-c", Enabled: true, Status: statusOK}, // no session id: skipped
			{ID: "p1", Label: "p1", Value: "partner-1", Enabled: true, Status: statusOK, Pool: "partner"},
		},
	})

	for i := 0; i < 40; i++ {
		rec := httptest.NewRecorder()
		app.handleProxy(rec, httptest.NewRequest(http.MethodPost, "/p/p/chat", bytes.NewBufferString(`{}`)))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d, body %s", i, rec.Code, rec.Body.String())
		}
	}
	want := map[string]string{"key-a": "session-a", "key-b": "session-b"}
	for i, s := range seen {
		if want[s.key] != s.session {
			t.Fatalf("request %d sent a mismatched pair: key=%q session=%q", i, s.key, s.session)
		}
		if s.partner != "partner-1" {
			t.Fatalf("request %d sent partner key %q, want the partner group's key", i, s.partner)
		}
	}
}
