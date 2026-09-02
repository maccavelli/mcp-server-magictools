// Package client provides functionality for the client subsystem.
package client

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/maccavelli/mcp-server-magictools/internal/util"
)

// maxParentWalkDepth bounds the ancestry walk. Real chains are a handful of
// levels deep; anything beyond this is a malformed process table, not a
// legitimate descendant.
const maxParentWalkDepth = 64

// parentMap returns the whole process table as pid -> ppid in ONE enumeration.
// It is a package variable so tests can supply a table without touching the OS.
var parentMap = osParentMap

// osParentMap enumerates the live process table exactly once.
//
// Taking the snapshot once is the point. Resolving a parent per step means one
// full process-table snapshot per step on Windows, because gopsutil's Ppid goes
// through CreateToolhelp32Snapshot and scans it linearly, and its parent cache
// lives on the Process value rather than the package — so a freshly built
// Process never hits it (MADR 0003 F1).
func osParentMap() map[int32]int32 {
	procs, err := process.Processes()
	if err != nil {
		return nil
	}
	parents := make(map[int32]int32, len(procs))
	for _, p := range procs {
		ppid, err := p.Ppid()
		if err != nil {
			continue
		}
		parents[p.Pid] = ppid
	}
	return parents
}

// isLegitimateDescendant reports whether pid's ancestry reaches this process,
// its parent, or a process this registry is actively managing.
//
// It is a pure function over the supplied parent table: it performs no
// syscalls, so it is exercisable on every platform, including the cycle case
// that no Linux or macOS process table can produce.
//
// The walk terminates on every input. Windows recycles PIDs aggressively, so a
// stale parent pointer can form a real A -> B -> A cycle; the previous
// implementation detected only a self-loop and span forever on anything longer
// (MADR 0003 F2).
func isLegitimateDescendant(pid int32, parents map[int32]int32, activePIDs map[int32]bool, myPid int32, myPpid int32) bool {
	visited := make(map[int32]bool, 8)
	currPid := pid
	for depth := 0; depth < maxParentWalkDepth; depth++ {
		if currPid == myPid || currPid == myPpid || activePIDs[currPid] {
			return true
		}
		if visited[currPid] {
			// A cycle in the parent chain. Not an ancestor of ours.
			return false
		}
		visited[currPid] = true

		ppid, ok := parents[currPid]
		if !ok || ppid == 0 || ppid == currPid {
			return false
		}
		currPid = ppid
	}
	// Depth cap reached: treat as not a descendant rather than keep walking.
	return false
}

// PruneOrphans scans running processes and kills any that were tagged by
// a previous orchestrator instance (MCP_ORCHESTRATOR_OWNED or MAGIC_TOOLS_PEER_ID)
// but are no longer actively managed by this instance.
func (m *WarmRegistry) PruneOrphans() {
	pids, err := process.Pids()
	if err != nil {
		return
	}

	// One enumeration per prune. Classification is therefore against a
	// point-in-time view: a process that reparents mid-prune is judged from
	// this snapshot and picked up by the next sweep.
	parents := parentMap()

	m.mu.RLock()
	activePIDs := make(map[int32]bool)
	for _, s := range m.Servers {
		if s.Process != nil && s.Process.Process != nil {
			activePIDs[safeInt32FromInt(s.Process.Process.Pid)] = true
		}
	}
	m.mu.RUnlock()

	killed := 0
	myPid := safeInt32FromInt(os.Getpid())
	myPpid := safeInt32FromInt(os.Getppid())

	for _, pid := range pids {
		if pid == myPid || pid == myPpid || pid <= 1 {
			continue
		}

		if isLegitimateDescendant(pid, parents, activePIDs, myPid, myPpid) {
			continue
		}

		p, err := process.NewProcess(pid)
		if err != nil {
			continue
		}

		env, err := util.GetProcessEnviron(int(pid))
		if err != nil {
			continue
		}

		isOrphan := slices.Contains(env, EnvManaged+"="+EnvManagedValue)

		if isOrphan {
			exeName, exeErr := p.Exe()
			if exeErr != nil {
				exeName = ""
			}
			slog.Warn("WarmRegistry: reaping orphaned child", "pid", pid, "exe", exeName)
			if err := killPIDGroup(int(pid)); err != nil {
				slog.Debug("lifecycle: orphan kill failed", "pid", pid, "error", err)
			}
			killed++
		}
	}
	if killed > 0 {
		slog.Info("WarmRegistry: prune complete", "terminated", killed)
	}
}

// enforceSingleInstance ensures only one process exists for a given server name
// by killing any existing instances (except those in excludePIDs).
func (m *WarmRegistry) enforceSingleInstance(ctx context.Context, name, command string, args []string, excludePIDs ...int) {
	excludeSet := make(map[int]bool, len(excludePIDs))
	for _, pid := range excludePIDs {
		excludeSet[pid] = true
	}

	toCheck := make(map[int]bool)
	m.lookupInternalRegistry(ctx, name, toCheck)
	// ALWAYS perform system lookup to ensure no "ghost" processes exist with the same name/tag
	m.lookupSystemProcesses(ctx, name, command, args, toCheck)

	// Remove excluded PIDs (our own recently-spawned children) from the kill list
	for pid := range excludeSet {
		delete(toCheck, pid)
	}

	if len(toCheck) > 0 {
		slog.Info("lifecycle: enforcing single instance, harvesting existing processes", "server", name, "count", len(toCheck))
	}

	for pid := range toCheck {
		m.harvestProcess(ctx, name, pid)
	}

	// Settling delay after harvesting to allow OS reaping before new spawn
	if len(toCheck) > 0 {
		time.Sleep(250 * time.Millisecond)
	}
}

// lookupInternalRegistry checks for a PID file indicating a previously-spawned process.
func (m *WarmRegistry) lookupInternalRegistry(_ context.Context, name string, toCheck map[int]bool) {
	if m.PIDDir == "" {
		return
	}
	pidFile := filepath.Join(m.PIDDir, name+".pid")
	data, err := os.ReadFile(filepath.Clean(pidFile)) //nolint:gosec // pid path under controlled PID directory
	if err != nil {
		return
	}
	if pid, err := strconv.Atoi(string(data)); err == nil {
		toCheck[pid] = true
	}
}

// lookupSystemProcesses scans all system processes for orphans matching
// the given server name via environment variables, binary paths, or command-line patterns.
func (m *WarmRegistry) lookupSystemProcesses(_ context.Context, name, command string, args []string, toCheck map[int]bool) {
	pids, err := process.Pids()
	if err != nil {
		return
	}

	myPid := safeInt32FromInt(os.Getpid())
	myPpid := safeInt32FromInt(os.Getppid())
	targetStamp := fmt.Sprintf("%s=%s", EnvServerName, name)

	for _, pid := range pids {
		if pid == myPid || pid == myPpid || pid <= 1 {
			continue
		}

		p, err := process.NewProcess(pid)
		if err != nil {
			continue
		}

		env, err := util.GetProcessEnviron(int(pid))
		if err != nil {
			continue
		}

		found := false
		if slices.Contains(env, targetStamp) {
			toCheck[int(pid)] = true
			found = true
		}

		if found {
			continue
		}

		// Fallback: Command-Line and Path Matching
		cmdline, cmdErr := p.Cmdline()
		if cmdErr != nil {
			cmdline = ""
		}
		exe, exeErr := p.Exe()
		if exeErr != nil {
			exe = ""
		}

		isInterpreter := false
		if exe != "" {
			base := filepath.Base(exe)
			if base == "node" || base == "python" || base == "python3" || base == "npm" || base == "npx" || base == "bash" || base == "sh" {
				isInterpreter = true
			}
		}

		// Match 1: Exact executable path match (except interpreters)
		if !isInterpreter && exe != "" && exe == command {
			slog.Warn("lifecycle: identified orphan via exact binary path matching", "server", name, "pid", pid, "exe", exe)
			toCheck[int(pid)] = true
			continue
		}

		// Match 2: Pattern-based suffixes
		if exe != "" && strings.HasSuffix(exe, "mcp-server-"+name) {
			slog.Warn("lifecycle: identified orphan via binary suffix matching", "server", name, "pid", pid, "exe", exe)
			toCheck[int(pid)] = true
			continue
		}

		// Match 3: Script-based matching (Interpreters)
		if isInterpreter && exe == command {
			if len(args) > 0 && strings.Contains(cmdline, args[0]) {
				slog.Warn("lifecycle: identified orphan via cmdline matching", "server", name, "pid", pid, "arg", args[0])
				toCheck[int(pid)] = true
			}
		}
	}
}

// reportSubServerFailure logs a sub-server crash event.
func (m *WarmRegistry) reportSubServerFailure(name string, exitCode int) {
	slog.Error("lifecycle: sub-server crashed", "server", name, "exit_code", exitCode)
}
