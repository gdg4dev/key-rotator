//go:build !windows

package main

import (
	"fmt"
	"os"
)

// handleServiceCommand returns true when the command was handled.
func handleServiceCommand(cmd string) bool {
	switch cmd {
	case "run":
		return false
	}
	fmt.Fprintln(os.Stderr, "Windows service control is only supported on Windows.")
	fmt.Fprintln(os.Stderr, "Run without -service to start in the foreground.")
	return true
}

func runAsService(_ *App) error {
	return fmt.Errorf("the Windows service mode is only available on Windows")
}
