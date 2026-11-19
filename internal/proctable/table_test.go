package proctable

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
)

// The one test of the operating system's process table (0010-MADR-make-tests-unable-to-signal-real-processes.md, D2).
//
// Safe by construction: it holds only a childOnlyTable, which reads and signals nothing but a child this test started
// (the test binary re-executed as a helper); it never passes pid 0, 1, -1, its own pid or its parent's to a real
// table (those refusals are tested on validateTarget over data); and its child carries an owner stamp with a random
// lineage, which is never a production one. Its first execution is the plan's P9, on the confirmed Linux machine.

const (
	helperEnv = "PROCTABLE_0010_HELPER" // "plain" or "ignore-term"
	markEnv   = "PROCTABLE_0010_MARK"
)

// TestOSTableHelperChild is the child process: it prints its own start token as read by its own table, then waits
// until its stdin closes or it is signalled. Run directly, it skips.
func TestOSTableHelperChild(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		t.Skip("helper process for the OS-table tests")
	}
	if mode == "ignore-term" {
		signal.Ignore(syscall.SIGTERM)
	}
	self, err := newChildOnlyTable(t, nil).Self()
	if err != nil {
		fmt.Printf("ERROR %v\n", err)
		os.Exit(3)
	}
	fmt.Printf("TOKEN %d\n", self.Start)
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n/2)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// cleanEnv drops every orchestrator variable this test or its caller may have inherited.
func cleanEnv(env []string) []string {
	drop := []string{client.EnvManaged + "=", client.EnvServerName + "=", client.EnvOwner + "=",
		"MAGIC_TOOLS_PEER_ID=", helperEnv + "=", markEnv + "="}
	return slices.DeleteFunc(slices.Clone(env), func(e string) bool {
		for _, d := range drop {
			if strings.HasPrefix(e, d) {
				return true
			}
		}
		return false
	})
}

type helperChild struct {
	cmd   *exec.Cmd
	token uint64
	done  chan error
	owner client.Owner
	mark  string
}

// startHelper starts this test binary as a helper child with a random owner stamp and mark, and reads its token.
func startHelper(t *testing.T, mode string) *helperChild {
	t.Helper()
	owner := client.Owner{Lineage: randomHex(t, 32), PID: safePID(os.Getpid()), Start: 1}
	mark := randomHex(t, 16)
	cmd := exec.Command(os.Args[0], "-test.run=^TestOSTableHelperChild$")
	cmd.Env = append(cleanEnv(os.Environ()), helperEnv+"="+mode, owner.EnvEntry(), markEnv+"="+mark)
	prepareChild(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	h := &helperChild{cmd: cmd, done: make(chan error, 1), owner: owner, mark: mark}
	// Wait starts at once, so that a t.Fatal below (no token within 30 s) cannot leave the cleanup waiting for ever.
	go func() { h.done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = stdin.Close()
		select {
		case <-h.done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill() // our own child, through its handle
			<-h.done
		}
	})
	lines := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "TOKEN ") || strings.HasPrefix(l, "ERROR ") {
				lines <- l
				return
			}
		}
		lines <- ""
	}()
	var line string
	select {
	case line = <-lines:
	case <-time.After(30 * time.Second):
		t.Fatal("the helper child did not report its token")
	}
	tok, err := strconv.ParseUint(strings.TrimPrefix(line, "TOKEN "), 10, 64)
	if err != nil || tok == 0 {
		t.Fatalf("the helper child reported %q", line)
	}
	h.token = tok
	return h
}

func findRow(t *testing.T, tbl *childOnlyTable, pid int) client.ProcInfo {
	t.Helper()
	snap, err := tbl.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range snap {
		if int(row.PID) == pid {
			return row
		}
	}
	t.Fatalf("the child %d is not in the snapshot", pid)
	return client.ProcInfo{}
}

func waitExit(t *testing.T, h *helperChild, within time.Duration) {
	t.Helper()
	select {
	case <-h.done:
		h.done <- nil // let the cleanup return
	case <-time.After(within):
		t.Fatalf("the child did not exit within %v", within)
	}
}

// TestOSTableChildRoundTrip: the snapshot sees the child with this process as its parent; the token the child reads
// of itself equals the token this process reads of it; its environment carries the mark and the owner stamp; a signal
// with a wrong token is refused and the child lives; TERM ends it.
func TestOSTableChildRoundTrip(t *testing.T) {
	h := startHelper(t, "plain")
	tbl := newChildOnlyTable(t, h.cmd)
	row := findRow(t, tbl, h.cmd.Process.Pid)
	if row.PPID != safePID(os.Getpid()) {
		t.Fatalf("the child's parent is %d, want this process %d", row.PPID, os.Getpid())
	}
	if row.Start == 0 || row.Start != h.token {
		t.Fatalf("start token: this process read %d, the child read %d of itself", row.Start, h.token)
	}
	env, err := tbl.Environ(row)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(env, markEnv+"="+h.mark) {
		t.Fatal("the child's environment, read back through the table, lacks the mark")
	}
	if got, ok := client.ParseOwner(env); !ok || got != h.owner {
		t.Fatalf("the owner stamp read back is %+v, %v; want %+v", got, ok, h.owner)
	}
	wrong := row
	wrong.Start++
	// Re-identification after a read: each read of the child with a wrong start token reads the child (it is ours)
	// and is then refused, because the token no longer matches.
	if _, err := tbl.Environ(wrong); !errors.Is(err, errNotSameProcess) {
		t.Fatalf("Environ with a wrong start token: %v, want errNotSameProcess", err)
	}
	if _, err := tbl.Exe(wrong); !errors.Is(err, errNotSameProcess) {
		t.Fatalf("Exe with a wrong start token: %v, want errNotSameProcess", err)
	}
	if _, err := tbl.Cmdline(wrong); !errors.Is(err, errNotSameProcess) {
		t.Fatalf("Cmdline with a wrong start token: %v, want errNotSameProcess", err)
	}
	if tbl.Alive(wrong) {
		t.Fatal("Alive with a wrong start token reported true")
	}
	if err := tbl.Signal(wrong, client.SigTerm); err == nil {
		t.Fatal("a signal with a wrong start token was not refused")
	}
	if !tbl.Alive(row) {
		t.Fatal("the child died after a refused signal")
	}
	if err := tbl.Signal(row, client.SigTerm); err != nil {
		t.Fatalf("TERM to the child: %v", err)
	}
	waitExit(t, h, 10*time.Second)
	if h.cmd.ProcessState == nil || h.cmd.ProcessState.Success() {
		t.Fatalf("the child did not end by the signal: %v", h.cmd.ProcessState)
	}
	if tbl.Alive(row) {
		t.Fatal("a reaped child is reported alive")
	}
}

// TestOSTableEscalationKillsTermIgnoringChild runs the client's real escalation (real clock, 2 s grace) against a
// child that ignores SIGTERM.
func TestOSTableEscalationKillsTermIgnoringChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("taskkill /F ends the child at once; the escalation path is unix-only")
	}
	h := startHelper(t, "ignore-term")
	tbl := newChildOnlyTable(t, h.cmd)
	row := findRow(t, tbl, h.cmd.Process.Pid)
	killed, err := client.Escalate(tbl, client.RealClock(), row, 2*time.Second, 100*time.Millisecond)
	if err != nil || !killed {
		t.Fatalf("Escalate = %v, %v; want true, nil", killed, err)
	}
	waitExit(t, h, 10*time.Second)
	assertKilledByKill(t, h.cmd.ProcessState)
}

// TestOSTableValidateTargetRefusesUnsafePIDs is over data only: no table is built and no call reaches the system.
func TestOSTableValidateTargetRefusesUnsafePIDs(t *testing.T) {
	const self, parent = int32(5000), int32(4999)
	for _, p := range []client.ProcInfo{
		{PID: 1, Start: 9}, {PID: 0, Start: 9}, {PID: -1, Start: 9}, {PID: -5000, Start: 9},
		{PID: self, Start: 9}, {PID: parent, Start: 9}, {PID: 6000, Start: 0},
	} {
		if err := validateTarget(p, self, parent); err == nil {
			t.Errorf("validateTarget(%+v) accepted an unsafe target", p)
		}
	}
	if err := validateTarget(client.ProcInfo{PID: 6000, Start: 9}, self, parent); err != nil {
		t.Errorf("validateTarget refused an ordinary target: %v", err)
	}
}

// TestOSTableParseStatStart is over data only.
func TestOSTableParseStatStart(t *testing.T) {
	fields := "S 1 1 1 0 -1 4194560 100 0 0 0 1 2 0 0 20 0 1 0 987654 1000 10"
	cases := map[string]uint64{
		"1234 (sleep) " + fields:            987654,
		"1234 (a b) c) " + fields:           987654,
		"1234 ((nested)) " + fields + " 99": 987654,
	}
	for in, want := range cases {
		got, err := parseStatStart([]byte(in))
		if err != nil || got != want {
			t.Errorf("parseStatStart(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "1234 sleep S 1", "1234 (x) S 1 2 3", "1234 (x) " + strings.Replace(fields, "987654", "0", 1),
		"1234 (x) " + strings.Replace(fields, "987654", "abc", 1)} {
		if _, err := parseStatStart([]byte(bad)); err == nil {
			t.Errorf("parseStatStart(%q) accepted a malformed line", bad)
		}
	}
}

// TestOSTableGroupSignalTargetRefusesLowPIDs is over data only: groupSignalTarget is the first statement of
// signalGroupAndPID, which this package's tests never call (rule A4); kill(-1) would reach every process the user may
// signal, so the refusal is tested here and never by sending anything.
func TestOSTableGroupSignalTargetRefusesLowPIDs(t *testing.T) {
	for _, pid := range []int{1, 0, -1, -2} {
		if err := groupSignalTarget(pid); err == nil {
			t.Errorf("groupSignalTarget(%d) accepted a pid that must never be signalled", pid)
		}
	}
	if err := groupSignalTarget(4242); err != nil {
		t.Errorf("groupSignalTarget refused an ordinary pid: %v", err)
	}
}

// TestOSTableGateShutForANilTable is over data only (the owner's ruling, as relayed: "The reviewer's third way: the
// gate as a pure function, plus a guard clause on every method (Recommended)"). The argument is an untyped nil, so the
// test names no osProcessTable (rule A3). The other shut branch, a non-nil table that is not armed, cannot be built in
// a test (rule A3) and is verified by reading only.
func TestOSTableGateShutForANilTable(t *testing.T) {
	if gateOpen(nil) {
		t.Fatal("gateOpen(nil) is true: a nil table would read and signal")
	}
}
