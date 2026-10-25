// Package list builds list queries for a DocType (spec §5). NUM owns it.
package list

import (
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
)

type Page = types.Page

type Result[T any] = types.Result[T]

// Op is a filter operator; NUM adds the constants.
type Op string

type Filter struct {
	Field  string
	Op     Op
	Values []string
}

type Query struct {
	Filters        []Filter
	Search         string
	Sort           []doctype.SortKey
	Page, PageSize int
}

func Build(dt *doctype.DocType, q Query, scope auth.Predicate) (sql string, args []any, err error) {
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
	size := q.PageSize
	if size == 0 {
		size = 25
	}
	page := max(q.Page, 1)
	sql = "SELECT " + strings.Join(cols, ", ") + " FROM " + dt.Table + where +
		" ORDER BY " + dt.Table + ".id LIMIT ? OFFSET ?"
	return sql, append(args, size, (page-1)*size), nil
}
