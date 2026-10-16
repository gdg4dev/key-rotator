//go:build unix && !darwin && !linux

package main

import (
	"errors"
	"os/exec"
)

// Start-at-login is not implemented here yet. The state reports
// Supported=false, so the dashboard hides the switch.
const autostartMechanism = ""

func autostartEnabled() bool { return false }

func autostartApply(bool, []string) error {
	return errors.New("start at login is not supported on this platform")
}

func openBrowser(url string) {
	_ = exec.Command("xdg-open", url).Start()
}
