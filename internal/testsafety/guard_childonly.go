package testsafety

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
)

// Rule A4 (after the independent review, P8): the discipline of internal/proctable's own tests, the one place a test
// holds the real table. Syntactic, like the others; the runtime barrier it backs is childOnlyTable's isOwnChild check.
//
//	(i)   a test names neither signalGroupAndPID nor startToken: no path around the table's own checks
//	(ii)  .inner is used only in childOnlyTable's methods and in newChildOnlyTable
//	(iii) newChildOnlyTable is declared as (t *testing.T, cmd *exec.Cmd): it is scoped to a child the test started
//	(iv)  every call of newChildOnlyTable passes a variable, a field of one, or nil: never a literal or a built value
//	(v)   a test builds no exec.Cmd or os.Process value by literal: a child is started, never forged
//	(vi)  each of childOnlyTable's Environ, Exe, Cmdline, Alive and Signal begins `if !<recv>.isOwnChild(<p>) {`
//
// Added after the review's second look (F1):
//
//	(vii)  childOnlyTable's method set, across all of the package's test files, is exactly isOwnChild and the seven
//	       methods of the ProcessTable interface: a further method cannot reach .inner under the type's name
//	(viii) no test file of the package imports reflect or unsafe: neither field access by reflection nor a cast to a
//	       look-alike struct
const (
	RuleA4        = "A4"
	childOnlyType = "childOnlyTable"
	ownChildCheck = "isOwnChild"
)

var (
	forbiddenInTableTests = map[string]bool{"signalGroupAndPID": true, "startToken": true}
	checkedMethods        = []string{"Environ", "Exe", "Cmdline", "Alive", signalMethod}
	// childOnlyMethods is the whole method set (vii): isOwnChild and the ProcessTable interface.
	childOnlyMethods = []string{ownChildCheck, "Snapshot", "Self", "Environ", "Exe", "Cmdline", "Alive", signalMethod}
	// forbiddenTableTestImports are (viii).
	forbiddenTableTestImports = map[string]bool{"reflect": true, "unsafe": true}
)

func recvTypeName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	t := fd.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func isPkgSel(e ast.Expr, pkg, name string) bool {
	s, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := s.X.(*ast.Ident)
	return ok && x.Name == pkg && s.Sel.Name == name
}

func isStarPkgSel(e ast.Expr, pkg, name string) bool {
	s, ok := e.(*ast.StarExpr)
	return ok && isPkgSel(s.X, pkg, name)
}

// plainRef reports whether e is nil, a variable, or a chain of field selections on one.
func plainRef(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return plainRef(v.X)
	}
	return false
}

// beginsWithOwnChildCheck reports whether fd's first statement is `if !<recv>.isOwnChild(<first param>) {`.
func beginsWithOwnChildCheck(fd *ast.FuncDecl) bool {
	if fd.Body == nil || len(fd.Body.List) == 0 || len(fd.Recv.List[0].Names) == 0 || fd.Type.Params == nil ||
		len(fd.Type.Params.List) == 0 || len(fd.Type.Params.List[0].Names) == 0 {
		return false
	}
	recv, param := fd.Recv.List[0].Names[0].Name, fd.Type.Params.List[0].Names[0].Name
	ifs, ok := fd.Body.List[0].(*ast.IfStmt)
	if !ok || ifs.Init != nil {
		return false
	}
	not, ok := ifs.Cond.(*ast.UnaryExpr)
	if !ok || not.Op != token.NOT {
		return false
	}
	call, ok := not.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !isPkgSel(call.Fun, recv, ownChildCheck) {
		return false
	}
	arg, ok := call.Args[0].(*ast.Ident)
	return ok && arg.Name == param
}

type a4Add = func(srcFile, ast.Node, string)

func checkChildOnlyDiscipline(files []srcFile) []Finding {
	var out []Finding
	add := func(sf srcFile, n ast.Node, detail string) {
		out = append(out, Finding{Rule: RuleA4, File: sf.rel, Line: sf.fset.Position(n.Pos()).Line, Detail: detail})
	}
	methods := map[string]bool{}
	var typeFile *srcFile
	for i, sf := range files {
		if !sf.isTest || !inTablePkg(sf.rel) {
			continue
		}
		checkTableTestImports(sf, add)
		if checkChildOnlyFile(sf, methods, add) {
			typeFile = &files[i]
		}
	}
	if typeFile == nil {
		return out
	}
	for _, m := range childOnlyMethods {
		if !methods[m] {
			add(*typeFile, typeFile.ast, childOnlyType+" lacks its method "+m)
		}
	}
	return out
}

// checkTableTestImports is (viii).
func checkTableTestImports(sf srcFile, add a4Add) {
	for _, imp := range sf.ast.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && forbiddenTableTestImports[p] {
			add(sf, imp, "a proctable test imports "+p+": it could reach the real table around "+childOnlyType)
		}
	}
}

// declaresType reports whether gd declares a type named name.
func declaresType(gd *ast.GenDecl, name string) bool {
	for _, sp := range gd.Specs {
		if ts, ok := sp.(*ast.TypeSpec); ok && ts.Name.Name == name {
			return true
		}
	}
	return false
}

// checkChildOnlyFile applies rule A4 to one test file of internal/proctable, recording childOnlyTable's methods in
// methods. It reports whether the file declares childOnlyTable.
func checkChildOnlyFile(sf srcFile, methods map[string]bool, add a4Add) bool {
	hasType := false
	for _, decl := range sf.ast.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			if gd, isGen := decl.(*ast.GenDecl); isGen && declaresType(gd, childOnlyType) {
				hasType = true
			}
			checkTableTestNode(sf, decl, false, add)
			continue
		}
		checkChildOnlyFunc(sf, fd, methods, add)
	}
	return hasType
}

// checkChildOnlyFunc applies rule A4 to one function of a proctable test file, recording childOnlyTable's methods.
func checkChildOnlyFunc(sf srcFile, fd *ast.FuncDecl, methods map[string]bool, add a4Add) {
	owner := recvTypeName(fd) == childOnlyType
	if owner {
		methods[fd.Name.Name] = true
		if !slices.Contains(childOnlyMethods, fd.Name.Name) {
			add(sf, fd, childOnlyType+" has the method "+fd.Name.Name+" beyond isOwnChild and the ProcessTable interface")
		}
		for _, m := range checkedMethods {
			if fd.Name.Name == m && !beginsWithOwnChildCheck(fd) {
				add(sf, fd, childOnlyType+"."+m+" must begin `if !c."+ownChildCheck+"(p) {`: every read and signal is scoped to the test's own child")
			}
		}
	}
	ctor := fd.Recv == nil && fd.Name.Name == childOnlyCtor
	if ctor {
		p := fd.Type.Params.List
		if len(p) != 2 || len(p[0].Names) > 1 || len(p[1].Names) > 1 ||
			!isStarPkgSel(p[0].Type, "testing", "T") || !isStarPkgSel(p[1].Type, "exec", "Cmd") {
			add(sf, fd, childOnlyCtor+" must be declared (t *testing.T, cmd *exec.Cmd): scoped to a child the test started")
		}
	}
	checkTableTestNode(sf, fd, owner || ctor, add)
}

// checkTableTestNode applies (i), (ii), (iv) and (v) to one declaration of a proctable test file.
func checkTableTestNode(sf srcFile, n ast.Node, mayUseInner bool, add a4Add) {
	ast.Inspect(n, func(x ast.Node) bool {
		switch v := x.(type) {
		case *ast.Ident:
			if forbiddenInTableTests[v.Name] {
				add(sf, v, "a proctable test names "+v.Name+": tests reach the real table only through "+childOnlyType)
			}
		case *ast.SelectorExpr:
			if v.Sel.Name == "inner" && !mayUseInner {
				add(sf, v, "a proctable test uses .inner outside "+childOnlyType+"'s methods: that is the unscoped real table")
			}
		case *ast.CallExpr:
			if id, ok := v.Fun.(*ast.Ident); ok && id.Name == childOnlyCtor {
				if len(v.Args) != 2 || !plainRef(v.Args[1]) {
					add(sf, v, childOnlyCtor+" must be given the *exec.Cmd the test started (a variable or nil), never a literal or a built value")
				}
			}
		case *ast.CompositeLit:
			if isPkgSel(v.Type, "exec", "Cmd") || isPkgSel(v.Type, "os", "Process") {
				add(sf, v, "a proctable test builds an exec.Cmd or os.Process by literal: a child is started, never forged")
			}
		}
		return true
	})
}
