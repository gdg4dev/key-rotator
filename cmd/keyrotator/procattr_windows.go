//go:build windows

package main

import "syscall"

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
