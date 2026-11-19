package testsafety

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// notYetEnforceable is the exact reason a constructor rule reports while the constructor does not exist. A skip
// carrying it is not a pass: P4 builds the constructor, which turns the rules on, and P7 plants a violation to prove
// they are live.
const notYetEnforceable = "not yet enforceable: constructor absent"

func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root %s has no go.mod: %v", root, err)
	}
	return root
}

func report(t *testing.T, fs []Finding) {
	t.Helper()
	for _, f := range fs {
		t.Error(f.String())
	}
}

// TestModuleSafety runs the guard over this module. It is red, by design, until the three tests that scan the real
// machine move onto a fake process table (P5 of 0010-PLAN-make-tests-unable-to-signal-real-processes.md).
func TestModuleSafety(t *testing.T) {
	opt := DefaultOptions()
	rep, err := Check(moduleRoot(t), opt)
	if err != nil {
		t.Fatal(err)
	}
	skipCtor := !rep.ConstructorPresent && !opt.ConstructorRequired

	t.Run("B_primitives_are_allowlisted", func(t *testing.T) { report(t, rep.ByRule(RuleB)) })
	t.Run("C_tests_that_call_the_reaper_use_a_fake_table", func(t *testing.T) { report(t, rep.ByRule(RuleC)) })
	t.Run("A_no_test_names_the_real_constructor", func(t *testing.T) {
		if skipCtor {
			t.Skip(notYetEnforceable)
		}
		report(t, rep.ByRule(RuleA))
	})
	t.Run("D_one_production_call_of_the_constructor", func(t *testing.T) {
		if skipCtor {
			t.Skip(notYetEnforceable)
		}
		report(t, rep.ByRule(RuleD))
	})
	t.Run("A2_no_test_outside_proctable_imports_it", func(t *testing.T) { report(t, rep.ByRule(RuleA2)) })
	t.Run("A3_the_real_table_is_built_only_by_its_two_constructors", func(t *testing.T) { report(t, rep.ByRule(RuleA3)) })
	t.Run("E_the_constructor_panics_first_under_test", func(t *testing.T) { report(t, rep.ByRule(RuleE)) })
	t.Run("A4_proctable_tests_hold_only_the_child_only_table", func(t *testing.T) { report(t, rep.ByRule(RuleA4)) })
	t.Run("E2_every_real_table_method_checks_the_gate_first", func(t *testing.T) { report(t, rep.ByRule(RuleE2)) })
	t.Run("E2b_the_real_tables_package_reaches_primitives_only_through_the_gate", func(t *testing.T) { report(t, rep.ByRule(RuleE2b)) })
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func mustCheck(t *testing.T, files map[string]string, opt Options) Report {
	t.Helper()
	rep, err := Check(writeTree(t, files), opt)
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func hasFinding(fs []Finding, file, contains string) bool {
	for _, f := range fs {
		if f.File == file && strings.Contains(f.Detail, contains) {
			return true
		}
	}
	return false
}

func TestFixture_CleanTreePasses(t *testing.T) {
	rep := mustCheck(t, map[string]string{
		"a/a.go": "package a\n\n// syscall.Kill in a comment is not a call.\nfunc f(x string) bool { return x == \"kill\" }\n",
	}, Options{})
	if len(rep.Findings) != 0 {
		t.Fatalf("a clean tree reported findings: %v", rep.Findings)
	}
	if rep.ConstructorPresent {
		t.Fatal("no constructor is declared, yet one was reported present")
	}
}

// The plants of rule B: each is a new site the allowlist does not know, and each must fail.
func TestFixture_RuleB_PlantedSites(t *testing.T) {
	plants := []struct {
		name, file, src, primitive string
	}{
		{"syscall.Kill in a new non-test file", "x/new.go",
			"package x\n\nimport \"syscall\"\n\nfunc f() { _ = syscall.Kill(1, 9) }\n", "syscall.Kill"},
		{"process.Pids in a test", "x/new_test.go",
			"package x\n\nimport (\n\t\"testing\"\n\n\t\"github.com/shirou/gopsutil/v4/process\"\n)\n\nfunc TestX(t *testing.T) { _, _ = process.Pids() }\n",
			"gopsutil.Pids"},
		{"process.Pids under an import alias", "x/alias.go",
			"package x\n\nimport ps \"github.com/shirou/gopsutil/v4/process\"\n\nfunc f() { _, _ = ps.Pids() }\n", "gopsutil.Pids"},
		{"exec.Command kill", "x/cmd.go",
			"package x\n\nimport \"os/exec\"\n\nfunc f() { _ = exec.Command(\"kill\", \"-9\", \"1\").Run() }\n", "cmd:kill"},
		{"launchctl in a slice literal", "x/lc.go",
			"package x\n\nfunc f() []string { return []string{\"launchctl\", \"kill\"} }\n", "cmd:launchctl"},
		{"a method Signal on a process", "x/sig.go",
			"package x\n\nimport \"os\"\n\nfunc f(p *os.Process) { _ = p.Signal(os.Interrupt) }\n", ".Signal"},
		{"os.FindProcess", "x/fp.go",
			"package x\n\nimport \"os\"\n\nfunc f() { _, _ = os.FindProcess(1) }\n", "os.FindProcess"},
		{"reading another process's environment", "x/env.go",
			"package x\n\nfunc f() { _, _ = util.GetProcessEnviron(1) }\n", "GetProcessEnviron"},
	}
	for _, p := range plants {
		t.Run(p.name, func(t *testing.T) {
			rep := mustCheck(t, map[string]string{p.file: p.src}, Options{})
			if !hasFinding(rep.ByRule(RuleB), p.file, p.primitive) {
				t.Fatalf("planted %s was not reported: %v", p.primitive, rep.Findings)
			}
		})
	}
}

func TestFixture_RuleB_AllowlistAndStaleEntry(t *testing.T) {
	src := map[string]string{"x/new.go": "package x\n\nimport \"syscall\"\n\nfunc f() { _ = syscall.Kill(1, 9) }\n"}
	allowed := Options{Allowlist: []Allow{{File: "x/new.go", Primitive: "syscall.Kill", Site: "S1", Reason: "fixture", Count: 1}}}
	if rep := mustCheck(t, src, allowed); len(rep.Findings) != 0 {
		t.Fatalf("an allowlisted use was reported: %v", rep.Findings)
	}
	// An extra use in an allowlisted file fails (the review's G14, G15): the entry counts its uses.
	twice := map[string]string{"x/new.go": "package x\n\nimport \"syscall\"\n\nfunc f() {\n\t_ = syscall.Kill(1, 9)\n\t_ = syscall.Kill(2, 9)\n}\n"}
	if rep := mustCheck(t, twice, allowed); !hasFinding(rep.ByRule(RuleB), "x/new.go", "used 2 times here, the allowlist records 1") {
		t.Fatalf("an extra use in an allowlisted file was not reported: %v", rep.Findings)
	}
	stale := Options{Allowlist: []Allow{{File: "x/gone.go", Primitive: "syscall.Kill", Site: "S1", Reason: "fixture"}}}
	rep := mustCheck(t, map[string]string{"x/ok.go": "package x\n"}, stale)
	if !hasFinding(rep.ByRule(RuleB), "x/gone.go", "stale allowlist entry") {
		t.Fatalf("a stale allowlist entry was not reported: %v", rep.Findings)
	}
}

func TestFixture_UnparseableFileIsReported(t *testing.T) {
	rep := mustCheck(t, map[string]string{"x/bad.go": "package x\nfunc ("}, Options{})
	if !hasFinding(rep.ByRule(RuleB), "x/bad.go", "cannot parse") {
		t.Fatalf("an unparseable file was skipped silently: %v", rep.Findings)
	}
}

func TestFixture_RuleC(t *testing.T) {
	bad := "package x\n\nimport \"testing\"\n\nfunc TestReaps(t *testing.T) {\n\tm := &Reg{}\n\tm.PruneOrphans()\n}\n\n" +
		"func TestMonitor(t *testing.T) {\n\tm := &Reg{}\n\tgo m.StartHealthMonitor(nil, 1)\n}\n"
	rep := mustCheck(t, map[string]string{"x/x_test.go": bad}, Options{})
	c := rep.ByRule(RuleC)
	if !hasFinding(c, "x/x_test.go", "TestReaps calls PruneOrphans") || !hasFinding(c, "x/x_test.go", "TestMonitor calls StartHealthMonitor") {
		t.Fatalf("tests that scan the real machine were not named: %v", rep.Findings)
	}
	good := "package x\n\nimport \"testing\"\n\nfunc TestReaps(t *testing.T) {\n\tm := &Reg{procs: &fakeProcessTable{}}\n\tm.PruneOrphans()\n}\n"
	if rep := mustCheck(t, map[string]string{"x/x_test.go": good}, Options{}); len(rep.Findings) != 0 {
		t.Fatalf("a test that builds a fake table was reported: %v", rep.Findings)
	}
	prod := "package x\n\nfunc g(m *Reg) { m.PruneOrphans() }\n"
	if rep := mustCheck(t, map[string]string{"x/prod.go": prod}, Options{}); len(rep.ByRule(RuleC)) != 0 {
		t.Fatalf("production code was held to the test rule: %v", rep.Findings)
	}
}

func TestFixture_ConstructorRules(t *testing.T) {
	// The constructor in these fixtures panics first under test, as rule E requires of any constructor.
	decl := "package client\n\nimport \"testing\"\n\nfunc " + constructor + "() int {\n\tif testing.Testing() {\n\t\tpanic(\"test\")\n\t}\n\treturn 0\n}\n"
	mainOne := "package main\n\nfunc main() { _ = client." + constructor + "() }\n"
	mainTwo := "package main\n\nfunc main() {\n\t_ = client." + constructor + "()\n\t_ = client." + constructor + "()\n}\n"
	other := "package other\n\nfunc g() { _ = client." + constructor + "() }\n"
	testRef := "package client\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { _ = " + constructor + "() }\n"

	t.Run("absent and not required: not yet enforceable, no findings", func(t *testing.T) {
		rep := mustCheck(t, map[string]string{"cmd/mcp-server-magictools/main.go": "package main\n"}, Options{})
		if rep.ConstructorPresent || len(rep.Findings) != 0 {
			t.Fatalf("present=%v findings=%v", rep.ConstructorPresent, rep.Findings)
		}
	})
	t.Run("absent but required fails", func(t *testing.T) {
		rep := mustCheck(t, map[string]string{"internal/client/c.go": "package client\n"}, Options{ConstructorRequired: true})
		if len(rep.ByRule(RuleD)) == 0 {
			t.Fatal("a required constructor that is absent was not reported")
		}
	})
	t.Run("exactly one call in main passes", func(t *testing.T) {
		rep := mustCheck(t, map[string]string{"internal/client/c.go": decl, "cmd/mcp-server-magictools/main.go": mainOne}, Options{})
		if !rep.ConstructorPresent || len(rep.Findings) != 0 {
			t.Fatalf("present=%v findings=%v", rep.ConstructorPresent, rep.Findings)
		}
	})
	t.Run("no call in main fails", func(t *testing.T) {
		rep := mustCheck(t, map[string]string{"internal/client/c.go": decl, "cmd/mcp-server-magictools/main.go": "package main\n"}, Options{})
		if !hasFinding(rep.ByRule(RuleD), "cmd/mcp-server-magictools/main.go", "found 0") {
			t.Fatalf("a missing wiring line was not reported: %v", rep.Findings)
		}
	})
	t.Run("two calls in main fails", func(t *testing.T) {
		rep := mustCheck(t, map[string]string{"internal/client/c.go": decl, "cmd/mcp-server-magictools/main.go": mainTwo}, Options{})
		if !hasFinding(rep.ByRule(RuleD), "cmd/mcp-server-magictools/main.go", "found 2") {
			t.Fatalf("a second call was not reported: %v", rep.Findings)
		}
	})
	t.Run("a call elsewhere in production fails", func(t *testing.T) {
		rep := mustCheck(t, map[string]string{"internal/client/c.go": decl, "cmd/mcp-server-magictools/main.go": mainOne, "internal/other/o.go": other}, Options{})
		if !hasFinding(rep.ByRule(RuleD), "internal/other/o.go", "outside") {
			t.Fatalf("a call outside main was not reported: %v", rep.Findings)
		}
	})
	t.Run("a test naming the constructor fails", func(t *testing.T) {
		rep := mustCheck(t, map[string]string{"internal/client/c.go": decl, "cmd/mcp-server-magictools/main.go": mainOne, "internal/client/c_test.go": testRef}, Options{})
		if !hasFinding(rep.ByRule(RuleA), "internal/client/c_test.go", "real process-table constructor") {
			t.Fatalf("a test naming the constructor was not reported: %v", rep.Findings)
		}
	})
}
