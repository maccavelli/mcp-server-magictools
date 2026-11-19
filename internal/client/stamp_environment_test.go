package client

import (
	"slices"
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

// The stamp on a sub-server's environment (0010, D4; P4b step 6). Pure functions and the spawn path's environment
// builder, over the fake table: no process is started.

func countPrefix(env []string, prefix string) int {
	n := 0
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

func TestSeamStampEnvironmentStripsInheritedAndStamps(t *testing.T) {
	mine := Owner{Lineage: testLineage, PID: 4242, Start: 9}
	stale := Owner{Lineage: strings.Repeat("f", 32), PID: 1234, Start: 1}
	base := []string{
		"PATH=/bin",
		EnvManaged + "=" + EnvManagedValue,
		EnvServerName + "=other",
		stale.EnvEntry(),
		envPeerID + "=abc",
		"HOME=/h",
		EnvManaged + "=false",
	}
	got := stampEnvironment(base, &mine, "svc")
	want := []string{"PATH=/bin", "HOME=/h", EnvManaged + "=" + EnvManagedValue, EnvServerName + "=svc", mine.EnvEntry()}
	if !slices.Equal(got, want) {
		t.Fatalf("stampEnvironment =\n%q\nwant\n%q", got, want)
	}
	if o, ok := ParseOwner(got); !ok || o != mine {
		t.Fatalf("the stamped owner reads back as %+v, %v", o, ok)
	}
	if !slices.Equal(base[:2], []string{"PATH=/bin", EnvManaged + "=" + EnvManagedValue}) {
		t.Fatal("stampEnvironment modified its input")
	}
}

func TestSeamStampEnvironmentWithoutOwner(t *testing.T) {
	got := stampEnvironment([]string{"PATH=/bin", EnvOwner + "=v1:" + testLineage + ":5:5"}, nil, "svc")
	want := []string{"PATH=/bin", EnvManaged + "=" + EnvManagedValue, EnvServerName + "=svc"}
	if !slices.Equal(got, want) {
		t.Fatalf("a registry without an owner stamped %q, want %q", got, want)
	}
}

// TestSeamSpawnEnvironmentCarriesTheOwner: the environment a spawn would receive, built by the spawn path from this
// process's environment and the server's configured one, both carrying stale stamps (as under another orchestrator or
// a test): exactly one of each variable survives, the owner is this registry's, and no peer id is stamped.
func TestSeamSpawnEnvironmentCarriesTheOwner(t *testing.T) {
	stale := Owner{Lineage: strings.Repeat("e", 32), PID: 77, Start: 7}
	t.Setenv(EnvOwner, stale.Stamp())
	t.Setenv(envPeerID, "inherited")
	t.Setenv(EnvManaged, "true")
	t.Setenv(EnvServerName, "parent-server")

	self := SelfInfo{ProcInfo: ProcInfo{PID: 4321, PPID: 1, Start: 99}}
	fake := newFakeProcessTable(t, self)
	m := &WarmRegistry{Config: &config.Config{}}
	if err := m.AttachProcessTable(fake, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	env := m.prepareProcessEnvironment("svc", map[string]string{EnvOwner: "set-by-config", "EXTRA": "1"}, 0, 0, 0)

	for prefix, want := range map[string]int{EnvManaged + "=": 1, EnvServerName + "=": 1, EnvOwner + "=": 1, envPeerID + "=": 0} {
		if got := countPrefix(env, prefix); got != want {
			t.Errorf("%s appears %d times, want %d", prefix, got, want)
		}
	}
	if !slices.Contains(env, EnvManaged+"="+EnvManagedValue) || !slices.Contains(env, EnvServerName+"=svc") {
		t.Errorf("the sub-server protocol flag or the name stamp is missing: %q", env)
	}
	owner, ok := ParseOwner(env)
	if !ok || owner != m.procs.Load().owner {
		t.Fatalf("the child's owner is %+v, %v; want this registry's %+v", owner, ok, m.procs.Load().owner)
	}
	if !slices.Contains(env, "EXTRA=1") {
		t.Error("the server's own configured environment was lost")
	}
	if fake.snapshotCount() != 0 || len(fake.signalLog()) != 0 {
		t.Error("building an environment read or signalled processes")
	}
}
