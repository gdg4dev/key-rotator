package main

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestSelectKey(t *testing.T) {
	hour := time.Now().Add(time.Hour).Unix()
	cases := []struct {
		name string
		keys []*Key
		want string
	}{
		{"skips disabled and invalid keys", []*Key{
			{ID: "off", Enabled: false, Status: statusOK},
			{ID: "bad", Enabled: true, Status: statusInvalid},
			{ID: "good", Enabled: true, Status: statusOK},
		}, "good"},
		{"prefers a ready key over a cooling one", []*Key{
			{ID: "cool", Enabled: true, Status: statusCooldown, CooldownUntil: hour},
			{ID: "ready", Enabled: true, Status: statusOK},
		}, "ready"},
		{"falls back to a cooling key when nothing is ready", []*Key{
			{ID: "cool", Enabled: true, Status: statusCooldown, CooldownUntil: hour},
		}, "cool"},
	}
	for _, c := range cases {
		p := &Provider{Strategy: "round_robin", Keys: c.keys}
		for i := 0; i < 3; i++ {
			if k, _ := p.selectKey(defaultPool, nil); k == nil || k.ID != c.want {
				t.Errorf("%s: pick %d = %+v, want %s", c.name, i, k, c.want)
			}
		}
	}
}

func TestRoundRobinSpreadsEvenly(t *testing.T) {
	p := &Provider{Strategy: "round_robin", Keys: []*Key{
		{ID: "a", Enabled: true, Status: statusOK},
		{ID: "b", Enabled: true, Status: statusOK},
	}}
	seen := map[string]int{}
	for i := 0; i < 4; i++ {
		k, _ := p.selectKey(defaultPool, nil)
		seen[k.ID]++
	}
	if seen["a"] != 2 || seen["b"] != 2 {
		t.Fatalf("distribution = %v, want 2 each", seen)
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
	applyInjection(p, func(string) *Key { return &Key{Value: "sk-123"} }, req)

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
		t.Errorf("query = %q, want api_key added and foo kept", req.URL.RawQuery)
	}
}

func TestLimiterBurstThenBlocks(t *testing.T) {
	l := newLimiter()
	for i := 0; i < 2; i++ {
		if !l.allow("p:test", 2) {
			t.Fatalf("request %d within the burst was denied", i)
		}
	}
	ok, wait := l.take("p:test", 2)
	if ok || wait <= 0 {
		t.Fatalf("over the burst: allowed=%v wait=%v, want denied with a retry delay", ok, wait)
	}
}
