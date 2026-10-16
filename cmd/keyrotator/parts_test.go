package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// A key's parts always travel together: whatever key is picked, its api key
// and its session id arrive as a matched pair, even with random rotation. A
// key that lacks a part a place sends is never used.
func TestKeyPartsAreSentAsMatchedPairs(t *testing.T) {
	type pair struct{ key, session string }
	var seen []pair
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		seen = append(seen, pair{q.Get("api_key"), r.Header.Get("x-session-id")})
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
			{Enabled: true, In: "query", Name: "api_key", Source: "key"},
			{Enabled: true, In: "header", Name: "x-session-id", Source: "key", Part: "session_id"},
		},
		Keys: []*Key{
			{ID: "a", Label: "a", Value: "key-a", Parts: session("session-a"), Enabled: true, Status: statusOK},
			{ID: "b", Label: "b", Value: "key-b", Parts: session("session-b"), Enabled: true, Status: statusOK},
			{ID: "c", Label: "c", Value: "key-c", Parts: session("session-c"), Enabled: true, Status: statusOK},
			{ID: "d", Label: "d", Value: "key-d", Enabled: true, Status: statusOK}, // no session id
		},
	})

	for i := 0; i < 40; i++ {
		rec := httptest.NewRecorder()
		app.handleProxy(rec, httptest.NewRequest(http.MethodPost, "/p/p/chat", bytes.NewBufferString(`{}`)))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d, body %s", i, rec.Code, rec.Body.String())
		}
	}
	want := map[string]string{"key-a": "session-a", "key-b": "session-b", "key-c": "session-c"}
	for i, s := range seen {
		if want[s.key] != s.session {
			t.Fatalf("request %d sent a mismatched pair: api_key=%q session=%q", i, s.key, s.session)
		}
	}
}
