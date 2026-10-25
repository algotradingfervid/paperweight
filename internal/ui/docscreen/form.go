package docscreen

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// FormStep is one step of a DocType's wizard: its header fields, or the
// line pages when Lines is set (design-system §8.3).
type FormStep struct {
	Slug, Label string
	Fields      []string
	Lines       bool
}

// Fixed step slugs.
const (
	slugDetails = "details"
	slugItems   = "items"
	slugNotes   = "notes"
	slugCheck   = "check"
)

// FormSteps is dt's wizard (CD-42): header fields grouped by Field.Step in
// order of first appearance, an empty Step being "details"; then "items"
// when dt has lines; then "check". ReadOnly fields are listed on their step
// to be shown there (CD-79). Fewer than 3 steps adds "notes" before check;
// more than 5 is an error.
func FormSteps(dt *doctype.DocType) ([]FormStep, error) {
	var steps []FormStep
	add := func(slug string, name string) {
		i := slices.IndexFunc(steps, func(s FormStep) bool { return s.Slug == slug })
		if i < 0 {
			steps = append(steps, FormStep{Slug: slug, Label: slugLabel(slug)})
			i = len(steps) - 1
		}
		steps[i].Fields = append(steps[i].Fields, name)
	}
	for _, f := range dt.Fields {
		if !f.OnLine && !f.RollUp && f.Step == "" {
			add(slugDetails, f.Name)
		}
	}
	for _, f := range dt.Fields {
		if !f.OnLine && !f.RollUp && f.Step != "" {
			add(f.Step, f.Name)
		}
	}
	if dt.LinesTable != "" {
		steps = append(steps, FormStep{Slug: slugItems, Label: "Items", Lines: true})
	}
	if len(steps) < 2 {
		steps = append(steps, FormStep{Slug: slugNotes, Label: "Other details"})
	}
	steps = append(steps, FormStep{Slug: slugCheck, Label: "Check and submit"})
	if len(steps) > 5 {
		return nil, fmt.Errorf("docscreen: %s has %d wizard steps; the most is 5", dt.Name, len(steps))
	}
	return steps, nil
}

// slugLabel is a step's label from its slug: "delivery-details" is
// "Delivery details".
func slugLabel(slug string) string {
	if slug == slugDetails {
		return "Details"
	}
	s := strings.ReplaceAll(slug, "-", " ")
	return strings.ToUpper(s[:1]) + s[1:]
}

// firstFailingStep is the first step owning an error field; a line error
// belongs to the Lines step. With no owner it is the check step.
func firstFailingStep(steps []FormStep, errs []doctype.FieldError) int {
	best := len(steps) - 1
	for _, e := range errs {
		i := slices.IndexFunc(steps, func(s FormStep) bool {
			if e.Line > 0 {
				return s.Lines
			}
			return slices.Contains(s.Fields, e.Field)
		})
		if i >= 0 && i < best {
			best = i
		}
	}
	return best
}

// stepOf finds the step named by {step}.
func stepOf(steps []FormStep, slug string) (int, bool) {
	i := slices.IndexFunc(steps, func(s FormStep) bool { return s.Slug == slug })
	return i, i >= 0
}

// editable reports whether f is posted by a form: ReadOnly fields never
// are (CD-79).
func editable(f doctype.Field) bool { return !f.ReadOnly }

// postedStep reads step s's editable fields from the POST: the typed text
// and, through ToPlain, DOC's plain values. A parse error is a FieldError
// with NUM's text and leaves the field out of plain (CD-48).
func postedStep(r *http.Request, dt *doctype.DocType, s FormStep) (typed, plain map[string]string, errs []doctype.FieldError) {
	typed, plain = map[string]string{}, map[string]string{}
	for _, name := range s.Fields {
		f, ok := dt.Field(name)
		if !ok || !editable(f) {
			continue
		}
		typed[name] = r.PostFormValue(name)
		v, fe := ToPlain(f, typed[name])
		if fe != nil {
			errs = append(errs, *fe)
			continue
		}
		plain[name] = v
	}
	return typed, plain, errs
}

// postedVersion is the page's version; a missing or non-numeric one is
// CD-146's 400.
func postedVersion(w http.ResponseWriter, r *http.Request) (int, bool) {
	v, err := strconv.Atoi(r.PostFormValue("version"))
	if err != nil {
		web.Error(w, r, http.StatusBadRequest, badVersionText)
		return 0, false
	}
	return v, true
}

// newGet serves GET /doc/{doctype}/new: step 1 with no draft yet.
func (d Deps) newGet(w http.ResponseWriter, r *http.Request) {
	dt, steps, ok := d.newTarget(w, r)
	if !ok {
		return
	}
	sv := d.stepViewOf(r, dt, steps, 0, nil, nil, nil)
	ui.Render(w, r, http.StatusOK, stepFull(sv), stepFrame(sv))
}

// newPost serves POST /doc/{doctype}/new: step 1 creates the draft.
func (d Deps) newPost(w http.ResponseWriter, r *http.Request) {
	dt, steps, ok := d.newTarget(w, r)
	if !ok {
		return
	}
	typed, plain, errs := postedStep(r, dt, steps[0])
	invalid := func(status int, pr Problem) {
		sv := d.stepViewOf(r, dt, steps, 0, nil, typed, pr.Fields)
		if status == http.StatusConflict {
			sv.M.Conflict, sv.M.ReloadURL = pr.Message, "/doc/"+dt.Name+"/new"
		}
		ui.Render(w, r, status, stepFull(sv), stepFrame(sv))
	}
	if len(errs) > 0 {
		invalid(http.StatusUnprocessableEntity, Problem{Fields: errs})
		return
	}
	res, err := d.Engine.Do(r.Context(), d.principal(r), dt.Name, 0, doctype.ActionSave, doc.ActionInput{Values: plain})
	if err != nil {
		pr, ok := Classify(err)
		switch {
		case !ok:
			web.ErrorFrom(w, r, err)
		case pr.Status == http.StatusUnprocessableEntity || pr.Status == http.StatusConflict:
			invalid(pr.Status, pr)
		default:
			problem(w, r, err)
		}
		return
	}
	d.advance(w, r, dt, steps, 1, res.ID, res.Warnings)
}

// newTarget reads {doctype} and checks the create right (CD-46).
func (d Deps) newTarget(w http.ResponseWriter, r *http.Request) (*doctype.DocType, []FormStep, bool) {
	dt, ok := doctype.Lookup(chi.URLParam(r, "doctype"))
	if !ok {
		problem(w, r, doctype.ErrNotFound)
		return nil, nil, false
	}
	if err := d.Auth.Can(r.Context(), d.principal(r), dt.Name, "create", nil); err != nil {
		problem(w, r, err)
		return nil, nil, false
	}
	steps, err := FormSteps(dt)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return nil, nil, false
	}
	return dt, steps, true
}

// stepTarget reads {doctype}, {id} and {step}.
func stepTarget(w http.ResponseWriter, r *http.Request) (*doctype.DocType, int64, []FormStep, int, bool) {
	dt, id, ok := target(w, r)
	if !ok {
		return nil, 0, nil, 0, false
	}
	steps, err := FormSteps(dt)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return nil, 0, nil, 0, false
	}
	i, ok := stepOf(steps, chi.URLParam(r, "step"))
	if !ok {
		problem(w, r, doctype.ErrNotFound)
		return nil, 0, nil, 0, false
	}
	return dt, id, steps, i, true
}

// stepGet serves GET /doc/{doctype}/{id}/edit/{step}: the whole page, or
// #wizard for htmx (design-system §8.3). A document past draft goes to its
// document page.
func (d Deps) stepGet(w http.ResponseWriter, r *http.Request) {
	dt, id, steps, i, ok := stepTarget(w, r)
	if !ok {
		return
	}
	v, err := d.View(r.Context(), d.principal(r), dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	if v.Info.State != doctype.StateDraft {
		http.Redirect(w, r, docURL(dt.Name, id), http.StatusSeeOther)
		return
	}
	sv := d.stepViewOf(r, dt, steps, i, &v, nil, nil)
	ui.Render(w, r, http.StatusOK, stepFull(sv), stepFrame(sv))
}

// stepPost serves POST /doc/{doctype}/{id}/edit/{step}. It saves the step's
// values with the posted version (CD-94) and the draft's lines as loaded,
// since DOC's save replaces the lines. nav=back saves without validating,
// nav=finish leaves for the document page, return=check goes back to the
// check page.
func (d Deps) stepPost(w http.ResponseWriter, r *http.Request) {
	dt, id, steps, i, ok := stepTarget(w, r)
	if !ok {
		return
	}
	version, ok := postedVersion(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	nav := wizard.Nav(r)
	typed, plain, errs := postedStep(r, dt, steps[i])
	rerender := func(pr Problem) {
		v, err := d.View(ctx, d.principal(r), dt.Name, id)
		if err != nil {
			problem(w, r, err)
			return
		}
		sv := d.stepViewOf(r, dt, steps, i, &v, typed, pr.Fields)
		o := wizard.Invalid
		if pr.Status == http.StatusConflict {
			o = wizard.Conflict
			sv.M.Conflict, sv.M.ReloadURL = pr.Message, sv.M.StepURL(i)
		} else if len(pr.Fields) == 0 && pr.Message != "" {
			sv.M.Errors = []components.FieldErr{{ID: "step-title", Message: pr.Message}}
		}
		wizard.Respond(w, r, o, "", func(fragment bool) templ.Component {
			if fragment {
				return stepFrame(sv)
			}
			return stepFull(sv)
		})
	}
	if len(errs) > 0 && nav != "back" {
		rerender(Problem{Status: http.StatusUnprocessableEntity, Fields: errs})
		return
	}
	rec, err := d.load(ctx, dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	res, err := d.Engine.Do(ctx, d.principal(r), dt.Name, id, doctype.ActionSave, doc.ActionInput{Version: version, Values: plain, Lines: rec.Lines})
	if err != nil {
		pr, ok := Classify(err)
		switch {
		case !ok:
			web.ErrorFrom(w, r, err)
			return
		case nav == "back" && pr.Status == http.StatusUnprocessableEntity:
			// Back never validates: DOC's field errors wait for later.
		case pr.Status == http.StatusUnprocessableEntity || pr.Status == http.StatusConflict:
			rerender(pr)
			return
		default:
			problem(w, r, err)
			return
		}
	}
	next := min(i+1, len(steps)-1)
	switch {
	case nav == "back":
		next = max(i-1, 0)
	case nav == "finish":
		for _, msg := range res.Warnings {
			web.Flash(ctx, "warning", msg)
		}
		wizard.Respond(w, r, wizard.Finish, docURL(dt.Name, id), nil)
		return
	case r.PostFormValue("return") == slugCheck:
		next = len(steps) - 1
	}
	d.advance(w, r, dt, steps, next, id, res.Warnings)
}

// advance moves to step i of draft id after a save: htmx gets that step's
// #wizard with the save's warnings and HX-Push-Url; otherwise the warnings
// are flashed and a 303 follows (CD-68, CD-80).
func (d Deps) advance(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, steps []FormStep, i int, id int64, warnings []string) {
	next := docURL(dt.Name, id) + "/edit/" + steps[i].Slug
	if !ui.IsHTMX(r) {
		for _, msg := range warnings {
			web.Flash(r.Context(), "warning", msg)
		}
		wizard.Respond(w, r, wizard.Advance, next, nil)
		return
	}
	v, err := d.View(r.Context(), d.principal(r), dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	sv := d.stepViewOf(r, dt, steps, i, &v, nil, nil)
	sv.M.Warnings = warnings
	wizard.Respond(w, r, wizard.Advance, next, func(bool) templ.Component { return stepFrame(sv) })
}

// failingStep answers a submit DOC refused with field errors: 422 with the
// first failing step and its errors, and HX-Push-Url set to it
// (design-system §8.3).
func (d Deps) failingStep(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64, pr Problem) {
	steps, err := FormSteps(dt)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	v, err := d.View(r.Context(), d.principal(r), dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	i := firstFailingStep(steps, pr.Fields)
	sv := d.stepViewOf(r, dt, steps, i, &v, nil, pr.Fields)
	w.Header().Set("HX-Push-Url", sv.M.StepURL(i))
	ui.Render(w, r, pr.Status, stepFull(sv), stepFrame(sv))
}

// checkPage renders draft id's check page as #main for an htmx action that
// leads there, with the action's banner and warnings, and HX-Push-Url.
func (d Deps) checkPage(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, id int64, banner string, warnings []string) {
	steps, err := FormSteps(dt)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	v, err := d.View(r.Context(), d.principal(r), dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	i := len(steps) - 1
	sv := d.stepViewOf(r, dt, steps, i, &v, nil, nil)
	sv.Banner, sv.M.Warnings = banner, warnings
	w.Header().Set("HX-Push-Url", sv.M.StepURL(i))
	ui.Render(w, r, http.StatusOK, stepFull(sv), stepMain(sv))
}

// stepView is one wizard step as rendered.
type stepView struct {
	M      wizard.Model
	Bar    wizard.Bar
	Inputs []templ.Component // the step's editable fields
	Fixed  []components.Pair // its ReadOnly fields, shown as text (CD-79)
	Empty  bool              // "Nothing needed here"
	Wide   bool              // the items and check steps use the content width
	Banner string            // an htmx action's success banner
}

// stepViewOf builds step i of dt's wizard. v is nil for a new document;
// typed, when not nil, holds the posted values of the step's fields; errs
// are shown in the summary and by their fields.
func (d Deps) stepViewOf(r *http.Request, dt *doctype.DocType, steps []FormStep, i int, v *DocView, typed map[string]string, errs []doctype.FieldError) stepView {
	s := steps[i]
	m := wizard.Model{Mode: wizard.Document, Title: dt.Label, Current: i, Errors: fieldErrs(errs)}
	for _, st := range steps {
		m.Steps = append(m.Steps, wizard.Step{Slug: st.Slug, Label: st.Label})
	}
	if v == nil {
		m.BaseURL = "/doc/" + dt.Name + "/new"
		m.Action = m.BaseURL
	} else {
		m.DocLabel, m.Version = v.Info.Display, v.Version
		m.BaseURL = docURL(dt.Name, v.Info.ID) + "/edit"
		m.Furthest = 1
		if len(v.Lines) > 0 || len(v.Inputs) > 0 {
			m.Furthest = len(steps) - 1
		}
		m.Furthest = max(m.Furthest, i)
	}
	if r.URL.Query().Get("return") == slugCheck || r.PostFormValue("return") == slugCheck {
		m.ReturnTo = slugCheck
	}
	sv := stepView{M: m, Bar: wizard.Bar{Back: i > 0}}
	for _, name := range s.Fields {
		f, ok := dt.Field(name)
		if !ok {
			continue
		}
		if !editable(f) {
			if v != nil && v.Display[name] != "" {
				sv.Fixed = append(sv.Fixed, components.Pair{Label: f.Label, Value: v.Display[name], Num: numeric(f)})
			}
			continue
		}
		value := ""
		if typed != nil {
			value = typed[name]
		} else if v != nil {
			value = v.Inputs[name]
		}
		sv.Inputs = append(sv.Inputs, FieldInput(f, components.FieldID(name), value, headerError(errs, name), d))
	}
	switch {
	case i == len(steps)-1:
		sv.Wide = true
		if v != nil {
			d.checkStep(r, dt, steps, *v, &sv)
		}
	case s.Lines:
		sv.Wide = true
		if v != nil {
			sv.M.After = d.itemsBody(r, dt, *v)
		}
	default:
		sv.Empty = len(sv.Inputs) == 0 && len(sv.Fixed) == 0
	}
	return sv
}

// headerError is the message of the header field's first error.
func headerError(errs []doctype.FieldError, name string) string {
	for _, e := range errs {
		if e.Line == 0 && e.Field == name {
			return e.Message
		}
	}
	return ""
}

// checkView is the check page's part after the step form.
type checkView struct {
	Preview           string
	Sections          []checkSection
	GrandTotal, Words string
	More              []components.MoreAction
}

// checkSection is one step's summary card.
type checkSection struct {
	Title, ChangeURL string
	Status           wizard.StepStatus
	Pairs            []components.Pair
	Lines            *components.LineListProps
}

// checkStep fills the check page (design-system §8.3): a section per step
// marked Complete or Needs attention from v.Problems, the totals, the
// approval preview, and the bar's state action posting to /submit with
// the version.
func (d Deps) checkStep(r *http.Request, dt *doctype.DocType, steps []FormStep, v DocView, sv *stepView) {
	rights := Rights(r.Context(), d.Auth, d.principal(r), dt.Name, &v.Record)
	bar, more := Actions(v, dt.Printable, rights)
	sv.M.Action = docURL(dt.Name, v.Info.ID) + "/" + string(doctype.ActionSubmit)
	sv.Bar = wizard.Bar{NoPrimary: true}
	if bar.Primary != nil {
		sv.Bar = wizard.Bar{Primary: bar.Primary.Label}
	}
	cv := checkView{Preview: v.Approval.Preview, GrandTotal: v.GrandTotal, Words: v.InWords, More: more}
	for i, s := range steps[:len(steps)-1] {
		sec := checkSection{Title: s.Label, ChangeURL: sv.M.StepURL(i) + "?return=check", Status: wizard.Complete}
		for _, p := range v.Problems {
			if p.Line > 0 && s.Lines || p.Line == 0 && slices.Contains(s.Fields, p.Field) {
				sec.Status = wizard.Attention
			}
		}
		if s.Lines {
			sec.Lines = &components.LineListProps{Cards: lineCards(v.Lines), Total: len(v.Lines)}
		}
		for _, name := range s.Fields {
			if f, ok := dt.Field(name); ok && v.Display[name] != "" {
				sec.Pairs = append(sec.Pairs, components.Pair{Label: f.Label, Value: v.Display[name], Num: numeric(f)})
			}
		}
		cv.Sections = append(cv.Sections, sec)
	}
	sv.M.After = checkBody(cv)
}

// load reads draft id's record in a read transaction.
func (d Deps) load(ctx context.Context, name string, id int64) (doc.Record, error) {
	var rec doc.Record
	err := d.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		rec, err = d.Engine.Load(ctx, tx, name, id)
		return err
	})
	return rec, err
}

func stepOpts(sv stepView) layout.PageOpts {
	o := layout.PageOpts{
		Title:      sv.M.Heading(),
		NoHeading:  true, // the frame's h1#step-title is the heading
		Width:      layout.Form,
		BannerSlot: stepBanner(sv.Banner),
		Error:      len(sv.M.Errors) > 0 || sv.M.Conflict != "",
	}
	if sv.M.DocLabel != "" {
		o.Title += " · " + sv.M.DocLabel
	}
	if sv.Wide {
		o.Width = layout.Content
	}
	return o
}
