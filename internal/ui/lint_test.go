package ui_test

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// lintRule is one line check from design-system §10 (research §6.3, CD-98, CD-112).
// Allow gets the slash path relative to the lint root, the innermost CSS rule's
// selector at the match ("" for templ) and the line; it returns true to excuse a match.
type lintRule struct {
	Name  string
	Ext   string
	Re    *regexp.Regexp
	Allow func(path, sel, line string) bool
}

func re(s string) *regexp.Regexp { return regexp.MustCompile("(?i)" + s) }

var (
	fixedWidthRe = re(`\b(width|min-width|inline-size|min-inline-size)\s*:\s*[^;]*\d(px|rem|em)\b`)
	maxDeclRe    = re(`\bmax-[a-z-]+\s*:[^;]*`)
	min100Re     = re(`min\(100%,[^)]*\)`)
	queryRe      = re(`@(media|container)\b[^{]*`)
	onePxRe      = re(`\b1px\b`)
	rootSelRe    = re(`(^|[\s,>+~])(html|body)\b`)
)

// visuallyHidden and truncate may use nowrap; visually-hidden may also use 1px (CD-112).
func isSel(sel string, names ...string) bool {
	for _, n := range names {
		if sel == "."+n || sel == "@utility "+n {
			return true
		}
	}
	return false
}

func none(_, _, _ string) bool { return false }

var lintRules = []lintRule{
	{"style-attr", "templ", re(`\bstyle\s*=`), none},
	{"inline-script", "templ", re(`<script\b`), func(_, _, l string) bool {
		return strings.Contains(l, "src=") || strings.Contains(l, `type="application/json"`)
	}},
	{"on-handler", "templ", re(`\son[a-z]+\s*=`), none},
	{"hx-on", "templ", re(`\bhx-on[:-]`), none},
	{"js-in-hx", "templ", re(`hx-(vals|headers)\s*=\s*['"][^'"]*js:`), none},
	{"trigger-filter", "templ", re(`hx-trigger\s*=\s*"[^"]*\[`), none},
	{"templ-css-script", "templ", re(`^\s*(css|script)\s+\w+\s*\(`), none},
	{"templ-js-helpers", "templ", re(`templ\.(JSFuncCall|ComponentScript|JSExpression)`), none},
	{"alpine-unsafe", "templ", re(`\bx-[\w:.-]+\s*=\s*"[^"]*(=>|` + "`" + `|\bwindow\.|\bdocument\.|\bMath\.|\bJSON\.|\bconsole\.)|\bx-html\b`), none},
	{"number-input", "templ", re(`type\s*=\s*"number"`), none},
	{"zoom-lock", "templ", re(`maximum-scale|user-scalable`), none},
	{"table", "templ", re(`<table\b`), func(p, _, _ string) bool { return strings.Contains("/"+p, "/print/") }},
	{"fixed-width", "css", fixedWidthRe, func(p, sel, l string) bool {
		if filepath.Base(p) == "tokens.css" {
			return true
		}
		// Media and container query conditions are not widths (mobile-first needs min-width queries).
		l = queryRe.ReplaceAllString(l, "")
		l = min100Re.ReplaceAllString(maxDeclRe.ReplaceAllString(l, ""), "")
		if isSel(sel, "visually-hidden") {
			l = onePxRe.ReplaceAllString(l, "")
		}
		return !fixedWidthRe.MatchString(l)
	}},
	{"vw", "css", re(`100vw`), none},
	{"overflow-x", "css", re(`overflow-x\s*:\s*(auto|scroll)`), none},
	{"overflow-x-hidden-root", "css", re(`overflow-x\s*:\s*hidden`), func(_, sel, _ string) bool { return !rootSelRe.MatchString(sel) }},
	{"nowrap", "css", re(`white-space\s*:\s*nowrap`), func(_, sel, _ string) bool { return isSel(sel, "truncate", "visually-hidden") }},
	{"raw-colour", "css", re(`#[0-9a-f]{3,8}\b|\brgba?\(|\bhsla?\(`), func(p, _, _ string) bool {
		b := filepath.Base(p)
		return b == "tokens.css" || b == "theme.css"
	}},
	{"dark-mode", "css", re(`prefers-color-scheme`), none},
	{"daisy-banned", "templ", re(`class\s*=\s*"[^"]*\b(countdown|radial-progress|carousel|dock|fab|hover-3d|hover-gallery|text-rotate|mockup-[a-z]+|diff|swap|rating|mask|theme-controller|table|[a-z]+-soft|[a-z]+-dash)\b`), none},
	{"dark-variant", "templ", re(`class\s*=\s*"[^"]*\bdark:`), none},
	{"daisy-outside-ui", "templ", re(`class\s*=\s*"[^"]*\b(btn|input|textarea|select|fieldset|checkbox|radio|toggle|file-input|modal|alert|badge|card|menu|drawer|navbar|collapse|loading|progress|join|toast|tooltip|stat)\b`), func(p, _, _ string) bool {
		return strings.Contains("/"+p, "/internal/ui/")
	}},
	{"tw-arbitrary", "templ", re(`class\s*=\s*"[^"]*-\[`), none},
	{"tw-overflow", "templ", re(`class\s*=\s*"[^"]*\b(overflow-x-auto|overflow-x-scroll|w-screen|whitespace-nowrap)\b`), none},
}

// cssScope tracks the stack of enclosing rule selectors, skipping comments and strings.
type cssScope struct {
	stack   []string
	pending string
	comment bool
	quote   byte
}

func (s *cssScope) feed(text string) {
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case s.comment:
			if c == '*' && i+1 < len(text) && text[i+1] == '/' {
				s.comment = false
				i++
			}
		case s.quote != 0:
			switch c {
			case '\\':
				i++
			case s.quote:
				s.quote = 0
			}
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			s.comment = true
			i++
		case c == '"' || c == '\'':
			s.quote = c
		case c == '{':
			s.stack = append(s.stack, strings.Join(strings.Fields(s.pending), " "))
			s.pending = ""
		case c == '}':
			if len(s.stack) > 0 {
				s.stack = s.stack[:len(s.stack)-1]
			}
			s.pending = ""
		case c == ';':
			s.pending = ""
		default:
			s.pending += string(c)
		}
	}
}

// selectorAt returns the innermost selector after feeding prefix, without changing s.
func (s *cssScope) selectorAt(prefix string) string {
	c := *s
	c.stack = slices.Clone(s.stack)
	c.feed(prefix)
	if len(c.stack) == 0 {
		return ""
	}
	return c.stack[len(c.stack)-1]
}

// lintFile checks root/rel and returns "rel:line: rule" findings.
func lintFile(root, rel string) []string {
	f, err := os.Open(filepath.Join(root, rel)) //nolint:gosec // G304: test reads repo files
	if err != nil {
		return []string{rel + ": " + err.Error()}
	}
	defer func() { _ = f.Close() }()
	path := filepath.ToSlash(rel)
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	var out []string
	var scope cssScope
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		for _, r := range lintRules {
			if r.Ext != ext {
				continue
			}
			loc := r.Re.FindStringIndex(line)
			if loc == nil {
				continue
			}
			sel := ""
			if ext == "css" {
				sel = scope.selectorAt(line[:loc[0]])
			}
			if !r.Allow(path, sel, line) {
				out = append(out, path+":"+strconv.Itoa(n)+": "+r.Name)
			}
		}
		if ext == "css" {
			scope.feed(line + "\n")
		}
	}
	if err := sc.Err(); err != nil {
		out = append(out, path+": "+err.Error())
	}
	return out
}

// lintTree walks every *.templ under root and every *.css under internal/ui/css/.
func lintTree(root string) []string {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case "reference", "testdata", "node_modules", ".git":
				if rel != "." {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(rel, ".templ") || (strings.HasPrefix(rel, "internal/ui/css/") && strings.HasSuffix(rel, ".css")) {
			out = append(out, lintFile(root, rel)...)
		}
		return nil
	})
	if err != nil {
		out = append(out, err.Error())
	}
	return out
}

// lintFixtures holds one bad and one or more good files per rule:
// bad/<rule>.<ext> and good/<rule>.<ext> or good/<rule>/…. The templ fixtures are
// stored as *.templ.txt so that neither templ generate nor Tailwind's .templ/.go
// scan picks them up; fixtureFiles copies them under their real names.
const lintFixtures = "testdata/lint"

// badLine is where each bad fixture puts its one offending line.
var badLine = map[string]int{"templ": 4, "css": 3}

// fixtureFiles copies lintFixtures/dir into root without the .txt suffix and returns the copies' paths.
func fixtureFiles(t *testing.T, root, dir string) []string {
	t.Helper()
	var rels []string
	err := filepath.WalkDir(filepath.Join(lintFixtures, dir), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(lintFixtures, path)
		rel = strings.TrimSuffix(filepath.ToSlash(rel), ".txt")
		b, err := os.ReadFile(path) //nolint:gosec // G304: test reads repo files
		if err != nil {
			return err
		}
		dst := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}
		rels = append(rels, rel)
		return os.WriteFile(dst, b, 0o600) //nolint:gosec // G703: dst is under t.TempDir()
	})
	if err != nil {
		t.Fatal(err)
	}
	return rels
}

func TestLint_Rules(t *testing.T) {
	root := t.TempDir()
	bad := map[string]bool{}
	for _, rel := range fixtureFiles(t, root, "bad") {
		base := filepath.Base(rel)
		ext := strings.TrimPrefix(filepath.Ext(base), ".")
		rule := strings.TrimSuffix(base, "."+ext)
		bad[rule] = true
		want := rel + ":" + strconv.Itoa(badLine[ext]) + ": " + rule
		got := lintFile(root, rel)
		if len(got) != 1 || got[0] != want {
			t.Errorf("%s: got %q, want [%q]", rel, got, want)
		}
	}
	good := map[string]bool{}
	for _, rel := range fixtureFiles(t, root, "good") {
		name := strings.TrimPrefix(rel, "good/")
		for _, r := range lintRules {
			if strings.HasPrefix(name, r.Name+".") || strings.HasPrefix(name, r.Name+"/") {
				good[r.Name] = true
			}
		}
		if got := lintFile(root, rel); len(got) != 0 {
			t.Errorf("%s: want no findings, got %q", rel, got)
		}
	}
	for _, r := range lintRules {
		if !bad[r.Name] || !good[r.Name] {
			t.Errorf("rule %s: missing fixture (bad %v, good %v)", r.Name, bad[r.Name], good[r.Name])
		}
	}
}

func TestLint_Repo(t *testing.T) {
	for _, f := range lintTree("../..") {
		t.Error(f)
	}
}
