package mstweb_test

import (
	"context"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// No case ID: the create wizard's generic branches, which Employee's service
// never takes. W is its own type, as Party's will be: one save, several records.

const (
	wizardFirst = base + "/new/details"
	wizardCheck = base + "/new/check"
	manySteps   = "mstweb.ManySteps"
)

// wideDocType has enough fields for a draft to outgrow maxDraftBytes.
const (
	wideDocType = "mstweb.Wide"
	wideFields  = 140
)

func init() {
	fields := []doctype.Field{
		{Name: "code", Label: "Code", Column: "code", Type: doctype.Text},
		{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check},
	}
	for i := range wideFields {
		name := "w" + strconv.Itoa(i) // no such column: only the wizard reads these
		maxLen := 0
		if i == 0 {
			maxLen = 2000 // past the kit's own limit
		}
		fields = append(fields, doctype.Field{Name: name, Label: "W", Column: name, Type: doctype.Text, MaxLen: maxLen})
	}
	mstkit.RegisterMaster(doctype.DocType{Name: wideDocType, Label: "Stub", PluralLabel: "Stubs", Table: "item", Fields: fields},
		mstkit.LookupFunc(func(context.Context, *db.Tx, int64) (doctype.MasterInfo, bool, error) {
			return doctype.MasterInfo{}, false, nil
		}))
}

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name: manySteps, Label: "Stub", PluralLabel: "Stubs", Table: "item",
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check},
			{Name: "hsn_sac", Label: "A", Column: "hsn_sac", Type: doctype.Text, Step: "a"},
			{Name: "name", Label: "B", Column: "name", Type: doctype.Text, Step: "b"},
			{Name: "item_type", Label: "C", Column: "item_type", Type: doctype.Text, Step: "c"},
			{Name: "standard_rate", Label: "D", Column: "standard_rate", Type: doctype.Text, Step: "d"},
		},
	}, mstkit.LookupFunc(func(context.Context, *db.Tx, int64) (doctype.MasterInfo, bool, error) {
		return doctype.MasterInfo{}, false, nil
	}))
}

type stubDraft struct{ code string }

func wizardHarness(t *testing.T, save func(context.Context, auth.Principal, stubDraft) (mstkit.Saved, error), opts ...func(*screen)) *webtest.Harness {
	t.Helper()
	return webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		s := screen{
			DocType: item.DocType, Base: base, Env: k, Svc: stubService{},
			Values: func(r stubRecord) map[string]string { return maps.Clone(r) },
			Head:   func(stubRecord) mstweb.Head { return mstweb.Head{Code: "STUB", Name: "Stub", Version: 3} },
			Ref:    func(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: item.DocType, ID: id} },
			Copy:   mstweb.Copy{Plural: "Stubs", Noun: "stubs", One: "stub", New: "New stub", Saved: "Stub saved.", Rows: "Conversions", AddRow: "Add conversion"},
			Wizard: mstweb.CreateWizard[stubDraft]{
				Sessions: sm, Save: save,
				Steps: []docscreen.FormStep{
					{Slug: "details", Label: "Details", Fields: []string{"code", "name", "item_type"}},
					{Slug: "more", Label: "More", Fields: []string{"hsn_sac"}},
				},
				ToInput: func(v map[string]string) (stubDraft, []doctype.FieldError) { return stubDraft{code: v["code"]}, nil },
			},
		}
		for _, opt := range opts {
			opt(&s)
		}
		return s.Routes()
	})
}

func toWizardCheck(t *testing.T, b *webtest.Browser) {
	t.Helper()
	webtest.WantStatus(t, b.Post(t, wizardFirst, url.Values{"code": {"CEMENT"}, "item_type": {"Goods"}}), http.StatusSeeOther)
	webtest.WantRedirect(t, b.Post(t, base+"/new/more", url.Values{}), wizardCheck)
}

// A refusal no step owns stays on Check and save, its summary pointing at the
// heading, since there is no input to link to.
func TestWizard_RefusalNoStepOwns(t *testing.T) {
	for name, err := range map[string]error{
		"no field":      &refusal{status: http.StatusUnprocessableEntity, text: "Nothing can be saved today."},
		"no such field": &refusal{status: http.StatusUnprocessableEntity, text: "x", fields: []doctype.FieldError{{Field: "nowhere", Message: "Nothing can be saved today."}}},
	} {
		t.Run(name, func(t *testing.T) {
			h := wizardHarness(t, func(context.Context, auth.Principal, stubDraft) (mstkit.Saved, error) { return mstkit.Saved{}, err })
			b := h.Browser(h.Env.Kavya)
			toWizardCheck(t, b)
			r := b.Post(t, wizardCheck, url.Values{})
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			webtest.WantText(t, r, "Nothing can be saved today.")
			webtest.WantText(t, r, "Check and save")
			if href, _ := r.Doc.Find(`a[href="#step-title"]`).Attr("href"); href == "" {
				t.Errorf("the summary doesn't link to the heading\nbody: %s", webtest.Squash(r.Body))
			}
			if got := r.Header.Get("HX-Push-Url"); got != wizardCheck {
				t.Errorf("HX-Push-Url = %q; want %q", got, wizardCheck)
			}
		})
	}
}

// A 503 or 403 from Save isn't a step's error, and the draft stays.
func TestWizard_OtherErrorsAreFailures(t *testing.T) {
	h := wizardHarness(t, func(context.Context, auth.Principal, stubDraft) (mstkit.Saved, error) {
		return mstkit.Saved{}, &refusal{status: http.StatusForbidden, text: "No."}
	})
	b := h.Browser(h.Env.Kavya)
	toWizardCheck(t, b)
	webtest.WantStatus(t, b.Post(t, wizardCheck, url.Values{}), http.StatusForbidden)
	webtest.WantStatus(t, b.Get(t, wizardCheck), http.StatusOK)
}

// Save's warnings go on the record page with the banner, and W reaches Save.
func TestWizard_SaveWarningsAreFlashed(t *testing.T) {
	var got stubDraft
	h := wizardHarness(t, func(_ context.Context, _ auth.Principal, in stubDraft) (mstkit.Saved, error) {
		got = in
		return mstkit.Saved{ID: 7, Warnings: []string{"Check the rate."}}, nil
	})
	b := h.Browser(h.Env.Kavya)
	toWizardCheck(t, b)
	saved := b.Post(t, wizardCheck, url.Values{})
	webtest.WantRedirect(t, saved, record)
	if got.code != "CEMENT" {
		t.Errorf("Save got %+v", got)
	}
	page := b.Get(t, saved.Location)
	webtest.WantText(t, page, "Check the rate.")
	webtest.WantBanner(t, page, "Stub saved.")
}

// Check and save shows a Select's label, not its stored value.
func TestWizard_CheckShowsOptionLabels(t *testing.T) {
	h := wizardHarness(t, nil, func(s *screen) {
		s.OptionLabels = map[string]map[string]string{"item_type": {"Goods": "Physical goods"}}
	})
	b := h.Browser(h.Env.Kavya)
	toWizardCheck(t, b)
	webtest.WantText(t, b.Get(t, wizardCheck), "Physical goods")
}

// NoNew leaves the wizard out too (MST-019), and the plain form is gone with a
// Wizard.
func TestWizard_Mounting(t *testing.T) {
	h := wizardHarness(t, nil, func(s *screen) { s.NoNew = true })
	webtest.WantStatus(t, h.Get(t, h.Env.Kavya, wizardFirst), http.StatusNotFound)
	h = wizardHarness(t, nil)
	webtest.WantStatus(t, h.Post(t, h.Env.Kavya, base+"/new", url.Values{}), http.StatusMethodNotAllowed)
}

// More steps than §8.3 allows is a programming error, found at mount.
func TestWizard_StepsPanicsPastTheLimit(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Errorf("Steps didn't panic")
		}
	}()
	mstweb.Steps(manySteps)
}

// Steps leaves out disabled: Disable and Enable set it.
func TestWizard_StepsHaveNoDisabled(t *testing.T) {
	for _, s := range mstweb.Steps(item.DocType) {
		for _, f := range s.Fields {
			if f == "disabled" {
				t.Errorf("step %s has disabled", s.Slug)
			}
		}
	}
}

// The session's limits: a value at the kit's default fits, counted in
// characters, and one past it is refused by name. A draft past the total is
// refused on the page and stays out of the session.
func TestWizard_SizeLimits(t *testing.T) {
	h := wizardHarness(t, nil)
	b := h.Browser(h.Env.Kavya)
	fits := url.Values{"name": {strings.Repeat("é", 1000)}}
	webtest.WantStatus(t, b.Post(t, wizardFirst, fits), http.StatusSeeOther)
	r := b.Post(t, wizardFirst, url.Values{"name": {strings.Repeat("é", 1001)}})
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "name", "Name can be at most 1,000 characters.")

	var names []string
	for i := range wideFields {
		names = append(names, "w"+strconv.Itoa(i))
	}
	h = webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		return screen{
			DocType: wideDocType, Base: base, Env: k, Svc: stubService{},
			Copy: mstweb.Copy{Plural: "Stubs", Noun: "stubs", One: "stub", New: "New stub", Saved: "Stub saved."},
			Wizard: mstweb.CreateWizard[stubDraft]{
				Sessions: sm,
				Steps:    []docscreen.FormStep{{Slug: "details", Label: "Details", Fields: names}, {Slug: "more", Label: "More"}},
				ToInput:  func(map[string]string) (stubDraft, []doctype.FieldError) { return stubDraft{}, nil },
			},
		}.Routes()
	})
	b = h.Browser(h.Env.Kavya)
	full := url.Values{}
	for _, n := range names {
		full.Set(n, strings.Repeat("x", 1000))
	}
	over := b.Post(t, wizardFirst, full)
	webtest.WantStatus(t, over, http.StatusUnprocessableEntity)
	webtest.WantText(t, over, "This draft is too large to keep. Shorten what you typed, or start over.")
	got, _ := b.Get(t, wizardFirst).Value("w0")
	if got != "" {
		t.Errorf("the refused draft reached the session: w0 has %d bytes", len(got))
	}
	// The cap is on what the session stores: quotes double when marshalled, so
	// 100,000 of them are over it though the plain bytes aren't.
	quoted := url.Values{}
	for _, n := range names[:100] {
		quoted.Set(n, strings.Repeat(`"`, 1000))
	}
	webtest.WantText(t, b.Post(t, wizardFirst, quoted), "This draft is too large to keep.")

	// A Field's MaxLen past the kit's limit raises it.
	long := url.Values{"w0": {strings.Repeat("x", 1500)}}
	webtest.WantStatus(t, b.Post(t, wizardFirst, long), http.StatusSeeOther)
	long.Set("w0", strings.Repeat("x", 2001))
	tooLong := b.Post(t, wizardFirst, long)
	webtest.WantStatus(t, tooLong, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, tooLong, "w0", "W can be at most 2,000 characters.")

	// Just under the cap fits: a Party's rows have the room.
	full = url.Values{}
	for _, n := range names[:125] {
		full.Set(n, strings.Repeat("x", 1000))
	}
	webtest.WantStatus(t, b.Post(t, wizardFirst, full), http.StatusSeeOther)
}

// A Field's MaxLen under the kit's limit is the service's rule: the kit keeps
// out of it, so a master with no Validate hears it from the save (CD-142).
func TestWizard_FieldMaxLenIsTheOwners(t *testing.T) {
	h := wizardHarness(t, nil)
	b := h.Browser(h.Env.Kavya)
	webtest.WantStatus(t, b.Post(t, wizardFirst, url.Values{"code": {strings.Repeat("C", 21)}}), http.StatusSeeOther)
}

// Two masters' wizards in one session keep separate drafts: the key names the
// DocType.
func TestWizard_TwoMastersDontMix(t *testing.T) {
	const other = "/m/stub2"
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		mount := func(docType, b string, fields ...string) func(chi.Router) {
			return screen{
				DocType: docType, Base: b, Env: k, Svc: stubService{},
				Copy: mstweb.Copy{Plural: "Stubs", Noun: "stubs", One: "stub", New: "New stub", Saved: "Stub saved.", Rows: "R", AddRow: "A"},
				Wizard: mstweb.CreateWizard[stubDraft]{
					Sessions: sm,
					Steps:    []docscreen.FormStep{{Slug: "details", Label: "Details", Fields: fields}, {Slug: "more", Label: "More"}},
					ToInput:  func(map[string]string) (stubDraft, []doctype.FieldError) { return stubDraft{}, nil },
				},
			}.Routes()
		}
		return func(r chi.Router) {
			mount(item.DocType, base, "code")(r)
			mount(nullableDocType, other, "code")(r)
		}
	})
	b := h.Browser(h.Env.Kavya)
	webtest.WantStatus(t, b.Post(t, wizardFirst, url.Values{"code": {"ONE"}}), http.StatusSeeOther)
	if got, _ := b.Get(t, other+"/new/details").Value("code"); got != "" {
		t.Errorf("the other master's wizard shows %q", got)
	}
	webtest.WantStatus(t, b.Post(t, other+"/new/details", url.Values{"code": {"TWO"}}), http.StatusSeeOther)
	if got, _ := b.Get(t, wizardFirst).Value("code"); got != "ONE" {
		t.Errorf("first wizard shows %q; want ONE", got)
	}
}

// A section save's warnings show on the record page, as a whole save's do.
func TestSections_SaveWarningsAreFlashed(t *testing.T) {
	svc := stubService{rec: stubRecord{"code": "X"}, saved: mstkit.Saved{ID: 7, Warnings: []string{"Check the rate."}}}
	h := harness(t, svc, func(s *screen) {
		s.Sections = []docscreen.FormStep{{Slug: "details", Label: "Details", Fields: []string{"code"}}}
	})
	b := h.Browser(h.Env.Kavya)
	r := b.Post(t, record+"/edit/details", url.Values{"version": {"3"}, "code": {"Y"}})
	webtest.WantRedirect(t, r, record)
	webtest.WantText(t, b.Get(t, r.Location), "Check the rate.")
}

// A master's mistakes are found at mount: a step naming a field the DocType
// lacks, and a wizard outside §8.3's shape.
func TestWizard_BadShapePanicsAtMount(t *testing.T) {
	bad := []docscreen.FormStep{{Slug: "details", Label: "Details", Fields: []string{"nosuch"}}, {Slug: "more", Label: "More"}}
	for name, set := range map[string]func(*screen){
		"section field": func(s *screen) { s.Sections = bad },
		"wizard field":  func(s *screen) { s.Wizard = mstweb.CreateWizard[stubDraft]{Steps: bad} },
		"one step":      func(s *screen) { s.Wizard = mstweb.CreateWizard[stubDraft]{Steps: bad[1:]} },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("no panic")
				}
			}()
			harness(t, stubService{}, set)
		})
	}
}

// Without a Validate a section's parse errors stand alone.
func TestSections_ParseErrorWithoutValidate(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"code": "X"}}, func(s *screen) {
		s.Sections = []docscreen.FormStep{{Slug: "details", Label: "Details", Fields: []string{"code"}}}
		s.ToInput = func(map[string]string) (stubRecord, []doctype.FieldError) {
			return nil, []doctype.FieldError{{Field: "code", Message: "Not a code."}}
		}
	})
	r := h.Browser(h.Env.Kavya).Post(t, record+"/edit/details", url.Values{"version": {"3"}, "code": {"Y"}})
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "code", "Not a code.")
}
