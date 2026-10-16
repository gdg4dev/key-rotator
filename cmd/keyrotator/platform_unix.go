//go:build unix

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// detachAttr starts the child in a new session, so the terminal's hangup
// signal does not reach it when the terminal closes.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// ---- "use from any terminal": a link in ~/.local/bin

const pathMechanism = "a link in ~/.local/bin"

func pathLink() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin", "keyrotator"), nil
}

func onPath() bool {
	link, err := pathLink()
	if err != nil {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	target, err := os.Readlink(link)
	return err == nil && target == exe
}

func setOnPath(on bool) error {
	link, err := pathLink()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if !on {
		// Only remove a link that points at this binary.
		if target, err := os.Readlink(link); err == nil && target == exe {
			return os.Remove(link)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(link); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s already exists and is not a link; move it away first", link)
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	}
	return os.Symlink(exe, link)
}

// pathNote says what is still needed when ~/.local/bin is not on PATH, which
// is the default on macOS and on some Linux distributions.
func pathNote() string {
	link, err := pathLink()
	if err != nil || pathListHas(os.Getenv("PATH"), filepath.Dir(link), ":", false) {
		return "Open a new terminal for it to take effect."
	}
	return `~/.local/bin is not on your PATH yet. Add this line to your shell profile (~/.zshrc or ~/.bashrc): export PATH="$HOME/.local/bin:$PATH"`
}
