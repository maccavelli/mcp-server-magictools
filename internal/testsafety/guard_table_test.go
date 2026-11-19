package testsafety

import (
	"strings"
	"testing"
)

// Fixtures for rules A2, A3, E and rule B's two P3 primitives. Each plant is a temporary tree; each must be red,
// and each sanctioned form must be green.

const (
	tblPkg  = "internal/proctable/"
	tblDecl = "package proctable\n\nimport \"testing\"\n\ntype osProcessTable struct{ armed bool }\n\n" +
		"func NewOSProcessTable() *osProcessTable {\n\tif testing.Testing() {\n\t\tpanic(\"no\")\n\t}\n\treturn &osProcessTable{armed: true}\n}\n"
	tblChildOnly = "package proctable\n\nimport \"testing\"\n\ntype childOnlyTable struct{ inner *osProcessTable }\n\n" +
		"func newChildOnlyTable(t *testing.T) *childOnlyTable {\n\treturn &childOnlyTable{inner: &osProcessTable{armed: true}}\n}\n"
)

func TestFixture_RuleA2_ImportOfTheRealTable(t *testing.T) {
	plants := map[string]string{
		"internal/handler/h_test.go": "package handler\n\nimport _ \"example.com/m/internal/proctable\"\n",
		"internal/client/c_test.go":  "package client_test\n\nimport pt \"example.com/m/internal/proctable\"\n\nvar _ = pt.X\n",
	}
	for file, src := range plants {
		rep := mustCheck(t, map[string]string{tblPkg + "t.go": tblDecl, file: src}, Options{})
		if !hasFinding(rep.ByRule(RuleA2), file, "imports the real process table") {
			t.Errorf("%s: an import of the real table from a test was not reported: %v", file, rep.Findings)
		}
	}
	ok := map[string]string{
		tblPkg + "t.go":            tblDecl,
		tblPkg + "x_test.go":       "package proctable\n\nimport _ \"example.com/m/internal/proctable/sub\"\n",
		"cmd/app/main.go":          "package main\n\nimport _ \"example.com/m/internal/proctable\"\n",
		"internal/other/o_test.go": "package other\n\nimport _ \"example.com/m/internal/proctablex\"\n",
	}
	if rep := mustCheck(t, ok, Options{}); len(rep.ByRule(RuleA2)) != 0 {
		t.Fatalf("sanctioned imports were reported: %v", rep.ByRule(RuleA2))
	}
}

func TestFixture_RuleA3_ConstructionOfTheRealTable(t *testing.T) {
	green := mustCheck(t, map[string]string{tblPkg + "t.go": tblDecl, tblPkg + "childonly_test.go": tblChildOnly}, Options{})
	if f := green.ByRule(RuleA3); len(f) != 0 {
		t.Fatalf("the two sanctioned constructors were reported: %v", f)
	}
	plants := []struct{ name, file, src, want string }{
		{"literal in a test outside the child-only constructor", tblPkg + "bad_test.go",
			"package proctable\n\nfunc helper() any { return &osProcessTable{armed: true} }\n", "outside newChildOnlyTable"},
		{"zero value declared in a test", tblPkg + "bad_test.go",
			"package proctable\n\nvar z osProcessTable\n", "outside newChildOnlyTable"},
		{"conversion in a test", tblPkg + "bad_test.go",
			"package proctable\n\nfunc f() { _ = (*osProcessTable)(nil) }\n", "outside newChildOnlyTable"},
		{"method declared on the type in a test", tblPkg + "bad_test.go",
			"package proctable\n\nfunc (t *osProcessTable) Sneak() {}\n", "outside newChildOnlyTable"},
		{"literal in non-test code outside the constructor", tblPkg + "other.go",
			"package proctable\n\nfunc other() *osProcessTable { return &osProcessTable{armed: true} }\n", "outside NewOSProcessTable"},
		{"new() in non-test code", tblPkg + "other.go",
			"package proctable\n\nfunc other() *osProcessTable { return new(osProcessTable) }\n", "new(osProcessTable)"},
		{"child-only constructor declared in non-test code", tblPkg + "other.go",
			"package proctable\n\nfunc newChildOnlyTable() {}\n", "declared in a non-test file"},
		{"child-only constructor named by non-test code elsewhere", "internal/client/c.go",
			"package client\n\nvar _ = newChildOnlyTable\n", "test-only child-scoped table"},
	}
	for _, p := range plants {
		t.Run(p.name, func(t *testing.T) {
			rep := mustCheck(t, map[string]string{tblPkg + "t.go": tblDecl, tblPkg + "childonly_test.go": tblChildOnly, p.file: p.src}, Options{})
			if !hasFinding(rep.ByRule(RuleA3), p.file, p.want) {
				t.Fatalf("plant not reported (want %q in %s): %v", p.want, p.file, rep.Findings)
			}
		})
	}
}

func TestFixture_RuleE_ConstructorPanicsFirst(t *testing.T) {
	if rep := mustCheck(t, map[string]string{tblPkg + "t.go": tblDecl}, Options{}); len(rep.ByRule(RuleE)) != 0 {
		t.Fatalf("a constructor that panics first was reported: %v", rep.ByRule(RuleE))
	}
	head := "package proctable\n\nimport \"testing\"\n\ntype osProcessTable struct{ armed bool }\n\nfunc scan() {}\n\n"
	plants := map[string]string{
		"the panic after another call": head + "func NewOSProcessTable() *osProcessTable {\n\tscan()\n\tif testing.Testing() {\n\t\tpanic(\"no\")\n\t}\n\treturn &osProcessTable{armed: true}\n}\n",
		"no if at all":                 head + "func NewOSProcessTable() *osProcessTable {\n\treturn &osProcessTable{armed: true}\n}\n",
		"the if without a panic":       head + "func NewOSProcessTable() *osProcessTable {\n\tif testing.Testing() {\n\t\treturn nil\n\t}\n\treturn &osProcessTable{armed: true}\n}\n",
		"another condition":            head + "func NewOSProcessTable() *osProcessTable {\n\tif testing.Short() {\n\t\tpanic(\"no\")\n\t}\n\treturn &osProcessTable{armed: true}\n}\n",
		"a negated condition":          head + "func NewOSProcessTable() *osProcessTable {\n\tif !testing.Testing() {\n\t\tpanic(\"no\")\n\t}\n\treturn &osProcessTable{armed: true}\n}\n",
		"testing not imported":         strings.Replace(head, "import \"testing\"\n\n", "", 1) + "func NewOSProcessTable() *osProcessTable {\n\treturn &osProcessTable{armed: true}\n}\n",
	}
	for name, src := range plants {
		t.Run(name, func(t *testing.T) {
			rep := mustCheck(t, map[string]string{tblPkg + "t.go": src}, Options{})
			if !hasFinding(rep.ByRule(RuleE), tblPkg+"t.go", "first statement") {
				t.Fatalf("not reported: %v", rep.Findings)
			}
		})
	}
}

func TestFixture_RuleB_LinknameAndProcfs(t *testing.T) {
	plants := []struct{ name, file, src, primitive string }{
		{"a linkname directive in a test", "x/l_test.go",
			"package x\n\nimport _ \"unsafe\"\n\n//go:linkname sneak example.com/m/internal/proctable.startToken\nfunc sneak(int32) (uint64, error)\n", primLinkname},
		{"a /proc literal in a new file", "x/p.go",
			"package x\n\nimport \"os\"\n\nfunc f() { _, _ = os.ReadFile(\"/proc/1/environ\") }\n", primProcfs},
	}
	for _, p := range plants {
		t.Run(p.name, func(t *testing.T) {
			rep := mustCheck(t, map[string]string{p.file: p.src}, Options{})
			if !hasFinding(rep.ByRule(RuleB), p.file, p.primitive) {
				t.Fatalf("planted %s was not reported: %v", p.primitive, rep.Findings)
			}
		})
	}
	clean := map[string]string{"x/c.go": "package x\n\n// a comment that mentions /proc/1/stat and go:linkname is not a use.\nfunc f() string { return \"proc/\" + \"x\" }\n"}
	if rep := mustCheck(t, clean, Options{}); len(rep.Findings) != 0 {
		t.Fatalf("a comment or a non-prefixed literal was reported: %v", rep.Findings)
	}
}
