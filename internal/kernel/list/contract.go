// Package list builds list queries for a DocType (spec §5). NUM owns it.
package list

import (
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
