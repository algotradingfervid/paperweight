package purchase_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-PO P2 (PO spec §15 item 8; AD-13 fix 1 ruling 1): amend calls
// Validate once, ignores its errors and keeps its writes
// (internal/kernel/doc/terminal.go). So Validate writes what it could compute
// even when it returns field errors: the vendor snapshot, the line
// snapshots, the totals and receipt_status. The part whose check failed (the
// site, after METRO-SITE moved to project PRJ-DEPOT) is left as it was. A cutover
// line (already received 200 of 1,200 kg) makes receipt_status
// part_received, which differs from the column's default.
func TestPO_AmendKeepsValidateWrites(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	lines := p2RefLines(t, env)
	lines[0]["already_received"] = "200"
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(),
		purchasetest.OrderHeader(map[string]string{"earlier_po_no": "P-17"}), purchasetest.OrderLines(lines...))
	if po.State != doctype.StateApproved {
		t.Fatalf("PO state %q, want approved", po.State)
	}
	vendorCols := []string{"snap_vendor_id", "snap_vendor_code", "snap_vendor_name", "snap_vendor_gstin", "snap_vendor_category", "snap_vendor_address", "snap_vendor_state"}
	totalCols := []string{"net_total", "cgst_total", "sgst_total", "igst_total", "grand_total", "rounded_total"}
	lineCols := []string{"snap_item_code", "snap_item_name", "snap_uom_code", "snap_tax_code", "snap_gst_rate", "rate", "amount"}
	vendor, totals, origLines := p2Snap(t, env, po.ID, vendorCols...), p2Snap(t, env, po.ID, totalCols...), p2LineSnap(t, env, po.ID, lineCols...)
	if vendor["snap_vendor_code"] != "SHREE" || vendor["snap_vendor_name"] != "Shree Steel Traders" {
		t.Fatalf("original vendor snapshot = %v, want SHREE", vendor)
	}
	p2Totals(t, "original PO", env, po.ID, p2Intra)
	if got := p2Snap(t, env, po.ID, "receipt_status")["receipt_status"]; got != "part_received" {
		t.Fatalf("original receipt_status %q, want part_received", got)
	}

	p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, po.ID, doc.ActionCancel, "Wrong delivery site"))

	// Break the site and project rule only: METRO-SITE moves to a new
	// project on its own branch (PUNE), PRJ-DEPOT.
	sites := site.New(p2Kit(env))
	var st site.Site
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		st, err = sites.Get(ctx, tx, p2Int(t, masterID(t, env.DB, "site", "METRO-SITE")))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	depot, err := project.New(p2Kit(env)).Create(ctx, env.Admin, project.Input{Code: "PRJ-DEPOT", Name: "Pune Depot Extension", BranchID: st.BranchID})
	if err != nil {
		t.Fatalf("create PRJ-DEPOT: %v", err)
	}
	if _, err := sites.Update(ctx, env.Admin, st.ID, st.Version, site.Input{Code: st.Code, Name: st.Name, BranchID: st.BranchID,
		ProjectID: depot.ID, Type: st.Type}); err != nil {
		t.Fatalf("move METRO-SITE to PRJ-DEPOT: %v", err)
	}

	res := p2Must(t, "Priya amends")(p2Do(t, env, env.Priya, po.ID, doc.ActionAmend, ""))
	if res.ID == po.ID || res.State != doctype.StateDraft {
		t.Fatalf("amend = %+v, want a new draft", res)
	}

	if got := p2Snap(t, env, res.ID, vendorCols...); !mapsEqual(got, vendor) {
		t.Errorf("amended draft vendor snapshot = %v, want the vendor's %v (Validate's writes kept despite the site error)", got, vendor)
	}
	if got := p2Snap(t, env, res.ID, totalCols...); !mapsEqual(got, totals) {
		t.Errorf("amended draft totals = %v, want the original PO's %v", got, totals)
	}
	if got := p2LineSnap(t, env, res.ID, lineCols...); len(got) != 2 || !mapsEqual(got[0], origLines[0]) || !mapsEqual(got[1], origLines[1]) {
		t.Errorf("amended draft line snapshots = %v, want the original's %v", got, origLines)
	}
	if got := p2Snap(t, env, res.ID, "receipt_status")["receipt_status"]; got != "part_received" {
		t.Errorf("amended draft receipt_status %q, want part_received (Validate's, from already_received)", got)
	}
	// The failed part is left as it was: no site snapshot on the new draft.
	if got := p2Snap(t, env, res.ID, "snap_site_name")["snap_site_name"]; got != "" {
		t.Errorf("amended draft snap_site_name %q, want blank (the site check failed)", got)
	}

	// The amend really hit the site and project error: a save of the new
	// draft is refused with it.
	_, err = p2Save(t, env, env.Priya, res.ID, map[string]string{"remarks": "Check"}, nil)
	p2WantInvalid(t, "save the amended draft", err, doctype.FieldError{Field: "site_id|project_id",
		Message: "Site METRO-SITE is on project PRJ-DEPOT. Pick that project or another site."})
}

// MOD-PO P2 (PO spec §3.2, §24.2; AD-13 fix 1 ruling 2): a line saved with a
// blank price, with no last price (P7's stub), takes the item's standard
// rate with discount 0; a line posted with a price keeps it.
func TestPO_SaveDefaultsStandardRate(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	items := item.New(p2Kit(env))
	var angle item.Item
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		angle, err = items.Get(ctx, tx, p2Int(t, masterID(t, env.DB, "item", "MS-ANGLE-50")))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	const standard = types.Rate(64_2500) // ₹64.2500
	if _, err := items.Update(ctx, env.Admin, angle.ID, angle.Version, item.Input{Code: angle.Code, Name: angle.Name,
		Type: angle.Type, HSNSAC: angle.HSNSAC, StockUoMID: angle.StockUoMID, PurchaseUoMID: angle.PurchaseUoMID,
		DefaultTaxCodeID: angle.DefaultTaxCodeID, IsStock: angle.IsStock, StandardRate: standard}); err != nil {
		t.Fatalf("set MS-ANGLE-50's standard rate: %v", err)
	}

	blank := p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "")
	blank["discount_pct"] = ""
	priced := p2Line(t, env, "MS-ANGLE-50", "KGS", "5", "60")
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(blank, priced))
	d := p2Load(t, env, po.ID)
	if len(d.Lines) != 2 {
		t.Fatalf("%d lines, want 2", len(d.Lines))
	}
	for i, want := range []map[string]string{
		{"price": standard.Plain(), "discount_pct": types.Percent(0).Plain(), "rate": standard.Plain()},
		{"price": types.Rate(60_0000).Plain(), "discount_pct": types.Percent(0).Plain()},
	} {
		for k, w := range want {
			if got := p2Plain(d.Lines[i].Values[k]); got != w {
				t.Errorf("line %d %s = %s, want %s", i+1, k, got, w)
			}
		}
	}
}

// MOD-PO P2 (PO spec §27.3; AD-13 fix 1 ruling 3): OrderWarnings on a draft
// with no site and no project, or a site without a project (PUNE-WH) and no
// project, returns no warnings and no error: the state warning needs both.
func TestPO_BlankDraftNoWarnings(t *testing.T) {
	env := purchasetest.New(t)
	for _, c := range []struct {
		what string
		site string
	}{{"no site or project", ""}, {"PUNE-WH, no project", "PUNE-WH"}} {
		values := map[string]string{"vendor_id": "", "site_id": "", "project_id": "", "order_date": ""}
		if c.site != "" {
			values["site_id"] = masterID(t, env.DB, "site", c.site)
		}
		res, err := p2Create(t, env, env.Arjun, values, nil)
		if err != nil {
			t.Fatalf("%s: create: %v", c.what, err)
		}
		if got := p2Plain(p2Load(t, env, res.ID).Header["project_id"]); got != "<nil>" {
			t.Fatalf("%s: project_id %s, want blank", c.what, got)
		}
		w, err := env.Service.OrderWarnings(t.Context(), env.Arjun, res.ID)
		if err != nil || len(w) != 0 {
			t.Errorf("%s: OrderWarnings = %q, %v; want none and a nil error", c.what, w, err)
		}
	}
}
