package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestExposedWithoutAuth(t *testing.T) {
	open := Settings{}
	withProxy := Settings{ProxyToken: "t"}
	withAdmin := Settings{AdminToken: "t"}

	cases := []struct {
		addr string
		st   Settings
		want bool
	}{
		{"127.0.0.1:8787", open, false},
		{"[::1]:8787", open, false},
		{"localhost:8787", open, false},
		{"0.0.0.0:8787", open, true},
		{"192.168.1.20:8787", open, true},
		{"0.0.0.0:8787", withProxy, false},
		{"0.0.0.0:8787", withAdmin, false},
	}
	for _, c := range cases {
		if got := exposedWithoutAuth(c.addr, c.st); got != c.want {
			t.Errorf("exposedWithoutAuth(%q, %+v) = %v, want %v", c.addr, c.st, got, c.want)
		}
	}
}

func TestShutdownStopsRun(t *testing.T) {
	app, err := NewApp(filepath.Join(t.TempDir(), "config.json"), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- app.Run(make(chan struct{})) }()

	// Wait for the listener to come up before asking it to stop.
	deadline := time.Now().Add(5 * time.Second)
	for app.Addr() == "127.0.0.1:0" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	app.Shutdown()
	app.Shutdown() // a second call must not panic

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after Shutdown")
	}
}

// The Activity page survives a restart: a new App on the same config reads
// back the request log the previous one saved.
func TestActivityLogSurvivesRestart(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.json")
	first, err := NewApp(cfg, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	first.logs.Add(LogEntry{Time: 1, Provider: "p", Key: "billing", Code: 200, OK: true})
	first.logs.Add(LogEntry{Time: 2, Provider: "p", Key: "key 1", Code: 429})
	first.persist("")

	second, err := NewApp(cfg, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got := second.logs.List()
	if len(got) != 2 || got[0].Key != "key 1" || got[1].Key != "billing" {
		t.Fatalf("after restart the log is %+v, want both entries, newest first", got)
	}
}
