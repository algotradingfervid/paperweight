package internal_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// plainAuthTwins maps each plain auth read to the twin a tx scope must call.
var plainAuthTwins = map[string]string{
	"Can":            "CanTx",
	"HasRole":        "HasRoleTx",
	"CheckValues":    "CheckValuesTx",
	"CheckLink":      "CheckLinkTx",
	"UsersWithRight": "UsersWithRightTx",
}

// authService supplies each read's argument count, so the rule follows the
// interface; a call with another count (auth.Access.Can) passes as a different
// method.
var authService = reflect.TypeFor[auth.Service]()

// Floors for the real tree: a walk or a type match that stops finding anything
// must fail, not pass with nothing checked.
const (
	minWalkedFiles = 250
	minTxScopes    = 400
)

// txParamTypes lists the parameter types that make a func a tx scope, spelled
// with the file's local package names. A file importing neither db nor a dbq
// package has none.
func txParamTypes(file *ast.File) map[string]bool {
	txTypes := map[string]bool{}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, "\"`")
		local := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			local = imp.Name.Name
		}
		switch {
		case strings.HasSuffix(path, "/internal/platform/db"):
			txTypes["*"+local+".Tx"] = true
			txTypes[local+".DBTX"] = true
		case strings.HasSuffix(path, "/dbq"):
			txTypes["*"+local+".Queries"] = true
		}
	}
	return txTypes
}

// plainAuthCalls returns the number of tx scopes in the file and one failure
// for each plain auth read, called or taken as a method value, and each Read or
// Write given a tx func literal, inside a tx scope: the body of a func or func
// literal with a tx parameter (see txParamTypes), nested literals included.
// Inspect runs in
// source order, so a node is inside a scope exactly when it starts before the
// scope's end.
func plainAuthCalls(filename, src string) ([]string, int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, 0, err
	}
	txTypes := txParamTypes(file)
	takesTx := func(ft *ast.FuncType) bool {
		return slices.ContainsFunc(ft.Params.List, func(f *ast.Field) bool {
			typ := f.Type
			if v, ok := typ.(*ast.Ellipsis); ok {
				typ = v.Elt
			}
			return txTypes[types.ExprString(typ)]
		})
	}
	var found []string
	scopes := 0
	var txEnd token.Pos
	otherMethod := map[*ast.SelectorExpr]bool{} // Fun of a call whose arg count isn't the interface's
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			if takesTx(n.Type) {
				scopes++
				txEnd = n.End()
			}
		case *ast.FuncLit:
			if takesTx(n.Type) {
				scopes++
				txEnd = max(txEnd, n.End())
			}
		case *ast.CallExpr:
			sel, ok := ast.Unparen(n.Fun).(*ast.SelectorExpr)
			if !ok {
				return true
			}
			name := sel.Sel.Name
			if m, ok := authService.MethodByName(name); ok && len(n.Args) != m.Type.NumIn() {
				otherMethod[sel] = true
			}
			isTxLit := func(arg ast.Expr) bool {
				lit, ok := arg.(*ast.FuncLit)
				return ok && takesTx(lit.Type)
			}
			if n.Pos() < txEnd && (name == "Read" || name == "Write") && slices.ContainsFunc(n.Args, isTxLit) {
				found = append(found, fmt.Sprintf("%s: %s opens a second transaction inside a tx scope: use the tx already held", fset.Position(n.Pos()), types.ExprString(n.Fun)))
			}
		case *ast.SelectorExpr:
			if twin, ok := plainAuthTwins[n.Sel.Name]; ok && n.Pos() < txEnd && !otherMethod[n] {
				found = append(found, fmt.Sprintf("%s: %s opens its own read inside a tx scope: use %s", fset.Position(n.Pos()), types.ExprString(n), twin))
			}
		}
		return true
	})
	return found, scopes, nil
}

// TestContracts_NoPlainAuthReadInsideTx is the static half of CD-213's nested
// transaction gate: plain auth reads open their own connection, so a tx scope
// must call the Tx twin. It sees only code lexically inside a tx scope, with no
// call graph, which the nestcheck gate covers when a non-admin test reaches it:
// a helper taking no tx parameter that is called from a scope is not followed,
// nor is a closure with no tx parameter defined outside the scope and called
// inside it. A tx held in a struct field or a named type, or a dot import of
// db, makes no scope. A call whose argument count differs from the interface's
// is not seen, so s.Auth.Can(f()) and auth.Service.Can(svc, ...) pass, as does
// Read or Write given a named func.
func TestContracts_NoPlainAuthReadInsideTx(t *testing.T) {
	for plain, twin := range plainAuthTwins {
		for _, name := range []string{plain, twin} {
			if _, ok := authService.MethodByName(name); !ok {
				t.Errorf("auth.Service has no method %s: update plainAuthTwins", name)
			}
		}
	}
	t.Run("real_tree", testNoPlainAuthRealTree)
	t.Run("rules", testNoPlainAuthRules)
}

// skippedDouble names the directories of test doubles: their names end in
// "test" or "fake". testNoPlainAuthRealTree fails if a binary links one.
func skippedDouble(name string) bool {
	return strings.HasSuffix(name, "test") || strings.HasSuffix(name, "fake")
}

// The walk skips dbq, generated templ files and test doubles.
func testNoPlainAuthRealTree(t *testing.T) {
	seen := map[string]bool{}
	scopes := 0
	for _, root := range []string{"../internal", "../cmd"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if name := d.Name(); name == "dbq" || skippedDouble(name) {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_templ.go") {
				return nil
			}
			src, err := os.ReadFile(path) //nolint:gosec // G304: test reads repo files
			if err != nil {
				return err
			}
			rel := filepath.ToSlash(strings.TrimPrefix(path, "../"))
			seen[rel] = true
			found, n, err := plainAuthCalls(rel, string(src))
			if err != nil {
				return err
			}
			scopes += n
			for _, f := range found {
				t.Error(f)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
	for _, f := range []string{"internal/kernel/auth/service.go", "internal/masters/uom/uom.go", "internal/masters/mstkit/rules.go", "cmd/app/main.go"} {
		if !seen[f] {
			t.Errorf("the walk never reached %s", f)
		}
	}
	t.Logf("files=%d scopes=%d", len(seen), scopes)
	if len(seen) < minWalkedFiles {
		t.Errorf("the walk read %d files, want at least %d", len(seen), minWalkedFiles)
	}
	if scopes < minTxScopes {
		t.Errorf("found %d tx scopes, want at least %d: did db.Tx change name?", scopes, minTxScopes)
	}

	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", "-f", "{{with .Module}}{{if .Main}}{{$.ImportPath}}{{end}}{{end}}", "./cmd/...")
	cmd.Dir = ".."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, pkg := range strings.Fields(string(out)) {
		if slices.ContainsFunc(strings.Split(pkg, "/"), skippedDouble) {
			t.Errorf("%s is linked into a binary but the walk skips it: rename it or walk it", pkg)
		}
	}
}

func testNoPlainAuthRules(t *testing.T) {
	refused := func(call, twin string) []string { return []string{call, twin} }
	inTx := func(call string) string {
		return "func f(ctx context.Context, tx *db.Tx) error {\n\t" + call + "\n\treturn nil\n}"
	}
	const canCall = "s.Auth.Can(ctx, p, dt, r, rec)"
	tests := []struct {
		name string
		src  string
		want []string // substrings of the one failure; nil means the source passes
	}{
		{"closure in Write", "func f() { d.Write(ctx, func(tx *db.Tx) error { return s.Auth.Can(ctx, p, dt, r, rec) }) }", refused("s.Auth.Can", "CanTx")},
		{"closure in Read", "func f() { d.Read(ctx, func(tx *db.Tx) error { return s.Auth.Can(ctx, p, dt, r, rec) }) }", refused("s.Auth.Can", "CanTx")},
		{"tx func Can", inTx(canCall), refused("s.Auth.Can", "CanTx")},
		{"tx func HasRole", inTx("s.Auth.HasRole(ctx, p, role)"), refused("s.Auth.HasRole", "HasRoleTx")},
		{"tx func CheckValues", inTx("s.Auth.CheckValues(ctx, p, dim, vals)"), refused("s.Auth.CheckValues", "CheckValuesTx")},
		{"tx func CheckLink", inTx("s.Auth.CheckLink(ctx, p, dt, id)"), refused("s.Auth.CheckLink", "CheckLinkTx")},
		{"tx func UsersWithRight", inTx("s.Auth.UsersWithRight(ctx, dt, r, rec)"), refused("s.Auth.UsersWithRight", "UsersWithRightTx")},
		{"tx param unnamed", "func f(context.Context, *db.Tx) { s.Auth.HasRole(ctx, p, role) }", refused("s.Auth.HasRole", "HasRoleTx")},
		{"tx param blank", "func f(_ *db.Tx) { s.Auth.HasRole(ctx, p, role) }", refused("s.Auth.HasRole", "HasRoleTx")},
		{"tx param grouped", "func f(a, tx *db.Tx) { s.Auth.HasRole(ctx, p, role) }", refused("s.Auth.HasRole", "HasRoleTx")},
		{"method taking tx", "func (s *S) f(tx *db.Tx) { e.Auth.Can(ctx, p, dt, r, rec) }", refused("e.Auth.Can", "CanTx")},
		{"literal assigned to a package var", "var f = func(tx *db.Tx) { s.Auth.Can(ctx, p, dt, r, rec) }", refused("s.Auth.Can", "CanTx")},
		{"nested literal in tx func", inTx("g := func() error { return " + canCall + " }; _ = g"), refused("s.Auth.Can", "CanTx")},
		{"nested literal in Write closure", "func f() { d.Write(ctx, func(tx *db.Tx) error { for range xs { go func() { s.Auth.Can(ctx, p, dt, r, rec) }() }; return nil }) }", refused("s.Auth.Can", "CanTx")},
		{"after a nested literal closes", inTx("g := func(inner *db.Tx) {}; _ = g; " + canCall), refused("s.Auth.Can", "CanTx")},
		{"nested Write", inTx("d.Write(ctx, func(tx2 *db.Tx) error { return nil })"), refused("d.Write", "inside a tx scope")},
		{"nested Read", inTx("d.Read(ctx, func(tx2 *db.Tx) error { return nil })"), refused("d.Read", "inside a tx scope")},
		{"method value assigned", inTx("can := s.Auth.Can; _ = can"), refused("s.Auth.Can", "CanTx")},
		{"method value as a callback", inTx("each(xs, s.Auth.HasRole)"), refused("s.Auth.HasRole", "HasRoleTx")},
		{"parenthesised call", inTx("(s.Auth.Can)(ctx, p, dt, r, rec)"), refused("s.Auth.Can", "CanTx")},
		{"variadic tx param", "func f(txs ...*db.Tx) { s.Auth.HasRole(ctx, p, role) }", refused("s.Auth.HasRole", "HasRoleTx")},
		{"aliased db import", "import dbx \"m/internal/platform/db\"\nfunc f(tx *dbx.Tx) { s.Auth.HasRole(ctx, p, role) }", refused("s.Auth.HasRole", "HasRoleTx")},
		{"DBTX param", "func f(q db.DBTX) { s.Auth.HasRole(ctx, p, role) }", refused("s.Auth.HasRole", "HasRoleTx")},
		{"dbq Queries param", "import \"m/internal/kernel/auth/dbq\"\nfunc f(q *dbq.Queries) { s.Auth.HasRole(ctx, p, role) }", refused("s.Auth.HasRole", "HasRoleTx")},
		{"aliased dbq Queries param", "import authq \"m/internal/kernel/auth/dbq\"\nfunc f(q *authq.Queries) { s.Auth.HasRole(ctx, p, role) }", refused("s.Auth.HasRole", "HasRoleTx")},

		{"CanTx allowed", inTx("s.Auth.CanTx(ctx, tx, p, dt, r, rec)"), nil},
		{"HasRoleTx allowed", inTx("s.Auth.HasRoleTx(ctx, tx, p, role)"), nil},
		{"CheckValuesTx allowed", inTx("s.Auth.CheckValuesTx(ctx, tx, p, dim, vals)"), nil},
		{"CheckLinkTx allowed", inTx("s.Auth.CheckLinkTx(ctx, tx, p, dt, id)"), nil},
		{"UsersWithRightTx allowed", inTx("s.Auth.UsersWithRightTx(ctx, tx, dt, r, rec)"), nil},
		{"plain Can outside any tx", "func f() { s.Auth.Can(ctx, p, dt, r, rec) }", nil},
		{"Access.Can with three args", inTx("acc.Can(dt, r, rec)"), nil},
		{"parenthesised Access.Can", inTx("(acc.Can)(dt, r, rec)"), nil},
		{"file without the db import", "import \"m/internal/platform/other\"\nfunc f(tx *db.Tx) { s.Auth.HasRole(ctx, p, role) }", nil},
		{"another package named db", "import \"m/other/db\"\nfunc f(tx *db.Tx) { s.Auth.HasRole(ctx, p, role) }", nil},
		{"db.Tx in a file aliasing db", "import dbx \"m/internal/platform/db\"\nfunc f(tx *db.Tx) { s.Auth.HasRole(ctx, p, role) }", nil},
		{"plain Can beside a tx literal", "func f() { g := func() { s.Auth.Can(ctx, p, dt, r, rec) }; _ = g; d.Write(ctx, func(tx *db.Tx) error { return nil }); s.Auth.Can(ctx, p, dt, r, rec) }", nil},
		{"func-typed parameter is no tx scope", "func f(set func(*db.Tx) error) { s.Auth.Can(ctx, p, dt, r, rec) }", nil},
		{"io Read inside a tx", inTx("r.Read(buf)"), nil},
		{"Read taking a literal with no tx", inTx("r.Read(func() {})"), nil},
		{"top-level Write is no nesting", "func f() { d.Write(ctx, func(tx *db.Tx) error { return nil }) }", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := tt.src
			if !strings.HasPrefix(src, "import") { // the row brings its own imports
				src = "import \"m/internal/platform/db\"\n" + src
			}
			got, _, err := plainAuthCalls("a/b.go", "package p\n"+src+"\n")
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == nil {
				if len(got) != 0 {
					t.Fatalf("refused allowed source: %q", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("got %d failures, want 1: %q", len(got), got)
			}
			want := append([]string{"a/b.go:"}, tt.want...)
			for _, w := range want {
				if !strings.Contains(got[0], w) {
					t.Errorf("failure %q does not name %q", got[0], w)
				}
			}
		})
	}
	if _, _, err := plainAuthCalls("bad.go", "not go"); err == nil {
		t.Error("unparsable source: want an error")
	}
	two := "package p\nimport \"m/internal/platform/db\"\nfunc f(tx *db.Tx) { d.Write(ctx, func(q db.DBTX) error { return nil }) }\nfunc g() {}\n"
	if _, scopes, err := plainAuthCalls("a/b.go", two); err != nil || scopes != 2 {
		t.Errorf("counted %d scopes (err %v), want 2", scopes, err)
	}
}
