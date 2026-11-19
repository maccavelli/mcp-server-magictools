package client

import "testing"

// The owner's ruling on own-stamped, detached processes (0010, P8 S3), label as relayed: "Spare it while the server
// named in its stamp is running (Recommended)". A helper a live sub-server deliberately detaches is left alone while
// that server runs in this instance, and reaped once it is gone. "Running": the registry holds the server's name and
// the pid it recorded for it is, in the same snapshot, a direct child of this process.

func launchdSelfWithRow() SelfInfo { return SelfInfo{ProcInfo: ProcInfo{PID: 100, PPID: 1, Start: 1}} }

// helperTable builds this process's row, the server "recall" (pid 1320, its direct child, unless serverAlive is
// false), a subreaper (pid 1330), and two helpers stamped by this process with the name nameOfHelper: one adopted by
// pid 1 (the review's A22) and one by the subreaper (A23).
func helperTable(serverAlive bool, nameOfHelper string) func(mine Owner) []fakeProc {
	return func(mine Owner) []fakeProc {
		self := launchdSelfWithRow()
		extra := []string{}
		if nameOfHelper != "-" {
			extra = append(extra, nameStamp(nameOfHelper))
		}
		procs := []fakeProc{
			{info: self.ProcInfo, env: []string{"PATH=/bin"}},
			{info: ProcInfo{PID: 1330, PPID: 1, Start: 13300}, env: []string{"PATH=/bin"}},
			stamped(1321, 1, mine, extra...),
			stamped(1331, 1330, mine, extra...),
		}
		if serverAlive {
			procs = append(procs, stamped(1320, self.PID, mine, nameStamp("recall")))
		}
		return procs
	}
}

func registerServer(m *WarmRegistry, name string, pid int) {
	m.Servers[name] = &SubServer{Name: name, LastKnownPID: pid}
}

// TestSeamDetachedHelperOfARunningServerSpared: A22 and A23 signal nothing while "recall" runs.
func TestSeamDetachedHelperOfARunningServerSpared(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, launchdSelfWithRow(), helperTable(true, "recall"))
	registerServer(m, "recall", 1320)
	prune(t, m, fake)
	wantSignalled(t, fake)
}

// TestSeamDetachedHelperReapedOnceItsServerIsGone: the same helpers are reaped when the server has exited (the
// registry still holds its name and recorded pid, but the pid is no longer a child of this process), and when the
// registry no longer holds the server at all.
func TestSeamDetachedHelperReapedOnceItsServerIsGone(t *testing.T) {
	t.Run("server exited", func(t *testing.T) {
		m, fake, _ := newFakeProcessTableRegistry(t, launchdSelfWithRow(), helperTable(false, "recall"))
		registerServer(m, "recall", 1320)
		prune(t, m, fake)
		wantSignalled(t, fake, 1321, 1331)
	})
	t.Run("server removed from the registry", func(t *testing.T) {
		m, fake, _ := newFakeProcessTableRegistry(t, launchdSelfWithRow(), helperTable(true, "recall"))
		prune(t, m, fake)
		wantSignalled(t, fake, 1321, 1331)
	})
	t.Run("the recorded pid now belongs to another process", func(t *testing.T) {
		m, fake, _ := newFakeProcessTableRegistry(t, launchdSelfWithRow(), func(mine Owner) []fakeProc {
			procs := helperTable(false, "recall")(mine)
			return append(procs, fakeProc{info: ProcInfo{PID: 1320, PPID: 1, Start: 99999}, env: []string{"PATH=/bin"}})
		})
		registerServer(m, "recall", 1320)
		prune(t, m, fake)
		wantSignalled(t, fake, 1321, 1331)
	})
}

// TestSeamDetachedHelperOfAnUnknownServerReaped: a stamp naming a server this registry never had is reaped, while
// another server runs.
func TestSeamDetachedHelperOfAnUnknownServerReaped(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, launchdSelfWithRow(), helperTable(true, "ghost"))
	registerServer(m, "recall", 1320)
	prune(t, m, fake)
	wantSignalled(t, fake, 1321, 1331)
}

// TestSeamDetachedHelperWithNoServerNameReaped: an empty or missing server name is reaped, while a server runs.
func TestSeamDetachedHelperWithNoServerNameReaped(t *testing.T) {
	for _, name := range []string{"", "-"} {
		t.Run("name="+name, func(t *testing.T) {
			m, fake, _ := newFakeProcessTableRegistry(t, launchdSelfWithRow(), helperTable(true, name))
			registerServer(m, "recall", 1320)
			registerServer(m, "", 1320)
			prune(t, m, fake)
			wantSignalled(t, fake, 1321, 1331)
		})
	}
}

// TestSeamRecordedPidOneIsNeverRunning is the review's state S3-13: a corrupt record naming pid 1 does not make its
// server "running", so the helper stamped with that server's name is reaped.
func TestSeamRecordedPidOneIsNeverRunning(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, launchdSelfWithRow(), func(mine Owner) []fakeProc {
		return []fakeProc{
			{info: launchdSelfWithRow().ProcInfo, env: []string{"PATH=/bin"}},
			{info: ProcInfo{PID: 1, PPID: 0, Start: 5}, env: []string{"PATH=/bin"}},
			stamped(1371, 1, mine, nameStamp("recall")),
		}
	})
	registerServer(m, "recall", 1)
	prune(t, m, fake)
	wantSignalled(t, fake, 1371)
}

// TestSeamHelperWithTwoNameStampsReaped is the review's state S3-07: a helper whose environment carries two different
// server names names no server, so it is reaped even while one of those servers runs.
func TestSeamHelperWithTwoNameStampsReaped(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, launchdSelfWithRow(), func(mine Owner) []fakeProc {
		return []fakeProc{
			{info: launchdSelfWithRow().ProcInfo, env: []string{"PATH=/bin"}},
			stamped(1320, 100, mine, nameStamp("recall")),
			stamped(1321, 1, mine, nameStamp("recall"), nameStamp("ddg")),
		}
	})
	registerServer(m, "recall", 1320)
	prune(t, m, fake)
	wantSignalled(t, fake, 1321)
}

// The owner's ruling on an unreadable parent (0010, F5), label as relayed: "Yes: an unreadable parent for a recorded
// pid counts as running; spare the helper (Recommended)".

// TestSeamServerWithUnreadableParentCountsAsRunning is the review's state S3-05: the server's recorded pid is in the
// snapshot but its parent could not be read (PPID -1). It counts as running, and its detached helper is spared.
func TestSeamServerWithUnreadableParentCountsAsRunning(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, launchdSelfWithRow(), func(mine Owner) []fakeProc {
		srv := stamped(1320, 100, mine, nameStamp("recall"))
		srv.info.PPID = -1
		return []fakeProc{{info: launchdSelfWithRow().ProcInfo, env: []string{"PATH=/bin"}}, srv,
			stamped(1321, 1, mine, nameStamp("recall"))}
	})
	registerServer(m, "recall", 1320)
	prune(t, m, fake)
	wantSignalled(t, fake)
}

// TestSeamServerMissingFromTheSnapshotIsNotRunning is the review's state S3-06, the half of F5 the ruling does not
// cover: a recorded pid with no row in the snapshot cannot be told from a server that exited, so it is not running and
// the helper is reaped. This pins the unchanged half.
func TestSeamServerMissingFromTheSnapshotIsNotRunning(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, launchdSelfWithRow(), func(mine Owner) []fakeProc {
		return []fakeProc{{info: launchdSelfWithRow().ProcInfo, env: []string{"PATH=/bin"}},
			stamped(1321, 1, mine, nameStamp("recall"))}
	})
	registerServer(m, "recall", 1320)
	prune(t, m, fake)
	wantSignalled(t, fake, 1321)
}

// TestSeamDeadOwnersOrphanReapedEvenIfItsServerRuns: the ruling spares only this process's own helpers; an orphan of
// a dead owner of this lineage is reaped as before, even when a server of the name it carries runs now.
func TestSeamDeadOwnersOrphanReapedEvenIfItsServerRuns(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, launchdSelfWithRow(), func(mine Owner) []fakeProc {
		procs := helperTable(true, "recall")(mine)
		return append(procs, stamped(1340, 1, Owner{Lineage: mine.Lineage, PID: 1349, Start: 13490}, nameStamp("recall")))
	})
	registerServer(m, "recall", 1320)
	prune(t, m, fake)
	wantSignalled(t, fake, 1340)
}
