package components

import (
	"strconv"

	"github.com/a-h/templ"
)

// LineCard is one line on the phone list (design-system §8.2). A move URL
// left empty hides that button (first line up, last line down).
type LineCard struct {
	No                                           int
	Item, Code, QtyRate, Amount, Tax             string
	ChangeURL, RemoveURL, MoveUpURL, MoveDownURL string
}

// GridCell is one input cell of a grid row; Label heads its column.
type GridCell struct {
	Label string
	Input templ.Component
}

// LineGridRow is one row of the desktop grid (CD-139). Cells are the item
// picker summary, Qty, UoM, Rate and Tax, with ids grid-<no>-<field>. An
// empty Remove or move URL hides that button.
type LineGridRow struct {
	No                                         int
	Cells                                      []GridCell
	Amount                                     string
	PostURL, RemoveURL, MoveUpURL, MoveDownURL string
}

// LineListProps describes the line cards. Total is the number of lines;
// when it is more than len(Cards), "Show all" and "Find a line" appear.
type LineListProps struct {
	Cards                                           []LineCard
	Grid                                            []LineGridRow
	TaxableTotal, AddURL, ShowAllURL, FindURL, Find string
	Total                                           int
	Editable                                        bool
}

func lineAria(verb string, no int) string {
	return verb + " line " + strconv.Itoa(no)
}

func lineGridID(no int) string {
	return "grid-row-" + strconv.Itoa(no)
}
