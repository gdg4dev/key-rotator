package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// Background mode. `keyrotator start` launches a detached server that keeps
// running after the terminal closes. `stop` and `status` go through the admin
// API, so they behave the same on every OS. Logs go to keyrotator.log and the
// process id to keyrotator.pid, both next to the config file.

func pidFile(cfgPath string) string { return filepath.Join(filepath.Dir(cfgPath), "keyrotator.pid") }
func logFile(cfgPath string) string { return filepath.Join(filepath.Dir(cfgPath), "keyrotator.log") }

// reachable reports whether a server answers its health check at c.base.
func (c *cliClient) reachable() bool {
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(c.base + "/healthz")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// waitFor polls until want() is true or the timeout passes.
func (c *cliClient) waitFor(want func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if want() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return want()
}

func (c *cliClient) cmdStart() int {
	if c.reachable() {
		return fail(fmt.Errorf("KeyRotator is already running at %s", c.base))
	}
	exe, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	if err := os.MkdirAll(filepath.Dir(c.cfgPath), 0o700); err != nil {
		return fail(err)
	}
	lf, err := os.OpenFile(logFile(c.cfgPath), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fail(err)
	}
	defer lf.Close()

	cmd := exec.Command(exe, autostartArgs(c.cfgPath)...)
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		return fail(err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	if err := os.WriteFile(pidFile(c.cfgPath), []byte(strconv.Itoa(pid)), 0o600); err != nil {
		return fail(err)
	}

	if !c.waitFor(c.reachable, 10*time.Second) {
		return fail(fmt.Errorf("started (pid %d) but it did not answer; see %s", pid, logFile(c.cfgPath)))
	}
	return ok(fmt.Sprintf("KeyRotator is running in the background (pid %d)\n  dashboard  %s\n  log        %s",
		pid, c.base+"/", logFile(c.cfgPath)))
}

func (c *cliClient) cmdStop() int {
	if !c.reachable() {
		_ = os.Remove(pidFile(c.cfgPath))
		return ok("KeyRotator is not running")
	}
	if err := c.call(http.MethodPost, "/api/shutdown", nil, nil); err != nil {
		return fail(err)
	}
	if !c.waitFor(func() bool { return !c.reachable() }, 10*time.Second) {
		return fail(errors.New("asked KeyRotator to stop, but it is still answering"))
	}
	_ = os.Remove(pidFile(c.cfgPath))
	return ok("KeyRotator stopped")
}

func (c *cliClient) cmdStatus() int {
	if !c.reachable() {
		fmt.Println("KeyRotator is not running at " + c.base)
		return 1
	}
	st, err := c.state()
	if err != nil {
		return fail(err)
	}
	fmt.Printf("KeyRotator %s is running at %s\n", st.Version, st.Addr)
	if raw, err := os.ReadFile(pidFile(c.cfgPath)); err == nil {
		fmt.Printf("pid        %s\n", string(raw))
	}
	fmt.Printf("config     %s\n", st.ConfigPath)
	return 0
}
