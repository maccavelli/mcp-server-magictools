package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/maccavelli/mcplib/selfupdate"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

// healthPoll and healthTimeout bound WaitHealthy. A service manager accepting
// `start` is not evidence the unit came up, so health is polled rather than
// assumed, and a timeout is fatal (MADR 0005).
const (
	healthPoll    = 250 * time.Millisecond
	healthTimeout = 30 * time.Second
)

// serviceActionStop and serviceActionStart name the two control verbs.
const (
	serviceActionStop  = "stop"
	serviceActionStart = "start"
)

// updateLifecycle adapts this repository's service control to the shared
// selfupdate.Lifecycle seam, bound to the executable the update will replace.
//
// Binding to the target matters: a service definition that points at some
// OTHER binary is treated as absent, so an update never stops, reconciles or
// restarts a service it does not own.
type updateLifecycle struct {
	target string

	// Seams. Production leaves them nil and the platform implementations run.
	installedFn func(ctx context.Context, target string) (bool, error)
	runningFn   func(ctx context.Context) (bool, error)
	stopFn      func(ctx context.Context) error
	startFn     func(ctx context.Context) error

	HealthTimeout time.Duration
	Poll          time.Duration
}

var _ selfupdate.Lifecycle = (*updateLifecycle)(nil)

// Installed reports whether a definition exists AND targets this executable.
func (l *updateLifecycle) Installed(ctx context.Context, _ string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if l.installedFn != nil {
		return l.installedFn(ctx, l.target)
	}
	return serviceDefinitionTargets(l.target)
}

// Running reports whether the recorded service process is alive.
func (l *updateLifecycle) Running(ctx context.Context, _ string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if l.runningFn != nil {
		return l.runningFn(ctx)
	}
	return serviceProcessAlive()
}

// Stop halts the managed service.
func (l *updateLifecycle) Stop(ctx context.Context, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.stopFn != nil {
		return l.stopFn(ctx)
	}
	return serviceControl(ctx, serviceActionStop)
}

// Start launches the managed service.
func (l *updateLifecycle) Start(ctx context.Context, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.startFn != nil {
		return l.startFn(ctx)
	}
	return serviceControl(ctx, serviceActionStart)
}

// WaitHealthy polls the recorded service PID until it is alive, the deadline
// passes, or the caller cancels. A timeout is fatal and enters the shared
// rollback path.
func (l *updateLifecycle) WaitHealthy(ctx context.Context, product string) error {
	timeout := l.HealthTimeout
	if timeout <= 0 {
		timeout = healthTimeout
	}
	poll := l.Poll
	if poll <= 0 {
		poll = healthPoll
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	var lastErr error
	for {
		alive, err := l.Running(ctx, product)
		switch {
		case err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled):
			lastErr = err
		case alive:
			return nil
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("%s did not become healthy within %s: %w", product, timeout, lastErr)
			}
			return fmt.Errorf("%s did not become healthy within %s", product, timeout)
		case <-ticker.C:
		}
	}
}

// serviceStatePath is where the running service records its PID and binary.
func serviceStatePath() string {
	return filepath.Join(config.DefaultConfigDir(), "service.state")
}

// readServiceState returns the recorded state, or false when there is none.
func readServiceState() (cliServiceState, bool) {
	data, err := os.ReadFile(serviceStatePath()) // #nosec G304 — fixed config path
	if err != nil {
		return cliServiceState{}, false
	}
	var st cliServiceState
	if json.Unmarshal(data, &st) != nil {
		return cliServiceState{}, false
	}
	return st, true
}

// serviceProcessAlive reports whether the recorded service PID is alive.
func serviceProcessAlive() (bool, error) {
	st, ok := readServiceState()
	if !ok || st.PID == 0 {
		return false, nil
	}
	return isProcessAlive(st.PID), nil
}

// serviceDefinitionTargets reports whether an OS service definition exists and
// refers to this exact executable. A definition for another binary is reported
// absent so the update leaves it alone.
func serviceDefinitionTargets(target string) (bool, error) {
	path, ok := serviceDefinitionPath()
	if !ok {
		return false, nil
	}
	data, err := os.ReadFile(path) // #nosec G304 — fixed per-platform definition path
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		resolved = target
	}
	body := string(data)
	return strings.Contains(body, target) || strings.Contains(body, resolved), nil
}

// serviceControl runs the platform service manager for stop/start.
func serviceControl(ctx context.Context, action string) error {
	switch runtime.GOOS {
	case goOSLinux:
		return runServiceTool(ctx, "systemctl", systemctlUserFlag, action, cmdName+".service")
	case goOSDarwin:
		path, ok := serviceDefinitionPath()
		if !ok {
			return fmt.Errorf("no launchd definition for %s", cmdName)
		}
		verb := "bootstrap"
		if action == serviceActionStop {
			verb = "bootout"
		}
		return runServiceTool(ctx, "launchctl", verb, launchdGUITarget(), path)
	case goOSWindows:
		if action == serviceActionStop {
			return stopWindowsService()
		}
		return startWindowsService()
	default:
		return fmt.Errorf("service control is not supported on %s", runtime.GOOS)
	}
}

// runServiceTool runs a service-manager command under the caller's context so
// an interrupted update stops waiting on it.
func runServiceTool(ctx context.Context, name string, args ...string) error {
	// #nosec G204 — name and args are constants plus a fixed definition path.
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail != "" {
			return fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, detail)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
