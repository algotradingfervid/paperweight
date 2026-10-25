package components

import "strconv"

// LineCard is one line on the phone list (design-system §8.2). A move URL
// left empty hides that button (first line up, last line down).
type LineCard struct {
	No                                           int
	Item, Code, QtyRate, Amount, Tax             string
	ChangeURL, RemoveURL, MoveUpURL, MoveDownURL string
}

// LineGridRow is one row of the desktop grid. Fields are the item picker
// summary, Qty, UoM, Rate and Tax, with ids grid-<no>-<field>.
type LineGridRow struct {
	No      int
	Fields  []FieldProps
	Amount  string
	PostURL string
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
