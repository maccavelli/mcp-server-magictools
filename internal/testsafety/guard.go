// Package testsafety is a parse-only guard: it reads this module's Go source with go/parser and reports where a test
// could enumerate or signal a real process, or where a new site that does so has appeared unclassified.
//
// It imports only the standard library, and in particular nothing from this module, so running its tests cannot
// start a process or scan one. Keep it that way: a guard that links the code it guards can run that code.
//
// The rules (MADR 0010-MADR-make-tests-unable-to-signal-real-processes.md, "What A+G is, precisely", item 6):
//
//	A  no _test.go names the real process-table constructor
//	B  every signalling or enumerating primitive is on an allowlist of the known sites, so a new site fails
//	C  a test that calls the reaper or its entry points builds a fake process table in the same function
//	D  the constructor is called from non-test code at exactly one line, in cmd/mcp-server-magictools/main.go
//
// Added by the design phase (P3; the MADR's "Design as approved", D2):
//
//	A2 no _test.go outside internal/proctable imports internal/proctable
//	A3 in internal/proctable the real table's type is built only by the two sanctioned constructors: a composite
//	   literal of it appears only in NewOSProcessTable (non-test) or newChildOnlyTable (a _test.go); new() of it never;
//	   a test file names the type only inside newChildOnlyTable or as the type of childOnlyTable's field inner (the
//	   review's second look, F1); newChildOnlyTable is declared only in a _test.go and named by no non-test file
//	E  the first statement of NewOSProcessTable is `if testing.Testing() { ... panic(...) ... }`
//	B  also: a //go:linkname directive, and a string literal beginning "/proc/", are primitives
//
// Added after the independent review (P8) and its second look: rule A4, the discipline of internal/proctable's own
// tests (guard_childonly.go); rule E2, every exported method of the real table begins by checking its gate
// (guard_gate.go); and rule B's further primitives (guard_primitives.go, guard_strings.go).
//
// Rules A and D refer to a constructor that does not exist until the seam is built. Until then Report.ConstructorPresent
// is false and the tests report them as "not yet enforceable: constructor absent", a skip and never a pass. Once the
// constructor exists they are enforced automatically; ConstructorRequired then makes its removal a failure too.
package testsafety

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Rule identifiers carried by a Finding.
const (
	RuleA  = "A"
	RuleB  = "B"
	RuleC  = "C"
	RuleD  = "D"
	RuleA2 = "A2"
	RuleA3 = "A3"
	RuleE  = "E"
)

// constructor is the name of the exported function that builds the real process table. P3 of the plan fixes the
// final name; this is the one place to change it.
const constructor = "NewOSProcessTable"

// constructorRequired is flipped to true by P4, the phase that builds the constructor. From then on an absent
// constructor is a failure and not a skip. Set by P4a step 3 (2026-11-18), when the wiring line landed.
const constructorRequired = true

// The real table's package, its unexported type and the one constructor its own tests may use (P3, D2).
const (
	osTablePkg    = "internal/proctable"
	osTableType   = "osProcessTable"
	childOnlyCtor = "newChildOnlyTable"
)

// mainFile is the one file allowed to call the constructor from non-test code.
const mainFile = "cmd/mcp-server-magictools/main.go"

// gopsutilProcessSuffix identifies the gopsutil process package by import path.
const (
	gopsutilPrefix = "github.com/shirou/gopsutil/"
	gopsutilSuffix = "/process"
)

// Primitive names, as the allowlist spells them. A leading dot is a method, "cmd:" a command word given to a program
// launcher, "test-call:" a function that only tests are held to.
const (
	primSyscallKill       = "syscall.Kill"
	primUnixKill          = "unix.Kill"
	primSysctlRaw         = "unix.SysctlRaw"
	primFindProcess       = "os.FindProcess"
	primGopsutilPrefix    = "gopsutil."
	primGopsutilPids      = primGopsutilPrefix + "Pids"
	primGopsutilNewProc   = primGopsutilPrefix + "NewProcess"
	primGopsutilProcs     = primGopsutilPrefix + "Processes"
	primEnviron           = ".Environ"
	primSignal            = ".Signal"
	primKill              = ".Kill"
	primCmdTaskkill       = "cmd:taskkill"
	primCmdSystemctl      = "cmd:systemctl"
	primCmdLaunchctl      = "cmd:launchctl"
	primCmdKill           = "cmd:kill"
	primTestCleanupStale  = "test-call:cleanupStaleProcess"
	primLinkname          = "directive:linkname"
	primProcfs            = "procfs"
	methodKill            = "Kill"
	methodEnviron         = "Environ"
	funcGetProcessEnviron = "GetProcessEnviron"
	funcCleanupStale      = "cleanupStaleProcess"
)

// fakePrefixes name the identifiers whose presence in a test function means it builds a fake process table.
var fakePrefixes = []string{"fakeProcessTable", "newFakeProcessTable"}

// seamCalls are the entry points that scan or signal processes through the registry; a test that calls one must
// build a fake process table in the same function.
var seamCalls = map[string]bool{
	"PruneOrphans":          true,
	"StartHealthMonitor":    true,
	"enforceSingleInstance": true,
	"lookupSystemProcesses": true,
	"harvestProcess":        true,
	"InitialProcessSweep":   true,
	"AttachProcessTable":    true, // P3: a test attaches a fake table, never another
}

// cmdWords are command names that, as a call argument or a slice element, mean a program is being asked to signal
// or control a process.
var cmdWords = map[string]bool{
	"kill":      true,
	"pkill":     true,
	"killall":   true,
	"taskkill":  true,
	"launchctl": true,
	"systemctl": true,
}

// gopsutilCalls are the gopsutil process-package functions that enumerate or open another process.
var gopsutilCalls = map[string]bool{
	"Pids": true, "PidsWithContext": true, "Processes": true, "ProcessesWithContext": true,
	"NewProcess": true, "NewProcessWithContext": true,
}

// Allow classifies one known site: a primitive used in a file, and which inventory site it belongs to.
type Allow struct {
	File      string // slash-separated path relative to the module root
	Primitive string
	Site      string // S1 to S8 of the MADR's inventory, or "probe" or "own-child"
	Reason    string
	Count     int // the number of uses of Primitive in File; any other number fails
}

// Options configures Check.
type Options struct {
	Allowlist           []Allow
	ConstructorRequired bool
}

// DefaultOptions is what the module's own test uses.
func DefaultOptions() Options {
	return Options{Allowlist: defaultAllowlist, ConstructorRequired: constructorRequired}
}

// Finding is one violation.
type Finding struct {
	Rule   string
	File   string
	Line   int
	Detail string
}

func (f Finding) String() string {
	loc := f.File
	if f.Line > 0 {
		loc = fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	return fmt.Sprintf("rule %s: %s: %s", f.Rule, loc, f.Detail)
}

// Report is the result of a Check.
type Report struct {
	Findings           []Finding
	ConstructorPresent bool
}

// ByRule returns the findings of one rule.
func (r Report) ByRule(rule string) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

type srcFile struct {
	rel    string
	isTest bool
	fset   *token.FileSet
	ast    *ast.File
}

// Check parses every Go file under root (build constraints are ignored, so the Windows twins are read) and applies
// the rules. It runs nothing.
func Check(root string, opt Options) (Report, error) {
	files, parseFindings, err := parseTree(root)
	if err != nil {
		return Report{}, err
	}
	rep := Report{Findings: parseFindings}
	rep.Findings = append(rep.Findings, checkPrimitives(files, opt.Allowlist)...)
	rep.Findings = append(rep.Findings, checkSeamCalls(files)...)
	present, ctorFindings := checkConstructor(files, opt.ConstructorRequired)
	rep.ConstructorPresent = present
	rep.Findings = append(rep.Findings, ctorFindings...)
	rep.Findings = append(rep.Findings, checkTableImports(files)...)
	rep.Findings = append(rep.Findings, checkTableConstruction(files)...)
	rep.Findings = append(rep.Findings, checkConstructorGuard(files)...)
	rep.Findings = append(rep.Findings, checkChildOnlyDiscipline(files)...)
	rep.Findings = append(rep.Findings, checkGateClauses(files)...)
	rep.Findings = append(rep.Findings, checkTableScope(files)...)
	sort.SliceStable(rep.Findings, func(i, j int) bool {
		a, b := rep.Findings[i], rep.Findings[j]
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return rep, nil
}

func skipDir(rel, name string) bool {
	if rel == "." {
		return false
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return true
	}
	switch name {
	case "dist", "vendor", "node_modules", "testdata":
		return true
	}
	return rel == "internal/forks"
}

func parseTree(root string) ([]srcFile, []Finding, error) {
	var files []srcFile
	var findings []Finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDir(rel, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") {
			return nil
		}
		fset := token.NewFileSet()
		// Comments are parsed so that a //go:linkname directive is seen (P3, rule B).
		f, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution|parser.ParseComments)
		if perr != nil {
			findings = append(findings, Finding{Rule: RuleB, File: rel, Detail: "cannot parse: " + perr.Error()})
			return nil //nolint:nilerr // a parse error is reported as a finding and the walk goes on
		}
		files = append(files, srcFile{rel: rel, isTest: strings.HasSuffix(rel, "_test.go"), fset: fset, ast: f})
		return nil
	})
	return files, findings, err
}

func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

func mentionsFake(fd *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fd, func(n ast.Node) bool {
		var name string
		switch v := n.(type) {
		case *ast.Ident:
			name = v.Name
		case *ast.SelectorExpr:
			name = v.Sel.Name
		default:
			return true
		}
		for _, p := range fakePrefixes {
			if strings.HasPrefix(name, p) {
				found = true
			}
		}
		return !found
	})
	return found
}

func checkSeamCalls(files []srcFile) []Finding {
	var out []Finding
	for _, sf := range files {
		if !sf.isTest {
			continue
		}
		for _, decl := range sf.ast.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			var calls []hit
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && seamCalls[calleeName(call)] {
					calls = append(calls, hit{primitive: calleeName(call), line: sf.fset.Position(call.Pos()).Line})
				}
				return true
			})
			if len(calls) == 0 || mentionsFake(fd) {
				continue
			}
			for _, c := range calls {
				out = append(out, Finding{Rule: RuleC, File: sf.rel, Line: c.line,
					Detail: fmt.Sprintf("%s calls %s without building a fake process table in the same function: it would scan or signal the real machine", fd.Name.Name, c.primitive)})
			}
		}
	}
	return out
}

func checkConstructor(files []srcFile, required bool) (bool, []Finding) {
	present := false
	var out []Finding
	var prodCalls []Finding
	testLines := map[string]bool{}
	for _, sf := range files {
		declNames := map[token.Pos]bool{}
		for _, decl := range sf.ast.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == constructor && fd.Recv == nil {
				declNames[fd.Name.Pos()] = true
				if !sf.isTest {
					present = true
				}
			}
		}
		ast.Inspect(sf.ast, func(n ast.Node) bool {
			var name string
			var pos token.Pos
			switch v := n.(type) {
			case *ast.SelectorExpr:
				name, pos = v.Sel.Name, v.Pos()
			case *ast.Ident:
				name, pos = v.Name, v.Pos()
			default:
				return true
			}
			if name != constructor {
				return true
			}
			line := sf.fset.Position(pos).Line
			if sf.isTest {
				if testLines[sf.rel+":"+strconv.Itoa(line)] {
					return true // a selector and its Sel identifier are one reference
				}
				testLines[sf.rel+":"+strconv.Itoa(line)] = true
				out = append(out, Finding{Rule: RuleA, File: sf.rel, Line: line,
					Detail: "a test names the real process-table constructor; tests use a fake table"})
			} else if id, ok := n.(*ast.Ident); ok && !declNames[id.Pos()] {
				// Every reference counts, a call or a function value (the review's G16): the Sel identifier of a
				// selector is visited as an identifier, so each reference is counted once.
				prodCalls = append(prodCalls, Finding{Rule: RuleD, File: sf.rel, Line: line})
			}
			return true
		})
	}
	if !present {
		if required {
			out = append(out, Finding{Rule: RuleD, Detail: "the real process-table constructor is required and absent: " + constructor})
		}
		return false, out
	}
	return true, append(out, constructorCallFindings(prodCalls)...)
}

func constructorCallFindings(calls []Finding) []Finding {
	var out []Finding
	inMain := 0
	for _, c := range calls {
		if c.File == mainFile {
			inMain++
			continue
		}
		c.Detail = "the real process-table constructor is referenced from non-test code outside " + mainFile
		out = append(out, c)
	}
	if inMain != 1 {
		out = append(out, Finding{Rule: RuleD, File: mainFile,
			Detail: fmt.Sprintf("the real process-table constructor must be called exactly once here, found %d references (zero leaves the reaper silently off)", inMain)})
	}
	return out
}
