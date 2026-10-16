package main

// Start-at-login support. The operating system is the source of truth: the
// per-OS files implement the three hooks below, and nothing is duplicated in
// the config file, so the setting cannot drift from what the OS will run.

// autostartState is what the admin API reports about start-at-login.
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
