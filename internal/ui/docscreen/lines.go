package docscreen

import (
	"maps"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// phoneCards is how many line cards the items step shows before "Show all"
// (design-system §8.2).
const phoneCards = 20

// itemsURL is draft id's items step.
func itemsURL(name string, id int64) string {
	return docURL(name, id) + "/edit/" + slugItems
}

func lineURL(name string, id int64, no int) string {
	return docURL(name, id) + "/lines/" + strconv.Itoa(no)
}

// itemsView is the items step after the step form.
type itemsView struct {
	List   components.LineListProps
	Totals []components.Pair
}

// itemsBody is the line editor (design-system §8.2): the first 20 cards
// (?all=1 for all, ?find= to filter by item name or code), the desktop grid
// with every line (CD-139), and the computed totals.
func (d Deps) itemsBody(r *http.Request, dt *doctype.DocType, v DocView) templ.Component {
	q := r.URL.Query()
	find := strings.TrimSpace(q.Get("find"))
	version := strconv.Itoa(v.Version)
	iv := itemsView{
		List: components.LineListProps{
			AddURL:     docURL(dt.Name, v.Info.ID) + "/lines/new",
			ShowAllURL: itemsURL(dt.Name, v.Info.ID) + "?all=1",
			FindURL:    itemsURL(dt.Name, v.Info.ID),
			Find:       find,
			Total:      len(v.Lines),
			Editable:   true,
		},
		Totals: totalsOf(dt, v),
	}
	needle := strings.ToLower(find)
	for _, lv := range v.Lines {
		iv.List.Grid = append(iv.List.Grid, d.gridRowOf(dt, v, lv, version, lv.Inputs, nil))
		c := lv.Card
		if needle != "" && !strings.Contains(strings.ToLower(c.Item), needle) && !strings.Contains(strings.ToLower(c.Code), needle) {
			continue
		}
		base := lineURL(dt.Name, v.Info.ID, lv.No)
		c.ChangeURL, c.RemoveURL, c.Version = base, base+"/remove", version
		c.MoveUpURL, c.MoveDownURL = moveURLs(base, lv.No, len(v.Lines))
		iv.List.Cards = append(iv.List.Cards, c)
	}
	if q.Get("all") != "1" && len(iv.List.Cards) > phoneCards {
		iv.List.Cards = iv.List.Cards[:phoneCards]
	}
	return itemsPart(iv)
}

// moveURLs are line no's move URLs; the first has no up and the last no down.
func moveURLs(base string, no, n int) (up, down string) {
	if no > 1 {
		up = base + "/move?dir=up"
	}
	if no < n {
		down = base + "/move?dir=down"
	}
	return up, down
}

// totalsOf are the computed header fields (CD-79) as the page shows them.
func totalsOf(dt *doctype.DocType, v DocView) []components.Pair {
	var out []components.Pair
	for _, f := range dt.Fields {
		if !f.OnLine && f.ReadOnly && v.Display[f.Name] != "" {
			out = append(out, components.Pair{Label: f.Label, Value: v.Display[f.Name], Num: numeric(f)})
		}
	}
	return out
}

// gridRowOf is line lv's grid row (CD-139): a Link shows its master's
// summary with a hidden id; every other editable field is the line page's
// FieldInput with the id grid-<no>-<field>. values are in input form.
func (d Deps) gridRowOf(dt *doctype.DocType, v DocView, lv LineView, version string, values map[string]string, errs []doctype.FieldError) components.LineGridRow {
	base := lineURL(dt.Name, v.Info.ID, lv.No)
	row := components.LineGridRow{No: lv.No, Amount: lv.Card.Amount, PostURL: base, RemoveURL: base + "/remove", Version: version}
	row.MoveUpURL, row.MoveDownURL = moveURLs(base, lv.No, len(v.Lines))
	for _, f := range dt.Fields {
		if !f.OnLine || !editable(f) {
			continue
		}
		var in templ.Component
		id := "grid-" + strconv.Itoa(lv.No) + "-" + f.Name
		if f.Type == doctype.Link {
			in = gridItem(id, f.Label, lv.Display[f.Name], f.Name, values[f.Name], lineError(errs, f.Name))
		} else {
			in = FieldInput(f, id, values[f.Name], lineError(errs, f.Name), d)
		}
		row.Cells = append(row.Cells, components.GridCell{Label: f.Label, Input: in})
	}
	return row
}

// lineError is the message of the line field's first error.
func lineError(errs []doctype.FieldError, name string) string {
	for _, e := range errs {
		if e.Field == name {
			return e.Message
		}
	}
	return ""
}

// postedLine reads the line fields from the POST: typed text and plain
// values (ToPlain). A parse error is a FieldError with NUM's text.
func postedLine(r *http.Request, dt *doctype.DocType) (typed, plain map[string]string, errs []doctype.FieldError) {
	typed, plain = map[string]string{}, map[string]string{}
	for _, f := range dt.Fields {
		if !f.OnLine || !editable(f) {
			continue
		}
		typed[f.Name] = r.PostFormValue(f.Name)
		v, fe := ToPlain(f, typed[f.Name])
		if fe != nil {
			errs = append(errs, *fe)
			continue
		}
		plain[f.Name] = v
	}
	return typed, plain, errs
}

// lineNo reads {no}: a line of rec, 1-based.
func lineNo(r *http.Request, n int) (int, bool) {
	no, err := strconv.Atoi(chi.URLParam(r, "no"))
	return no, err == nil && no >= 1 && no <= n
}

// linePageView is the one-line form page, or the remove confirm page.
type linePageView struct {
	Opts     layout.PageOpts
	Action   string
	Version  int
	Inputs   []templ.Component
	Question string // the remove page's question
	Button   components.ButtonProps
	Danger   string
	Errors   []components.FieldErr
}

// draftView reads draft id for a line page; anything past draft goes to
// its document page.
func (d Deps) draftView(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64) (DocView, bool) {
	v, err := d.View(r.Context(), d.principal(r), dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return DocView{}, false
	}
	if v.Info.State != doctype.StateDraft {
		http.Redirect(w, r, docURL(dt.Name, id), http.StatusSeeOther) //nolint:gosec // G710: a local /doc/ path from a registered DocType name and a numeric id
		return DocView{}, false
	}
	return v, true
}

// linePage builds the line form for line no (0 = a new line) from values
// in input form.
func (d Deps) linePage(dt *doctype.DocType, v DocView, no, version int, values map[string]string, errs []doctype.FieldError, danger string) linePageView {
	lv := linePageView{
		Opts:    layout.PageOpts{Title: "Add an item", Width: layout.Form, Back: &components.Link{Label: "Items", Href: itemsURL(dt.Name, v.Info.ID)}},
		Action:  docURL(dt.Name, v.Info.ID) + "/lines/new",
		Version: version,
		Button:  components.ButtonProps{Label: "Save line", Variant: components.Primary},
		Danger:  danger,
		Errors:  fieldErrs(errs),
	}
	if no > 0 {
		lv.Opts.Title = "Line " + strconv.Itoa(no)
		lv.Action = lineURL(dt.Name, v.Info.ID, no)
	}
	for _, f := range dt.Fields {
		if f.OnLine && editable(f) {
			lv.Inputs = append(lv.Inputs, FieldInput(f, components.FieldID(f.Name), values[f.Name], lineError(errs, f.Name), d))
		}
	}
	for n, e := range errs {
		if e.Field == "" || e.Field == "lines" {
			lv.Errors[n].ID = "main" // no input to link to
		}
	}
	lv.Opts.Error = danger != "" || len(errs) > 0
	return lv
}

// lineGet serves GET …/lines/new and …/lines/{no}.
func (d Deps) lineGet(w http.ResponseWriter, r *http.Request) {
	dt, id, ok := target(w, r)
	if !ok {
		return
	}
	v, ok := d.draftView(w, r, dt, id)
	if !ok {
		return
	}
	no, values := 0, map[string]string{}
	if chi.URLParam(r, "no") != "" {
		if no, ok = lineNo(r, len(v.Lines)); !ok {
			problem(w, r, doctype.ErrNotFound)
			return
		}
		values = v.Lines[no-1].Inputs
	}
	lp := d.linePage(dt, v, no, v.Version, values, nil, "")
	ui.Render(w, r, http.StatusOK, linePageFull(lp), linePageMain(lp))
}

// linePost serves POST …/lines/new and …/lines/{no}: the loaded lines,
// plain and with their ids, with this one replaced or added, saved with
// the posted version (CD-94) and scoped to the lines, so only a changed
// line's error refuses (CD-207). An htmx post comes from the desktop grid and
// gets its row back (CD-95).
func (d Deps) linePost(w http.ResponseWriter, r *http.Request) {
	dt, id, ok := target(w, r)
	if !ok {
		return
	}
	version, ok := postedVersion(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	rec, err := d.load(ctx, dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	no := 0
	if chi.URLParam(r, "no") != "" {
		if no, ok = lineNo(r, len(rec.Lines)); !ok {
			problem(w, r, doctype.ErrNotFound)
			return
		}
	}
	grid := ui.IsHTMX(r) && no > 0
	typed, plain, errs := postedLine(r, dt)
	refused := func(pr Problem) {
		if grid {
			d.gridRefused(w, r, dt, id, no, version, typed, pr)
			return
		}
		v, ok := d.draftView(w, r, dt, id)
		if !ok {
			return
		}
		danger := ""
		if len(pr.Fields) == 0 {
			danger = pr.Message
		}
		lp := d.linePage(dt, v, no, version, typed, pr.Fields, danger)
		ui.Render(w, r, pr.Status, linePageFull(lp), linePageMain(lp))
	}
	if len(errs) > 0 {
		refused(Problem{Status: http.StatusUnprocessableEntity, Fields: errs})
		return
	}
	lines := rec.Lines
	if no == 0 {
		lines = append(lines, plain)
	} else {
		l := maps.Clone(lines[no-1])
		maps.Copy(l, plain)
		lines[no-1] = l
	}
	res, err := d.Engine.Do(ctx, d.principal(r), dt.Name, id, doctype.ActionSave, doc.ActionInput{Version: version, Lines: lines, Only: []string{"lines"}})
	if err != nil {
		pr, ok := Classify(err)
		switch {
		case !ok:
			web.ErrorFrom(w, r, err)
		case pr.Status == http.StatusUnprocessableEntity || pr.Status == http.StatusConflict:
			pos := no
			if pos == 0 {
				pos = len(lines)
			}
			pr.Fields = lineErrors(pr.Fields, pos)
			refused(pr)
		default:
			problem(w, r, err)
		}
		return
	}
	if grid {
		d.gridSaved(w, r, dt, id, no, res)
		return
	}
	d.backToItems(w, r, dt, id, res.Warnings)
}

// lineErrors keeps the errors of line pos, and those no line owns: with no
// field name, or on the lines as a whole. When none is left, a refusal must
// still say why: each error's own message, with no field.
func lineErrors(errs []doctype.FieldError, pos int) []doctype.FieldError {
	var out []doctype.FieldError
	for _, e := range errs {
		if e.Line == pos || e.Field == "" || e.Field == "lines" {
			out = append(out, doctype.FieldError{Field: e.Field, Message: e.Message})
		}
	}
	if len(out) == 0 {
		return messagesOnly(errs)
	}
	return out
}

// backToItems flashes the save's warnings and goes back to the items step
// (CD-68, CD-80).
func (d Deps) backToItems(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64, warnings []string) {
	for _, msg := range warnings {
		web.Flash(r.Context(), "warning", msg)
	}
	http.Redirect(w, r, itemsURL(dt.Name, id), http.StatusSeeOther) //nolint:gosec // G710: a local /doc/ path from a registered DocType name and a numeric id
}

// gridSaved answers a grid row save: the row as saved, and out of band
// the new #doc-version, the move forms' versions and the totals (CD-95).
func (d Deps) gridSaved(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64, no int, res doc.Result) {
	v, err := d.View(r.Context(), d.principal(r), dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	version := strconv.Itoa(res.Version)
	var rows []components.LineGridRow
	for _, lv := range v.Lines {
		rows = append(rows, d.gridRowOf(dt, v, lv, version, lv.Inputs, nil))
	}
	if no > len(rows) {
		problem(w, r, doctype.ErrNotFound)
		return
	}
	g := gridView{Row: rows[no-1], Rows: rows, Version: res.Version, Totals: totalsOf(dt, v), Warnings: res.Warnings}
	ui.Render(w, r, http.StatusOK, gridResponse(g), nil)
}

// gridRefused answers a refused grid row save with the typed values kept:
// field errors in the row, any other message in #grid-alerts out of band.
func (d Deps) gridRefused(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64, no, version int, typed map[string]string, pr Problem) {
	v, err := d.View(r.Context(), d.principal(r), dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	if no > len(v.Lines) {
		problem(w, r, doctype.ErrNotFound)
		return
	}
	g := gridView{Row: d.gridRowOf(dt, v, v.Lines[no-1], strconv.Itoa(version), typed, pr.Fields)}
	g.Alert, g.ReloadURL = formText(pr), itemsURL(dt.Name, id)
	ui.Render(w, r, pr.Status, gridResponse(g), nil)
}

// formText is what a refusal says beyond a row's cells: its message when it
// has no field errors or is a conflict, else the errors no cell can show.
func formText(pr Problem) string {
	if len(pr.Fields) == 0 || pr.Status == http.StatusConflict {
		return pr.Message
	}
	var out []string
	for _, e := range pr.Fields {
		if e.Field == "" || e.Field == "lines" {
			out = append(out, e.Message)
		}
	}
	return strings.Join(out, " ")
}

// gridView is a grid row save's response.
type gridView struct {
	Row              components.LineGridRow
	Rows             []components.LineGridRow // non-nil after a save: the move forms to update
	Version          int
	Totals           []components.Pair
	Warnings         []string
	Alert, ReloadURL string
}

// removeGet serves GET …/lines/{no}/remove: "Remove line {no} ({item})?".
func (d Deps) removeGet(w http.ResponseWriter, r *http.Request) {
	dt, id, ok := target(w, r)
	if !ok {
		return
	}
	v, ok := d.draftView(w, r, dt, id)
	if !ok {
		return
	}
	no, ok := lineNo(r, len(v.Lines))
	if !ok {
		problem(w, r, doctype.ErrNotFound)
		return
	}
	lp := removePage(dt, v, no, v.Version, "")
	ui.Render(w, r, http.StatusOK, linePageFull(lp), linePageMain(lp))
}

func removePage(dt *doctype.DocType, v DocView, no, version int, danger string) linePageView {
	n := strconv.Itoa(no)
	q := "Remove line " + n + "?"
	if item := v.Lines[no-1].Card.Item; item != "" {
		q = "Remove line " + n + " (" + item + ")?"
	}
	return linePageView{
		Opts:     layout.PageOpts{Title: "Remove line " + n, Width: layout.Form, Error: danger != "", Back: &components.Link{Label: "Items", Href: itemsURL(dt.Name, v.Info.ID)}},
		Action:   lineURL(dt.Name, v.Info.ID, no) + "/remove",
		Version:  version,
		Question: q,
		Button:   components.ButtonProps{Label: "Remove line", Variant: components.Danger},
		Danger:   danger,
	}
}

// removePost serves POST …/lines/{no}/remove.
func (d Deps) removePost(w http.ResponseWriter, r *http.Request) {
	d.reorder(w, r, func(lines []map[string]string, no int) ([]map[string]string, bool) {
		return append(lines[:no-1:no-1], lines[no:]...), true
	})
}

// movePost serves POST …/lines/{no}/move?dir=up|down; the first line up and
// the last down change nothing.
func (d Deps) movePost(w http.ResponseWriter, r *http.Request) {
	step := map[string]int{"up": -1, "down": 1}[r.URL.Query().Get("dir")]
	if step == 0 {
		problem(w, r, doctype.ErrNotFound)
		return
	}
	d.reorder(w, r, func(lines []map[string]string, no int) ([]map[string]string, bool) {
		j := no - 1 + step
		if j < 0 || j >= len(lines) {
			return lines, false
		}
		lines[no-1], lines[j] = lines[j], lines[no-1]
		return lines, true
	})
}

// reorder saves the loaded lines as change returns them, with the posted
// version (CD-94) and scoped to the lines (CD-207), and goes back to the items
// step. A refusal shows the items step (or the remove page) with the kernel's
// message and status.
func (d Deps) reorder(w http.ResponseWriter, r *http.Request, change func(lines []map[string]string, no int) ([]map[string]string, bool)) {
	dt, id, ok := target(w, r)
	if !ok {
		return
	}
	version, ok := postedVersion(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	rec, err := d.load(ctx, dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	no, ok := lineNo(r, len(rec.Lines))
	if !ok {
		problem(w, r, doctype.ErrNotFound)
		return
	}
	lines, changed := change(rec.Lines, no)
	if !changed {
		d.backToItems(w, r, dt, id, nil)
		return
	}
	res, err := d.Engine.Do(ctx, d.principal(r), dt.Name, id, doctype.ActionSave, doc.ActionInput{Version: version, Lines: lines, Only: []string{"lines"}})
	if err != nil {
		pr, ok := Classify(err)
		if !ok {
			web.ErrorFrom(w, r, err)
			return
		}
		d.itemsRefused(w, r, dt, id, no, version, pr)
		return
	}
	d.backToItems(w, r, dt, id, res.Warnings)
}

// itemsRefused re-shows the remove page, or the items step for a move, with
// the kernel's message unchanged, or its field errors when it has any, and
// its status.
func (d Deps) itemsRefused(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64, no, version int, pr Problem) {
	v, ok := d.draftView(w, r, dt, id)
	if !ok {
		return
	}
	if strings.HasSuffix(r.URL.Path, "/remove") && no <= len(v.Lines) {
		danger := formText(pr)
		if danger == "" {
			pr.Fields = messagesOnly(pr.Fields)
			danger = formText(pr)
		}
		lp := removePage(dt, v, no, version, danger)
		ui.Render(w, r, pr.Status, linePageFull(lp), linePageMain(lp))
		return
	}
	steps, err := FormSteps(dt)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	i, _ := stepOf(steps, slugItems)
	fields := refusalErrors(steps, i, pr.Fields)
	sv := d.stepViewOf(r, dt, steps, i, &v, nil, fields)
	if len(fields) == 0 {
		sv.M.Conflict, sv.M.ReloadURL = pr.Message, itemsURL(dt.Name, id)
	}
	ui.Render(w, r, pr.Status, stepFull(sv), stepMain(sv))
}

func linePageOpts(lp linePageView) layout.PageOpts {
	o := lp.Opts
	o.BannerSlot = linePageBanners(lp)
	return o
}
