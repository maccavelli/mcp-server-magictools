package client

import (
	"slices"
	"strings"
)

// The selection rules of 0010-MADR-make-tests-unable-to-signal-real-processes.md ("Design as approved", D4), as pure
// functions over one snapshot. A process is signalled only if its owner stamp names this instance's lineage; a
// process with the old constant alone, no stamp, an unreadable environment, or a stamp that cannot be attributed is
// never signalled (the owner's third ruling, P3 Q3; R4).

// reapView is what S1 judges against: one snapshot, this process, its lineage, the pids it manages, and the names of
// its servers that are running.
type reapView struct {
	procs   map[int32]ProcInfo
	parents map[int32]int32
	self    SelfInfo
	lineage string
	managed map[int32]bool
	running map[string]bool
}

// newReapView builds the view. recorded maps each server name the registry holds to the pids it recorded for it (the
// pid recorded when the process was started, and the managed process's pid once its handshake completed).
func newReapView(procs []ProcInfo, self SelfInfo, lineage string, managed map[int32]bool, recorded map[string][]int32) reapView {
	byPID := make(map[int32]ProcInfo, len(procs))
	for _, p := range procs {
		byPID[p.PID] = p
	}
	return reapView{procs: byPID, parents: parentsOf(procs), self: self, lineage: lineage, managed: managed,
		running: runningServers(byPID, self, recorded)}
}

// runningServers is the owner's ruling's "running" (P8 S3): a server is running when the registry holds its name and a
// pid it recorded for it is, in this snapshot, a direct child of this process. That covers a server that is starting
// (its pid is recorded as soon as it is started, before the handshake), healthy, syncing, or draining after a
// disconnect (still a child until it exits). It excludes a server that has exited or crashed (its pid is gone, or now
// belongs to a process that is not this one's child), and one the registry no longer holds.
//
// A recorded pid whose row has an unreadable parent (PPID -1) also counts as running: the owner's ruling on F5, as
// relayed, "Yes: an unreadable parent for a recorded pid counts as running; spare the helper (Recommended)". It errs
// toward leaving a process alone. A recorded pid with no row at all is not running: in one snapshot it cannot be told
// from a server that exited.
func runningServers(procs map[int32]ProcInfo, self SelfInfo, recorded map[string][]int32) map[string]bool {
	running := make(map[string]bool, len(recorded))
	for name, pids := range recorded {
		if name == "" {
			continue
		}
		for _, pid := range pids {
			if row, ok := procs[pid]; ok && pid > 1 && (row.PPID == self.PID || row.PPID < 0) {
				running[name] = true
				break
			}
		}
	}
	return running
}

// serverNameOf returns the one value of the server-name stamp in env, or "" when it is absent, empty or ambiguous.
func serverNameOf(env []string) string {
	prefix := EnvServerName + "="
	name, seen := "", false
	for _, e := range env {
		v, ok := strings.CutPrefix(e, prefix)
		if !ok {
			continue
		}
		if seen && v != name {
			return ""
		}
		name, seen = v, true
	}
	return name
}

// reapPrefilter is the part of reapable that needs no environment: pid 1 or less, this process, its parent, and any
// descendant of this process or of a managed child are never reaped. The walk no longer stops at this process's
// parent (F2): under a service manager that parent adopts every orphan.
func reapPrefilter(r ProcInfo, v reapView) (bool, string) {
	switch {
	case r.PID <= 1:
		return false, "pid 1 or less"
	case r.PID == v.self.PID:
		return false, "this process"
	case r.PID == v.self.PPID:
		return false, "this process's parent"
	case isLegitimateDescendant(r.PID, v.parents, v.managed, v.self.PID, v.self.PID):
		return false, "a descendant of this process or of a managed child"
	}
	return true, ""
}

// reapable is S1's rule (D4). It reports whether r may be reaped, and why not.
func reapable(r ProcInfo, env []string, envErr error, v reapView) (bool, string) {
	if ok, why := reapPrefilter(r, v); !ok {
		return false, why
	}
	if envErr != nil {
		return false, "environment unreadable"
	}
	o, ok := ParseOwner(env)
	if !ok {
		return false, "no attributable owner stamp"
	}
	if o.Lineage != v.lineage {
		return false, "another instance's lineage"
	}
	if o.PID == v.self.PID && o.Start == v.self.Start {
		if !detached(r, v.parents) {
			return false, "this process's child, not provably detached"
		}
		// The owner's ruling (P8 S3), as relayed: "Spare it while the server named in its stamp is running
		// (Recommended)". A helper a live sub-server deliberately detached is left alone while that server runs here.
		if name := serverNameOf(env); name != "" && v.running[name] {
			return false, "a detached helper of a running server"
		}
		return true, ""
	}
	if ownerDead(o, v.procs) {
		return true, ""
	}
	return false, "its owner is alive"
}

// ownerDead: the owner's pid is absent from the snapshot, or present with another readable start token (the pid was
// reused). An unreadable token counts as alive.
func ownerDead(o Owner, procs map[int32]ProcInfo) bool {
	row, ok := procs[o.PID]
	if !ok {
		return true
	}
	if row.Start == 0 {
		return false
	}
	return row.Start != o.Start
}

// detached reports whether the walk from r reaches pid 1 or less through known links only, without a cycle, within
// the depth cap: r provably no longer descends from anything that could still own it.
func detached(r ProcInfo, parents map[int32]int32) bool {
	visited := make(map[int32]bool, 8)
	cur := r.PID
	for depth := 0; depth < maxParentWalkDepth; depth++ {
		if visited[cur] {
			return false
		}
		visited[cur] = true
		ppid, ok := parents[cur]
		if !ok || ppid == cur {
			return false
		}
		if ppid <= 1 {
			return true
		}
		cur = ppid
	}
	return false
}

// harvestable is S2's rule (D4): before spawning name, a process is harvested only if it carries this lineage's
// owner stamp and the name stamp of the same server. Owner liveness is not required: within one lineage, one process
// per server name is the enforcer's purpose. The look-alike rules (same executable, the mcp-server-<name> suffix, an
// interpreter's command line) are removed (the third ruling, P3 Q4).
func harvestable(r ProcInfo, env []string, envErr error, self SelfInfo, lineage, name string, exclude map[int]bool) (bool, string) {
	switch {
	case r.PID <= 1:
		return false, "pid 1 or less"
	case r.PID == self.PID:
		return false, "this process"
	case r.PID == self.PPID:
		return false, "this process's parent"
	case exclude[int(r.PID)]:
		return false, "excluded by the caller"
	case envErr != nil:
		return false, "environment unreadable"
	}
	o, ok := ParseOwner(env)
	if !ok {
		return false, "no attributable owner stamp"
	}
	if o.Lineage != lineage {
		return false, "another instance's lineage"
	}
	if !slices.Contains(env, EnvServerName+"="+name) {
		return false, "another server's name"
	}
	return true, ""
}
