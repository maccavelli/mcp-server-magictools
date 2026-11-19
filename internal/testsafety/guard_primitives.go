package testsafety

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Rule B: every use of a primitive that enumerates or signals processes is on the allowlist, by file, primitive and
// count (0010-MADR, D2; hardened after the independent review, P8, and its second look, F2). It is syntactic: import
// aliases of the packages it knows are resolved, a dot-import of one is itself a primitive, a package-qualified
// primitive counts wherever it is referenced (a call or a function value), a method that signals counts as a call, a
// method value or a method expression, strings are read as constants (literals, same-file constants and their
// concatenations), and each allowlist entry records how many uses its file has, so an extra use fails as surely as a
// new file does.

// Primitive names added after the review.
const (
	primRawSyscall = "raw-syscall"     // syscall/unix Syscall, Syscall6, RawSyscall, ...
	primSysKill    = "sys-kill-number" // SYS_KILL, SYS_TKILL, SYS_TGKILL, SYS_PIDFD_SEND_SIGNAL
	primCgo        = "cgo"             // import "C"
	primDotImport  = "dot-import:"     // a dot-import of a package this rule resolves
)

// Primitive names added after the review's second look (F2).
const (
	primSyscallOpenProcess = "syscall.OpenProcess"
	primSyscallTerminate   = "syscall.TerminateProcess"
	primWinOpenProcess     = "windows.OpenProcess"
	primWinTerminate       = "windows.TerminateProcess"
	primPsEnviron          = "cmd:ps-environ"       // ps asked for every process's environment, or for every process
	primTestExecNonLiteral = "test-exec-nonliteral" // a test launches a program whose name is not a constant
	primTestCallPrefix     = "test-call:"           // a function that signals, named by a test
)

// procfsRoot is built at run time so that this file's own source holds no literal or constant of the primitive.
var procfsRoot = path.Join("/", "proc")

const (
	pkgSyscall = "syscall"
	pkgUnix    = "golang.org/x/sys/unix"
	pkgWindows = "golang.org/x/sys/windows"
	pkgOS      = "os"
	pkgExec    = "os/exec"
)

// testCalls are functions of this module that signal another process; a test that names one is a primitive. The S4
// pid-file cleanup, and package main's service wrappers (F2: killStaleProcess and waitForProcessExit signal the pid
// they are given, signalOrWarn the process it is given).
var testCalls = map[string]bool{
	funcCleanupStale:     true,
	"killStaleProcess":   true,
	"waitForProcessExit": true,
	"signalOrWarn":       true,
}

// signalMethods are the methods that signal another process (os.Process, gopsutil's Process), with the number of
// arguments each takes: a call with that many arguments, a method value or a method expression is a use.
var signalMethods = map[string]int{
	signalMethod: 1, "Kill": 0, "KillWithContext": 1,
	"Terminate": 0, "TerminateWithContext": 1,
	"SendSignal": 1, "SendSignalWithContext": 2,
	"Suspend": 0, "SuspendWithContext": 1,
}

// importNames are a file's local names for the packages rule B resolves, and every local package name in it.
type importNames struct {
	syscall, unix, windows, os, exec, gops string
	all                                    map[string]bool
}

type hit struct {
	primitive string
	line      int
	pos       token.Pos
}

func localName(imp *ast.ImportSpec, ipath string) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	last := ipath[strings.LastIndex(ipath, "/")+1:]
	if i := strings.Index(last, "."); i > 0 {
		last = last[:i]
	}
	return last
}

func isGopsutilProcess(ipath string) bool {
	return strings.HasPrefix(ipath, gopsutilPrefix) && strings.HasSuffix(ipath, gopsutilSuffix)
}

func knownPkg(p string) bool {
	switch p {
	case pkgSyscall, pkgUnix, pkgWindows, pkgOS, pkgExec:
		return true
	}
	return isGopsutilProcess(p)
}

// resolveImports names the file's imports and reports the import-level primitives: cgo and dot-imports.
func resolveImports(f *ast.File, add func(string, token.Pos)) importNames {
	n := importNames{all: map[string]bool{}}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if p == "C" {
			add(primCgo, imp.Pos())
			continue
		}
		local := localName(imp, p)
		if local == "." {
			if knownPkg(p) {
				add(primDotImport+p, imp.Pos())
			}
			continue
		}
		if local == "_" {
			continue
		}
		n.all[local] = true
		switch {
		case p == pkgSyscall:
			n.syscall = local
		case p == pkgUnix:
			n.unix = local
		case p == pkgWindows:
			n.windows = local
		case p == pkgOS:
			n.os = local
		case p == pkgExec:
			n.exec = local
		case isGopsutilProcess(p):
			n.gops = local
		}
	}
	return n
}

func syscallPrimitive(sel string) string {
	switch sel {
	case methodKill:
		return primSyscallKill
	case "Syscall", "Syscall6", "Syscall9", "RawSyscall", "RawSyscall6":
		return primRawSyscall
	case "SYS_KILL", "SYS_TKILL", "SYS_TGKILL":
		return primSysKill
	case "OpenProcess":
		return primSyscallOpenProcess
	case "TerminateProcess":
		return primSyscallTerminate
	}
	return ""
}

func unixPrimitive(sel string) string {
	switch sel {
	case methodKill, "Tgkill", "PidfdSendSignal":
		return primUnixKill
	case "SysctlRaw":
		return primSysctlRaw
	case "Syscall", "Syscall6", "RawSyscall", "RawSyscall6", "SyscallNoError", "RawSyscallNoError":
		return primRawSyscall
	case "SYS_KILL", "SYS_TKILL", "SYS_TGKILL", "SYS_PIDFD_SEND_SIGNAL":
		return primSysKill
	}
	return ""
}

// pkgPrimitive classifies a package-qualified selector x.sel.
func pkgPrimitive(n importNames, x, sel string) string {
	switch {
	case n.syscall != "" && x == n.syscall:
		return syscallPrimitive(sel)
	case n.unix != "" && x == n.unix:
		return unixPrimitive(sel)
	case n.windows != "" && x == n.windows:
		switch sel {
		case "OpenProcess":
			return primWinOpenProcess
		case "TerminateProcess":
			return primWinTerminate
		}
	case n.os != "" && x == n.os:
		if sel == "FindProcess" {
			return primFindProcess
		}
	case n.gops != "" && x == n.gops:
		if gopsutilCalls[sel] {
			return primGopsutilPrefix + sel
		}
	}
	return ""
}

// isPkgQualified reports whether sel is x.Name with x one of the file's package names.
func isPkgQualified(n importNames, sel *ast.SelectorExpr) bool {
	x, ok := sel.X.(*ast.Ident)
	return ok && n.all[x.Name]
}

// methodPrimitive classifies a method call on a value (not on a package): another process's environment, a signal,
// a kill, a terminate or a suspend.
func methodPrimitive(n importNames, call *ast.CallExpr, sel *ast.SelectorExpr) string {
	if isPkgQualified(n, sel) {
		return "" // a package-qualified name, classified by pkgPrimitive (syscall.Signal(0) is a conversion)
	}
	if sel.Sel.Name == methodEnviron && len(call.Args) == 0 {
		return primEnviron
	}
	if want, ok := signalMethods[sel.Sel.Name]; ok && (len(call.Args) == want || (len(call.Args) == want+1 && isTypeExpr(n, sel.X))) {
		return "." + sel.Sel.Name
	}
	return ""
}

// isTypeExpr reports a receiver written as a type, as in a called method expression: (*os.Process).Kill(p) or
// pkg.T.Method(v), whose first argument is the receiver.
func isTypeExpr(n importNames, x ast.Expr) bool {
	switch v := unparen(x).(type) {
	case *ast.StarExpr:
		return true
	case *ast.SelectorExpr:
		id, ok := v.X.(*ast.Ident)
		return ok && n.all[id.Name]
	}
	return false
}

// methodValuePrimitive classifies a signalling method that is referenced but not called here: a method value
// (f := p.Signal) or a method expression ((*os.Process).Kill). Either can be called later under another name.
func methodValuePrimitive(n importNames, sel *ast.SelectorExpr) string {
	if isPkgQualified(n, sel) {
		return ""
	}
	if _, ok := signalMethods[sel.Sel.Name]; ok {
		return "." + sel.Sel.Name
	}
	return ""
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

// commandWord reports the command a string names: the word itself, or a path whose base is the word (with any
// .exe), as in "/bin/kill" or a Windows path to taskkill.exe.
func commandWord(s string) string {
	if cmdWords[s] {
		return s
	}
	if strings.ContainsAny(s, " \t\n") {
		return ""
	}
	i := strings.LastIndexAny(s, `/\`)
	if i < 0 {
		return ""
	}
	b := strings.TrimSuffix(strings.ToLower(s[i+1:]), ".exe")
	if cmdWords[b] {
		return b
	}
	return ""
}

func shellTokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(";|&()`$<>", r) })
}

// execNameArg returns the argument of an exec.Command or exec.CommandContext call that names the program.
func execNameArg(n importNames, call *ast.CallExpr) (ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || n.exec == "" || !isPkgSel(sel, n.exec, sel.Sel.Name) {
		return nil, false
	}
	idx := map[string]int{"Command": 0, "CommandContext": 1}
	i, ok := idx[sel.Sel.Name]
	if !ok || len(call.Args) <= i {
		return nil, false
	}
	return call.Args[i], true
}

// collector gathers one file's primitive uses.
type collector struct {
	sf       srcFile
	names    importNames
	consts   map[string]string
	decls    map[token.Pos]bool
	callFuns map[*ast.SelectorExpr]bool
	inner    map[*ast.BinaryExpr]bool
	hits     []hit
}

func (c *collector) add(p string, pos token.Pos) {
	c.hits = append(c.hits, hit{primitive: p, line: c.sf.fset.Position(pos).Line, pos: pos})
}

func (c *collector) selector(v *ast.SelectorExpr) {
	if x, ok := v.X.(*ast.Ident); ok {
		if p := pkgPrimitive(c.names, x.Name, v.Sel.Name); p != "" {
			c.add(p, v.Pos())
			return
		}
	}
	if !c.callFuns[v] {
		if p := methodValuePrimitive(c.names, v); p != "" {
			c.add(p, v.Pos())
		}
	}
}

func (c *collector) ident(v *ast.Ident) {
	if c.decls[v.Pos()] {
		return
	}
	switch {
	case v.Name == funcGetProcessEnviron:
		c.add(funcGetProcessEnviron, v.Pos())
	case c.sf.isTest && testCalls[v.Name]:
		c.add(primTestCallPrefix+v.Name, v.Pos())
	}
}

func (c *collector) call(v *ast.CallExpr) {
	if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
		if p := methodPrimitive(c.names, v, sel); p != "" {
			c.add(p, v.Pos())
		}
	}
	if c.sf.isTest {
		if arg, ok := execNameArg(c.names, v); ok {
			if _, isConst := strValue(arg, c.consts); !isConst {
				c.add(primTestExecNonLiteral, v.Pos())
			}
		}
	}
	commandWordsIn(v.Args, c.consts, c.add)
}

// concat reports a /proc path built by concatenating constants, once per outermost concatenation, unless its first
// operand is itself a /proc literal (reported as one).
func (c *collector) concat(v *ast.BinaryExpr) {
	if v.Op != token.ADD {
		return
	}
	for _, side := range []ast.Expr{v.X, v.Y} {
		if b, ok := unparen(side).(*ast.BinaryExpr); ok {
			c.inner[b] = true
		}
	}
	if c.inner[v] {
		return
	}
	if lit, ok := leftmostLeaf(v).(*ast.BasicLit); ok {
		if s, ok := stringLit(lit); ok && isProcfs(s) {
			return
		}
	}
	if isProcfs(leadingConst(v, c.consts)) {
		c.add(primProcfs, v.Pos())
	}
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

func collectHits(sf srcFile) []hit {
	c := &collector{sf: sf, consts: fileConsts(sf.ast), decls: map[token.Pos]bool{},
		callFuns: map[*ast.SelectorExpr]bool{}, inner: map[*ast.BinaryExpr]bool{}}
	c.names = resolveImports(sf.ast, c.add)
	for _, d := range sf.ast.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			c.decls[fd.Name.Pos()] = true
		}
	}
	ast.Inspect(sf.ast, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := unparen(call.Fun).(*ast.SelectorExpr); ok {
				c.callFuns[sel] = true
			}
		}
		return true
	})
	ast.Inspect(sf.ast, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.SelectorExpr:
			c.selector(v)
		case *ast.Ident:
			c.ident(v)
		case *ast.CallExpr:
			c.call(v)
		case *ast.CompositeLit:
			commandWordsIn(v.Elts, c.consts, c.add)
		case *ast.BinaryExpr:
			c.concat(v)
		case *ast.BasicLit:
			if s, ok := stringLit(v); ok && isProcfs(s) {
				c.add(primProcfs, v.Pos())
			}
		}
		return true
	})
	for _, cg := range sf.ast.Comments {
		for _, cm := range cg.List {
			if strings.HasPrefix(cm.Text, "//go:linkname") {
				c.add(primLinkname, cm.Pos())
			}
		}
	}
	return c.hits
}

// checkPrimitives compares each file's uses of each primitive with the allowlist's count for it.
func checkPrimitives(files []srcFile, allow []Allow) []Finding {
	allowed := make(map[string]int, len(allow))
	site := make(map[string]string, len(allow))
	for _, a := range allow {
		k := a.File + "|" + a.Primitive
		allowed[k] += a.Count
		site[k] = a.Site
	}
	type use struct {
		file, prim string
		lines      []int
	}
	uses := map[string]*use{}
	for _, sf := range files {
		for _, h := range collectHits(sf) {
			k := sf.rel + "|" + h.primitive
			u := uses[k]
			if u == nil {
				u = &use{file: sf.rel, prim: h.primitive}
				uses[k] = u
			}
			u.lines = append(u.lines, h.line)
		}
	}
	keys := make([]string, 0, len(uses))
	for k := range uses {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []Finding
	for _, k := range keys {
		u := uses[k]
		want, ok := allowed[k]
		switch {
		case !ok:
			out = append(out, Finding{Rule: RuleB, File: u.file, Line: u.lines[0],
				Detail: fmt.Sprintf("primitive %q is not on the allowlist: a new site that enumerates or signals processes must be classified in the MADR's inventory and added to the allowlist", u.prim)})
		case len(u.lines) != want:
			line := u.lines[0]
			if len(u.lines) > want && want > 0 {
				line = u.lines[want]
			}
			out = append(out, Finding{Rule: RuleB, File: u.file, Line: line,
				Detail: fmt.Sprintf("primitive %q is used %d times here, the allowlist records %d (%s): an extra use must be classified, and the count changed with its reason", u.prim, len(u.lines), want, site[k])})
		}
	}
	for _, a := range allow {
		if uses[a.File+"|"+a.Primitive] == nil {
			out = append(out, Finding{Rule: RuleB, File: a.File,
				Detail: fmt.Sprintf("stale allowlist entry %q (%s): no such use remains; remove the entry", a.Primitive, a.Site)})
		}
	}
	return out
}
