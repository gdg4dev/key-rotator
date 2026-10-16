//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const serviceName = "KeyRotator"
const serviceDisplayName = "API Key Rotator"

var (
	advapi32                       = syscall.NewLazyDLL("advapi32.dll")
	procStartServiceCtrlDispatcher = advapi32.NewProc("StartServiceCtrlDispatcherW")
	procRegisterServiceCtrlHandler = advapi32.NewProc("RegisterServiceCtrlHandlerW")
	procSetServiceStatus           = advapi32.NewProc("SetServiceStatus")
)

const (
	svcWin32OwnProcess = 0x00000010
	svcAcceptStop      = 0x00000001
	svcAcceptShutdown  = 0x00000004

	svcStateStartPending = 0x00000002
	svcStateRunning      = 0x00000004
	svcStateStopPending  = 0x00000003
	svcStateStopped      = 0x00000001

	svcControlStop     = 0x00000001
	svcControlShutdown = 0x00000005
)

type serviceStatus struct {
	ServiceType             uint32
	CurrentState            uint32
	ControlsAccepted        uint32
	Win32ExitCode           uint32
	ServiceSpecificExitCode uint32
	CheckPoint              uint32
	WaitHint                uint32
}

type serviceTableEntry struct {
	ServiceName *uint16
	ServiceProc uintptr
}

var (
	statusHandle uintptr
	statusMu     sync.Mutex
	status       serviceStatus
	stopOnce     sync.Once
	serviceStop  chan struct{}
)

func setStatus(state uint32) {
	statusMu.Lock()
	status.CurrentState = state
	status.Win32ExitCode = 0
	status.ServiceSpecificExitCode = 0
	h := statusHandle
	st := status
	statusMu.Unlock()
	if h != 0 {
		procSetServiceStatus.Call(h, uintptr(unsafe.Pointer(&st)))
	}
}

func serviceCtrlHandler(ctrl uintptr) uintptr {
	switch uint32(ctrl) {
	case svcControlStop:
		setStatus(svcStateStopPending)
		stopOnce.Do(func() { close(serviceStop) })
	case svcControlShutdown:
		stopOnce.Do(func() { close(serviceStop) })
	}
	return 0
}

func serviceMain(_ uintptr, _ uintptr) uintptr {
	name, err := syscall.UTF16PtrFromString(serviceName)
	if err != nil {
		return 1
	}
	h, _, _ := procRegisterServiceCtrlHandler.Call(
		uintptr(unsafe.Pointer(name)),
		syscall.NewCallback(serviceCtrlHandler),
	)
	statusMu.Lock()
	statusHandle = h
	status = serviceStatus{
		ServiceType:      svcWin32OwnProcess,
		CurrentState:     svcStateStartPending,
		ControlsAccepted: svcAcceptStop | svcAcceptShutdown,
	}
	statusMu.Unlock()
	setStatus(svcStateRunning)

	app := serviceApp
	if app == nil {
		log.Println("service: application not initialised")
		setStatus(svcStateStopped)
		return 1
	}
	if err := app.Run(serviceStop); err != nil {
		log.Printf("service stopped with error: %v", err)
	}
	setStatus(svcStateStopped)
	return 0
}

// serviceApp is set by main before entering service dispatch.
var serviceApp *App

// runAsService hands control to the Windows service dispatcher. It blocks until
// the service is stopped.
func runAsService(app *App) error {
	serviceApp = app
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	serviceStop = make(chan struct{})
	name, err := syscall.UTF16PtrFromString(serviceName)
	if err != nil {
		return err
	}
	table := []serviceTableEntry{
		{ServiceName: name, ServiceProc: syscall.NewCallback(serviceMain)},
		{ServiceName: nil, ServiceProc: 0},
	}
	r, _, callErr := procStartServiceCtrlDispatcher.Call(uintptr(unsafe.Pointer(&table[0])))
	if r == 0 {
		return fmt.Errorf("StartServiceCtrlDispatcher failed: %v", callErr)
	}
	return nil
}

// handleServiceCommand returns true when the command was handled here.
func handleServiceCommand(cmd string) bool {
	switch strings.ToLower(strings.TrimSpace(cmd)) {
	case "install":
		if err := installService(); err != nil {
			fatal(err)
		}
		fmt.Println("Service installed and set to start automatically.")
	case "uninstall":
		if err := uninstallService(); err != nil {
			fatal(err)
		}
		fmt.Println("Service stopped and removed.")
	case "start":
		if err := sc("start", serviceName); err != nil {
			fatal(err)
		}
	case "stop":
		if err := sc("stop", serviceName); err != nil {
			fatal(err)
		}
	case "restart":
		_ = sc("stop", serviceName)
		if err := sc("start", serviceName); err != nil {
			fatal(err)
		}
	case "status":
		_ = sc("query", serviceName)
	case "run":
		return false // fall through to foreground mode
	default:
		fmt.Fprintf(os.Stderr, "unknown -service value %q (want: install|uninstall|start|stop|restart|status|run)\n", cmd)
		os.Exit(2)
	}
	return true
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func sc(args ...string) error {
	out, err := exec.Command("sc.exe", args...).CombinedOutput()
	msg := strings.TrimSpace(string(out))
	if err != nil {
		if msg == "" {
			msg = err.Error()
		}
		if strings.Contains(msg, "Access is denied") || strings.Contains(msg, "FAILED 5") ||
			strings.Contains(msg, "OpenSCManager") {
			return fmt.Errorf("%s\n\nThis needs an elevated (Administrator) terminal.", msg)
		}
		return fmt.Errorf("%s", msg)
	}
	if msg != "" {
		fmt.Println(msg)
	}
	return nil
}

func installService() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cfg := defaultConfigPath()
	binPath := fmt.Sprintf("\"%s\" -service-run -config \"%s\"", exe, cfg)
	if err := sc("create", serviceName,
		"binPath=", binPath,
		"start=", "auto",
		"DisplayName=", serviceDisplayName,
	); err != nil {
		return err
	}
	_ = sc("description", serviceName, "Local round-robin proxy that rotates API keys.")
	// Restart on crash: 3 attempts, then give up for a day.
	_ = sc("failure", serviceName, "reset=", "86400",
		"actions=", "restart/5000/restart/15000/restart/60000")
	return sc("start", serviceName)
}

func uninstallService() error {
	_ = sc("stop", serviceName)
	return sc("delete", serviceName)
}
