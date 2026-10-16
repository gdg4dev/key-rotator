package main

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// metrics holds the counters the proxy updates on every request. Gauges that
// describe the live key pool are computed at scrape time from the config, so
// they can never disagree with the dashboard.
//
// The output is the Prometheus text exposition format, written by hand so the
// server core keeps its small dependency footprint.
type metrics struct {
	mu       sync.Mutex
	requests map[seriesKey]uint64 // provider, upstream status code
	limited  map[seriesKey]uint64 // provider, scope ("provider" or "key")
	retries  map[string]uint64
	latSum   map[string]float64 // seconds
	latCount map[string]uint64
}

type seriesKey struct{ provider, label string }

func newMetrics() *metrics {
	return &metrics{
		requests: map[seriesKey]uint64{},
		limited:  map[seriesKey]uint64{},
		retries:  map[string]uint64{},
		latSum:   map[string]float64{},
		latCount: map[string]uint64{},
	}
}

// request records one finished proxied request and how long it took.
func (m *metrics) request(provider string, code int, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[seriesKey{provider, strconv.Itoa(code)}]++
	m.latSum[provider] += d.Seconds()
	m.latCount[provider]++
}

// retry records one failover to another key within the same request.
func (m *metrics) retry(provider string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.retries[provider]++
}

// rateLimited records a request refused by a local rate limit.
func (m *metrics) rateLimited(provider, scope string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.limited[seriesKey{provider, scope}]++
}

// write renders every series. Label values are sorted so the output is stable
// between scrapes, which keeps diffs and tests readable.
func (m *metrics) write(w io.Writer, cfg Config, version string, now int64) {
	m.mu.Lock()
	requests := cloneSeries(m.requests)
	limited := cloneSeries(m.limited)
	retries := cloneStr(m.retries)
	latSum := cloneFloat(m.latSum)
	latCount := cloneStr64(m.latCount)
	m.mu.Unlock()

	fmt.Fprintf(w, "# HELP keyrotator_info Build information.\n# TYPE keyrotator_info gauge\n")
	fmt.Fprintf(w, "keyrotator_info{version=%q} 1\n", version)

	fmt.Fprintf(w, "# HELP keyrotator_providers Configured providers.\n# TYPE keyrotator_providers gauge\n")
	fmt.Fprintf(w, "keyrotator_providers %d\n", len(cfg.Providers))

	fmt.Fprintf(w, "# HELP keyrotator_keys Keys per provider and state.\n# TYPE keyrotator_keys gauge\n")
	for _, p := range sortedProviders(cfg.Providers) {
		counts := map[string]int{}
		for _, k := range p.Keys {
			counts[keyState(k, now)]++
		}
		for _, state := range []string{"ok", "cooldown", "invalid", "error", "disabled"} {
			fmt.Fprintf(w, "keyrotator_keys{provider=%s,state=%q} %d\n", quote(p.ID), state, counts[state])
		}
	}

	fmt.Fprintf(w, "# HELP keyrotator_requests_total Proxied requests by provider and status code.\n# TYPE keyrotator_requests_total counter\n")
	for _, k := range sortedSeries(requests) {
		fmt.Fprintf(w, "keyrotator_requests_total{provider=%s,code=%s} %d\n", quote(k.provider), quote(k.label), requests[k])
	}

	fmt.Fprintf(w, "# HELP keyrotator_rate_limited_total Requests refused by a local rate limit.\n# TYPE keyrotator_rate_limited_total counter\n")
	for _, k := range sortedSeries(limited) {
		fmt.Fprintf(w, "keyrotator_rate_limited_total{provider=%s,scope=%s} %d\n", quote(k.provider), quote(k.label), limited[k])
	}

	fmt.Fprintf(w, "# HELP keyrotator_retries_total Failovers to another key within a request.\n# TYPE keyrotator_retries_total counter\n")
	for _, p := range sortedStr(retries) {
		fmt.Fprintf(w, "keyrotator_retries_total{provider=%s} %d\n", quote(p), retries[p])
	}

	fmt.Fprintf(w, "# HELP keyrotator_request_duration_seconds Time spent proxying, by provider.\n# TYPE keyrotator_request_duration_seconds summary\n")
	for _, p := range sortedStr(latCount) {
		fmt.Fprintf(w, "keyrotator_request_duration_seconds_sum{provider=%s} %g\n", quote(p), latSum[p])
		fmt.Fprintf(w, "keyrotator_request_duration_seconds_count{provider=%s} %d\n", quote(p), latCount[p])
	}
}

// keyState maps a key to the same vocabulary the dashboard uses.
func keyState(k *Key, now int64) string {
	switch {
	case !k.Enabled:
		return "disabled"
	case k.Status == statusCooldown && k.CooldownUntil > now:
		return "cooldown"
	case k.Status == statusInvalid:
		return "invalid"
	case k.Status == statusError:
		return "error"
	default:
		return "ok"
	}
}

// quote escapes a Prometheus label value.
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

func sortedProviders(ps []*Provider) []*Provider {
	out := append([]*Provider(nil), ps...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sortedSeries[V any](m map[seriesKey]V) []seriesKey {
	keys := make([]seriesKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].provider != keys[j].provider {
			return keys[i].provider < keys[j].provider
		}
		return keys[i].label < keys[j].label
	})
	return keys
}

func sortedStr[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func cloneSeries[V any](m map[seriesKey]V) map[seriesKey]V {
	out := make(map[seriesKey]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneStr[V any](m map[string]V) map[string]V {
	out := make(map[string]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneFloat(m map[string]float64) map[string]float64 { return cloneStr(m) }
func cloneStr64(m map[string]uint64) map[string]uint64   { return cloneStr(m) }

// handleMetrics serves the Prometheus scrape endpoint. It sits behind the admin
// token when one is set, so the counters are not public on a shared network.
func (a *App) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	a.metrics.write(w, a.store.Snapshot(), version, time.Now().Unix())
}
