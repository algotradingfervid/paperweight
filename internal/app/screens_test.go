package app_test

import (
	"bufio"
	"fmt"
	"go/build"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	uiImport = "github.com/vardhan/internaltools/internal/ui"

	// Floors: today's counts. A wrong root or pattern would scan nothing and
	// pass; a package or file may be added, not lost.
	minScreenPackages = 4
	minScreenFiles    = 19
)

// rawMarkup is what the UI components supply: a screen composes them instead.
var rawMarkup = regexp.MustCompile(`(?i)<(table|button|input)\b|role\s*=\s*["']alert["']`)

// screenPackages maps each package dir under internal/kernel/*/web that has
// .templ files to those files.
func screenPackages(t *testing.T) map[string][]string {
	t.Helper()
	webDirs, err := filepath.Glob(filepath.Join("..", "kernel", "*", "web"))
	if err != nil {
		t.Fatal(err)
	}
	pkgs := map[string][]string{}
	for _, root := range webDirs {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && filepath.Ext(path) == ".templ" {
				dir := filepath.Dir(path)
				pkgs[dir] = append(pkgs[dir], path)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return pkgs
}

// CD-49, CD-91: area screens compose internal/ui components.
func TestScreens_UseUIComponents(t *testing.T) {
	pkgs := screenPackages(t)
	files := 0
	for dir, templs := range pkgs {
		p, err := build.ImportDir(dir, 0)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if !importsUI(p.Imports) {
			t.Errorf("%s: has .templ files but imports nothing under internal/ui", dir)
		}
		files += len(templs)
		for _, path := range templs {
			checkNoRawMarkup(t, path)
		}
	}
	if len(pkgs) < minScreenPackages || files < minScreenFiles {
		t.Errorf("scanned %d packages and %d files, want at least %d and %d", len(pkgs), files, minScreenPackages, minScreenFiles)
	}
}

func importsUI(imports []string) bool {
	for _, imp := range imports {
		if imp == uiImport || strings.HasPrefix(imp, uiImport+"/") {
			return true
		}
	}
	return false
}

func checkNoRawMarkup(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // path comes from the walk over the repo
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	for n := 1; s.Scan(); n++ {
		if m := rawMarkup.FindString(s.Text()); m != "" {
			t.Errorf("%s:%d: raw %s: use the internal/ui component", path, n, m)
		}
	}
	if err := s.Err(); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// A fresh client has no shell token: /setup and /signin work only because
// their forms post the token the page was given.
func TestApp_SetupAndSignInFormsPostTheirToken(t *testing.T) {
	w := newWorld(t)
	formToken := func(b *browser, path string) {
		t.Helper()
		sel := fmt.Sprintf(`form[action=%q] input[name="csrf_token"]`, path)
		b.csrf = b.get(path).doc.Find(sel).AttrOr("value", "")
		if b.csrf == "" {
			t.Fatalf("GET %s: its form has no csrf_token", path)
		}
	}

	setup := w.browser()
	formToken(setup, "/setup")
	r := setup.post("/setup", url.Values{"email": {adminEmail}, "name": {"Anil Kapoor"}, "password": {testPassword}})
	if r.status != http.StatusSeeOther || r.location() != "/signin" {
		t.Fatalf("POST /setup = %d to %q (%q), want 303 to /signin", r.status, r.location(), r.pageText())
	}

	signin := w.browser()
	formToken(signin, "/signin")
	r = signin.post("/signin", url.Values{"email": {adminEmail}, "password": {testPassword}})
	if r.status != http.StatusSeeOther || r.location() == "/signin" {
		t.Fatalf("POST /signin = %d to %q (%q), want 303 into the app", r.status, r.location(), r.pageText())
	}
	if got := signin.get("/").location(); strings.HasPrefix(got, "/signin") || got == "/setup" {
		t.Fatalf("GET / after signing in goes to %q: the session is not signed in", got)
	}
}
