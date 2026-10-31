package purchase_test

import (
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// CD-299 mutation pass on P2's queries (queries/order_validate.sql); CD-302:
// Validate never writes another record's data. Each test below kills a
// widened WHERE or a flipped ORDER BY that the other P2 tests let through.

// p2ScopeHeaderCols are the purchase_order columns Validate writes on a draft
// (SetOrderVendor, SetOrderSite, SetOrderTotals), plus version.
var p2ScopeHeaderCols = []string{
	"vendor_address_id", "payment_terms_id", "snap_vendor_id", "snap_vendor_code", "snap_vendor_name",
	"snap_vendor_gstin", "snap_vendor_category", "snap_vendor_state", "snap_vendor_address", "snap_terms_name",
	"project_id", "branch_id", "snap_site_name", "snap_branch_name", "snap_branch_gstin", "snap_place_state",
	"terms", "net_total", "cgst_total", "sgst_total", "igst_total", "grand_total", "rounded_total", "receipt_status",
	"version",
}

// p2ScopeLineCols are the purchase_order_line columns SetOrderLineValidated
// writes, plus id and line_no.
var p2ScopeLineCols = []string{
	"id", "line_no", "description", "hsn_sac", "uom_id", "tax_code_id", "price", "discount_pct", "already_received",
	"rate", "amount", "cgst", "sgst", "igst", "snap_item_code", "snap_item_name", "snap_uom_code", "snap_factor",
	"snap_stock_qty", "snap_gst_rate", "snap_tax_code",
}

// p2ScopeUnchanged reports any header or line column of order id that differs
// from before.
func p2ScopeUnchanged(t *testing.T, what string, env *purchasetest.Env, id int64, header map[string]string, lines []map[string]string) {
	t.Helper()
	if got := p2Snap(t, env, id, p2ScopeHeaderCols...); !maps.Equal(got, header) {
		t.Errorf("%s: other order %d's header = %v, want %v unchanged", what, id, got, header)
	}
	got := p2LineSnap(t, env, id, p2ScopeLineCols...)
	if !slices.EqualFunc(got, lines, maps.Equal) {
		t.Errorf("%s: other order %d's lines = %v, want %v unchanged", what, id, got, lines)
	}
}

// p2ScopeAmount is purchase.AmountOrder for order id, formatted.
func p2ScopeAmount(t *testing.T, env *purchasetest.Env, id int64) string {
	t.Helper()
	var m types.Money
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		m, err = purchase.AmountOrder(t.Context(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("AmountOrder(%d): %v", id, err)
	}
	return types.FormatMoney(m)
}

// CD-302, CD-299 (OrderForValidate, OrderLinesForValidate, SetOrderVendor,
// SetOrderSite, SetOrderTotals, SetOrderLineValidated and OrderAmount filter
// on this PO): two draft POs with different vendors, sites, terms and lines.
// Saving either one leaves every column Validate writes on the other
// unchanged; the saved one's snapshots, totals and lines come from its own
// header and lines; OrderWarnings and the Amount hook read only their PO.
// B is created second, so a widened :one query (which returns the first
// row) would read A when asked for B.
func TestPO_ValidateWritesOnlyThisOrder(t *testing.T) {
	env := purchasetest.New(t)

	// A: SHREE, METRO-SITE, the reference lines, line 1 already received
	// above its qty (a warning), its own terms.
	ha := p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO")
	ha["earlier_po_no"], ha["terms"] = "P-17", "A's own terms"
	la := p2RefLines(t, env)
	la[0]["already_received"] = "1300"
	a := p2Must(t, "create A")(p2Create(t, env, env.Arjun, ha, la))

	// B: BHARAT, TECH-SITE, one WIRE-18G line, blank terms.
	hb := p2Header(t, env, "BHARAT", "TECH-SITE", "PRJ-TECHPARK")
	b := p2Must(t, "create B")(p2Create(t, env, env.Arjun, hb, []map[string]string{p2Line(t, env, "WIRE-18G", "KGS", "10", "96")}))

	aHeader, aLines := p2Snap(t, env, a.ID, p2ScopeHeaderCols...), p2LineSnap(t, env, a.ID, p2ScopeLineCols...)
	if aHeader["snap_vendor_code"] != "SHREE" || aHeader["terms"] != "A's own terms" || aHeader["receipt_status"] != "part_received" || len(aLines) != 2 {
		t.Fatalf("A after creating B: header %v, %d lines; want SHREE, A's own terms, part_received, 2 lines", aHeader, len(aLines))
	}

	// Save B: qty 10 -> 20.
	p2Must(t, "save B")(p2Save(t, env, env.Arjun, b.ID, map[string]string{"remarks": "Twenty"},
		[]map[string]string{p2Line(t, env, "WIRE-18G", "KGS", "20", "96")}))
	p2ScopeUnchanged(t, "after saving B", env, a.ID, aHeader, aLines)

	wantB := map[string]string{
		"snap_vendor_code": "BHARAT", "project_id": masterID(t, env.DB, "project", "PRJ-TECHPARK"),
		"snap_site_name": p2ScopeSiteName(t, env, "TECH-SITE"), "terms": "",
		"net_total": "192000", "receipt_status": "not_received",
	}
	if got := p2Snap(t, env, b.ID, slices.Sorted(maps.Keys(wantB))...); !maps.Equal(got, wantB) {
		t.Errorf("B after its save: %v, want %v (its own vendor, site, terms and lines)", got, wantB)
	}
	bLines := p2LineSnap(t, env, b.ID, "line_no", "snap_item_code", "amount")
	if want := []map[string]string{{"line_no": "1", "snap_item_code": "WIRE-18G", "amount": "192000"}}; !slices.EqualFunc(bLines, want, maps.Equal) {
		t.Errorf("B's lines = %v, want %v", bLines, want)
	}

	// Warnings and the Amount hook read only their own PO.
	if got := p2Warnings(t, env, env.Arjun, b.ID); len(got) != 0 {
		t.Errorf("OrderWarnings(B) = %q, want none (A's line 1 warning is A's)", got)
	}
	if got, want := p2Warnings(t, env, env.Arjun, a.ID), []string{"Line 1: Already received is more than the quantity."}; !slices.Equal(got, want) {
		t.Errorf("OrderWarnings(A) = %q, want %q", got, want)
	}
	if got := p2ScopeAmount(t, env, b.ID); got != "2,266.00" {
		t.Errorf("AmountOrder(B) = %s, want 2,266.00 (B's rounded total, not A's)", got)
	}
	if got := p2ScopeAmount(t, env, a.ID); got != "94,164.00" {
		t.Errorf("AmountOrder(A) = %s, want 94,164.00", got)
	}

	// The other way round: saving A leaves B unchanged.
	bHeader, bAll := p2Snap(t, env, b.ID, p2ScopeHeaderCols...), p2LineSnap(t, env, b.ID, p2ScopeLineCols...)
	p2Must(t, "save A")(p2Save(t, env, env.Arjun, a.ID, map[string]string{"remarks": "Recheck"}, nil))
	p2ScopeUnchanged(t, "after saving A", env, b.ID, bHeader, bAll)
}

// p2ScopeSiteName is site code's name as the site master holds it.
func p2ScopeSiteName(t *testing.T, env *purchasetest.Env, code string) string {
	t.Helper()
	var name string
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT name FROM site WHERE code = ?`, code).Scan(&name); err != nil {
		t.Fatalf("name of site %s: %v", code, err)
	}
	return name
}

// p2ScopeFields is err's field errors, failing the test when err isn't DOC's
// invalid error.
func p2ScopeFields(t *testing.T, what string, err error) []doctype.FieldError {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) || de.Kind != doc.KindInvalid {
		t.Fatalf("%s: err = %v, want an invalid error", what, err)
	}
	return de.Fields
}

// CD-299 (OrderLinesForValidate's ORDER BY line_no): DOC appends Validate's
// errors after its own sort (doc/save.go), so Validate itself names the
// lines in line order.
func TestPO_ValidateErrorsInLineOrder(t *testing.T) {
	env := purchasetest.New(t)
	_, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), []map[string]string{
		p2Line(t, env, "MS-ANGLE-50", "BOX", "10", "62.5"),
		p2Line(t, env, "WIRE-18G", "BOX", "5", "96"),
	})
	want := []doctype.FieldError{
		{Field: "uom_id", Line: 1, Message: "Line 1: Item MS-ANGLE-50 has no conversion for UoM BOX. Pick another UoM."},
		{Field: "uom_id", Line: 2, Message: "Line 2: Item WIRE-18G has no conversion for UoM BOX. Pick another UoM."},
	}
	if got := p2ScopeFields(t, "two lines without a conversion", err); !slices.Equal(got, want) {
		t.Errorf("field errors = %+v, want %+v in line order", got, want)
	}
}

// CD-299 (OrderLinesForValidate's ORDER BY line_no, BeforeSubmit's path):
// doc.Invalid keeps BeforeSubmit's order, so it names the lines in line
// order.
func TestPO_BeforeSubmitErrorsInLineOrder(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(),
		purchasetest.OrderHeader(map[string]string{"reverse_charge": "true"}),
		purchasetest.OrderLines(
			p2Line(t, env, "MS-ANGLE-50", "KGS", "1200", "62.5", "NIL"),
			p2Line(t, env, "WIRE-18G", "KGS", "50", "96", "NIL"),
		))
	_, err := p2Do(t, env, env.Priya, po.ID, doc.ActionSubmit, "")
	want := []doctype.FieldError{
		{Field: "tax_code_id", Line: 1, Message: "Line 1: Reverse charge needs a taxable tax code on every line."},
		{Field: "tax_code_id", Line: 2, Message: "Line 2: Reverse charge needs a taxable tax code on every line."},
	}
	if got := p2ScopeFields(t, "submit with two NIL lines", err); !slices.Equal(got, want) {
		t.Errorf("field errors = %+v, want %+v in line order", got, want)
	}
}
