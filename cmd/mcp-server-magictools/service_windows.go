//go:build windows

// Package main: Windows Service Control Manager (SCM) integration.
//
// B2: replaces the legacy Task Scheduler `.cmd` wrapper with a real SCM service.
// This gives run-without-login, SCM-level crash recovery, and a graceful stop
// control event — none of which `schtasks /SC ONLOGON` provided. The cross-
// platform CLI in service.go dispatches the windows branch into the functions
// here; the non-windows build provides stubs (service_other.go).
package main

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	windowsServiceName        = "MagicToolsService"
	windowsServiceDisplayName = "MagicTools MCP Orchestrator"
	windowsServiceDescription = "Background MCP orchestrator service for magictools (serve mode)."
)

// runServe runs fn, dispatching through the Windows SCM when the process was
// started by the service manager. Run interactively it simply calls fn(parent).
// Under the SCM a Stop/Shutdown control request cancels the context handed to
// fn, which flows into the serve loop's graceful teardown (Start -> Stop), so
// sub-servers are drained and service.state is removed before exit.
func runServe(parent context.Context, fn func(context.Context) error) error {
	isSvc, err := svc.IsWindowsService()
	if err != nil || !isSvc {
		return fn(parent)
	}
	h := &windowsServiceHandler{parent: parent, fn: fn}
	if runErr := svc.Run(windowsServiceName, h); runErr != nil {
		return runErr
	}
	return h.runErr
}

type windowsServiceHandler struct {
	parent context.Context
	fn     func(context.Context) error
	runErr error
}

// Execute is the SCM service entrypoint. It runs the serve loop in a goroutine
// and translates Stop/Shutdown control requests into context cancellation.
func (h *windowsServiceHandler) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(h.parent)
	defer cancel()

	done := make(chan struct{})
	go func() {
		h.runErr = h.fn(ctx)
		close(done)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case <-done:
			// Serve loop exited on its own (fatal listener error or completion).
			changes <- svc.Status{State: svc.Stopped}
			return false, 0
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				// WaitHint matches the app's 30s shutdown deadline (+slack) so the
				// SCM does not consider the stop hung and SIGKILL us prematurely.
				changes <- svc.Status{State: svc.StopPending, WaitHint: 35000}
				cancel()
				select {
				case <-done:
				case <-time.After(35 * time.Second):
				}
				changes <- svc.Status{State: svc.Stopped}
				return false, 0
			default:
				// Ignore controls we do not accept.
			}
		}
	}
}

// installWindowsService registers an auto-start SCM service with crash-recovery
// actions and bakes the env vars into the service registry key so `serve` boots
// in service mode with the right endpoints. Requires Administrator.
func installWindowsService(binPath string, env []string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as Administrator?): %w", err)
	}
	defer m.Disconnect()

	if s, openErr := m.OpenService(windowsServiceName); openErr == nil {
		s.Close()
		if !forceServiceInstall {
			fmt.Printf("Service %q already exists.\nUse --force to overwrite for upgrades.\n", windowsServiceName)
			return nil
		}
		if err := removeWindowsService(m); err != nil {
			return fmt.Errorf("failed to remove existing service for --force reinstall: %w", err)
		}
	}

	s, err := m.CreateService(windowsServiceName, binPath, mgr.Config{
		DisplayName: windowsServiceDisplayName,
		Description: windowsServiceDescription,
		StartType:   mgr.StartAutomatic,
	}, "serve")
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	// Crash recovery, mirroring systemd Restart=always/RestartSec=5: restart on
	// the first two failures after 5s, then back off to 30s; reset the failure
	// counter daily.
	if err := s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, 86400); err != nil {
		fmt.Printf("  ⚠ Failed to set recovery actions: %v\n", err)
	}

	if len(env) > 0 {
		if err := setWindowsServiceEnv(env); err != nil {
			fmt.Printf("  ⚠ Failed to set service environment: %v\n", err)
		}
	}

	if err := s.Start(); err != nil {
		// HARD-3 parity: roll back the registration if it cannot start.
		_ = s.Delete()
		return fmt.Errorf("service created but failed to start (rolled back): %w", err)
	}

	fmt.Printf("✓ Windows service installed and started\n")
	fmt.Printf("  Service: %s (auto-start, restart-on-failure)\n", windowsServiceName)
	fmt.Printf("  Check:   sc query %s\n", windowsServiceName)
	return nil
}

// setWindowsServiceEnv writes env (KEY=VALUE strings) to the service's
// per-service Environment registry value (REG_MULTI_SZ), which the SCM applies
// to the service process at launch.
func setWindowsServiceEnv(env []string) error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SYSTEM\CurrentControlSet\Services\`+windowsServiceName, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringsValue("Environment", env)
}

// removeWindowsService stops (best-effort, bounded wait) and deletes the service.
func removeWindowsService(m *mgr.Mgr) error {
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return nil // already gone — idempotent
	}
	defer s.Close()

	_, _ = s.Control(svc.Stop)
	// Bound the wait so a wedged service cannot block uninstall indefinitely.
	for i := 0; i < 35; i++ {
		st, qErr := s.Query()
		if qErr != nil || st.State == svc.Stopped {
			break
		}
		time.Sleep(time.Second)
	}
	return s.Delete()
}

// uninstallWindowsService stops and deletes the SCM service. Absent service is
// treated as success (idempotent). Requires Administrator.
func uninstallWindowsService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as Administrator?): %w", err)
	}
	defer m.Disconnect()

	if s, openErr := m.OpenService(windowsServiceName); openErr != nil {
		return nil // not installed
	} else {
		s.Close()
	}
	return removeWindowsService(m)
}

func startWindowsService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return fmt.Errorf("open service (is it installed?): %w", err)
	}
	defer s.Close()
	return s.Start()
}

func stopWindowsService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return fmt.Errorf("open service (is it installed?): %w", err)
	}
	defer s.Close()
	_, err = s.Control(svc.Stop)
	return err
}

// windowsServiceInstalled reports whether the SCM service is registered.
func windowsServiceInstalled() (bool, error) {
	m, err := mgr.Connect()
	if err != nil {
		return false, err
	}
	defer m.Disconnect()
	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		return false, nil
	}
	s.Close()
	return true, nil
}
