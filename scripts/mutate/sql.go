package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// sqlArg matches a parameter as written in a .sql file or in sqlc's const
// (`?1` when an argument repeats).
var sqlArg = regexp.MustCompile(`sqlc\.(?:n?arg|slice)\(\s*'?\w+'?\s*\)|@\w+|/\*SLICE:\w+\*/\?\d*|\?\d*`)

// query is one "-- name:" block of a .sql file, by 1-based line.
type query struct {
	name       string
	first, end int
}

// sqlFileMutants returns the mutants of the touched queries of the .sql file
// at repo path p, applied to their consts in the package's dbq/*.sql.go, and
// the mutate:ok lines inside those queries.
func sqlFileMutants(root, p string, touched map[int]bool) ([]mutant, map[int]string, error) {
	data, err := os.ReadFile(filepath.Join(root, p)) //nolint:gosec // G304: a path git listed inside the repo.
	if err != nil {
		return nil, nil, err
	}
	lines := strings.Split(string(data), "\n")
	var consts map[string]sqlConst
	var ms []mutant
	ok := map[int]string{}
	for _, q := range queries(lines) {
		hit := false
		for l := q.first; l <= q.end; l++ {
			hit = hit || touched[l]
		}
		if !hit {
			continue
		}
		if consts == nil {
			if consts, err = dbqConsts(root, path.Join(path.Dir(path.Dir(p)), "dbq")); err != nil {
				return nil, nil, err
			}
		}
		c, found := consts[q.name]
		if !found {
			return nil, nil, fmt.Errorf("%s: query %s has no const in dbq; run make generate", p, q.name)
		}
		if !sameQuery(normQuery(lines[q.first:q.end]), normQuery(strings.Split(c.text, "\n")[1:])) {
			return nil, nil, fmt.Errorf("%s: query %s differs from its const in %s; run make generate", p, q.name, c.file)
		}
		for l := q.first; l <= q.end; l++ {
			if _, rest, found := strings.Cut(lines[l-1], "-- mutate:ok"); found {
				ok[l] = reasonOf(rest)
			}
		}
		lineOf := mapLines(strings.Split(c.text, "\n"), lines, q)
		for _, m := range sqlMutants(c.text) {
			m.File, m.target = p, c.file
			// The line of the first changed token, not of the blanks before it.
			at := m.start + len(c.text[m.start:m.end]) - len(strings.TrimLeft(c.text[m.start:m.end], " \t\n"))
			m.Line = lineOf[strings.Count(c.text[:at], "\n")]
			m.start += c.offset
			m.end += c.offset
			if _, skip := ok[m.Line]; !skip {
				ms = append(ms, m)
			}
		}
	}
	return ms, ok, nil
}

// queries splits a .sql file into its named queries: from the "-- name:"
// line to the first line ending in ";" (or the line before the next name).
func queries(lines []string) []query {
	var qs []query
	for i, l := range lines {
		m := queryName.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			continue
		}
		q := query{name: m[1], first: i + 1, end: len(lines)}
		for j := i + 1; j < len(lines); j++ {
			if queryName.MatchString(strings.TrimSpace(lines[j])) {
				q.end = j
				break
			}
			if strings.HasSuffix(norm(lines[j]), ";") {
				q.end = j + 1
				break
			}
		}
		qs = append(qs, q)
	}
	return qs
}

// norm strips a line comment and blanks and turns sqlc parameters into "?".
func norm(s string) string {
	if i := strings.Index(s, "--"); i >= 0 {
		s = s[:i]
	}
	return strings.Join(strings.Fields(sqlArg.ReplaceAllString(s, "?")), " ")
}

// normQuery is a query's lines after its name line, normalised, without
// blank lines and the closing ";".
func normQuery(lines []string) string {
	var out []string
	for _, l := range lines {
		if n := norm(l); n != "" {
			out = append(out, n)
		}
	}
	return strings.TrimSuffix(strings.Join(out, "\n"), ";")
}

// columnStar matches a column `*` in a normalised query, which sqlc expands
// into the column list.
var columnStar = regexp.MustCompile(`(?:SELECT |RETURNING |DISTINCT |, |\.)\*`)

// sameQuery reports whether the normalised .sql text matches sqlc's const,
// reading each column `*` as any column list.
func sameQuery(sqlText, constText string) bool {
	var re strings.Builder
	re.WriteString(`(?s)^`)
	last := 0
	for _, m := range columnStar.FindAllStringIndex(sqlText, -1) {
		re.WriteString(regexp.QuoteMeta(sqlText[last:m[1]-1]) + `.+?`)
		last = m[1]
	}
	re.WriteString(regexp.QuoteMeta(sqlText[last:]) + `$`)
	return regexp.MustCompile(re.String()).MatchString(constText)
}

// mapLines maps each line of a query const to its line in the .sql file,
// matching lines in order; a line that matches none maps to the name line.
func mapLines(constLines, sqlLines []string, q query) []int {
	out := make([]int, len(constLines))
	next := q.first + 1
	for i, cl := range constLines {
		out[i] = q.first
		want := strings.TrimSuffix(norm(cl), ";")
		if i == 0 || want == "" {
			continue
		}
		for l := next; l <= q.end; l++ {
			if sameQuery(strings.TrimSuffix(norm(sqlLines[l-1]), ";"), want) {
				out[i], next = l, l+1
				break
			}
		}
	}
	return out
}

// sqlConst is a query const's text and where it starts in its file.
type sqlConst struct {
	file   string // repo path
	offset int    // byte offset of text in the file
	text   string
}

// dbqConsts returns the raw-string query consts in dir/*.sql.go by query
// name.
func dbqConsts(root, dir string) (map[string]sqlConst, error) {
	files, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(dir), "*.sql.go"))
	if err != nil {
		return nil, err
	}
	consts := map[string]sqlConst{}
	for _, file := range files {
		src, err := os.ReadFile(file) //nolint:gosec // G304: a generated file in the repo.
		if err != nil {
			return nil, err
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || !strings.HasPrefix(lit.Value, "`") {
				return true
			}
			text := lit.Value[1 : len(lit.Value)-1]
			if m := queryName.FindStringSubmatch(text); m != nil {
				consts[m[1]] = sqlConst{file: path.Join(dir, filepath.Base(file)), offset: fset.Position(lit.Pos()).Offset + 1, text: text}
			}
			return true
		})
	}
	return consts, nil
}

// tok is one SQL token: a word (keyword or name), a number, a string, an
// operator or one of "(),;?".
type tok struct {
	kind       byte // 'w' word, 'n' number, 's' string, 'o' operator, or the punctuation itself
	text       string
	start, end int
}

func (t tok) is(word string) bool { return t.kind == 'w' && strings.EqualFold(t.text, word) }

// lex splits SQL into tokens, dropping comments and blanks.
func lex(s string) []tok {
	var ts []tok
	i := 0
	for i < len(s) {
		c := s[i]
		start := i
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
			continue
		case strings.HasPrefix(s[i:], "--"):
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		case strings.HasPrefix(s[i:], "/*"):
			if j := strings.Index(s[i+2:], "*/"); j >= 0 {
				i += j + 4
			} else {
				i = len(s)
			}
			continue
		case c == '\'' || c == '"':
			i++
			for i < len(s) {
				if s[i] == c {
					if i+1 < len(s) && s[i+1] == c {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			kind := byte('s')
			if c == '"' {
				kind = 'w'
			}
			ts = append(ts, tok{kind, s[start:i], start, i})
			continue
		case isWord(c):
			for i < len(s) && (isWord(s[i]) || s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
				i++
			}
			ts = append(ts, tok{'w', s[start:i], start, i})
			continue
		case c >= '0' && c <= '9':
			for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
				i++
			}
			ts = append(ts, tok{'n', s[start:i], start, i})
			continue
		case strings.ContainsRune("(),;", rune(c)):
			ts = append(ts, tok{c, s[i : i+1], i, i + 1})
			i++
			continue
		case c == '?':
			i++
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			ts = append(ts, tok{'?', s[start:i], start, i})
			continue
		}
		for _, op := range []string{"<=", ">=", "<>", "!=", "==", "||", "<", ">", "="} {
			if strings.HasPrefix(s[i:], op) {
				i += len(op)
				break
			}
		}
		if i == start {
			i++
		}
		ts = append(ts, tok{'o', s[start:i], start, i})
	}
	return ts
}

func isWord(c byte) bool {
	return c == '_' || c == '@' || c == ':' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

var compares = map[string]string{"=": "!=", "==": "!=", "!=": "=", "<>": "=", "<": "<=", "<=": "<", ">": ">=", ">=": ">"}

// termEnders end an AND term, an ORDER BY clause or a SET clause.
var termEnders = []string{"AND", "OR", "ORDER", "GROUP", "LIMIT", "OFFSET", "HAVING", "RETURNING", "UNION", "EXCEPT",
	"INTERSECT", "WINDOW", "WHERE", "JOIN", "LEFT", "INNER", "CROSS", "ON", "WHEN", "THEN", "ELSE", "FROM"}

func ender(t tok) bool {
	if t.kind == ';' || t.kind == ')' {
		return true
	}
	for _, w := range termEnders {
		if t.is(w) {
			return true
		}
	}
	return false
}

// opens and closes report a nesting token: parentheses and CASE ... END.
func opens(t tok) bool  { return t.kind == '(' || t.is("CASE") }
func closes(t tok) bool { return t.kind == ')' || t.is("END") }

// clauseEnd returns the index of the first token from i on, at the starting
// depth, that ends a term. A BETWEEN's AND does not end it.
func clauseEnd(ts []tok, i int) int {
	depth, between := 0, false
	for ; i < len(ts); i++ {
		t := ts[i]
		switch {
		case opens(t):
			depth++
		case closes(t) && depth > 0:
			depth--
		case depth > 0:
		case t.is("BETWEEN"):
			between = true
		case t.is("AND") && between:
			between = false
		case ender(t):
			return i
		}
	}
	return i
}

// items splits ts[from:to] at commas outside parentheses into [start, end)
// token index pairs.
func items(ts []tok, from, to int) [][2]int {
	var out [][2]int
	depth, s := 0, from
	for i := from; i < to; i++ {
		switch {
		case opens(ts[i]):
			depth++
		case closes(ts[i]):
			depth--
		case ts[i].kind == ',' && depth == 0:
			out = append(out, [2]int{s, i})
			s = i + 1
		}
	}
	return append(out, [2]int{s, to})
}

// sqlMutants returns the mutants of one query's text; offsets are into s.
func sqlMutants(s string) []mutant {
	ts := lex(s)
	var ms []mutant
	add := func(start, end int, repl, desc string) {
		ms = append(ms, mutant{Mutation: desc, start: start, end: end, repl: repl})
	}
	span := func(a, b int) string { return strings.Join(strings.Fields(s[ts[a].start:ts[b-1].end]), " ") }
	// dropItem removes item k of list (with one adjacent comma).
	dropItem := func(list [][2]int, k int, desc string) {
		it := list[k]
		if k+1 < len(list) {
			add(ts[it[0]].start, ts[list[k+1][0]].start, "", desc)
		} else {
			add(ts[list[k-1][1]-1].end, ts[it[1]-1].end, "", desc)
		}
	}
	inSet, between := false, map[int]bool{}
	depth := 0
	for i, t := range ts {
		switch {
		case opens(t):
			depth++
		case closes(t):
			between[depth] = false
			depth--
		}
		switch {
		case t.is("SET"):
			inSet = true
		case t.is("WHERE") || t.is("RETURNING") || t.is("FROM"):
			inSet = false
		case t.kind == 'o' && compares[t.text] != "" && (!inSet || (t.text != "=" && t.text != "==")):
			around := func(op string) string { return strings.TrimSpace(word(ts, i-1) + " " + op + " " + word(ts, i+1)) }
			add(t.start, t.end, compares[t.text], around(t.text)+" -> "+around(compares[t.text]))
		case t.is("BETWEEN"):
			between[depth] = true
		case t.is("AND") && between[depth]:
			between[depth] = false
		case t.is("AND") && i > 0:
			end := clauseEnd(ts, i+1)
			if end > i+1 {
				add(ts[i-1].end, ts[end-1].end, "", fmt.Sprintf("drop %q", span(i, end)))
			}
		case t.is("IN") && i+2 < len(ts) && ts[i+1].kind == '(' && !ts[i+2].is("SELECT") && !ts[i+2].is("WITH") && ts[i+2].kind != ')':
			end := i + 1
			for d := 0; end < len(ts); end++ {
				if opens(ts[end]) {
					d++
				} else if closes(ts[end]) {
					if d--; d == 0 {
						break
					}
				}
			}
			list := items(ts, i+2, end)
			if len(list) < 2 {
				break
			}
			for k, it := range list {
				dropItem(list, k, word(ts, i-1)+" IN drop "+span(it[0], it[1]))
			}
		case t.is("ORDER") && i > 0 && i+2 < len(ts) && ts[i+1].is("BY"):
			end := clauseEnd(ts, i+2)
			list := items(ts, i+2, end)
			for k, it := range list {
				key := span(it[0], it[1])
				last := ts[it[1]-1]
				switch {
				case last.is("DESC"):
					add(last.start, last.end, "ASC", "ORDER BY "+key+" -> ASC")
				case last.is("ASC"):
					add(last.start, last.end, "DESC", "ORDER BY "+key+" -> DESC")
				default:
					add(last.end, last.end, " DESC", "ORDER BY "+key+" -> DESC")
				}
				if len(list) > 1 {
					dropItem(list, k, "ORDER BY drop "+key)
				}
			}
			if len(list) == 1 {
				add(ts[i-1].end, ts[end-1].end, "", "ORDER BY "+span(i+2, end)+" dropped")
			}
		case t.is("DISTINCT") && i+1 < len(ts):
			add(t.start, ts[i+1].start, "", "DISTINCT "+ts[i+1].text+" -> "+ts[i+1].text)
		case t.is("LIMIT") && i+1 < len(ts) && ts[i+1].kind == 'n':
			if n, err := strconv.Atoi(ts[i+1].text); err == nil {
				add(ts[i+1].start, ts[i+1].end, strconv.Itoa(n+1), fmt.Sprintf("LIMIT %d -> %d", n, n+1))
			}
		}
	}
	return ms
}

// word returns token i's text, or "" outside ts.
func word(ts []tok, i int) string {
	if i < 0 || i >= len(ts) {
		return ""
	}
	return ts[i].text
}
