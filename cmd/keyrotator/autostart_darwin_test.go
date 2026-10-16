//go:build darwin

package main

import (
	"strings"
	"testing"
)

func TestLaunchAgentPlistEscapesArguments(t *testing.T) {
	plist := string(launchAgentPlist([]string{"/Applications/Key & Rotator/kr", "-config", "/Users/me/a<b>.json"}))
	for _, want := range []string{
		"<string>/Applications/Key &amp; Rotator/kr</string>",
		"<string>/Users/me/a&lt;b&gt;.json</string>",
		"<key>RunAtLoad</key><true/>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q\n%s", want, plist)
		}
	}
}
