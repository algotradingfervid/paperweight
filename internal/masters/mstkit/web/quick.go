package mstweb

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// Quick lets a picker add a record of the master without leaving its page
// (UI v2 §5.8, N10): the search offers "New {noun} "{q}"", which opens
// components.QuickCreate, and POST Base/quick saves it and answers with the
// picker, the new record chosen.
type Quick struct {
	// Fields are the quick form's fields, in order (QuickFields): the
	// DocType's or another master's, rendered as the form's are. A "name"
	// field opens with the search. A "code" field stays hidden and blank, and
	// the save takes mstkit.SuggestCode's code for the name, until that code
	// is refused: then it shows, with the refusal. They post as quick_{name}.
	Fields []doctype.Field
	// Given names fields another quick field can give, which stay hidden
	// while it does (Party's state, from a valid GSTIN).
	Given map[string]Given
	// Nouns is a record's noun (one and many) by the picker's role, "" for a picker with
	// none: {"vendor": "vendor"}. Any other role offers and saves nothing.
	Nouns map[string]components.Noun
	// Icon is the sheet's tile; Line its sentence under the title.
	Icon, Line string
	// Creates names the other DocTypes a quick save creates: the New option
	// and the route need create on each, as on the master.
	Creates []string
	// Defaults are values every quick record gets and the form doesn't show.
	Defaults map[string]string
	// Save is optional and saves values (keyed by Field.Name) in one write
	// for the picker's role; without it ToInput and Svc.Create do.
	Save func(ctx context.Context, p auth.Principal, role string, values map[string]string) (mstkit.Saved, error)
}

// Given says a quick field can be left out: By names the field that gives it,
// Check is By's live check, which echoes only a value that gives the field,
// and Echo is that echo for a value, "" when it doesn't give it. The field
// hides with the echo, also as By changes in the browser (quickcreate.css),
// and the save leaves it blank for the service to derive, so a value left in
// the hidden field never disagrees with By.
type Given struct {
	By    string
	Check components.CodeKind
	Echo  func(by string) string
}

// quickPrefix keeps the quick form's names apart from the page's.
const quickPrefix = "quick_"

// quickNoun is the noun of the New option p gets on a search with role, and
// false when there is none: no Quick, a role it doesn't take, or no create
// on the master or on what it Creates.
func (h *handlers[R, In]) quickNoun(ctx context.Context, p auth.Principal, role string) (components.Noun, bool, error) {
	if h.Quick == nil {
		return components.Noun{}, false, nil
	}
	noun, ok := h.Quick.Nouns[role]
	if !ok {
		return components.Noun{}, false, nil
	}
	err := h.quickAllowed(ctx, p)
	if errors.Is(err, auth.ErrForbidden) {
		return noun, false, nil
	}
	return noun, err == nil, err
}

// quickAllowed is AUTH's create right on the master and on each of Creates.
func (h *handlers[R, In]) quickAllowed(ctx context.Context, p auth.Principal) error {
	for _, dt := range slices.Concat([]string{h.DocType}, h.Quick.Creates) {
		if err := h.Env.Auth.Can(ctx, p, dt, auth.RightCreate, nil); err != nil {
			return err
		}
	}
	return nil
}

// quickResults is the search's rows with the New option and its sheet,
// which opens with the search as the name.
func (h *handlers[R, In]) quickResults(listboxID, role string, noun components.Noun, rows []components.PickRow, q string) templ.Component {
	f := form{Values: map[string]string{quickPrefix + "name": q}}
	sheet := components.QuickCreate(h.quickProps(listboxID, role, noun, f))
	return components.PickerResultsNew(listboxID, rows, q, components.PickNew{Noun: noun, Sheet: sheet})
}

// quickProps is the sheet's form with f's values and errors. A field that
// needs no answer is drawn blank and hidden, and a refused one always shows.
func (h *handlers[R, In]) quickProps(listboxID, role string, noun components.Noun, f form) components.QuickCreateProps {
	qp := components.QuickCreateProps{
		ListboxID: listboxID, Title: "New " + noun.One, Icon: h.Quick.Icon, Line: h.Quick.Line,
		Action: h.Base + "/quick", Role: role, Problem: f.Problem,
	}
	for _, e := range f.Errs {
		qp.Errors = append(qp.Errors, components.FieldErr{ID: components.FieldID(e.Field), Message: e.Message})
	}
	for _, fd := range h.Quick.Fields {
		path := quickPrefix + fd.Name
		_, gives := h.Quick.Given[fd.Name]
		in := h.control(fd, path, f)
		for _, g := range h.Quick.Given {
			if g.By == fd.Name { // control's CodeInput with By's own check
				in = components.QuickGives(components.CodeInput(withMaxLen(props(fd, path, f), fd), g.Check))
			}
		}
		switch {
		case fd.Name == "code":
			in = components.QuickHidden(f.Values[path] == "" && f.errFor(path) == "", false, in)
		case gives:
			in = components.QuickHidden(false, true, in)
		}
		qp.Inputs = append(qp.Inputs, in)
	}
	return qp
}

// quick is POST Base/quick: a record from the quick form, for the picker
// whose props the sheet posts (components.PickerFromForm). quickAllowed's
// rights come first; then the role must be one of Nouns' and the picker's
// own (its search's role=), before any value is read (CD-259). A form that
// posts no code takes the one suggested for the name; the save's own check
// stays authoritative. The save is one write. A refusal redraws the sheet's
// form (422). A save answers with the picker, the record chosen and Change
// focused, and empties the sheet's slot, so the sheet closes and the rest of
// the page stays as typed; the chosen record's label and line need read on
// it, as any link label does (CD-233).
func (h *handlers[R, In]) quick(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	ctx := r.Context()
	if err := h.quickAllowed(ctx, p); err != nil {
		return err
	}
	role := r.PostFormValue("role")
	noun, ok := h.Quick.Nouns[role]
	picker := components.PickerFromForm(r.PostForm)
	if !ok || role != pickerRole(picker.SearchURL) {
		return auth.ErrBadForm
	}
	values := maps.Clone(h.Quick.Defaults)
	if values == nil {
		values = map[string]string{}
	}
	f := form{Values: map[string]string{}}
	for _, fd := range h.Quick.Fields {
		v := r.PostFormValue(quickPrefix + fd.Name)
		values[fd.Name], f.Values[quickPrefix+fd.Name] = v, v
	}
	for name, given := range h.Quick.Given {
		if by := quickPrefix + given.By; given.Echo(f.Values[by]) != "" {
			values[name], f.Values[quickPrefix+name] = "", ""
			f.Echo = map[string]string{by: given.Echo(f.Values[by])}
		}
	}
	if code, coded := values["code"]; coded && code == "" {
		if err := h.suggestCode(ctx, values); err != nil {
			return err
		}
	}
	saved, err := h.quickSave(ctx, p, role, values)
	if f.Values[quickPrefix+"code"] == "" && codeRefused(err) {
		// A code generated from the name and taken before the write: one more
		// try with the next free code (CD-388).
		if err := h.suggestCode(ctx, values); err != nil {
			return err
		}
		saved, err = h.quickSave(ctx, p, role, values)
	}
	if err != nil {
		return h.quickRefused(w, r, picker.ListboxID, role, noun, f, values["code"], err)
	}
	chosen := components.PickRow{ID: saved.ID}
	err = h.Env.DB.Read(ctx, func(tx *db.Tx) error {
		if err := h.Env.CanRowTx(ctx, tx, p, h.kind(), auth.RightRead, h.Ref(saved.ID)); err != nil {
			return err
		}
		rec, err := h.Svc.Get(ctx, tx, saved.ID)
		if err != nil {
			return err
		}
		head := h.Head(rec)
		chosen.Code, chosen.Name = head.Code, head.Name
		if h.PickLine != nil {
			chosen.Line, err = h.PickLine(ctx, tx, p, saved.ID)
		}
		return err
	})
	if err != nil {
		return err
	}
	picker.Chosen = &chosen
	picker.Focus = true
	answer := components.QuickChosen(picker)
	ui.Render(w, r, http.StatusOK, answer, answer)
	return nil
}

// suggestCode sets values' code to mstkit.SuggestCode's for its name.
func (h *handlers[R, In]) suggestCode(ctx context.Context, values map[string]string) error {
	return h.Env.DB.Read(ctx, func(tx *db.Tx) (err error) {
		values["code"], err = mstkit.SuggestCode(ctx, tx, h.kind(), values["name"])
		return err
	})
}

// codeRefused reports whether err is a 422 with an error on the code.
func codeRefused(err error) bool {
	var he doctype.HTTPError
	return errors.As(err, &he) && he.Status() == http.StatusUnprocessableEntity &&
		slices.ContainsFunc(he.FieldErrors(), func(e doctype.FieldError) bool { return e.Field == "code" })
}

// pickerRole is the role= of a picker's search URL, "" when it has none or
// doesn't parse.
func pickerRole(searchURL string) string {
	u, err := url.Parse(searchURL)
	if err != nil {
		return ""
	}
	return u.Query().Get("role")
}

// quickSave is Quick.Save, else ToInput and the service's Create; a value
// ToInput can't read refuses with its error, and the service isn't called
// (CD-37).
func (h *handlers[R, In]) quickSave(ctx context.Context, p auth.Principal, role string, values map[string]string) (mstkit.Saved, error) {
	if h.Quick.Save != nil {
		return h.Quick.Save(ctx, p, role, values)
	}
	in, errs := h.ToInput(values)
	if len(errs) > 0 {
		return mstkit.Saved{}, &mstkit.ValidationError{Fields: errs}
	}
	return h.Svc.Create(ctx, p, in)
}

// quickRefused redraws the sheet's form for a 422: an error on a quick field
// next to it. The others go in the form's banner only while no quick field
// has one, since they often follow from it (Item's purchase UoM is the
// stock UoM, so a bad UoM refuses both); fixing the shown ones brings them
// back. A code refused while it was generated shows, as generated; with no
// name there is nothing to generate from, and Name's own error says so.
// Anything else goes to fail.
func (h *handlers[R, In]) quickRefused(w http.ResponseWriter, r *http.Request, listboxID, role string, noun components.Noun, f form, code string, err error) error {
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != http.StatusUnprocessableEntity {
		return err
	}
	var others []string
	for _, e := range he.FieldErrors() {
		if e.Field == "code" && f.Values[quickPrefix+"code"] == "" {
			if f.Values[quickPrefix+"name"] == "" {
				continue
			}
			f.Values[quickPrefix+"code"] = code
		}
		if slices.ContainsFunc(h.Quick.Fields, func(fd doctype.Field) bool { return fd.Name == e.Field }) {
			f.Errs = append(f.Errs, doctype.FieldError{Field: quickPrefix + e.Field, Message: e.Message})
		} else {
			others = append(others, e.Message)
		}
	}
	if len(f.Errs) == 0 {
		f.Problem = cmp.Or(strings.Join(others, " "), he.Error())
	}
	w.Header().Set("HX-Retarget", "#"+components.QuickFormID(listboxID))
	w.Header().Set("HX-Reswap", "outerHTML")
	frag := components.QuickForm(h.quickProps(listboxID, role, noun, f))
	ui.Render(w, r, http.StatusUnprocessableEntity, frag, frag)
	return nil
}
