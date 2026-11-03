package purchase_test

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// r13NoTax takes the default tax code off item code.
func r13NoTax(t *testing.T, env *purchasetest.Env, code string) {
	t.Helper()
	if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(), `UPDATE item SET default_tax_code_id = NULL WHERE code = ?`, code)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// R13 item 1 (CD-387 (2)): UoM and Tax code resolve from the item and aren't
// Required. Project keeps the flag DOC's CD-178 check reads (PO-012); its star
// goes at the screen (purchaseweb's detailsBody).
func TestR13_ResolvingFieldsAreNotRequired(t *testing.T) {
	for _, c := range []struct {
		dt       string
		off, req []string
	}{
		{purchase.DocType, []string{"uom_id", "tax_code_id"}, []string{"project_id", "vendor_id", "site_id", "order_date", "item_id", "qty"}},
		{purchase.RequestDocType, []string{"uom_id"}, []string{"project_id", "site_id", "request_date", "item_id", "qty"}},
	} {
		dt, _ := doctype.Lookup(c.dt)
		for _, name := range c.off {
			if f, ok := dt.Field(name); !ok || f.Required {
				t.Errorf("%s.%s: found %v, Required %v, want not Required", c.dt, name, ok, f.Required)
			}
		}
		for _, name := range c.req {
			if f, ok := dt.Field(name); !ok || !f.Required {
				t.Errorf("%s.%s: found %v, Required %v, want Required", c.dt, name, ok, f.Required)
			}
		}
	}
}

// R13 item 1: a PO whose project can't resolve (PUNE-WH has none) saves as a
// draft, and Submit refuses it on project_id; a PO on a site with a project
// stores and submits that project.
func TestR13_OrderSubmitBlankProject(t *testing.T) {
	env := purchasetest.New(t)
	res := p2Must(t, "draft on PUNE-WH")(p2Create(t, env, env.Arjun, p2Header(t, env, "SHREE", "PUNE-WH", ""), p2RefLines(t, env)))
	_, err := p2Do(t, env, env.Arjun, res.ID, doctype.ActionSubmit, "")
	p2WantInvalid(t, "submit, no project", err, doctype.FieldError{Field: "project_id", Message: "Project is required."})
	if got := p2Load(t, env, res.ID).State; got != doctype.StateDraft {
		t.Errorf("state after the refused submit = %s, want draft", got)
	}

	ok := p2Must(t, "draft on METRO-SITE")(p2Create(t, env, env.Arjun, p2Header(t, env, "SHREE", "METRO-SITE", ""), p2RefLines(t, env)))
	if got := p2Plain(p2Load(t, env, ok.ID).Header["project_id"]); got != masterID(t, env.DB, "project", "PRJ-METRO") {
		t.Errorf("resolved project_id = %s", got)
	}
	if _, err := p2Do(t, env, env.Arjun, ok.ID, doctype.ActionSubmit, ""); err != nil {
		t.Errorf("submit with the site's project: %v", err)
	}
}

// R13 item 1: a blank tax code on an item with no default is refused on the
// line, named, with and without reverse charge. Under reverse charge the
// blank line is never looked up (no raw error), and a taxable line beside it
// passes while a Nil-Rated one keeps its own refusal.
func TestR13_OrderSubmitBlankTaxCode(t *testing.T) {
	want := doctype.FieldError{Field: "tax_code_id", Line: 1, Message: "Line 1: Tax code is required."}
	for _, reverse := range []bool{false, true} {
		env := purchasetest.New(t)
		r13NoTax(t, env, "MS-ANGLE-50")
		h := p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO")
		if reverse {
			h["reverse_charge"] = "true"
		}
		line := p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "62.5")
		delete(line, "tax_code_id")
		res, err := p2Create(t, env, env.Priya, h, []map[string]string{line})
		if err != nil {
			var de *doc.Error
			errors.As(err, &de)
			t.Fatalf("draft: %+v", de.Fields)
		}
		_, err = p2Do(t, env, env.Priya, res.ID, doctype.ActionSubmit, "")
		p2WantInvalid(t, "submit, no tax code", err, want)
	}

	env := purchasetest.New(t)
	r13NoTax(t, env, "MS-ANGLE-50")
	h := p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO")
	h["reverse_charge"] = "true"
	blank := p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "62.5")
	delete(blank, "tax_code_id")
	res := p2Must(t, "draft")(p2Create(t, env, env.Priya, h, []map[string]string{blank, p2Line(t, env, "CEMENT-OPC", "BAG", "1", "350", "NIL")}))
	_, err := p2Do(t, env, env.Priya, res.ID, doctype.ActionSubmit, "")
	p2WantInvalid(t, "submit, blank and nil-rated lines", err, want,
		doctype.FieldError{Field: "tax_code_id", Line: 2, Message: "Line 2: Reverse charge needs a taxable tax code on every line."})
}

// R13 item 1: the UoM resolves from the item (an item always has one), so
// DOC's Item error stops a line with neither first; the hook itself still
// names a blank UoM rather than storing it, as MissingOrder does for Check.
func TestR13_BlankUoM(t *testing.T) {
	env := purchasetest.New(t)
	res := p2Must(t, "draft, no item")(p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), []map[string]string{{"qty": "5"}}))
	_, err := p2Do(t, env, env.Priya, res.ID, doctype.ActionSubmit, "")
	p2WantInvalid(t, "submit, no item", err, doctype.FieldError{Field: "item_id", Line: 1, Message: "Line 1: Item is required."})

	want := []doctype.FieldError{
		{Field: "uom_id", Line: 1, Message: "Line 1: UoM is required."},
		{Field: "tax_code_id", Line: 1, Message: "Line 1: Tax code is required."},
	}
	dt, _ := doctype.Lookup(purchase.DocType)
	hookErr := env.DB.Write(t.Context(), func(tx *db.Tx) error { return dt.Hooks.BeforeSubmit(t.Context(), tx, res.ID) })
	r13WantFields(t, "BeforeSubmit", hookErr, want)
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		got, err := purchase.MissingOrder(t.Context(), tx, res.ID)
		if !r13Same(got, want) {
			t.Errorf("MissingOrder = %+v, want %+v", got, want)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// R13 item 1: on a request a project that can't resolve stays DOC's refusal,
// and a line with no item has the Item error, and Unit's from the hook.
func TestR13_RequestBlanks(t *testing.T) {
	env := purchasetest.New(t)
	res, err := p5Create(t, env, env.Suresh, p5Header(t, env, "PUNE-WH", ""), []map[string]string{p5Line(t, env, "MS-ANGLE-50", "", "10")})
	if err != nil {
		t.Fatalf("draft on PUNE-WH: %v", err)
	}
	if got := p5Load(t, env, purchase.RequestDocType, res.ID).Lines[0].Values["uom_id"]; got == nil {
		t.Errorf("the unit didn't resolve from the item")
	}
	_, err = p5Do(t, env, env.Suresh, res.ID, doctype.ActionSubmit, "")
	p5WantInvalid(t, "submit, no project", err, doctype.FieldError{Field: "project_id", Message: "Project is required."})

	noItem, err := p5Create(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"), []map[string]string{{"qty": "5"}})
	if err != nil {
		t.Fatalf("draft, no item: %v", err)
	}
	want := []doctype.FieldError{{Field: "uom_id", Line: 1, Message: "Line 1: Unit is required."}}
	dt, _ := doctype.Lookup(purchase.RequestDocType)
	hookErr := env.DB.Write(t.Context(), func(tx *db.Tx) error { return dt.Hooks.BeforeSubmit(t.Context(), tx, noItem.ID) })
	r13WantFields(t, "BeforeSubmit", hookErr, want)
}

// R13 item 4: RejectedReason is the rejection this draft was revised from,
// through saves, until the next submit; none before a rejection, after a
// resubmit, or after that resubmit is withdrawn.
func TestR13_RejectedReason(t *testing.T) {
	env := purchasetest.New(t)
	reason := func(id int64) string {
		t.Helper()
		var s string
		if err := env.DB.Read(t.Context(), func(tx *db.Tx) (err error) {
			s, err = purchase.RejectedReason(t.Context(), tx, id)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return s
	}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(map[string]string{"qty": "1000"}))
	if got := reason(po.ID); got != "" {
		t.Errorf("never rejected: %q", got)
	}
	p2Must(t, "submit")(p2Do(t, env, env.Priya, po.ID, doctype.ActionSubmit, ""))
	if got := reason(po.ID); got != "" {
		t.Errorf("pending: %q", got)
	}
	p2Must(t, "reject first")(p2Do(t, env, env.Ravi, po.ID, doctype.ActionReject, "First reason"))
	p2Must(t, "revise")(p2Do(t, env, env.Priya, po.ID, doctype.ActionRevise, ""))
	p2Must(t, "submit again")(p2Do(t, env, env.Priya, po.ID, doctype.ActionSubmit, ""))
	p2Must(t, "reject again")(p2Do(t, env, env.Ravi, po.ID, doctype.ActionReject, "Second reason"))
	if got := reason(po.ID); got != "Second reason" {
		t.Errorf("rejected = %q, want the newest", got)
	}
	p2Must(t, "revise again")(p2Do(t, env, env.Priya, po.ID, doctype.ActionRevise, ""))
	p2Must(t, "save")(p2Save(t, env, env.Priya, po.ID, map[string]string{"remarks": "Asked the vendor again"}, p2KeepLines(t, env, po.ID)))
	if got := reason(po.ID); got != "Second reason" {
		t.Errorf("revised and saved = %q, want the reason", got)
	}
	p2Must(t, "resubmit")(p2Do(t, env, env.Priya, po.ID, doctype.ActionSubmit, ""))
	if got := reason(po.ID); got != "" {
		t.Errorf("resubmitted: %q", got)
	}
	p2Must(t, "withdraw")(p2Do(t, env, env.Priya, po.ID, doctype.ActionWithdraw, ""))
	if got := reason(po.ID); got != "" {
		t.Errorf("withdrawn after the resubmit: %q", got)
	}
}

// r13WantFields checks err is doc.Invalid with exactly want.
func r13WantFields(t *testing.T, what string, err error, want []doctype.FieldError) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) {
		t.Fatalf("%s: %v, want field errors", what, err)
	}
	if !r13Same(de.Fields, want) {
		t.Errorf("%s: %+v, want %+v", what, de.Fields, want)
	}
}

func r13Same(a, b []doctype.FieldError) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
