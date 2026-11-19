package testsafety

import (
	"go/ast"
	"go/token"
	"strings"
	"unicode"
)

// Rule B reads strings the way the compiler would see a constant: a literal, a constant of the same file, or a
// concatenation of those (the review's second look, F2: a /proc path built as "/pro" + "c/1/environ", and a command
// word held in a constant). A value that is not constant is not read; where it names a program in a test, that is
// itself a primitive (primTestExecNonLiteral).

// fileConsts maps each string constant declared anywhere in f, at package level or in a function, to its value.
func fileConsts(f *ast.File) map[string]string {
	raw := map[string]ast.Expr{}
	ast.Inspect(f, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			return true
		}
		for _, sp := range gd.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok || len(vs.Values) != len(vs.Names) {
				continue
			}
			for i, name := range vs.Names {
				raw[name.Name] = vs.Values[i]
			}
		}
		return true
	})
	out := map[string]string{}
	for name, e := range raw {
		if s, ok := foldWith(e, raw, map[string]bool{name: true}); ok {
			out[name] = s
		}
	}
	return out
}

// foldWith folds e over the raw constant expressions, refusing a cycle.
func foldWith(e ast.Expr, raw map[string]ast.Expr, busy map[string]bool) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		return stringLit(v)
	case *ast.ParenExpr:
		return foldWith(v.X, raw, busy)
	case *ast.Ident:
		x, ok := raw[v.Name]
		if !ok || busy[v.Name] {
			return "", false
		}
		busy[v.Name] = true
		defer delete(busy, v.Name)
		return foldWith(x, raw, busy)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok := foldWith(v.X, raw, busy)
		if !ok {
			return "", false
		}
		r, ok := foldWith(v.Y, raw, busy)
		return l + r, ok
	}
	return "", false
}

// strValue is e's constant string value, if it has one.
func strValue(e ast.Expr, consts map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		return stringLit(v)
	case *ast.ParenExpr:
		return strValue(v.X, consts)
	case *ast.Ident:
		s, ok := consts[v.Name]
		return s, ok
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, ok := strValue(v.X, consts)
		if !ok {
			return "", false
		}
		r, ok := strValue(v.Y, consts)
		return l + r, ok
	}
	return "", false
}

// leadingConst is the constant prefix of a concatenation: "/proc/" + pid + "/environ" has the prefix "/proc/".
func leadingConst(e ast.Expr, consts map[string]string) string {
	if s, ok := strValue(e, consts); ok {
		return s
	}
	switch v := e.(type) {
	case *ast.ParenExpr:
		return leadingConst(v.X, consts)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return ""
		}
		if l, ok := strValue(v.X, consts); ok {
			return l + leadingConst(v.Y, consts)
		}
		return leadingConst(v.X, consts)
	}
	return ""
}

// leftmostLeaf is the first operand of a chain of concatenations.
func leftmostLeaf(e ast.Expr) ast.Expr {
	for {
		switch v := e.(type) {
		case *ast.ParenExpr:
			e = v.X
		case *ast.BinaryExpr:
			e = v.X
		default:
			return e
		}
	}
}

func isProcfs(s string) bool { return s == procfsRoot || strings.HasPrefix(s, procfsRoot+"/") }

// isShellCFlag reports a short-option cluster that includes c: "-c", "-lc", "-ec" (sh -c, bash -lc, ...).
func isShellCFlag(s string) bool {
	if len(s) < 2 || s[0] != '-' || s[1] == '-' {
		return false
	}
	for _, r := range s[1:] {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return strings.ContainsRune(s[1:], 'c')
}

// isPs reports the ps program, by word or by the base name of a path.
func isPs(s string) bool {
	if s == "ps" {
		return true
	}
	i := strings.LastIndexAny(s, `/\`)
	return i >= 0 && !strings.ContainsAny(s, " \t\n") && strings.TrimSuffix(strings.ToLower(s[i+1:]), ".exe") == "ps"
}

// psEnvFlag reports a ps argument that asks for every process's environment or for every process: a BSD-style
// cluster with e ("eww"), or a dash-style one with e or E ("-E", "-e", "-Ae").
func psEnvFlag(s string) bool {
	if strings.HasPrefix(s, "--") {
		return false
	}
	t := strings.TrimPrefix(s, "-")
	if t == "" {
		return false
	}
	for _, r := range t {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	if t != s {
		return strings.ContainsAny(t, "eE")
	}
	return strings.ContainsRune(t, 'e')
}

// scanShell scans the words of a shell string: its first command word, and ps with an environment flag.
func scanShell(s string, at token.Pos, add func(string, token.Pos)) {
	toks := shellTokens(s)
	for _, tok := range toks {
		if w := commandWord(tok); w != "" {
			add("cmd:"+w, at)
			break
		}
	}
	for i, tok := range toks {
		if !isPs(tok) {
			continue
		}
		for _, arg := range toks[i+1:] {
			if psEnvFlag(arg) {
				add(primPsEnviron, at)
				return
			}
		}
	}
}

// scanRun checks one run of constant words, in order, each reported at its own position: a command word; any word
// after a shell's -c cluster (sh -c, bash -lc) or cmd's /c, scanned as a shell string; a ps flag after ps.
func scanRun(run []string, pos []token.Pos, add func(string, token.Pos)) {
	afterC, afterPs := false, false
	for i, s := range run {
		if w := commandWord(s); w != "" {
			add("cmd:"+w, pos[i])
		} else if afterC {
			scanShell(s, pos[i], add)
		}
		if afterPs && psEnvFlag(s) {
			add(primPsEnviron, pos[i])
			afterPs = false
		}
		if isPs(s) {
			afterPs = true
		}
		afterC = isShellCFlag(s) || strings.EqualFold(s, "/c")
	}
}

// commandWordsIn finds command words in a list of call arguments or slice elements. A value that is not constant
// ends a run of words.
func commandWordsIn(list []ast.Expr, consts map[string]string, add func(string, token.Pos)) {
	var run []string
	var pos []token.Pos
	for _, e := range list {
		s, ok := strValue(e, consts)
		if !ok {
			scanRun(run, pos, add)
			run, pos = nil, nil
			continue
		}
		run = append(run, s)
		pos = append(pos, e.Pos())
	}
	scanRun(run, pos, add)
}
