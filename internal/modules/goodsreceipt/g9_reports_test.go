package goodsreceipt_test

import (
	"bytes"
	"encoding/csv"
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN G9 (GRN spec §12; plan G9; GRN-031, GRN-032): Bills to enter and
// Received items through goodsreceipt.Service on the real auth.Service.
// The data is grntest.ReportWorld, made through the engine; its comment
// lists every GRN, its state and its date.

// g9Money parses a money literal of a table; a bad literal is a test bug.
func g9Money(s string) types.Money {
	m, err := types.ParseMoney(s)
	if err != nil {
		panic("g9Money(" + s + "): " + err.Error())
	}
	return m
}

// g9Rate parses a rate literal of a table; a bad literal is a test bug.
func g9Rate(s string) types.Rate {
	r, err := types.ParseRate(s)
	if err != nil {
		panic("g9Rate(" + s + "): " + err.Error())
	}
	return r
}

// g9Bills is BillsReport as p with f; an error stops the test.
func g9Bills(t *testing.T, env *grntest.Env, p auth.Principal, f goodsreceipt.BillsFilter) []goodsreceipt.BillRow {
	t.Helper()
	rows, err := env.Service.BillsReport(t.Context(), p, f)
	if err != nil {
		t.Fatalf("BillsReport as %s with %+v: %v", p.Name, f, err)
	}
	return rows
}

// g9Items is ItemsReport as p with f; an error stops the test.
func g9Items(t *testing.T, env *grntest.Env, p auth.Principal, f goodsreceipt.ItemsFilter) []goodsreceipt.ItemRow {
	t.Helper()
	rows, err := env.Service.ItemsReport(t.Context(), p, f)
	if err != nil {
		t.Fatalf("ItemsReport as %s with %+v: %v", p.Name, f, err)
	}
	return rows
}

// g9BillIDs is the sorted GRN ids of rows.
func g9BillIDs(rows []goodsreceipt.BillRow) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	slices.Sort(out)
	return out
}

// g9LineIDs is the sorted GRN line ids of rows.
func g9LineIDs(rows []goodsreceipt.ItemRow) []int64 {
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.LineID)
	}
	slices.Sort(out)
	return out
}

// g9IDs is ids sorted.
func g9IDs(ids ...int64) []int64 {
	slices.Sort(ids)
	return ids
}

// GRN-031 (GRN spec §12, §16; plan G9; AUTH-110; CD-233): Bills to enter has
// one row per approved or closed GRN the viewer can read. The state list is
// pinned by stored values: approved and closed are rows; draft, pending
// approval, rejected and cancelled never are. Suresh (Stores, sites PUNE-WH
// and METRO-SITE) never sees the TECH-SITE GRN, on the real auth.Service.
// No bill details yet, vendor and month narrow the rows; the month is the
// IST receipt date (G3 was made at 31-10-2026 18:31 UTC, receipt date
// 01-11-2026: October's rows leave it out). Rows run oldest receipt first
// (UX §5.11, approved under CD-282).
func TestGRN_BillsReport(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	metro := g9IDs(w.G1.ID, w.G2.ID, w.GC.ID, w.G3.ID)

	t.Run("approved and closed, scoped", func(t *testing.T) {
		if got, want := g9BillIDs(g9Bills(t, env, env.Admin, goodsreceipt.BillsFilter{})), g9IDs(w.G1.ID, w.G2.ID, w.GT.ID, w.GC.ID, w.G3.ID); !slices.Equal(got, want) {
			t.Errorf("Admin's rows = %v, want %v (approved and closed; never draft %d, cancelled %d, pending %d, rejected %d)",
				got, want, w.Draft.ID, w.Cancelled.ID, w.Pending.ID, w.Rejected.ID)
		}
		for _, p := range []auth.Principal{env.Suresh, env.Priya} {
			if got := g9BillIDs(g9Bills(t, env, p, goodsreceipt.BillsFilter{})); !slices.Equal(got, metro) {
				t.Errorf("%s's rows = %v, want %v (never the TECH-SITE GRN %d)", p.Name, got, metro, w.GT.ID)
			}
		}
	})

	t.Run("oldest receipt first", func(t *testing.T) {
		rows := g9Bills(t, env, env.Suresh, goodsreceipt.BillsFilter{})
		for i := 1; i < len(rows); i++ {
			if rows[i].ReceiptDate.Compare(rows[i-1].ReceiptDate) < 0 {
				t.Errorf("row %d (%s) is before row %d (%s): want oldest receipt first", i, rows[i].ReceiptDate.ISO(), i-1, rows[i-1].ReceiptDate.ISO())
			}
		}
		if len(rows) == 0 || rows[len(rows)-1].ID != w.G3.ID {
			t.Errorf("last row = %+v, want G3 (%d), the newest receipt", rows, w.G3.ID)
		}
	})

	t.Run("filters", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			f    goodsreceipt.BillsFilter
			want []int64
		}{
			{"no bill details yet", goodsreceipt.BillsFilter{NoBill: true}, g9IDs(w.G2.ID, w.GC.ID, w.G3.ID)},
			{"vendor BHARAT", goodsreceipt.BillsFilter{VendorID: w.Bharat}, g9IDs(w.G2.ID)},
			{"vendor SHREE", goodsreceipt.BillsFilter{VendorID: w.Shree}, g9IDs(w.G1.ID, w.GC.ID, w.G3.ID)},
			{"month October 2026", goodsreceipt.BillsFilter{Month: "2026-10"}, g9IDs(w.G1.ID, w.G2.ID, w.GC.ID)},
			{"month November 2026 (IST date of 31-10 18:31 UTC)", goodsreceipt.BillsFilter{Month: "2026-11"}, g9IDs(w.G3.ID)},
			{"month September 2026", goodsreceipt.BillsFilter{Month: "2026-09"}, g9IDs()},
			{"all three together", goodsreceipt.BillsFilter{NoBill: true, VendorID: w.Shree, Month: "2026-10"}, g9IDs(w.GC.ID)},
		} {
			if got := g9BillIDs(g9Bills(t, env, env.Suresh, tc.f)); !slices.Equal(got, tc.want) {
				t.Errorf("%s: Suresh's rows = %v, want %v", tc.name, got, tc.want)
			}
		}
	})
}

// GRN-032 for Bills to enter (GRN spec §12; lessons §11 column read-back):
// every column of G1 reads back its value, one assertion per column. The
// vendor, GSTIN and PO no. are the GRN's own snapshots: SHREE was renamed
// after the GRN, and the report still says Shree Steel Traders (plan G9).
// G2 has no bill details and no photos: blank invoice columns, 0 photos.
func TestGRN_BillsReportColumns(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	rows := g9Bills(t, env, env.Suresh, goodsreceipt.BillsFilter{})
	byID := map[int64]goodsreceipt.BillRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	g1, ok := byID[w.G1.ID]
	if !ok {
		t.Fatalf("no row for G1 (%d) in %+v", w.G1.ID, rows)
	}
	if g1.Number != "GR/26-27/00001" || g1.Number != w.G1.Number {
		t.Errorf("GRN no. = %q, want %q", g1.Number, w.G1.Number)
	}
	if g1.ReceiptDate != grntest.Day(24, time.October) {
		t.Errorf("receipt date = %s, want 2026-10-24", g1.ReceiptDate.ISO())
	}
	if g1.Vendor != "Shree Steel Traders" {
		t.Errorf("vendor = %q, want the snapshot %q (SHREE was renamed after the GRN)", g1.Vendor, "Shree Steel Traders")
	}
	if g1.GSTIN != "27AABCS1429B1ZU" {
		t.Errorf("GSTIN = %q, want 27AABCS1429B1ZU", g1.GSTIN)
	}
	if g1.PONumber != w.RefPO.Number {
		t.Errorf("PO no. = %q, want %q", g1.PONumber, w.RefPO.Number)
	}
	if g1.ChallanNo != "DC-1042" {
		t.Errorf("challan no. = %q, want DC-1042", g1.ChallanNo)
	}
	if g1.InvoiceNo != grntest.ReportInvoice {
		t.Errorf("invoice no. = %q, want %q", g1.InvoiceNo, grntest.ReportInvoice)
	}
	if g1.InvoiceDate != grntest.Day(24, time.October) {
		t.Errorf("invoice date = %s, want 2026-10-24", g1.InvoiceDate.ISO())
	}
	if !g1.InvoiceTotal.Valid || g1.InvoiceTotal.V != g9Money("34426.50") {
		t.Errorf("invoice total = %+v, want ₹34,426.50", g1.InvoiceTotal)
	}
	if g1.AcceptedValue != g9Money("29175") {
		t.Errorf("accepted value = %s, want ₹29,175.00 (390 kg × ₹62.50 + 50 kg × ₹96.00)", types.FormatMoney(g1.AcceptedValue))
	}
	if g1.Photos != 2 {
		t.Errorf("photos = %d, want 2", g1.Photos)
	}

	g2, ok := byID[w.G2.ID]
	if !ok {
		t.Fatalf("no row for G2 (%d)", w.G2.ID)
	}
	if g2.Vendor != grntest.ReportVendor || g2.GSTIN != "29AABCT1332L1ZA" || g2.PONumber != w.BharatPO.Number {
		t.Errorf("G2 vendor, GSTIN, PO no. = %q, %q, %q; want %q, 29AABCT1332L1ZA, %q", g2.Vendor, g2.GSTIN, g2.PONumber, grntest.ReportVendor, w.BharatPO.Number)
	}
	if g2.InvoiceNo != "" || g2.InvoiceDate != (types.Date{}) || g2.InvoiceTotal.Valid {
		t.Errorf("G2 bill columns = %q, %s, %+v; want blank (no bill details)", g2.InvoiceNo, g2.InvoiceDate.ISO(), g2.InvoiceTotal)
	}
	if g2.Photos != 0 {
		t.Errorf("G2 photos = %d, want 0", g2.Photos)
	}
	if g2.AcceptedValue != g9Money("1250") {
		t.Errorf("G2 accepted value = %s, want ₹1,250.00", types.FormatMoney(g2.AcceptedValue))
	}
	if g3 := byID[w.G3.ID]; g3.ReceiptDate != grntest.Day(1, time.November) {
		t.Errorf("G3 receipt date = %s, want 2026-11-01 (the IST date at 31-10 18:31 UTC)", g3.ReceiptDate.ISO())
	}
}

// GRN-032 (GRN spec §12, §16; plan G9; CD-282 Q11; AUTH-110): Received items
// has one row per line of an approved or closed GRN the viewer can read
// (never draft, pending approval, rejected or cancelled lines; never the
// TECH-SITE GRN's for Suresh, on the real auth.Service). Rejected only keeps
// lines with rejected above 0; the item filter keeps that item's lines; the
// vendor and month filters narrow by the GRN (the month is the IST receipt
// date). Rows run newest receipt first (UX §5.12, approved under CD-282).
func TestGRN_ItemsReport(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	g1a, g1w := w.G1.Lines[0].ID, w.G1.Lines[1].ID
	g2l, gtl, gcl, g3l := w.G2.Lines[0].ID, w.GT.Lines[0].ID, w.GC.Lines[0].ID, w.G3.Lines[0].ID

	t.Run("approved and closed lines, scoped", func(t *testing.T) {
		if got, want := g9LineIDs(g9Items(t, env, env.Admin, goodsreceipt.ItemsFilter{})), g9IDs(g1a, g1w, g2l, gtl, gcl, g3l); !slices.Equal(got, want) {
			t.Errorf("Admin's lines = %v, want %v (never the draft, cancelled, pending or rejected GRN's: %d, %d, %d, %d)",
				got, want, w.Draft.Lines[0].ID, w.Cancelled.Lines[0].ID, w.Pending.Lines[0].ID, w.Rejected.Lines[0].ID)
		}
		if got, want := g9LineIDs(g9Items(t, env, env.Suresh, goodsreceipt.ItemsFilter{})), g9IDs(g1a, g1w, g2l, gcl, g3l); !slices.Equal(got, want) {
			t.Errorf("Suresh's lines = %v, want %v (never the TECH-SITE GRN's line %d)", got, want, gtl)
		}
	})

	t.Run("newest receipt first", func(t *testing.T) {
		rows := g9Items(t, env, env.Suresh, goodsreceipt.ItemsFilter{})
		for i := 1; i < len(rows); i++ {
			if rows[i].Date.Compare(rows[i-1].Date) > 0 {
				t.Errorf("row %d (%s) is after row %d (%s): want newest receipt first", i, rows[i].Date.ISO(), i-1, rows[i-1].Date.ISO())
			}
		}
		if len(rows) == 0 || rows[0].LineID != g3l {
			t.Errorf("first row = %+v, want G3's line (%d), the newest receipt", rows, g3l)
		}
	})

	t.Run("filters", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			f    goodsreceipt.ItemsFilter
			want []int64
		}{
			{"rejected only", goodsreceipt.ItemsFilter{RejectedOnly: true}, g9IDs(g1a)},
			{"item WIRE-18G", goodsreceipt.ItemsFilter{ItemID: w.Wire}, g9IDs(g1w, g3l)},
			{"item MS-ANGLE-50", goodsreceipt.ItemsFilter{ItemID: w.Angle}, g9IDs(g1a, g2l, gcl)},
			{"vendor BHARAT", goodsreceipt.ItemsFilter{VendorID: w.Bharat}, g9IDs(g2l)},
			{"vendor SHREE", goodsreceipt.ItemsFilter{VendorID: w.Shree}, g9IDs(g1a, g1w, gcl, g3l)},
			{"month October 2026", goodsreceipt.ItemsFilter{Month: "2026-10"}, g9IDs(g1a, g1w, g2l, gcl)},
			{"month November 2026 (IST date of 31-10 18:31 UTC)", goodsreceipt.ItemsFilter{Month: "2026-11"}, g9IDs(g3l)},
			{"rejected only, BHARAT", goodsreceipt.ItemsFilter{RejectedOnly: true, VendorID: w.Bharat}, g9IDs()},
			{"WIRE-18G in October", goodsreceipt.ItemsFilter{ItemID: w.Wire, Month: "2026-10"}, g9IDs(g1w)},
		} {
			if got := g9LineIDs(g9Items(t, env, env.Suresh, tc.f)); !slices.Equal(got, tc.want) {
				t.Errorf("%s: Suresh's lines = %v, want %v", tc.name, got, tc.want)
			}
		}
	})
}

// GRN-032 (GRN spec §12; lessons §11 column read-back; plan G9 "labels come
// from the GRN's own snapshots"): every column of G1's rejected line reads
// back its value, one assertion per column. SHREE's name, MS-ANGLE-50's
// code, name and HSN and the KGS unit's code changed after the GRN; the row
// keeps the snapshots. G1's second line has its own rate and line accepted
// value, never the GRN's.
func TestGRN_ItemsReportColumns(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	byLine := map[int64]goodsreceipt.ItemRow{}
	rows := g9Items(t, env, env.Suresh, goodsreceipt.ItemsFilter{})
	for _, r := range rows {
		byLine[r.LineID] = r
	}
	r, ok := byLine[w.G1.Lines[0].ID]
	if !ok {
		t.Fatalf("no row for G1's line 1 (%d) in %+v", w.G1.Lines[0].ID, rows)
	}
	if r.GRNID != w.G1.ID {
		t.Errorf("GRN id = %d, want %d", r.GRNID, w.G1.ID)
	}
	if r.Number != w.G1.Number {
		t.Errorf("GRN no. = %q, want %q", r.Number, w.G1.Number)
	}
	if r.Date != grntest.Day(24, time.October) {
		t.Errorf("date = %s, want 2026-10-24", r.Date.ISO())
	}
	if r.Vendor != "Shree Steel Traders" {
		t.Errorf("vendor = %q, want the snapshot Shree Steel Traders", r.Vendor)
	}
	if r.PONumber != w.RefPO.Number {
		t.Errorf("PO no. = %q, want %q", r.PONumber, w.RefPO.Number)
	}
	if r.ItemCode != "MS-ANGLE-50" {
		t.Errorf("item code = %q, want the snapshot MS-ANGLE-50", r.ItemCode)
	}
	if r.ItemName != "MS Angle 50x50x5" {
		t.Errorf("item name = %q, want the snapshot MS Angle 50x50x5", r.ItemName)
	}
	if r.HSN != "7216" {
		t.Errorf("HSN = %q, want the snapshot 7216", r.HSN)
	}
	if r.Received != g3Q("400") {
		t.Errorf("received = %s, want 400", r.Received.Plain())
	}
	if r.Accepted != g3Q("390") {
		t.Errorf("accepted = %s, want 390", r.Accepted.Plain())
	}
	if r.Rejected != g3Q("10") {
		t.Errorf("rejected = %s, want 10", r.Rejected.Plain())
	}
	if r.Reason != "Bent pieces" {
		t.Errorf("reason = %q, want Bent pieces", r.Reason)
	}
	if r.UoM != "KGS" {
		t.Errorf("UoM = %q, want the snapshot KGS", r.UoM)
	}
	if r.Rate != g9Rate("62.5") {
		t.Errorf("rate = %s, want 62.5", r.Rate.Plain())
	}
	if r.AcceptedValue != g9Money("24375") {
		t.Errorf("accepted value = %s, want ₹24,375.00 (390 kg × ₹62.50)", types.FormatMoney(r.AcceptedValue))
	}

	wire, ok := byLine[w.G1.Lines[1].ID]
	if !ok {
		t.Fatalf("no row for G1's line 2 (%d)", w.G1.Lines[1].ID)
	}
	if wire.ItemCode != "WIRE-18G" || wire.HSN != "7217" {
		t.Errorf("line 2 item, HSN = %q, %q; want WIRE-18G, 7217", wire.ItemCode, wire.HSN)
	}
	if wire.Rate != g9Rate("96") || wire.AcceptedValue != g9Money("4800") {
		t.Errorf("line 2 rate, accepted value = %s, %s; want 96, ₹4,800.00", wire.Rate.Plain(), types.FormatMoney(wire.AcceptedValue))
	}
	if wire.Rejected != 0 || wire.Reason != "" {
		t.Errorf("line 2 rejected, reason = %s, %q; want 0 and blank", wire.Rejected.Plain(), wire.Reason)
	}
	if g2 := byLine[w.G2.Lines[0].ID]; g2.Vendor != grntest.ReportVendor || g2.PONumber != w.BharatPO.Number {
		t.Errorf("G2's line vendor, PO no. = %q, %q; want %q, %q", g2.Vendor, g2.PONumber, grntest.ReportVendor, w.BharatPO.Number)
	}
	if g3 := byLine[w.G3.Lines[0].ID]; g3.Date != grntest.Day(1, time.November) {
		t.Errorf("G3's line date = %s, want 2026-11-01 (the IST date at 31-10 18:31 UTC)", g3.Date.ISO())
	}
}

// CD-297 (M0-33, on main), CD-233; plan G9 Do 5: the generic GRN register
// CSV (/doc/goodsreceipt.Receipt/export.csv, kernel csvio) exports
// purchase_order_id as the PO's number for a viewer who can read the PO,
// and blank for one who can't (a Receipt Reader: read and export on the GRN,
// nothing on the PO), on the real auth.Service.
func TestGRN_RegisterCSVPurchaseOrder(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	grn := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "10"}}, doctype.StateApproved)
	reader := env.ReceiptReader(t)
	dt, _ := doctype.Lookup(goodsreceipt.DocType)
	for _, tc := range []struct {
		who  string
		p    auth.Principal
		want string
	}{
		{"Priya (reads the PO, export)", env.Priya, po.Number},
		{"Receipt Reader (can't read the PO)", reader, ""},
	} {
		var buf bytes.Buffer
		if _, err := csvio.NewExporter(env.DB, env.Auth).Export(t.Context(), &buf, tc.p, dt, list.Query{}); err != nil {
			t.Fatalf("%s: export: %v", tc.who, err)
		}
		recs, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(buf.Bytes(), []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
		if err != nil || len(recs) != 2 {
			t.Fatalf("%s: CSV = %q, %v; want a header and one row", tc.who, recs, err)
		}
		col := slices.Index(recs[0], "Purchase order")
		num := slices.Index(recs[0], "GRN number")
		if col < 0 || num < 0 {
			t.Fatalf("%s: header %q has no Purchase order or GRN number column", tc.who, recs[0])
		}
		if recs[1][num] != grn.Number {
			t.Errorf("%s: row's GRN number = %q, want %q", tc.who, recs[1][num], grn.Number)
		}
		if recs[1][col] != tc.want {
			t.Errorf("%s: Purchase order cell = %q, want %q", tc.who, recs[1][col], tc.want)
		}
	}
}
