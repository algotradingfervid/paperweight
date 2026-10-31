package purchase_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

const (
	p2SameState = "Intra-state: the supplier and the delivery site are both in Maharashtra."
	p2DiffState = "Inter-state: the supplier is in Karnataka and the delivery site is in Maharashtra."
	p2SEZ       = "Inter-state: Shree Steel Traders is an SEZ supplier."
)

// p2Totals asserts the header totals of order id, formatted with FormatMoney.
func p2Totals(t *testing.T, what string, env *purchasetest.Env, id int64, want map[string]string) {
	t.Helper()
	h := p2Load(t, env, id).Header
	for _, k := range []string{"net_total", "cgst_total", "sgst_total", "igst_total", "grand_total", "rounded_total"} {
		w, ok := want[k]
		if !ok {
			continue
		}
		if got := p2Money(h[k]); got != w {
			t.Errorf("%s: %s = %s, want %s", what, k, got, w)
		}
	}
}

// p2Intra and p2Inter are the reference PO's totals (PO spec §18).
var (
	p2Intra = map[string]string{"net_total": "79,800.00", "cgst_total": "7,182.00", "sgst_total": "7,182.00",
		"igst_total": "0.00", "grand_total": "94,164.00", "rounded_total": "94,164.00"}
	p2Inter = map[string]string{"net_total": "79,800.00", "cgst_total": "0.00", "sgst_total": "0.00",
		"igst_total": "14,364.00", "grand_total": "94,164.00", "rounded_total": "94,164.00"}
)

// p2PlaceReason is gstcalc.PlaceReason over the snapshot columns Validate
// stored (PO spec §3.1, §7.1): the reason the check page and print show.
func p2PlaceReason(t *testing.T, env *purchasetest.Env, id int64) string {
	t.Helper()
	s := p2Snap(t, env, id, "snap_vendor_category", "snap_vendor_name", "snap_vendor_state", "snap_place_state")
	return gstcalc.PlaceReason(s["snap_vendor_category"], s["snap_vendor_name"], s["snap_vendor_state"], s["snap_place_state"])
}

// p2SetCategory changes a party's GST category through the party master.
func p2SetCategory(t *testing.T, env *purchasetest.Env, code string, c gstin.Category) {
	t.Helper()
	svc := party.New(p2Kit(env))
	var pt party.Party
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		pt, err = svc.Get(t.Context(), tx, p2Int(t, masterID(t, env.DB, "party", code)))
		return err
	}); err != nil {
		t.Fatalf("get party %s: %v", code, err)
	}
	if _, err := svc.Update(t.Context(), env.Admin, pt.ID, pt.Version, party.Input{
		Code: pt.Code, Name: pt.Name, IsVendor: pt.IsVendor, IsCustomer: pt.IsCustomer, PAN: pt.PAN,
		GSTCategory: c, DefaultPaymentTermsID: pt.DefaultPaymentTermsID,
	}); err != nil {
		t.Fatalf("set %s's category to %s: %v", code, c, err)
	}
}

// PO-001 (PO spec §3.1, §3.2, §7, §18; 00-fixtures.md reference PO): the
// reference PO for SHREE delivering to METRO-SITE (branch PUNE) totals to the
// paisa, intra-state; Validate copies the branch from the site and stores the
// header and line snapshots on the first save.
func TestPO_ReferenceTotals(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2RefLines(t, env)...))
	p2Totals(t, "reference PO", env, po.ID, p2Intra)

	d := p2Load(t, env, po.ID)
	if got, want := p2Plain(d.Header["branch_id"]), masterID(t, env.DB, "branch", "PUNE"); got != want {
		t.Errorf("branch_id = %s, want PUNE's %s (copied from the site)", got, want)
	}
	wantLines := []struct{ rate, amount, cgst, sgst, igst string }{
		{"62.5", "75,000.00", "6,750.00", "6,750.00", "0.00"},
		{"96", "4,800.00", "432.00", "432.00", "0.00"},
	}
	if len(d.Lines) != len(wantLines) {
		t.Fatalf("order has %d lines, want 2", len(d.Lines))
	}
	for i, w := range wantLines {
		v := d.Lines[i].Values
		rate, _ := types.ParseRate(w.rate)
		if got, ok := v["rate"].(types.Rate); !ok || got != rate {
			t.Errorf("line %d rate = %v, want %s", i+1, v["rate"], w.rate)
		}
		for k, want := range map[string]string{"amount": w.amount, "cgst": w.cgst, "sgst": w.sgst, "igst": w.igst} {
			if got := p2Money(v[k]); got != want {
				t.Errorf("line %d %s = %s, want %s", i+1, k, got, want)
			}
		}
	}

	snap := p2Snap(t, env, po.ID, "snap_vendor_id", "snap_vendor_code", "snap_vendor_name", "snap_vendor_gstin",
		"snap_vendor_category", "snap_vendor_address", "snap_branch_name", "snap_branch_gstin", "snap_site_name")
	for k, want := range map[string]string{
		"snap_vendor_id": masterID(t, env.DB, "party", "SHREE"), "snap_vendor_code": "SHREE", "snap_vendor_name": "Shree Steel Traders",
		"snap_vendor_gstin": "27AABCS1429B1ZU", "snap_vendor_category": "Registered Regular",
		"snap_branch_name": "Pune Works", "snap_branch_gstin": "27AAFCV2449G1Z7", "snap_site_name": "METRO-SITE",
	} {
		if snap[k] != want {
			t.Errorf("%s = %q, want %q", k, snap[k], want)
		}
	}
	if snap["snap_vendor_address"] == "" {
		t.Error("snap_vendor_address is blank, want the Mumbai address text")
	}
	lines := p2LineSnap(t, env, po.ID, "snap_item_code", "snap_item_name", "snap_uom_code", "snap_tax_code", "snap_gst_rate")
	wantSnap := []map[string]string{
		{"snap_item_code": "MS-ANGLE-50", "snap_item_name": "MS Angle 50x50x5", "snap_uom_code": "KGS", "snap_tax_code": "GST18", "snap_gst_rate": "18000"},
		{"snap_item_code": "WIRE-18G", "snap_item_name": "Binding Wire 18G", "snap_uom_code": "KGS", "snap_tax_code": "GST18", "snap_gst_rate": "18000"},
	}
	if len(lines) != 2 {
		t.Fatalf("line snapshots: %d lines, want 2", len(lines))
	}
	for i, w := range wantSnap {
		for k, want := range w {
			if lines[i][k] != want {
				t.Errorf("line %d %s = %q, want %q", i+1, k, lines[i][k], want)
			}
		}
	}
}

// PO-002 (PO spec §7, §18): the reference lines from SHREE (Maharashtra)
// delivered to TECH-SITE (branch BLR, Karnataka) are inter-state: all IGST,
// the same grand total. Arjun raises it, since Priya is restricted to
// PRJ-METRO (00-fixtures.md GRN seed note).
func TestPO_InterStateSplit(t *testing.T) {
	env := purchasetest.New(t)
	res := p2Must(t, "create TECH-SITE order")(p2Create(t, env, env.Arjun, p2Header(t, env, "SHREE", "TECH-SITE", "PRJ-TECHPARK"), p2RefLines(t, env)))
	p2Totals(t, "TECH-SITE order", env, res.ID, p2Inter)
	d := p2Load(t, env, res.ID)
	if got, want := p2Plain(d.Header["branch_id"]), masterID(t, env.DB, "branch", "BLR"); got != want {
		t.Errorf("branch_id = %s, want BLR's %s", got, want)
	}
	if len(d.Lines) == 2 {
		for i, want := range []string{"13,500.00", "864.00"} {
			if got := p2Money(d.Lines[i].Values["igst"]); got != want {
				t.Errorf("line %d igst = %s, want %s", i+1, got, want)
			}
		}
	} else {
		t.Errorf("order has %d lines, want 2", len(d.Lines))
	}
	if got := p2Snap(t, env, res.ID, "snap_branch_gstin")["snap_branch_gstin"]; got != "29AAFCV2449G1Z3" {
		t.Errorf("snap_branch_gstin = %q, want BLR's 29AAFCV2449G1Z3", got)
	}
}

// PO-003 (PO spec §8, §18; NUM-041): changing the vendor of a draft
// recomputes the split and the vendor snapshots follow; changing it back
// undoes it. Changing the site recomputes the split too.
func TestPO_VendorChangeRecomputes(t *testing.T) {
	env := purchasetest.New(t)
	res := p2Must(t, "create")(p2Create(t, env, env.Arjun, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), p2RefLines(t, env)))
	id := res.ID
	p2Totals(t, "SHREE draft", env, id, p2Intra)

	p2Must(t, "vendor to BHARAT")(p2Save(t, env, env.Arjun, id, map[string]string{"vendor_id": masterID(t, env.DB, "party", "BHARAT")}, nil))
	p2Totals(t, "after the change to BHARAT", env, id, p2Inter)
	snap := p2Snap(t, env, id, "snap_vendor_id", "snap_vendor_code", "snap_vendor_name", "snap_vendor_gstin")
	for k, want := range map[string]string{
		"snap_vendor_id": masterID(t, env.DB, "party", "BHARAT"), "snap_vendor_code": "BHARAT",
		"snap_vendor_name": "Bharat Tools", "snap_vendor_gstin": "29AABCT1332L1ZA",
	} {
		if snap[k] != want {
			t.Errorf("after the change to BHARAT: %s = %q, want %q", k, snap[k], want)
		}
	}
	if got := p2PlaceReason(t, env, id); got != p2DiffState {
		t.Errorf("after the change to BHARAT: place reason %q, want %q", got, p2DiffState)
	}

	p2Must(t, "vendor back to SHREE")(p2Save(t, env, env.Arjun, id, map[string]string{"vendor_id": masterID(t, env.DB, "party", "SHREE")}, nil))
	p2Totals(t, "back to SHREE", env, id, p2Intra)
	if got := p2Snap(t, env, id, "snap_vendor_code")["snap_vendor_code"]; got != "SHREE" {
		t.Errorf("back to SHREE: snap_vendor_code = %q", got)
	}

	p2Must(t, "site to TECH-SITE")(p2Save(t, env, env.Arjun, id, map[string]string{
		"site_id": masterID(t, env.DB, "site", "TECH-SITE"), "project_id": masterID(t, env.DB, "project", "PRJ-TECHPARK")}, nil))
	p2Totals(t, "site TECH-SITE", env, id, p2Inter)
	p2Must(t, "site back to METRO-SITE")(p2Save(t, env, env.Arjun, id, map[string]string{
		"site_id": masterID(t, env.DB, "site", "METRO-SITE"), "project_id": masterID(t, env.DB, "project", "PRJ-METRO")}, nil))
	p2Totals(t, "site back to METRO-SITE", env, id, p2Intra)
}

// PO-004 (PO spec §7, §7.1, §18; gstcalc.IsInterStateSupply): an SEZ vendor
// in Maharashtra delivering to a PUNE site is inter-state; set back to
// Registered Regular, the next draft save is intra-state again.
func TestPO_SEZInterState(t *testing.T) {
	env := purchasetest.New(t)
	p2SetCategory(t, env, "SHREE", gstin.SEZ)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2RefLines(t, env)...))
	p2Totals(t, "SEZ vendor", env, po.ID, p2Inter)
	if got := p2Snap(t, env, po.ID, "snap_vendor_category")["snap_vendor_category"]; got != "SEZ" {
		t.Errorf("snap_vendor_category = %q, want SEZ", got)
	}

	p2SetCategory(t, env, "SHREE", gstin.Regular)
	p2Must(t, "save after the category change")(p2Save(t, env, env.Priya, po.ID, map[string]string{"remarks": "Recheck tax"}, nil))
	p2Totals(t, "Registered Regular again", env, po.ID, p2Intra)
}

// PO-005, stored part (PO spec §7, §7.1; CD-254): the snapshots Validate
// stores give gstcalc.PlaceReason's three texts. The check page (P15a) and
// print (P8) parts are TestPO_PlaceReasonShown in those tasks.
func TestPO_PlaceReasonStored(t *testing.T) {
	env := purchasetest.New(t)
	same := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2RefLines(t, env)...))
	diff := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2RefLines(t, env)...),
		purchasetest.OrderHeader(map[string]string{"vendor_id": masterID(t, env.DB, "party", "BHARAT")}))
	p2SetCategory(t, env, "SHREE", gstin.SEZ)
	sez := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2RefLines(t, env)...))
	for _, c := range []struct {
		what string
		id   int64
		want string
	}{{"same state", same.ID, p2SameState}, {"different states", diff.ID, p2DiffState}, {"SEZ supplier", sez.ID, p2SEZ}} {
		if got := p2PlaceReason(t, env, c.id); got != c.want {
			t.Errorf("%s: place reason from the stored snapshot = %q, want %q", c.what, got, c.want)
		}
	}
}

// PO-006 (PO spec §3.2, §18; NUM-015): rate is price less the discount to 4
// dp and amount is gstcalc.LineAmount(qty, rate); a blank discount is 0.
// Clearing the discount gives the price back (undo).
func TestPO_LineDiscount(t *testing.T) {
	env := purchasetest.New(t)
	line := func(qty, price, disc string) map[string]string {
		l := p2Line(t, env, "MS-ANGLE-50", "KGS", qty, price)
		l["discount_pct"] = disc
		return l
	}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(
		line("10", "100", "12.5"), line("3", "99.99", "33.333"), line("2", "50", ""), line("4", "80", "100")))
	check := func(what string, want []struct{ disc, rate, amount string }) {
		t.Helper()
		d := p2Load(t, env, po.ID)
		if len(d.Lines) != len(want) {
			t.Fatalf("%s: %d lines, want %d", what, len(d.Lines), len(want))
		}
		for i, w := range want {
			v := d.Lines[i].Values
			pct, _ := types.ParsePercent(w.disc)
			if got, ok := v["discount_pct"].(types.Percent); !ok || got != pct {
				t.Errorf("%s line %d discount_pct = %v, want %s", what, i+1, v["discount_pct"], w.disc)
			}
			rate, _ := types.ParseRate(w.rate)
			if got, ok := v["rate"].(types.Rate); !ok || got != rate {
				t.Errorf("%s line %d rate = %v, want %s", what, i+1, v["rate"], w.rate)
			}
			if got := p2Money(v["amount"]); got != w.amount {
				t.Errorf("%s line %d amount = %s, want %s", what, i+1, got, w.amount)
			}
		}
	}
	check("discounts", []struct{ disc, rate, amount string }{
		{"12.5", "87.5", "875.00"}, {"33.333", "66.6603", "199.98"}, {"0", "50", "100.00"}, {"100", "0", "0.00"},
	})
	p2Must(t, "clear the discounts")(p2Save(t, env, env.Priya, po.ID, nil, []map[string]string{
		line("10", "100", "0"), line("3", "99.99", "0"), line("2", "50", ""), line("4", "80", "0")}))
	check("no discounts", []struct{ disc, rate, amount string }{
		{"0", "100", "1,000.00"}, {"0", "99.99", "299.97"}, {"0", "50", "100.00"}, {"0", "80", "320.00"},
	})
}

// PO-026 (PO spec §3.1, §5, §7, §18; D-APR-03): with reverse charge on, GST
// is still computed and shown but left out of the grand total, and the
// approval Amount is the rounded total plus the tax. A ₹45,000.00 order under
// reverse charge has Amount ₹53,100.00, so it routes to the Purchase Manager
// rule although its rounded total is below ₹50,000.00. Turning reverse charge
// off on a draft puts the tax back in the grand total (undo).
func TestPO_ReverseChargeTotals(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	amount := func(id int64) string {
		t.Helper()
		var m types.Money
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			m, err = purchase.AmountOrder(ctx, tx, id)
			return err
		}); err != nil {
			t.Fatalf("amountOrder(%d): %v", id, err)
		}
		return types.FormatMoney(m)
	}
	rc := purchasetest.OrderHeader(map[string]string{"reverse_charge": "true"})

	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), rc, purchasetest.OrderLines(p2RefLines(t, env)...))
	p2Totals(t, "reverse charge", env, draft.ID, map[string]string{"net_total": "79,800.00", "cgst_total": "7,182.00",
		"sgst_total": "7,182.00", "igst_total": "0.00", "grand_total": "79,800.00", "rounded_total": "79,800.00"})
	if got := amount(draft.ID); got != "94,164.00" {
		t.Errorf("reverse charge: Amount = %s, want 94,164.00 (rounded total plus the tax)", got)
	}

	p2Must(t, "reverse charge off")(p2Save(t, env, env.Priya, draft.ID, map[string]string{"reverse_charge": "false"}, nil))
	p2Totals(t, "reverse charge off", env, draft.ID, p2Intra)
	if got := amount(draft.ID); got != "94,164.00" {
		t.Errorf("reverse charge off: Amount = %s, want 94,164.00", got)
	}

	// The routing reads the Amount: rounded ₹45,000.00 alone would approve on
	// submit, rounded plus tax ₹53,100.00 needs the Purchase Manager.
	small := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), rc,
		purchasetest.OrderLines(p2Line(t, env, "MS-ANGLE-50", "KGS", "720", "62.5")))
	if small.State != doctype.StatePending || small.Number != "" {
		t.Fatalf("₹45,000.00 order under reverse charge: state %q number %q, want pending with no number", small.State, small.Number)
	}
	p2Totals(t, "₹45,000.00 order", env, small.ID, map[string]string{"grand_total": "45,000.00", "rounded_total": "45,000.00"})
	var req approval.Request
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var ok bool
		var err error
		req, ok, err = env.Approvals.LatestRequest(ctx, tx, purchase.DocType, small.ID)
		if err == nil && !ok {
			t.Error("no approval request for the pending order")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if types.FormatMoney(req.Amount) != "53,100.00" || len(req.ApproverRoles) == 0 || req.ApproverRoles[0] != "Purchase Manager" {
		t.Errorf("approval request amount %s roles %v, want 53,100.00 with Purchase Manager first", types.FormatMoney(req.Amount), req.ApproverRoles)
	}
}

// PO-027 (PO spec §4, §18; CD-264 (2) doc.Invalid): reverse charge on with a
// Nil-Rated, Exempted or Non-GST line saves as a draft but is refused at
// submit through BeforeSubmit, naming that line only; nothing is written.
// With a taxable code on the line, or reverse charge off, submit goes through.
func TestPO_ReverseChargeNeedsTaxable(t *testing.T) {
	for _, code := range []string{"NIL", "EXEMPT", "NONGST"} {
		t.Run(code, func(t *testing.T) {
			env := purchasetest.New(t)
			lines := []map[string]string{
				p2Line(t, env, "MS-ANGLE-50", "KGS", "1200", "62.5"),
				p2Line(t, env, "WIRE-18G", "KGS", "50", "96", code),
			}
			po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(),
				purchasetest.OrderHeader(map[string]string{"reverse_charge": "true"}), purchasetest.OrderLines(lines...))
			if po.State != doctype.StateDraft {
				t.Fatalf("draft save with a %s line: state %q, want draft (the save isn't refused)", code, po.State)
			}
			before := p2CountAll(t, env)
			_, err := p2Do(t, env, env.Priya, po.ID, doc.ActionSubmit, "")
			p2WantInvalid(t, "submit", err, doctype.FieldError{Field: "tax_code_id", Line: 2,
				Message: "Line 2: Reverse charge needs a taxable tax code on every line."})
			p2SameCounts(t, "refused submit", before, env)
			if d := p2Load(t, env, po.ID); d.State != doctype.StateDraft || d.Number != "" || d.Version != po.Version {
				t.Errorf("after the refused submit: state %q number %q version %d, want draft, none, %d", d.State, d.Number, d.Version, po.Version)
			}

			// Reverse charge off: the same lines submit.
			off := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(lines...))
			if res, err := p2Do(t, env, env.Priya, off.ID, doc.ActionSubmit, ""); err != nil || res.State == doctype.StateDraft {
				t.Errorf("submit with reverse charge off: %v, state %q; want it to leave draft", err, res.State)
			}

			// Undo: a taxable code on line 2 lets the first one submit.
			lines[1] = p2Line(t, env, "WIRE-18G", "KGS", "50", "96")
			p2Must(t, "line 2 taxable")(p2Save(t, env, env.Priya, po.ID, nil, lines))
			if res, err := p2Do(t, env, env.Priya, po.ID, doc.ActionSubmit, ""); err != nil || res.State == doctype.StateDraft {
				t.Errorf("submit with every line taxable: %v, state %q; want it to leave draft", err, res.State)
			}
		})
	}
}
