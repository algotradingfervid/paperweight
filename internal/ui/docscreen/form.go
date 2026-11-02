package docscreen

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
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
// line pages when Lines is set (design-system §8.3), and its Intro, the line
// under the stepper (UI v2 §5.6).
type FormStep struct {
	Slug, Label string
	Fields      []string
	Lines       bool
	Intro       string
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

// owns reports whether step s owns error e: a line error, or DOC's error
// on the lines as a whole (Field "lines", "Add at least one line."),
// belongs to the Lines step; a header error to the step listing its field.
func (s FormStep) owns(e doctype.FieldError) bool {
	if e.Line > 0 || e.Field == "lines" {
		return s.Lines
	}
	return slices.Contains(s.Fields, e.Field)
}

// only is what a save of step s is scoped to (CD-205): its fields, or the
// lines. It is never nil, since a step naming no field is still a scoped save.
func (s FormStep) only() []string {
	if s.Lines {
		return []string{"lines"}
	}
	return append([]string{}, s.Fields...)
}

// stepErrors keeps the errors step i shows: its own, and those with no step
// to show them on (no field name, or a name no step owns), so its summary
// links only to what is on the page. An error on a field the module hides
// keeps only its message, with no field (CD-256 (1)).
func stepErrors(c Ctx, steps []FormStep, i int, errs []doctype.FieldError) []doctype.FieldError {
	var out []doctype.FieldError
	for _, e := range errs {
		if e.Field == "" || steps[i].owns(e) || !slices.ContainsFunc(steps, func(s FormStep) bool { return s.owns(e) }) {
			out = append(out, unhidden(c, e))
		}
	}
	return out
}

// refusalErrors is stepErrors for a refusal, which must always say why: when
// no error is left, each dropped error's own message, with no field.
func refusalErrors(c Ctx, steps []FormStep, i int, errs []doctype.FieldError) []doctype.FieldError {
	out := stepErrors(c, steps, i, errs)
	if len(out) > 0 {
		return out
	}
	return messagesOnly(errs)
}

// messagesOnly is each error's own message, with no field.
func messagesOnly(errs []doctype.FieldError) []doctype.FieldError {
	var out []doctype.FieldError
	for _, e := range errs {
		out = append(out, doctype.FieldError{Message: e.Message})
	}
	return out
}

// firstFailingStep is the first step owning an error field. With no owner
// it is the check step.
func firstFailingStep(steps []FormStep, errs []doctype.FieldError) int {
	best := len(steps) - 1
	for _, e := range errs {
		i := slices.IndexFunc(steps, func(s FormStep) bool { return s.owns(e) })
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
// with NUM's text and leaves the field out of plain (CD-48). A field the
// module hides is left out, so DOC keeps its stored value (hooks spec §4.3).
func postedStep(c Ctx, s FormStep) (typed, plain map[string]string, errs []doctype.FieldError) {
	typed, plain = typedStep(c, s), map[string]string{}
	for _, name := range s.Fields {
		f, _ := c.DT.Field(name)
		if _, ok := typed[name]; !ok {
			continue
		}
		v, fe := ToPlain(f, typed[name])
		if fe != nil {
			errs = append(errs, *fe)
			continue
		}
		plain[name] = v
	}
	return typed, plain, errs
}

// typedStep is the text the POST holds for step s's editable fields, but
// those the module hides; nothing is parsed, so a refusal made before the
// parse still redraws what the user typed (CD-335).
func typedStep(c Ctx, s FormStep) map[string]string {
	typed := map[string]string{}
	for _, name := range s.Fields {
		f, ok := c.DT.Field(name)
		if !ok || !editable(f) || hidden(c, f) {
			continue
		}
		typed[name] = c.Req.PostFormValue(name)
	}
	return typed
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

// moved is DOC's refusal of a write on rec from a page posting version,
// when rec is past draft or its version isn't the posted one; nil
// otherwise. Callers check the user's right first and call it before any
// hook or parse, so a stale page gets DOC's 409, never a 422 (CD-335, CD-259).
// The refusal is the engine's own: it is asked to save rec's stored lines
// and nothing else at a version the record can't hold, so it refuses before
// writing: past draft with its state text, else with its stale text. That
// is the posted version only when it is behind the stored one (versions only
// grow, CD-94); any other is never sent, since a concurrent save could reach
// it and the stale lines would be saved: it is asked at version 0, which no
// record holds (versions start at 1), with the same stale text.
func (d Deps) moved(r *http.Request, dt *doctype.DocType, rec doc.Record, version int) error {
	if rec.Version == version && rec.Info.State == doctype.StateDraft {
		return nil
	}
	behind := 0
	if version < rec.Version {
		behind = version
	}
	_, err := d.Engine.Do(r.Context(), d.principal(r), dt.Name, rec.Info.ID, doctype.ActionSave, doc.ActionInput{Version: behind, Lines: rec.Lines, Only: []string{}})
	return err
}

// newGet serves GET /doc/{doctype}/new: step 1 with no draft yet.
func (d Deps) newGet(w http.ResponseWriter, r *http.Request) {
	dt, steps, ok := d.newTarget(w, r)
	if !ok {
		return
	}
	defaults, err := d.defaults(r, dt)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	sv, err := d.stepViewOf(r, dt, steps, 0, nil, defaults, nil)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	ui.Render(w, r, http.StatusOK, stepFull(sv), stepFrame(sv))
}

// defaults holds each header ProjectFields and SiteFields field the user may
// fill with exactly one value, prefilled with it (AUTH-100, CD-245).
func (d Deps) defaults(r *http.Request, dt *doctype.DocType) (map[string]string, error) {
	out := map[string]string{}
	for dim, cols := range map[auth.Dimension][]string{auth.DimProject: dt.ProjectFields, auth.DimSite: dt.SiteFields} {
		if len(cols) == 0 {
			continue
		}
		id, ok, err := d.Auth.DefaultValue(r.Context(), d.principal(r), dim)
		if err != nil {
			return nil, err
		}
		for _, f := range dt.Fields {
			if ok && !f.OnLine && slices.Contains(cols, f.Column) {
				out[f.Name] = strconv.FormatInt(id, 10)
			}
		}
	}
	return out, nil
}

// newPost serves POST /doc/{doctype}/new: step 1 creates the draft.
func (d Deps) newPost(w http.ResponseWriter, r *http.Request) {
	dt, steps, ok := d.newTarget(w, r)
	if !ok {
		return
	}
	hook, err := d.stepHook(dt, steps, 0)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	c := d.ctxOf(r, dt, nil)
	typed, plain, errs := postedStep(c, steps[0])
	invalid := func(status int, pr Problem) {
		pr.Fields = refusalErrors(c, steps, 0, pr.Fields)
		sv, err := d.stepViewOf(r, dt, steps, 0, nil, typed, pr.Fields)
		if err != nil {
			web.ErrorFrom(w, r, err)
			return
		}
		if status == http.StatusConflict {
			sv.M.Conflict, sv.M.ReloadURL = pr.Message, "/doc/"+dt.Name+"/new"
		}
		ui.Render(w, r, status, stepFull(sv), stepFrame(sv))
	}
	var saved Save
	if hook.Save != nil {
		// The module's first step creates the draft with its lines in one
		// call (hooks spec §4.1).
		if saved, err = hook.Save(c); err != nil {
			// A 403 on a field (a posted record the user can't read, CD-240)
			// redraws the step as a 422, which htmx swaps (CD-323 (1)).
			if pr, ok := Classify(err); ok && pr.Status == http.StatusForbidden && len(pr.Fields) > 0 {
				invalid(http.StatusUnprocessableEntity, pr)
				return
			}
			problem(w, r, err)
			return
		}
		plain, errs = saved.Values, saved.Errors
	}
	if len(errs) > 0 {
		invalid(http.StatusUnprocessableEntity, Problem{Fields: errs})
		return
	}
	res, err := d.Engine.Do(r.Context(), d.principal(r), dt.Name, 0, doctype.ActionSave, doc.ActionInput{Values: plain, Lines: saved.Lines})
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
	if wizard.Nav(r) == "stay" {
		d.stay(w, r, dt, steps, 0, res.ID, res.Warnings)
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
// #wizard for htmx (design-system §8.3), with the draft's stored problems on
// this step (CD-205). A document past draft, or a draft the user may only
// read (CD-263 G1), goes to its document page.
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
	if d.readOnly(w, r, dt, v) {
		return
	}
	sv, err := d.arrival(r, dt, steps, i, &v)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	ui.Render(w, r, http.StatusOK, stepFull(sv), stepFrame(sv))
}

// stepPost serves POST /doc/{doctype}/{id}/edit/{step}. It saves the step's
// values with the posted version (CD-94) and the draft's lines as loaded,
// since DOC's save replaces the lines, scoped to the step, so another step's
// problem stops nothing here (CD-205). nav=back stays on the step when a
// value can't be parsed or DOC refuses the save (CD-178),
// nav=finish leaves for the document page, nav=stay redraws the same step
// at its bare URL, return=check goes back to the check page.
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
	// Hide sees the draft as loaded, as on GET (CD-256 (2)).
	loaded, err := d.View(ctx, d.principal(r), dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	// A reader gets AUTH's refusal, as DOC gives it, before any parsing (CD-263 G1).
	if err := d.writeErr(r, dt, loaded); err != nil {
		problem(w, r, err)
		return
	}
	hook, err := d.stepHook(dt, steps, i)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	rec, err := d.load(ctx, dt.Name, id)
	if err != nil {
		problem(w, r, err)
		return
	}
	c := d.ctxOf(r, dt, &loaded)
	var typed, plain map[string]string
	var errs []doctype.FieldError
	rerender := func(pr Problem) {
		v, err := d.View(ctx, d.principal(r), dt.Name, id)
		if err != nil {
			problem(w, r, err)
			return
		}
		pr.Fields = refusalErrors(d.ctxOf(r, dt, &v), steps, i, pr.Fields)
		sv, err := d.stepViewOf(r, dt, steps, i, &v, typed, pr.Fields)
		if err != nil {
			web.ErrorFrom(w, r, err)
			return
		}
		sv.M.Version = version // the page's, so a save made since still conflicts (CD-94)
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
	// A stale page or a document past draft gets DOC's refusal before the
	// module's save or any parse (CD-335).
	if err := d.moved(r, dt, rec, version); err != nil {
		typed = typedStep(c, steps[i])
		stepRefused(w, r, err, rerender)
		return
	}
	typed, plain, errs = postedStep(c, steps[i])
	var saved Save
	if hook.Save != nil {
		// The module's save replaces the posted fields (hooks spec §4.1).
		if saved, err = hook.Save(c); err != nil {
			// A 403 on a field (a posted record the user can't read, CD-240)
			// redraws the step as a 422, which htmx swaps (CD-323 (1)).
			if pr, ok := Classify(err); ok && pr.Status == http.StatusForbidden && len(pr.Fields) > 0 {
				rerender(pr) // wizard.Invalid: 422
				return
			}
			problem(w, r, err)
			return
		}
		plain, errs = saved.Values, saved.Errors
	}
	if len(errs) > 0 {
		rerender(Problem{Status: http.StatusUnprocessableEntity, Fields: errs})
		return
	}
	// On a stale page an unknown line id may be one removed since: the
	// stored lines go with the posted version, and the engine answers the
	// conflict (CD-94).
	lines, err := mergeLines(rec.Lines, saved.Lines)
	if err != nil {
		if version == rec.Version {
			web.ErrorFrom(w, r, err)
			return
		}
		lines = rec.Lines
	}
	only := steps[i].only()
	if saved.Lines != nil && !slices.Contains(only, "lines") {
		only = append(only, "lines")
	}
	res, err := d.Engine.Do(ctx, d.principal(r), dt.Name, id, doctype.ActionSave, doc.ActionInput{Version: version, Values: plain, Lines: lines, Only: only})
	if err != nil {
		stepRefused(w, r, err, rerender)
		return
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
	case nav == "stay":
		d.stay(w, r, dt, steps, i, id, res.Warnings)
		return
	case r.PostFormValue("return") == slugCheck:
		next = len(steps) - 1
	}
	d.advance(w, r, dt, steps, next, id, res.Warnings)
}

// stepRefused answers a step save the engine refused: a 422 or 409 redraws
// the step, any other refusal is its own page.
func stepRefused(w http.ResponseWriter, r *http.Request, err error, rerender func(Problem)) {
	pr, ok := Classify(err)
	switch {
	case !ok:
		web.ErrorFrom(w, r, err)
	case pr.Status == http.StatusUnprocessableEntity || pr.Status == http.StatusConflict:
		rerender(pr)
	default:
		problem(w, r, err)
	}
}

// stay is advance to step i of draft id after a nav=stay save, drawn as a
// GET of the step's bare URL shows it: no query and no posted form, so
// return=check isn't followed (CD-263 G6).
func (d Deps) stay(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, steps []FormStep, i int, id int64, warnings []string) {
	g := r.Clone(r.Context())
	g.Method, g.URL, g.Body = http.MethodGet, &url.URL{Path: docURL(dt.Name, id) + "/edit/" + steps[i].Slug}, http.NoBody
	g.Form, g.PostForm, g.MultipartForm = url.Values{}, url.Values{}, nil
	d.advance(w, g, dt, steps, i, id, warnings)
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
	sv, err := d.arrival(r, dt, steps, i, &v)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	sv.M.Warnings = warnings
	wizard.Respond(w, r, wizard.Advance, next, func(bool) templ.Component { return stepFrame(sv) })
}

// failingStep answers a submit DOC refused with field errors: 422 with the
// first failing step and only that step's errors, and HX-Push-Url set to it
// (design-system §8.3). htmx gets #main, the check page form's target. A
// user without write gets the document page with the refusal instead.
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
	if !d.canWrite(r, dt, v) {
		d.refused(w, r, dt, id, pr) // a reader stays on the read-only page (CD-263 G1)
		return
	}
	i := firstFailingStep(steps, pr.Fields)
	sv, err := d.stepViewOf(r, dt, steps, i, &v, nil, stepErrors(d.ctxOf(r, dt, &v), steps, i, pr.Fields))
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	w.Header().Set("HX-Push-Url", sv.M.StepURL(i))
	ui.Render(w, r, pr.Status, stepFull(sv), stepMain(sv))
}

// checkPage renders draft id's check page as #main for an htmx action that
// leads there, with the action's banner and warnings, and HX-Push-Url; a
// user without write gets the read-only document page.
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
	if !d.canWrite(r, dt, v) {
		d.readOnlyPage(w, r, dt, v, banner, warnings)
		return
	}
	i := len(steps) - 1
	sv, err := d.stepViewOf(r, dt, steps, i, &v, nil, nil)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
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

// arrival is step i as a user reaches it who did nothing wrong: draft v's
// stored problems on this step show by their fields and in M.Stored, and
// M.Errors stays empty, so the title is plain and the heading takes focus.
func (d Deps) arrival(r *http.Request, dt *doctype.DocType, steps []FormStep, i int, v *DocView) (stepView, error) {
	errs := stepErrors(d.ctxOf(r, dt, v), steps, i, v.DraftProblems)
	sv, err := d.stepViewOf(r, dt, steps, i, v, nil, errs)
	if err != nil {
		return stepView{}, err
	}
	sv.M.Errors = nil
	for _, e := range errs {
		sv.M.Stored = append(sv.M.Stored, e.Message)
	}
	return sv, nil
}

// stepViewOf builds step i of dt's wizard. v is nil for a new document;
// typed, when not nil, holds the posted values of the step's fields (on a GET
// of /new, the prefilled ones); errs are shown in the summary and by their
// fields. A module's Body replaces the step's inputs, and on the Items step
// M.After too; an error from it, or a bad Steps key, is returned.
func (d Deps) stepViewOf(r *http.Request, dt *doctype.DocType, steps []FormStep, i int, v *DocView, typed map[string]string, errs []doctype.FieldError) (stepView, error) {
	hook, err := d.stepHook(dt, steps, i)
	if err != nil {
		return stepView{}, err
	}
	steps = d.labelled(dt, steps)
	s := steps[i]
	m := wizard.Model{Mode: wizard.Document, Title: dt.Label, Current: i, Errors: fieldErrs(errs)}
	for _, st := range steps {
		m.Steps = append(m.Steps, wizard.Step{Slug: st.Slug, Label: st.Label, Intro: st.Intro})
	}
	if v == nil {
		m.BaseURL = "/doc/" + dt.Name + "/new"
		m.Action = m.BaseURL
	} else {
		m.DocLabel, m.Version = v.Info.Display, v.Version
		m.BaseURL = docURL(dt.Name, v.Info.ID) + "/edit"
		m.Close = docURL(dt.Name, v.Info.ID)
		m.Furthest = 1
		if len(v.Lines) > 0 || len(v.Inputs) > 0 {
			m.Furthest = len(steps) - 1
		}
		m.Furthest = max(m.Furthest, i)
	}
	// The × goes back to the document page from a draft's step, and to the
	// list from a new document and from a draft's check step, which is that
	// page (UI v2 §5.6).
	if v == nil || i == len(steps)-1 {
		m.Close = listURL(dt.Name, ListQuery{})
	}
	if r.URL.Query().Get("return") == slugCheck || r.PostFormValue("return") == slugCheck {
		m.ReturnTo = slugCheck
	}
	sv := stepView{M: m, Bar: wizard.Bar{Back: i > 0}}
	if v != nil {
		shown := *v
		v = &shown
	}
	c := d.ctxOf(r, dt, v) // c.View is what this step renders
	if v != nil {
		applyLineCards(c, v)
	}
	// body is the Body's component; with none the step is generic, under
	// the module's Top (CD-263 G6, CD-280 D5).
	var body templ.Component
	var bodyHTML bytes.Buffer
	if hook.Body != nil || hook.Bar != nil {
		// typed is a redraw's posted values only on a POST; a GET of /new
		// passes the prefill, which Defaults holds on every /new render.
		st := StepState{Posted: typed != nil && r.Method == http.MethodPost, Errors: errs}
		switch {
		case v == nil && !st.Posted:
			st.Defaults = typed
		case v == nil:
			if st.Defaults, err = d.defaults(r, dt); err != nil {
				return stepView{}, err
			}
		}
		if hook.Body != nil {
			if body, err = hook.Body(c, st); err != nil {
				return stepView{}, err
			}
		}
		if body != nil {
			// Rendered once, so line errors link only to ids it has (CD-286 R1).
			if err := body.Render(r.Context(), &bodyHTML); err != nil {
				return stepView{}, err
			}
			body = templ.Raw(bodyHTML.String())
			sv.Bar.EnterPrimary = true // Enter presses the primary, never a Body button (CD-286 R4)
		}
		if hook.Bar != nil {
			b, err := hook.Bar(c, st)
			if err != nil {
				return stepView{}, err
			}
			sv.Bar = stepBar(sv.Bar, b)
		}
	}
	switch {
	case body != nil:
		sv.Inputs = []templ.Component{body}
	case hook.Top != nil:
		top, err := hook.Top(c)
		if err != nil {
			return stepView{}, err
		}
		sv.Inputs = []templ.Component{top}
	}
	for n, e := range errs {
		switch {
		case e.Field == "":
			sv.M.Errors[n].ID = "step-title"
		case body != nil && e.Line > 0 && strings.Contains(bodyHTML.String(), ` id="`+LineFieldID(e.Line, e.Field)+`"`):
			sv.M.Errors[n].ID = LineFieldID(e.Line, e.Field)
		case v != nil && dt.LinesTable != "" && (e.Line > 0 || e.Field == "lines"):
			sv.M.Errors[n].ID = linesTarget(*v, d.gridShown(r, dt, *v) && body == nil) // a Body's component replaces the line editor
		}
	}
	var shown []MoreInput
	for _, name := range s.Fields {
		f, ok := dt.Field(name)
		if !ok || hidden(c, f) {
			continue
		}
		if !editable(f) {
			if v != nil && v.Display[name] != "" {
				sv.Fixed = append(sv.Fixed, components.Pair{Label: f.Label, Value: v.Display[name], Num: numeric(f)})
			}
			continue
		}
		if body != nil {
			continue // the Body has the step's inputs
		}
		value := ""
		if typed != nil {
			value = typed[name]
		} else if v != nil {
			value = v.Inputs[name]
		}
		msg := headerError(errs, name)
		shown = append(shown, MoreInput{Field: f, Value: value, Error: msg, Input: FieldInput(f, components.FieldID(name), value, msg, c.Deps)})
	}
	sv.Inputs = append(sv.Inputs, GroupMore(shown).Inputs()...)
	switch {
	case i == len(steps)-1:
		sv.Wide = !d.modOf(dt).CardsOnly // cards sit in the form column (CD-326 (1))
		if v != nil {
			if err := d.checkStep(c, steps, *v, &sv); err != nil {
				return stepView{}, err
			}
		}
	case s.Lines:
		sv.Wide = !d.modOf(dt).CardsOnly
		if v != nil && body == nil {
			sv.M.After = d.itemsBody(c, *v)
		}
		if v != nil {
			if t := footerTotal(c, *v); t != nil {
				sv.Bar.Summary = components.BarTotalLine(*t, false) // A-22
			}
		}
	default:
		sv.Empty = len(sv.Inputs) == 0 && len(sv.Fixed) == 0
	}
	return sv, nil
}

// stepBar is bar with a module's StepBar applied; each zero field keeps
// bar's (CD-263 G6).
func stepBar(bar wizard.Bar, b StepBar) wizard.Bar {
	if b.Primary != "" {
		bar.Primary = b.Primary
	}
	if b.Stay {
		bar.PrimaryNav = "stay"
	}
	bar.BackLink = b.Back
	return bar
}

// linesTarget is the id a line's or whole-lines error links to, on the Items
// step: the grid editor when it is shown and the draft has lines to show in
// it, else the step heading.
func linesTarget(v DocView, grid bool) string {
	if grid && len(v.Lines) > 0 {
		return "line-editor"
	}
	return "step-title"
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
	AmendedFrom       string // the original's number on an amended draft
	AmendedURL        string
	Sections          []checkSection
	GrandTotal, Words string
	More              []components.MoreAction
	Attach            *attachView
	Extra             CheckExtra // the module's additions (hooks spec §4.4)
}

// checkSection is one step's summary card.
type checkSection struct {
	Title, Icon, ChangeURL string
	Status                 wizard.StepStatus
	Problems               []string // what submit will refuse on this step (CD-178)
	Pairs                  []components.Pair
	Lines                  *components.LineListProps
}

// checkStep fills the check page (design-system §8.3): a section per step
// marked Complete or Needs attention from v.Problems and v.DraftProblems,
// and, with a lines table, "Add at least one line." for a draft with none
// (CD-178), in the module's LineNoun (CD-326 (4)); each section lists its
// header and whole-lines problems and DOC's line problems. Then the totals,
// the approval preview, and the bar's state action posting to /submit with
// the version. A field the module hides is neither summarised nor a problem
// (CD-256 (1)), nor, as the figure field, the grand total (CD-319 (8)); one
// the module's totals cover (CheckExtra.Covers) isn't summarised either
// (CD-326 (4)). The module's Check additions come last; its error is
// returned (hooks spec §4.4). A section's icon tile is file-text, or
// list-checks for the lines step (UI v2 §5.6).
func (d Deps) checkStep(c Ctx, steps []FormStep, v DocView, sv *stepView) error {
	r, dt := c.Req, c.DT
	rights := Rights(r.Context(), d.Auth, d.principal(r), dt.Name, &v.Record)
	bar, more := Actions(v, dt.Printable, rights)
	// A submit leaves the wizard: its answer (the document page or the
	// failing step) is #main.
	sv.M.Action, sv.M.Target = docURL(dt.Name, v.Info.ID)+"/"+string(doctype.ActionSubmit), "#main"
	sv.Bar = wizard.Bar{NoPrimary: true}
	if bar.Primary != nil {
		sv.Bar = wizard.Bar{Primary: bar.Primary.Label}
	}
	hideSummary(c, &v)
	cv := checkView{Preview: v.Approval.Preview, GrandTotal: v.GrandTotal, Words: v.InWords, More: more, Attach: attachOf(dt, v, rights)}
	if v.Origin.AmendedFrom != "" {
		cv.AmendedFrom, cv.AmendedURL = v.Origin.AmendedFrom, docURL(dt.Name, v.Origin.AmendedFromID)
	}
	extra, err := checkExtra(c)
	if err != nil {
		return err
	}
	cv.Extra = extra
	problems := v.Problems
	if dt.LinesTable != "" && len(v.Lines) == 0 {
		problems = append(slices.Clip(problems), doctype.FieldError{Field: "lines", Message: noLinesMessage(d.modOf(dt).LineNoun)})
	}
	problems = slices.Concat(problems, v.DraftProblems)
	for i, s := range steps[:len(steps)-1] {
		sec := checkSection{Title: s.Label, Icon: "file-text", ChangeURL: sv.M.StepURL(i) + "?return=check", Status: wizard.Complete}
		for _, p := range problems {
			if f, ok := dt.Field(p.Field); ok && hidden(c, f) {
				continue
			}
			if s.owns(p) {
				sec.Status = wizard.Attention
				if p.Line == 0 || slices.Contains(v.DraftProblems, p) {
					sec.Problems = append(sec.Problems, p.Message)
				}
			}
		}
		if s.Lines {
			sec.Icon, sec.Lines = "list-checks", d.lineList(r, dt, v)
		}
		for _, name := range s.Fields {
			if f, ok := dt.Field(name); ok && v.Display[name] != "" && !hidden(c, f) && !slices.Contains(extra.Covers, name) {
				sec.Pairs = append(sec.Pairs, components.Pair{Label: f.Label, Value: v.Display[name], Num: numeric(f)})
			}
		}
		cv.Sections = append(cv.Sections, sec)
	}
	sv.M.After = checkBody(cv)
	return nil
}

// noLinesMessage asks for a first line in the module's word for one
// (LineNoun.Title, lower case), or "line" (CD-326 (4)).
func noLinesMessage(n LineNoun) string {
	if n.Title == "" {
		return "Add at least one line."
	}
	return "Add at least one " + strings.ToLower(n.Title) + "."
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
		NoHeading:  true, // the frame's h2#step-title is the heading
		Focus:      true,
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
