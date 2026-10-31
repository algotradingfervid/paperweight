package goodsreceipt_test

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// GRN-004 (GRN spec §3.1, §4; CD-302, CD-303): the PO can't change once the
// draft has lines, detected from the stored lines. A plain save is refused
// on purchase_order_id with nothing changed; a step save whose scope leaves
// that error out commits, and then every derived value follows what is
// stored: the header from the stored (new) PO, each line from its own PO
// line. A draft with no lines takes a new PO and its snapshots follow it. A
// blank PO is refused with "Choose a purchase order." and, kept by a step
// save, clears the header's copied values.
func TestGRN_POFixedOnceLines(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	// Another approved PO that differs in vendor, site and project, on a
	// site Suresh may read (PUNE-WH), raised by admin.
	bharat, puneWH := g1MasterID(t, env.DB, "party", "BHARAT"), g1MasterID(t, env.DB, "site", "PUNE-WH")
	other := grntest.POAs(t, env, env.Admin, doctype.StateApproved, map[string]string{
		"vendor_id": bharat, "site_id": puneWH, "project_id": g1MasterID(t, env.DB, "project", "PRJ-TECHPARK"),
	})
	otherHeader := g2Header(t, env, other)

	t.Run("refused with lines", func(t *testing.T) {
		d := g2MustCreate(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", ""), g2Line(po.Lines[1], "10", "", ""))
		g2CheckDerived(t, "draft with lines", env, d.ID)
		before, counts := env.Load(t, goodsreceipt.DocType, d.ID), env.RowCounts(t)
		_, err := g2Save(t, env, env.Suresh, d.ID, otherHeader, g2StoredLines(t, env, d.ID, nil))
		g2WantInvalid(t, "change the PO on a draft with lines", err,
			doctype.FieldError{Field: "purchase_order_id", Message: g2MsgDeleteDraft})
		after := env.Load(t, goodsreceipt.DocType, d.ID)
		if after.Version != before.Version || g1Int(t, after.Header["purchase_order_id"]) != strconv.FormatInt(po.ID, 10) {
			t.Errorf("after the refusal: version %d PO %v, want version %d PO %d (nothing changes)",
				after.Version, after.Header["purchase_order_id"], before.Version, po.ID)
		}
		if got := env.RowCounts(t); got != counts {
			t.Errorf("row counts after the refusal %+v, want %+v", got, counts)
		}
		g2CheckDerived(t, "after the refused save", env, d.ID)
	})

	t.Run("step save keeps no stale value", func(t *testing.T) {
		d := g2MustCreate(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", ""), g2Line(po.Lines[1], "10", "", ""))
		res, err := g2Save(t, env, env.Suresh, d.ID,
			map[string]string{"purchase_order_id": strconv.FormatInt(other.ID, 10), "remarks": "Second truck"},
			g2StoredLines(t, env, d.ID, nil), "remarks")
		g2WantProblems(t, "step save of a changed PO", res, err,
			doctype.FieldError{Field: "purchase_order_id", Message: g2MsgDeleteDraft})
		if got := g2Column(t, env, d.ID, "purchase_order_id"); got != strconv.FormatInt(other.ID, 10) {
			t.Fatalf("stored purchase_order_id = %q, want %d (the step save committed)", got, other.ID)
		}
		// The header follows the stored PO; the lines still follow their
		// own (reference) PO lines.
		g2CheckDerived(t, "after the step save", env, d.ID)
		if v, s := g2Column(t, env, d.ID, "vendor_id"), g2Column(t, env, d.ID, "site_id"); v != bharat || s != puneWH {
			t.Errorf("vendor_id %q site_id %q, want BHARAT %s and PUNE-WH %s from the stored PO", v, s, bharat, puneWH)
		}
		if got := g2LineColumn(t, env, d.ID, "snap_rate"); len(got) != 2 || got[0] != "625000" || got[1] != "960000" {
			t.Errorf("line snap_rate %v, want [625000 960000] from the reference PO's lines", got)
		}
	})

	t.Run("blank PO", func(t *testing.T) {
		counts := env.RowCounts(t)
		h := g2Header(t, env, po)
		h["purchase_order_id"] = ""
		_, err := g2Create(t, env, env.Suresh, h)
		g2WantInvalid(t, "create with a blank PO", err, doctype.FieldError{Field: "purchase_order_id", Message: g2MsgChoosePO})
		if got := env.RowCounts(t); got != counts {
			t.Errorf("row counts after the refused create %+v, want %+v", got, counts)
		}

		d := g2MustCreate(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", ""))
		res, err := g2Save(t, env, env.Suresh, d.ID, map[string]string{"purchase_order_id": "", "remarks": "PO not known"},
			g2StoredLines(t, env, d.ID, nil), "remarks")
		g2WantProblems(t, "step save of a blank PO", res, err, doctype.FieldError{Field: "purchase_order_id", Message: g2MsgChoosePO})
		for _, col := range []string{"vendor_id", "site_id", "project_id", "snap_po_number", "snap_vendor_name"} {
			if got := g2Column(t, env, d.ID, col); !g2Same(got, "") {
				t.Errorf("%s = %q after the PO was blanked, want cleared (CD-302)", col, got)
			}
		}
		g2CheckDerived(t, "after blanking the PO", env, d.ID)
	})

	t.Run("no lines takes the new PO", func(t *testing.T) {
		d := g2MustCreate(t, env, env.Suresh, g2Header(t, env, po))
		g2CheckDerived(t, "draft with no lines", env, d.ID)
		if got := g2Column(t, env, d.ID, "snap_po_number"); got != "PO/26-27/00001" {
			t.Errorf("snap_po_number = %q, want PO/26-27/00001", got)
		}
		if _, err := g2Save(t, env, env.Suresh, d.ID, otherHeader, nil); err != nil {
			t.Fatalf("change the PO on a draft with no lines: %v %+v", err, g1FieldErrors(err))
		}
		g2CheckDerived(t, "after changing the PO", env, d.ID)
		if got := g2Column(t, env, d.ID, "snap_po_number"); got != other.Number {
			t.Errorf("snap_po_number = %q, want %q (the new PO)", got, other.Number)
		}
		if got := g2Column(t, env, d.ID, "vendor_id"); got != bharat {
			t.Errorf("vendor_id = %q, want BHARAT %s", got, bharat)
		}
	})
}

// GRN-005 (GRN spec §4; plan G2 Do 1; proposed decision 15): a line whose
// po_line_id is a line of another approved PO Suresh can read is refused on
// that line's po_line_id with the GRN's own PO number, on create and on a
// save of a draft; the same PO line twice is refused on the second line.
func TestGRN_ForeignLine(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	po2 := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved()) // SHREE, METRO-SITE
	foreign := doctype.FieldError{Field: "po_line_id", Line: 1, Message: "Line 1: This line isn't on PO/26-27/00001."}

	counts := env.RowCounts(t)
	_, err := g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po2.Lines[0], "10", "", ""), g2Line(po.Lines[1], "10", "", ""))
	g2WantInvalid(t, "create with a line of another PO", err, foreign)
	if got := env.RowCounts(t); got != counts {
		t.Errorf("row counts after the refused create %+v, want %+v", got, counts)
	}

	d := g2MustCreate(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", ""), g2Line(po.Lines[1], "10", "", ""))
	before := env.Load(t, goodsreceipt.DocType, d.ID)
	_, err = g2Save(t, env, env.Suresh, d.ID, nil, g2StoredLines(t, env, d.ID, map[int]map[string]string{
		1: {"po_line_id": strconv.FormatInt(po2.Lines[0].ID, 10)},
	}))
	g2WantInvalid(t, "save line 1 with a line of another PO", err, foreign)
	if after := env.Load(t, goodsreceipt.DocType, d.ID); after.Version != before.Version {
		t.Errorf("version after the refusal %d, want %d", after.Version, before.Version)
	}

	_, err = g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", ""), g2Line(po.Lines[0], "50", "", ""))
	g2WantInvalid(t, "the same PO line twice", err,
		doctype.FieldError{Field: "po_line_id", Line: 2, Message: "Line 2: This PO line is already on line 1."})
}

// GRN-006, the refusals (GRN spec §3.1, §4) and G5's before-order warning
// rows (CD-279 (9): one Go test). The clock is 18:31 UTC on 23 October, just after IST
// midnight, so "today" is the IST business date 24-10-2026: a receipt
// dated 24-10-2026 is today and saves, 25-10-2026 is refused, and a blank
// receipt date saves as 2026-10-24. A challan dated after the receipt date
// is refused; the same date saves.
func TestGRN_DateRules(t *testing.T) {
	env := grntest.New(t)
	env.SetNow(t, time.Date(2026, 10, 23, 18, 31, 0, 0, time.UTC))
	po := g2RefPO(t, env)
	line := g2Line(po.Lines[0], "100", "", "")
	with := func(changes map[string]string) map[string]string {
		h := g2Header(t, env, po)
		for k, v := range changes {
			h[k] = v
		}
		return h
	}

	counts := env.RowCounts(t)
	_, err := g2Create(t, env, env.Suresh, with(map[string]string{"receipt_date": "2026-10-25"}), line)
	g2WantInvalid(t, "receipt date 25-10-2026", err, doctype.FieldError{Field: "receipt_date", Message: g2MsgFuture})
	_, err = g2Create(t, env, env.Suresh, with(map[string]string{"receipt_date": "2026-10-24", "challan_date": "2026-10-25"}), line)
	g2WantInvalid(t, "challan after receipt", err, doctype.FieldError{Field: "challan_date", Message: g2MsgChallanAfter})
	if got := env.RowCounts(t); got != counts {
		t.Errorf("row counts after the refusals %+v, want %+v", got, counts)
	}

	for _, c := range []struct {
		what    string
		changes map[string]string
	}{
		{"receipt date today (IST)", map[string]string{"receipt_date": "2026-10-24"}},
		{"challan on the receipt date", map[string]string{"receipt_date": "2026-10-24", "challan_date": "2026-10-24"}},
		{"blank receipt date", map[string]string{"receipt_date": ""}},
	} {
		res, err := g2Create(t, env, env.Suresh, with(c.changes), line)
		if err != nil {
			t.Errorf("%s: %v %+v, want it saved", c.what, err, g1FieldErrors(err))
			continue
		}
		if got := g2Column(t, env, res.ID, "receipt_date"); got != "2026-10-24" {
			t.Errorf("%s: stored receipt_date %q, want 2026-10-24 (today in IST)", c.what, got)
		}
	}

	// G5's warning rows (GRN spec §4): the PO was ordered on 24-10-2026 (IST
	// today), so a receipt dated 23-10-2026 saves and warns with the order
	// date as dd-mm-yyyy; one dated on the order date doesn't warn.
	for _, c := range []struct {
		what, date string
		want       []string
	}{
		{"receipt dated 23-10-2026, before the order", "2026-10-23", []string{g5BeforeOrder("24-10-2026")}},
		{"receipt dated on the order date", "2026-10-24", nil},
	} {
		res, err := g2Create(t, env, env.Suresh, with(map[string]string{"receipt_date": c.date}), line)
		if err != nil {
			t.Errorf("%s: %v %+v, want it saved", c.what, err, g1FieldErrors(err))
			continue
		}
		g5WantList(t, c.what+": warnings", g5Warnings(t, env, res.ID), c.want...)
	}
}

// GRN-009 (GRN spec §3.1, §3.2; CD-302, CD-308): accepted is received −
// rejected per line and accepted_value is Σ accepted × the PO line's net
// rate, before tax. Rejected raised above received is refused on a plain
// save (nothing changes); kept by a step save, that line's accepted_qty is
// 0 and accepted_value is the sum of the lines as stored. Lines each valid
// whose accepted values sum to exactly ₹10,00,00,00,000.00 save; ₹2,500.00
// above it is refused on "lines" with CD-308's text, and a step save
// that keeps it leaves accepted_value cleared.
func TestGRN_AcceptedValue(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	d := g2MustCreate(t, env, env.Suresh, g2Header(t, env, po),
		g2Line(po.Lines[0], "800", "40", "Rusted"), g2Line(po.Lines[1], "50", "", ""))
	if got := g2LineColumn(t, env, d.ID, "accepted_qty"); len(got) != 2 || got[0] != "760000" || got[1] != "50000" {
		t.Errorf("accepted_qty %v, want [760000 50000] (760.000 and 50.000)", got)
	}
	if got := g2Column(t, env, d.ID, "accepted_value"); got != "5230000" {
		t.Errorf("accepted_value = %q, want 5230000 (₹52,300.00 = 760 × ₹62.5000 + 50 × ₹96.0000)", got)
	}
	g2CheckDerived(t, "first save", env, d.ID)

	overReject := map[int]map[string]string{1: {"rejected_qty": "900", "reject_reason": "Rusted"}}
	rejectErr := doctype.FieldError{Field: "rejected_qty", Line: 1, Message: "Line 1: Rejected can't be more than received."}
	_, err := g2Save(t, env, env.Suresh, d.ID, nil, g2StoredLines(t, env, d.ID, overReject))
	g2WantInvalid(t, "rejected 900 of 800", err, rejectErr)
	if got := g2Column(t, env, d.ID, "accepted_value"); got != "5230000" {
		t.Errorf("accepted_value after the refusal = %q, want 5230000 unchanged", got)
	}

	res, err := g2Save(t, env, env.Suresh, d.ID, map[string]string{"remarks": "Recounted"}, g2StoredLines(t, env, d.ID, overReject), "remarks")
	g2WantProblems(t, "step save of rejected 900 of 800", res, err, rejectErr)
	if got := g2LineColumn(t, env, d.ID, "accepted_qty"); len(got) != 2 || got[0] != "0" || got[1] != "50000" {
		t.Errorf("accepted_qty after the step save %v, want [0 50000]", got)
	}
	if got := g2Column(t, env, d.ID, "accepted_value"); got != "480000" {
		t.Errorf("accepted_value after the step save = %q, want 480000 (line 2 alone, as stored)", got)
	}
	g2CheckDerived(t, "after the step save", env, d.ID)

	t.Run("total limit CD-308", func(t *testing.T) {
		// Three lines of 1,000 at ₹25,00,000.00: each PO line is
		// ₹2,50,00,00,000.00, the PO is under the maximum with GST.
		big := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(
			map[string]string{"qty": "1000", "price": "2500000"},
			map[string]string{"item_id": g1MasterID(t, env.DB, "item", "WIRE-18G"), "qty": "1000", "price": "2500000", "hsn_sac": "7217"},
			map[string]string{"item_id": g1MasterID(t, env.DB, "item", "CABLE-4C"), "uom_id": g1MasterID(t, env.DB, "uom", "MTR"),
				"qty": "1000", "price": "2500000", "hsn_sac": "8544"},
		))
		// 2,000 + 1,999 + 1 = 4,000 × ₹25,00,000.00 = ₹10,00,00,00,000.00.
		at := g2MustCreate(t, env, env.Suresh, g2Header(t, env, big),
			g2Line(big.Lines[0], "2000", "", ""), g2Line(big.Lines[1], "1999", "", ""), g2Line(big.Lines[2], "1", "", ""))
		if got := g2Column(t, env, at.ID, "accepted_value"); got != strconv.FormatInt(int64(types.MaxMoney), 10) {
			t.Errorf("accepted_value at the limit = %q, want %d (exactly the maximum saves)", got, types.MaxMoney)
		}
		g2CheckDerived(t, "at the limit", env, at.ID)

		totalErr := doctype.FieldError{Field: "lines", Message: g2MsgTotal}
		counts := env.RowCounts(t)
		_, err := g2Create(t, env, env.Suresh, g2Header(t, env, big),
			g2Line(big.Lines[0], "2000", "", ""), g2Line(big.Lines[1], "1999", "", ""), g2Line(big.Lines[2], "1.001", "", ""))
		g2WantInvalid(t, "total ₹2,500.00 over the maximum", err, totalErr)
		if got := env.RowCounts(t); got != counts {
			t.Errorf("row counts after the refused create %+v, want %+v", got, counts)
		}

		res, err := g2Save(t, env, env.Suresh, at.ID, map[string]string{"remarks": "One more metre"},
			g2StoredLines(t, env, at.ID, map[int]map[string]string{3: {"received_qty": "1.001"}}), "remarks")
		g2WantProblems(t, "step save over the maximum", res, err, totalErr)
		if got := g2Column(t, env, at.ID, "accepted_value"); !g2Same(got, "") {
			t.Errorf("accepted_value over the maximum = %q, want cleared (CD-308, CD-302)", got)
		}
		g2CheckDerived(t, "over the limit", env, at.ID)
	})
}

// GRN-017 (GRN spec §3.2, §4; CD-277 (4); 06-masters.md MST-092): a GRN line
// on a whole-number UoM (BOLT-M12 in NOS) refuses a fractional received or
// rejected quantity with uom.CheckLineQty's text on that column. Each
// uom.RegisterQtyUse entry is wired to the uom_id Validate copies: in one
// database an approved GRN's only fractional quantity is a received 10.5
// KGS, in another a rejected 0.5 KGS (both on a whole PO quantity), and
// MST-092's check finds the GRN in each when KGS is made whole-number.
func TestGRN_WholeNumber(t *testing.T) {
	env := grntest.New(t)
	bolt := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(map[string]string{
		"item_id": g1MasterID(t, env.DB, "item", "BOLT-M12"), "uom_id": g1MasterID(t, env.DB, "uom", "NOS"),
		"qty": "100", "price": "12", "hsn_sac": "7318",
	}))
	_, err := g2Create(t, env, env.Suresh, g2Header(t, env, bolt), g2Line(bolt.Lines[0], "10.5", "", ""))
	g2WantInvalid(t, "received 10.5 NOS", err, doctype.FieldError{Field: "received_qty", Line: 1,
		Message: "Line 1: Quantity 10.5 can't be a fraction for UoM NOS. Enter a whole number."})
	_, err = g2Create(t, env, env.Suresh, g2Header(t, env, bolt), g2Line(bolt.Lines[0], "10", "0.5", "Broken"))
	g2WantInvalid(t, "rejected 0.5 NOS", err, doctype.FieldError{Field: "rejected_qty", Line: 1,
		Message: "Line 1: Quantity 0.5 can't be a fraction for UoM NOS. Enter a whole number."})

	for _, c := range []struct {
		column string
		line   func(po doc.Doc) grntest.Line
	}{
		{"received_qty", func(po doc.Doc) grntest.Line { return grntest.Line{POLineID: po.Lines[0].ID, Received: "10.5"} }},
		{"rejected_qty", func(po doc.Doc) grntest.Line {
			return grntest.Line{POLineID: po.Lines[0].ID, Received: "10", Rejected: "0.5", Reason: "Cut short"}
		}},
	} {
		t.Run(c.column, func(t *testing.T) {
			// A separate database for each registered use.
			w := grntest.New(t)
			po := purchasetest.NewOrder(t, w.Env, purchasetest.StopAtApproved()) // 100 KGS, whole
			grn := w.GRN(t, w.Suresh, po.ID, []grntest.Line{c.line(po)}, doctype.StateApproved)
			if grn.Number != "GR/26-27/00001" {
				t.Fatalf("GRN number %q, want GR/26-27/00001", grn.Number)
			}
			svc := uom.New(mstkit.Env{DB: w.DB, Auth: w.Auth, Engine: w.Engine, Auditor: audit.Default, Clock: w.Clock})
			id, _ := strconv.ParseInt(g1MasterID(t, w.DB, "uom", "KGS"), 10, 64)
			var before uom.UoM
			if err := w.DB.Read(t.Context(), func(tx *db.Tx) error {
				var err error
				before, err = svc.Get(t.Context(), tx, id)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			_, err := svc.Update(t.Context(), w.Admin, before.ID, before.Version,
				uom.Input{Code: before.Code, Name: before.Name, UQC: before.UQC, WholeNumber: true})
			want := doctype.FieldError{Field: "whole_number", Message: "KGS is used with fractional quantities (e.g. GR/26-27/00001). Create a new UoM instead."}
			var ve *mstkit.ValidationError
			if !errors.As(err, &ve) || len(ve.Fields) != 1 || ve.Fields[0] != want {
				t.Errorf("make KGS whole-number over the GRN's %s: err = %v, want %+v (MST-092)", c.column, err, want)
			}
		})
	}
}

// GRN-033 (GRN spec §3.1, §8): MS-ANGLE-50, its UoM KGS and the vendor SHREE
// are disabled after the reference PO is approved; Suresh still creates and
// submits a GRN for it, it is approved, and Validate still copies the
// disabled vendor and unit from the PO (System fields, never master-checked).
func TestGRN_DisabledMasterStillReceives(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	kit := mstkit.Env{DB: env.DB, Auth: env.Auth, Engine: env.Engine, Auditor: audit.Default, Clock: env.Clock}
	version := func(table, code string) (int64, int) {
		t.Helper()
		var id int64
		var v int
		if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT id, version FROM `+table+` WHERE code = ?`, code).Scan(&id, &v); err != nil { //nolint:gosec // G202: fixed table names
			t.Fatalf("read %s %s: %v", table, code, err)
		}
		return id, v
	}
	ctx := t.Context()
	itemID, iv := version("item", "MS-ANGLE-50")
	if err := item.New(kit).SetDisabled(ctx, env.Admin, itemID, iv, true); err != nil {
		t.Fatalf("disable MS-ANGLE-50: %v", err)
	}
	kgs, uv := version("uom", "KGS")
	if err := uom.New(kit).SetDisabled(ctx, env.Admin, kgs, uv, true); err != nil {
		t.Fatalf("disable KGS: %v", err)
	}
	shree, pv := version("party", "SHREE")
	if err := party.New(kit).SetDisabled(ctx, env.Admin, shree, pv, true); err != nil {
		t.Fatalf("disable SHREE: %v", err)
	}

	grn := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "100"}, {POLineID: po.Lines[1].ID, Received: "10"}}, doctype.StateApproved)
	if grn.State != doctype.StateApproved {
		t.Fatalf("GRN state %s, want approved", grn.State)
	}
	if got := g2Column(t, env, grn.ID, "vendor_id"); got != strconv.FormatInt(shree, 10) {
		t.Errorf("vendor_id = %q, want the disabled SHREE %d", got, shree)
	}
	if got := g2LineColumn(t, env, grn.ID, "uom_id"); len(got) != 2 || got[0] != strconv.FormatInt(kgs, 10) || got[1] != strconv.FormatInt(kgs, 10) {
		t.Errorf("line uom_id %v, want the disabled KGS %d on both", got, kgs)
	}
	g2CheckDerived(t, "GRN on disabled masters", env, grn.ID)
}

// GRN-043 (research Gap 3, S47: ERPNext controllers/status_updater.py
// over-allowance; GRN spec §4; CD-272): accepted above twice the PO line's
// ordered quantity is refused on received_qty, with the quantities as
// types.FormatQtyUoM shows them; exactly twice saves (G5 adds its warning
// row). Accepted is what is compared: 3,000 received with 700 rejected
// (2,300 accepted) saves, 3,000 with 599.99 rejected (2,400.01) doesn't. Line
// 2 (50 kg ordered) shows {n} and {y} come from that line.
func TestGRN_TwiceOrderedStop(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	twice1 := doctype.FieldError{Field: "received_qty", Line: 1,
		Message: "Line 1: Accepted 2,400.01 KGS is more than twice the 1,200 KGS ordered. Check the quantity."}

	counts := env.RowCounts(t)
	_, err := g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "2400.010", "", ""))
	g2WantInvalid(t, "received 2,400.010 of 1,200 ordered", err, twice1)
	_, err = g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "3000", "599.99", "Bent"))
	g2WantInvalid(t, "accepted 2,400.01 of 1,200 ordered", err, twice1)
	_, err = g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", ""), g2Line(po.Lines[1], "100.001", "", ""))
	g2WantInvalid(t, "line 2 received 100.001 of 50 ordered", err, doctype.FieldError{Field: "received_qty", Line: 2,
		Message: "Line 2: Accepted 100.001 KGS is more than twice the 50 KGS ordered. Check the quantity."})
	if got := env.RowCounts(t); got != counts {
		t.Errorf("row counts after the refusals %+v, want %+v", got, counts)
	}

	for _, c := range []struct {
		what, received, rejected, accepted string
	}{
		{"exactly twice", "2400", "", "2400000"},
		{"2,300 accepted of 3,000 received", "3000", "700", "2300000"},
	} {
		res, err := g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], c.received, c.rejected, "Bent"))
		if err != nil {
			t.Errorf("%s: %v %+v, want it saved", c.what, err, g1FieldErrors(err))
			continue
		}
		if got := g2LineColumn(t, env, res.ID, "accepted_qty"); len(got) != 1 || got[0] != c.accepted {
			t.Errorf("%s: accepted_qty %v, want [%s]", c.what, got, c.accepted)
		}
	}

	// G5's warning row (GRN spec §4, CD-234): exactly twice saves and carries
	// the over-delivery warning (drafts don't count toward pending).
	twice := g2MustCreate(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "2400", "", ""))
	g5WantList(t, "exactly twice: warnings", g5Warnings(t, env, twice.ID),
		g5Over(1, "2,400 KGS", "1,200 KGS", "PO/26-27/00001"))
}
