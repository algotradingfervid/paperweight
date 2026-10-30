package internal_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// plainAuthTwins maps each plain auth read to its argument count (which keeps
// auth.Access.Can and unrelated methods out) and the twin a tx scope must call.
var plainAuthTwins = map[string]struct {
	args int
	twin string
}{
	"Can":            {5, "CanTx"},
	"HasRole":        {3, "HasRoleTx"},
	"CheckValues":    {4, "CheckValuesTx"},
	"CheckLink":      {4, "CheckLinkTx"},
	"UsersWithRight": {4, "UsersWithRightTx"},
}

func takesTx(ft *ast.FuncType) bool {
	return slices.ContainsFunc(ft.Params.List, func(f *ast.Field) bool {
		return types.ExprString(f.Type) == "*db.Tx"
	})
}

// plainAuthCalls returns one failure for each plain auth read, and each nested
// Read or Write, inside a tx scope: the body of a func or func literal with a
// *db.Tx parameter, nested literals included. Inspect runs in source order, so
// a node is inside a scope exactly when it starts before the scope's end.
func plainAuthCalls(filename, src string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var found []string
	var txEnd token.Pos
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			if takesTx(n.Type) {
				txEnd = n.End()
			}
		case *ast.FuncLit:
			if takesTx(n.Type) {
				txEnd = max(txEnd, n.End())
			}
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || n.Pos() >= txEnd {
				return true
			}
			at, name := fset.Position(n.Pos()), sel.Sel.Name
			if p, ok := plainAuthTwins[name]; ok && len(n.Args) == p.args {
				found = append(found, fmt.Sprintf("%s: %s opens its own read inside a tx scope: use %s", at, types.ExprString(n.Fun), p.twin))
			}
			isTxLit := func(arg ast.Expr) bool {
				lit, ok := arg.(*ast.FuncLit)
				return ok && takesTx(lit.Type)
			}
			if (name == "Read" || name == "Write") && slices.ContainsFunc(n.Args, isTxLit) {
				found = append(found, fmt.Sprintf("%s: %s opens a second transaction inside a tx scope: use the tx already held", at, types.ExprString(n.Fun)))
			}
		}
		return true
	})
	return found, nil
}

// TestContracts_NoPlainAuthReadInsideTx is the static half of CD-213's nested
// transaction gate: plain auth reads open their own connection, so a tx scope
// must call the Tx twin. It sees only code lexically inside a tx scope: a
// helper taking no *db.Tx that is called from one is not followed (no call
// graph), which the nestcheck gate covers when a non-admin test reaches it.
func TestContracts_NoPlainAuthReadInsideTx(t *testing.T) {
	t.Run("real_tree", testNoPlainAuthRealTree)
	t.Run("rules", testNoPlainAuthRules)
}

// The walk skips dbq, generated templ files and test doubles: every directory
// whose name ends in "test" or "fake". A production package must not be named so.
func testNoPlainAuthRealTree(t *testing.T) {
	seen := map[string]bool{}
	for _, root := range []string{"../internal", "../cmd"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if name := d.Name(); name == "dbq" || strings.HasSuffix(name, "test") || strings.HasSuffix(name, "fake") {
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
			found, err := plainAuthCalls(rel, string(src))
			if err != nil {
				return err
			}
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

		{"CanTx allowed", inTx("s.Auth.CanTx(ctx, tx, p, dt, r, rec)"), nil},
		{"HasRoleTx allowed", inTx("s.Auth.HasRoleTx(ctx, tx, p, role)"), nil},
		{"CheckValuesTx allowed", inTx("s.Auth.CheckValuesTx(ctx, tx, p, dim, vals)"), nil},
		{"CheckLinkTx allowed", inTx("s.Auth.CheckLinkTx(ctx, tx, p, dt, id)"), nil},
		{"UsersWithRightTx allowed", inTx("s.Auth.UsersWithRightTx(ctx, tx, dt, r, rec)"), nil},
		{"plain Can outside any tx", "func f() { s.Auth.Can(ctx, p, dt, r, rec) }", nil},
		{"Access.Can with three args", inTx("acc.Can(dt, r, rec)"), nil},
		{"plain Can beside a tx literal", "func f() { g := func() { s.Auth.Can(ctx, p, dt, r, rec) }; _ = g; d.Write(ctx, func(tx *db.Tx) error { return nil }); s.Auth.Can(ctx, p, dt, r, rec) }", nil},
		{"func-typed parameter is no tx scope", "func f(set func(*db.Tx) error) { s.Auth.Can(ctx, p, dt, r, rec) }", nil},
		{"io Read inside a tx", inTx("r.Read(buf)"), nil},
		{"Read taking a literal with no tx", inTx("r.Read(func() {})"), nil},
		{"top-level Write is no nesting", "func f() { d.Write(ctx, func(tx *db.Tx) error { return nil }) }", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := plainAuthCalls("a/b.go", "package p\n"+tt.src+"\n")
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
	if _, err := plainAuthCalls("bad.go", "not go"); err == nil {
		t.Error("unparsable source: want an error")
	}
}
