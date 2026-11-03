// Command handin runs a task's hand-in checks and reports each one, so the
// problems that used to reach review fail here first.
//
// Usage, from the repository root:
//
//	go run ./scripts/handin -base <rev> -pkgs "<go package patterns>" [-tags <build tags>] [-area <test-id area>]
//
// or `make handin BASE=<rev> PKGS="..." [TAGS=...] [AREA=...]`.
//
// Changed files are those that differ from base: committed, staged, unstaged
// and untracked (not ignored). Base must be an ancestor of HEAD. Every step
// runs, even after a failure:
//
//   - gofmt: `gofmt -l` on the changed .go files, except *_templ.go.
//   - generate: hashes every generated file (tracked or untracked), runs
//     `make generate`, and fails if it changed, added or removed any.
//   - test: `go test -race -count=1 -timeout 60m` on the packages, plus
//     ./internal/ui/... when a changed file is under internal/ui or is .templ.
//   - gates: the e2e gates that need no browser (TestE2E_NotPagesAreChecked,
//     TestModule_EveryDocTypeSwept), and
//   - route gate: TestE2E_EveryRouteCovered, which needs Chrome; both only
//     when a change touches internal/ui, internal/app, internal/modules,
//     internal/kernel, internal/masters or e2e. Each gate must report
//     `--- PASS`, so a renamed or deleted gate fails. When the only failure
//     is a missing Chrome, the route gate is a SKIP.
//   - checkids: `make checkids AREA=<area>`, when -area is set.
//   - lint: golangci-lint with --new-from-rev=<base> on the packages.
//
// Each step prints `PASS <name>`, `FAIL <name>` with its output under it, or
// `SKIP <name>: <reason>`; the total time comes last. The exit code is 1 when
// a step failed, 2 on a usage error and 0 otherwise.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	pass = "PASS"
	fail = "FAIL"
	skip = "SKIP"
)

// gatePrefixes are the trees whose changes can break the e2e route and sweep gates.
var gatePrefixes = []string{"internal/ui/", "internal/app/", "internal/modules/", "internal/kernel/", "internal/masters/", "e2e/"}

// step is one check. run returns its status and output; for a SKIP the
// output is the reason.
type step struct {
	name string
	run  func() (status, out string)
}

func main() {
	os.Exit(run(os.Args[1:], ".", os.Stdout))
}

// run checks the repository at root and returns the exit code.
func run(args []string, root string, out io.Writer) int {
	fset := flag.NewFlagSet("handin", flag.ContinueOnError)
	fset.SetOutput(out)
	base := fset.String("base", "", "revision the task started from (required)")
	pkgList := fset.String("pkgs", "", "space-separated go package patterns (required)")
	tags := fset.String("tags", "", "build tags for go test and lint")
	area := fset.String("area", "", "test-id area for make checkids")
	if err := fset.Parse(args); err != nil {
		return 2
	}
	pkgs := strings.Fields(*pkgList)
	if *base == "" || len(pkgs) == 0 || fset.NArg() > 0 {
		_, _ = fmt.Fprintln(out, "ERROR usage: handin -base <rev> -pkgs \"<packages>\" [-tags <tags>] [-area <area>]")
		return 2
	}
	if _, err := git(root, "merge-base", "--is-ancestor", *base, "HEAD"); err != nil {
		_, _ = fmt.Fprintf(out, "ERROR usage: %s is not an ancestor of HEAD (or not a revision): pass the commit the task started from\n", *base)
		return 2
	}
	files, err := changedFiles(root, *base)
	if err != nil {
		_, _ = fmt.Fprintf(out, "ERROR %v\n", err)
		return 2
	}
	var tagArgs []string
	if *tags != "" {
		tagArgs = []string{"-tags", *tags}
	}
	cmd := func(name string, args ...string) (string, string) {
		if o, err := command(root, name, args...); err != nil {
			return fail, o + err.Error()
		}
		return pass, ""
	}
	goTest := func(args ...string) (string, error) { return command(root, "go", args...) }

	return report([]step{
		{"gofmt", func() (string, string) {
			targets := gofmtFiles(root, files)
			if len(targets) == 0 {
				return skip, "no changed .go files"
			}
			o, err := command(root, "gofmt", append([]string{"-l"}, targets...)...)
			if err != nil || o != "" {
				return fail, o + errText(err)
			}
			return pass, ""
		}},
		{"generate", func() (string, string) {
			before, err := hashGenerated(root)
			if err != nil {
				return fail, err.Error()
			}
			if o, err := command(root, "make", "generate"); err != nil {
				return fail, o + err.Error()
			}
			after, err := hashGenerated(root)
			if err != nil {
				return fail, err.Error()
			}
			if d := hashDiff(before, after); len(d) > 0 {
				return fail, "make generate changed generated files (commit them):\n" + strings.Join(d, "\n")
			}
			return pass, ""
		}},
		{"test", func() (string, string) {
			args := append([]string{"test", "-race", "-count=1", "-timeout", "60m"}, tagArgs...)
			return cmd("go", append(args, testPkgs(pkgs, files)...)...)
		}},
		{"gates", func() (string, string) {
			return gateStep(files, []string{"TestE2E_NotPagesAreChecked", "TestModule_EveryDocTypeSwept"}, false, goTest)
		}},
		{"route gate", func() (string, string) {
			return gateStep(files, []string{"TestE2E_EveryRouteCovered"}, true, goTest)
		}},
		{"checkids", func() (string, string) {
			if *area == "" {
				return skip, "AREA not set"
			}
			return cmd("make", "checkids", "AREA="+*area)
		}},
		{"lint", func() (string, string) {
			args := []string{"run", "--allow-serial-runners", "--new-from-rev=" + *base}
			if *tags != "" {
				args = append(args, "--build-tags", *tags)
			}
			return cmd("bin/tools/golangci-lint", append(args, pkgs...)...)
		}},
	}, out)
}

// report runs every step, prints its result and returns 1 if any failed.
func report(steps []step, out io.Writer) int {
	start := time.Now()
	code := 0
	for _, s := range steps {
		t := time.Now()
		status, o := s.run()
		switch status {
		case skip:
			_, _ = fmt.Fprintf(out, "%s %s: %s\n", status, s.name, o)
			continue
		case fail:
			code = 1
		}
		_, _ = fmt.Fprintf(out, "%s %s (%s)\n", status, s.name, time.Since(t).Round(100*time.Millisecond))
		if status == fail {
			for l := range strings.SplitSeq(strings.TrimRight(o, "\n"), "\n") {
				_, _ = fmt.Fprintf(out, "    %s\n", l)
			}
		}
	}
	_, _ = fmt.Fprintf(out, "total %s\n", time.Since(start).Round(time.Second))
	return code
}

// command runs name in root and returns its combined output.
func command(root, name string, args ...string) (string, error) {
	c := exec.Command(name, args...) //nolint:gosec // G204: fixed tools; the arguments are the caller's own flags.
	c.Dir = root
	o, err := c.CombinedOutput()
	return string(o), err
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// changedFiles returns the repo paths that differ from base, in the working
// tree (committed, staged or not, deleted, both sides of a rename) or
// untracked and not ignored, sorted.
func changedFiles(root, base string) ([]string, error) {
	diff, err := git(root, "diff", "-z", "--name-only", "--no-renames", base)
	if err != nil {
		return nil, err
	}
	others, err := git(root, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	files := slices.Concat(splitZ(diff), splitZ(others))
	slices.Sort(files)
	return slices.Compact(files), nil
}

func git(root string, args ...string) (string, error) {
	o, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output() //nolint:gosec // G204: fixed git subcommands; base is the caller's own flag.
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(o), nil
}

func splitZ(s string) []string {
	var out []string
	for p := range strings.SplitSeq(s, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// gofmtFiles returns the changed .go files that still exist, except *_templ.go.
func gofmtFiles(root string, files []string) []string {
	var out []string
	for _, f := range files {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_templ.go") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			out = append(out, f)
		}
	}
	return out
}

// generated reports whether path is a file `make generate` writes.
func generated(p string) bool {
	if strings.HasPrefix(p, "dbq/") || strings.Contains(p, "/dbq/") || strings.HasSuffix(p, "_templ.go") {
		return true
	}
	if ok, _ := path.Match("internal/ui/static/*", p); ok {
		return true
	}
	switch p {
	case "internal/ui/assets_gen.go", "internal/ui/icons/icons_gen.go", "internal/platform/brand/brand_gen.go":
		return true
	}
	return false
}

// hashGenerated returns the sha256 of every generated file in the working
// tree, tracked or untracked, by repo path.
func hashGenerated(root string) (map[string]string, error) {
	list, err := git(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	hashes := map[string]string{}
	for _, p := range splitZ(list) {
		if !generated(p) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, p)) //nolint:gosec // G304: a path git listed inside the repo.
		if os.IsNotExist(err) {
			continue // deleted in the working tree
		}
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		hashes[p] = hex.EncodeToString(sum[:])
	}
	return hashes, nil
}

// hashDiff lists the paths whose hash changed, that disappeared or that are
// new in after, sorted by path.
func hashDiff(before, after map[string]string) []string {
	var paths []string
	for p := range before {
		paths = append(paths, p)
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	var out []string
	for _, p := range paths {
		b, inBefore := before[p]
		a, inAfter := after[p]
		switch {
		case !inAfter:
			out = append(out, "removed: "+p)
		case !inBefore:
			out = append(out, "new: "+p)
		case a != b:
			out = append(out, "changed: "+p)
		}
	}
	return out
}

// touchesUI reports whether a file is under internal/ui or is a .templ file.
func touchesUI(files []string) bool {
	return slices.ContainsFunc(files, func(f string) bool {
		return strings.HasPrefix(f, "internal/ui/") || strings.HasSuffix(f, ".templ")
	})
}

// touchesGates reports whether a file is under one of gatePrefixes.
func touchesGates(files []string) bool {
	return slices.ContainsFunc(files, func(f string) bool {
		return slices.ContainsFunc(gatePrefixes, func(p string) bool { return strings.HasPrefix(f, p) })
	})
}

// testPkgs is pkgs plus ./internal/ui/... when a UI file changed, where the
// assets and UI lint tests live.
func testPkgs(pkgs, files []string) []string {
	const ui = "./internal/ui/..."
	if touchesUI(files) && !slices.Contains(pkgs, ui) {
		return append(slices.Clone(pkgs), ui)
	}
	return pkgs
}

// gateStep runs the named e2e gates through goTest when a change can affect
// them, and fails unless each reports `--- PASS`: a renamed, deleted or
// skipped gate fails too. For a browser gate (browser), a run whose only
// failure is a missing Chrome is a SKIP.
func gateStep(files, names []string, browser bool, goTest func(args ...string) (string, error)) (string, string) {
	if !touchesGates(files) {
		return skip, "no change under " + strings.Join(gatePrefixes, ", ")
	}
	o, err := goTest("test", "-v", "-tags", "e2e", "-count=1", "-timeout", "10m", "-run", "^("+strings.Join(names, "|")+")$", "./e2e/")
	if err != nil {
		if browser && browserMissing(o) {
			return skip, "Chrome not found, so " + strings.Join(names, ", ") + " did not run"
		}
		return fail, o + err.Error()
	}
	if missing := notPassed(o, names); len(missing) > 0 {
		return fail, o + "did not pass (renamed, deleted or skipped?): " + strings.Join(missing, ", ")
	}
	return pass, ""
}

// notPassed returns the names with no `--- PASS: <name> ` line in a go test -v output.
func notPassed(out string, names []string) []string {
	var missing []string
	for _, n := range names {
		if !strings.Contains(out, "--- PASS: "+n+" ") {
			missing = append(missing, n)
		}
	}
	return missing
}

// testFailLine is a t.Error or t.Fatal line from a test file.
var testFailLine = regexp.MustCompile(`(?m)^\s+\S+_test\.go:\d+:`)

// browserMissing reports whether a missing Chrome is the only failure: the
// "Chrome not found" line, one failed test and no failure line from a test
// file (a route check that failed before the browser started).
func browserMissing(out string) bool {
	return strings.Contains(out, "Chrome not found") &&
		strings.Count(out, "--- FAIL") == 1 &&
		!testFailLine.MatchString(out)
}
