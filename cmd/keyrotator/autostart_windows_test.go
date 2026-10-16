//go:build windows

package main

import "testing"

func TestWindowsQuote(t *testing.T) {
	cases := map[string]string{
		`C:\bin\keyrotator.exe`:   `C:\bin\keyrotator.exe`,
		`C:\Program Files\kr.exe`: `"C:\Program Files\kr.exe"`,
		`C:\My "apps"\kr.exe`:     `"C:\My \"apps\"\kr.exe"`,
	}
	for in, want := range cases {
		if got := windowsQuote(in); got != want {
			t.Errorf("windowsQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
