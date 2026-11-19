package client

import "time"

// Escalation defaults: the grace period and poll interval the reaper and the enforcer have always used.
const (
	escalateGrace = 2 * time.Second
	escalatePoll  = 100 * time.Millisecond
)

// Escalate ends a process the caller did not start, through t only: TERM; then, every poll until grace has passed,
// an identity-checked liveness test; then KILL if p is still the same live process. It returns whether KILL was sent.
// A refused or failed TERM ends the escalation: the process is gone, has been replaced, or cannot be signalled.
func Escalate(t ProcessTable, c Clock, p ProcInfo, grace, poll time.Duration) (killed bool, err error) {
	if err := t.Signal(p, SigTerm); err != nil {
		return false, err
	}
	deadline := c.Now().Add(grace)
	for c.Now().Before(deadline) {
		if !t.Alive(p) {
			return false, nil
		}
		c.Sleep(poll)
	}
	if !t.Alive(p) {
		return false, nil
	}
	if err := t.Signal(p, SigKill); err != nil {
		return false, err
	}
	return true, nil
}
