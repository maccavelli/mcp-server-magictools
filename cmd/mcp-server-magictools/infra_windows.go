//go:build windows

// Package main provides functionality for the main subsystem.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/maccavelli/mcp-server-magictools/internal/client"

	"github.com/shirou/gopsutil/v4/process"
)

// setupZombieReaper is a no-op on Windows since process groups inherently track lifecycles avoiding zombies efficiently.
func setupZombieReaper(ctx context.Context) {}

// enforceResourceLimits is a no-op on Windows since file descriptor scaling is inherently managed by the kernel limits implicitly.
func enforceResourceLimits() {}

// makeSignalContext returns a context cancelled on SIGINT or SIGTERM.
// SIGHUP is not defined on Windows and is intentionally omitted here.
func makeSignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

// reloadSignals returns nil on Windows — SIGHUP is not delivered, so service-mode
// config reload via signal is unavailable (use the file watcher instead).
func reloadSignals() []os.Signal { return nil }

// isReloadSignal always reports false on Windows.
func isReloadSignal(sig os.Signal) bool { return false }

// InitialProcessSweep targets taskkill against matching processes securely over Win32 execution targets natively avoiding POSIX group kills perfectly.
func InitialProcessSweep() {
	pids, err := process.Pids()
	if err != nil {
		return
	}

	myPid := int32(os.Getpid())
	myPpid := int32(os.Getppid())

	killed := 0
	for _, pid := range pids {
		if pid == myPid || pid == myPpid || pid <= 1 {
			continue
		}

		p, err := process.NewProcess(pid)
		if err != nil {
			continue
		}

		env, err := p.Environ()
		if err != nil {
			continue
		}

		isOrphan := false
		for _, e := range env {
			if e == client.EnvManaged+"="+client.EnvManagedValue || strings.HasPrefix(e, "MAGIC_TOOLS_PEER_ID=") {
				isOrphan = true
				break
			}
		}

		if isOrphan {
			// 🛡️ MULTI-INSTANCE GUARD: Skip if parented by another live orchestrator
			ppid, ppidErr := p.Ppid()
			if ppidErr == nil && ppid > 1 && ppid != myPid && ppid != myPpid {
				if parent, pErr := process.NewProcess(ppid); pErr == nil {
					if pName, nErr := parent.Name(); nErr == nil && strings.Contains(pName, "magictools") {
						continue
					}
				}
			}
			fmt.Fprintf(os.Stderr, "InitialSweep: reaping orphaned Windows task %d\n", pid)
			killCmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(int(pid)))
			if err := killCmd.Run(); err != nil {
				slog.Debug("failed to taskkill orphaned process", "pid", pid, "error", err)
			}
			killed++
		}
	}
	if killed > 0 {
		fmt.Fprintf(os.Stderr, "InitialSweep: total orphaned processes reaped: %d\n", killed)
	}
}
