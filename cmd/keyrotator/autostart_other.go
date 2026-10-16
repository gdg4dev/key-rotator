//go:build !windows && !darwin && !linux

package main

import "errors"

// Start-at-login is not implemented for this platform. The state reports
// Supported=false, so the UI hides the toggle.
const autostartMechanism = ""

func autostartEnabled() bool { return false }

func autostartApply(bool, []string) error {
	return errors.New("start at login is not supported on this platform")
}
