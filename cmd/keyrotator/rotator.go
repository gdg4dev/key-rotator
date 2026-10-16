package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Groups let some fields be sent only together. Rules with no group are
// independent: each one is sent whenever it is switched on. Rules that share a
// group name are all-or-nothing: if any member is switched off or has no name,
// none of the group is sent. The name is compared case-insensitively.

// ruleSendable reports whether a rule is switched on and has a name, so it can
// be written onto a request at all.
func ruleSendable(r InjectRule) bool {
	return r.Enabled && strings.TrimSpace(r.Name) != ""
}

// groupKey normalises a group name, so "Beta" and " beta " are the same group.
func groupKey(group string) string {
	return strings.ToLower(strings.TrimSpace(group))
}

// activeRules returns the rules to send on one request.
func activeRules(rules []InjectRule) []InjectRule {
	held := map[string]bool{}
	for _, r := range rules {
		if g := groupKey(r.Group); g != "" && !ruleSendable(r) {
			held[g] = true
		}
	}
	out := make([]InjectRule, 0, len(rules))
	for _, r := range rules {
		if !ruleSendable(r) {
			continue
		}
		if g := groupKey(r.Group); g != "" && held[g] {
			continue
		}
		out = append(out, r)
	}
	return out
}

// applyInjection writes every active rule onto the outgoing request. A key
// place sends the named part of the key chosen for its group (keyFor); a
// "static" rule always sends its fixed value.
func applyInjection(p *Provider, keyFor func(pool string) *Key, req *http.Request) {
	for _, r := range activeRules(p.Inject) {
		raw := r.Value
		if r.Source != "static" {
			k := keyFor(poolKey(r.Pool))
			if k == nil {
				continue
			}
			v, ok := k.part(partName(r.Part))
			if !ok {
				continue
			}
			raw = v
		}
		value := r.Prefix + raw + r.Suffix
		if strings.EqualFold(r.In, "query") {
			q := req.URL.Query()
			q.Set(r.Name, value)
			req.URL.RawQuery = q.Encode()
			continue
		}
		req.Header.Set(r.Name, value)
	}
}

const (
	statusOK       = "ok"
	statusCooldown = "cooldown"
	statusInvalid  = "invalid"
	statusError    = "error"
)

// selectKey returns the next key from one key group. Keys missing a part that
// a place of that group sends are skipped. allow, when non-nil, filters out keys
// that are over their own rate limit. It reports whether usable keys existed
// but were all filtered out. The caller holds p.mu.
func (p *Provider) selectKey(pool string, allow func(*Key) bool) (*Key, bool) {
	needed := neededPartsFor(p.Inject, pool)
	now := time.Now().Unix()
	var ready, cooling []*Key
	blocked := false
	for _, k := range p.Keys {
		if poolKey(k.Pool) != pool || !k.Enabled || k.Status == statusInvalid || !k.hasParts(needed) {
			continue
		}
		if allow != nil && !allow(k) {
			blocked = true
			continue
		}
		if k.CooldownUntil > now {
			cooling = append(cooling, k)
			continue
		}
		ready = append(ready, k)
	}
	candidates := ready
	if len(candidates) == 0 {
		candidates = cooling
	}
	if len(candidates) == 0 {
		return nil, blocked
	}
	switch p.Strategy {
	case "random":
		return candidates[rand.Intn(len(candidates))], false
	case "least_used":
		best := candidates[0]
		for _, k := range candidates[1:] {
			if k.LastUsed < best.LastUsed {
				best = k
			}
		}
		return best, false
	default: // round_robin, one position per key group
		if p.rr == nil {
			p.rr = map[string]int{}
		}
		// Use the current position, then move on, so the first request gets the
		// first key and the order matches the dashboard preview.
		i := p.rr[pool] % len(candidates)
		p.rr[pool] = i + 1
		return candidates[i], false
	}
}

// shortestKeyWait is how long until any usable key of this provider has a free
// rate-limit slot, used for a truthful Retry-After header.
func (p *Provider) shortestKeyWait(keyRPS float64, l *limiter) float64 {
	if keyRPS <= 0 {
		return 0
	}
	p.mu.Lock()
	keys := make([]*Key, 0, len(p.Keys))
	for _, k := range p.Keys {
		if k.Enabled && k.Status != statusInvalid {
			keys = append(keys, k)
		}
	}
	p.mu.Unlock()

	best := 0.0
	for _, k := range keys {
		w := l.wait("k:"+k.ID, keyRPS)
		if best == 0 || w < best {
			best = w
		}
	}
	return best
}

func (p *Provider) markOK(k *Key) {
	p.mu.Lock()
	defer p.mu.Unlock()
	k.Success++
	k.LastUsed = time.Now().Unix()
	k.Status = statusOK
	k.CooldownUntil = 0
	k.LastError = ""
}

func (p *Provider) markFail(k *Key, code int, msg string, cooldownSec int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().Unix()
	k.Errors++
	k.LastUsed = now
	k.LastError = strings.TrimSpace(fmt.Sprintf("%d %s", code, truncate(msg, 200)))
	switch {
	case code == 401 || code == 403:
		k.Status = statusInvalid
		k.CooldownUntil = 0
	case code == 429 || code >= 500 || code == 0:
		k.Status = statusCooldown
		k.CooldownUntil = now + int64(cooldownSec)
	default:
		k.Status = statusError
	}
}

// retryable reports whether a response means "try the next key".
func retryable(code int) bool {
	return code == 401 || code == 403 || code == 429 || code >= 500
}

// cooldownFor extracts a Retry-After hint (seconds) when present.
func cooldownFor(h map[string][]string, def int) int {
	for k, vv := range h {
		if !strings.EqualFold(k, "Retry-After") || len(vv) == 0 {
			continue
		}
		v := strings.TrimSpace(vv[0])
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			if secs > 3600 {
				secs = 3600
			}
			return secs
		}
		if t, err := time.Parse(time.RFC1123, v); err == nil {
			if d := int(time.Until(t).Seconds()); d > 0 {
				return d
			}
		}
	}
	return def
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// keyState is a key's state in the words the dashboard, TUI and metrics all
// use: ok, cooldown, invalid, error or disabled.
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
