package main

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// An alias is a key's human name within its provider: "billing", "team A".
// It is stored in Key.Label, so older configs keep working unchanged. Keys
// added without one get the lowest free "key N" ("key 1", "key 2", ...).
// Aliases are unique per provider, compared case-insensitively, because the
// proxy pins a key by alias (X-Rotator-Key) and an ambiguous alias would make
// that pin pick an arbitrary key.

const (
	defaultAliasPrefix = "key "
	maxAliasLen        = 64
)

var (
	errAliasTaken   = errors.New("another key on this provider already uses that alias")
	errAliasInvalid = errors.New("alias must be 1-64 characters with no control characters")
)

// validateAlias trims s and checks it. An empty result means "use the default".
func validateAlias(s string) (string, error) {
	alias := strings.TrimSpace(s)
	if alias == "" {
		return "", nil
	}
	if len([]rune(alias)) > maxAliasLen || strings.ContainsFunc(alias, unicode.IsControl) {
		return "", errAliasInvalid
	}
	return alias, nil
}

// aliasInUse reports whether any key in keys has this alias. The key self is
// skipped, so a key can keep its own alias; pass nil to check every key.
func aliasInUse(keys []*Key, alias string, self *Key) bool {
	for _, k := range keys {
		if k != self && strings.EqualFold(k.Label, alias) {
			return true
		}
	}
	return false
}

// nextAlias returns the lowest "key N" that no key uses. Removing "key 2" and
// adding another key therefore gets "key 2" back rather than "key 4".
func nextAlias(keys []*Key) string {
	for n := 1; ; n++ {
		alias := fmt.Sprintf("%s%d", defaultAliasPrefix, n)
		if !aliasInUse(keys, alias, nil) {
			return alias
		}
	}
}

// assignAliases names every key that has no label. Existing labels, including
// the masked-key labels older versions created, are left alone.
func assignAliases(keys []*Key) {
	for _, k := range keys {
		if strings.TrimSpace(k.Label) == "" {
			k.Label = nextAlias(keys)
		}
	}
}

// addKey appends k under the provider lock. An empty alias gets the default.
func (p *Provider) addKey(k *Key, alias string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if alias == "" {
		alias = nextAlias(p.Keys)
	} else if aliasInUse(p.Keys, alias, nil) {
		return errAliasTaken
	}
	k.Label = alias
	p.Keys = append(p.Keys, k)
	return nil
}

// renameKey changes one key's alias. found is false when the key does not exist.
func (p *Provider) renameKey(kid, alias string) (found bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var target *Key
	for _, k := range p.Keys {
		if k.ID == kid {
			target = k
			break
		}
	}
	if target == nil {
		return false, nil
	}
	if alias == "" {
		return true, errAliasInvalid
	}
	if aliasInUse(p.Keys, alias, target) {
		return true, errAliasTaken
	}
	target.Label = alias
	return true, nil
}
