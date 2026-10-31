package docscreen_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// minStepViewOfCalls is how many stepViewOf calls docscreen has today (form.go
// 6, lines.go 1). Lower it only when a call is removed: a rename that hides
// every call must fail here, not pass with nothing checked (lessons §11).
const minStepViewOfCalls = 7

// filterFuncs are the functions a stepViewOf errors argument comes from: the
// step's own errors with the module's hidden fields filtered (CD-256 (1)).
var filterFuncs = []string{"stepErrors", "refusalErrors"}

// stepViewOfProblems returns one line per stepViewOf call in file whose last
// (errors) argument is not nil, a stepErrors or refusalErrors call, or an
// expression every assignment of which in the enclosing function is such a
// call, with at least one before the call. calls counts the calls seen.
func stepViewOfProblems(fset *token.FileSet, file *ast.File) (problems []string, calls int) {
	var funcs []ast.Node // enclosing functions, innermost last
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			funcs = append(funcs, n)
			ast.Inspect(funcBody(n), visit)
			funcs = funcs[:len(funcs)-1]
			return false
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "stepViewOf" || len(n.Args) == 0 {
				return true
			}
			calls++
			arg := n.Args[len(n.Args)-1]
			if len(funcs) == 0 || !filtered(funcBody(funcs[len(funcs)-1]), arg, n.Pos()) {
				problems = append(problems, fset.Position(n.Pos()).String()+": stepViewOf errors argument "+exprText(arg)+
					" is not nil, stepErrors(...) or refusalErrors(...)")
			}
		}
		return true
	}
	for _, d := range file.Decls {
		ast.Inspect(d, visit)
	}
	return problems, calls
}

func funcBody(n ast.Node) *ast.BlockStmt {
	if f, ok := n.(*ast.FuncDecl); ok {
		return f.Body
	}
	return n.(*ast.FuncLit).Body
}

func isFilterCall(e ast.Expr) bool {
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := c.Fun.(*ast.Ident)
	return ok && (id.Name == filterFuncs[0] || id.Name == filterFuncs[1])
}

// filtered reports whether arg, used at pos inside body, is nil, a filter
// call, or assigned in body only from filter calls, once before pos. It
// compares source positions, not control flow: an assignment earlier in the
// text counts even on a branch the call doesn't follow.
func filtered(body *ast.BlockStmt, arg ast.Expr, pos token.Pos) bool {
	if id, ok := arg.(*ast.Ident); ok && id.Name == "nil" {
		return true
	}
	if isFilterCall(arg) {
		return true
	}
	want := exprText(arg)
	if want == "" {
		return false
	}
	before, other := false, false
	note := func(lhs, rhs ast.Expr, at token.Pos) {
		if exprText(lhs) != want {
			return
		}
		if rhs == nil || !isFilterCall(rhs) {
			other = true
		} else if at < pos {
			before = true
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, l := range n.Lhs {
				var r ast.Expr
				if len(n.Rhs) == len(n.Lhs) {
					r = n.Rhs[i]
				}
				note(l, r, n.Pos())
			}
		case *ast.ValueSpec:
			for i, l := range n.Names {
				var r ast.Expr
				if i < len(n.Values) {
					r = n.Values[i]
				}
				note(l, r, n.Pos())
			}
		}
		return true
	})
	return before && !other
}

// exprText is an identifier or selector chain as written ("pr.Fields"), or
// "" for anything else.
func exprText(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		if x := exprText(e.X); x != "" {
			return x + "." + e.Sel.Name
		}
	}
	return ""
}

// CLAUDE.md "Things that will bite you": every stepViewOf caller passes its
// errors through stepErrors or refusalErrors, or nil. newPost's 422 and 409
// redraw skipped the hidden-field filter in M0-1b.
func TestStepViewOf_CallersFilterErrors(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	total, scanned := 0, 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_templ.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		problems, n := stepViewOfProblems(fset, f)
		total += n
		for _, p := range problems {
			t.Error(p)
		}
	}
	if scanned < 10 {
		t.Errorf("scanned %d docscreen source files, want at least 10", scanned)
	}
	if total < minStepViewOfCalls {
		t.Errorf("found %d stepViewOf calls, want at least %d: was it renamed?", total, minStepViewOfCalls)
	}
}

// The checker flags what it must and accepts what it may, on source written
// here (go test -overlay can't reach a test that reads from disk).
func TestStepViewOf_CheckerFlagsUnfiltered(t *testing.T) {
	const src = `package p

func ok1() { d.stepViewOf(r, nil) }
func ok2() { d.stepViewOf(r, stepErrors(c, s, i, e)) }
func ok3() { errs := refusalErrors(c, s, i, e); d.stepViewOf(r, errs) }
func ok4(pr Problem) { f := func() { pr.Fields = refusalErrors(c, s, i, pr.Fields); d.stepViewOf(r, pr.Fields) }; f() }
func bad1(pr Problem) { d.stepViewOf(r, pr.Fields) }
func bad2() { errs := load(); d.stepViewOf(r, errs) }
func bad3() { errs := stepErrors(c, s, i, e); errs = load(); d.stepViewOf(r, errs) }
func bad4() { d.stepViewOf(r, messagesOnly(e)) }
func bad5() { d.stepViewOf(r, errs); errs := stepErrors(c, s, i, e); _ = errs }
func bad6(pr Problem) { pr.Fields = refusalErrors(c, s, i, pr.Fields); f := func() { d.stepViewOf(r, pr.Fields) }; f() }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	problems, n := stepViewOfProblems(fset, f)
	if n != 10 {
		t.Errorf("counted %d calls, want 10", n)
	}
	var lines []int
	for _, p := range problems {
		l, err := strconv.Atoi(strings.Split(p, ":")[1])
		if err != nil {
			t.Fatalf("problem %q has no line: %v", p, err)
		}
		lines = append(lines, l)
	}
	want := []int{7, 8, 9, 10, 11, 12}
	if len(lines) != len(want) {
		t.Fatalf("flagged lines %v, want %v:\n%s", lines, want, strings.Join(problems, "\n"))
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("flagged lines %v, want %v", lines, want)
		}
	}
}
