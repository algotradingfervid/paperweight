package grntest

import (
	"bytes"
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN G9 helpers (reports and CSV).

// Photos attaches n small JPEGs to GRN id as p through the real files.Store
// (each a different size, so none is refused as a duplicate).
func (e *Env) Photos(t testing.TB, p auth.Principal, id int64, n int) {
	t.Helper()
	ctx := context.Background()
	for i := range n {
		sp, err := e.Files.Spool(bytes.NewReader(filestest.JPEG(2048+i)), files.MaxFileBytes)
		if err != nil {
			t.Fatalf("grntest: spool photo %d: %v", i+1, err)
		}
		err = e.DB.Write(ctx, func(tx *db.Tx) error {
			_, err := e.Files.Attach(ctx, tx, p, auth.RecordRef{DocType: goodsreceipt.DocType, ID: id}, "challan.jpg", sp)
			return err
		})
		_ = sp.Cleanup()
		if err != nil {
			t.Fatalf("grntest: attach photo %d to GRN %d as %s: %v", i+1, id, p.Name, err)
		}
	}
}

// RenameMaster sets column col of the master row with code in table to v
// with plain SQL: a master changed after the GRN, for "labels come from the
// GRN's own snapshots" (plan G9). The masters' own service would refuse
// none of these values; SQL keeps the test free of its form rules.
func (e *Env) RenameMaster(t testing.TB, table, code, col, v string) {
	t.Helper()
	if _, err := e.DB.SQL().ExecContext(context.Background(), `UPDATE `+table+` SET `+col+` = ? WHERE code = ?`, v, code); err != nil { //nolint:gosec // G202: fixed table and column names
		t.Fatalf("grntest: set %s.%s of %s: %v", table, col, code, err)
	}
}

// ForceClosed sets approved GRN id to closed with plain SQL. CloseRight is
// doctype.CloseNone, so no public path closes a GRN (GRN-034); the reports
// still list closed GRNs (spec §12), and this pins the state list.
func (e *Env) ForceClosed(t testing.TB, id int64) {
	t.Helper()
	res, err := e.DB.SQL().ExecContext(context.Background(),
		`UPDATE goods_receipt SET state = ? WHERE id = ? AND state = ?`, string(doctype.StateClosed), id, string(doctype.StateApproved))
	if err != nil {
		t.Fatalf("grntest: close GRN %d: %v", id, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("grntest: close GRN %d changed %d rows, want 1 (not approved?)", id, n)
	}
}

// ReceiptReader creates a user whose only role, "Receipt Reader", holds
// read and export on goodsreceipt.Receipt at scope all and nothing on
// purchase.Order, through the real AUTH, and returns the principal: a
// viewer of the GRN who can't read its PO (CD-233, CD-297).
func (e *Env) ReceiptReader(t testing.TB) auth.Principal {
	t.Helper()
	ctx := context.Background()
	if err := e.Auth.CreateRole(ctx, e.Admin, "Receipt Reader"); err != nil {
		t.Fatalf("grntest: create the Receipt Reader role: %v", err)
	}
	if err := e.Auth.SetRights(ctx, e.Admin, "Receipt Reader", goodsreceipt.DocType,
		map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightExport: auth.ScopeAll}); err != nil {
		t.Fatalf("grntest: Receipt Reader rights: %v", err)
	}
	id, _, err := e.Auth.CreateUser(ctx, e.Admin, auth.NewUser{ //nolint:gosec // G101: a test user's password
		Email: "rcpread@vardhan.test", Name: "Receipt Reader", Password: "Counting the challans 42", Roles: []string{"Receipt Reader"},
	})
	if err != nil {
		t.Fatalf("grntest: create the Receipt Reader user: %v", err)
	}
	p, _, err := e.Auth.LoadPrincipal(ctx, id)
	if err != nil {
		t.Fatalf("grntest: load the Receipt Reader user: %v", err)
	}
	return p
}

// ReportVendor is BHARAT's name in the report world: a comma, quotes and a
// leading "=" (a spreadsheet formula trigger), for the CSV's escaping.
const ReportVendor = `=Bharat "Tools", Pune`

// ReportInvoice is G1's bill details in the report world (grn.md's vendor
// invoice), dated 24-10-2026 for ₹34,426.50.
const ReportInvoice = "SST/2627/0457"

// ReportWorld is the reports' data (plan G9; GRN-031, GRN-032), built
// through the engine with the grntest builders:
//
//   - RefPO: grn.md's reference PO by Priya for SHREE on METRO-SITE
//     (PO/26-27/00001: line 1 MS-ANGLE-50 1,200 kg at ₹62.50, line 2
//     WIRE-18G 50 kg at ₹96.00, HSN 7217). BharatPO: Priya's PO for BHARAT
//     (renamed ReportVendor first), 100 kg MS-ANGLE-50. TechPO: admin's PO
//     on TECH-SITE / PRJ-TECHPARK, 100 kg MS-ANGLE-50.
//   - On 24-10-2026 (authtest.T0): G1, Suresh on RefPO, line 1 received 400
//     rejected 10 "Bent pieces", line 2 received 50 (accepted value
//     ₹29,175.00), with bill details ReportInvoice and 2 photos; G2, Suresh
//     on BharatPO, 20 kg; GT, admin on TechPO, 30 kg; GC, Suresh on RefPO,
//     5 kg, then closed (ForceClosed); Cancelled, Suresh on RefPO, 7 kg,
//     cancelled by Ravi; Draft, Suresh on RefPO, 8 kg, a draft.
//   - With the clock at 31-10-2026 18:31 UTC, just after IST midnight, so
//     the business date is 01-11-2026: G3, Suresh on RefPO, line 2 received
//     4 kg; then a Goods Receipt rule from ₹0 approved by the Administrator,
//     and Pending (Suresh on RefPO, 9 kg, left pending_approval) and
//     Rejected (11 kg, rejected by the Administrator).
//   - Then, after every GRN, the masters change: SHREE's name, MS-ANGLE-50's
//     code, name and HSN and the KGS unit's code, so a report reading the
//     masters instead of the GRN's snapshots shows the new texts.
//
// G1, G2, GT, GC and G3 are the rows (approved or closed); the others never
// are. Suresh (Stores, sites PUNE-WH and METRO-SITE) and Priya (project
// PRJ-METRO) read every row but GT. The clock stays at 31-10-2026 18:31 UTC.
type ReportWorld struct {
	RefPO, BharatPO, TechPO             doc.Doc
	G1, G2, GT, GC, G3                  doc.Doc
	Draft, Cancelled, Pending, Rejected doc.Doc
	Shree, Bharat, Angle, Wire          int64 // party and item ids
}

// AfterMidnight is 31-10-2026 18:31 UTC, 01-11-2026 00:01 IST.
var AfterMidnight = time.Date(2026, time.October, 31, 18, 31, 0, 0, time.UTC)

// NewReportWorld builds ReportWorld on e.
func (e *Env) NewReportWorld(t testing.TB) ReportWorld {
	t.Helper()
	var w ReportWorld
	w.Shree, w.Bharat = e.id(t, "party", "SHREE"), e.id(t, "party", "BHARAT")
	w.Angle, w.Wire = e.id(t, "item", "MS-ANGLE-50"), e.id(t, "item", "WIRE-18G")
	e.RenameMaster(t, "party", "BHARAT", "name", ReportVendor)

	w.RefPO = purchasetest.NewOrder(t, e.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(
		map[string]string{"qty": "1200", "price": "62.5"},
		map[string]string{"item_id": e.masterID(t, "item", "WIRE-18G"), "qty": "50", "price": "96", "hsn_sac": "7217"},
	))
	w.BharatPO = purchasetest.NewOrder(t, e.Env, purchasetest.StopAtApproved(),
		purchasetest.OrderHeader(map[string]string{"vendor_id": strconv.FormatInt(w.Bharat, 10)}))
	w.TechPO = POAs(t, e, e.Admin, doctype.StateApproved, map[string]string{
		"site_id": e.masterID(t, "site", "TECH-SITE"), "project_id": e.masterID(t, "project", "PRJ-TECHPARK"),
	})
	l1, l2 := w.RefPO.Lines[0].ID, w.RefPO.Lines[1].ID
	approved := doctype.StateApproved

	w.G1 = e.GRN(t, e.Suresh, w.RefPO.ID, []Line{{POLineID: l1, Received: "400", Rejected: "10", Reason: "Bent pieces"}, {POLineID: l2, Received: "50"}}, approved)
	if _, err := e.Service.SaveBill(context.Background(), e.Suresh, w.G1.ID, 0,
		goodsreceipt.BillInput{InvoiceNo: ReportInvoice, InvoiceDate: "2026-10-24", InvoiceAmount: "34426.50"}); err != nil {
		t.Fatalf("grntest: G1's bill details: %v %+v", err, fieldErrors(err))
	}
	e.Photos(t, e.Suresh, w.G1.ID, 2)
	w.G2 = e.GRN(t, e.Suresh, w.BharatPO.ID, []Line{{POLineID: w.BharatPO.Lines[0].ID, Received: "20"}}, approved)
	w.GT = e.GRN(t, e.Admin, w.TechPO.ID, []Line{{POLineID: w.TechPO.Lines[0].ID, Received: "30"}}, approved)
	w.GC = e.GRN(t, e.Suresh, w.RefPO.ID, []Line{{POLineID: l1, Received: "5"}}, approved)
	e.ForceClosed(t, w.GC.ID)
	w.GC = e.Load(t, goodsreceipt.DocType, w.GC.ID)
	w.Cancelled = e.GRN(t, e.Suresh, w.RefPO.ID, []Line{{POLineID: l1, Received: "7"}}, approved)
	w.Cancelled = e.Cancel(t, e.Ravi, w.Cancelled.ID, "Wrong order")
	w.Draft = e.GRN(t, e.Suresh, w.RefPO.ID, []Line{{POLineID: l1, Received: "8"}}, doctype.StateDraft)

	e.SetNow(t, AfterMidnight)
	w.G3 = e.GRN(t, e.Suresh, w.RefPO.ID, []Line{{POLineID: l2, Received: "4"}}, approved)
	e.AddRule(t, goodsreceipt.DocType, 0, "Administrator")
	w.Pending = e.GRN(t, e.Suresh, w.RefPO.ID, []Line{{POLineID: l1, Received: "9"}}, doctype.StatePending)
	w.Rejected = e.GRN(t, e.Suresh, w.RefPO.ID, []Line{{POLineID: l1, Received: "11"}}, doctype.StatePending)
	w.Rejected = e.act(t, e.Admin, goodsreceipt.DocType, w.Rejected.ID, doctype.ActionReject, "Not our delivery")

	e.RenameMaster(t, "party", "SHREE", "name", "Shree Steel (renamed)")
	e.RenameMaster(t, "item", "MS-ANGLE-50", "name", "Angle (renamed)")
	e.RenameMaster(t, "item", "MS-ANGLE-50", "hsn_sac", "7228")
	e.RenameMaster(t, "item", "MS-ANGLE-50", "code", "MS-ANGLE-RENAMED")
	e.RenameMaster(t, "uom", "KGS", "code", "KGM")
	return w
}

// id is masterID as an int64.
func (e *Env) id(t testing.TB, table, code string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(e.masterID(t, table, code), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Day is d-m-2026 as a types.Date.
func Day(d int, m time.Month) types.Date { return types.Date{Year: 2026, Month: m, Day: d} }
