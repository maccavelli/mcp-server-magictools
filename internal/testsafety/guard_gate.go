package testsafety

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// Rule E2, the owner's ruling on how the armed gate is covered, as relayed: "The reviewer's third way: the gate as a
// pure function, plus a guard clause on every method (Recommended)". In internal/proctable's non-test code:
//
//   - live()'s only statement is `return gateOpen(<recv>)`: the gate is the pure function a test shows shut for nil;
//   - every exported method of osProcessTable begins with `if !<recv>.live() ... { ... return ... }`, the live() test
//     the leftmost operand of the condition; Signal may first refuse an unsafe target with
//     `if err := validateTarget(...); err != nil { return err }`, which touches nothing.
//
// A broken gate can then signal nothing in a test. The gate's other shut branch, a non-nil table that is not armed,
// cannot be built in a test (rule A3) and is verified by reading only.
//
// E2a (the reviewer's third read; the owner's ruling, as relayed: "Close the two real-table gaps; record the nine
// exotic forms as known limits; no further bypass hunt (Recommended)"): the live-check's shut branch is one return
// that calls nothing, so nothing (no call, no defer) runs on a shut gate; and Signal's refusal is exactly
// `validateTarget(<p>, safePID(os.Getpid()), safePID(os.Getppid()))`, so no argument of it can run code before the gate.
const (
	RuleE2       = "E2"
	gateFunc     = "gateOpen"
	liveMethod   = "live"
	targetCheck  = "validateTarget"
	signalMethod = "Signal"
)

func checkGateClauses(files []srcFile) []Finding {
	var out []Finding
	add := func(sf srcFile, n ast.Node, detail string) {
		out = append(out, Finding{Rule: RuleE2, File: sf.rel, Line: sf.fset.Position(n.Pos()).Line, Detail: detail})
	}
	for _, sf := range files {
		if sf.isTest || !inTablePkg(sf.rel) {
			continue
		}
		for _, decl := range sf.ast.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || recvTypeName(fd) != osTableType {
				continue
			}
			recv := recvName(fd)
			switch {
			case fd.Name.Name == liveMethod:
				if !isGateReturn(fd.Body.List, recv) {
					add(sf, fd, osTableType+".live must be exactly `return "+gateFunc+"(<recv>)`")
				}
			case ast.IsExported(fd.Name.Name):
				if !beginsWithLiveCheck(fd, recv) {
					add(sf, fd, osTableType+"."+fd.Name.Name+" must begin `if !<recv>.live() ... { return ... }` before anything else")
				}
			}
		}
	}
	return out
}

func recvName(fd *ast.FuncDecl) string {
	if len(fd.Recv.List[0].Names) == 0 {
		return ""
	}
	return fd.Recv.List[0].Names[0].Name
}

func isGateReturn(list []ast.Stmt, recv string) bool {
	if recv == "" || len(list) != 1 {
		return false
	}
	ret, ok := list[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	call, ok := ret.Results[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	fn, ok := call.Fun.(*ast.Ident)
	arg, isID := call.Args[0].(*ast.Ident)
	return ok && fn.Name == gateFunc && isID && arg.Name == recv
}

// liveCheckIndex is the index of fd's live-check statement: 0, or 1 in Signal after the target refusal. It is -1 when
// the method does not begin as E2 requires.
func liveCheckIndex(fd *ast.FuncDecl, recv string) int {
	list := fd.Body.List
	i := 0
	if fd.Name.Name == signalMethod && len(list) > 0 && isTargetRefusal(list[0], firstParam(fd)) {
		i = 1
	}
	if recv == "" || len(list) <= i || !isLiveCheck(list[i], recv) {
		return -1
	}
	return i
}

func beginsWithLiveCheck(fd *ast.FuncDecl, recv string) bool { return liveCheckIndex(fd, recv) >= 0 }

func firstParam(fd *ast.FuncDecl) string {
	if fd.Type.Params == nil || len(fd.Type.Params.List) == 0 || len(fd.Type.Params.List[0].Names) == 0 {
		return ""
	}
	return fd.Type.Params.List[0].Names[0].Name
}

// targetArgs is the exact argument list of Signal's refusal (E2a): the target, this process and its parent, each read
// where the call is made. Any other expression there could run before the gate.
func targetArgs(param string) string {
	return param + ", safePID(os.Getpid()), safePID(os.Getppid())"
}

// isTargetRefusal reports `if err := validateTarget(<param>, safePID(os.Getpid()), safePID(os.Getppid())); err != nil
// { return err }`.
func isTargetRefusal(st ast.Stmt, param string) bool {
	ifs, ok := st.(*ast.IfStmt)
	if !ok || ifs.Else != nil || len(ifs.Body.List) != 1 || param == "" {
		return false
	}
	as, ok := ifs.Init.(*ast.AssignStmt)
	if !ok || len(as.Rhs) != 1 {
		return false
	}
	call, ok := as.Rhs[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	fn, ok := call.Fun.(*ast.Ident)
	if !ok || fn.Name != targetCheck || exprList(call.Args) != targetArgs(param) {
		return false
	}
	_, isRet := ifs.Body.List[0].(*ast.ReturnStmt)
	return isRet
}

func exprList(es []ast.Expr) string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = types.ExprString(e)
	}
	return strings.Join(parts, ", ")
}

// isPlainReturn reports a return statement whose results call nothing and build no function (E2a): the shut branch
// of the gate may do nothing but return.
func isPlainReturn(st ast.Stmt) bool {
	ret, ok := st.(*ast.ReturnStmt)
	if !ok {
		return false
	}
	plain := true
	for _, r := range ret.Results {
		ast.Inspect(r, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.CallExpr, *ast.FuncLit:
				plain = false
			}
			return plain
		})
	}
	return plain
}

// isLiveCheck reports `if !<recv>.live() [|| ...] { return ... }` with no init statement, whose shut branch is one
// plain return (E2a: no call, no defer, nothing before the return).
func isLiveCheck(st ast.Stmt, recv string) bool {
	ifs, ok := st.(*ast.IfStmt)
	if !ok || ifs.Init != nil || ifs.Else != nil || len(ifs.Body.List) != 1 || !isPlainReturn(ifs.Body.List[0]) {
		return false
	}
	cond := ifs.Cond
	for {
		b, isOr := cond.(*ast.BinaryExpr)
		if !isOr || b.Op != token.LOR {
			break
		}
		cond = b.X
	}
	not, ok := cond.(*ast.UnaryExpr)
	if !ok || not.Op != token.NOT {
		return false
	}
	call, ok := not.X.(*ast.CallExpr)
	return ok && len(call.Args) == 0 && isPkgSel(call.Fun, recv, liveMethod)
}
