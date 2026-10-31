package purchase_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MOD-PO P2 fix 2 (CD-302, AD-13; CD-205, CD-207; PO spec §15 item 8): on a
// draft, Validate's writes are always computed from the current inputs or
// cleared, never stale. A step save (ActionInput.Only) commits with the
// out-of-scope errors as Result.Problems, and amend keeps Validate's writes
// and ignores its errors, so each part is tested on both paths: the stored
// value before is non-blank and differs from the expected one, and after it
// is the newly computed value or cleared. Every test also checks that the
// header totals are the sum of the stored lines.

// p2VendorCols are the vendor part's columns: the address and the snapshots.
var p2VendorCols = []string{"vendor_address_id", "snap_vendor_id", "snap_vendor_code", "snap_vendor_name", "snap_vendor_gstin",
	"snap_vendor_category", "snap_vendor_state", "snap_vendor_address"}

// p2LineMoney are the line columns Validate computes from the amount.
var p2LineMoney = []string{"amount", "cgst", "sgst", "igst"}

// p2Step is a step save of draft id as p at its stored version: only scopes
// the errors (CD-205). nil lines keep the stored lines (p2KeepLines).
func p2Step(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64, only []string, values map[string]string, lines []map[string]string) (doc.Result, error) {
	t.Helper()
	if lines == nil {
		lines = p2KeepLines(t, env, id)
	}
	return env.Engine.Do(t.Context(), p, purchase.DocType, id, doc.ActionSave,
		doc.ActionInput{Version: p2Version(t, env, id), Values: values, Lines: lines, Only: only})
}

// p2WantProblems asserts the step save committed (err nil) and its Problems
// include each of want (a want Field of "a|b" accepts either name): the
// failing check really ran and was out of scope.
func p2WantProblems(t *testing.T, what string, res doc.Result, err error, want ...doctype.FieldError) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v, want the save committed with problems %+v", what, err, want)
	}
	for _, w := range want {
		if !slices.ContainsFunc(res.Problems, func(g doctype.FieldError) bool {
			return g.Line == w.Line && g.Message == w.Message && slices.Contains(strings.Split(w.Field, "|"), g.Field)
		}) {
			t.Errorf("%s: problems %+v lack %+v", what, res.Problems, w)
		}
	}
}

// p2Cleared reports a column read by p2Snap or p2LineSnap that is NULL or 0.
func p2Cleared(v string) bool { return v == "" || v == "0" }

// p2HeaderIsSum asserts the header totals of order id equal the sums of its
// stored lines: net_total the amounts, each tax total its line column, and
// grand_total the net plus the tax (the net alone under reverse charge).
func p2HeaderIsSum(t *testing.T, what string, env *purchasetest.Env, id int64) {
	t.Helper()
	var net, cgst, sgst, igst, hNet, hCgst, hSgst, hIgst, grand, rc int64
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT
		COALESCE((SELECT SUM(amount) FROM purchase_order_line WHERE parent_id = o.id), 0),
		COALESCE((SELECT SUM(cgst) FROM purchase_order_line WHERE parent_id = o.id), 0),
		COALESCE((SELECT SUM(sgst) FROM purchase_order_line WHERE parent_id = o.id), 0),
		COALESCE((SELECT SUM(igst) FROM purchase_order_line WHERE parent_id = o.id), 0),
		net_total, cgst_total, sgst_total, igst_total, grand_total, reverse_charge
		FROM purchase_order o WHERE id = ?`, id).Scan(&net, &cgst, &sgst, &igst, &hNet, &hCgst, &hSgst, &hIgst, &grand, &rc); err != nil {
		t.Fatalf("%s: read totals of order %d: %v", what, id, err)
	}
	if hNet != net || hCgst != cgst || hSgst != sgst || hIgst != igst {
		t.Errorf("%s: header net/cgst/sgst/igst = %d/%d/%d/%d, want the lines' sums %d/%d/%d/%d", what, hNet, hCgst, hSgst, hIgst, net, cgst, sgst, igst)
	}
	want := net + cgst + sgst + igst
	if rc != 0 {
		want = net
	}
	if grand != want {
		t.Errorf("%s: grand_total %d, want %d from the lines", what, grand, want)
	}
}

// p2AllCleared asserts each of cols in got is cleared.
func p2AllCleared(t *testing.T, what string, got map[string]string, cols ...string) {
	t.Helper()
	for _, c := range cols {
		if !p2Cleared(got[c]) {
			t.Errorf("%s: %s = %q, want cleared", what, c, got[c])
		}
	}
}

// p2VendorBefore asserts got is code's vendor part, the state each clear
// test starts from: code's primary billing address and snapshots, non-blank
// in every column but blankOK.
func p2VendorBefore(t *testing.T, what string, env *purchasetest.Env, got map[string]string, code string, blankOK ...string) {
	t.Helper()
	if got["vendor_address_id"] != p2PrimaryBilling(t, env, code) || got["snap_vendor_code"] != code {
		t.Fatalf("%s: vendor part %v, want %s's", what, got, code)
	}
	for _, c := range p2VendorCols {
		if got[c] == "" && !slices.Contains(blankOK, c) {
			t.Fatalf("%s: %s blank, want %s's value before the save", what, c, code)
		}
	}
}

// p2SHREEVendor is p2VendorBefore for SHREE (registered, so nothing blank).
func p2SHREEVendor(t *testing.T, what string, env *purchasetest.Env, got map[string]string) {
	t.Helper()
	p2VendorBefore(t, what, env, got, "SHREE")
}

// p2GlobalVendor creates GLOBAL (Global Metals), an unregistered vendor with
// a Pune address, which Validate accepts until it is made overseas: SHREE
// can't be, its GSTIN doesn't match the category.
func p2GlobalVendor(t *testing.T, env *purchasetest.Env) map[string]string {
	t.Helper()
	ctx := t.Context()
	saved, err := party.New(p2Kit(env)).Create(ctx, env.Admin, party.Input{Code: "GLOBAL", Name: "Global Metals", IsVendor: true, GSTCategory: gstin.Unregistered})
	if err != nil {
		t.Fatalf("create vendor GLOBAL: %v", err)
	}
	if _, err := address.New(p2Kit(env)).Create(ctx, env.Admin, address.Input{PartyID: saved.ID, Title: "Pune", Line1: "Bhosari MIDC",
		City: "Pune", StateCode: "27", Country: "India", Pincode: "411026", IsBilling: true, PrimaryBilling: true}); err != nil {
		t.Fatalf("create GLOBAL's address: %v", err)
	}
	return map[string]string{"vendor_id": strconv.FormatInt(saved.ID, 10)}
}

// Test 1 (CD-302 vendor part, NUM-038): a step save of the main step (Only
// vendor_id) switches a SHREE draft to NOADDR, a vendor with no GSTIN and no
// address. It commits with NUM-038 as a problem (vendor_address_id is on the
// terms step), and Validate writes NOADDR's computed part: no address, and
// NOADDR's snapshots. Nothing of SHREE's is left.
func TestPO_StepSaveVendorNoState(t *testing.T) {
	env := purchasetest.New(t)
	if _, err := party.New(p2Kit(env)).Create(t.Context(), env.Admin, party.Input{Code: "NOADDR", Name: "Sai Fabricators", IsVendor: true}); err != nil {
		t.Fatalf("create vendor NOADDR: %v", err)
	}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2RefLines(t, env)...))
	p2SHREEVendor(t, "SHREE draft", env, p2Snap(t, env, po.ID, p2VendorCols...))

	noaddr := masterID(t, env.DB, "party", "NOADDR")
	res, err := p2Step(t, env, env.Priya, po.ID, []string{"vendor_id"}, map[string]string{"vendor_id": noaddr}, nil)
	p2WantProblems(t, "switch to NOADDR", res, err, doctype.FieldError{Field: "vendor_address_id|vendor_id",
		Message: "Set the state on the supplier's address so GST can be worked out."})

	got := p2Snap(t, env, po.ID, p2VendorCols...)
	if got["vendor_address_id"] != "" {
		t.Errorf("vendor_address_id = %q after the switch, want cleared (NOADDR has no address; SHREE's was %s)", got["vendor_address_id"], p2PrimaryBilling(t, env, "SHREE"))
	}
	for c, want := range map[string]string{"snap_vendor_id": noaddr, "snap_vendor_code": "NOADDR", "snap_vendor_name": "Sai Fabricators",
		"snap_vendor_gstin": "", "snap_vendor_state": "", "snap_vendor_address": ""} {
		if got[c] != want {
			t.Errorf("%s = %q after the switch to NOADDR, want %q (computed for NOADDR, never SHREE's)", c, got[c], want)
		}
	}
	p2HeaderIsSum(t, "after the switch to NOADDR", env, po.ID)
}

// Test 2, step save (CD-302 vendor part, PO spec §4 overseas): GLOBAL
// becomes overseas after the draft was saved (unticking Vendor is refused while a
// document uses the party, party.checkRolesInUse, so overseas is the vendor
// refusal a stored vendor can reach). A terms-step save (Only remarks)
// commits with the overseas error as a problem, and the vendor snapshots and
// vendor_address_id are cleared; vendor_id stays as the user set it.
func TestPO_StepSaveOverseasClearsVendor(t *testing.T) {
	env := purchasetest.New(t)
	global := p2GlobalVendor(t, env)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderHeader(global), purchasetest.OrderLines(p2RefLines(t, env)...))
	p2VendorBefore(t, "GLOBAL draft", env, p2Snap(t, env, po.ID, p2VendorCols...), "GLOBAL", "snap_vendor_gstin")
	p2SetCategory(t, env, "GLOBAL", gstin.Overseas)

	res, err := p2Step(t, env, env.Priya, po.ID, []string{"remarks"}, map[string]string{"remarks": "Recheck vendor"}, nil)
	p2WantProblems(t, "terms save, GLOBAL overseas", res, err, doctype.FieldError{Field: "vendor_id",
		Message: "Overseas vendors aren't supported on purchase orders yet."})
	p2AllCleared(t, "GLOBAL overseas, step save", p2Snap(t, env, po.ID, p2VendorCols...), p2VendorCols...)
	if got := p2Plain(p2Load(t, env, po.ID).Header["vendor_id"]); got != global["vendor_id"] {
		t.Errorf("vendor_id = %s, want GLOBAL kept (the user's input)", got)
	}
	p2HeaderIsSum(t, "GLOBAL overseas, step save", env, po.ID)
}

// Test 2, amend (CD-302 vendor part, PO spec §15 item 8): an approved GLOBAL
// PO is cancelled, GLOBAL becomes overseas, and the PO is amended. Amend copies
// GLOBAL's address to the new draft; Validate's overseas refusal (ignored by
// amend) clears it and the vendor snapshots.
func TestPO_AmendOverseasClearsVendor(t *testing.T) {
	env := purchasetest.New(t)
	global := p2GlobalVendor(t, env)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(), purchasetest.OrderHeader(global), purchasetest.OrderLines(p2RefLines(t, env)...))
	p2VendorBefore(t, "approved GLOBAL PO", env, p2Snap(t, env, po.ID, p2VendorCols...), "GLOBAL", "snap_vendor_gstin")
	p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, po.ID, doc.ActionCancel, "Vendor gone overseas"))
	p2SetCategory(t, env, "GLOBAL", gstin.Overseas)

	res := p2Must(t, "Priya amends")(p2Do(t, env, env.Priya, po.ID, doc.ActionAmend, ""))
	if res.ID == po.ID || res.State != doctype.StateDraft {
		t.Fatalf("amend = %+v, want a new draft", res)
	}
	p2AllCleared(t, "amended draft, GLOBAL overseas", p2Snap(t, env, res.ID, p2VendorCols...), p2VendorCols...)
	// The refusal really happens on the new draft.
	_, err := p2Save(t, env, env.Priya, res.ID, map[string]string{"remarks": "Check"}, nil)
	p2WantInvalid(t, "save the amended draft", err, doctype.FieldError{Field: "vendor_id", Message: "Overseas vendors aren't supported on purchase orders yet."})
	p2HeaderIsSum(t, "amended draft, GLOBAL overseas", env, res.ID)
}

// Test 2, role (CD-302 vendor part, MST-042): the role refusal can't be
// reached by a stored vendor (unticking Vendor is refused while a document
// uses the party) nor by a real step save (vendor_id is on the main step, so
// its error is in scope and refuses). Engine.Do still accepts a save that
// posts vendor_id with Only naming another field (Only scopes errors, not
// values), so Validate's write on that path is pinned here: the role error is
// a problem, and the vendor part is cleared, never SHREE's.
func TestPO_StepSaveRoleRefusedClearsVendor(t *testing.T) {
	env := purchasetest.New(t)
	if _, err := party.New(p2Kit(env)).Create(t.Context(), env.Admin, party.Input{Code: "ACME", Name: "Acme Builders", IsCustomer: true}); err != nil {
		t.Fatalf("create customer ACME: %v", err)
	}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2RefLines(t, env)...))
	p2SHREEVendor(t, "SHREE draft", env, p2Snap(t, env, po.ID, p2VendorCols...))

	res, err := p2Step(t, env, env.Priya, po.ID, []string{"remarks"},
		map[string]string{"vendor_id": masterID(t, env.DB, "party", "ACME"), "remarks": "Customer by mistake"}, nil)
	p2WantProblems(t, "ACME posted outside Only", res, err, doctype.FieldError{Field: "vendor_id",
		Message: "Acme Builders is not marked as a vendor. Tick Vendor on the party first."})
	p2AllCleared(t, "ACME, role refused", p2Snap(t, env, po.ID, p2VendorCols...), p2VendorCols...)
	p2HeaderIsSum(t, "ACME, role refused", env, po.ID)
}

// p2BoltLines are a valid MS-ANGLE-50 line (10 kg at ₹62.50) and a BOLT-M12
// line of 2 boxes at ₹1,200.00, both 18%.
func p2BoltLines(t *testing.T, env *purchasetest.Env) []map[string]string {
	t.Helper()
	return []map[string]string{p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "62.5"), p2Line(t, env, "BOLT-M12", "BOX", "2", "1200")}
}

// p2DropBox removes BOLT-M12's Box conversion through the item master, so
// its stored Box line fails CD-278's check without changing.
func p2DropBox(t *testing.T, env *purchasetest.Env) {
	t.Helper()
	ctx := t.Context()
	items := item.New(p2Kit(env))
	var it item.Item
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		it, err = items.Get(ctx, tx, p2Int(t, masterID(t, env.DB, "item", "BOLT-M12")))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := items.Update(ctx, env.Admin, it.ID, it.Version, item.Input{Code: it.Code, Name: it.Name, Type: it.Type, HSNSAC: it.HSNSAC,
		StockUoMID: it.StockUoMID, PurchaseUoMID: it.StockUoMID, DefaultTaxCodeID: it.DefaultTaxCodeID, IsStock: it.IsStock,
		StandardRate: it.StandardRate, Conversions: []item.Conversion{{UoMID: it.StockUoMID, Factor: uom.FactorOne}}}); err != nil {
		t.Fatalf("drop BOLT-M12's Box conversion: %v", err)
	}
}

// p2WantLineCleared asserts line 2 (BOLT-M12) of id has its amount and GST
// cleared and line 1 still holds before[0]'s computed values.
func p2WantLineCleared(t *testing.T, what string, env *purchasetest.Env, id int64, before []map[string]string) {
	t.Helper()
	got := p2LineSnap(t, env, id, p2LineMoney...)
	if len(got) != 2 {
		t.Fatalf("%s: %d lines, want 2", what, len(got))
	}
	if !mapsEqual(got[0], before[0]) {
		t.Errorf("%s: line 1 = %v, want %v (its checks pass)", what, got[0], before[0])
	}
	p2AllCleared(t, what+", line 2", got[1], p2LineMoney...)
	p2HeaderIsSum(t, what, env, id)
}

// p2WantLineBefore asserts the line money columns of id before the change:
// every one non-blank and non-zero (both lines are 18%, intra-state).
func p2WantLineBefore(t *testing.T, what string, env *purchasetest.Env, id int64) []map[string]string {
	t.Helper()
	before := p2LineSnap(t, env, id, p2LineMoney...)
	if len(before) != 2 || before[1]["amount"] != "240000" {
		t.Fatalf("%s: lines %v, want 2 with line 2 at ₹2,400.00", what, before)
	}
	for i, l := range before {
		for _, c := range []string{"amount", "cgst", "sgst"} {
			if p2Cleared(l[c]) {
				t.Fatalf("%s: line %d %s = %q, want a computed value before the change", what, i+1, c, l[c])
			}
		}
	}
	return before
}

var p2BoxProblem = doctype.FieldError{Field: "uom_id", Line: 2, Message: "Line 2: Item BOLT-M12 has no conversion for UoM BOX. Pick another UoM."}

// Test 3, step save (CD-302 lines; PO spec §3.2, CD-278): a saved Box line of
// BOLT-M12 loses its conversion in the item master. A terms-step save leaves
// the line unchanged, so its error is out of scope (CD-207 (1)) and the save
// commits; the line's amount and GST are cleared, and the header totals are
// the sum of the lines as written.
func TestPO_StepSaveLineNoConversion(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2BoltLines(t, env)...))
	before := p2WantLineBefore(t, "valid draft", env, po.ID)
	p2HeaderIsSum(t, "valid draft", env, po.ID)
	p2DropBox(t, env)

	res, err := p2Step(t, env, env.Priya, po.ID, []string{"remarks"}, map[string]string{"remarks": "Bolts by the box"}, nil)
	p2WantProblems(t, "terms save after Box dropped", res, err, p2BoxProblem)
	p2WantLineCleared(t, "step save after Box dropped", env, po.ID, before)
}

// Test 3, amend (CD-302 lines; PO spec §15 item 8): the same line on an
// approved PO; after cancel the Box conversion goes, and amend copies the
// line with its old amount. Validate clears the line's amount and GST and the
// totals follow the lines.
func TestPO_AmendLineNoConversion(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(), purchasetest.OrderLines(p2BoltLines(t, env)...))
	before := p2WantLineBefore(t, "approved PO", env, po.ID)
	p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, po.ID, doc.ActionCancel, "Bolts by the piece"))
	p2DropBox(t, env)

	res := p2Must(t, "Priya amends")(p2Do(t, env, env.Priya, po.ID, doc.ActionAmend, ""))
	if res.ID == po.ID || res.State != doctype.StateDraft {
		t.Fatalf("amend = %+v, want a new draft", res)
	}
	p2WantLineCleared(t, "amended draft after Box dropped", env, res.ID, before)
	_, err := p2Save(t, env, env.Priya, res.ID, map[string]string{"remarks": "Check"}, nil)
	p2WantInvalid(t, "save the amended draft", err, p2BoxProblem)
}

// Test 4 (CD-302 lines; types' money limit): the amount limit depends only on
// the line's own qty, price and discount, which no master edit changes, so an
// unchanged line can't reach it and an approved PO (and so an amend) never
// holds one; a real lines-step save of a changed line names "lines" and
// refuses. Engine.Do still commits a changed line posted with Only naming a
// header field (Only scopes errors, not values; the line error is out of
// scope), so Validate's write on that path is pinned: the over-limit line's
// amount and GST are cleared, and the totals are the sum of the lines.
func TestPO_StepSaveLineAmountTooLarge(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2BoltLines(t, env)...))
	before := p2WantLineBefore(t, "valid draft", env, po.ID)

	lines := p2KeepLines(t, env, po.ID)
	lines[1]["qty"], lines[1]["price"], lines[1]["uom_id"] = "1000000", "100000", masterID(t, env.DB, "uom", "BOX") // ₹10,00,00,00,00,000.00
	res, err := p2Step(t, env, env.Priya, po.ID, []string{"remarks"}, map[string]string{"remarks": "Big order"}, lines)
	p2WantProblems(t, "line 2 over the limit", res, err, doctype.FieldError{Field: "qty|price|amount", Line: 2,
		Message: "Line 2: Line amount is too large. The most a line can be is ₹10,00,00,00,000.00."})
	p2WantLineCleared(t, "line 2 over the limit", env, po.ID, before)
}

// Test 5 (CD-302 site part; PO spec §4): METRO-SITE is renamed and moved to
// PRJ-DEPOT after the draft was saved. On the step save path a terms-step
// save commits with the site and project error as a problem; the site and
// branch snapshots are written from the site as it is now (not the old name,
// not blank), and the project stays PRJ-METRO. A draft whose site is then
// cleared (Arjun, unrestricted, so CD-178 doesn't refuse) has the site part
// cleared.
func TestPO_StepSaveSiteProject(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2RefLines(t, env)...))
	siteCols := []string{"snap_site_name", "snap_branch_name", "snap_place_state", "branch_id"}
	before := p2Snap(t, env, po.ID, siteCols...)
	for _, c := range siteCols {
		if before[c] == "" {
			t.Fatalf("draft %s blank before the change", c)
		}
	}

	sites := site.New(p2Kit(env))
	var st site.Site
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		st, err = sites.Get(ctx, tx, p2Int(t, masterID(t, env.DB, "site", "METRO-SITE")))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	const renamed = "Metro Site Phase 2"
	if before["snap_site_name"] != st.Name || st.Name == renamed {
		t.Fatalf("draft snap_site_name %q, site name %q: want the site's own name, unlike %q", before["snap_site_name"], st.Name, renamed)
	}
	depot, err := project.New(p2Kit(env)).Create(ctx, env.Admin, project.Input{Code: "PRJ-DEPOT", Name: "Pune Depot Extension", BranchID: st.BranchID})
	if err != nil {
		t.Fatalf("create PRJ-DEPOT: %v", err)
	}
	if _, err := sites.Update(ctx, env.Admin, st.ID, st.Version, site.Input{Code: st.Code, Name: renamed, BranchID: st.BranchID,
		ProjectID: depot.ID, Type: st.Type}); err != nil {
		t.Fatalf("rename METRO-SITE and move it to PRJ-DEPOT: %v", err)
	}

	res, err := p2Step(t, env, env.Priya, po.ID, []string{"remarks"}, map[string]string{"remarks": "Site moved"}, nil)
	p2WantProblems(t, "terms save after the site moved", res, err, doctype.FieldError{Field: "site_id|project_id",
		Message: "Site METRO-SITE is on project PRJ-DEPOT. Pick that project or another site."})
	got := p2Snap(t, env, po.ID, append(siteCols, "project_id")...)
	if got["snap_site_name"] != renamed {
		t.Errorf("snap_site_name = %q after the site moved, want %q (the site now; was %q)", got["snap_site_name"], renamed, before["snap_site_name"])
	}
	for _, c := range []string{"snap_branch_name", "snap_place_state", "branch_id"} {
		if got[c] != before[c] { // the site's branch didn't change
			t.Errorf("%s = %q after the site moved, want %q from the site's branch", c, got[c], before[c])
		}
	}
	if want := masterID(t, env.DB, "project", "PRJ-METRO"); got["project_id"] != want {
		t.Errorf("project_id = %q, want PRJ-METRO %s unchanged (the project default is skipped)", got["project_id"], want)
	}
	p2HeaderIsSum(t, "after the site moved", env, po.ID)

	// No site: the site part is cleared.
	fresh := p2Must(t, "Arjun's draft")(p2Create(t, env, env.Arjun, p2Header(t, env, "SHREE", "TECH-SITE", "PRJ-TECHPARK"), p2RefLines(t, env)))
	had := p2Snap(t, env, fresh.ID, siteCols...)
	for _, c := range siteCols {
		if had[c] == "" {
			t.Fatalf("Arjun's draft %s blank before the site is cleared", c)
		}
	}
	if _, err := p2Step(t, env, env.Arjun, fresh.ID, []string{"site_id"}, map[string]string{"site_id": ""}, nil); err != nil {
		t.Fatalf("clear the site: %v", err)
	}
	p2AllCleared(t, "site cleared", p2Snap(t, env, fresh.ID, siteCols...), siteCols...)
	p2HeaderIsSum(t, "site cleared", env, fresh.ID)
}

// Test 1b (CD-302 vendor part, no vendor): a main-step save that blanks the
// vendor of a SHREE draft (Required is checked at submit, CD-178) clears the
// address and the vendor snapshots.
func TestPO_StepSaveNoVendorClears(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2RefLines(t, env)...))
	p2SHREEVendor(t, "SHREE draft", env, p2Snap(t, env, po.ID, p2VendorCols...))
	if _, err := p2Step(t, env, env.Priya, po.ID, []string{"vendor_id"}, map[string]string{"vendor_id": ""}, nil); err != nil {
		t.Fatalf("blank the vendor: %v", err)
	}
	p2AllCleared(t, "vendor blanked", p2Snap(t, env, po.ID, p2VendorCols...), p2VendorCols...)
	p2HeaderIsSum(t, "vendor blanked", env, po.ID)
}
