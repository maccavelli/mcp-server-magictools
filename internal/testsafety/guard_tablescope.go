package testsafety

import (
	"go/ast"
	"go/token"
)

// Rule E2b, the scope of the real table's own package (the reviewer's third read; the owner's ruling, as relayed:
// "Close the two real-table gaps; record the nine exotic forms as known limits; no further bypass hunt
// (Recommended)", put as: signalling code named only inside Signal after the gate; no init; no function-valued
// variables). The three clauses below are the coordinator's decision, inside that ruling, on what the code can satisfy
// (0010's PLAN records the coordinator's brief error that it corrects). In internal/proctable's non-test code:
//
//	(a) signalGroupAndPID is named only inside (*osProcessTable).Signal, after its live-check
//	(b) the signalling primitives (every rule-B primitive that is not a read) are named only inside the body of
//	    func signalGroupAndPID
//	(c) the reading primitives are named only inside osProcessTable methods or inside the token helpers startToken
//	    and pidNamespace, and those two are named only inside osProcessTable methods
//	(d) no func init, and no package-level var that holds a function: a function literal, a package function or a
//	    selector as its value, or a function type
//
// So no new function, promoted method, unexported method, function value or init of the package can reach a
// primitive except through the gated Signal and the gated methods.
const (
	RuleE2b      = "E2b"
	signalHelper = "signalGroupAndPID"
)

// tokenHelpers are the two functions (c) lets read outside a method.
var tokenHelpers = map[string]bool{"startToken": true, "pidNamespace": true}

// readPrimitives are rule B's primitives that read and never signal.
var readPrimitives = map[string]bool{
	primGopsutilPids: true, primGopsutilProcs: true, primGopsutilNewProc: true,
	primGopsutilPrefix + "PidsWithContext": true, primGopsutilPrefix + "ProcessesWithContext": true,
	primGopsutilPrefix + "NewProcessWithContext": true,
	funcGetProcessEnviron:                        true, primEnviron: true, primProcfs: true, primSysctlRaw: true, primPsEnviron: true,
}

// enclosing returns the top-level function declaration that contains pos, or nil.
func enclosing(f *ast.File, pos token.Pos) *ast.FuncDecl {
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Pos() <= pos && pos < fd.End() {
			return fd
		}
	}
	return nil
}

func isTableMethod(fd *ast.FuncDecl) bool { return fd != nil && recvTypeName(fd) == osTableType }

func isTokenHelper(fd *ast.FuncDecl) bool {
	return fd != nil && fd.Recv == nil && tokenHelpers[fd.Name.Name]
}

func isPlainFunc(fd *ast.FuncDecl, name string) bool {
	return fd != nil && fd.Recv == nil && fd.Name.Name == name
}

// afterSignalGate reports whether pos lies inside (*osProcessTable).Signal after its live-check statement.
func afterSignalGate(fd *ast.FuncDecl, pos token.Pos) bool {
	if !isTableMethod(fd) || fd.Name.Name != signalMethod || fd.Body == nil {
		return false
	}
	i := liveCheckIndex(fd, recvName(fd))
	return i >= 0 && pos >= fd.Body.List[i].End()
}

func checkTableScope(files []srcFile) []Finding {
	var out []Finding
	add := func(sf srcFile, pos token.Pos, detail string) {
		out = append(out, Finding{Rule: RuleE2b, File: sf.rel, Line: sf.fset.Position(pos).Line, Detail: detail})
	}
	funcs := map[string]bool{} // the package's functions, across all its non-test files
	for _, sf := range files {
		if sf.isTest || !inTablePkg(sf.rel) {
			continue
		}
		for _, d := range sf.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				funcs[fd.Name.Name] = true
			}
		}
	}
	for _, sf := range files {
		if sf.isTest || !inTablePkg(sf.rel) {
			continue
		}
		checkScopeNames(sf, add)
		checkScopePrimitives(sf, add)
		checkScopeDecls(sf, funcs, add)
	}
	return out
}

// checkScopeNames applies (a) and the second half of (c): where signalGroupAndPID and the token helpers are named.
func checkScopeNames(sf srcFile, add func(srcFile, token.Pos, string)) {
	decls := map[token.Pos]bool{}
	for _, d := range sf.ast.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			decls[fd.Name.Pos()] = true
		}
	}
	ast.Inspect(sf.ast, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || decls[id.Pos()] {
			return true
		}
		fd := enclosing(sf.ast, id.Pos())
		switch {
		case id.Name == signalHelper && !afterSignalGate(fd, id.Pos()):
			add(sf, id.Pos(), "(a) "+signalHelper+" is named outside (*"+osTableType+").Signal after its live-check")
		case tokenHelpers[id.Name] && !isTableMethod(fd):
			add(sf, id.Pos(), "(c) "+id.Name+" is named outside a method of "+osTableType)
		}
		return true
	})
}

// checkScopePrimitives applies (b) and the first half of (c): where each rule-B primitive is used.
func checkScopePrimitives(sf srcFile, add func(srcFile, token.Pos, string)) {
	for _, h := range collectHits(sf) {
		fd := enclosing(sf.ast, h.pos)
		if readPrimitives[h.primitive] {
			if !isTableMethod(fd) && !isTokenHelper(fd) {
				add(sf, h.pos, "(c) the reading primitive "+h.primitive+" is used outside a method of "+osTableType+
					" and the token helpers")
			}
			continue
		}
		if !isPlainFunc(fd, signalHelper) {
			add(sf, h.pos, "(b) the signalling primitive "+h.primitive+" is used outside "+signalHelper)
		}
	}
}

// checkScopeDecls applies (d).
func checkScopeDecls(sf srcFile, funcs map[string]bool, add func(srcFile, token.Pos, string)) {
	for _, d := range sf.ast.Decls {
		switch v := d.(type) {
		case *ast.FuncDecl:
			if isPlainFunc(v, "init") {
				add(sf, v.Pos(), "(d) func init in "+osTablePkg+": it would run in every binary, tests included")
			}
		case *ast.GenDecl:
			if v.Tok != token.VAR {
				continue
			}
			for _, sp := range v.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if _, isFn := unparen(vs.Type).(*ast.FuncType); isFn && vs.Type != nil {
					add(sf, vs.Pos(), "(d) a package-level var of a function type")
					continue
				}
				for _, val := range vs.Values {
					if holdsFunction(unparen(val), funcs) {
						add(sf, val.Pos(), "(d) a package-level var holds a function")
					}
				}
			}
		}
	}
}

// holdsFunction reports a value that is a function: a literal, a function of the package, or any selector (a package
// function, a method value or a method expression; a package variable is not used this way in the table's package).
func holdsFunction(e ast.Expr, funcs map[string]bool) bool {
	switch v := e.(type) {
	case *ast.FuncLit, *ast.SelectorExpr:
		return true
	case *ast.Ident:
		return funcs[v.Name]
	}
	return false
}
