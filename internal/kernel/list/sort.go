package list

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// Build is the page query: every field, the WHERE clause, the sort and
// LIMIT ? OFFSET ? (D-NUM-20).
func Build(dt *doctype.DocType, q Query, scope auth.Predicate) (string, []any, error) {
	size, offset, err := pageBounds(q)
	if err != nil {
		return "", nil, err
	}
	sql, args, err := BuildAll(dt, q, scope)
	if err != nil {
		return "", nil, err
	}
	return sql + " LIMIT ? OFFSET ?", append(args, size, offset), nil
}

// BuildAll is Build without LIMIT and OFFSET (export).
func BuildAll(dt *doctype.DocType, q Query, scope auth.Predicate) (string, []any, error) {
	order, err := orderBy(dt, q.Sort)
	if err != nil {
		return "", nil, err
	}
	where, args, err := whereClause(dt, q, scope)
	if err != nil {
		return "", nil, err
	}
	cols := []string{dt.Table + ".id"}
	for _, f := range dt.Fields {
		if !f.OnLine {
			cols = append(cols, column(dt, f)+" AS "+f.Name)
		}
	}
	return "SELECT " + strings.Join(cols, ", ") + " FROM " + dt.Table + where + order, args, nil
}

// ParseSort reads the URL form "<field> <asc|desc>[,…]".
func ParseSort(dt *doctype.DocType, s string) ([]doctype.SortKey, error) {
	var keys []doctype.SortKey
	for part := range strings.SplitSeq(s, ",") {
		words := strings.Fields(part)
		if len(words) == 0 {
			continue
		}
		if _, ok := sortField(dt, words[0]); !ok {
			return nil, badSort(dt, words[0])
		}
		if len(words) != 2 || (words[1] != "asc" && words[1] != "desc") {
			return nil, badSort(dt, strings.Join(words, " "))
		}
		keys = append(keys, doctype.SortKey{Field: words[0], Desc: words[1] == "desc"})
	}
	return keys, nil
}

func badSort(dt *doctype.DocType, what string) error {
	return badQuery(fmt.Sprintf("You can't sort %s by '%s'.", dt.PluralLabel, what))
}

func sortField(dt *doctype.DocType, name string) (doctype.Field, bool) {
	i := slices.IndexFunc(dt.Fields, func(f doctype.Field) bool { return f.Name == name && f.Sortable && !f.OnLine })
	if i < 0 {
		return doctype.Field{}, false
	}
	return dt.Fields[i], true
}

// orderBy is " ORDER BY …": the requested keys (each must be Sortable), else
// dt.DefaultSort, empty values last, then the id as the tie-breaker.
func orderBy(dt *doctype.DocType, keys []doctype.SortKey) (string, error) {
	fields := make([]doctype.Field, len(keys))
	for i, k := range keys {
		f, ok := sortField(dt, k.Field)
		if !ok {
			return "", badSort(dt, k.Field)
		}
		fields[i] = f
	}
	if len(keys) == 0 {
		keys = dt.DefaultSort
		fields = make([]doctype.Field, len(keys))
		for i, k := range keys {
			j := slices.IndexFunc(dt.Fields, func(f doctype.Field) bool { return f.Name == k.Field && !f.OnLine })
			if j < 0 {
				return "", fmt.Errorf("list: %s default sort names unknown field %q", dt.Name, k.Field)
			}
			fields[i] = dt.Fields[j]
		}
	}
	dir := " DESC"
	var terms []string
	for i, k := range keys {
		dir = " ASC"
		if k.Desc {
			dir = " DESC"
		}
		col := column(dt, fields[i])
		empty, expr := col+" IS NULL", col
		switch fields[i].Type {
		case doctype.Text, doctype.Select:
			empty += " OR " + col + " = ''"
			expr += " COLLATE NOCASE"
		case doctype.Link:
			target, ok := doctype.Lookup(fields[i].Link)
			if !ok {
				return "", fmt.Errorf("list: %s.%s links to unknown DocType %q", dt.Name, fields[i].Name, fields[i].Link)
			}
			expr = "(SELECT " + target.Table + ".name FROM " + target.Table + " WHERE " +
				target.Table + ".id = " + col + ") COLLATE NOCASE"
		}
		terms = append(terms, "CASE WHEN "+empty+" THEN 1 ELSE 0 END", expr+dir)
	}
	terms = append(terms, dt.Table+".id"+dir)
	return " ORDER BY " + strings.Join(terms, ", "), nil
}
