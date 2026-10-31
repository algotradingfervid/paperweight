package app_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// releaseFile is one Go file a package compiles in the given build.
type releaseFile struct{ pkg, path string }

// goFiles lists the non-test Go files of pkg and, with deps, of every
// package in this module it depends on, as the default build (no test or
// e2e tags) compiles them.
func goFiles(t *testing.T, pkg string, withDeps bool) []releaseFile {
	t.Helper()
	args := []string{"list", "-f", "{{.ImportPath}}\t{{.Dir}}\t{{join .GoFiles \"\\t\"}}\t{{join .CgoFiles \"\\t\"}}"}
	if withDeps {
		args = append(args, "-deps")
	}
	cmd := exec.Command("go", append(args, pkg)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %s: %v: %s", pkg, err, stderr.String())
	}
	var files []releaseFile
	for line := range strings.Lines(string(out)) {
		parts := strings.Split(strings.TrimRight(line, "\n"), "\t")
		if !strings.HasPrefix(parts[0], modulePath+"/") {
			continue
		}
		for _, f := range parts[2:] {
			if f != "" {
				files = append(files, releaseFile{pkg: parts[0], path: filepath.Join(parts[1], f)})
			}
		}
	}
	return files
}

// testOnlyScan counts the DocType literals in files and lists every place
// that sets TestOnly to anything but a literal false.
func testOnlyScan(t *testing.T, files []releaseFile) (docTypes int, sets []string) {
	t.Helper()
	fset := token.NewFileSet()
	for _, f := range files {
		file, err := parser.ParseFile(fset, f.path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", f.path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if sel, ok := n.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "DocType" {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == "doctype" {
						docTypes++
					}
				}
			case *ast.KeyValueExpr:
				if k, ok := n.Key.(*ast.Ident); ok && k.Name == "TestOnly" {
					if v, ok := n.Value.(*ast.Ident); !ok || v.Name != "false" {
						sets = append(sets, fset.Position(n.Pos()).String())
					}
				}
			case *ast.AssignStmt:
				for _, l := range n.Lhs {
					if sel, ok := l.(*ast.SelectorExpr); ok && sel.Sel.Name == "TestOnly" {
						sets = append(sets, fset.Position(n.Pos()).String())
					}
				}
			}
			return true
		})
	}
	return docTypes, sets
}

// CD-291: the release build (cmd/app without test or e2e tags) registers no
// TestOnly DocType. This test binary links internal/fixture, whose DocTypes
// are TestOnly, so the registry can't be read at run time: the test scans the
// source of every module package cmd/app compiles instead, and proves the
// scan on the packages that do set TestOnly.
func TestApp_NoTestOnlyDocTypeInRelease(t *testing.T) {
	release := goFiles(t, modulePath+"/cmd/app", true)
	pkgs := map[string]bool{}
	for _, f := range release {
		pkgs[f.pkg] = true
	}
	for _, p := range []string{"/internal/app", "/internal/kernel/doctype", "/internal/masters/mstkit"} {
		if !pkgs[modulePath+p] {
			t.Fatalf("cmd/app's release files miss %s, so this scan proves nothing", p)
		}
	}
	docTypes, sets := testOnlyScan(t, release)
	if len(pkgs) < 40 || len(release) < 200 || docTypes < 10 {
		t.Fatalf("scanned %d packages, %d files, %d DocType literals; want at least 40, 200 and 10: a clean scan of too little proves nothing",
			len(pkgs), len(release), docTypes)
	}
	for _, at := range sets {
		t.Errorf("the release build sets TestOnly at %s", at)
	}

	// The scan finds the flag where test-only packages set it.
	for _, c := range []struct {
		pkg  string
		want int
	}{
		{"/internal/fixture", 4},            // Order, Receipt, Claim, Quote
		{"/internal/masters/mstfixture", 1}, // mst.Order
	} {
		if _, got := testOnlyScan(t, goFiles(t, modulePath+c.pkg, false)); len(got) != c.want {
			t.Errorf("TestOnly set in %s at %v, want %d places", c.pkg, got, c.want)
		}
	}
}
