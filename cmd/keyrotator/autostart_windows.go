//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Per-user Run key: no Administrator rights needed, and it is the same place
// Windows' own Startup-apps settings page reads from.
const (
	autostartMechanism = "per-user Run key (HKCU)"
	runKey             = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	runValue           = "KeyRotator"
)

func autostartEnabled() bool {
	return exec.Command("reg", "query", runKey, "/v", runValue).Run() == nil
}

func autostartApply(enable bool, args []string) error {
	if !enable {
		// A missing value is already the desired state, so the error is ignored.
		_ = exec.Command("reg", "delete", runKey, "/v", runValue, "/f").Run()
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmdline := windowsQuote(exe)
	for _, a := range args {
		cmdline += " " + windowsQuote(a)
	}
	out, err := exec.Command("reg", "add", runKey, "/v", runValue, "/t", "REG_SZ", "/d", cmdline, "/f").CombinedOutput()
	if err != nil {
		return fmt.Errorf("reg add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// windowsQuote wraps an argument in quotes when it contains spaces, so the
// Run value is parsed back into the same argv at login.
func windowsQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
