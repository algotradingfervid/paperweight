package allmasters_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/vardhan/internaltools"

func goList(t *testing.T, args ...string) []string {
	t.Helper()
	var stderr bytes.Buffer
	cmd := exec.Command("go", append([]string{"list"}, args...)...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func hasWebElement(importPath string) bool {
	return slices.Contains(strings.Split(importPath, "/"), "web")
}

func isUI(importPath string) bool {
	return importPath == module+"/internal/ui" || strings.HasPrefix(importPath, module+"/internal/ui/")
}

// CD-89: no package under internal/masters/ outside a web/ subpackage depends
// on internal/ui, directly or through another package.
func TestMasters_ServicesDoNotImportUI(t *testing.T) {
	lines := goList(t, "-e", "-f", `{{.ImportPath}} {{join .Deps " "}}`, module+"/internal/masters/...")
	var checked []string
	for _, line := range lines {
		pkg, deps, _ := strings.Cut(line, " ")
		if hasWebElement(pkg) {
			continue
		}
		// Ruling (AD, MST-22): allmasters must import every <pkg>/web package
		// to mount the screens (CD-89; TestMasters_RoutesUseWebSubpackages), so
		// its transitive deps reach internal/ui by design. Exempt only this
		// import path; its subpackages stay in the walk.
		if pkg == module+"/internal/masters/allmasters" {
			continue
		}
		checked = append(checked, pkg)
		for _, dep := range strings.Fields(deps) {
			if isUI(dep) {
				t.Errorf("%s depends on %s; only internal/masters/<pkg>/web may import internal/ui (CD-89)", pkg, dep)
			}
		}
	}
	// The walk must have covered the master service packages themselves.
	for _, pkg := range []string{"company", "paymentterms", "uom", "taxcode", "item", "expensetype", "mstkit", "msttest"} {
		if !slices.Contains(checked, module+"/internal/masters/"+pkg) {
			t.Errorf("go list did not report %s/internal/masters/%s", module, pkg)
		}
	}
}

// allmasters reaches the screens through the web/ subpackages, and
// routes.go is the only allmasters file that imports them (Global
// Constraints, CD-89).
func TestMasters_RoutesUseWebSubpackages(t *testing.T) {
	deps := goList(t, "-deps", module+"/internal/masters/allmasters")
	for _, pkg := range []string{"company", "paymentterms", "uom", "taxcode", "item", "expensetype"} {
		web := module + "/internal/masters/" + pkg + "/web"
		if !slices.Contains(deps, web) {
			t.Errorf("internal/masters/allmasters does not depend on %s", web)
		}
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(files, "routes.go") {
		t.Error("internal/masters/allmasters/routes.go is missing")
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if name == "routes.go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(p, module+"/internal/masters/") && hasWebElement(p) {
				t.Errorf("%s imports %s; only routes.go may import the web/ subpackages", name, p)
			}
		}
	}
}

// A web/ package that has a Routes function is called from routes.go. A
// master whose web directory holds only tests isn't built yet; mstkit/web and
// gstin/web have no Routes function.
func TestMasters_BuiltScreensAreMounted(t *testing.T) {
	fset := token.NewFileSet()
	routes, err := parser.ParseFile(fset, "routes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	imports := map[string]string{} // alias → directory
	for _, imp := range routes.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		if rel, ok := strings.CutPrefix(p, module+"/internal/masters/"); ok && imp.Name != nil {
			imports[imp.Name.Name] = filepath.Join("..", rel)
		}
	}
	mounted := map[string]bool{}
	ast.Inspect(routes, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Routes" {
			if x, ok := sel.X.(*ast.Ident); ok {
				mounted[imports[x.Name]] = true
			}
		}
		return true
	})

	files, err := filepath.Glob("../*/web/*.go")
	if err != nil {
		t.Fatal(err)
	}
	built := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "Routes" {
				built++
				if dir := filepath.Dir(name); !mounted[dir] {
					t.Errorf("%s has Routes; routes.go doesn't call it", dir)
				}
			}
		}
	}
	if built == 0 {
		t.Error("found no web/ package with a Routes function")
	}
}
