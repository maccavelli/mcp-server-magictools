package db

import (
	"errors"
	"testing"
)

func TestDecideCleanup_0016(t *testing.T) {
	const (
		self = 10
		pid  = 20
		exe  = "/bin/mcp-server-magictools"
	)
	unread := errors.New("unreadable")
	cases := []struct {
		name    string
		pid     int
		lock    bool
		alive   bool
		exe     string
		selfExe string
		exeErr  error
		want    cleanupDecision
	}{
		{"caller", self, false, true, exe, exe, nil, cleanupLeave},
		{"lock holder", pid, true, true, exe, exe, nil, cleanupLeave},
		{"dead", pid, false, false, "", exe, nil, cleanupRemove},
		{"foreign", pid, false, true, "/bin/sleep", exe, nil, cleanupRemove},
		{"unreadable", pid, false, true, "", exe, unread, cleanupLeave},
		{"self exe unreadable", pid, false, true, exe, "", nil, cleanupLeave},
		{"match", pid, false, true, exe, exe, nil, cleanupSignal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := decideCleanup(c.pid, self, c.lock, c.alive, c.exe, c.selfExe, c.exeErr)
			if got != c.want {
				t.Fatalf("decideCleanup() = %d, want %d", got, c.want)
			}
		})
	}
}
