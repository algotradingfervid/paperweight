package mstweb_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// No case ID: UI v2 C1b (§5.5 item 3, CD-359 (7)). A master's Field.More
// inputs go in one components.MoreDetails at the end of the form, on every
// mstkit form: the record page, a section, a wizard step and a SubForm. The
// group is open when a field shown inside it has a value or an error; a Check
// stored "0" holds nothing, and an inverted Check is named by its Invert. A field the form leaves out (Disabled, set by
// Disable and Enable) is never inside it, named in its summary or opening it.
// A form with no shown More field has no group, and a form whose every shown
// field is More shows them without one: a section the user chose to change
// never opens on a closed group alone.

const moreType = "mstweb.MoreC1b"

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name: moreType, Label: "Stub", PluralLabel: "Stubs", Table: "item",
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, More: true},
			{Name: "note", Label: "Note", Column: "note", Type: doctype.Text, More: true},
			{Name: "urgent", Label: "Urgent", Column: "urgent", Type: doctype.Check, More: true},
			{Name: "no_print", Label: "No print", Column: "no_print", Type: doctype.Check, Invert: "Print", More: true},
		},
	}, mstkit.LookupFunc(func(context.Context, *db.Tx, int64) (doctype.MasterInfo, bool, error) {
		return doctype.MasterInfo{}, false, nil
	}))
}

const moreSummary = "More details · note, urgent, print"

// onMore makes the screen moreType's.
func onMore(s *screen) {
	s.DocType = moreType
	s.Ref = func(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: moreType, ID: id} }
}

// moreGroup is the form's one More details group, after its other inputs.
func moreGroup(t *testing.T, r webtest.Response) *goquery.Selection {
	t.Helper()
	d := r.Doc.Find("form details.disclosure")
	if d.Length() != 1 {
		t.Fatalf("%d More details groups in the form, want 1\nbody: %s", d.Length(), webtest.Squash(r.Body))
	}
	if got := webtest.Squash(d.Find("summary").Text()); got != moreSummary {
		t.Errorf("summary = %q; want %q", got, moreSummary)
	}
	for _, id := range []string{"f-note", "f-urgent"} {
		if d.Find("#"+id).Length() != 1 {
			t.Errorf("#%s is not inside More details", id)
		}
	}
	if r.Doc.Find("#f-code").Length() != 1 || d.Find("#f-code").Length() != 0 {
		t.Error("Code should be outside More details")
	}
	if r.Doc.Find(`[name="disabled"]`).Length() != 0 {
		t.Error("the form has a Disabled input")
	}
	if d.NextAll().Find("input:not([type=hidden]), select, textarea").Length() != 0 {
		t.Error("an input comes after More details")
	}
	return d
}

func isOpen(d *goquery.Selection) bool {
	_, ok := d.Attr("open")
	return ok
}

// noGroup reports a failure when r's form has a More details group.
func noGroup(t *testing.T, r webtest.Response, why string) {
	t.Helper()
	if n := r.Doc.Find("form details.disclosure").Length(); n != 0 {
		t.Errorf("%s: %d More details groups, want none", why, n)
	}
}

// The values that open the group or leave it closed. Disabled is "1" in
// every case: the form leaves it out, so it never opens the group.
var moreCases = []struct {
	name string
	vals map[string]string
	open bool
}{
	{"nothing", map[string]string{}, false},
	{"unticked check", map[string]string{"urgent": "0"}, false},
	{"ticked check", map[string]string{"urgent": "1"}, true},
	{"text", map[string]string{"note": "x"}, true},
}

func moreRecord(vals map[string]string) stubRecord {
	rec := stubRecord{"code": "X", "disabled": "1"}
	for k, v := range vals {
		rec[k] = v
	}
	return rec
}

// The new page has the group closed; a stored record's page opens it for a
// value inside, and not for an unticked Check or the left-out Disabled.
func TestMoreC1b_RecordPage(t *testing.T) {
	h := harness(t, stubService{}, onMore)
	if d := moreGroup(t, h.Get(t, h.Env.Kavya, base+"/new")); isOpen(d) {
		t.Error("new page: More details is open with nothing typed")
	}
	for _, c := range moreCases {
		t.Run(c.name, func(t *testing.T) {
			h := harness(t, stubService{rec: moreRecord(c.vals)}, onMore)
			if got := isOpen(moreGroup(t, h.Get(t, h.Env.Kavya, record))); got != c.open {
				t.Errorf("open = %v; want %v", got, c.open)
			}
		})
	}
}

// An error on a blank field inside opens the group on the refused form.
func TestMoreC1b_RecordPageErrorOpens(t *testing.T) {
	h := harness(t, stubService{err: &mstkit.ValidationError{Fields: []doctype.FieldError{{Field: "note", Message: "Note is wrong."}}}}, onMore)
	r := h.Post(t, h.Env.Kavya, base+"/new", url.Values{"code": {"X"}, "note": {""}, "urgent": {"0"}})
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	if !isOpen(moreGroup(t, r)) {
		t.Error("More details is closed with an error inside")
	}
}

// A master with no More field has no group.
func TestMoreC1b_NoMoreFieldNoGroup(t *testing.T) {
	h := harness(t, stubService{})
	noGroup(t, h.Get(t, h.Env.Kavya, base+"/new"), "Item's new page")
}

// A section groups its own More fields as the record page does, both ways; a
// section without a More field has no group, and one of only More fields
// shows them plainly.
func TestMoreC1b_Section(t *testing.T) {
	sections := func(s *screen) {
		onMore(s)
		s.Sections = []docscreen.FormStep{
			{Slug: "details", Label: "Details", Fields: []string{"code", "note", "urgent", "no_print"}},
			{Slug: "name", Label: "Name", Fields: []string{"name"}},
			{Slug: "extra", Label: "Extra", Fields: []string{"note", "urgent", "no_print"}},
		}
	}
	for _, c := range moreCases {
		t.Run(c.name, func(t *testing.T) {
			h := harness(t, stubService{rec: moreRecord(c.vals)}, sections)
			if got := isOpen(moreGroup(t, h.Get(t, h.Env.Kavya, record+"/edit/details"))); got != c.open {
				t.Errorf("open = %v; want %v", got, c.open)
			}
		})
	}
	t.Run("error", func(t *testing.T) {
		h := harness(t, stubService{rec: moreRecord(nil), err: &mstkit.ValidationError{Fields: []doctype.FieldError{{Field: "note", Message: "Note is wrong."}}}}, sections)
		r := h.Post(t, h.Env.Kavya, record+"/edit/details", url.Values{"version": {"3"}, "code": {"X"}, "note": {""}, "urgent": {"0"}})
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		if !isOpen(moreGroup(t, r)) {
			t.Error("More details is closed with an error inside")
		}
	})
	h := harness(t, stubService{rec: moreRecord(nil)}, sections)
	noGroup(t, h.Get(t, h.Env.Kavya, record+"/edit/name"), "section without a More field")
	r := h.Get(t, h.Env.Kavya, record+"/edit/extra")
	webtest.WantStatus(t, r, http.StatusOK)
	noGroup(t, r, "section of only More fields")
	if r.Doc.Find("form #f-note").Length() != 1 || r.Doc.Find("form #f-urgent").Length() != 1 {
		t.Error("section of only More fields: Note or Urgent is missing")
	}
}

// moreWizard is the wizard on moreType: a details step with the More fields,
// whose ToInput refuses the note "bad".
func moreWizard(s *screen) {
	onMore(s)
	w := s.Wizard.(mstweb.CreateWizard[stubDraft])
	w.Steps = []docscreen.FormStep{
		{Slug: "details", Label: "Details", Fields: []string{"code", "note", "urgent", "no_print"}},
		{Slug: "more", Label: "More", Fields: []string{"name"}},
	}
	w.ToInput = func(v map[string]string) (stubDraft, []doctype.FieldError) {
		if v["note"] == "bad" {
			return stubDraft{}, []doctype.FieldError{{Field: "note", Message: "Note is wrong."}}
		}
		return stubDraft{code: v["code"]}, nil
	}
	s.Wizard = w
}

// A wizard step groups its More fields, both ways: what was typed opens the
// group when the step is shown again, and so does an error.
func TestMoreC1b_WizardStep(t *testing.T) {
	h := wizardHarness(t, nil, moreWizard)
	b := h.Browser(h.Env.Kavya)
	if d := moreGroup(t, b.Get(t, wizardFirst)); isOpen(d) {
		t.Error("first visit: More details is open with nothing typed")
	}
	for _, c := range moreCases {
		t.Run(c.name, func(t *testing.T) {
			b := wizardHarness(t, nil, moreWizard).Browser(h.Env.Kavya)
			form := url.Values{"code": {"X"}, "note": {""}, "urgent": {"0"}}
			for k, v := range c.vals {
				form.Set(k, v)
			}
			webtest.WantStatus(t, b.Post(t, wizardFirst, form), http.StatusSeeOther)
			if got := isOpen(moreGroup(t, b.Get(t, wizardFirst))); got != c.open {
				t.Errorf("open = %v; want %v", got, c.open)
			}
		})
	}
	t.Run("error", func(t *testing.T) {
		b := wizardHarness(t, nil, moreWizard).Browser(h.Env.Kavya)
		r := b.Post(t, wizardFirst, url.Values{"code": {"X"}, "note": {"bad"}, "urgent": {"0"}})
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		if !isOpen(moreGroup(t, r)) {
			t.Error("More details is closed with an error inside")
		}
	})
	noGroup(t, h.Browser(h.Env.Kavya).Get(t, base+"/new/more"), "step without a More field")
}

// A SubForm built from Pages.Input groups the More fields as the Screen's own
// forms do: the query gives the values, and err an error on Note.
func TestMoreC1b_SubForm(t *testing.T) {
	const sub = record + "/sub"
	h := harness(t, stubService{rec: moreRecord(nil)}, func(s *screen) {
		onMore(s)
		s.Mount = func(pages mstweb.Pages) func(chi.Router) {
			return func(r chi.Router) {
				r.Get(sub, pages.Handle(func(w http.ResponseWriter, r *http.Request, _ auth.Principal) error {
					q := r.URL.Query()
					f := mstweb.Form{Values: map[string]string{"code": "X", "note": q.Get("note"), "urgent": q.Get("urgent")}}
					if q.Has("err") {
						f.Errs = []doctype.FieldError{{Field: "note", Message: "Note is wrong."}}
					}
					s := mstweb.SubForm{Title: "Sub", Action: sub, Back: components.Link{Label: "Stub", Href: record}, Form: f}
					dt, _ := doctype.Lookup(moreType)
					for _, fd := range dt.Fields {
						if fd.Name != "disabled" && (q.Has("all") || fd.More || fd.Name == "code") {
							s.Inputs = append(s.Inputs, pages.Input(fd, fd.Name, f))
						}
					}
					mstweb.RenderSubForm(w, r, http.StatusOK, s)
					return nil
				}))
			}
		}
	})
	for _, c := range moreCases {
		t.Run(c.name, func(t *testing.T) {
			q := url.Values{}
			for k, v := range c.vals {
				q.Set(k, v)
			}
			if got := isOpen(moreGroup(t, h.Get(t, h.Env.Kavya, sub+"?"+q.Encode()))); got != c.open {
				t.Errorf("open = %v; want %v", got, c.open)
			}
		})
	}
	if !isOpen(moreGroup(t, h.Get(t, h.Env.Kavya, sub+"?err"))) {
		t.Error("error: More details is closed with an error inside")
	}
	if !isOpen(moreGroup(t, h.Get(t, h.Env.Kavya, sub+"?all&note=x"))) {
		t.Error("all fields: More details is closed with a value inside")
	}
}
