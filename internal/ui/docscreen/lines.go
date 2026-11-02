package docscreen

import (
	"cmp"
	"maps"
	"math"
	"net/http"
	"slices"
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

// addAgainFrom is how many lines bring the module's AddTop button back
// after the totals (UX-E2, EUX-037).
const addAgainFrom = 4

// itemsURL is draft id's items step.
func itemsURL(name string, id int64) string {
	return docURL(name, id) + "/edit/" + slugItems
}

func lineURL(name string, id int64, no int) string {
	return docURL(name, id) + "/lines/" + strconv.Itoa(no)
}

// itemsView is the items step after the step form. AddAgain repeats the
// add button after the totals.
type itemsView struct {
	List     components.LineListProps
	Totals   []components.Pair
	AddAgain bool
}

// itemsBody is the line editor (design-system §8.2): the first 20 cards
// (?all=1 for all, ?find= to filter by item name or code), the desktop grid
// with every line (CD-139) or the module's display grid (CD-313), and the
// computed totals. The module's CardsOnly, Line and NoLinePages leave the
// editable grid out (CD-256 (6)), as does a row of other than 3 or 5 cells
// (CD-315); NoLinePages also leaves out every link to a line page
// (CD-256 (7)).
func (d Deps) itemsBody(c Ctx, v DocView) templ.Component {
	dt := c.DT
	m := d.modOf(dt)
	grid := d.gridShown(c.Req, dt, v)
	q := c.Req.URL.Query()
	find := strings.TrimSpace(q.Get("find"))
	version := strconv.Itoa(v.Version)
	iv := itemsView{
		List: components.LineListProps{
			AddURL:     docURL(dt.Name, v.Info.ID) + "/lines/new",
			ShowAllURL: itemsURL(dt.Name, v.Info.ID) + "?all=1",
			FindURL:    itemsURL(dt.Name, v.Info.ID),
			Find:       find,
			Total:      len(v.Lines),
			Editable:   !m.NoLinePages,
			AddLabel:   m.LineNoun.Add,
			AddTop:     m.AddTop,
			Empty:      m.LineNoun.Empty,
		},
		Totals:   totalsOf(c, v),
		AddAgain: m.AddTop && !m.NoLinePages && len(v.Lines) >= addAgainFrom,
	}
	needle := strings.ToLower(find)
	var all []components.LineCard // every line's card, for the display grid
	for _, lv := range v.Lines {
		if grid {
			iv.List.Grid = append(iv.List.Grid, d.gridRowOf(c.Req, dt, v, lv, version, lv.Inputs, nil))
		}
		card := lv.Card
		if iv.List.Editable {
			base := lineURL(dt.Name, v.Info.ID, lv.No)
			card.ChangeURL, card.RemoveURL, card.Version = base, base+"/remove", version
			card.MoveUpURL, card.MoveDownURL = moveURLs(base, lv.No, len(v.Lines))
		}
		all = append(all, card)
		if needle != "" && !strings.Contains(strings.ToLower(card.Item), needle) && !strings.Contains(strings.ToLower(card.Code), needle) {
			continue
		}
		iv.List.Cards = append(iv.List.Cards, card)
	}
	if m.LineGrid != nil {
		iv.List.Grid, iv.List.Display = d.displayRows(c.Req, dt, v, all), true
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

// totalsOf are the computed header fields (CD-79) as the page shows them,
// but those the module hides.
func totalsOf(c Ctx, v DocView) []components.Pair {
	var out []components.Pair
	for _, f := range c.DT.Fields {
		if !f.OnLine && f.ReadOnly && v.Display[f.Name] != "" && !hidden(c, f) {
			out = append(out, components.Pair{Label: f.Label, Value: v.Display[f.Name], Num: numeric(f)})
		}
	}
	return out
}

// footerID is the Items footer total's id, which a grid save swaps (A-22).
const footerID = "footer-total"

// footerTotal is the Items footer's summary (UI v2 A-22): "{n} items ·
// {label} ₹…" for the module's FooterTotal, n the saved lines; only the
// count when the module hides the field or the total is blank (CD-378 (4));
// nil when the module names none.
func footerTotal(c Ctx, v DocView) *components.BarTotal {
	name := c.Deps.modOf(c.DT).FooterTotal
	if name == "" {
		return nil
	}
	t := &components.BarTotal{ID: footerID, Count: strconv.Itoa(len(v.Lines)) + " items"}
	if len(v.Lines) == 1 {
		t.Count = "1 item"
	}
	if f, _ := c.DT.Field(name); !hidden(c, f) && v.Display[name] != "" {
		t.Label, t.Value = f.Label, v.Display[name]
	}
	return t
}

// gridRowOf is line lv's grid row (CD-139): a Link shows its master's
// summary with a hidden id; every other editable field is the line page's
// FieldInput with the id grid-<no>-<field>. values are in input form. A field
// the module hides has no cell; Hide sees the line's stored inputs in Deps.
func (d Deps) gridRowOf(r *http.Request, dt *doctype.DocType, v DocView, lv LineView, version string, values map[string]string, errs []doctype.FieldError) components.LineGridRow {
	c := d.lineCtx(r, dt, &v, lv.No)
	base := lineURL(dt.Name, v.Info.ID, lv.No)
	row := components.LineGridRow{No: lv.No, Amount: lv.Card.Amount, PostURL: base, RemoveURL: base + "/remove", Version: version}
	row.MoveUpURL, row.MoveDownURL = moveURLs(base, lv.No, len(v.Lines))
	for _, f := range dt.Fields {
		if !f.OnLine || !editable(f) || hidden(c, f) {
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
// values (ToPlain). A parse error is a FieldError with NUM's text. A field
// the module hides is left out, so the loaded line keeps its value.
func postedLine(c Ctx) (typed, plain map[string]string, errs []doctype.FieldError) {
	typed, plain = typedLine(c), map[string]string{}
	for _, f := range c.DT.Fields {
		if _, ok := typed[f.Name]; !ok {
			continue
		}
		v, fe := ToPlain(f, typed[f.Name])
		if fe != nil {
			errs = append(errs, *fe)
			continue
		}
		plain[f.Name] = v
	}
	return typed, plain, errs
}

// typedLine is the text the POST holds for the line fields, but those the
// module hides; nothing is parsed (CD-335).
func typedLine(c Ctx) map[string]string {
	typed := map[string]string{}
	for _, f := range c.DT.Fields {
		if f.OnLine && editable(f) && !hidden(c, f) {
			typed[f.Name] = c.Req.PostFormValue(f.Name)
		}
	}
	return typed
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
	Reload   *components.Link // a 409's document and page: Danger shows as the ConflictBanner
	Required bool             // a line field the form shows is required: it starts with components.RequiredNote
	Errors   []components.FieldErr
	Top      templ.Component // the module's Line.Top, outside the form (UX-E1)
	FormID   string          // the form's id: line-form with the module's Line
}

// conflict makes a 409's Danger the ConflictBanner, whose Reload names the
// document and goes to href (CD-362).
func (lp *linePageView) conflict(pr Problem, label, href string) {
	if pr.Status == http.StatusConflict {
		lp.Reload = &components.Link{Label: label, Href: href}
	}
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

// lineShell is line no's page (0 = a new line) without its inputs: title
// (in the module's LineNoun), action, version, Back, button and the refusal.
// An error on a field the module hides is a plain message (CD-319).
func (d Deps) lineShell(r *http.Request, dt *doctype.DocType, v DocView, no, version int, errs []doctype.FieldError, danger string) linePageView {
	c := d.lineCtx(r, dt, &v, no)
	errs = unhiddenAll(c, errs)
	noun := d.modOf(dt).LineNoun
	lv := linePageView{
		Opts:    layout.PageOpts{Title: cmp.Or(noun.Add, components.AddItem), Width: layout.Form, Focus: true, Back: &components.Link{Label: d.itemsLabel(dt), Href: itemsURL(dt.Name, v.Info.ID)}},
		Action:  docURL(dt.Name, v.Info.ID) + "/lines/new",
		Version: version,
		Button:  components.ButtonProps{Label: "Save line", Variant: components.Primary},
		Danger:  danger,
		Errors:  fieldErrs(errs),
		Required: slices.ContainsFunc(dt.Fields, func(f doctype.Field) bool {
			return f.OnLine && editable(f) && required(f) && !hidden(c, f)
		}),
	}
	if no > 0 {
		lv.Opts.Title = cmp.Or(noun.Title, "Line") + " " + strconv.Itoa(no)
		lv.Action = lineURL(dt.Name, v.Info.ID, no)
	}
	for n, e := range errs {
		if e.Field == "" || e.Field == "lines" {
			lv.Errors[n].ID = "main" // no input to link to
		}
	}
	lv.Opts.Error = danger != "" || len(errs) > 0
	return lv
}

// linePage builds the line form for line no (0 = a new line) from values
// in input form, without the fields the module hides.
func (d Deps) linePage(r *http.Request, dt *doctype.DocType, v DocView, no, version int, values map[string]string, errs []doctype.FieldError, danger string) linePageView {
	lv := d.lineShell(r, dt, v, no, version, errs, danger)
	c := d.lineCtx(r, dt, &v, no)
	var shown []MoreInput
	for _, f := range dt.Fields {
		if !f.OnLine || !editable(f) || hidden(c, f) {
			continue
		}
		msg := lineError(errs, f.Name)
		shown = append(shown, MoreInput{Field: f, Value: values[f.Name], Error: msg, Input: FieldInput(f, components.FieldID(f.Name), values[f.Name], msg, c.Deps)})
	}
	lv.Inputs = append(lv.Inputs, GroupMore(shown).Inputs()...)
	return lv
}

// lineGet serves GET …/lines/new and …/lines/{no}: the module's Line.Body
// when it has one. A reader goes to the document page (CD-263 G1).
func (d Deps) lineGet(w http.ResponseWriter, r *http.Request) {
	dt, id, ok := d.lineTarget(w, r)
	if !ok {
		return
	}
	v, ok := d.draftView(w, r, dt, id)
	if !ok || d.readOnly(w, r, dt, v) {
		return
	}
	no := 0
	if chi.URLParam(r, "no") != "" {
		if no, ok = lineNo(r, len(v.Lines)); !ok {
			problem(w, r, doctype.ErrNotFound)
			return
		}
	}
	if d.modOf(dt).Line != nil {
		lp, err := d.lineBody(r, dt, v, no, v.Version, StepState{}, "")
		if err != nil {
			web.ErrorFrom(w, r, err)
			return
		}
		ui.Render(w, r, http.StatusOK, linePageFull(lp), linePageMain(lp))
		return
	}
	values := map[string]string{}
	if no > 0 {
		values = v.Lines[no-1].Inputs
	}
	lp := d.linePage(r, dt, v, no, v.Version, values, nil, "")
	ui.Render(w, r, http.StatusOK, linePageFull(lp), linePageMain(lp))
}

// linePost serves POST …/lines/new and …/lines/{no}: the loaded lines,
// plain and with their ids, with this one replaced or added, saved with
// the posted version (CD-94) and scoped to the lines, so only a changed
// line's error refuses (CD-207). An htmx post comes from the desktop grid and
// gets its row back (CD-95). With the module's Line, Line.Save gives the
// values and parse errors in place of the posted fields, and a refusal
// redraws Line.Body with Posted set (hooks spec §4.2, CD-256 (5)).
func (d Deps) linePost(w http.ResponseWriter, r *http.Request) {
	dt, id, ok := d.lineTarget(w, r)
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
	// Hide sees the draft as loaded, as on GET (CD-256 (2)).
	loaded, err := d.View(ctx, d.principal(r), dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	no := 0
	gone := "" // with a line removed since a stale page, its 409's Reload
	if chi.URLParam(r, "no") != "" {
		// Both reads must have line no. A stale page's line may be gone: DOC's
		// 409 below redraws its form as a new line's, so the entries stay, and
		// Reload goes to the items step. A grid row has no form to redraw.
		if no, ok = lineNo(r, min(len(rec.Lines), len(loaded.Lines))); !ok {
			if _, ok = lineNo(r, math.MaxInt); !ok || version == rec.Version || ui.IsHTMX(r) {
				problem(w, r, doctype.ErrNotFound)
				return
			}
			no, gone = 0, itemsURL(dt.Name, id)
		}
	}
	// A reader gets AUTH's refusal, as DOC gives it, before any parsing (CD-263 G1).
	if err := d.writeErr(r, dt, loaded); err != nil {
		problem(w, r, err)
		return
	}
	line := d.modOf(dt).Line
	grid := ui.IsHTMX(r) && no > 0 && d.gridShown(r, dt, loaded)
	var typed, plain map[string]string
	var errs []doctype.FieldError
	refused := func(pr Problem) {
		if grid {
			d.gridRefused(w, r, dt, id, no, version, typed, pr)
			return
		}
		v, ok := d.draftView(w, r, dt, id)
		if !ok {
			return
		}
		if no > len(v.Lines) { // removed since the save was read
			problem(w, r, doctype.ErrNotFound)
			return
		}
		danger := ""
		if len(pr.Fields) == 0 {
			danger = pr.Message
		}
		if line != nil {
			lp, err := d.lineBody(r, dt, v, no, version, StepState{Posted: true, Errors: pr.Fields}, danger)
			if err != nil {
				web.ErrorFrom(w, r, err)
				return
			}
			lp.conflict(pr, v.Info.Display, cmp.Or(gone, lp.Action))
			ui.Render(w, r, pr.Status, linePageFull(lp), linePageMain(lp))
			return
		}
		lp := d.linePage(r, dt, v, no, version, typed, pr.Fields, danger)
		lp.conflict(pr, v.Info.Display, cmp.Or(gone, lp.Action))
		ui.Render(w, r, pr.Status, linePageFull(lp), linePageMain(lp))
	}
	// engineRefused answers the engine's refusal of lines, line pos's errors
	// redrawing the page.
	engineRefused := func(err error, lines []map[string]string) {
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
	}
	// A stale page or a document past draft gets DOC's refusal before the
	// module's save or any parse (CD-335).
	if err := d.moved(r, dt, rec, version); err != nil {
		typed = typedLine(d.lineCtx(r, dt, &loaded, no))
		engineRefused(err, rec.Lines)
		return
	}
	if line != nil {
		if plain, errs, err = d.lineSaved(d.lineCtx(r, dt, &loaded, no), no); err != nil {
			problem(w, r, err)
			return
		}
	} else {
		typed, plain, errs = postedLine(d.lineCtx(r, dt, &loaded, no))
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
		engineRefused(err, lines)
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
	c := d.ctxOf(r, dt, &v)
	applyLineCards(c, &v)
	var rows []components.LineGridRow
	for _, lv := range v.Lines {
		rows = append(rows, d.gridRowOf(r, dt, v, lv, version, lv.Inputs, nil))
	}
	if no > len(rows) {
		problem(w, r, doctype.ErrNotFound)
		return
	}
	g := gridView{Row: rows[no-1], Rows: rows, Version: res.Version, Totals: totalsOf(c, v), Footer: footerTotal(c, v), Warnings: res.Warnings}
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
	applyLineCards(d.ctxOf(r, dt, &v), &v)
	pr.Fields = unhiddenAll(d.lineCtx(r, dt, &v, no), pr.Fields) // a hidden field has no cell: its error goes in the alert (CD-319)
	g := gridView{Row: d.gridRowOf(r, dt, v, v.Lines[no-1], strconv.Itoa(version), typed, pr.Fields)}
	g.Alert, g.ReloadURL = formText(pr), itemsURL(dt.Name, id)
	if pr.Status == http.StatusConflict {
		g.Conflict = v.Info.Display // the ConflictBanner's Reload names the document (CD-362)
	}
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
	Footer           *components.BarTotal // the Items footer's total, swapped out of band (A-22)
	Warnings         []string
	Alert, ReloadURL string
	Conflict         string // a 409's document label: Alert shows as the ConflictBanner
}

// removeGet serves GET …/lines/{no}/remove: "Remove line {no} ({item})?".
// A reader goes to the document page (CD-263 G1).
func (d Deps) removeGet(w http.ResponseWriter, r *http.Request) {
	dt, id, ok := d.lineTarget(w, r)
	if !ok {
		return
	}
	v, ok := d.draftView(w, r, dt, id)
	if !ok || d.readOnly(w, r, dt, v) {
		return
	}
	no, ok := lineNo(r, len(v.Lines))
	if !ok {
		problem(w, r, doctype.ErrNotFound)
		return
	}
	lp := d.removePage(r, dt, v, no, v.Version, "")
	ui.Render(w, r, http.StatusOK, linePageFull(lp), linePageMain(lp))
}

// removePage names line no by its card as the module shows it (LineCard).
func (d Deps) removePage(r *http.Request, dt *doctype.DocType, v DocView, no, version int, danger string) linePageView {
	applyLineCards(d.ctxOf(r, dt, &v), &v)
	n := strconv.Itoa(no)
	q := "Remove line " + n + "?"
	if item := v.Lines[no-1].Card.Item; item != "" {
		q = "Remove line " + n + " (" + item + ")?"
	}
	return linePageView{
		Opts:     layout.PageOpts{Title: "Remove line " + n, Width: layout.Form, Focus: true, Error: danger != "", Back: &components.Link{Label: d.itemsLabel(dt), Href: itemsURL(dt.Name, v.Info.ID)}},
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
	dt, id, ok := d.lineTarget(w, r)
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
		lp := d.removePage(r, dt, v, no, version, danger)
		lp.conflict(pr, v.Info.Display, lp.Action)
		notice(w, r, pr)
		ui.Render(w, r, pr.Status, linePageFull(lp), linePageMain(lp))
		return
	}
	steps, err := FormSteps(dt)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	i, _ := stepOf(steps, slugItems)
	fields := refusalErrors(d.ctxOf(r, dt, &v), steps, i, pr.Fields)
	sv, err := d.stepViewOf(r, dt, steps, i, &v, nil, fields)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	if len(fields) == 0 {
		sv.M.Conflict, sv.M.ReloadURL = pr.Message, itemsURL(dt.Name, id)
	}
	notice(w, r, pr)
	ui.Render(w, r, pr.Status, stepFull(sv), stepMain(sv))
}

// linePageOpts adds the banners and, with the module's Line, the sticky bar
// whose Save line posts the line form (UX-E4).
func linePageOpts(lp linePageView) layout.PageOpts {
	o := lp.Opts
	o.BannerSlot = linePageBanners(lp)
	if lp.FormID != "" {
		o.BarSlot = components.ActionBar(components.ActionBarProps{Primary: &lp.Button, Form: lp.FormID})
	}
	return o
}
