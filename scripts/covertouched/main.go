// Command covertouched reports Go statements added or changed since a base
// revision that no test runs.
//
// Usage, from the repository root:
//
//	go test -coverprofile=cover.out ./internal/<pkg>/...
//	go run ./scripts/covertouched -base <rev> -profile cover.out
//
// or `make covertouched BASE=<rev> PKGS=./internal/<pkg>/...`.
//
// Touched lines are the new-side lines of `git diff -U0 <base>` (working tree,
// staged or not) plus every line of untracked files; deleted-only hunks add
// none. Only these files count: *.go files that are not _test.go, *_templ.go,
// generated (a "// Code generated ... DO NOT EDIT." line before the package
// clause) or under a dbq/ or testdata/ directory. A touched file whose package
// (directory) has no block in the profile prints
//
//	path: not in profile, not checked
//
// and does not change the exit code.
//
// A touched line is uncovered when a coverage block with count 0 holds code of
// its own on that line: the block's part of the line is not blank, a comment,
// "{" or "}". So a changed `if x {` line whose body is untested is not
// reported, only the body's touched lines are. Each uncovered line prints as
//
//	path:line: not covered by any test
//
// and the exit code is 1 (2 on a usage or read error, 0 when all are covered).
// When nothing is uncovered or exempt, the last line is
//
//	covertouched: all touched statements covered (<n> blocks)
//
// and when exempt blocks (below) did not, it is
//
//	covertouched: <c>/<n> touched blocks covered; <k> untested error returns exempt (CD-185): a.go:12, :30, b.go:7
//
// Error returns are exempt: a block whose only statement is a return whose
// last value is the variable err (`return err`, `return x, err`) or a
// fmt.Errorf call with err among its arguments (`return x, fmt.Errorf("...: %w",
// err)`). This is typically the body of `if err != nil { return ..., err }`.
// Any other block is checked: a refusal (`return ErrX`, `return
// &ValidationError{...}`), an error wrapped by any other function, or an
// `if err != nil` body that does more than return (logging, cleanup).
// With -strict, untested error returns are reported as uncovered too.
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// block is one coverage block from a profile, with a repo-relative path.
type block struct {
	path                          string
	startLine, startCol           int
	endLine, endCol, stmts, count int
}

var (
	hunkHeader = regexp.MustCompile(`^@@ -\S+ \+(\d+)(?:,(\d+))? @@`)
	generated  = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)
	profLine   = regexp.MustCompile(`^(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)$`)
)

func main() {
	os.Exit(run(os.Args[1:], ".", os.Stdout))
}

// run checks the repository at root and returns the exit code.
func run(args []string, root string, out io.Writer) int {
	fset := flag.NewFlagSet("covertouched", flag.ContinueOnError)
	fset.SetOutput(out)
	base := fset.String("base", "", "revision to diff the working tree against (required)")
	profile := fset.String("profile", "", "coverage profile from go test -coverprofile (required)")
	strict := fset.Bool("strict", false, "report untested error returns as uncovered too")
	printf := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format, a...) }
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if *base == "" || *profile == "" || fset.NArg() > 0 {
		printf("ERROR usage: covertouched [-strict] -base <rev> -profile <cover.out>\n")
		return 2
	}

	touched, err := touchedLines(root, *base)
	if err != nil {
		printf("ERROR reading the git diff: %v\n", err)
		return 2
	}
	module, err := modulePath(root)
	if err != nil {
		printf("ERROR reading go.mod: %v\n", err)
		return 2
	}
	data, err := os.ReadFile(*profile) //nolint:gosec // G304: the profile path is the caller's own flag.
	if err != nil {
		printf("ERROR %v\n", err)
		return 2
	}
	blocks, err := parseProfile(data, module)
	if err != nil {
		printf("ERROR reading the profile: %v\n", err)
		return 2
	}
	profiled := map[string]bool{}
	for _, b := range blocks {
		profiled[filepath.Dir(b.path)] = true
	}
	var missing []string
	for path := range touched {
		keep, err := checked(root, path)
		if err != nil {
			printf("ERROR %v\n", err)
			return 2
		}
		if !keep {
			delete(touched, path)
		} else if !profiled[filepath.Dir(path)] {
			missing = append(missing, path)
		}
	}
	slices.Sort(missing)
	for _, path := range missing {
		printf("%s: not in profile, not checked\n", path)
	}

	r, err := uncovered(root, touched, blocks, *strict)
	if err != nil {
		printf("ERROR %v\n", err)
		return 2
	}
	for _, l := range r.findings {
		printf("%s: not covered by any test\n", l)
	}
	if len(r.exempt) > 0 {
		printf("covertouched: %d/%d touched blocks covered; %d untested error returns exempt (CD-185): %s\n",
			r.covered, r.n, len(r.exempt), joinLocs(r.exempt))
	}
	if len(r.findings) > 0 {
		return 1
	}
	if len(r.exempt) == 0 {
		printf("covertouched: all touched statements covered (%d blocks)\n", r.n)
	}
	return 0
}

// joinLocs joins sorted "path:line" strings, naming each path once:
// "a.go:12, :30, b.go:7".
func joinLocs(locs []string) string {
	var b strings.Builder
	prev := ""
	for i, loc := range locs {
		if i > 0 {
			b.WriteString(", ")
		}
		path, line, _ := strings.Cut(loc, ":")
		if path != prev {
			b.WriteString(path)
		}
		b.WriteString(":" + line)
		prev = path
	}
	return b.String()
}

// touchedLines returns the lines added or changed since base, by repo path,
// counting every line of untracked files as new.
func touchedLines(root, base string) (map[string]map[int]bool, error) {
	// Fixed prefixes: diff.noprefix or diff.mnemonicPrefix would change "+++ b/".
	diff, err := git(root, "diff", "-U0", "--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/", base, "--", "*.go")
	if err != nil {
		return nil, err
	}
	touched := parseHunks(diff)
	others, err := git(root, "ls-files", "-z", "--others", "--exclude-standard", "--", "*.go")
	if err != nil {
		return nil, err
	}
	for path := range strings.SplitSeq(strings.TrimSuffix(others, "\x00"), "\x00") {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, path)) //nolint:gosec // G304: a path git listed inside the repo.
		if err != nil {
			return nil, err
		}
		lines := map[int]bool{}
		for i := range bytes.Count(data, []byte("\n")) + 1 {
			lines[i+1] = true
		}
		touched[path] = lines
	}
	return touched, nil
}

func git(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...) //nolint:gosec // G204: fixed git subcommands; base is the caller's own flag.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// parseHunks returns the new-side line numbers of each hunk in a
// `git diff -U0` output, by the file's new path.
func parseHunks(diff string) map[string]map[int]bool {
	touched := map[string]map[int]bool{}
	var cur map[int]bool
	for _, line := range strings.Split(diff, "\n") {
		if path, ok := strings.CutPrefix(line, "+++ "); ok {
			cur = nil
			if path, ok = strings.CutPrefix(path, "b/"); ok {
				cur = map[int]bool{}
				touched[path] = cur
			}
			continue
		}
		m := hunkHeader.FindStringSubmatch(line)
		if m == nil || cur == nil {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		count := 1
		if m[2] != "" {
			count, _ = strconv.Atoi(m[2])
		}
		for i := range count {
			cur[start+i] = true
		}
	}
	return touched
}

// checked reports whether path is a file whose coverage counts.
func checked(root, path string) (bool, error) {
	if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_templ.go") {
		return false, nil
	}
	for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(path)), "/") {
		if part == "dbq" || part == "testdata" {
			return false, nil
		}
	}
	gen, err := isGenerated(filepath.Join(root, path))
	return !gen, err
}

// isGenerated reports whether the file has a "Code generated ... DO NOT
// EDIT." line before its package clause. A deleted file counts as generated,
// so it is skipped.
func isGenerated(path string) (bool, error) {
	f, err := os.Open(path) //nolint:gosec // G304: a path git listed inside the repo.
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if generated.MatchString(line) {
			return true, nil
		}
		if strings.HasPrefix(line, "package ") {
			break
		}
	}
	return false, sc.Err()
}

// modulePath returns the module path declared in root/go.mod.
func modulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod")) //nolint:gosec // G304: the repo's own go.mod.
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if mod, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(mod), `"`), nil
		}
	}
	return "", fmt.Errorf("no module line in go.mod")
}

// parseProfile reads a coverage profile, keeping the blocks of files inside
// module with their paths relative to the module root. A block listed more
// than once (several test binaries) is merged, adding the counts.
func parseProfile(data []byte, module string) ([]block, error) {
	var blocks []block
	index := map[block]int{}
	for n, line := range strings.Split(string(data), "\n") {
		if n == 0 && strings.HasPrefix(line, "mode:") || line == "" {
			continue
		}
		m := profLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("line %d: %q is not a coverage block", n+1, line)
		}
		path, ok := strings.CutPrefix(m[1], module+"/")
		if !ok {
			continue
		}
		var v [6]int
		for i := range v {
			v[i], _ = strconv.Atoi(m[i+2])
		}
		b := block{path: path, startLine: v[0], startCol: v[1], endLine: v[2], endCol: v[3], stmts: v[4]}
		if i, seen := index[b]; seen {
			blocks[i].count += v[5]
			continue
		}
		index[b] = len(blocks)
		b.count = v[5]
		blocks = append(blocks, b)
	}
	return blocks, nil
}

// source is a parsed file and its lines.
type source struct {
	file  *ast.File
	lines []string
}

// result is what uncovered found.
type result struct {
	findings []string // touched code lines of uncovered blocks, sorted "path:line"
	exempt   []string // first touched code line of each untested error return, sorted
	n        int      // blocks that overlap a touched line
	covered  int      // of those, the blocks that ran
}

// uncovered checks the touched lines against the blocks. With strict, untested
// error returns are findings instead of exempt.
func uncovered(root string, touched map[string]map[int]bool, blocks []block, strict bool) (result, error) {
	var r result
	bad := map[string]bool{}
	files := map[string]*source{}
	tokens := token.NewFileSet()
	for _, b := range blocks {
		lines := touched[b.path]
		var hit []int
		for l := b.startLine; l <= b.endLine; l++ {
			if lines[l] {
				hit = append(hit, l)
			}
		}
		if len(hit) == 0 {
			continue
		}
		r.n++
		if b.count > 0 {
			r.covered++
			continue
		}
		src, ok := files[b.path]
		if !ok {
			path := filepath.Join(root, b.path)
			data, err := os.ReadFile(path) //nolint:gosec // G304: a path git listed inside the repo.
			if err != nil {
				return result{}, err
			}
			file, err := parser.ParseFile(tokens, path, data, parser.SkipObjectResolution)
			if err != nil {
				return result{}, err
			}
			src = &source{file: file, lines: strings.Split(string(data), "\n")}
			files[b.path] = src
		}
		exempt := !strict && errorOnly(tokens, src.file, b)
		for _, l := range hit {
			if !hasCode(src.lines, b, l) {
				continue
			}
			loc := fmt.Sprintf("%s:%d", b.path, l)
			if exempt {
				r.exempt = append(r.exempt, loc)
				break
			}
			bad[loc] = true
		}
	}
	for loc := range bad {
		r.findings = append(r.findings, loc)
	}
	slices.SortFunc(r.findings, compareLoc)
	slices.SortFunc(r.exempt, compareLoc)
	return r, nil
}

// hasCode reports whether the block's part of line l (1-based) holds code:
// anything but blanks, a comment, "{" or "}".
func hasCode(lines []string, b block, l int) bool {
	text := lines[l-1]
	if l == b.endLine {
		text = text[:min(b.endCol-1, len(text))]
	}
	if l == b.startLine {
		text = text[min(b.startCol-1, len(text)):]
	}
	text = strings.TrimSpace(text)
	return text != "" && text != "{" && text != "}" && !strings.HasPrefix(text, "//")
}

// compareLoc orders "path:line" by path, then by line.
func compareLoc(a, b string) int {
	pa, la, _ := strings.Cut(a, ":")
	pb, lb, _ := strings.Cut(b, ":")
	if pa != pb {
		return strings.Compare(pa, pb)
	}
	na, _ := strconv.Atoi(la)
	nb, _ := strconv.Atoi(lb)
	return na - nb
}

// errorOnly reports whether the block's only statement is a return whose last
// value passes on err (see the package comment).
func errorOnly(tokens *token.FileSet, file *ast.File, b block) bool {
	if b.stmts != 1 {
		return false
	}
	inside := func(p token.Pos) bool {
		pos := tokens.Position(p)
		after := pos.Line > b.startLine || pos.Line == b.startLine && pos.Column >= b.startCol
		before := pos.Line < b.endLine || pos.Line == b.endLine && pos.Column <= b.endCol
		return after && before
	}
	var first ast.Stmt
	ast.Inspect(file, func(node ast.Node) bool {
		if first != nil {
			return false
		}
		stmt, ok := node.(ast.Stmt)
		if ok && !isBlock(stmt) && inside(stmt.Pos()) && inside(stmt.End()) {
			first = stmt
			return false
		}
		return true
	})
	ret, ok := first.(*ast.ReturnStmt)
	if !ok || len(ret.Results) == 0 {
		return false
	}
	last := ret.Results[len(ret.Results)-1]
	if isErr(last) {
		return true
	}
	call, ok := last.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Errorf" {
		return false
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "fmt" {
		return false
	}
	return slices.ContainsFunc(call.Args, isErr)
}

func isBlock(s ast.Stmt) bool {
	_, ok := s.(*ast.BlockStmt)
	return ok
}

func isErr(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "err"
}
