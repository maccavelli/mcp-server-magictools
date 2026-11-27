package db

// cleanupDecision is what cleanupStaleProcess does with a parsed pid file.
// It does not signal. The platform files do.
type cleanupDecision int

const (
	// cleanupLeave leaves the pid file and does not signal.
	cleanupLeave cleanupDecision = iota
	// cleanupRemove removes the pid file and does not signal.
	cleanupRemove
	// cleanupSignal is the stale-instance kill. The caller has already shown
	// the pid is alive, is not the caller, does not hold the lock, and its
	// executable matches.
	cleanupSignal
)

// decideCleanup is 0016's rule. lockHeld means some other process holds
// LOCK.magic; this process cannot name the holder, so a held lock is never
// a signal. exeErr set means the executable could not be read (S8).
func decideCleanup(pid, selfPID int, lockHeld, alive bool, exe, selfExe string, exeErr error) cleanupDecision {
	if pid == selfPID || lockHeld {
		return cleanupLeave
	}
	if !alive {
		return cleanupRemove
	}
	if exeErr != nil || exe == "" || selfExe == "" || exe != selfExe {
		if exeErr != nil || exe == "" || selfExe == "" {
			return cleanupLeave
		}
		return cleanupRemove
	}
	return cleanupSignal
}
