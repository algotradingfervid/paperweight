// Command briefcds lists the contract-change (CD) entries added or changed
// since a base revision that mention the files, packages or identifiers a
// brief names, so a brief can be checked against the latest decisions.
//
// Usage, from the repository root:
//
//	go run ./scripts/briefcds -base <rev> [-head <rev>] [-ident X]... <path or package>...
//
// or `make briefcds BASE=<rev> FILES="<paths>"`.
//
// It reads docs/specs/contract-changes.md at base and at head (default main,
// not the worktree's HEAD: a lane lags main, and the decisions it hasn't
// seen yet are the ones to find). An entry starts at a table row
// `| CD-NNN | ...` and runs on through the paragraphs and bullets under it, up
// to the next CD row or heading; so text trailing a round's table belongs to
// that round's last CD. An entry is new or changed when its id is missing at
// base or its text differs.
//
// Each argument gives these terms: its path (without "./" and "/..."), its
// file name without the extension, and its package directory name. A term
// matches as a whole word (case-sensitive), so "form" doesn't match
// "formatter". Each matching entry prints as
//
//	CD-NNN: <first 160 characters of its text>
//
// When none matches it prints "no CDs since <base> mention these". The exit
// code is 0 either way, and 2 on a usage or git error.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strings"
)

const logPath = "docs/specs/contract-changes.md"

var cdRow = regexp.MustCompile(`^\| (CD-\d+) \|`)

// entry is one CD entry: its id and its text with whitespace collapsed.
type entry struct{ id, text string }

func main() {
	os.Exit(run(os.Args[1:], ".", os.Stdout))
}

// run checks the repository at root and returns the exit code.
func run(args []string, root string, out io.Writer) int {
	fset := flag.NewFlagSet("briefcds", flag.ContinueOnError)
	fset.SetOutput(out)
	base := fset.String("base", "", "revision to compare the CD log against (required)")
	head := fset.String("head", "main", "revision holding the latest CD log")
	var idents []string
	fset.Func("ident", "identifier to look for (repeatable)", func(s string) error {
		idents = append(idents, s)
		return nil
	})
	printf := func(format string, a ...any) { _, _ = fmt.Fprintf(out, format, a...) }
	if err := fset.Parse(args); err != nil {
		return 2
	}
	if *base == "" || (fset.NArg() == 0 && len(idents) == 0) {
		printf("ERROR usage: briefcds -base <rev> [-head <rev>] [-ident X]... <path or package>...\n")
		return 2
	}
	words := idents
	for _, a := range fset.Args() {
		words = append(words, terms(a)...)
	}
	var logs [2]string
	for i, rev := range []string{*base, *head} {
		text, err := show(root, rev)
		if err != nil {
			printf("ERROR %v\n", err)
			return 2
		}
		logs[i] = text
	}
	found := false
	for _, e := range newOrChanged(parseEntries(logs[0]), parseEntries(logs[1])) {
		if mentions(e.text, words) {
			printf("%s: %s\n", e.id, cut(e.text, 160))
			found = true
		}
	}
	if !found {
		printf("no CDs since %s mention these\n", *base)
	}
	return 0
}

// show returns the CD log at rev.
func show(root, rev string) (string, error) {
	cmd := exec.Command("git", "-C", root, "show", rev+":"+logPath) //nolint:gosec // G204: a fixed git subcommand; rev is the caller's own flag.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git show %s:%s: %v: %s", rev, logPath, err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// parseEntries returns the CD entries of a CD log in file order.
func parseEntries(md string) []entry {
	var entries []entry
	var cur []string
	flush := func() {
		if cur != nil {
			id := cdRow.FindStringSubmatch(cur[0])[1]
			cur[0] = strings.TrimPrefix(cur[0], "| "+id+" |")
			entries = append(entries, entry{id, strings.Join(strings.Fields(strings.Join(cur, " ")), " ")})
		}
		cur = nil
	}
	for line := range strings.SplitSeq(md, "\n") {
		switch {
		case cdRow.MatchString(line):
			flush()
			cur = []string{line}
		case strings.HasPrefix(line, "#"):
			flush()
		case cur != nil:
			cur = append(cur, line)
		}
	}
	flush()
	return entries
}

// newOrChanged returns the head entries whose id is missing from base or whose
// text differs.
func newOrChanged(base, head []entry) []entry {
	old := map[string]string{}
	for _, e := range base {
		old[e.id] = e.text
	}
	var out []entry
	for _, e := range head {
		if text, ok := old[e.id]; !ok || text != e.text {
			out = append(out, e)
		}
	}
	return out
}

// terms returns the words an argument is known by: its path, its file name
// without the extension and its package directory name.
func terms(arg string) []string {
	p := strings.TrimSuffix(strings.TrimSuffix(path.Clean(arg), "/..."), "/")
	p = strings.TrimPrefix(p, "./")
	out := []string{p}
	add := func(w string) {
		if w != "" && w != "." && w != "/" && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	if ext := path.Ext(p); ext != "" {
		add(strings.TrimSuffix(path.Base(p), ext))
		add(path.Base(path.Dir(p)))
	} else {
		add(path.Base(p))
	}
	return out
}

// mentions reports whether text holds any of words as a whole word.
func mentions(text string, words []string) bool {
	for _, w := range words {
		re := regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(w) + `($|[^A-Za-z0-9_])`)
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// cut returns the first n characters of s.
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
