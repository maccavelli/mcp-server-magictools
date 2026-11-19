package client

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// These tests are pure: they build strings and read a temporary directory, and start, enumerate or signal nothing.

const testLineage = "0123456789abcdef0123456789abcdef"

func TestSeamOwnerStampRoundTrip(t *testing.T) {
	o := Owner{Lineage: testLineage, PID: 4242, Start: 987654321}
	if got := o.Stamp(); got != "v1:"+testLineage+":4242:987654321" {
		t.Fatalf("Stamp() = %q", got)
	}
	got, ok := ParseOwner([]string{"PATH=/bin", o.EnvEntry(), "HOME=/tmp"})
	if !ok || got != o {
		t.Fatalf("ParseOwner(Stamp) = %+v, %v; want %+v, true", got, ok, o)
	}
	// The same value twice is one stamp.
	if _, ok := ParseOwner([]string{o.EnvEntry(), o.EnvEntry()}); !ok {
		t.Fatal("two identical stamps were rejected")
	}
}

// TestSeamOwnerStampRejectsUnattributable is R4 as data: every stamp the reaper cannot attribute is rejected.
func TestSeamOwnerStampRejectsUnattributable(t *testing.T) {
	good := "v1:" + testLineage + ":4242:99"
	cases := map[string][]string{
		"missing":                 {"PATH=/bin"},
		"empty value":             {EnvOwner + "="},
		"foreign version":         {EnvOwner + "=v2:" + testLineage + ":4242:99"},
		"three fields":            {EnvOwner + "=v1:" + testLineage + ":4242"},
		"five fields":             {EnvOwner + "=" + good + ":1"},
		"short lineage":           {EnvOwner + "=v1:0123:4242:99"},
		"upper-case lineage":      {EnvOwner + "=v1:" + strings.ToUpper(testLineage) + ":4242:99"},
		"non-hex lineage":         {EnvOwner + "=v1:" + strings.Repeat("g", 32) + ":4242:99"},
		"pid zero":                {EnvOwner + "=v1:" + testLineage + ":0:99"},
		"pid one (init, launchd)": {EnvOwner + "=v1:" + testLineage + ":1:99"},
		"pid negative":            {EnvOwner + "=v1:" + testLineage + ":-5:99"},
		"pid with plus":           {EnvOwner + "=v1:" + testLineage + ":+5:99"},
		"pid above int32":         {EnvOwner + "=v1:" + testLineage + ":2147483648:99"},
		"token zero":              {EnvOwner + "=v1:" + testLineage + ":4242:0"},
		"token not decimal":       {EnvOwner + "=v1:" + testLineage + ":4242:0x10"},
		"token with space":        {EnvOwner + "=v1:" + testLineage + ":4242: 99"},
		"two different values":    {EnvOwner + "=" + good, EnvOwner + "=v1:" + testLineage + ":4243:99"},
		"old constant only":       {EnvManaged + "=" + EnvManagedValue},
		"prefix of the name only": {EnvOwner + "X=" + good},
	}
	for name, env := range cases {
		if o, ok := ParseOwner(env); ok {
			t.Errorf("%s: ParseOwner(%q) = %+v, true; want rejected", name, env, o)
		}
	}
}

// TestSeamOwnerStampNotMatchedByOldTests checks the new stamp's entry against the two tests old binaries apply:
// the reaper's exact match (process_reaper.go:132 at 3443524) and the start-up sweep's exact-or-prefix match
// (infra_unix.go:105). The child as a whole still carries the sub-server flag and is still matched (MADR R3,
// amended); this checks only that the new entry adds no match of its own.
func TestSeamOwnerStampNotMatchedByOldTests(t *testing.T) {
	entry := Owner{Lineage: testLineage, PID: 1234, Start: 5}.EnvEntry()
	oldReaper := func(env []string) bool { return slices.Contains(env, EnvManaged+"="+EnvManagedValue) }
	oldSweep := func(env []string) bool {
		for _, e := range env {
			if e == EnvManaged+"="+EnvManagedValue || strings.HasPrefix(e, "MAGIC_TOOLS_PEER_ID=") {
				return true
			}
		}
		return false
	}
	if oldReaper([]string{entry}) || oldSweep([]string{entry}) {
		t.Fatalf("the owner entry %q is matched by an old binary's test", entry)
	}
}

func TestSeamLineageIsStableAndDistinct(t *testing.T) {
	a := LineageFor("/data/one", "")
	if len(a) != 32 || !validLineage(a) {
		t.Fatalf("LineageFor = %q: not 32 lower-case hex characters", a)
	}
	if a != LineageFor("/data/one", "") {
		t.Fatal("LineageFor is not deterministic")
	}
	if a == LineageFor("/data/two", "") {
		t.Fatal("two store directories share a lineage")
	}
	if a == LineageFor("/data/one", "pid:[4026531836]") {
		t.Fatal("two pid namespaces share a lineage")
	}
	// The separator keeps the two inputs apart.
	if LineageFor("/a", "b") == LineageFor("/a\x00b", "") {
		t.Fatal("directory and namespace are not separated")
	}
}

func TestSeamCanonicalStoreDirResolvesLinks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "store")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	a, err := CanonicalStoreDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalStoreDir(link + string(filepath.Separator) + ".")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("one directory, two canonical forms: %q and %q", a, b)
	}
	if _, err := CanonicalStoreDir(""); err == nil {
		t.Fatal("an empty store directory was accepted")
	}
	if _, err := CanonicalStoreDir(filepath.Join(root, "absent")); err == nil {
		t.Fatal("a missing store directory was accepted")
	}
}
