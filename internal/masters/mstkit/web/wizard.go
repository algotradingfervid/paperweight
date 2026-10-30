package mstweb

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/a-h/templ"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

const (
	// checkSlug is the wizard's last step.
	checkSlug = "check"
	// furthestKey holds the furthest step reached among the session's values;
	// no field has this name.
	furthestKey = "_furthest"
	// startOverSlug is the route that drops a draft; it is no step.
	startOverSlug = "start-over"

	// defaultMaxLen is the least a step value may hold. It only keeps the
	// session small: a Field's own MaxLen is the service's rule, so Validate
	// refuses that with the owner's text (CD-142), and a higher one raises this.
	defaultMaxLen = 1000
	// maxDraftBytes caps one draft as the session stores it, marshalled. Sessions
	// load on every request (CD-201). 128 KiB holds a Party with 50 addresses and
	// 50 contacts at 10 and 6 fields of about 100 bytes each, keys included:
	// about 100 KiB.
	maxDraftBytes = 128 << 10

	msgDraftTooBig = "This draft is too large to keep. Shorten what you typed, or start over."
)

// Steps is a DocType's form as steps, by Field.Step in order of first
// appearance (docscreen.FormSteps): a Screen's Sections, and a CreateWizard's
// Steps. Disabled is set by Disable and Enable, so no step has it.
func Steps(docType string) []docscreen.FormStep {
	dt, _ := doctype.Lookup(docType) // registered by the master package's init
	steps, err := docscreen.FormSteps(dt)
	if err != nil {
		panic(err)
	}
	steps = steps[:len(steps)-1] // docscreen's check step; the wizard adds its own
	for i, s := range steps {
		steps[i].Fields = slices.DeleteFunc(s.Fields, func(name string) bool { return name == "disabled" })
	}
	return steps
}

// CreateWizard is a Master-mode create wizard (CD-50): step values wait in the
// session, and only Check and save writes, through Save. W is what Save takes,
// which need not be the Screen's In: one save can make several records.
type CreateWizard[W any] struct {
	Sessions *scs.SessionManager
	Steps    []docscreen.FormStep // without Check and save, which is added
	// ToInput is Screen.ToInput's contract for W, over all the steps' values:
	// a step's Next hears the errors on its own fields.
	ToInput func(values map[string]string) (W, []doctype.FieldError)
	Save    func(ctx context.Context, p auth.Principal, in W) (mstkit.Saved, error)
	// Validate is optional and is W's pure Validate: a step's Next hears its
	// errors on that step's fields, except the blank Required field, which
	// waits for Check and save.
	Validate func(W) (W, error)
}

// Wizard is a CreateWizard of any W.
type Wizard interface{ core() wizardCore }

// wizardCore is a CreateWizard with W out of the signatures.
type wizardCore struct {
	sessions *scs.SessionManager
	steps    []docscreen.FormStep
	// read parses the values into errs and Validate's rules; save is for when
	// there are no errs.
	read func(values map[string]string) (save saveFunc, errs, rules []doctype.FieldError)
}

// saveFunc is one read draft's Save.
type saveFunc func(ctx context.Context, p auth.Principal) (mstkit.Saved, error)

func (c CreateWizard[W]) core() wizardCore {
	wc := wizardCore{
		sessions: c.Sessions,
		steps:    append(slices.Clone(c.Steps), docscreen.FormStep{Slug: checkSlug, Label: "Check and save"}),
		read: func(values map[string]string) (saveFunc, []doctype.FieldError, []doctype.FieldError) {
			in, errs := c.ToInput(values)
			var rules []doctype.FieldError
			if c.Validate != nil {
				rules = validationErrors(c.Validate(in))
			}
			return func(ctx context.Context, p auth.Principal) (mstkit.Saved, error) { return c.Save(ctx, p, in) }, errs, rules
		},
	}
	if err := wc.model().Validate(); err != nil {
		panic("mstweb: " + err.Error())
	}
	return wc
}

// model is the wizard's shape.
func (c wizardCore) model() wizard.Model {
	m := wizard.Model{Mode: wizard.Master}
	for _, s := range c.steps {
		m.Steps = append(m.Steps, wizard.Step{Slug: s.Slug, Label: s.Label})
	}
	return m
}

// validationErrors is a Validate's field errors.
func validationErrors[T any](_ T, err error) []doctype.FieldError {
	var he doctype.HTTPError
	if errors.As(err, &he) {
		return he.FieldErrors()
	}
	return nil
}

// session is p's draft of this master. The key names the user too, so a shared
// browser never shows one user's draft to another.
func (h *handlers[R, In]) session(p auth.Principal) wizard.SessionValues {
	return wizard.SessionValues{SM: h.wiz.sessions, Key: "mst_wizard:" + h.DocType + ":" + strconv.FormatInt(int64(p.ID), 10)}
}

// draft is the session's values over Defaults, and the furthest step reached.
func (h *handlers[R, In]) draft(ctx context.Context, s wizard.SessionValues) (map[string]string, int) {
	held := s.Get(ctx)
	furthest, _ := strconv.Atoi(held[furthestKey]) // absent: 0
	delete(held, furthestKey)
	vals := make(map[string]string, len(h.Defaults)+len(held))
	maps.Copy(vals, h.Defaults)
	maps.Copy(vals, held)
	return vals, furthest
}

// wizardIndex is the step the URL names; a bare /new is the first.
func (h *handlers[R, In]) wizardIndex(r *http.Request) (int, error) {
	slug := chi.URLParam(r, "step")
	if slug == "" {
		return 0, nil
	}
	i := slices.IndexFunc(h.wiz.steps, func(s docscreen.FormStep) bool { return s.Slug == slug })
	if i < 0 {
		return 0, doctype.ErrNotFound
	}
	return i, nil
}

func (h *handlers[R, In]) wizardURL(i int) string {
	return h.Base + "/new/" + h.wiz.steps[i].Slug
}

// wizardGet serves a step. One past the furthest reached sends the browser to
// that one.
func (h *handlers[R, In]) wizardGet(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	ctx := r.Context()
	if err := h.checkCreate(ctx, p); err != nil {
		return err
	}
	i, err := h.wizardIndex(r)
	if err != nil {
		return err
	}
	vals, furthest := h.draft(ctx, h.session(p))
	if i > furthest {
		wizard.Respond(w, r, wizard.Finish, h.wizardURL(furthest), nil)
		return nil
	}
	v, err := h.wizardStep(r, p, i, furthest, vals, nil)
	if err != nil {
		return err
	}
	ui.Render(w, r, http.StatusOK, wizardPage(v), wizardFrame(v))
	return nil
}

// wizardStartOver drops p's own draft. It writes nothing else, so it asks
// nothing first.
func (h *handlers[R, In]) wizardStartOver(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	ctx := r.Context()
	if err := h.checkCreate(ctx, p); err != nil {
		return err
	}
	h.session(p).Clear(ctx)
	wizard.Respond(w, r, wizard.Finish, h.wizardURL(0), nil)
	return nil
}

// wizardPost is a step's Back, Next or, on the last, Check and save. What was
// typed goes into the session first, so nothing is lost, and before the
// response starts (CD-136). A finished wizard's session is empty, so a replay
// lands here on a step past the furthest and goes back to the start.
func (h *handlers[R, In]) wizardPost(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	ctx := r.Context()
	if err := h.checkCreate(ctx, p); err != nil {
		return err
	}
	i, err := h.wizardIndex(r)
	if err != nil {
		return err
	}
	session := h.session(p)
	vals, furthest := h.draft(ctx, session)
	if i > furthest {
		wizard.Respond(w, r, wizard.Finish, h.wizardURL(furthest), nil)
		return nil
	}
	last := len(h.wiz.steps) - 1
	nav := wizard.Nav(r)
	if i == last && nav != "back" {
		return h.wizardSave(w, r, p, session, vals)
	}
	step := h.wiz.steps[i]
	typed := h.postedStep(r, step)
	maps.Copy(vals, typed)
	if over := h.overLimit(step, typed, vals); len(over) > 0 { // never into the session
		return h.wizardStay(w, r, p, i, furthest, vals, over)
	}
	if nav == "back" {
		session.Merge(ctx, typed)
		return h.wizardAdvance(w, r, p, max(i-1, 0), furthest, vals)
	}
	_, errs, rules, err := h.wizardRead(ctx, vals)
	if err != nil {
		return err
	}
	if own := withRules(stepErrors(step, errs), stepErrors(step, answered(h.dt, vals, rules))); len(own) > 0 {
		session.Merge(ctx, typed)
		return h.wizardStay(w, r, p, i, furthest, vals, own)
	}
	furthest = max(furthest, i+1)
	typed[furthestKey] = strconv.Itoa(furthest)
	session.Merge(ctx, typed)
	next := i + 1
	if r.PostFormValue("return") == checkSlug && furthest == last {
		next = last
	}
	return h.wizardAdvance(w, r, p, next, furthest, vals)
}

// overLimit refuses what is too big to hold: a value over the larger of its
// Field's MaxLen and defaultMaxLen, or a draft over maxDraftBytes.
func (h *handlers[R, In]) overLimit(s docscreen.FormStep, typed, vals map[string]string) []doctype.FieldError {
	var errs []doctype.FieldError
	for _, name := range s.Fields {
		fd, _ := h.dt.Field(name) // handlers checked every step's fields
		limit := max(fd.MaxLen, defaultMaxLen)
		if utf8.RuneCountInString(typed[name]) > limit {
			errs = append(errs, doctype.FieldError{Field: name, Message: fd.Label + " can be at most " + types.FormatCount(int64(limit)) + " characters."})
		}
	}
	if len(errs) > 0 {
		return errs
	}
	stored, err := json.Marshal(vals) // as SessionValues stores it: a map[string]string always marshals
	if err == nil && len(stored) > maxDraftBytes {
		return []doctype.FieldError{{Message: msgDraftTooBig}}
	}
	return nil
}

// answered drops the errors on a blank Required field: they wait for Check and
// save.
func answered(dt *doctype.DocType, vals map[string]string, errs []doctype.FieldError) []doctype.FieldError {
	return slices.DeleteFunc(slices.Clone(errs), func(e doctype.FieldError) bool {
		fd, ok := dt.Field(e.Field)
		return ok && fd.Required && strings.TrimSpace(vals[e.Field]) == ""
	})
}

// withRules adds rules to errs, a parse error standing for its field: Validate
// saw that field empty.
func withRules(errs, rules []doctype.FieldError) []doctype.FieldError {
	for _, e := range rules {
		if !slices.ContainsFunc(errs, func(x doctype.FieldError) bool { return x.Field == e.Field && x.Line == e.Line }) {
			errs = append(errs, e)
		}
	}
	return errs
}

// postedStep is the step's fields as typed.
func (h *handlers[R, In]) postedStep(r *http.Request, s docscreen.FormStep) map[string]string {
	typed := make(map[string]string, len(s.Fields))
	for _, name := range s.Fields {
		typed[name] = r.PostFormValue(name)
	}
	return typed
}

// wizardRead reads the draft through Lookups, ToInput and Validate.
func (h *handlers[R, In]) wizardRead(ctx context.Context, vals map[string]string) (saveFunc, []doctype.FieldError, []doctype.FieldError, error) {
	resolved, errs, err := h.resolve(ctx, vals, h.wizardFields(), nil)
	if err != nil {
		return nil, nil, nil, err
	}
	save, parsed, rules := h.wiz.read(resolved)
	return save, append(errs, parsed...), rules, nil
}

func (h *handlers[R, In]) wizardFields() []string {
	var names []string
	for _, s := range h.wiz.steps {
		names = append(names, s.Fields...)
	}
	return names
}

// resolve is vals with the Lookups fields among names turned from what was
// typed into what is stored. keep is a record's stored values: a field still
// showing its stored value keeps it unasked, so a leaver's disabled user isn't
// refused for standing still.
func (h *handlers[R, In]) resolve(ctx context.Context, vals map[string]string, names []string, keep map[string]string) (map[string]string, []doctype.FieldError, error) {
	out := maps.Clone(vals)
	var errs []doctype.FieldError
	err := h.Env.DB.Read(ctx, func(tx *db.Tx) error {
		for _, name := range names {
			l, ok := h.Lookups[name]
			if !ok {
				continue
			}
			typed := strings.TrimSpace(out[name])
			out[name] = ""
			if typed == "" {
				continue
			}
			if old := keep[name]; old != "" {
				shown, err := l.Show(ctx, tx, old)
				if err != nil {
					return err
				}
				if strings.EqualFold(shown, typed) {
					out[name] = old
					continue
				}
			}
			stored, found, err := l.Find(ctx, tx, typed)
			if err != nil {
				return err
			}
			if !found {
				errs = append(errs, doctype.FieldError{Field: name, Message: l.Msg})
				continue
			}
			out[name] = stored
		}
		return nil
	})
	return out, errs, err
}

// wizardSave is Check and save: one Save from the session's values. A refusal
// the page can show goes to the first step that owns a field in it, 422, with
// the session as it was; success clears the session.
func (h *handlers[R, In]) wizardSave(w http.ResponseWriter, r *http.Request, p auth.Principal, session wizard.SessionValues, vals map[string]string) error {
	ctx := r.Context()
	save, errs, _, err := h.wizardRead(ctx, vals) // the service runs the rules
	if err != nil {
		return err
	}
	if len(errs) > 0 {
		return h.wizardRefused(w, r, p, vals, errs)
	}
	saved, err := save(ctx, p)
	if err != nil {
		var he doctype.HTTPError
		if !errors.As(err, &he) || he.Status() != http.StatusUnprocessableEntity {
			return err
		}
		errs := he.FieldErrors()
		if len(errs) == 0 {
			errs = []doctype.FieldError{{Message: he.Error()}}
		}
		return h.wizardRefused(w, r, p, vals, errs)
	}
	session.Clear(ctx)
	for _, warning := range saved.Warnings {
		web.Flash(ctx, "warning", warning)
	}
	web.Flash(ctx, "success", h.Copy.Saved)
	wizard.Respond(w, r, wizard.Finish, h.recordPath(saved.ID), nil)
	return nil
}

// wizardRefused shows the first failing step with its errors, or Check and save
// when no step owns them. Save and continue comes straight back.
func (h *handlers[R, In]) wizardRefused(w http.ResponseWriter, r *http.Request, p auth.Principal, vals map[string]string, errs []doctype.FieldError) error {
	last := len(h.wiz.steps) - 1
	i := slices.IndexFunc(h.wiz.steps, func(s docscreen.FormStep) bool { return len(stepErrors(s, errs)) > 0 })
	if i < 0 {
		i = last
	} else {
		errs = stepErrors(h.wiz.steps[i], errs)
	}
	v, err := h.wizardStep(r, p, i, last, vals, errs)
	if err != nil {
		return err
	}
	if i < last {
		v.M.ReturnTo = checkSlug
	}
	w.Header().Set("HX-Push-Url", h.wizardURL(i))
	return respondInvalid(w, r, v)
}

// wizardStay shows step i again with errs, 422.
func (h *handlers[R, In]) wizardStay(w http.ResponseWriter, r *http.Request, p auth.Principal, i, furthest int, vals map[string]string, errs []doctype.FieldError) error {
	v, err := h.wizardStep(r, p, i, furthest, vals, errs)
	if err != nil {
		return err
	}
	return respondInvalid(w, r, v)
}

func respondInvalid(w http.ResponseWriter, r *http.Request, v wizardView) error {
	wizard.Respond(w, r, wizard.Invalid, "", func(fragment bool) templ.Component {
		if fragment {
			return wizardFrame(v)
		}
		return wizardPage(v)
	})
	return nil
}

// wizardAdvance moves to step i: 303 for a plain post, the step's fragment with
// HX-Push-Url for htmx.
func (h *handlers[R, In]) wizardAdvance(w http.ResponseWriter, r *http.Request, p auth.Principal, i, furthest int, vals map[string]string) error {
	v, err := h.wizardStep(r, p, i, furthest, vals, nil)
	if err != nil {
		return err
	}
	wizard.Respond(w, r, wizard.Advance, h.wizardURL(i), func(bool) templ.Component { return wizardFrame(v) })
	return nil
}

// wizardStep is step i on screen. A return to Check and save is kept from the
// URL or the post. Start over shows once p has a draft to drop.
func (h *handlers[R, In]) wizardStep(r *http.Request, p auth.Principal, i, furthest int, vals map[string]string, errs []doctype.FieldError) (wizardView, error) {
	ctx := r.Context()
	last := len(h.wiz.steps) - 1
	m := h.wiz.model()
	m.Title, m.BaseURL, m.Current, m.Furthest = h.Copy.New, h.Base+"/new", i, furthest
	if i < last && r.FormValue("return") == checkSlug {
		m.ReturnTo = checkSlug
	}
	f := form{Values: vals, Errs: errs}
	m.Errors = wizardSummary(f, h.wiz.steps[i])
	v := wizardView{M: m, Bar: wizard.Bar{Back: i > 0}, Back: components.Link{Label: h.Copy.Plural, Href: h.Base}}
	if len(h.session(p).Get(ctx)) > 0 {
		v.M.Below = wizardStartOverForm(h.Base + "/new/" + startOverSlug)
	}
	if i == last {
		cards, err := h.wizardCards(ctx, vals)
		if err != nil {
			return v, err
		}
		v.M.After = wizardCheck(cards)
		v.Bar.Primary = "Save"
		return v, nil
	}
	for _, name := range h.wiz.steps[i].Fields {
		fd, _ := h.dt.Field(name) // handlers checked every step's fields
		v.Inputs = append(v.Inputs, h.input(fd, name, f))
	}
	return v, nil
}

// wizardSummary is the step's error summary. An error the step has no input for
// links to the heading.
func wizardSummary(f form, s docscreen.FormStep) []components.FieldErr {
	out := f.summary()
	for n, e := range f.Errs {
		if !slices.Contains(s.Fields, e.Field) {
			out[n].ID = "step-title"
		}
	}
	return out
}

// wizardCards is Check and save's summary of the session's values, as typed: a
// value a later save would refuse still shows.
func (h *handlers[R, In]) wizardCards(ctx context.Context, vals map[string]string) ([]sectionCard, error) {
	var cards []sectionCard
	err := h.Env.DB.Read(ctx, func(tx *db.Tx) error {
		for i, s := range h.wiz.steps[:len(h.wiz.steps)-1] {
			pairs, err := h.stepPairs(ctx, tx, s, vals, h.typedDisplay)
			if err != nil {
				return err
			}
			cards = append(cards, sectionCard{Title: s.Label, Pairs: pairs, Change: &components.Link{Label: "Change", Href: h.wizardURL(i) + "?return=" + checkSlug}})
		}
		return nil
	})
	return cards, err
}

// typedDisplay is a typed value's text: a Link's record when it has one, a
// label, else as typed. A Lookups field's value is what the person typed.
func (h *handlers[R, In]) typedDisplay(ctx context.Context, tx *db.Tx, fd doctype.Field, value string) (string, error) {
	if label := h.OptionLabels[fd.Name][value]; label != "" {
		return label, nil
	}
	if fd.Type == doctype.Date {
		if d, err := types.ParseDate(value); err == nil {
			return types.FormatDate(d), nil
		}
	}
	if fd.Type != doctype.Link {
		return value, nil
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return value, nil //nolint:nilerr // the step's Next shows why; here it is only a summary
	}
	s, err := docscreen.Display(ctx, tx, h.Env.Engine, fd, value)
	if errors.Is(err, doctype.ErrNotFound) {
		return value, nil
	}
	return s, err
}

// stepErrors keeps the errors on s's fields.
func stepErrors(s docscreen.FormStep, errs []doctype.FieldError) []doctype.FieldError {
	return slices.DeleteFunc(slices.Clone(errs), func(e doctype.FieldError) bool {
		return !slices.Contains(s.Fields, e.Field)
	})
}

// messages is each error's message.
func messages(errs []doctype.FieldError) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Message
	}
	return out
}

// wizardView is one wizard step as rendered.
type wizardView struct {
	M      wizard.Model
	Bar    wizard.Bar
	Inputs []templ.Component
	Back   components.Link
}
