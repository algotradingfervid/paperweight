package expenseweb_test

import (
	"strconv"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// PH (AD-11, CD-370, UI v2 §5.5 item 5): the expense fields' example
// placeholders reach the inputs drawn on the real pages. A Link's example is
// on its Picker's search input, which holds the field's id while nothing is
// chosen (components/picker.go).

// phWant checks that the input with field name's id in s shows placeholder
// want, for each name.
func phWant(t *testing.T, what string, s *goquery.Selection, want map[string]string) {
	t.Helper()
	for name, ph := range want {
		in := s.Find("input#" + components.FieldID(name))
		if in.Length() != 1 {
			t.Errorf("%s: %d inputs with %s's id, want 1", what, in.Length(), name)
			continue
		}
		if got, _ := in.Attr("placeholder"); got != ph {
			t.Errorf("%s: %s placeholder %q, want %q", what, name, got, ph)
		}
	}
}

// Claim details: Purpose for everyone; an Administrator's hand-built
// employee code box shows its own example.
func TestPH_ClaimDetails(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	path := s2ClaimPath(c.ID) + "/edit/details"
	phWant(t, "employee", e.page(t, s2Deepak, path).Selection, map[string]string{
		"purpose": "e.g. Pune Metro site visit",
	})
	// A new claim: its code box is blank (a stored one needs Deps.Employee).
	phWant(t, "admin", e.page(t, "admin@vardhan.test", "/doc/"+expense.ClaimDocType+"/new").Selection, map[string]string{
		"purpose":     "e.g. Pune Metro site visit",
		"employee_id": "e.g. EMP-007",
	})
}

// The claim line page: Note, Project and Site through FieldInput; Supplier
// GSTIN, Invoice no. and HSN/SAC built by the page. Type is tiles and
// Billed to radios: neither takes a placeholder.
func TestPH_ClaimLine(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	form := s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/new"))
	phWant(t, "claim line", form, map[string]string{
		"description":    "e.g. Auto from Pune station to site",
		"project_id":     "e.g. Pune Metro or PRJ-METRO",
		"site_id":        "e.g. Metro site or METRO-SITE",
		"supplier_gstin": "e.g. 27AABCS1429B1ZU",
		"invoice_no":     "e.g. INV-901",
		"hsn_sac":        "e.g. 996411",
	})
	if n := form.Find(`[name="expense_type_id"][placeholder], [name="billed_to_id"][placeholder]`).Length(); n != 0 {
		t.Errorf("%d Type or Billed to controls carry a placeholder", n)
	}
}

// Advance details (docscreen's generic step): Purpose.
func TestPH_AdvanceDetails(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	adv := e.DraftAdvance(t, e.P(t, s2Deepak), nil)
	page := e.page(t, s2Deepak, "/doc/"+expense.AdvanceDocType+"/"+strconv.FormatInt(adv.ID, 10)+"/edit/details")
	phWant(t, "advance details", page.Selection, map[string]string{
		"purpose": "e.g. Nagpur site visit, 3 days",
	})
}

// The advance line page (docscreen's generic one): Type, Note, Project and
// Site.
func TestPH_AdvanceLine(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	adv := e.DraftAdvance(t, e.P(t, s2Deepak), nil)
	page := e.page(t, s2Deepak, "/doc/"+expense.AdvanceDocType+"/"+strconv.FormatInt(adv.ID, 10)+"/lines/new")
	phWant(t, "advance line", page.Selection, map[string]string{
		"expense_type_id": "e.g. Travel or TRAVEL",
		"description":     "e.g. Bus fare and lodging",
		"project_id":      "e.g. Pune Metro or PRJ-METRO",
		"site_id":         "e.g. Metro site or METRO-SITE",
	})
}

// The claim's Type and Billed to are declared with their examples, though
// the claim line page draws both as radios, which show none: a Picker
// drawing either later (docscreen's generic line page) shows them.
func TestPH_ClaimRadiosDeclared(t *testing.T) {
	t.Parallel()
	dt, _ := doctype.Lookup(expense.ClaimDocType)
	for name, want := range map[string]string{
		"expense_type_id": "e.g. Travel or TRAVEL",
		"billed_to_id":    "e.g. Pune office or PUNE",
	} {
		if f, _ := dt.Field(name); f.Placeholder != want {
			t.Errorf("%s Placeholder %q, want %q", name, f.Placeholder, want)
		}
	}
}
