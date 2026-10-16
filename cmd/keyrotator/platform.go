package main

// Everything that differs between operating systems lives in one file per OS.
// Each file implements the same small set of hooks, so the rest of the program
// never checks runtime.GOOS:
//
//	platform_windows.go  Windows
//	platform_darwin.go   macOS
//	platform_linux.go    Linux
//	platform_other.go    other Unix systems (FreeBSD, OpenBSD, ...)
//	platform_unix.go     shared by every Unix system, macOS and Linux included
//
// The hooks:
//
//	openBrowser(url)             open the dashboard in the default browser
//	detachAttr()                 process attributes that let `keyrotator start`
//	                             outlive the terminal that launched it (unix.go)
//	autostartMechanism           human name of the start-at-login method,
//	                             or "" when the OS has none
//	autostartEnabled()           whether start-at-login is registered
//	autostartApply(on, args)     register or remove start-at-login
//	pathMechanism                how "use from any terminal" works on this OS
//	onPath(), setOnPath(on)      whether `keyrotator` runs from any terminal; change it
//	pathNote()                   what the user still has to do, if anything
//
// The Windows service (service_windows.go, with a stub in service_other.go) is
// kept separate because it is Windows-only and much larger.

// autostartState is what the admin API reports about start-at-login. The OS is
// the source of truth; nothing about it is stored in the config file, so the
// setting cannot drift from what the OS will actually run.
type autostartState struct {
	Supported bool   `json:"supported"`
	Enabled   bool   `json:"enabled"`
	Mechanism string `json:"mechanism"`
}

// autostartArgs is what the OS runs at login. The config path is pinned so the
// login instance reads the same state as the one you configured.
func autostartArgs(cfgPath string) []string {
	return []string{"-quiet", "-config", cfgPath}
}

// pathState is what the admin API reports about running keyrotator from any
// terminal. Like start-at-login, the OS is the source of truth.
type pathState struct {
	Enabled   bool   `json:"enabled"`
	Mechanism string `json:"mechanism"`
	Note      string `json:"note"`
}

func currentPath() pathState {
	return pathState{Enabled: onPath(), Mechanism: pathMechanism, Note: pathNote()}
}

func currentAutostart() autostartState {
	if autostartMechanism == "" {
		return autostartState{}
	}
	return autostartState{
		Supported: true,
		Enabled:   autostartEnabled(),
		Mechanism: autostartMechanism,
	}
}
