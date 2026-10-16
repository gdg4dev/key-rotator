package main

import (
	"fmt"
	"strings"
	"unicode"
)

// Key pools. Every key belongs to one pool; a key with no pool is in "default".
// Each place a key is sent (an InjectRule with Source "key") names the pool it
// draws from. On every attempt the proxy picks one key from each pool that an
// active place needs, and each pool rotates on its own counter. Most providers
// only ever use the default pool.

const defaultPool = "default"

// poolKey normalises a pool name: case-insensitive, and empty means default.
func poolKey(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return defaultPool
	}
	return n
}

// storedPool is how a pool name is saved: trimmed, and "" for the default
// pool, so configs written before pools existed stay unchanged.
func storedPool(name string) string {
	n := strings.TrimSpace(name)
	if strings.EqualFold(n, defaultPool) {
		return ""
	}
	return n
}

// neededPools lists, in order, the pools that the active key places draw from.
// A provider with no active key place still needs a key, from the default pool.
func neededPools(rules []InjectRule) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range activeRules(rules) {
		if r.Source == "static" {
			continue
		}
		if pk := poolKey(r.Pool); !seen[pk] {
			seen[pk] = true
			out = append(out, pk)
		}
	}
	if len(out) == 0 {
		out = []string{defaultPool}
	}
	return out
}

// PickKeys chooses one key per needed pool for one attempt, under the provider
// lock. A pin (a key's alias or id) is used for that key's own pool. The bool
// reports that a pool had keys but every one was over its rate limit.
func (p *Provider) PickKeys(pin string, allow func(*Key) bool) ([]*Key, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var pinned *Key
	if pin != "" {
		for _, k := range p.Keys {
			if k.ID == pin || strings.EqualFold(k.Label, pin) {
				// A pinned key is an explicit choice; it ignores the per-key
				// budget but still counts against the provider limit.
				pinned = k
				break
			}
		}
	}
	var keys []*Key
	for _, pool := range neededPools(p.Inject) {
		if pinned != nil && poolKey(pinned.Pool) == pool {
			keys = append(keys, pinned)
			continue
		}
		k, blocked := p.selectKey(pool, allow)
		if k == nil {
			return nil, blocked
		}
		keys = append(keys, k)
	}
	return keys, false
}

// keyFinder returns a lookup from pool to the chosen key, for applyInjection.
func keyFinder(keys []*Key) func(pool string) *Key {
	return func(pool string) *Key {
		for _, k := range keys {
			if poolKey(k.Pool) == pool {
				return k
			}
		}
		return nil
	}
}

// probeFinder is what a key test sends: the tested key in the places of its
// own pool, and the first usable key of any other pool a place needs, so pairs
// of keys from two pools are still tested as a pair.
func (p *Provider) probeFinder(tested *Key) func(pool string) *Key {
	return func(pool string) *Key {
		if poolKey(tested.Pool) == pool {
			return tested
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, k := range p.Keys {
			if poolKey(k.Pool) == pool && k.Enabled && k.Status != statusInvalid {
				return k
			}
		}
		return nil
	}
}

// keyLabels joins the aliases and ids of the keys one request used.
func keyLabels(keys []*Key) (labels, ids string) {
	var l, i []string
	for _, k := range keys {
		l = append(l, k.Label)
		i = append(i, k.ID)
	}
	return strings.Join(l, ", "), strings.Join(i, ", ")
}

const maxNameLen = 40

// validateName checks a group or pool name from the API. Empty is allowed.
func validateName(what, name string) error {
	n := strings.TrimSpace(name)
	if len([]rune(n)) > maxNameLen || strings.ContainsFunc(n, unicode.IsControl) {
		return fmt.Errorf("%s must be 1-%d characters with no control characters", what, maxNameLen)
	}
	return nil
}

// markFailAll records one failed attempt against every key it used. When an
// attempt used keys from several pools, the upstream does not say which key it
// objected to, so each one is treated as failed.
func (p *Provider) markFailAll(keys []*Key, code int, msg string, cooldownSec int) {
	for _, k := range keys {
		p.markFail(k, code, msg, cooldownSec)
	}
}
