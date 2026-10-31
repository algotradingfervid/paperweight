package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // G204: fixed git subcommands in a test.
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, root, name, text string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestChangedFiles covers every way a file can differ from base: committed,
// staged, unstaged, untracked, deleted and renamed; unchanged and ignored
// files are left out.
func TestChangedFiles(t *testing.T) {
	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	for _, name := range []string{"same.go", "committed.go", "staged.go", "unstaged.go", "deleted.go", "old.go"} {
		write(t, root, name, "package x // "+name+"\n")
	}
	write(t, root, ".gitignore", "ignored.go\n")
	commit := func() {
		gitRun(t, root, "add", "-A")
		gitRun(t, root, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "c")
	}
	commit()
	gitRun(t, root, "tag", "base")
	write(t, root, "committed.go", "package x // changed\n")
	commit()
	write(t, root, "staged.go", "package x // changed\n")
	gitRun(t, root, "add", "staged.go")
	write(t, root, "unstaged.go", "package x // changed\n")
	write(t, root, "dir/untracked.go", "package x\n")
	write(t, root, "ignored.go", "package x\n")
	gitRun(t, root, "rm", "-q", "deleted.go")
	gitRun(t, root, "mv", "old.go", "new.go")

	got, err := changedFiles(root, "base")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"committed.go", "deleted.go", "dir/untracked.go", "new.go", "old.go", "staged.go", "unstaged.go"}
	if !slices.Equal(got, want) {
		t.Errorf("changedFiles = %q, want %q", got, want)
	}
}

func TestGofmtFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.go", "")
	write(t, root, "a_templ.go", "")
	write(t, root, "a.templ", "")
	write(t, root, "a_test.go", "")
	got := gofmtFiles(root, []string{"a.go", "a_templ.go", "a.templ", "a_test.go", "gone.go", "README.md"})
	if want := []string{"a.go", "a_test.go"}; !slices.Equal(got, want) {
		t.Errorf("gofmtFiles = %q, want %q", got, want)
	}
}

func TestGenerated(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"internal/kernel/auth/dbq/models.go", true},
		{"dbq/x.go", true},
		{"internal/x/dbq/sub/y.go", true},
		{"internal/x/dbqx/y.go", false},
		{"internal/x/mydbq/y.go", false},
		{"internal/ui/page_templ.go", true},
		{"internal/ui/page.templ", false},
		{"internal/ui/static/app.5e9112eb.css", true},
		{"internal/ui/static/sub/x.css", false},
		{"internal/ui/assets_gen.go", true},
		{"internal/ui/icons/icons_gen.go", true},
		{"internal/platform/brand/brand_gen.go", true},
		{"internal/platform/brand/brand.go", false},
		{"internal/other/assets_gen.go", false},
	} {
		if got := generated(tc.path); got != tc.want {
			t.Errorf("generated(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestTouchesUI(t *testing.T) {
	for _, tc := range []struct {
		files []string
		want  bool
	}{
		{[]string{"internal/ui/docscreen/form.go"}, true},
		{[]string{"internal/modules/expense/page.templ"}, true},
		{[]string{"internal/uikit/x.go"}, false},
		{[]string{"scripts/handin/main.go", "Makefile"}, false},
		{nil, false},
	} {
		if got := touchesUI(tc.files); got != tc.want {
			t.Errorf("touchesUI(%q) = %v, want %v", tc.files, got, tc.want)
		}
	}
}

func TestTouchesGates(t *testing.T) {
	for _, tc := range []struct {
		file string
		want bool
	}{
		{"internal/ui/x.go", true},
		{"internal/app/app.go", true},
		{"internal/modules/purchase/order.go", true},
		{"internal/kernel/auth/web/h.go", true},
		{"e2e/routes.go", true},
		{"internal/kernel/doc/engine.go", true},
		{"internal/kernel/approval/web/routes.go", true},
		{"internal/masters/mstkit/web/routes.go", true},
		{"internal/platform/db/db.go", false},
		{"internal/application/x.go", false},
		{"e2eish/x.go", false},
		{"scripts/handin/main.go", false},
	} {
		if got := touchesGates([]string{"README.md", tc.file}); got != tc.want {
			t.Errorf("touchesGates(%q) = %v, want %v", tc.file, got, tc.want)
		}
	}
}

func TestTestPkgs(t *testing.T) {
	for _, tc := range []struct {
		pkgs, files, want []string
	}{
		{[]string{"./scripts/handin/..."}, []string{"scripts/handin/main.go"}, []string{"./scripts/handin/..."}},
		{[]string{"./internal/modules/expense/..."}, []string{"internal/modules/expense/claim.templ"},
			[]string{"./internal/modules/expense/...", "./internal/ui/..."}},
		{[]string{"./internal/app/..."}, []string{"internal/ui/js/upload.js"}, []string{"./internal/app/...", "./internal/ui/..."}},
		{[]string{"./internal/ui/..."}, []string{"internal/ui/x.go"}, []string{"./internal/ui/..."}},
	} {
		if got := testPkgs(tc.pkgs, tc.files); !slices.Equal(got, tc.want) {
			t.Errorf("testPkgs(%q, %q) = %q, want %q", tc.pkgs, tc.files, got, tc.want)
		}
	}
}

func TestHashDiff(t *testing.T) {
	before := map[string]string{"a": "1", "b": "2", "c": "3"}
	for _, tc := range []struct {
		name  string
		after map[string]string
		want  []string
	}{
		{"same", map[string]string{"a": "1", "b": "2", "c": "3"}, nil},
		{"changed", map[string]string{"a": "1", "b": "9", "c": "3"}, []string{"changed: b"}},
		{"new", map[string]string{"a": "1", "b": "2", "c": "3", "d": "4"}, []string{"new: d"}},
		{"removed", map[string]string{"a": "1", "b": "2"}, []string{"removed: c"}},
		{"all", map[string]string{"a": "0", "b": "2", "z": "4"}, []string{"changed: a", "removed: c", "new: z"}},
	} {
		if got := hashDiff(before, tc.after); !slices.Equal(got, tc.want) {
			t.Errorf("%s: hashDiff = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestHashGenerated(t *testing.T) {
	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	write(t, root, "x/dbq/q.go", "tracked")
	write(t, root, "x/plain.go", "not generated")
	gitRun(t, root, "add", "-A")
	write(t, root, "x/page_templ.go", "untracked")
	got, err := hashGenerated(root)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for p := range got {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	if want := []string{"x/dbq/q.go", "x/page_templ.go"}; !slices.Equal(paths, want) {
		t.Fatalf("hashed %q, want %q", paths, want)
	}
	write(t, root, "x/dbq/q.go", "regenerated")
	again, err := hashGenerated(root)
	if err != nil {
		t.Fatal(err)
	}
	if d := hashDiff(got, again); !slices.Equal(d, []string{"changed: x/dbq/q.go"}) {
		t.Errorf("after an edit: %q", d)
	}
}

// TestReport runs every step even after a failure, prints one line per step
// with the failing output under it, and exits 1 when any step failed.
func TestReport(t *testing.T) {
	ran := 0
	steps := []step{
		{"one", func() (string, string) { ran++; return fail, "bad line\n" }},
		{"two", func() (string, string) { ran++; return skip, "no reason to run" }},
		{"three", func() (string, string) { ran++; return pass, "noise" }},
	}
	var out bytes.Buffer
	if code := report(steps, &out); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if ran != 3 {
		t.Errorf("ran %d steps, want 3", ran)
	}
	want := regexp.MustCompile(`^FAIL one \(\d.*\)\n    bad line\nSKIP two: no reason to run\nPASS three \(\d.*\)\ntotal \d.*\n$`)
	if !want.MatchString(out.String()) {
		t.Errorf("output:\n%s", out.String())
	}

	out.Reset()
	if code := report(steps[1:], &out); code != 0 {
		t.Errorf("no failure: exit %d, want 0", code)
	}
}

func TestBrowserMissing(t *testing.T) {
	chrome := "    browser.go:59: Chrome not found: install Chrome or Chromium to run e2e tests\n"
	for _, tc := range []struct {
		name, out string
		want      bool
	}{
		{"Chrome alone", "=== RUN   TestE2E_EveryRouteCovered\n" + chrome + "--- FAIL: TestE2E_EveryRouteCovered (1.2s)\nFAIL\n", true},
		{"route failure then Chrome", "=== RUN   TestE2E_EveryRouteCovered\n    sweep_test.go:171: routes with no screen: [\"/x\"]\n" + chrome +
			"--- FAIL: TestE2E_EveryRouteCovered (1.2s)\nFAIL\n", false},
		{"two tests failed", chrome + "--- FAIL: TestE2E_EveryRouteCovered (1s)\n--- FAIL: TestE2E_Other (1s)\n", false},
		{"no Chrome line", "    sweep_test.go:171: routes with no screen\n--- FAIL: TestE2E_EveryRouteCovered (1s)\n", false},
	} {
		if got := browserMissing(tc.out); got != tc.want {
			t.Errorf("%s: browserMissing = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNotPassed(t *testing.T) {
	names := []string{"TestA", "TestB"}
	for _, tc := range []struct {
		name, out string
		want      []string
	}{
		{"both pass", "--- PASS: TestA (0.1s)\n--- PASS: TestB (2.0s)\nPASS\n", nil},
		{"renamed or deleted", "--- PASS: TestA (0.1s)\ntesting: warning: no tests to run\nPASS\n", []string{"TestB"}},
		{"skipped", "--- PASS: TestA (0.1s)\n--- SKIP: TestB (0.0s)\n", []string{"TestB"}},
		{"prefix only", "--- PASS: TestAB (0.1s)\n--- PASS: TestB (0.1s)\n", []string{"TestA"}},
		{"none", "ok  \tx\t0.1s [no tests to run]\n", []string{"TestA", "TestB"}},
	} {
		if got := notPassed(tc.out, names); !slices.Equal(got, tc.want) {
			t.Errorf("%s: notPassed = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRun_Usage(t *testing.T) {
	for _, args := range [][]string{{}, {"-base", "x"}, {"-pkgs", "./x/..."}} {
		var out bytes.Buffer
		if code := run(args, t.TempDir(), &out); code != 2 {
			t.Errorf("run(%q) = %d, want 2", args, code)
		}
	}
}

// TestGateStep checks the gate step's own wiring: the trigger, the -run
// pattern, a gate that didn't pass and the browser SKIP.
func TestGateStep(t *testing.T) {
	ui := []string{"internal/ui/x.go"}
	names := []string{"TestA", "TestB"}
	chrome := "    browser.go:59: Chrome not found\n--- FAIL: TestA (1s)\nFAIL\n"
	for _, tc := range []struct {
		name    string
		files   []string
		browser bool
		out     string
		err     error
		want    string
	}{
		{"no trigger", []string{"scripts/x.go"}, false, "", nil, skip},
		{"all pass", ui, false, "--- PASS: TestA (0s)\n--- PASS: TestB (0s)\n", nil, pass},
		{"one missing", ui, false, "--- PASS: TestA (0s)\n", nil, fail},
		{"test failed", ui, false, "--- FAIL: TestA (0s)\n", errors.New("exit status 1"), fail},
		{"no Chrome, browser gate", ui, true, chrome, errors.New("exit status 1"), skip},
		{"no Chrome, other gate", ui, false, chrome, errors.New("exit status 1"), fail},
	} {
		var args []string
		got, out := gateStep(tc.files, names, tc.browser, func(a ...string) (string, error) {
			args = a
			return tc.out, tc.err
		})
		if got != tc.want {
			t.Errorf("%s: status %s, want %s; output:\n%s", tc.name, got, tc.want, out)
		}
		if tc.want != skip || tc.browser {
			want := []string{"test", "-v", "-tags", "e2e", "-count=1", "-timeout", "10m", "-run", "^(TestA|TestB)$", "./e2e/"}
			if !slices.Equal(args, want) {
				t.Errorf("%s: go %q, want %q", tc.name, args, want)
			}
		}
	}
}

// A base that isn't an ancestor of HEAD is a usage error, before any step runs.
func TestRun_BaseNotAncestor(t *testing.T) {
	root := t.TempDir()
	gitRun(t, root, "init", "-q")
	commit := func(text string) {
		write(t, root, "a.go", text)
		gitRun(t, root, "add", "-A")
		gitRun(t, root, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "-m", text)
	}
	commit("package a\n")
	gitRun(t, root, "tag", "first")
	commit("package a // 2\n")
	gitRun(t, root, "tag", "second")
	gitRun(t, root, "reset", "-q", "--hard", "first")

	var out bytes.Buffer
	if code := run([]string{"-base", "second", "-pkgs", "./..."}, root, &out); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if !strings.Contains(out.String(), "second is not an ancestor of HEAD") || strings.Contains(out.String(), "PASS") {
		t.Errorf("output:\n%s", out.String())
	}
}
