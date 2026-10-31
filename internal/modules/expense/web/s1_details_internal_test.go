package expenseweb

import (
	"bytes"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// Claim details honours the registered Module.Hide for every field it draws
// or saves (CD-319, hooks spec §4.3): a hidden field is never drawn, even
// with an error on it, and never saved, even when posted. Each field is
// hidden in turn; the others stay drawn and saved, so the check bites.
func TestDetails_HonoursHide(t *testing.T) {
	e := expensetest.New(t)
	d := Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files}
	dt, ok := doctype.Lookup(expense.ClaimDocType)
	if !ok {
		t.Fatal("expense.Claim isn't registered")
	}
	all := []string{"purpose", "claim_date", "remarks", "employee_id", "advance_id", "less_advance"}
	for _, hidden := range all {
		form := url.Values{
			"purpose": {"Site visit"}, "claim_date": {"24-10-2026"}, "remarks": {"Note"},
			"employee_id": {""}, "advance_id": {""}, "less_advance": {"10.00"}, advanceDrawn: {"1"},
		}
		req := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		hide := func(_ docscreen.Ctx, f doctype.Field) bool { return f.Name == hidden }
		c := docscreen.Ctx{
			Req: req, Principal: e.P(t, "admin@vardhan.test"), DT: dt,
			Deps: docscreen.Deps{DB: e.DB, Modules: map[string]docscreen.Module{expense.ClaimDocType: {Hide: hide}}},
		}
		var errs []doctype.FieldError
		for _, name := range all {
			errs = append(errs, doctype.FieldError{Field: name, Message: "Wrong " + name + "."})
		}
		body, err := d.detailsBody(c, docscreen.StepState{Posted: true, Errors: errs})
		if err != nil {
			t.Fatalf("hide %s: detailsBody: %v", hidden, err)
		}
		var buf bytes.Buffer
		if err := body.Render(t.Context(), &buf); err != nil {
			t.Fatal(err)
		}
		page, err := goquery.NewDocumentFromReader(&buf)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range all {
			drawn := page.Find(`[name="`+name+`"]`).Length() > 0 || page.Find("#"+components.FieldID(name)).Length() > 0
			if drawn != (name != hidden) {
				t.Errorf("hide %s: %s drawn = %v, want %v", hidden, name, drawn, name != hidden)
			}
			if shown := strings.Contains(page.Text(), "Wrong "+name+"."); shown != (name != hidden) {
				t.Errorf("hide %s: %s's error shown = %v, want %v", hidden, name, shown, name != hidden)
			}
		}
		s, err := d.detailsSave(c)
		if err != nil {
			t.Fatalf("hide %s: detailsSave: %v", hidden, err)
		}
		for _, name := range all {
			if _, saved := s.Values[name]; saved != (name != hidden) {
				t.Errorf("hide %s: %s in Save.Values = %v, want %v (%v)", hidden, name, saved, name != hidden, s.Values)
			}
		}
	}
}

// The advance disclosure draws only what the module leaves shown: with
// advance_id and less_advance both hidden, nothing of it is drawn, even for
// an employee with a paid advance; an error on a hidden advance field alone
// draws no input either.
func TestDetails_HideAdvance(t *testing.T) {
	e := expensetest.New(t)
	d := Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files}
	dt, _ := doctype.Lookup(expense.ClaimDocType)
	deepak, admin := e.P(t, "deepak@vardhan.test"), e.P(t, "admin@vardhan.test")
	e.PaidAdvance(t, deepak, 100000)
	for _, tc := range []struct {
		name   string
		p      auth.Principal
		hide   []string
		errors []string
	}{
		{"both hidden, a paid advance", deepak, []string{"advance_id", "less_advance"}, nil},
		{"advance_id hidden, its error, no advance", admin, []string{"advance_id"}, []string{"advance_id"}},
		{"less_advance hidden, its error, no advance", admin, []string{"less_advance"}, []string{"less_advance"}},
	} {
		hide := func(_ docscreen.Ctx, f doctype.Field) bool { return slices.Contains(tc.hide, f.Name) }
		c := docscreen.Ctx{
			Req: httptest.NewRequest("GET", "/", nil), Principal: tc.p, DT: dt,
			Deps: docscreen.Deps{DB: e.DB, Modules: map[string]docscreen.Module{expense.ClaimDocType: {Hide: hide}}},
		}
		var errs []doctype.FieldError
		for _, name := range tc.errors {
			errs = append(errs, doctype.FieldError{Field: name, Message: "Wrong."})
		}
		body, err := d.detailsBody(c, docscreen.StepState{Errors: errs})
		if err != nil {
			t.Fatalf("%s: detailsBody: %v", tc.name, err)
		}
		var buf bytes.Buffer
		if err := body.Render(t.Context(), &buf); err != nil {
			t.Fatal(err)
		}
		page, err := goquery.NewDocumentFromReader(&buf)
		if err != nil {
			t.Fatal(err)
		}
		for _, sel := range []string{"details", `[name="` + advanceDrawn + `"]`, `[name="advance_id"]`, `[name="less_advance"]`} {
			if page.Find(sel).Length() > 0 {
				t.Errorf("%s: %s drawn, want nothing of the advance", tc.name, sel)
			}
		}
	}
}
