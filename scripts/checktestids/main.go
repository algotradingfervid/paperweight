// Command checktestids reports which test-case IDs in docs/tests/core have no
// matching Go test function.
//
// Usage: go run ./scripts/checktestids [--report] [--all] [--exclude-level <level>]... [AREA...]
//
// Flags may come before or after the AREA arguments.
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
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type testCase struct {
	id, priority, level, goTest string
}

var (
	caseHeading = regexp.MustCompile(`^### ([A-Z]+-[0-9]+)\b`)
	fieldLine   = regexp.MustCompile(`^- \*\*(Priority|Level|Go test):\*\*\s*(.*)$`)
	backticked  = regexp.MustCompile("`([^`]+)`")
)

type levels []string

func (l *levels) String() string     { return strings.Join(*l, ",") }
func (l *levels) Set(v string) error { *l = append(*l, v); return nil }

func main() {
	os.Exit(run(os.Args[1:], ".", os.Stdout))
}

// run checks the repository at root and returns the exit code.
func run(args []string, root string, out io.Writer) int {
	fset := flag.NewFlagSet("checktestids", flag.ContinueOnError)
	fset.SetOutput(out)
	report := fset.Bool("report", false, "print the report but always exit 0")
	all := fset.Bool("all", false, "also report missing P2 cases")
	var exclude levels
	fset.Var(&exclude, "exclude-level", "skip cases whose level's first word is this (may repeat)")

	printf := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format, a...) }

	// The flag package stops at the first positional argument, so parse
	// repeatedly to accept flags after AREA as well.
	var areas []string
	for {
		if err := fset.Parse(args); err != nil {
			return 2
		}
		if fset.NArg() == 0 {
			break
		}
		areas = append(areas, fset.Arg(0))
		args = fset.Args()[1:]
	}

	cases, err := readCases(root)
	if err != nil {
		printf("ERROR reading test docs: %v\n", err)
		return 1
	}
	funcs, err := scanTests(root)
	if err != nil {
		printf("ERROR scanning test files: %v. Fix the file so it parses.\n", err)
		return 1
	}

	failed := false
	excluded := map[string]int{}
	bare := map[string]string{} // bare Go test name -> first case ID using it
	total, implemented := 0, 0
	for _, c := range cases {
		if !inAreas(c.id, areas) {
			continue
		}
		if c.level == "" {
			printf("ERROR %s has no **Level:** line. Add one (unit, integration or e2e).\n", c.id)
			failed = true
			continue
		}
		if c.goTest == "" {
			printf("ERROR %s has no **Go test:** name. Add one in backticks, e.g. `TestArea_Behaviour`.\n", c.id)
			failed = true
			continue
		}
		first := strings.Fields(c.level)[0]
		if slices.Contains(exclude, first) {
			if c.priority == "P1" {
				excluded[first]++
			}
			continue
		}
		if first == "e2e" && !strings.HasPrefix(c.goTest, "TestE2E_") {
			printf("ERROR %s is an e2e case but its Go test %q does not start with TestE2E_. Rename it.\n", c.id, c.goTest)
			failed = true
		}
		if !strings.Contains(c.goTest, "/") {
			if other, ok := bare[c.goTest]; ok {
				printf("ERROR %s names the same Go test %s as %s. Give each case its own function or a subtest name (TestFunc/sub_name).\n", c.id, c.goTest, other)
				failed = true
			} else {
				bare[c.goTest] = c.id
			}
		}
		fn, _, _ := strings.Cut(c.goTest, "/")
		found := funcs[fn]
		if c.priority == "P1" {
			total++
			if found {
				implemented++
			}
		}
		if !found && (c.priority == "P1" || (*all && c.priority == "P2")) {
			printf("MISSING %s %s %s\n", c.id, c.goTest, c.priority)
			if c.priority == "P1" {
				failed = true
			}
		}
	}

	summary := fmt.Sprintf("P1 implemented: %d/%d", implemented, total)
	var parts []string
	for _, l := range exclude {
		parts = append(parts, fmt.Sprintf("%d %s", excluded[l], l))
	}
	if len(parts) > 0 {
		summary += " (excluded " + strings.Join(parts, ", ") + ")"
	}
	printf("%s\n", summary)

	if failed && !*report {
		return 1
	}
	return 0
}

// readCases parses every docs/tests/core/0*.md file under root.
func readCases(root string) ([]testCase, error) {
	fsys := os.DirFS(root)
	files, err := fs.Glob(fsys, "docs/tests/core/0*.md")
	if err != nil {
		return nil, err
	}
	var cases []testCase
	for _, name := range files {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		var cur *testCase
		sc := bufio.NewScanner(bytes.NewReader(data))
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "#") {
				cur = nil
				if m := caseHeading.FindStringSubmatch(line); m != nil {
					cases = append(cases, testCase{id: m[1]})
					cur = &cases[len(cases)-1]
				}
				continue
			}
			m := fieldLine.FindStringSubmatch(line)
			if cur == nil || m == nil {
				continue
			}
			value := strings.TrimSpace(m[2])
			switch m[1] {
			case "Priority":
				cur.priority = value
			case "Level":
				cur.level = value
			case "Go test":
				if b := backticked.FindStringSubmatch(value); b != nil {
					value = b[1]
				}
				cur.goTest = value
			}
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return cases, nil
}

// scanTests returns the names of top-level Test and Fuzz functions declared in
// *_test.go files under root, skipping reference/, testdata and directories
// starting with "." or "_". Build tags are ignored, so e2e/ is always included.
func scanTests(root string) (map[string]bool, error) {
	funcs := map[string]bool{}
	tokens := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || path == filepath.Join(root, "reference")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(tokens, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				continue
			}
			if strings.HasPrefix(fn.Name.Name, "Test") || strings.HasPrefix(fn.Name.Name, "Fuzz") {
				funcs[fn.Name.Name] = true
			}
		}
		return nil
	})
	return funcs, err
}

func inAreas(id string, areas []string) bool {
	if len(areas) == 0 {
		return true
	}
	for _, a := range areas {
		if strings.HasPrefix(id, a+"-") {
			return true
		}
	}
	return false
}
