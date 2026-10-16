//go:build linux

package main

import "testing"

func TestDesktopQuote(t *testing.T) {
	cases := map[string]string{
		"/usr/bin/keyrotator": "/usr/bin/keyrotator",
		"/home/me/My Apps/kr": `"/home/me/My Apps/kr"`,
		`/tmp/50%off/kr`:      `/tmp/50%%off/kr`,
		`/tmp/a"b$c`:          `"/tmp/a\"b\$c"`,
	}
	for in, want := range cases {
		if got := desktopQuote(in); got != want {
			t.Errorf("desktopQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
