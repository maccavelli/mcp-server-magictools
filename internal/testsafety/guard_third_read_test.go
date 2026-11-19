package testsafety

import "testing"

// Fixtures for the reviewer's third read: rule E2a (the gate's shut branch and Signal's refusal) and rule E2b (the
// real table's package reaches its primitives only through the gate). The sanctioned shape is a miniature of
// internal/proctable; each plant is the reviewer's E2-1 to E2-8, the read-side twin, or a clause of its own.

const (
	scopeTable = "package proctable\n\n" +
		"import (\n\t\"errors\"\n\t\"os\"\n\n\t\"github.com/shirou/gopsutil/v4/process\"\n\n\t\"example.com/m/internal/util\"\n)\n\n" +
		"var errRefused = errors.New(\"refused\")\n\n" +
		"type osProcessTable struct{ armed bool }\n\n" +
		"func gateOpen(t *osProcessTable) bool { return t != nil && t.armed }\n\n" +
		"func (t *osProcessTable) live() bool { return gateOpen(t) }\n\n" +
		"func (t *osProcessTable) Snapshot() error {\n\tif !t.live() {\n\t\treturn errRefused\n\t}\n\t_, _ = process.Processes()\n\t_, _ = startToken(1)\n\treturn nil\n}\n\n" +
		"func (t *osProcessTable) Environ(p int) error {\n\tif !t.live() || p < 2 {\n\t\treturn errRefused\n\t}\n\t_, err := util.GetProcessEnviron(p)\n\treturn err\n}\n\n" +
		"func (t *osProcessTable) Signal(p, s int) error {\n\tif err := validateTarget(p, safePID(os.Getpid()), safePID(os.Getppid())); err != nil {\n\t\treturn err\n\t}\n" +
		"\tif !t.live() || s != 9 {\n\t\treturn errRefused\n\t}\n\treturn signalGroupAndPID(p, s)\n}\n\n" +
		"func validateTarget(p, self, parent int) error { return nil }\n\n" +
		"func safePID(n int) int { return n }\n"
	scopeSignal = "package proctable\n\nimport \"syscall\"\n\n" +
		"func signalGroupAndPID(pid, s int) error {\n\t_ = syscall.Kill(-pid, syscall.Signal(s))\n\treturn syscall.Kill(pid, syscall.Signal(s))\n}\n"
	scopeStart = "package proctable\n\nimport \"os\"\n\n" +
		"func startToken(pid int) (int, error) {\n\t_, err := os.ReadFile(\"/proc/1/stat\")\n\treturn pid, err\n}\n\n" +
		"func pidNamespace() string { return \"\" }\n"
)

func scopeTree(extra map[string]string) map[string]string {
	files := map[string]string{tblPkg + "table.go": scopeTable, tblPkg + "signal_unix.go": scopeSignal, tblPkg + "start_linux.go": scopeStart}
	for k, v := range extra {
		files[k] = v
	}
	return files
}

func TestFixture_RuleE2b_SanctionedShapeIsGreen(t *testing.T) {
	rep := mustCheck(t, scopeTree(nil), Options{})
	if f := append(rep.ByRule(RuleE2), rep.ByRule(RuleE2b)...); len(f) != 0 {
		t.Fatalf("the sanctioned shape was reported: %v", f)
	}
}

func TestFixture_RuleE2aE2b_ThirdReadPlantsAreReported(t *testing.T) {
	pkgHead := "package proctable\n\n"
	plants := []struct {
		id, file, old, src, rule, want string
	}{
		{"E2-1 the shut branch signals before returning", tblPkg + "table.go",
			"func (t *osProcessTable) Snapshot() error {\n\tif !t.live() {\n\t\treturn errRefused",
			"func (t *osProcessTable) Snapshot() error {\n\tif !t.live() {\n\t\t_ = signalGroupAndPID(1, 9)\n\t\treturn errRefused",
			RuleE2, "osProcessTable.Snapshot must begin"},
		{"E2-1 a call in the shut branch's return", tblPkg + "table.go",
			"\tif !t.live() || p < 2 {\n\t\treturn errRefused", "\tif !t.live() || p < 2 {\n\t\treturn sneak(p)",
			RuleE2, "osProcessTable.Environ must begin"},
		{"E2-2 Signal's refusal runs a helper before the gate", tblPkg + "table.go",
			"validateTarget(p, safePID(os.Getpid()), safePID(os.Getppid()))", "validateTarget(p, sneakSelf(p), safePID(os.Getppid()))",
			RuleE2, "osProcessTable.Signal must begin"},
		{"E2-3 a new exported function that signals", tblPkg + "zap.go", "",
			pkgHead + "// Zap is a plant.\nfunc Zap(pid int) error { return signalGroupAndPID(pid, 9) }\n", RuleE2b, "(a) signalGroupAndPID is named outside"},
		{"E2-4 a promoted method of an embedded helper type", tblPkg + "helper.go", "",
			pkgHead + "type helperT struct{}\n\n// Zap is a plant.\nfunc (helperT) Zap(pid int) error { return signalGroupAndPID(pid, 9) }\n", RuleE2b, "(a) signalGroupAndPID is named outside"},
		{"E2-5 an unexported table method reached through a nil receiver", tblPkg + "zap2.go", "",
			pkgHead + "func (t *osProcessTable) zap(pid int) error { return signalGroupAndPID(pid, 9) }\n\n// Zap2 is a plant.\nfunc Zap2(pid int) error {\n\tvar t *osProcessTable\n\treturn t.zap(pid)\n}\n",
			RuleE2b, "(a) signalGroupAndPID is named outside"},
		{"E2-6 a package-level function value", tblPkg + "zap3.go", "",
			pkgHead + "var zapper = func(pid int) error { return signalGroupAndPID(pid, 9) }\n", RuleE2b, "(d) a package-level var holds a function"},
		{"E2-7 init", tblPkg + "zap4.go", "",
			pkgHead + "func init() { _ = signalGroupAndPID(123456, 9) }\n", RuleE2b, "(d) func init"},
		{"E2-8 a deferred signal in the shut branch", tblPkg + "zap5.go", "",
			pkgHead + "// Probe is a plant.\nfunc (t *osProcessTable) Probe(p int) bool {\n\tif !t.live() {\n\t\tdefer func() { _ = signalGroupAndPID(p, 9) }()\n\t\treturn false\n\t}\n\treturn true\n}\n",
			RuleE2, "osProcessTable.Probe must begin"},
		{"read twin: a new exported function reads another process's environment", tblPkg + "peek.go", "",
			pkgHead + "import \"example.com/m/internal/util\"\n\n// Peek is a plant.\nfunc Peek(pid int) ([]string, error) { return util.GetProcessEnviron(pid) }\n",
			RuleE2b, "(c) the reading primitive GetProcessEnviron"},
		{"(b) a signalling primitive in a gated table method", tblPkg + "kick.go", "",
			pkgHead + "import \"syscall\"\n\n// Kick is a plant.\nfunc (t *osProcessTable) Kick(p int) error {\n\tif !t.live() {\n\t\treturn errRefused\n\t}\n\treturn syscall.Kill(p, 9)\n}\n",
			RuleE2b, "(b) the signalling primitive syscall.Kill"},
		{"(c) a token helper named outside a method", tblPkg + "tok.go", "",
			pkgHead + "// Tok is a plant.\nfunc Tok() string { return pidNamespace() }\n", RuleE2b, "(c) pidNamespace is named outside"},
		{"(a) signalGroupAndPID before Signal's gate", tblPkg + "table.go",
			"\tif !t.live() || s != 9 {\n\t\treturn errRefused\n\t}\n\treturn signalGroupAndPID(p, s)",
			"\t_ = signalGroupAndPID(p, s)\n\tif !t.live() || s != 9 {\n\t\treturn errRefused\n\t}\n\treturn nil",
			RuleE2b, "(a) signalGroupAndPID is named outside"},
		{"(a) signalGroupAndPID inside Signal's live-check", tblPkg + "table.go",
			"\tif !t.live() || s != 9 {", "\tif !t.live() || signalGroupAndPID(p, s) != nil {",
			RuleE2b, "(a) signalGroupAndPID is named outside"},
		{"(d) a package function as a value", tblPkg + "zap6.go", "", pkgHead + "var zapper = signalGroupAndPID\n", RuleE2b, "(d) a package-level var holds a function"},
		{"(d) a var of a function type", tblPkg + "zap7.go", "", pkgHead + "var hook func(int) error\n", RuleE2b, "(d) a package-level var of a function type"},
	}
	for _, p := range plants {
		t.Run(p.id, func(t *testing.T) {
			var files map[string]string
			if p.old == "" {
				files = scopeTree(map[string]string{p.file: p.src})
			} else {
				files = scopeTree(nil)
				files[p.file] = replaceOnce(t, files[p.file], p.old, p.src)
			}
			rep := mustCheck(t, files, Options{})
			if !hasFinding(rep.ByRule(p.rule), p.file, p.want) {
				t.Fatalf("plant not reported by rule %s (want %q): %v", p.rule, p.want, rep.Findings)
			}
		})
	}
	// A test file of the package is not held to E2b: its rules are A3 and A4.
	rep := mustCheck(t, scopeTree(map[string]string{tblPkg + "x_test.go": pkgHead + "func init() {}\n\nvar f = func() {}\n"}), Options{})
	if f := rep.ByRule(RuleE2b); len(f) != 0 {
		t.Fatalf("a test file was held to rule E2b: %v", f)
	}
}
