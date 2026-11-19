package testsafety

import (
	"fmt"
	"strings"
	"testing"
)

func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }

func replaceOnce(t *testing.T, s, old, repl string) string {
	t.Helper()
	if strings.Count(s, old) != 1 {
		t.Fatalf("fixture anchor occurs %d times: %q", strings.Count(s, old), old)
	}
	return strings.Replace(s, old, repl, 1)
}

// Fixtures for the guard's hardening after the independent review (P8): each of the review's bypasses G1 to G16 is
// planted in a temporary tree and must be reported by the rule named; each sanctioned form must stay green.

const childOnlyGood = "package proctable\n\nimport (\n\t\"os/exec\"\n\t\"testing\"\n)\n\n" +
	"type childOnlyTable struct {\n\tinner *osProcessTable\n\tcmd   *exec.Cmd\n}\n\n" +
	"func newChildOnlyTable(t *testing.T, cmd *exec.Cmd) *childOnlyTable {\n\treturn &childOnlyTable{inner: &osProcessTable{armed: true}, cmd: cmd}\n}\n\n" +
	"func (c *childOnlyTable) isOwnChild(p int) bool { return c.cmd != nil && p > 1 }\n" +
	"func (c *childOnlyTable) Snapshot() error { return c.inner.x() }\n" +
	"func (c *childOnlyTable) Self() error { return c.inner.x() }\n" +
	"func (c *childOnlyTable) Environ(p int) error {\n\tif !c.isOwnChild(p) {\n\t\treturn nil\n\t}\n\treturn c.inner.x()\n}\n" +
	"func (c *childOnlyTable) Exe(p int) error {\n\tif !c.isOwnChild(p) {\n\t\treturn nil\n\t}\n\treturn c.inner.x()\n}\n" +
	"func (c *childOnlyTable) Cmdline(p int) error {\n\tif !c.isOwnChild(p) {\n\t\treturn nil\n\t}\n\treturn c.inner.x()\n}\n" +
	"func (c *childOnlyTable) Alive(p int) bool {\n\tif !c.isOwnChild(p) {\n\t\treturn false\n\t}\n\treturn c.inner.y()\n}\n" +
	"func (c *childOnlyTable) Signal(p int, s int) error {\n\tif !c.isOwnChild(p) {\n\t\treturn nil\n\t}\n\treturn c.inner.x()\n}\n"

func TestFixture_ReviewBypassesAreReported(t *testing.T) {
	hp := "package handler\n\nimport (\n\t\"testing\"\n%s)\n\nfunc TestPlant(t *testing.T) {\n%s\n}\n"
	pt := "package proctable\n\nimport \"testing\"\n\nfunc TestPlant(t *testing.T) {\n%s\n}\n"
	sp := func(f string, a ...any) string { return sprintf(f, a...) }
	plants := []struct {
		id, file, src, rule, want string
	}{
		{"G1 syscall under an import alias", "h/p_test.go", sp(hp, "\tsc \"syscall\"\n", "\t_ = sc.Kill(123456, 9)"), RuleB, `"syscall.Kill"`},
		{"G2 syscall dot-imported", "h/p_test.go", sp(hp, "\t. \"syscall\"\n", "\t_ = Kill(123456, 9)"), RuleB, `"dot-import:syscall"`},
		{"G3 syscall.Kill as a function value", "h/p_test.go", sp(hp, "\t\"syscall\"\n", "\tk := syscall.Kill\n\t_ = k(123456, 9)"), RuleB, `"syscall.Kill"`},
		{"G4 raw syscall", "h/p_test.go", sp(hp, "\t\"syscall\"\n", "\t_, _, _ = syscall.Syscall(syscall.SYS_KILL, 123456, 9, 0)"), RuleB, `"raw-syscall"`},
		{"G4 SYS_KILL", "h/p_test.go", sp(hp, "\t\"syscall\"\n", "\t_, _, _ = syscall.Syscall(syscall.SYS_KILL, 123456, 9, 0)"), RuleB, `"sys-kill-number"`},
		{"G4 unix raw syscall under an alias", "h/p_test.go", sp(hp, "\tux \"golang.org/x/sys/unix\"\n", "\t_, _, _ = ux.RawSyscall(ux.SYS_TGKILL, 1, 2, 9)"), RuleB, `"raw-syscall"`},
		{"G5 /bin/kill by path", "h/p_test.go", sp(hp, "\t\"os/exec\"\n", "\t_ = exec.Command(\"/bin/kill\", \"-9\", \"123456\").Run()"), RuleB, `"cmd:kill"`},
		{"G5 taskkill.exe by Windows path", "h/p.go", "package h\n\nfunc f() []string { return []string{`C:\\Windows\\System32\\taskkill.exe`, \"/F\"} }\n", RuleB, `"cmd:taskkill"`},
		{"G6 kill through sh -c", "h/p_test.go", sp(hp, "\t\"os/exec\"\n", "\t_ = exec.Command(\"sh\", \"-c\", \"sleep 1; kill -9 123456\").Run()"), RuleB, `"cmd:kill"`},
		{"G7 exact /proc literal", "h/p_test.go", sp(hp, "\t\"os\"\n\t\"path/filepath\"\n", "\t_, _ = os.ReadFile(filepath.Join(\"/proc\", \"1\", \"environ\"))"), RuleB, `"procfs"`},
		{"G8 cgo", "h/p.go", "package h\n\n// #include <signal.h>\nimport \"C\"\n\nfunc plantKill(pid int) { C.kill(C.int(pid), 9) }\n", RuleB, `"cgo"`},
		{"G10 GetProcessEnviron as a function value", "h/p_test.go", sp(hp, "\t\"example.com/m/internal/util\"\n", "\tf := util.GetProcessEnviron\n\t_, _ = f(1)"), RuleB, `"GetProcessEnviron"`},
		{"os.FindProcess under an alias", "h/p.go", "package h\n\nimport o \"os\"\n\nfunc f() { _, _ = o.FindProcess(1) }\n", RuleB, `"os.FindProcess"`},
		{"G11 a proctable test names signalGroupAndPID", "internal/proctable/p_test.go", sp(pt, "\t_ = signalGroupAndPID(123456, 9)"), RuleA4, "names signalGroupAndPID"},
		{"G11 a proctable test names startToken", "internal/proctable/p_test.go", sp(pt, "\t_, _ = startToken(1)"), RuleA4, "names startToken"},
		{"G12 .inner outside the child-only table", "internal/proctable/p_test.go", sp(pt, "\t_ = newChildOnlyTable(t, nil).inner.Signal(nil, 9)"), RuleA4, "uses .inner outside"},
		{"G13 the child-only table scoped to a literal pid", "internal/proctable/p_test.go", sp(pt, "\t_, _ = newChildOnlyTable(t, 123456).Environ(nil)"), RuleA4, "must be given the *exec.Cmd"},
		{"G13 the child-only table given a forged command", "internal/proctable/p_test.go",
			"package proctable\n\nimport (\n\t\"os\"\n\t\"os/exec\"\n\t\"testing\"\n)\n\nfunc TestPlant(t *testing.T) {\n\tc := &exec.Cmd{Process: &os.Process{Pid: 123456}}\n\t_ = newChildOnlyTable(t, c)\n}\n",
			RuleA4, "never forged"},
		{"G16 the constructor as a function value in non-test code", "internal/h/p.go",
			"package h\n\nimport \"example.com/m/internal/proctable\"\n\nvar plantTable = proctable.NewOSProcessTable\n", RuleD, "referenced from non-test code outside"},
	}
	for _, p := range plants {
		t.Run(p.id, func(t *testing.T) {
			files := map[string]string{p.file: p.src}
			if p.rule == RuleD {
				files["internal/proctable/t.go"] = tblDecl
				files[mainFile] = "package main\n\nfunc main() { _ = proctable.NewOSProcessTable() }\n"
			}
			if p.rule == RuleA4 {
				files["internal/proctable/childonly_test.go"] = childOnlyGood
			}
			rep := mustCheck(t, files, Options{})
			if !hasFinding(rep.ByRule(p.rule), p.file, p.want) {
				t.Fatalf("plant not reported by rule %s (want %q): %v", p.rule, p.want, rep.Findings)
			}
		})
	}
}

func TestFixture_RuleB_ExtraTestCallOfTheCleanup(t *testing.T) {
	src := "package db\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { _ = cleanupStaleProcess(t.TempDir()) }\n\n" +
		"func TestPlant(t *testing.T) { _ = cleanupStaleProcess(\"/some/live/store\") }\n"
	allow := Options{Allowlist: []Allow{{File: "internal/db/c_test.go", Primitive: primTestCleanupStale, Site: "S4", Reason: "fixture", Count: 1}}}
	rep := mustCheck(t, map[string]string{"internal/db/c_test.go": src}, allow)
	if !hasFinding(rep.ByRule(RuleB), "internal/db/c_test.go", "used 2 times here, the allowlist records 1") {
		t.Fatalf("an extra cleanupStaleProcess call in an allowlisted test was not reported: %v", rep.Findings)
	}
}

func TestFixture_RuleB_SanctionedFormsStayGreen(t *testing.T) {
	clean := map[string]string{
		"a/a.go": "package a\n\nimport (\n\t\"os\"\n\t\"syscall\"\n)\n\n" +
			"// syscall.Kill, /proc/1/environ and sh -c \"kill\" in a comment are not uses.\n" +
			"func f(p *os.Process) error {\n\t_ = os.Environ()\n\t_ = syscall.SIGKILL\n\t_ = \"process 1 could not kill\"\n\treturn p.Signal(syscall.Signal(0))\n}\n",
	}
	allow := Options{Allowlist: []Allow{{File: "a/a.go", Primitive: primSignal, Site: "probe", Reason: "fixture", Count: 1}}}
	if rep := mustCheck(t, clean, allow); len(rep.Findings) != 0 {
		t.Fatalf("sanctioned forms were reported (syscall.Signal(0) is a conversion, not a second signal): %v", rep.Findings)
	}
}

func TestFixture_RuleA4_TheChildOnlyTableShape(t *testing.T) {
	if rep := mustCheck(t, map[string]string{"internal/proctable/childonly_test.go": childOnlyGood}, Options{}); len(rep.ByRule(RuleA4)) != 0 {
		t.Fatalf("the sanctioned shape was reported: %v", rep.ByRule(RuleA4))
	}
	plants := map[string]struct{ old, new, want string }{
		"scoped to a pid instead of the command": {"cmd *exec.Cmd) *childOnlyTable {", "pid int) *childOnlyTable {", "must be declared (t *testing.T, cmd *exec.Cmd)"},
		"a read without the own-child check": {"func (c *childOnlyTable) Exe(p int) error {\n\tif !c.isOwnChild(p) {\n\t\treturn nil\n\t}\n",
			"func (c *childOnlyTable) Exe(p int) error {\n", "childOnlyTable.Exe must begin"},
		"the signal checked against something else": {"func (c *childOnlyTable) Signal(p int, s int) error {\n\tif !c.isOwnChild(p) {",
			"func (c *childOnlyTable) Signal(p int, s int) error {\n\tif !c.isOwnChild(s) {", "childOnlyTable.Signal must begin"},
		"a method missing": {"func (c *childOnlyTable) Alive(p int) bool {\n\tif !c.isOwnChild(p) {\n\t\treturn false\n\t}\n\treturn c.inner.y()\n}\n", "",
			"lacks its method Alive"},
	}
	for name, p := range plants {
		t.Run(name, func(t *testing.T) {
			src := replaceOnce(t, childOnlyGood, p.old, p.new)
			rep := mustCheck(t, map[string]string{"internal/proctable/childonly_test.go": src}, Options{})
			if !hasFinding(rep.ByRule(RuleA4), "internal/proctable/childonly_test.go", p.want) {
				t.Fatalf("not reported (want %q): %v", p.want, rep.Findings)
			}
		})
	}
}
