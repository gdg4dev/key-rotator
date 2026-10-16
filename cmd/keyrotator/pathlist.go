package main

import "strings"

// Helpers for editing a PATH-style list ("a;b;c" on Windows, "a:b:c" on Unix).
// They are pure, so the rules are tested on every platform. On Windows entries
// compare case-insensitively and may carry quotes or a trailing backslash.

// samePathEntry reports whether a PATH entry names dir.
func samePathEntry(entry, dir string, fold bool) bool {
	clean := func(s string) string { return strings.TrimRight(strings.Trim(strings.TrimSpace(s), `"`), `\/`) }
	a, b := clean(entry), clean(dir)
	if fold {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func pathListHas(list, dir, sep string, fold bool) bool {
	for _, e := range strings.Split(list, sep) {
		if e != "" && samePathEntry(e, dir, fold) {
			return true
		}
	}
	return false
}

// pathListAdd appends dir unless the list already has it.
func pathListAdd(list, dir, sep string, fold bool) string {
	if pathListHas(list, dir, sep, fold) {
		return list
	}
	if strings.TrimSpace(list) == "" {
		return dir
	}
	return strings.TrimRight(list, sep) + sep + dir
}

// pathListRemove drops every entry naming dir and any empty entries it leaves.
func pathListRemove(list, dir, sep string, fold bool) string {
	var keep []string
	for _, e := range strings.Split(list, sep) {
		if e != "" && !samePathEntry(e, dir, fold) {
			keep = append(keep, e)
		}
	}
	return strings.Join(keep, sep)
}
