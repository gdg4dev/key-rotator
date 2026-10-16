package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Keys with several parts. Some APIs need values that only work as a matched
// set, such as an API key and the session id issued with it. Such a key keeps
// its main value in Key.Value and the others in Key.Parts, by name. Each place
// a key is sent names the part it sends (InjectRule.Part, "" for the main
// value). Rotation picks one whole key, so its parts always travel together:
// key 1's api key is never sent with key 2's session id.

// mainPart is the display name of a key's main value. A rule or part named
// "key" refers to that value.
const mainPart = "key"

// partName normalises the part a rule sends: "" (or "key") means the main value.
func partName(name string) string {
	n := strings.TrimSpace(name)
	if strings.EqualFold(n, mainPart) {
		return ""
	}
	return n
}

// part returns one of the key's values: "" is the main value.
func (k *Key) part(name string) (string, bool) {
	if name == "" {
		return k.Value, k.Value != ""
	}
	v, ok := k.Parts[name]
	return v, ok && v != ""
}

// hasParts reports whether the key has a value for every named part.
func (k *Key) hasParts(names []string) bool {
	for _, n := range names {
		if _, ok := k.part(n); !ok {
			return false
		}
	}
	return true
}

// partNames lists the key's named parts, sorted, for display.
func (k *Key) partNames() []string {
	names := make([]string, 0, len(k.Parts))
	for n := range k.Parts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// normalizeParts trims names and values and drops empty entries. A part named
// "key" would shadow the main value, so it is dropped too.
func normalizeParts(in map[string]string) map[string]string {
	out := map[string]string{}
	for n, v := range in {
		n, v = strings.TrimSpace(n), strings.TrimSpace(v)
		if n == "" || v == "" || partName(n) == "" {
			continue
		}
		out[n] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

const maxNameLen = 40

// validateName checks a group or part name from the API. Empty is allowed.
func validateName(what, name string) error {
	n := strings.TrimSpace(name)
	if len([]rune(n)) > maxNameLen || strings.ContainsFunc(n, unicode.IsControl) {
		return fmt.Errorf("%s must be 1-%d characters with no control characters", what, maxNameLen)
	}
	return nil
}

var errPartIsMain = errors.New(`a part cannot be called "key": that name means the key's main value`)

// validateParts checks the parts sent with a key through the API.
func validateParts(parts map[string]string) error {
	for n := range parts {
		if err := validateName("part name", n); err != nil {
			return err
		}
		if strings.TrimSpace(n) != "" && partName(n) == "" {
			return errPartIsMain
		}
	}
	return nil
}
