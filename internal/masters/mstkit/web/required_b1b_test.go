package mstweb_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// No case ID: UI v2 B1b, A-9 on the master record page. Item's Code (required)
// goes through the kit's CodeInput and HSN/SAC (optional) through docscreen;
// the required one is marked, the optional one isn't, and no label says
// "(optional)".
func TestScreens_RequiredMarkedOnTheForm(t *testing.T) {
	h := harness(t, stubService{}, func(s *screen) { s.Codes = map[string]components.CodeKind{"code": components.GSTIN} })
	page := h.Get(t, h.Env.Kavya, base+"/new")
	webtest.WantStatus(t, page, http.StatusOK)
	if page.Doc.Find(`#f-code[aria-required="true"]`).Length() != 1 {
		t.Error(`#f-code has no aria-required="true"`)
	}
	if !strings.Contains(page.Doc.Find(`label[for="f-code"]`).Text(), "(required)") {
		t.Error("Code's label has no (required)")
	}
	if page.Doc.Find(`#f-hsn_sac[aria-required]`).Length() != 0 || strings.Contains(page.Doc.Find(`label[for="f-hsn_sac"]`).Text(), "(required)") {
		t.Error("optional HSN/SAC is marked required")
	}
	if strings.Contains(page.Doc.Find("label, legend").Text(), "(optional)") {
		t.Error("a label says (optional)")
	}
}

const requiredNote = "Fields marked * are required."

func hasNote(r webtest.Response) bool {
	return strings.Contains(webtest.Squash(r.Doc.Find("form").Text()), requiredNote)
}

// The record page's form starts with the required note when it has a
// required field (Item's Code); a section of only optional fields doesn't.
func TestScreens_RequiredNoteOnlyWithARequiredField(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"code": "X"}}, func(s *screen) {
		s.Sections = []docscreen.FormStep{
			{Slug: "details", Label: "Details", Fields: []string{"code"}},
			{Slug: "tax", Label: "Tax", Fields: []string{"hsn_sac"}},
		}
	})
	if r := h.Get(t, h.Env.Kavya, base+"/new"); !hasNote(r) {
		t.Error("new page: no required note")
	}
	if r := h.Get(t, h.Env.Kavya, record+"/edit/details"); !hasNote(r) {
		t.Error("section with Code: no required note")
	}
	if r := h.Get(t, h.Env.Kavya, record+"/edit/tax"); hasNote(r) {
		t.Error("section of only optional fields shows the required note")
	}
}

// The new and record pages of a master whose fields are all optional
// (manySteps, without sections) show no note.
func TestScreens_NoRequiredNoteWithoutARequiredField(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"code": "X"}}, func(s *screen) {
		s.DocType = manySteps
		s.Ref = func(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: manySteps, ID: id} }
	})
	for _, target := range []string{base + "/new", record} {
		r := h.Get(t, h.Env.Kavya, target)
		webtest.WantStatus(t, r, http.StatusOK)
		if r.Doc.Find(`input[name="code"]`).Length() != 1 {
			t.Fatalf("%s: no form with a code input", target)
		}
		if hasNote(r) {
			t.Errorf("%s: a form of only optional fields shows the required note", target)
		}
	}
}

// A wizard step with a required field starts with the note; a step of only
// optional fields doesn't.
func TestWizard_RequiredNoteOnlyWithARequiredField(t *testing.T) {
	h := wizardHarness(t, nil)
	b := h.Browser(h.Env.Kavya)
	if r := b.Get(t, wizardFirst); !hasNote(r) {
		t.Error("details step: no required note")
	}
	webtest.WantStatus(t, b.Post(t, wizardFirst, url.Values{"code": {"CEMENT"}, "item_type": {"Goods"}}), http.StatusSeeOther)
	if r := b.Get(t, base+"/new/more"); hasNote(r) {
		t.Error("step of only optional fields shows the required note")
	}
}
