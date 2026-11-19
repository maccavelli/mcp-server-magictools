package client

import (
	"errors"
	"testing"
)

// AttachProcessTable over the fake table: no process is read or signalled.

func TestSeamAttachProcessTableBindsOwnerAndLineage(t *testing.T) {
	self := SelfInfo{ProcInfo: ProcInfo{PID: 4321, PPID: 1, Start: 777}, Namespace: "pid:[1]"}
	fake := newFakeProcessTable(t, self)
	dir := t.TempDir()
	m := &WarmRegistry{}
	if m.ProcessTableAttached() {
		t.Fatal("a zero registry reports a table")
	}
	if err := m.AttachProcessTable(fake, dir); err != nil {
		t.Fatal(err)
	}
	if !m.ProcessTableAttached() {
		t.Fatal("the table was not attached")
	}
	canon, err := CanonicalStoreDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	b := m.procs.Load()
	want := Owner{Lineage: LineageFor(canon, self.Namespace), PID: 4321, Start: 777}
	if b.owner != want || b.table != ProcessTable(fake) || b.self != self {
		t.Fatalf("binding = %+v, want owner %+v", b, want)
	}
	if err := m.AttachProcessTable(fake, dir); err == nil {
		t.Fatal("a second attach was accepted")
	}
	if got := fake.signalLog(); len(got) != 0 || fake.snapshotCount() != 0 {
		t.Fatalf("attaching read or signalled processes: signals %v, snapshots %d", got, fake.snapshotCount())
	}
}

func TestSeamAttachProcessTableRefusesBadInputs(t *testing.T) {
	self := SelfInfo{ProcInfo: ProcInfo{PID: 4321, PPID: 1, Start: 777}}
	m := &WarmRegistry{}
	if err := m.AttachProcessTable(nil, t.TempDir()); err == nil {
		t.Fatal("a nil table was accepted")
	}
	if err := m.AttachProcessTable(newFakeProcessTable(t, self), ""); err == nil {
		t.Fatal("an empty store directory was accepted")
	}
	broken := newFakeProcessTable(t, self)
	broken.selfErr = errors.New("no self")
	if err := m.AttachProcessTable(broken, t.TempDir()); err == nil {
		t.Fatal("a table that cannot describe this process was accepted")
	}
	noToken := newFakeProcessTable(t, SelfInfo{ProcInfo: ProcInfo{PID: 4321, PPID: 1}})
	if err := m.AttachProcessTable(noToken, t.TempDir()); err == nil {
		t.Fatal("a table with no start token for this process was accepted")
	}
	if m.ProcessTableAttached() {
		t.Fatal("a refused attach left a table attached")
	}
}
