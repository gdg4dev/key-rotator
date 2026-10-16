//go:build unix

package main

import "syscall"

// detachAttr starts the child in a new session, so the terminal's hangup
// signal does not reach it when the terminal closes.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
