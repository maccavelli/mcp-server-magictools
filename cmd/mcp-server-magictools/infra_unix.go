//go:build !windows

// Package main provides functionality for the main subsystem.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/maccavelli/mcp-server-magictools/internal/client"

	"github.com/shirou/gopsutil/v4/process"
)

// makeSignalContext returns a context cancelled on SIGINT, SIGTERM, or SIGHUP.
// It is used in INTERACTIVE (stdio) mode only; there SIGHUP is a terminal hangup
// and shuts the process down. In SERVICE mode the serve context is a plain cancel
// context and setupGracefulShutdown owns signals, where SIGHUP triggers a live
// config reload instead (see reloadSignals / performGracefulShutdown).
func makeSignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
}

// reloadSignals returns the signals that trigger a live config reload in service
// mode. On Unix this is SIGHUP.
func reloadSignals() []os.Signal { return []os.Signal{syscall.SIGHUP} }

// isReloadSignal reports whether sig is a config-reload signal (SIGHUP on Unix).
func isReloadSignal(sig os.Signal) bool { return sig == syscall.SIGHUP }

// setupZombieReaper collects zombie child processes via SIGCHLD.
func setupZombieReaper(ctx context.Context) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGCHLD)
	defer signal.Stop(sigs)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sigs:
			for {
				var wstatus syscall.WaitStatus
				pid, err := syscall.Wait4(-1, &wstatus, syscall.WNOHANG, nil)
				if err != nil || pid <= 0 {
					break
				}
				slog.Debug("reaper: child process collected", "pid", pid, "exit_status", wstatus.ExitStatus())
			}
		}
	}
}

// enforceResourceLimits ensures the file descriptor limit is at least 4096.
func enforceResourceLimits() {
	var rLimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
		slog.Warn("resource: failed to get ulimit", "error", err)
		return
	}

	const minLimit = 4096
	if rLimit.Cur < minLimit {
		slog.Info("resource: increasing open files limit", "old", rLimit.Cur, "new", minLimit)
		rLimit.Cur = minLimit
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit); err != nil {
			slog.Error("resource: failed to set ulimit", "error", err)
		}
	}
}

// InitialProcessSweep kills orphaned sub-server processes from previous runs.
func InitialProcessSweep() {
	pids, err := process.Pids()
	if err != nil {
		return
	}

	myPid := safeInt32FromInt(os.Getpid())
	myPpid := safeInt32FromInt(os.Getppid())

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
			fmt.Fprintf(os.Stderr, "InitialSweep: reaping orphan %d\n", pid)
			if err := syscall.Kill(-int(pid), syscall.SIGKILL); err != nil {
				slog.Debug("failed to kill process group", "pid", pid, "error", err)
			}
			if err := syscall.Kill(int(pid), syscall.SIGKILL); err != nil {
				slog.Debug("failed to kill process", "pid", pid, "error", err)
			}
			killed++
		}
	}
	if killed > 0 {
		fmt.Fprintf(os.Stderr, "InitialSweep: total orphaned processes reaped: %d\n", killed)
	}
}
