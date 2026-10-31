package components

import (
	"strconv"

	"github.com/a-h/templ"
)

// LineCard is one line on the phone list (design-system §8.2). A move URL
// left empty hides that button (first line up, last line down). Version,
// when set, is posted by the move forms (CD-94).
type LineCard struct {
	No                                           int
	Item, Code, QtyRate, Amount, Tax, Received   string
	ChangeURL, RemoveURL, MoveUpURL, MoveDownURL string
	Version                                      string
	Note                                         string      // a module's note on this line (hooks spec §3)
	Receipt                                      *Attachment // the line's receipt (UX-E3)
	ReceiptNote                                  string      // why the line has no receipt (UX-E3)
	ReceiptWarn                                  bool        // no receipt and no reason (UX-E3)
}

// GridCell is one input cell of a grid row; Label heads its column. Wide
// gives a number cell in a RowsEditor the track its longest value needs
// (CD-203); LineGrid sets its columns itself and ignores it. The track
// assumes no suffix: a suffix needs 137 px and gets 83 px. Item's factor has
// none.
type GridCell struct {
	Label string
	Input templ.Component
	Wide  bool
	Text  string // read-only content when Input is nil (CD-313)
	Sub   string // a muted second line under Text (CD-313)
	Kind  string // "", "qty", "rate" or "money": the input's content minimum (CD-313)
}

// LineGridRow is one row of the desktop grid (CD-139). Cells are the item
// picker summary, Qty, UoM, Rate and Tax, with ids grid-<no>-<field>. An
// empty Remove or move URL hides that button. Version, when set, is posted
// by the move forms; LineGridVersions brings it up to date after a row save.
type LineGridRow struct {
	No                                         int
	Cells                                      []GridCell
	Amount                                     string
	PostURL, RemoveURL, MoveUpURL, MoveDownURL string
	Version                                    string
	Note                                       string // a module's note on this row (hooks spec §3)
	ChangeURL                                  string // the line page, for a read-only row (CD-313)
}

// LineListProps describes the line cards. Total is the number of lines;
// when it is more than len(Cards), "Show all" and "Find a line" appear.
type LineListProps struct {
	Cards                                           []LineCard
	Grid                                            []LineGridRow
	TaxableTotal, AddURL, ShowAllURL, FindURL, Find string
	Total                                           int
	Editable                                        bool
	AddLabel                                        string // the add button's label; "" is AddItem (UX-E2)
	AddTop                                          bool   // the add button above the cards (UX-E2)
	Empty                                           string // the text with no lines (UX-E2)
	Display                                         bool   // Grid rows are read-only (CD-313)
}

// AddItem is the add button's and the new-line page's words when a module
// names none (UX-E2).
const AddItem = "Add an item"

// AddButton is the add button of the line cards: AddLabel, or AddItem.
func (p LineListProps) AddButton() ButtonProps {
	return ButtonProps{Label: orDefault(p.AddLabel, AddItem), Href: p.AddURL, Icon: "plus", Variant: Secondary, Attrs: templ.Attributes{"data-add-line": true}}
}

func lineAria(verb string, no int) string {
	return verb + " line " + strconv.Itoa(no)
}

func lineGridID(no int) string {
	return "grid-row-" + strconv.Itoa(no)
}

// moveVersionID is the id of a grid move form's version input; dir is "up"
// or "down".
func moveVersionID(no int, dir string) string {
	return "grid-" + strconv.Itoa(no) + "-move-" + dir + "-version"
}

// gridCols is the grid's column class for its cell count. Only three-cell
// grids have their own column layout (linegrid--3); every other count uses
// the five-cell tracks.
func gridCols(rows []LineGridRow) string {
	if len(rows) > 0 && len(rows[0].Cells) == 3 {
		return "linegrid--3"
	}
	return ""
}
