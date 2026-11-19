package testsafety

import "testing"

// Fixtures for the guard's hardening after the review's second look (F1, F2) and the owner's ruling on the armed gate
// (rule E2). Each of the second look's bypasses N01 to N14 and N18 is planted in a temporary tree and must be reported
// by the rule named; each sanctioned form must stay green.

func TestFixture_SecondLookBypassesAreReported(t *testing.T) {
	hp := "package handler\n\nimport (\n\t\"testing\"\n%s)\n\nfunc TestPlant(t *testing.T) {\n%s\n}\n"
	ptHead := "package proctable\n\nimport (\n%s\t\"testing\"\n\n\t\"example.com/m/internal/client\"\n)\n\n"
	sp := func(f string, a ...any) string { return sprintf(f, a...) }
	plants := []struct {
		id, file, src, rule, want string
	}{
		{"N01 method value of os.Process.Signal", "h/p_test.go", sp(hp, "\t\"os\"\n", "\tvar p *os.Process\n\tf := p.Signal\n\t_ = f(os.Kill)"), RuleB, `".Signal"`},
		{"N02 command name held in a variable", "h/p_test.go", sp(hp, "\t\"os/exec\"\n", "\tname := \"kill\"\n\t_ = exec.Command(name, \"-9\", \"123456\").Run()"), RuleB, `"test-exec-nonliteral"`},
		{"N03 bash -lc with kill inside", "h/p_test.go", sp(hp, "\t\"os/exec\"\n", "\t_ = exec.Command(\"bash\", \"-lc\", \"kill -9 123456\").Run()"), RuleB, `"cmd:kill"`},
		{"N04 ps eww reads every process's environment", "h/p_test.go", sp(hp, "\t\"os/exec\"\n", "\t_, _ = exec.Command(\"ps\", \"eww\", \"-A\").Output()"), RuleB, `"cmd:ps-environ"`},
		{"N05 x/sys/windows OpenProcess", "h/p_windows_test.go", sp(hp, "\t\"golang.org/x/sys/windows\"\n", "\th, _ := windows.OpenProcess(windows.PROCESS_TERMINATE, false, 123456)\n\t_ = windows.TerminateProcess(h, 1)"), RuleB, `"windows.OpenProcess"`},
		{"N05 x/sys/windows TerminateProcess", "h/p_windows_test.go", sp(hp, "\t\"golang.org/x/sys/windows\"\n", "\th, _ := windows.OpenProcess(windows.PROCESS_TERMINATE, false, 123456)\n\t_ = windows.TerminateProcess(h, 1)"), RuleB, `"windows.TerminateProcess"`},
		{"N06 syscall.TerminateProcess", "h/p_windows_test.go", sp(hp, "\t\"syscall\"\n", "\th, _ := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, 123456)\n\t_ = syscall.TerminateProcess(h, 1)"), RuleB, `"syscall.TerminateProcess"`},
		{"N06 syscall.OpenProcess", "h/p_windows_test.go", sp(hp, "\t\"syscall\"\n", "\th, _ := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, 123456)\n\t_ = syscall.TerminateProcess(h, 1)"), RuleB, `"syscall.OpenProcess"`},
		{"N07 a cmd test calls killStaleProcess", "cmd/x/p_test.go", "package main\n\nimport \"testing\"\n\nfunc TestPlant(t *testing.T) { killStaleProcess(123456) }\n", RuleB, `"test-call:killStaleProcess"`},
		{"N07 a cmd test calls waitForProcessExit", "cmd/x/p_test.go", "package main\n\nimport \"testing\"\n\nfunc TestPlant(t *testing.T) { waitForProcessExit(123456, 1) }\n", RuleB, `"test-call:waitForProcessExit"`},
		{"N08 a cmd test calls signalOrWarn", "cmd/x/p_test.go", "package main\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestPlant(t *testing.T) { signalOrWarn(&os.Process{Pid: 123456}, os.Kill) }\n", RuleB, `"test-call:signalOrWarn"`},
		{"N09 a further childOnlyTable method", "internal/proctable/p_test.go", sp(ptHead, "") +
			"func (c *childOnlyTable) Zap(p client.ProcInfo) error { return c.inner.Signal(p, client.SigKill) }\n", RuleA4, "has the method Zap beyond"},
		{"N10 reflect in a proctable test", "internal/proctable/p_test.go", sp(ptHead, "\t\"reflect\"\n\t\"unsafe\"\n") + "var _ = reflect.TypeOf(unsafe.Pointer(nil))\n", RuleA4, "imports reflect"},
		{"N10 unsafe in a proctable test", "internal/proctable/p_test.go", sp(ptHead, "\t\"reflect\"\n\t\"unsafe\"\n") + "var _ = reflect.TypeOf(unsafe.Pointer(nil))\n", RuleA4, "imports unsafe"},
		{"N11 a look-alike struct naming the real type", "internal/proctable/p_test.go", sp(ptHead, "\t\"os/exec\"\n") +
			"type sneak struct {\n\tx *osProcessTable\n\tc *exec.Cmd\n}\n", RuleA3, "childOnlyTable.inner"},
		{"N12 a /proc path built by concatenation", "h/p_test.go", sp(hp, "\t\"os\"\n", "\t_, _ = os.ReadFile(\"/pro\" + \"c/1/environ\")"), RuleB, `"procfs"`},
		{"N12 a /proc path built from same-file constants", "h/p.go", "package h\n\nimport \"os\"\n\nconst a = \"/pro\"\n\nfunc f(pid string) { _, _ = os.ReadFile(a + \"c/\" + pid) }\n", RuleB, `"procfs"`},
		{"N13 method expression (*os.Process).Kill", "h/p_test.go", sp(hp, "\t\"os\"\n", "\tvar p *os.Process\n\t_ = (*os.Process).Kill(p)"), RuleB, `".Kill"`},
		{"N13 method expression as a value", "h/p_test.go", sp(hp, "\t\"os\"\n", "\tk := (*os.Process).Kill\n\t_ = k"), RuleB, `".Kill"`},
		{"N14 gopsutil Terminate", "h/p_test.go", sp(hp, "\tgp \"github.com/shirou/gopsutil/v4/process\"\n", "\tvar p *gp.Process\n\t_ = p.Terminate()\n\t_ = p.SendSignal(9)\n\t_ = p.Suspend()"), RuleB, `".Terminate"`},
		{"N14 gopsutil SendSignal", "h/p_test.go", sp(hp, "\tgp \"github.com/shirou/gopsutil/v4/process\"\n", "\tvar p *gp.Process\n\t_ = p.Terminate()\n\t_ = p.SendSignal(9)\n\t_ = p.Suspend()"), RuleB, `".SendSignal"`},
		{"N14 gopsutil Suspend", "h/p_test.go", sp(hp, "\tgp \"github.com/shirou/gopsutil/v4/process\"\n", "\tvar p *gp.Process\n\t_ = p.Terminate()\n\t_ = p.SendSignal(9)\n\t_ = p.Suspend()"), RuleB, `".Suspend"`},
		{"N18 a program name in a package-level variable", "h/p_test.go", "package h\n\nimport (\n\t\"os/exec\"\n\t\"testing\"\n)\n\nvar killer = \"/usr/bin/pkill\"\n\nfunc TestPlant(t *testing.T) { _ = exec.Command(killer, \"mcp\").Run() }\n", RuleB, `"test-exec-nonliteral"`},
		{"a command word in a constant", "h/p.go", "package h\n\nimport \"os/exec\"\n\nconst k = \"pkill\"\n\nfunc f() { _ = exec.Command(k, \"x\").Run() }\n", RuleB, `"cmd:pkill"`},
		{"ps -E inside a shell string", "h/p.go", "package h\n\nimport \"os/exec\"\n\nfunc f() { _, _ = exec.Command(\"sh\", \"-ec\", \"ps -E -ww -o pid=\").Output() }\n", RuleB, `"cmd:ps-environ"`},
		{"a dot-import of os/exec", "h/p_test.go", sp(hp, "\t. \"os/exec\"\n", "\t_ = Command"), RuleB, `"dot-import:os/exec"`},
	}
	for _, p := range plants {
		t.Run(p.id, func(t *testing.T) {
			files := map[string]string{p.file: p.src}
			if p.rule == RuleA4 || p.rule == RuleA3 {
				files["internal/proctable/childonly_test.go"] = childOnlyGood
			}
			rep := mustCheck(t, files, Options{})
			if !hasFinding(rep.ByRule(p.rule), p.file, p.want) {
				t.Fatalf("plant not reported by rule %s (want %q): %v", p.rule, p.want, rep.Findings)
			}
		})
	}
}

func TestFixture_SecondLookSanctionedFormsStayGreen(t *testing.T) {
	clean := map[string]string{
		"h/c_test.go": "package h\n\nimport (\n\t\"os\"\n\t\"os/exec\"\n\t\"syscall\"\n\t\"testing\"\n)\n\n" +
			"const tool = \"git\"\n\n" +
			"// ps eww, bash -lc \"kill\" and \"/pro\" + \"c\" in a comment are not uses.\n" +
			"func TestC(t *testing.T) {\n" +
			"\t_ = exec.Command(\"git\", \"status\").Run()\n" +
			"\t_ = exec.Command(tool, \"log\").Run()\n" +
			"\t_ = exec.Command(\"sh\", \"-c\", \"echo ok\").Run()\n" +
			"\t_, _ = exec.Command(\"ps\", \"-o\", \"pid=\", \"-p\", \"1\").Output()\n" +
			"\t_ = \"/pro\" + \"cess\"\n" +
			"\t_ = os.Kill\n" +
			"\t_ = syscall.Signal(0)\n" +
			"}\n",
	}
	if rep := mustCheck(t, clean, Options{}); len(rep.Findings) != 0 {
		t.Fatalf("sanctioned forms were reported: %v", rep.Findings)
	}
}

func TestFixture_RuleA3_OnlyTheInnerFieldNamesTheType(t *testing.T) {
	plants := map[string]string{
		"an alias of the type":                      "package proctable\n\ntype sneak = osProcessTable\n",
		"a second field of the type in the wrapper": "package proctable\n\nimport \"os/exec\"\n\ntype childOnlyTable struct {\n\tinner *osProcessTable\n\tspare *osProcessTable\n\tcmd   *exec.Cmd\n}\n",
		"the field by value, not by pointer":        "package proctable\n\ntype childOnlyTable struct{ inner osProcessTable }\n",
		"a slice of the type":                       "package proctable\n\ntype many []*osProcessTable\n",
	}
	for name, src := range plants {
		t.Run(name, func(t *testing.T) {
			rep := mustCheck(t, map[string]string{tblPkg + "t.go": tblDecl, tblPkg + "p_test.go": src}, Options{})
			if !hasFinding(rep.ByRule(RuleA3), tblPkg+"p_test.go", "childOnlyTable.inner") {
				t.Fatalf("not reported: %v", rep.Findings)
			}
		})
	}
}

// gateGood is the sanctioned shape of rule E2, in miniature.
const gateGood = "package proctable\n\n" +
	"type osProcessTable struct{ armed bool }\n\n" +
	"func gateOpen(t *osProcessTable) bool { return t != nil && t.armed }\n\n" +
	"func (t *osProcessTable) live() bool { return gateOpen(t) }\n\n" +
	"func (t *osProcessTable) Snapshot() error {\n\tif !t.live() {\n\t\treturn nil\n\t}\n\treturn nil\n}\n\n" +
	"func (t *osProcessTable) Exe(p int) error {\n\tif !t.live() || p < 0 {\n\t\treturn nil\n\t}\n\treturn nil\n}\n\n" +
	"func (t *osProcessTable) Signal(p, s int) error {\n\tif err := validateTarget(p, safePID(os.Getpid()), safePID(os.Getppid())); err != nil {\n\t\treturn err\n\t}\n\tif !t.live() || s != 9 {\n\t\treturn nil\n\t}\n\treturn nil\n}\n\n" +
	"func (t *osProcessTable) same(p int) bool { return t.live() && p > 0 }\n\n" +
	"func validateTarget(p, a, b int) error { return nil }\n"

func TestFixture_RuleE2_EveryMethodChecksTheGateFirst(t *testing.T) {
	if rep := mustCheck(t, map[string]string{tblPkg + "t.go": gateGood}, Options{}); len(rep.ByRule(RuleE2)) != 0 {
		t.Fatalf("the sanctioned shape was reported: %v", rep.ByRule(RuleE2))
	}
	plants := map[string]struct{ old, new, want string }{
		"a method without the check": {"func (t *osProcessTable) Exe(p int) error {\n\tif !t.live() || p < 0 {\n\t\treturn nil\n\t}\n",
			"func (t *osProcessTable) Exe(p int) error {\n", "osProcessTable.Exe must begin"},
		"the check after another statement": {"func (t *osProcessTable) Snapshot() error {\n\tif !t.live() {",
			"func (t *osProcessTable) Snapshot() error {\n\t_ = 1\n\tif !t.live() {", "osProcessTable.Snapshot must begin"},
		"the check that falls through": {"func (t *osProcessTable) Snapshot() error {\n\tif !t.live() {\n\t\treturn nil\n\t}",
			"func (t *osProcessTable) Snapshot() error {\n\tif !t.live() {\n\t\t_ = 1\n\t}", "osProcessTable.Snapshot must begin"},
		"the check not leftmost": {"if !t.live() || p < 0 {", "if p < 0 || !t.live() {", "osProcessTable.Exe must begin"},
		"the check not negated":  {"if !t.live() || p < 0 {", "if t.live() || p < 0 {", "osProcessTable.Exe must begin"},
		"Signal's first statement something other than the target refusal": {"\tif err := validateTarget(p, safePID(os.Getpid()), safePID(os.Getppid())); err != nil {\n\t\treturn err\n\t}\n\tif !t.live() || s != 9 {",
			"\tif err := sneak(p); err != nil {\n\t\treturn err\n\t}\n\tif !t.live() || s != 9 {", "osProcessTable.Signal must begin"},
		"live not the pure gate": {"func (t *osProcessTable) live() bool { return gateOpen(t) }", "func (t *osProcessTable) live() bool { return true }",
			"osProcessTable.live must be exactly"},
		"live checking another value": {"func (t *osProcessTable) live() bool { return gateOpen(t) }",
			"func (t *osProcessTable) live() bool { return gateOpen(nil) }", "osProcessTable.live must be exactly"},
		"an unnamed receiver": {"func (t *osProcessTable) Snapshot() error {\n\tif !t.live() {",
			"func (*osProcessTable) Snapshot() error {\n\tif !t.live() {", "osProcessTable.Snapshot must begin"},
	}
	for name, p := range plants {
		t.Run(name, func(t *testing.T) {
			src := replaceOnce(t, gateGood, p.old, p.new)
			rep := mustCheck(t, map[string]string{tblPkg + "t.go": src}, Options{})
			if !hasFinding(rep.ByRule(RuleE2), tblPkg+"t.go", p.want) {
				t.Fatalf("not reported (want %q): %v", p.want, rep.Findings)
			}
		})
	}
	// A test file is not held to E2: the child-only table's methods are rule A4's.
	if rep := mustCheck(t, map[string]string{tblPkg + "t_test.go": "package proctable\n\nfunc (t *osProcessTable) Zap() {}\n"}, Options{}); len(rep.ByRule(RuleE2)) != 0 {
		t.Fatalf("a test file was held to rule E2: %v", rep.ByRule(RuleE2))
	}
}
