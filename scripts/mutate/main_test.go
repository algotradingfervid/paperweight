package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const fixture = "testdata/mod"

func TestGoMutants(t *testing.T) {
	tests := []struct {
		name, body string
		want       []string
	}{
		{"compare", "func f(a, b int) bool { return a == b }", []string{"== -> !="}},
		{"order", "func f(a, b int) bool { return a < b || a >= b }", []string{"< -> <=", "|| -> &&", ">= -> >"}},
		{"not", "func f(a bool) bool { return !a }", []string{"!a -> a"}},
		{"int", "func f() int { return 0x10 }", []string{"0x10 -> 17"}},
		{"make capacity", "func f() []int { return make([]int, 0, 8) }", []string{"0 -> 1"}},
		{"if", "func f(a bool) int { if a { return 1 }; return 2 }", []string{"if a -> true", "if a -> false", "1 -> 2", "2 -> 3"}},
		{"guard", "func f(a bool) error { if a { return errX }; return nil }", []string{"if a -> true", "guard body removed (if a)"}},
		{"guard with else", "func f(a bool) error { if a { return errX } else { g() }; return nil }",
			[]string{"if a -> true", "if a -> false", "guard body removed (if a)"}},
		{"nil return is no guard", "func f(a bool) error { if a { return nil }; return errX }", []string{"if a -> true", "if a -> false"}},
		{"no error result", "func f(a bool) string { if a { return \"x\" }; return \"\" }", []string{"if a -> true", "if a -> false"}},
		{"call in init is a guard", "func f() error { if err := g(); err != nil { return err }; return nil }",
			[]string{"if err != nil -> true", "!= -> ==", "guard body removed (if err != nil)"}},
		{"pass-through", "func f() error { err := g(); if err != nil { return err }; return nil }", []string{"if err != nil -> true", "!= -> =="}},
		{"errorf pass-through", "func f() error { err := g(); if err != nil { return fmt.Errorf(\"x: %w\", err) }; return nil }",
			[]string{"if err != nil -> true", "!= -> =="}},
		{"comma-ok", "func f(m map[int]bool) int { if _, ok := m[1]; ok { return 1 }; return 0 }",
			[]string{"1 -> 2", "if ok -> true", "if ok -> false", "1 -> 2", "0 -> 1"}},
		{"func literal", "func f() { _ = func(a bool) error { if a { return errX }; return nil } }", []string{"if a -> true", "guard body removed (if a)"}},
		{"literal condition", "func f() { if true { g() } }", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "package p\n\n" + tt.body + "\n"
			ms, _, _, err := goMutants([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, m := range ms {
				got = append(got, m.Mutation)
				mutated := src[:m.start] + m.repl + src[m.end:]
				if _, err := parser.ParseFile(token.NewFileSet(), "", mutated, 0); err != nil {
					t.Errorf("%s: mutated source does not parse: %v\n%s", m.Mutation, err, mutated)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("mutants = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGoMutants_LinesAndOK(t *testing.T) {
	src := "package p\n\nfunc f(a int) bool {\n\treturn a > 1 // mutate:ok boundary is cosmetic\n}\n\nconst c = 2 // mutate:ok\n"
	ms, ok, _, err := goMutants([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.Line != map[string]int{"> -> >=": 4, "1 -> 2": 4, "2 -> 3": 7}[m.Mutation] {
			t.Errorf("%s on line %d", m.Mutation, m.Line)
		}
	}
	want := map[int]string{4: "boundary is cosmetic", 7: "(no reason given)"}
	if fmt.Sprint(ok) != fmt.Sprint(want) {
		t.Errorf("ok = %v, want %v", ok, want)
	}
}

func TestGoMutants_ConditionKeepsCode(t *testing.T) {
	src := "package p\n\nfunc f(m map[int]bool) bool {\n\tif _, ok := m[1]; ok {\n\t\treturn true\n\t}\n\treturn false\n}\n"
	ms, _, _, err := goMutants([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range ms {
		if strings.HasPrefix(m.Mutation, "if ") {
			got = append(got, src[m.start:m.end]+" => "+m.repl)
		}
	}
	if want := []string{"ok => (ok) || true", "ok => (ok) && false"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGoMutants_ExemptLines(t *testing.T) {
	src := "package p\n\nfunc f() error {\n\terr := g()\n\tif err != nil {\n\t\treturn err\n\t}\n\tif err := g(); err != nil {\n\t\treturn err\n\t}\n\treturn nil\n}\n"
	_, _, exempt, err := goMutants([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(exempt, []int{5}) {
		t.Errorf("exempt = %v, want [5]", exempt)
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name   string
		passed bool
		output string
		want   string
	}{
		{"pass", true, "ok  \tx\t0.1s\n", survived},
		{"build", false, "# x\n./a.go:3:1: syntax error\nFAIL\tx [build failed]\n", buildError},
		{"setup", false, "FAIL\tx [setup failed]\n", buildError},
		{"test fails", false, "--- FAIL: TestA (0.00s)\nFAIL\nFAIL\tx\t0.1s\n", killed},
		{"test panics", false, "panic: runtime error: index out of range\nFAIL\tx\t0.1s\n", killed},
		{"runtime fatal error", false, "fatal error: concurrent map writes\n\ngoroutine 7 [running]:\nFAIL\tx\t0.1s\n", killed},
		{"stack overflow", false, "runtime: goroutine stack exceeds 1000000000-byte limit\nfatal error: stack overflow\nFAIL\tx\t0.1s\n", killed},
		{"timeout", false, "panic: test timed out after 2s\n\trunning tests:\nFAIL\tx\t2.0s\n", timedOut},
		{"timeout and a failure", false, "--- FAIL: TestA (0.00s)\npanic: test timed out after 2s\n", killed},
		{"exit without failure", false, "exit status 2\nFAIL\tx\t0.1s\n", toolError},
		{"go command error", false, "go: reading overlay file: open /tmp/x.json: too many open files\n", toolError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classify(tt.passed, tt.output); got != tt.want {
				t.Errorf("classify = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSQLMutants(t *testing.T) {
	tests := []struct {
		name, sql string
		want      []string // "mutation => mutated text"
	}{
		{"compare", "SELECT a FROM t WHERE a = ? AND b <> 1",
			[]string{"a = ? -> a != ? => SELECT a FROM t WHERE a != ? AND b <> 1",
				`drop "AND b <> 1" => SELECT a FROM t WHERE a = ?`,
				"b <> 1 -> b = 1 => SELECT a FROM t WHERE a = ? AND b = 1"}},
		{"ranges", "SELECT a FROM t WHERE a < 1 OR b >= 2",
			[]string{"a < 1 -> a <= 1 => SELECT a FROM t WHERE a <= 1 OR b >= 2", "b >= 2 -> b > 2 => SELECT a FROM t WHERE a < 1 OR b > 2"}},
		{"set is not a comparison", "UPDATE t SET a = ? WHERE id = ?",
			[]string{"id = ? -> id != ? => UPDATE t SET a = ? WHERE id != ?"}},
		{"between", "SELECT a FROM t WHERE a BETWEEN 1 AND 2 AND b",
			[]string{`drop "AND b" => SELECT a FROM t WHERE a BETWEEN 1 AND 2`}},
		{"and in parens", "SELECT a FROM t WHERE (a OR b AND c) AND d",
			[]string{`drop "AND c" => SELECT a FROM t WHERE (a OR b) AND d`, `drop "AND d" => SELECT a FROM t WHERE (a OR b AND c)`}},
		{"in list", "SELECT a FROM t WHERE s IN ('x', 'y', 'z')",
			[]string{"s IN drop 'x' => SELECT a FROM t WHERE s IN ('y', 'z')",
				"s IN drop 'y' => SELECT a FROM t WHERE s IN ('x', 'z')",
				"s IN drop 'z' => SELECT a FROM t WHERE s IN ('x', 'y')"}},
		{"in one value or subquery", "SELECT a FROM t WHERE s IN ('x') OR s IN (SELECT b FROM u)", nil},
		{"order by", "SELECT a FROM t ORDER BY a DESC, b LIMIT 5",
			[]string{"ORDER BY a DESC -> ASC => SELECT a FROM t ORDER BY a ASC, b LIMIT 5",
				"ORDER BY drop a DESC => SELECT a FROM t ORDER BY b LIMIT 5",
				"ORDER BY b -> DESC => SELECT a FROM t ORDER BY a DESC, b DESC LIMIT 5",
				"ORDER BY drop b => SELECT a FROM t ORDER BY a DESC LIMIT 5",
				"LIMIT 5 -> 6 => SELECT a FROM t ORDER BY a DESC, b LIMIT 6"}},
		{"single order key", "SELECT a FROM t ORDER BY a ASC;",
			[]string{"ORDER BY a ASC -> DESC => SELECT a FROM t ORDER BY a DESC;", "ORDER BY a ASC dropped => SELECT a FROM t;"}},
		{"distinct", "SELECT COUNT(DISTINCT a) FROM t",
			[]string{"DISTINCT a -> a => SELECT COUNT(a) FROM t"}},
		{"numbered parameter", "SELECT a FROM t WHERE a = ?1", []string{"a = ?1 -> a != ?1 => SELECT a FROM t WHERE a != ?1"}},
		{"strings and comments are not code", "SELECT 'a = b' FROM t -- x = y\nWHERE c", nil},
		{"limit parameter", "SELECT a FROM t LIMIT ?", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, m := range sqlMutants(tt.sql) {
				got = append(got, m.Mutation+" => "+tt.sql[:m.start]+m.repl+tt.sql[m.end:])
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("mutants =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestSameQuery(t *testing.T) {
	tests := []struct {
		sql, gen string
		want     bool
	}{
		{"SELECT * FROM t WHERE a = ?", "SELECT id, a FROM t WHERE a = ?", true},
		{"SELECT c.*, d.x FROM c", "SELECT c.id, c.y, d.x FROM c", true},
		{"INSERT INTO t (a) VALUES (?)\nRETURNING *", "INSERT INTO t (a) VALUES (?)\nRETURNING id, a", true},
		{"SELECT COUNT(*) FROM t", "SELECT COUNT(id) FROM t", false},
		{"SELECT a * 2 FROM t", "SELECT a 2 FROM t", false},
		{"SELECT * FROM t ORDER BY a", "SELECT id, a FROM t ORDER BY a DESC", false},
	}
	for _, tt := range tests {
		if got := sameQuery(tt.sql, tt.gen); got != tt.want {
			t.Errorf("sameQuery(%q, %q) = %v, want %v", tt.sql, tt.gen, got, tt.want)
		}
	}
}

func TestParseHunks(t *testing.T) {
	diff := "+++ b/a.go\n@@ -1 +2,2 @@\n@@ -9,2 +11,0 @@\n+++ /dev/null\n@@ -1 +0,0 @@\n+++ b/b.sql\n@@ -3 +3 @@\n"
	got := fmt.Sprint(parseHunks(diff))
	if want := "map[a.go:map[2:true 3:true] b.sql:map[3:true]]"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// repo copies the fixture module into a temp git repository: a first commit
// with go.mod only, then a commit with every file. It returns the root.
func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	err := os.CopyFS(root, os.DirFS(fixture))
	if err != nil {
		t.Fatal(err)
	}
	commit := func(args ...string) {
		gitRun(t, root, append([]string{"add"}, args...)...)
		gitRun(t, root, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "c")
	}
	gitRun(t, root, "init", "-q")
	commit("go.mod")
	commit("-A")
	return root
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// treeHash hashes every file under root outside .git.
func treeHash(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.Name() == ".git" {
			if err == nil {
				err = filepath.SkipDir
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p) //nolint:gosec // G304: a file in the test's temp dir.
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00", p, len(data))
		h.Write(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// mutate runs the tool in root and returns its output, exit code and -json
// report.
func mutate(t *testing.T, root string, args ...string) (string, int, report) {
	t.Helper()
	jsonPath := filepath.Join(t.TempDir(), "r.json")
	var out bytes.Buffer
	code := run(context.Background(), append([]string{"-json", jsonPath}, args...), root, &out)
	var r report
	if data, err := os.ReadFile(jsonPath); err == nil { //nolint:gosec // G304: the test's own temp file.
		if err := json.Unmarshal(data, &r); err != nil {
			t.Fatal(err)
		}
	}
	return out.String(), code, r
}

// results maps "file:line: mutation" to each mutant's result.
func results(r report) map[string]string {
	m := map[string]string{}
	for _, x := range r.Mutants {
		m[fmt.Sprintf("%s:%d: %s", x.File, x.Line, x.Mutation)] = x.Result
	}
	return m
}

// weak is the -run Weak result of every mutant in the fixture's root and dbq
// packages.
var weak = map[string]string{
	"age.go:13: 255 -> 256":                                 buildError,
	"age.go:17: >= -> >":                                    survived,
	"age.go:17: 18 -> 19":                                   survived,
	"age.go:22: if n < 0 -> true":                           killed,
	"age.go:22: < -> <=":                                    survived,
	"age.go:22: 0 -> 1":                                     survived,
	"age.go:24: guard body removed (if n < 0)":              survived,
	"age.go:33: if ok -> true":                              survived,
	"age.go:33: if ok -> false":                             killed,
	"age.go:42: if err != nil -> true":                      killed,
	"age.go:42: != -> ==":                                   killed,
	"age.go:43: 0 -> 1":                                     survived,
	"queries/q.sql:4: DISTINCT id -> id":                    survived,
	"queries/q.sql:6: owner = ? -> owner != ?":              survived,
	`queries/q.sql:7: drop "AND state IN ('open', 'held')"`: survived,
	"queries/q.sql:7: state IN drop 'open'":                 survived,
	"queries/q.sql:7: state IN drop 'held'":                 survived,
	"queries/q.sql:8: ORDER BY id DESC -> ASC":              survived,
	"queries/q.sql:8: ORDER BY id DESC dropped":             survived,
	"queries/q.sql:9: LIMIT 10 -> 11":                       survived,
	"queries/q.sql:14: owner = ?1 -> owner != ?1":           survived,
	"queries/q.sql:15: shared_by = ?1 -> shared_by != ?1":   survived,
	"queries/q.sql:16: ORDER BY id -> DESC":                 survived,
	"queries/q.sql:16: ORDER BY id dropped":                 survived,
}

func TestRun_WeakTestsLeaveSurvivors(t *testing.T) {
	root := repo(t)
	before := treeHash(t, root)
	out, code, r := mutate(t, root, "-base", "HEAD~1", "-pkgs", ". ./dbq", "-run", "Weak")
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, out)
	}
	if after := treeHash(t, root); after != before {
		t.Error("the run changed the working tree")
	}
	if got := results(r); fmt.Sprint(got) != fmt.Sprint(weak) {
		t.Errorf("results =\n%v\nwant\n%v", got, weak)
	}
	for _, line := range []string{
		"mutate: 24 mutants, 4 killed, 19 survived, 1 build errors, 0 timed out, 0 tool errors, 1 skipped lines, 1 exempt lines in ",
		"age.go:10: skipped: only documents the cap\n",
		"age.go:42: exempt: error pass-through (CD-185)\n",
		"age.go:13: 255 -> 256 build error\n",
		"age.go:17: >= -> > survived\n",
		"age.go:24: guard body removed (if n < 0) survived\n",
		"age.go:33: if ok -> true survived\n",
		"queries/q.sql:15: shared_by = ?1 -> shared_by != ?1 survived\n",
	} {
		if !strings.Contains(out, line) {
			t.Errorf("output lacks %q:\n%s", line, out)
		}
	}
	if strings.Contains(out, "if n < 0 -> true") {
		t.Errorf("killed mutant listed:\n%s", out)
	}
}

func TestRun_StrongTestsKillAll(t *testing.T) {
	root := repo(t)
	out, code, r := mutate(t, root, "-base", "HEAD~1", "-pkgs", ". ./dbq", "-run", "Strong", "-j", "2")
	if code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
	if !strings.HasPrefix(out, "mutate: 24 mutants, 23 killed, 0 survived, 1 build errors, 0 timed out, 0 tool errors, 1 skipped lines, 1 exempt lines in ") {
		t.Errorf("output:\n%s", out)
	}
	if r.Killed != 23 || r.Build != 1 {
		t.Errorf("report: %+v", r)
	}
}

// edit replaces old with new in the file at root/name.
func edit(t *testing.T, root, name, old, new string) {
	t.Helper()
	path := filepath.Join(root, name)
	data, err := os.ReadFile(path) //nolint:gosec // G304: the test's temp copy.
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(old)) {
		t.Fatalf("%s lacks %q", name, old)
	}
	if err := os.WriteFile(path, bytes.Replace(data, []byte(old), []byte(new), 1), 0o600); err != nil { //nolint:gosec // G703: the test's temp copy.
		t.Fatal(err)
	}
}

func TestRun_OnlyTouchedLines(t *testing.T) {
	root := repo(t)
	edit(t, root, "age.go", "age >= 18", "18 <= age")
	edit(t, root, "queries/q.sql", "  OR shared_by = sqlc.arg(owner)", "  OR shared_by = sqlc.arg(owner) -- mutate:ok shared rows tested elsewhere")
	out, _, r := mutate(t, root, "-base", "HEAD", "-pkgs", ". ./dbq", "-run", "Strong")
	want := map[string]string{
		"age.go:17: 18 -> 19":                         killed,
		"age.go:17: <= -> <":                          killed,
		"queries/q.sql:14: owner = ?1 -> owner != ?1": killed,
		"queries/q.sql:16: ORDER BY id -> DESC":       killed,
		"queries/q.sql:16: ORDER BY id dropped":       killed,
	}
	if got := results(r); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("results = %v, want %v", got, want)
	}
	if !strings.Contains(out, "queries/q.sql:15: skipped: shared rows tested elsewhere\n") {
		t.Errorf("output:\n%s", out)
	}
}

func TestRun_StaleDBQ(t *testing.T) {
	root := repo(t)
	edit(t, root, "queries/q.sql", "ORDER BY id;", "ORDER BY id DESC;")
	out, code, _ := mutate(t, root, "-base", "HEAD", "-pkgs", "./dbq", "-run", "Strong")
	if code != 2 || !strings.Contains(out, "ERROR queries/q.sql: query MineOrShared differs from its const in dbq/q.sql.go; run make generate") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestRun_TimeoutIsItsOwnResult(t *testing.T) {
	root := repo(t)
	out, code, r := mutate(t, root, "-base", "HEAD~1", "-pkgs", "./slow", "-run", "Slow", "-timeout", "3s")
	if code != 1 || r.Timeouts != 1 || !strings.Contains(out, " 1 timed out, ") || !strings.Contains(out, "slow/slow.go:6: !done -> done timed out\n") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestRun_ToolErrorExits2(t *testing.T) {
	root := repo(t)
	out, code, r := mutate(t, root, "-base", "HEAD~1", "-pkgs", "./quit", "-run", "Quit")
	if code != 2 || r.Tool != 2 || !strings.Contains(out, "quit/quit.go:8: if code > 0 -> true tool error: ") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestRun_FilesAndPkgsFilter(t *testing.T) {
	root := repo(t)
	_, code, r := mutate(t, root, "-base", "HEAD~1", "-pkgs", "./dbq", "-run", "Strong", "-files", "*.go")
	if code != 0 || len(r.Mutants) != 0 {
		t.Errorf("exit %d, mutants %v; want none", code, results(r))
	}
}

func TestRun_BaselineMustPass(t *testing.T) {
	root := repo(t)
	if err := os.WriteFile(filepath.Join(root, "fail_test.go"), []byte("package mod\n\nimport \"testing\"\n\nfunc TestWeakFails(t *testing.T) { t.Fatal(\"no\") }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code, _ := mutate(t, root, "-base", "HEAD~1", "-pkgs", ".", "-run", "Weak")
	if code != 2 || !strings.Contains(out, "ERROR baseline go test fails") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestRun_Usage(t *testing.T) {
	out, code, _ := mutate(t, t.TempDir(), "-base", "HEAD")
	if code != 2 || !strings.Contains(out, "ERROR usage") {
		t.Errorf("exit %d\n%s", code, out)
	}
}
