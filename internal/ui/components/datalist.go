package components

import (
	"errors"
	"strconv"
	"strings"

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
// values may break anywhere. NoWrap values (dates, codes, GSTINs) keep
// whole tokens in aligned rows.
type Column struct {
	Key, Label        string
	Priority          Priority
	Num, Code, NoWrap bool
}

// Row is one list entry. Values are keyed by Column.Key; the PState column
// shows State. Title is used when no column is PTitle or the PTitle value
// is blank (Info.Display, CD-371); Party is Info.Party (CD-55) and PartyKey
// the Key of the column it comes from, which cards skip (CD-371).
type Row struct {
	Href, Title, Party string
	PartyKey           string
	Values             map[string]string
	State              doctype.State
	StateLabel         string // the DocType's word for State; "" keeps the component's
	Tone               string // "" or ToneWarning, which tints the row (CD-326 (8))
}

// ToneWarning is a Row's Tone for a row that needs attention, such as an
// overdue line (CD-330 (4)).
const ToneWarning = "warning"

// DataListProps describes a DataList; Empty is shown when there are no rows.
type DataListProps struct {
	Label   string
	Columns []Column
	Rows    []Row
	Empty   templ.Component
}

// Validate checks the columns: 1 to 8 and at most 8 grid tracks, every
// priority PTitle or more, at most one PTitle, PFigure and PState each, and
// with no PTitle column every row has a Title. At most 3 columns are
// NoWrap: 4 minimum-width tracks don't fit an 8-track list at 48rem.
func (p DataListProps) Validate() error {
	if len(p.Columns) < 1 || len(p.Columns) > 8 {
		return errors.New("components.DataList: 1 to 8 columns are allowed")
	}
	n := map[Priority]int{}
	nowrap := 0
	for _, c := range p.Columns {
		if c.Priority < PTitle {
			return errors.New("components.DataList: a column priority is below 1")
		}
		n[c.Priority]++
		if c.NoWrap {
			nowrap++
		}
	}
	if nowrap > 3 {
		return errors.New("components.DataList: at most 3 NoWrap columns")
	}
	if n[PTitle] > 1 || n[PFigure] > 1 || n[PState] > 1 {
		return errors.New("components.DataList: at most one title, figure and state column")
	}
	if listLayoutOf(p.Columns).tracks() > 8 {
		return errors.New("components.DataList: at most 8 grid tracks, counting the title track")
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

// nowrapTracks is the 1-based grid tracks of the NoWrap columns, space
// separated; the CSS gives each a minimum width that fits its value.
func (l listLayout) nowrapTracks() string {
	cols := []*Column{l.title, l.figure}
	for i := range l.meta {
		cols = append(cols, &l.meta[i])
	}
	cols = append(cols, l.state)
	var tracks []string
	n := 0
	for i, c := range cols {
		if c == nil && i > 0 {
			continue
		}
		n++
		if c != nil && c.NoWrap {
			tracks = append(tracks, strconv.Itoa(n))
		}
	}
	return strings.Join(tracks, " ")
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

// titleText is card row 1's title: the PTitle value, or Title when that
// value is blank (CD-371).
func (l listLayout) titleText(r Row) string {
	if v := r.Values[l.title.Key]; strings.TrimSpace(v) != "" {
		return v
	}
	return r.Title
}

// listSep ends a card value that has a drawn value after it. The no-break
// space keeps the dot on the value's line.
const listSep = "\u00a0\u00b7 "

// cardCell is one value on a card: title marks the first-row title, state the
// state badge, blank a value cards don't draw: one with no text, or the
// row's party column, which row 2 already shows (CD-371). sep and sepLow are listCell's.
type cardCell struct {
	Column
	title, state, blank, sep, sepLow bool
}

// thirdCells is card row 3: the listThird values, then the state badge.
// Each drawn value gets a separator when a drawn value follows it, low
// priority when every drawn value after it is.
func (l listLayout) thirdCells(r Row) []cardCell {
	var cells []cardCell
	for _, c := range l.listThird(r) {
		blank := strings.TrimSpace(r.Values[c.Key]) == "" || c.Key == r.PartyKey
		cells = append(cells, cardCell{Column: c, blank: blank})
	}
	if l.state != nil {
		cells = append(cells, cardCell{Column: *l.state, state: true})
	}
	for i := range cells {
		if cells[i].blank {
			continue
		}
		low := true
		for _, n := range cells[i+1:] {
			if !n.blank {
				cells[i].sep = true
				low = low && n.Priority >= PLow
			}
		}
		cells[i].sepLow = cells[i].sep && low
	}
	return cells
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
