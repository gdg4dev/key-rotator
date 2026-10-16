//go:build darwin

package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	autostartMechanism = "LaunchAgent (~/Library/LaunchAgents)"
	launchLabel        = "com.keyrotator.agent"
)

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchLabel+".plist"), nil
}

func autostartEnabled() bool {
	p, err := launchAgentPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func autostartApply(enable bool, args []string) error {
	p, err := launchAgentPath()
	if err != nil {
		return err
	}
	if !enable {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, launchAgentPlist(append([]string{exe}, args...)), 0o644)
}

// launchAgentPlist builds the LaunchAgent. KeepAlive restarts the process if it
// crashes, but not after a clean exit, so "Quit" stays quit.
func launchAgentPlist(program []string) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0"><dict>` + "\n")
	fmt.Fprintf(&b, "  <key>Label</key><string>%s</string>\n", launchLabel)
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, a := range program {
		b.WriteString("    <string>")
		_ = xml.EscapeText(&b, []byte(a))
		b.WriteString("</string>\n")
	}
	b.WriteString("  </array>\n")
	b.WriteString("  <key>RunAtLoad</key><true/>\n")
	b.WriteString("  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>\n")
	b.WriteString("</dict></plist>\n")
	return b.Bytes()
}
