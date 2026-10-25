package components

import (
	"errors"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// Priority is a list column's place on a card; it matches
// doctype.Field.ListPriority (CD-42). 5 or more is low priority.
type Priority int

// List priorities.
const (
	PTitle  Priority = 1
	PFigure Priority = 2
	PState  Priority = 3
	PMeta   Priority = 4
	PLow    Priority = 5
)

// Column is one list field. Num values are right-aligned figures; Code
// values may break anywhere.
type Column struct {
	Key, Label string
	Priority   Priority
	Num, Code  bool
}

// Row is one list entry. Values are keyed by Column.Key; the PState column
// shows State. Title is used when no column is PTitle (Info.Display);
// Party is Info.Party (CD-55).
type Row struct {
	Href, Title, Party string
	Values             map[string]string
	State              doctype.State
}

// DataListProps describes a DataList; Empty is shown when there are no rows.
type DataListProps struct {
	Label   string
	Columns []Column
	Rows    []Row
	Empty   templ.Component
}

// Validate checks the columns: 1 to 8, at most one PTitle, PFigure and
// PState each, and with no PTitle column every row has a Title.
func (p DataListProps) Validate() error {
	if len(p.Columns) < 1 || len(p.Columns) > 8 {
		return errors.New("components.DataList: 1 to 8 columns are allowed")
	}
	n := map[Priority]int{}
	for _, c := range p.Columns {
		n[c.Priority]++
	}
	if n[PTitle] > 1 || n[PFigure] > 1 || n[PState] > 1 {
		return errors.New("components.DataList: at most one title, figure and state column")
	}
	if n[PTitle] == 0 {
		for _, r := range p.Rows {
			if r.Title == "" {
				return errors.New("components.DataList: no title column and a row without Title")
			}
		}
	}
	return nil
}

// listLayout is the columns in the order a card and a row draw them:
// title, figure, the PMeta columns, the low-priority ones, then state.
type listLayout struct {
	title, figure, state *Column
	meta                 []Column
}

func listLayoutOf(cols []Column) listLayout {
	var l listLayout
	var low []Column
	for i := range cols {
		switch c := &cols[i]; {
		case c.Priority == PTitle:
			l.title = c
		case c.Priority == PFigure:
			l.figure = c
		case c.Priority == PState:
			l.state = c
		case c.Priority >= PLow:
			low = append(low, *c)
		default:
			l.meta = append(l.meta, *c)
		}
	}
	l.meta = append(l.meta, low...)
	return l
}

// tracks is the number of grid columns; a missing title column still
// takes the first track for Row.Title.
func (l listLayout) tracks() int {
	n := len(l.meta) + 1
	if l.figure != nil {
		n++
	}
	if l.state != nil {
		n++
	}
	return n
}

// listSecond is the column shown on card row 2 when the row has no Party:
// the first PMeta column, if any.
func (l listLayout) listSecond(r Row) *Column {
	if r.Party != "" || len(l.meta) == 0 || l.meta[0].Priority != PMeta {
		return nil
	}
	return &l.meta[0]
}

// listThird is the meta columns on card row 3.
func (l listLayout) listThird(r Row) []Column {
	if l.listSecond(r) != nil {
		return l.meta[1:]
	}
	return l.meta
}

// listCellClass is a value's classes, written out whole for the CSS build.
func listCellClass(c Column, title bool) string {
	switch {
	case title && c.Code:
		return "datalist-cell datalist-title code"
	case title:
		return "datalist-cell datalist-title"
	case c.Num:
		return "datalist-cell num"
	case c.Code:
		return "datalist-cell code"
	}
	return "datalist-cell"
}
