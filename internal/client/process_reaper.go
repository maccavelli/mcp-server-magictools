// Package client provides functionality for the client subsystem.
package client

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// maxParentWalkDepth bounds the ancestry walk. Real chains are a handful of
// levels deep; anything beyond this is a malformed process table, not a
// legitimate descendant.
const maxParentWalkDepth = 64

// parentsOf builds pid -> ppid from ONE snapshot of the process table.
//
// Taking the snapshot once is the point (MADR 0003 F1): the walk below is a
// pure function over it. The snapshot comes from the registry's process table
// (0010-MADR-make-tests-unable-to-signal-real-processes.md, D1); a process
// whose parent could not be read is left out, as before.
func parentsOf(procs []ProcInfo) map[int32]int32 {
	parents := make(map[int32]int32, len(procs))
	for _, p := range procs {
		if p.PPID >= 0 {
			parents[p.PID] = p.PPID
		}
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

// PruneOrphans reaps orphans of this instance (0010-MADR, D4): processes whose owner stamp names this store's
// lineage and either a dead owner, or this process when they are provably detached from it. A process with the old
// constant alone, no stamp, another lineage's stamp, or an unreadable environment is never touched.
//
// It does nothing until a process table is attached (AttachProcessTable).
func (m *WarmRegistry) PruneOrphans() {
	b := m.procs.Load()
	if b == nil {
		return
	}
	// One enumeration per prune. Classification is therefore against a
	// point-in-time view: a process that reparents mid-prune is judged from
	// this snapshot and picked up by the next sweep.
	procs, err := b.table.Snapshot()
	if err != nil {
		return
	}
	self, err := b.table.Self()
	if err != nil {
		return
	}

	m.mu.RLock()
	activePIDs := make(map[int32]bool)
	recorded := make(map[string][]int32, len(m.Servers))
	for name, s := range m.Servers {
		if s.LastKnownPID > 0 {
			recorded[name] = append(recorded[name], safeInt32FromInt(s.LastKnownPID))
		}
		if s.Process != nil && s.Process.Process != nil {
			pid := safeInt32FromInt(s.Process.Process.Pid)
			activePIDs[pid] = true
			recorded[name] = append(recorded[name], pid)
		}
	}
	m.mu.RUnlock()

	view := newReapView(procs, self, b.owner.Lineage, activePIDs, recorded)
	killed := 0
	for _, p := range procs {
		if ok, _ := reapPrefilter(p, view); !ok {
			continue
		}
		env, envErr := b.table.Environ(p)
		if ok, _ := reapable(p, env, envErr, view); !ok {
			continue
		}
		exeName, exeErr := b.table.Exe(p)
		if exeErr != nil {
			exeName = ""
		}
		slog.Warn("WarmRegistry: reaping orphaned child", "pid", p.PID, "exe", exeName)
		m.reapAsync(b.table, p)
		killed++
	}
	if killed > 0 {
		slog.Info("WarmRegistry: prune complete", "terminated", killed)
	}
}

// reapAsync escalates one orphan in the background, as killPIDGroup's
// escalation goroutine did; reapWG lets tests wait for it.
func (m *WarmRegistry) reapAsync(t ProcessTable, p ProcInfo) {
	m.reapWG.Add(1)
	go func() {
		defer m.reapWG.Done()
		if _, err := Escalate(t, m.clk(), p, escalateGrace, escalatePoll); err != nil {
			slog.Debug("lifecycle: orphan kill failed", "pid", p.PID, "error", err)
		}
	}()
}

// enforceSingleInstance ensures only one process of this instance runs for a given server name, by harvesting any
// process stamped with this lineage and that name (except those in excludePIDs) before a new one is spawned
// (0010-MADR, D4). command and args no longer select anything: the look-alike rules are removed (P3 Q4).
//
// It does nothing until a process table is attached (AttachProcessTable).
func (m *WarmRegistry) enforceSingleInstance(ctx context.Context, name, _ string, _ []string, excludePIDs ...int) {
	b := m.procs.Load()
	if b == nil {
		return
	}
	// One enumeration serves the pid file, the scan and the harvest.
	procs, err := b.table.Snapshot()
	if err != nil {
		return
	}
	self, err := b.table.Self()
	if err != nil {
		return
	}
	byPID := make(map[int32]ProcInfo, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}

	excludeSet := make(map[int]bool, len(excludePIDs))
	for _, pid := range excludePIDs {
		excludeSet[pid] = true
	}

	toCheck := make(map[int]bool)
	m.lookupInternalRegistry(ctx, name, toCheck)
	m.lookupSystemProcesses(ctx, b.table, procs, self, b.owner.Lineage, name, excludeSet, toCheck)

	// Remove excluded PIDs (our own recently-spawned children) from the kill list
	for pid := range excludeSet {
		delete(toCheck, pid)
	}

	// Every candidate, the pid file's included, is harvested only as the process the snapshot saw and only if it
	// passes the rule: a pid file left by a crash cannot name a process that merely reused the pid.
	harvest := make([]ProcInfo, 0, len(toCheck))
	for pid := range toCheck {
		p, ok := byPID[safeInt32FromInt(pid)]
		if !ok {
			continue
		}
		env, envErr := b.table.Environ(p)
		ok, why := harvestable(p, env, envErr, self, b.owner.Lineage, name, excludeSet)
		if !ok {
			// A candidate the rule refuses (most often a pid file naming a pid that now belongs to another process).
			slog.Debug("lifecycle: candidate not harvested", "server", name, "pid", p.PID, "reason", why)
			continue
		}
		harvest = append(harvest, p)
	}

	if len(harvest) > 0 {
		slog.Info("lifecycle: enforcing single instance, harvesting existing processes", "server", name, "count", len(harvest))
	}

	for _, p := range harvest {
		m.harvestProcess(ctx, b.table, name, p)
	}

	// Settling delay after harvesting to allow OS reaping before new spawn
	if len(harvest) > 0 {
		m.clk().Sleep(250 * time.Millisecond)
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

// lookupSystemProcesses adds to toCheck every process of the snapshot that harvestable accepts for name: this
// lineage's owner stamp and the server's name stamp.
func (m *WarmRegistry) lookupSystemProcesses(_ context.Context, t ProcessTable, procs []ProcInfo, self SelfInfo, lineage, name string, exclude map[int]bool, toCheck map[int]bool) {
	for _, p := range procs {
		if p.PID <= 1 || p.PID == self.PID || p.PID == self.PPID || exclude[int(p.PID)] {
			continue
		}
		env, envErr := t.Environ(p)
		if ok, _ := harvestable(p, env, envErr, self, lineage, name, exclude); ok {
			toCheck[int(p.PID)] = true
		}
	}
}

// harvestProcess gracefully terminates a process found by the enforcer, through
// the process table: TERM, a 2 s wait on the registry's clock, then KILL. It
// replaces the platform files reaper_harvest_unix.go and reaper_harvest_windows.go.
func (m *WarmRegistry) harvestProcess(_ context.Context, t ProcessTable, name string, p ProcInfo) {
	// Verify the process still exists and is the one the snapshot saw.
	if !t.Alive(p) {
		return
	}
	slog.Warn("lifecycle: harvesting existing process instance", "server", name, "pid", p.PID)
	killed, err := Escalate(t, m.clk(), p, escalateGrace, escalatePoll)
	if err != nil {
		slog.Debug("lifecycle: harvest signal failed", "server", name, "pid", p.PID, "error", err)
	}
	if killed {
		slog.Warn("lifecycle: SIGTERM ignored, escalated to SIGKILL", "server", name, "pid", p.PID)
	}
}

// reportSubServerFailure logs a sub-server crash event.
func (m *WarmRegistry) reportSubServerFailure(name string, exitCode int) {
	slog.Error("lifecycle: sub-server crashed", "server", name, "exit_code", exitCode)
}
