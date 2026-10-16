//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

// Per-user Run key: no Administrator rights needed, and it is the same place
// Windows' own Startup-apps settings page reads from.
//
// The registry is read through the Windows API, not by running reg.exe. The
// dashboard polls this state every few seconds, and a child console process per
// poll would open and close a terminal window each time.
const (
	autostartMechanism = "per-user Run key (HKCU)"
	runKeyPath         = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue           = "KeyRotator"
)

func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValue)
	return err == nil
}

func autostartApply(enable bool, args []string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if !enable {
		// A missing value is already the desired state.
		if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
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
	return k.SetStringValue(runValue, cmdline)
}

// windowsQuote wraps an argument in quotes when it contains spaces, so the
// Run value is parsed back into the same argv at login.
func windowsQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func openBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// detachAttr starts the child with no console attached and in its own process
// group, so closing the terminal that started it does not stop it.
func detachAttr() *syscall.SysProcAttr {
	const detachedProcess = 0x00000008
	const createNewProcessGroup = 0x00000200
	return &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
		HideWindow:    true,
	}
}
