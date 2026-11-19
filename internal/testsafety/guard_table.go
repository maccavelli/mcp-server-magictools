package testsafety

import (
	"fmt"
	"go/ast"
	"strconv"
	"strings"
)

// Rules A2, A3 and E (the MADR's "Design as approved", D2). They are syntactic, like the others; the structural
// barriers they back are the import cycle between internal/client and internal/proctable, the unexported and
// otherwise inert type, and the constructor's panic.

func inTablePkg(rel string) bool { return strings.HasPrefix(rel, osTablePkg+"/") }

// checkTableImports is rule A2: no _test.go outside internal/proctable imports it.
func checkTableImports(files []srcFile) []Finding {
	var out []Finding
	for _, sf := range files {
		if !sf.isTest || inTablePkg(sf.rel) {
			continue
		}
		for _, imp := range sf.ast.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			if p == osTablePkg || strings.HasSuffix(p, "/"+osTablePkg) {
				out = append(out, Finding{Rule: RuleA2, File: sf.rel, Line: sf.fset.Position(imp.Pos()).Line,
					Detail: "a test outside " + osTablePkg + " imports the real process table; tests use a fake table"})
			}
		}
	}
	return out
}

func isTableType(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == osTableType
}

// checkTableConstruction is rule A3.
func checkTableConstruction(files []srcFile) []Finding {
	var out []Finding
	add := func(sf srcFile, n ast.Node, detail string) {
		out = append(out, Finding{Rule: RuleA3, File: sf.rel, Line: sf.fset.Position(n.Pos()).Line, Detail: detail})
	}
	for _, sf := range files {
		if !inTablePkg(sf.rel) {
			if !sf.isTest {
				ast.Inspect(sf.ast, func(n ast.Node) bool {
					if id, ok := n.(*ast.Ident); ok && id.Name == childOnlyCtor {
						add(sf, id, "non-test code names "+childOnlyCtor+", the test-only child-scoped table")
					}
					return true
				})
			}
			continue
		}
		for _, decl := range sf.ast.Decls {
			fd, isFunc := decl.(*ast.FuncDecl)
			if isFunc && fd.Recv == nil && fd.Name.Name == childOnlyCtor && !sf.isTest {
				add(sf, fd, childOnlyCtor+" is declared in a non-test file; it must live in a _test.go")
			}
			allowed := isFunc && fd.Recv == nil &&
				((fd.Name.Name == constructor && !sf.isTest) || (fd.Name.Name == childOnlyCtor && sf.isTest))
			if !sf.isTest {
				checkProdConstruction(sf, decl, allowed, add)
				continue
			}
			if allowed {
				continue
			}
			checkTestNaming(sf, decl, add)
		}
		if !sf.isTest {
			ast.Inspect(sf.ast, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == childOnlyCtor {
					add(sf, id, "non-test code names "+childOnlyCtor+", the test-only child-scoped table")
				}
				return true
			})
		}
	}
	return dedupe(out)
}

// checkProdConstruction: in a non-test file, a composite literal of the type outside the constructor, and new() of it
// anywhere, are construction.
func checkProdConstruction(sf srcFile, decl ast.Decl, allowed bool, add func(srcFile, ast.Node, string)) {
	ast.Inspect(decl, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CompositeLit:
			if isTableType(v.Type) && !allowed {
				add(sf, v, "a literal of "+osTableType+" outside "+constructor+": only the constructor builds the real table")
			}
		case *ast.CallExpr:
			if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "new" && len(v.Args) == 1 && isTableType(v.Args[0]) {
				add(sf, v, "new("+osTableType+"): only the constructor builds the real table")
			}
		}
		return true
	})
}

// testNamingDetail is rule A3's finding for a test that names the real table's type where it may not.
const testNamingDetail = "a test names " + osTableType + " outside " + childOnlyCtor + " and " + childOnlyType +
	".inner: tests hold the child-only table"

// checkTestNaming: in a test file, the type may be named only as the type of childOnlyTable's field inner (the
// review's second look, F1: not in any other type declaration, such as a look-alike struct to cast to), and not in any
// expression or variable declaration outside newChildOnlyTable.
func checkTestNaming(sf srcFile, decl ast.Decl, add func(srcFile, ast.Node, string)) {
	flag := func(root ast.Node) {
		ast.Inspect(root, func(n ast.Node) bool {
			if isTableType(asExpr(n)) {
				add(sf, n, testNamingDetail)
			}
			return true
		})
	}
	gd, ok := decl.(*ast.GenDecl)
	if !ok {
		flag(decl)
		return
	}
	for _, spec := range gd.Specs {
		ts, isType := spec.(*ast.TypeSpec)
		if !isType || ts.Name.Name != childOnlyType {
			flag(spec)
			continue
		}
		st, isStruct := ts.Type.(*ast.StructType)
		if !isStruct || ts.TypeParams != nil || ts.Assign.IsValid() {
			flag(spec)
			continue
		}
		for _, field := range st.Fields.List {
			if !isInnerField(field) {
				flag(field)
			}
		}
	}
}

// isInnerField reports the one sanctioned naming: the field `inner *osProcessTable`, alone on its line.
func isInnerField(f *ast.Field) bool {
	if len(f.Names) != 1 || f.Names[0].Name != "inner" {
		return false
	}
	star, ok := f.Type.(*ast.StarExpr)
	return ok && isTableType(star.X)
}

// asExpr returns n as an expression, or nil when it is not one.
func asExpr(n ast.Node) ast.Expr {
	if e, ok := n.(ast.Expr); ok {
		return e
	}
	return nil
}

func dedupe(fs []Finding) []Finding {
	seen := map[string]bool{}
	var out []Finding
	for _, f := range fs {
		k := fmt.Sprintf("%s|%s|%d|%s", f.Rule, f.File, f.Line, f.Detail)
		if !seen[k] {
			seen[k] = true
			out = append(out, f)
		}
	}
	return out
}

// checkConstructorGuard is rule E: the constructor's first statement is the testing.Testing() panic.
func checkConstructorGuard(files []srcFile) []Finding {
	var out []Finding
	for _, sf := range files {
		if sf.isTest {
			continue
		}
		alias := importAlias(sf.ast, "testing")
		for _, decl := range sf.ast.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Name.Name != constructor || fd.Body == nil {
				continue
			}
			if alias == "" || len(fd.Body.List) == 0 || !isTestingPanic(fd.Body.List[0], alias) {
				out = append(out, Finding{Rule: RuleE, File: sf.rel, Line: sf.fset.Position(fd.Pos()).Line,
					Detail: "the first statement of " + constructor + " must be `if testing.Testing() { panic(...) }`, before anything touches a process"})
			}
		}
	}
	return out
}

func importAlias(f *ast.File, path string) string {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != path {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return path
	}
	return ""
}

func isTestingPanic(st ast.Stmt, alias string) bool {
	ifs, ok := st.(*ast.IfStmt)
	if !ok || ifs.Init != nil || ifs.Else != nil {
		return false
	}
	call, ok := ifs.Cond.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Testing" {
		return false
	}
	if x, ok := sel.X.(*ast.Ident); !ok || x.Name != alias {
		return false
	}
	panics := false
	ast.Inspect(ifs.Body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "panic" {
				panics = true
			}
		}
		return !panics
	})
	return panics
}
