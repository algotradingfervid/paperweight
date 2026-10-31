package docscreen

import (
	"errors"
	"net/http"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
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

// gridShown reports whether dt's Items step has the desktop grid: not with
// the module's CardsOnly, Line or NoLinePages (CD-256 (6)).
func (d Deps) gridShown(dt *doctype.DocType) bool {
	m := d.modOf(dt)
	return !m.CardsOnly && m.Line == nil && !m.NoLinePages
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
	lp := d.lineShell(dt, v, no, version, s.Errors, danger)
	lp.FormID = "line-form"
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
