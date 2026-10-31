package purchase_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// MOD-PO P5 "uom.CheckLineQty" (PO spec §20.2 lines; CD-278): a request line
// in a unit its item has no conversion for is refused with the PO's
// conversion text, writing nothing, instead of failing the save with an
// internal error; the item's own unit saves.
func TestRequest_NoConversion(t *testing.T) {
	env := purchasetest.New(t)
	before := p5CountAll(t, env)
	_, err := p5Create(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"),
		[]map[string]string{p5Line(t, env, "MS-ANGLE-50", "NOS", "10")})
	p5WantInvalid(t, "MS-ANGLE-50 in NOS", err, doctype.FieldError{Field: "uom_id", Line: 1,
		Message: "Line 1: Item MS-ANGLE-50 has no conversion for UoM NOS. Pick another UoM."})
	p5SameCounts(t, "MS-ANGLE-50 in NOS", before, env)
	if _, err := p5Create(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"),
		[]map[string]string{p5Line(t, env, "MS-ANGLE-50", "KGS", "10")}); err != nil {
		t.Errorf("MS-ANGLE-50 in KGS: %v %+v, want saved", err, p5Fields(err))
	}
}

// MOD-PO P5 (CD-178: Required is checked on submit, not on draft saves): a
// draft with no request date and a line with no item yet saves, and the
// date and line rules skip what is blank; submit then reports both as
// required.
func TestRequest_BlankDraftFields(t *testing.T) {
	env := purchasetest.New(t)
	h := p5Header(t, env, "METRO-SITE", "PRJ-METRO")
	h["request_date"], h["required_by"] = "", "2026-10-30"
	res, err := p5Create(t, env, env.Suresh, h, []map[string]string{{"qty": "5"}})
	if err != nil {
		t.Fatalf("draft with no request date and no item: %v %+v, want saved", err, p5Fields(err))
	}
	d := p5Load(t, env, purchase.RequestDocType, res.ID)
	if len(d.Lines) != 1 || p5Plain(d.Lines[0].Values["item_id"]) != "<nil>" || p5Plain(d.Lines[0].Values["uom_id"]) != "<nil>" {
		t.Errorf("stored lines %+v, want one line with no item and no unit", d.Lines)
	}
	if _, err := p5Do(t, env, env.Suresh, res.ID, doctype.ActionSubmit, ""); err == nil {
		t.Errorf("submit with no request date and no item: allowed, want refused (Required)")
	}
}
