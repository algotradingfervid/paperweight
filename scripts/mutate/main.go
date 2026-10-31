// Command mutate runs a mutation pass over the Go lines and SQL queries
// changed since a base revision and reports the mutants no test kills
// (CD-299).
//
// Usage, from the repository root:
//
//	go run ./scripts/mutate -base <rev> -pkgs '<patterns>' [-run <test regex>]
//	    [-tags <tags>] [-j 4] [-timeout 5m] [-files <glob>] [-json <file>]
//
// Touched lines are the new-side lines of `git diff -U0 <base>` (committed,
// staged and unstaged) plus every line of untracked files, in the packages
// the -pkgs patterns list. Go mutants come from touched lines of *.go files
// that are not _test.go, *_templ.go, generated or under dbq/ or testdata/.
// -files keeps only files whose repo path or base name matches the glob.
//
// Go mutants, one change each, on touched lines only:
//
//	== <-> !=, < <-> <=, > <-> >=, && <-> ||
//	if <cond>    -> if (<cond>) || true, if (<cond>) && false
//	!x           -> x
//	n            -> n+1 (integer literal; not make's capacity argument)
//	guard body removed: the body of an if that ends in `return <error>` in a
//	    function whose last result is error is wrapped in `if false { }`.
//	    Its `if false` mutant is then skipped unless the if has an else (they
//	    would be the same mutant).
//
// The condition stays in the code, so `if _, ok := m[k]; ok` still builds.
// A plain error pass-through with no init statement (`err := f()` earlier,
// then `if err != nil { return ..., err }`, or fmt.Errorf with err among its
// arguments; CD-185) gets neither the guard removal nor the `if false`
// mutant; its line is counted and listed as exempt. With an init statement
// (`if err := s.CanTx(...); err != nil { return err }`) the call is a guard
// and gets every mutant.
//
// SQL mutants: when a query in <pkg>/queries/*.sql is touched (a line from its
// "-- name:" line to its closing ";"), the query's const in <pkg>/dbq/*.sql.go
// is mutated:
//
//	= <-> != (<> -> =), < <-> <=, > <-> >=   (not the = of a SET clause)
//	each AND <term> dropped (not BETWEEN's AND)
//	each value removed from an IN (...) list of two or more values
//	each ORDER BY key flipped (ASC <-> DESC) and dropped (a single key drops
//	    the whole ORDER BY)
//	DISTINCT removed
//	LIMIT n -> LIMIT n+1
//
// SQL mutants report the .sql file's line. A touched query whose const no
// longer matches the .sql text (after folding comments, blanks, parameters
// and sqlc's expansion of a column *) stops the run: run make generate. A mutant that breaks the SQL
// at run time is killed only if a test fails.
//
// Each mutant is written to a fresh temp directory outside the repository
// and run with `go test -overlay <json> -vet=off -count=1 -timeout <timeout>
// [-run] [-tags] <the mutant's package> <-pkgs patterns>`, -j at a time. The
// working tree is never written. An unmutated baseline run goes first; if it
// fails, mutate stops with exit 2. A mutant's result is:
//
//	survived     go test passes
//	build error  a package fails to build ("[build failed]", "[setup failed]")
//	killed       a test fails ("--- FAIL"), panics (not a timeout) or dies
//	             with a runtime "fatal error:" (stack overflow, concurrent map writes)
//	timed out    "panic: test timed out" and no test failed
//	tool error   anything else (a go command error); exit 2
//
// A line comment `// mutate:ok <reason>` (in SQL: `-- mutate:ok <reason>`)
// skips the touched line it is on; skipped lines and their reasons are
// counted and listed.
//
// Output: the summary line
//
//	mutate: <n> mutants, <k> killed, <s> survived, <b> build errors, <t> timed out, <e> tool errors, <x> skipped lines, <y> exempt lines in <time>
//
// then `path:line: skipped: <reason>` per skipped line, `path:line: exempt:
// error pass-through (CD-185)` per exempt line and `path:line: <mutation>
// <result>` per mutant not killed, sorted by file and line (a tool error adds
// the first line of go's output). -json <file> writes every mutant with its
// result. Exit code 2 on a usage, git, baseline or tool error, else 1 when a
// mutant survived or timed out, else 0.
package main

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	hunkHeader = regexp.MustCompile(`^@@ -\S+ \+(\d+)(?:,(\d+))? @@`)
	generated  = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)
	queryName  = regexp.MustCompile(`^--\s*name:\s*(\w+)`)
)

// Results of a mutant run.
const (
	killed     = "killed"
	survived   = "survived"
	buildError = "build error"
	timedOut   = "timed out"
	toolError  = "tool error"
)

const exemptReason = "error pass-through (CD-185)"

// mutant is one change to one file.
type mutant struct {
	File     string  `json:"file"` // repo path reported (the .sql file for SQL mutants)
	Line     int     `json:"line"`
	Mutation string  `json:"mutation"`
	Result   string  `json:"result"`
	Seconds  float64 `json:"seconds"`
	Detail   string  `json:"detail,omitempty"` // first line of go's output for a tool error

	target     string // repo path of the file the edit applies to
	start, end int    // byte range in target replaced by repl
	repl       string
}

// skipped is a touched line carrying a mutate:ok comment, or an exempt one.
type skipped struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// report is what -json writes.
type report struct {
	Mutants  []mutant  `json:"mutants"`
	Skipped  []skipped `json:"skipped"`
	Exempt   []skipped `json:"exempt"`
	Killed   int       `json:"killed"`
	Survived int       `json:"survived"`
	Build    int       `json:"build_errors"`
	Timeouts int       `json:"timed_out"`
	Tool     int       `json:"tool_errors"`
	Seconds  float64   `json:"seconds"`
}

// config holds the flags.
type config struct {
	base, pkgs, run, tags, files, json string
	jobs                               int
	timeout                            time.Duration
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], ".", os.Stdout)
	stop()
	os.Exit(code)
}

// run mutates the repository at root and returns the exit code.
func run(ctx context.Context, args []string, root string, out io.Writer) int {
	var c config
	fset := flag.NewFlagSet("mutate", flag.ContinueOnError)
	fset.SetOutput(out)
	fset.StringVar(&c.base, "base", "", "revision to diff the working tree against (required)")
	fset.StringVar(&c.pkgs, "pkgs", "", "package patterns, space separated (required)")
	fset.StringVar(&c.run, "run", "", "go test -run regex")
	fset.StringVar(&c.tags, "tags", "", "go build tags")
	fset.StringVar(&c.files, "files", "", "only files whose repo path or base name match this glob")
	fset.StringVar(&c.json, "json", "", "write every mutant and its result to this file")
	fset.IntVar(&c.jobs, "j", 4, "mutants run in parallel")
	fset.DurationVar(&c.timeout, "timeout", 5*time.Minute, "go test -timeout per mutant")
	printf := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format, a...) }
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if c.base == "" || c.pkgs == "" || fset.NArg() > 0 || c.jobs < 1 {
		printf("ERROR usage: mutate -base <rev> -pkgs '<patterns>' [-run re] [-tags t] [-j n] [-timeout d] [-files glob] [-json file]\n")
		return 2
	}
	start := time.Now()
	root, err := filepath.Abs(root)
	if err == nil {
		// go sees paths under the real directory; overlay keys must match.
		root, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		printf("ERROR %v\n", err)
		return 2
	}

	mutants, skips, exempt, err := collect(root, c)
	if err != nil {
		printf("ERROR %v\n", err)
		return 2
	}
	if len(mutants) > 0 {
		if res, output := goTest(ctx, root, c, "", nil); res != survived {
			printf("ERROR baseline go test fails without mutants (%s):\n%s\n", res, output)
			return 2
		}
		if err := runAll(ctx, root, c, mutants); err != nil {
			printf("ERROR %v\n", err)
			return 2
		}
	}

	r := report{Mutants: mutants, Skipped: skips, Exempt: exempt, Seconds: time.Since(start).Seconds()}
	for _, m := range mutants {
		switch m.Result {
		case killed:
			r.Killed++
		case survived:
			r.Survived++
		case buildError:
			r.Build++
		case timedOut:
			r.Timeouts++
		case toolError:
			r.Tool++
		}
	}
	printf("mutate: %d mutants, %d killed, %d survived, %d build errors, %d timed out, %d tool errors, %d skipped lines, %d exempt lines in %s\n",
		len(mutants), r.Killed, r.Survived, r.Build, r.Timeouts, r.Tool, len(skips), len(exempt), time.Since(start).Round(time.Second))
	for _, s := range skips {
		printf("%s:%d: skipped: %s\n", s.File, s.Line, s.Reason)
	}
	for _, s := range exempt {
		printf("%s:%d: exempt: %s\n", s.File, s.Line, s.Reason)
	}
	for _, m := range mutants {
		switch m.Result {
		case killed:
		case toolError:
			printf("%s:%d: %s tool error: %s\n", m.File, m.Line, m.Mutation, m.Detail)
		default:
			printf("%s:%d: %s %s\n", m.File, m.Line, m.Mutation, m.Result)
		}
	}
	if c.json != "" {
		data, err := json.MarshalIndent(r, "", "  ")
		if err == nil {
			err = os.WriteFile(c.json, append(data, '\n'), 0o600)
		}
		if err != nil {
			printf("ERROR %v\n", err)
			return 2
		}
	}
	switch {
	case r.Tool > 0:
		return 2
	case r.Survived > 0 || r.Timeouts > 0:
		return 1
	}
	return 0
}

// collect lists the mutants, skipped lines and exempt lines of the touched
// files in the -pkgs packages, sorted by file and line.
func collect(root string, c config) ([]mutant, []skipped, []skipped, error) {
	dirs, err := packageDirs(root, c)
	if err != nil {
		return nil, nil, nil, err
	}
	touched, err := touchedLines(root, c.base)
	if err != nil {
		return nil, nil, nil, err
	}
	paths := make([]string, 0, len(touched))
	for p := range touched {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	var mutants []mutant
	var skips, exempt []skipped
	for _, p := range paths {
		if c.files != "" && !globMatch(c.files, p) {
			continue
		}
		var ms []mutant
		var ok map[int]string
		var ex []int
		switch {
		case strings.HasSuffix(p, ".sql") && path.Base(path.Dir(p)) == "queries":
			pkg := path.Dir(path.Dir(p))
			if !dirs[pkg] && !dirs[path.Join(pkg, "dbq")] {
				continue
			}
			ms, ok, err = sqlFileMutants(root, p, touched[p])
		case strings.HasSuffix(p, ".go") && dirs[path.Dir(p)]:
			var keep bool
			if keep, err = checked(root, p); err != nil {
				return nil, nil, nil, err
			}
			if !keep {
				continue
			}
			ms, ok, ex, err = goFileMutants(root, p, touched[p])
		default:
			continue
		}
		if err != nil {
			return nil, nil, nil, err
		}
		for l, reason := range ok {
			skips = append(skips, skipped{File: p, Line: l, Reason: reason})
		}
		for _, l := range ex {
			exempt = append(exempt, skipped{File: p, Line: l, Reason: exemptReason})
		}
		mutants = append(mutants, ms...)
	}
	slices.SortStableFunc(mutants, func(a, b mutant) int {
		if a.File != b.File {
			return strings.Compare(a.File, b.File)
		}
		return a.Line - b.Line
	})
	byLine := func(a, b skipped) int {
		if a.File != b.File {
			return strings.Compare(a.File, b.File)
		}
		return a.Line - b.Line
	}
	slices.SortFunc(skips, byLine)
	slices.SortFunc(exempt, byLine)
	return mutants, skips, exempt, nil
}

// globMatch reports whether the glob matches the repo path or its base name.
func globMatch(glob, p string) bool {
	if ok, _ := path.Match(glob, p); ok {
		return true
	}
	ok, _ := path.Match(glob, path.Base(p))
	return ok
}

// packageDirs returns the repo-relative directories of the -pkgs packages.
func packageDirs(root string, c config) (map[string]bool, error) {
	args := []string{"list", "-f", "{{.Dir}}"}
	if c.tags != "" {
		args = append(args, "-tags", c.tags)
	}
	cmd := exec.Command("go", append(args, strings.Fields(c.pkgs)...)...) //nolint:gosec // G204: go list with the caller's own flags.
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	dirs := map[string]bool{}
	for _, d := range strings.Fields(string(data)) {
		if real, err := filepath.EvalSymlinks(d); err == nil {
			d = real
		}
		rel, err := filepath.Rel(root, d)
		if err != nil {
			return nil, err
		}
		dirs[filepath.ToSlash(rel)] = true
	}
	return dirs, nil
}

// touchedLines returns the lines added or changed since base in *.go and
// *.sql files, by repo path, counting every line of untracked files as new.
func touchedLines(root, base string) (map[string]map[int]bool, error) {
	// Fixed prefixes: diff.noprefix or diff.mnemonicPrefix would change "+++ b/".
	diff, err := git(root, "diff", "-U0", "--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/", base, "--", "*.go", "*.sql")
	if err != nil {
		return nil, err
	}
	touched := parseHunks(diff)
	others, err := git(root, "ls-files", "-z", "--others", "--exclude-standard", "--", "*.go", "*.sql")
	if err != nil {
		return nil, err
	}
	for p := range strings.SplitSeq(strings.TrimSuffix(others, "\x00"), "\x00") {
		if p == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, p)) //nolint:gosec // G304: a path git listed inside the repo.
		if err != nil {
			return nil, err
		}
		lines := map[int]bool{}
		for i := range bytes.Count(data, []byte("\n")) + 1 {
			lines[i+1] = true
		}
		touched[p] = lines
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
		if p, ok := strings.CutPrefix(line, "+++ "); ok {
			cur = nil
			if p, ok = strings.CutPrefix(p, "b/"); ok {
				cur = map[int]bool{}
				touched[p] = cur
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

// checked reports whether the Go file at repo path p is mutated.
func checked(root, p string) (bool, error) {
	if strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, "_templ.go") {
		return false, nil
	}
	for _, part := range strings.Split(path.Dir(p), "/") {
		if part == "dbq" || part == "testdata" {
			return false, nil
		}
	}
	gen, err := isGenerated(filepath.Join(root, p))
	return !gen, err
}

// isGenerated reports whether the file has a "Code generated ... DO NOT
// EDIT." line before its package clause. A deleted file counts as generated,
// so it is skipped.
func isGenerated(p string) (bool, error) {
	f, err := os.Open(p) //nolint:gosec // G304: a path git listed inside the repo.
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

// goFileMutants returns the mutants on the touched lines of the Go file at
// repo path p, its touched mutate:ok lines (mutants on those are dropped) and
// its touched exempt lines.
func goFileMutants(root, p string, touched map[int]bool) ([]mutant, map[int]string, []int, error) {
	src, err := os.ReadFile(filepath.Join(root, p)) //nolint:gosec // G304: a path git listed inside the repo.
	if err != nil {
		return nil, nil, nil, err
	}
	ms, ok, ex, err := goMutants(src)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%s: %v", p, err)
	}
	for l := range ok {
		if !touched[l] {
			delete(ok, l)
		}
	}
	var keep []mutant
	for _, m := range ms {
		if _, skip := ok[m.Line]; touched[m.Line] && !skip {
			m.File, m.target = p, p
			keep = append(keep, m)
		}
	}
	var exempt []int
	for _, l := range ex {
		if _, skip := ok[l]; touched[l] && !skip {
			exempt = append(exempt, l)
		}
	}
	return keep, ok, exempt, nil
}

var swaps = map[token.Token]token.Token{
	token.EQL: token.NEQ, token.NEQ: token.EQL,
	token.LSS: token.LEQ, token.LEQ: token.LSS,
	token.GTR: token.GEQ, token.GEQ: token.GTR,
	token.LAND: token.LOR, token.LOR: token.LAND,
}

// goMutants returns the mutants of a Go source file in source order, with
// line numbers, the lines carrying a `// mutate:ok` comment with its reason,
// and the lines of exempt error pass-throughs.
func goMutants(src []byte) ([]mutant, map[int]string, []int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, nil, err
	}
	tf := fset.File(f.Pos())
	var ms []mutant
	add := func(pos token.Pos, start, end int, repl, desc string) {
		ms = append(ms, mutant{Line: tf.Line(pos), Mutation: desc, start: start, end: end, repl: repl})
	}
	off := tf.Offset
	text := func(n ast.Node) string { return string(src[off(n.Pos()):off(n.End())]) }

	var exempt []int
	capacity := map[ast.Node]bool{}
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		stack = append(stack, n)
		switch n := n.(type) {
		case *ast.BinaryExpr:
			if to, ok := swaps[n.Op]; ok {
				s := off(n.OpPos)
				add(n.OpPos, s, s+len(n.Op.String()), to.String(), n.Op.String()+" -> "+to.String())
			}
		case *ast.UnaryExpr:
			if n.Op == token.NOT {
				s := off(n.OpPos)
				add(n.OpPos, s, s+1, "", "!"+text(n.X)+" -> "+text(n.X))
			}
		case *ast.CallExpr:
			if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "make" && len(n.Args) == 3 {
				capacity[n.Args[2]] = true
			}
		case *ast.BasicLit:
			if n.Kind != token.INT || capacity[n] {
				break
			}
			if v, err := strconv.ParseInt(n.Value, 0, 64); err == nil {
				repl := strconv.FormatInt(v+1, 10)
				add(n.Pos(), off(n.Pos()), off(n.End()), repl, n.Value+" -> "+repl)
			}
		case *ast.IfStmt:
			if !ifMutants(n, enclosingResults(stack), add, off, text) {
				exempt = append(exempt, tf.Line(n.Cond.Pos()))
			}
		}
		return true
	})
	slices.SortStableFunc(ms, func(a, b mutant) int { return a.start - b.start })

	ok := map[int]string{}
	for _, g := range f.Comments {
		for _, c := range g.List {
			if rest, found := strings.CutPrefix(c.Text, "// mutate:ok"); found {
				ok[tf.Line(c.Pos())] = reasonOf(rest)
			}
		}
	}
	return ms, ok, exempt, nil
}

func reasonOf(rest string) string {
	if r := strings.TrimSpace(rest); r != "" {
		return r
	}
	return "(no reason given)"
}

// ifMutants adds the condition and guard-removal mutants of an if. It
// returns false for an exempt error pass-through.
func ifMutants(n *ast.IfStmt, results *ast.FieldList, add func(token.Pos, int, int, string, string), off func(token.Pos) int, text func(ast.Node) string) bool {
	cs, ce := off(n.Cond.Pos()), off(n.Cond.End())
	full := text(n.Cond)
	cond := full
	if len(cond) > 40 {
		cond = cond[:37] + "..."
	}
	cond = strings.Join(strings.Fields(cond), " ")
	exempt := n.Init == nil && passThrough(n.Body)
	guard := false
	if len(n.Body.List) > 0 && !exempt && returnsError(results) {
		if ret, ok := n.Body.List[len(n.Body.List)-1].(*ast.ReturnStmt); ok && len(ret.Results) > 0 {
			if id, isIdent := ret.Results[len(ret.Results)-1].(*ast.Ident); !isIdent || id.Name != "nil" {
				guard = true
				// Kept as dead code, so its imports and variables stay used.
				bs, be := off(n.Body.Lbrace)+1, off(n.Body.Rbrace)
				add(ret.Pos(), bs, be, "if false {"+text(n.Body)[1:be-bs+1]+"}", "guard body removed (if "+cond+")")
			}
		}
	}
	if id, ok := n.Cond.(*ast.Ident); ok && (id.Name == "true" || id.Name == "false") {
		return !exempt
	}
	add(n.Cond.Pos(), cs, ce, "("+full+") || true", "if "+cond+" -> true")
	if !exempt && (!guard || n.Else != nil) {
		add(n.Cond.Pos(), cs, ce, "("+full+") && false", "if "+cond+" -> false")
	}
	return !exempt
}

// enclosingResults returns the results of the innermost function on the
// stack.
func enclosingResults(stack []ast.Node) *ast.FieldList {
	for i := len(stack) - 1; i >= 0; i-- {
		switch fn := stack[i].(type) {
		case *ast.FuncDecl:
			return fn.Type.Results
		case *ast.FuncLit:
			return fn.Type.Results
		}
	}
	return nil
}

// returnsError reports whether the last result type is error.
func returnsError(results *ast.FieldList) bool {
	if results == nil || len(results.List) == 0 {
		return false
	}
	id, ok := results.List[len(results.List)-1].Type.(*ast.Ident)
	return ok && id.Name == "error"
}

// passThrough reports whether the body is only `return ..., err` or
// `return ..., fmt.Errorf(..., err)` (CD-185).
func passThrough(body *ast.BlockStmt) bool {
	if len(body.List) != 1 {
		return false
	}
	ret, ok := body.List[0].(*ast.ReturnStmt)
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

func isErr(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "err"
}

// runAll runs the mutants, jobs at a time, and sets their results.
func runAll(ctx context.Context, root string, c config, mutants []mutant) error {
	tmp, err := os.MkdirTemp("", "mutate-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	sources := map[string][]byte{}
	for _, m := range mutants {
		if sources[m.target] == nil {
			data, err := os.ReadFile(filepath.Join(root, m.target)) //nolint:gosec // G304: a path git listed inside the repo.
			if err != nil {
				return err
			}
			sources[m.target] = data
		}
	}
	work := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for range c.jobs {
		wg.Go(func() {
			for i := range work {
				if err := runOne(ctx, root, c, tmp, i, &mutants[i], sources[mutants[i].target]); err != nil {
					mu.Lock()
					firstErr = cmp.Or(firstErr, err)
					mu.Unlock()
				}
			}
		})
	}
	for i := range mutants {
		if ctx.Err() != nil {
			break
		}
		work <- i
	}
	close(work)
	wg.Wait()
	if firstErr == nil && ctx.Err() != nil {
		firstErr = ctx.Err()
	}
	return firstErr
}

// runOne writes mutant m into its own directory under tmp and runs go test
// with an overlay.
func runOne(ctx context.Context, root string, c config, tmp string, i int, m *mutant, src []byte) error {
	dir := filepath.Join(tmp, strconv.Itoa(i))
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	mutated := slices.Concat(src[:m.start], []byte(m.repl), src[m.end:])
	file := filepath.Join(dir, filepath.Base(m.target))
	if err := os.WriteFile(file, mutated, 0o600); err != nil {
		return err
	}
	overlay, err := json.Marshal(map[string]map[string]string{
		"Replace": {filepath.Join(root, filepath.FromSlash(m.target)): file},
	})
	if err != nil {
		return err
	}
	jsonPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(jsonPath, overlay, 0o600); err != nil {
		return err
	}
	start := time.Now()
	var output string
	m.Result, output = goTest(ctx, root, c, jsonPath, []string{"./" + path.Dir(m.target)})
	if m.Result == toolError {
		m.Detail, _, _ = strings.Cut(strings.TrimSpace(output), "\n")
	}
	m.Seconds = time.Since(start).Seconds()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// goTest runs go test over the -pkgs patterns (and extra packages) and
// classifies the outcome.
func goTest(ctx context.Context, root string, c config, overlay string, extra []string) (string, string) {
	args := []string{"test", "-vet=off", "-count=1", "-timeout", c.timeout.String()}
	if overlay != "" {
		args = append(args, "-overlay", overlay)
	}
	if c.run != "" {
		args = append(args, "-run", c.run)
	}
	if c.tags != "" {
		args = append(args, "-tags", c.tags)
	}
	args = append(args, extra...)
	args = append(args, strings.Fields(c.pkgs)...)
	cmd := exec.CommandContext(ctx, "go", args...) //nolint:gosec // G204: go test with the caller's own flags.
	cmd.Dir = root
	// Interrupt, so go test stops its test binaries rather than orphaning them.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	output, err := cmd.CombinedOutput()
	return classify(err == nil, string(output)), string(output)
}

// classify turns a go test outcome into a result (see the package comment).
func classify(passed bool, output string) string {
	panics := strings.Count(output, "panic:") - strings.Count(output, "panic: test timed out")
	switch {
	case passed:
		return survived
	case strings.Contains(output, "[build failed]") || strings.Contains(output, "[setup failed]"):
		return buildError
	case strings.Contains(output, "--- FAIL") || panics > 0 || strings.Contains(output, "fatal error:"):
		return killed
	case strings.Contains(output, "panic: test timed out"):
		return timedOut
	default:
		return toolError
	}
}
