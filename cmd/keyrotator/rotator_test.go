package main

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestSelectKeySkipsDisabledAndInvalid(t *testing.T) {
	p := &Provider{Strategy: "round_robin", Keys: []*Key{
		{ID: "a", Value: "A", Enabled: true, Status: statusOK},
		{ID: "b", Value: "B", Enabled: false, Status: statusOK},
		{ID: "c", Value: "C", Enabled: true, Status: statusInvalid},
	}}
	for i := 0; i < 4; i++ {
		k, _ := p.selectKey(nil)
		if k == nil || k.ID != "a" {
			t.Fatalf("pick %d: got %+v, want key a only", i, k)
		}
	}
}

func TestSelectKeyPrefersReadyOverCooling(t *testing.T) {
	p := &Provider{Strategy: "round_robin", Keys: []*Key{
		{ID: "cool", Value: "X", Enabled: true, Status: statusCooldown, CooldownUntil: time.Now().Add(time.Hour).Unix()},
		{ID: "ready", Value: "Y", Enabled: true, Status: statusOK},
	}}
	for i := 0; i < 3; i++ {
		if k, _ := p.selectKey(nil); k == nil || k.ID != "ready" {
			t.Fatalf("pick %d: got %+v, want ready key", i, k)
		}
	}
}

func TestSelectKeyFallsBackToCoolingWhenNothingReady(t *testing.T) {
	p := &Provider{Strategy: "round_robin", Keys: []*Key{
		{ID: "cool", Value: "X", Enabled: true, Status: statusCooldown, CooldownUntil: time.Now().Add(time.Hour).Unix()},
	}}
	if k, _ := p.selectKey(nil); k == nil || k.ID != "cool" {
		t.Fatalf("got %+v, want the cooling key as a last resort", k)
	}
}

func TestSelectKeyRoundRobinCoversPool(t *testing.T) {
	p := &Provider{Strategy: "round_robin", Keys: []*Key{
		{ID: "a", Enabled: true, Status: statusOK},
		{ID: "b", Enabled: true, Status: statusOK},
	}}
	seen := map[string]int{}
	for i := 0; i < 4; i++ {
		k, _ := p.selectKey(nil)
		seen[k.ID]++
	}
	if seen["a"] != 2 || seen["b"] != 2 {
		t.Fatalf("round robin distribution = %v, want 2 each", seen)
	}
}

func TestApplyInjectionHeaderAndQuery(t *testing.T) {
	p := &Provider{Inject: []InjectRule{
		{Enabled: true, In: "header", Name: "Authorization", Source: "key", Prefix: "Bearer "},
		{Enabled: true, In: "query", Name: "api_key", Source: "key"},
		{Enabled: true, In: "header", Name: "x-api-version", Source: "static", Value: "2024-02-01"},
		{Enabled: false, In: "header", Name: "X-Off", Source: "static", Value: "nope"},
	}}
	req, _ := http.NewRequest("GET", "http://example.test/v1/models?foo=bar", nil)
	applyInjection(p, &Key{Value: "sk-123"}, req)

	if got := req.Header.Get("Authorization"); got != "Bearer sk-123" {
		t.Errorf("Authorization = %q", got)
	}
	if got := req.Header.Get("x-api-version"); got != "2024-02-01" {
		t.Errorf("x-api-version = %q", got)
	}
	if got := req.Header.Get("X-Off"); got != "" {
		t.Errorf("disabled rule was applied: %q", got)
	}
	q, _ := url.ParseQuery(req.URL.RawQuery)
	if q.Get("api_key") != "sk-123" || q.Get("foo") != "bar" {
		t.Errorf("query = %q, want api_key and existing foo kept", req.URL.RawQuery)
	}
}

func TestLimiterBurstThenBlocks(t *testing.T) {
	l := newLimiter()
	const rps = 2.0
	for i := 0; i < 2; i++ {
		if !l.allow("p:test", rps) {
			t.Fatalf("request %d within burst was denied", i)
		}
	}
	ok, wait := l.take("p:test", rps)
	if ok {
		t.Fatal("request over the burst was allowed")
	}
	if wait <= 0 {
		t.Fatalf("wait = %v, want a positive retry delay", wait)
	}
}

func TestLimiterUnlimitedWhenZero(t *testing.T) {
	l := newLimiter()
	for i := 0; i < 1000; i++ {
		if !l.allow("p:free", 0) {
			t.Fatal("rps 0 must mean unlimited")
		}
	}
}
