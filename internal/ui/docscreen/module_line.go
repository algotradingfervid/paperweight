package docscreen

import (
	"errors"
	"net/http"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// errLineSaveLines is a Line.Save that returned Save.Lines: a line page saves
// one line, never the whole set (CD-256 (5)).
var errLineSaveLines = errors.New("docscreen: Line.Save returned Save.Lines")

// lineTarget is target for the line routes: with the module's NoLinePages
// every one answers 404, before any version check (CD-256 (7)).
func (d Deps) lineTarget(w http.ResponseWriter, r *http.Request) (*doctype.DocType, int64, bool) {
	dt, id, ok := target(w, r)
	if ok && d.modOf(dt).NoLinePages {
		problem(w, r, doctype.ErrNotFound)
		return nil, 0, false
	}
	return dt, id, ok
}

// gridShown reports whether v's Items step has the editable desktop grid:
// not with the module's CardsOnly, Line or NoLinePages (CD-256 (6)), nor
// with its LineGrid, edited only on the line page (CD-313), and only when a
// row has 3 or 5 cells, the sizes the grid lays out (M0-35). Any other size
// shows the cards at every width, edited on the line page (M0-40). The size
// is v's first line's, or a new line's when v has none (CD-315).
func (d Deps) gridShown(r *http.Request, dt *doctype.DocType, v DocView) bool {
	m := d.modOf(dt)
	if m.CardsOnly || m.Line != nil || m.NoLinePages || m.LineGrid != nil {
		return false
	}
	var lv LineView // line 0, a new line: Hide sees no stored inputs
	if len(v.Lines) > 0 {
		lv = v.Lines[0]
	}
	n := len(d.gridRowOf(r, dt, v, lv, "", nil, nil).Cells)
	return n == 3 || n == 5
}

// itemsLabel is the Items step's label as the module names it, for the line
// and remove pages' Back link (CD-256 (9)).
func (d Deps) itemsLabel(dt *doctype.DocType) string {
	if l := d.modOf(dt).StepLabels[slugItems]; l != "" {
		return l
	}
	return "Items"
}

// lineBody is the line page with the module's Line.Body in place of the
// line fields, for line no (0 = a new line); s is its redraw state, for
// Line.Top too, which renders outside the form with the id line-form
// (UX-E1, CD-275).
func (d Deps) lineBody(r *http.Request, dt *doctype.DocType, v DocView, no, version int, s StepState, danger string) (linePageView, error) {
	lp := d.lineShell(r, dt, v, no, version, s.Errors, danger)
	line, c := d.modOf(dt).Line, d.lineCtx(r, dt, &v, no)
	if line.Top != nil {
		top, err := line.Top(c, no, s)
		if err != nil {
			return linePageView{}, err
		}
		lp.Top = top
	}
	body, err := line.Body(c, no, s)
	if err != nil {
		return linePageView{}, err
	}
	lp.Inputs = append(lp.Inputs, body)
	return lp, nil
}

// lineSaved is Line.Save's answer for line no as postedLine's: the values to
// merge and the parse errors. A set Save.Lines is an internal error.
func (d Deps) lineSaved(c Ctx, no int) (map[string]string, []doctype.FieldError, error) {
	s, err := d.modOf(c.DT).Line.Save(c, no)
	if err != nil {
		return nil, nil, err
	}
	if s.Lines != nil {
		return nil, nil, errLineSaveLines
	}
	return s.Values, s.Errors, nil
}

// displayRows are v's lines as the display grid (CD-313), nil without the
// module's LineGrid: its cells for each line, with # and Amount, the note
// and the actions of that line's card in cards (parallel to v.Lines), so
// the grid shows actions only where the card does.
func (d Deps) displayRows(r *http.Request, dt *doctype.DocType, v DocView, cards []components.LineCard) []components.LineGridRow {
	lg := d.modOf(dt).LineGrid
	if lg == nil {
		return nil
	}
	rows := make([]components.LineGridRow, len(v.Lines))
	for i, lv := range v.Lines {
		c := cards[i]
		rows[i] = components.LineGridRow{
			No: lv.No, Cells: lg(d.lineCtx(r, dt, &v, lv.No), lv), Amount: c.Amount, Note: c.Note,
			ChangeURL: c.ChangeURL, RemoveURL: c.RemoveURL, MoveUpURL: c.MoveUpURL, MoveDownURL: c.MoveDownURL, Version: c.Version,
		}
	}
	return rows
}
