package grnweb_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	grnweb "github.com/vardhan/internaltools/internal/modules/goodsreceipt/web"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN G9 (GRN spec §12; plan G9; GRN-031, GRN-032; CD-145): the
// reports' CSVs, GET /reports/grn/bills/export.csv and
// /reports/grn/items/export.csv. The routes' read guard is internal/app's
// (Guard, TestGRNWeb_Guards); here the handler is called on a bare router
// with the principal in the context, as the app's guard leaves it, and it
// checks export itself. The cells read as the purchase pending CSV's do
// (csvio.FormatCell: ISO dates, plain numbers, formula-escaped text), after
// a BOM and a header of spec §12's columns in its order. The data is
// grntest.ReportWorld.

const (
	g9wBillsPath = "/reports/grn/bills/export.csv"
	g9wItemsPath = "/reports/grn/items/export.csv"
	// g9wExportDenied is AUTH's export refusal for the Goods Receipt
	// (AUTH-080), copied exactly (CD-142).
	g9wExportDenied = "You don't have permission to export Goods Receipt. Ask your administrator for a role that allows it."
)

// The CSV headers: spec §12's columns, in its order (UX §5.11, §5.12: the
// CSV has every column, one each; Received items' item is one column).
var (
	g9wBillsHeader = []string{"GRN no.", "Receipt date", "Vendor", "GSTIN", "PO no.", "Challan no.", "Invoice no.", "Invoice date", "Invoice total", "Accepted value", "Photos"}
	g9wItemsHeader = []string{"GRN no.", "Date", "Vendor", "PO no.", "Item", "HSN", "Received", "Accepted", "Rejected", "Reason", "UoM", "Rate", "Accepted value"}
)

// g9wBOM is the UTF-8 byte order mark csvio's exports start with.
var g9wBOM = []byte{0xEF, 0xBB, 0xBF}

// g9wGet sends GET path as p to the module routes on a bare router.
func g9wGet(t *testing.T, env *grntest.Env, p auth.Principal, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	grnweb.Routes(grnweb.Deps{Service: env.Service})(r)
	req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), p), http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// g9wRecords checks a CSV answer (200, text/csv, a BOM first, header first)
// and returns its data records.
func g9wRecords(t *testing.T, what string, rec *httptest.ResponseRecorder, header []string) [][]string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s = %d %q, want 200", what, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("%s Content-Type = %q, want text/csv", what, ct)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), g9wBOM) {
		t.Errorf("%s doesn't start with the UTF-8 BOM: %q", what, rec.Body.Bytes()[:min(rec.Body.Len(), 8)])
	}
	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(rec.Body.Bytes(), g9wBOM))).ReadAll()
	if err != nil {
		t.Fatalf("%s: parse CSV: %v\n%s", what, err, rec.Body.String())
	}
	if len(rows) == 0 || !slices.Equal(rows[0], header) {
		t.Fatalf("%s header = %q, want %q", what, rows, header)
	}
	for _, r := range rows[1:] {
		if len(r) != len(header) {
			t.Errorf("%s row %q has %d cells, want %d", what, r, len(r), len(header))
		}
	}
	return rows[1:]
}

// g9wFirst is the sorted first cells (GRN no.) of rows.
func g9wFirst(rows [][]string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r[0])
	}
	slices.Sort(out)
	return out
}

// g9wSorted is s sorted.
func g9wSorted(s ...string) []string {
	slices.Sort(s)
	return s
}

// g9wWantRow asserts got against want cell by cell, one assertion per
// column, naming the column.
func g9wWantRow(t *testing.T, what string, header, got, want []string) {
	t.Helper()
	for i, col := range header {
		if i >= len(got) {
			t.Errorf("%s: no %s cell in %q", what, col, got)
			continue
		}
		if got[i] != want[i] {
			t.Errorf("%s: %s = %q, want %q", what, col, got[i], want[i])
		}
	}
}

func g9wQty(t *testing.T, s string) string {
	t.Helper()
	q, err := types.ParseQty(s)
	if err != nil {
		t.Fatalf("ParseQty(%q): %v", s, err)
	}
	return q.Plain()
}

func g9wRate(t *testing.T, s string) string {
	t.Helper()
	r, err := types.ParseRate(s)
	if err != nil {
		t.Fatalf("ParseRate(%q): %v", s, err)
	}
	return r.Plain()
}

func g9wMoney(t *testing.T, s string) string {
	t.Helper()
	m, err := types.ParseMoney(s)
	if err != nil {
		t.Fatalf("ParseMoney(%q): %v", s, err)
	}
	return m.Plain()
}

func g9wID(id int64) string { return strconv.FormatInt(id, 10) }

// GRN-031 (GRN spec §12; AUTH-090, AUTH-080; CD-145): each CSV checks export
// in the handler. Suresh (Stores: read, no export) gets AUTH's 403 with its
// export text and no CSV, with or without filters; Priya (Purchase User:
// read and export) gets both CSVs.
func TestGRNWeb_ReportsCSVExportRight(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	for _, path := range []string{g9wBillsPath, g9wBillsPath + "?nobill=1", g9wItemsPath, g9wItemsPath + "?rejected=1"} {
		rec := g9wGet(t, env, env.Suresh, path)
		body := html.UnescapeString(rec.Body.String())
		if rec.Code != http.StatusForbidden || !strings.Contains(body, g9wExportDenied) {
			t.Errorf("GET %s as Suresh = %d %q, want 403 with %q", path, rec.Code, body, g9wExportDenied)
		}
		if strings.Contains(body, "GRN no.") || strings.Contains(body, w.G1.Number) || strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
			t.Errorf("GET %s as Suresh leaked the CSV: %q", path, body)
		}
	}
	g9wRecords(t, "Priya's bills CSV", g9wGet(t, env, env.Priya, g9wBillsPath), g9wBillsHeader)
	g9wRecords(t, "Priya's items CSV", g9wGet(t, env, env.Priya, g9wItemsPath), g9wItemsHeader)
}

// GRN-031, GRN-032 (GRN spec §12; CD-233, AUTH-110; lessons §11): the Bills
// to enter CSV has Priya's rows only (PRJ-METRO: never the TECH-SITE GRN;
// never a draft, pending, rejected or cancelled GRN). G1's row reads back
// every column, one assertion each, from the GRN's snapshots (SHREE was
// renamed after the GRN). G2's vendor, with a comma, quotes and a leading
// "=", is formula-escaped and CSV-quoted; its bill columns are blank and
// photos 0. The query string's nobill=1, vendor and month narrow the rows.
func TestGRNWeb_BillsCSV(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	rec := g9wGet(t, env, env.Priya, g9wBillsPath)
	rows := g9wRecords(t, "Priya's bills CSV", rec, g9wBillsHeader)
	if got, want := g9wFirst(rows), g9wSorted(w.G1.Number, w.G2.Number, w.GC.Number, w.G3.Number); !slices.Equal(got, want) {
		t.Errorf("Priya's rows = %q, want %q (never %s, TECH-SITE)", got, want, w.GT.Number)
	}
	byNo := map[string][]string{}
	for _, r := range rows {
		byNo[r[0]] = r
	}
	g9wWantRow(t, "G1's row", g9wBillsHeader, byNo[w.G1.Number], []string{
		w.G1.Number, "2026-10-24", "Shree Steel Traders", "27AABCS1429B1ZU", w.RefPO.Number, "DC-1042",
		grntest.ReportInvoice, "2026-10-24", g9wMoney(t, "34426.50"), g9wMoney(t, "29175"), "2",
	})
	g9wWantRow(t, "G2's row", g9wBillsHeader, byNo[w.G2.Number], []string{
		w.G2.Number, "2026-10-24", csvio.EscapeFormula(grntest.ReportVendor), "29AABCT1332L1ZA", w.BharatPO.Number, "DC-1042",
		"", "", "", g9wMoney(t, "1250"), "0",
	})
	if !strings.HasPrefix(csvio.EscapeFormula(grntest.ReportVendor), "'") {
		t.Fatalf("csvio.EscapeFormula(%q) doesn't escape the leading =; the escaping row tests nothing", grntest.ReportVendor)
	}
	quoted := `"` + strings.ReplaceAll(csvio.EscapeFormula(grntest.ReportVendor), `"`, `""`) + `"`
	if !strings.Contains(rec.Body.String(), ","+quoted+",") {
		t.Errorf("body doesn't carry G2's vendor CSV-quoted as %s:\n%s", quoted, rec.Body.String())
	}

	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"?nobill=1", g9wSorted(w.G2.Number, w.GC.Number, w.G3.Number)},
		{"?vendor=" + g9wID(w.Bharat), g9wSorted(w.G2.Number)},
		{"?month=2026-11", g9wSorted(w.G3.Number)},
		{"?month=2026-10", g9wSorted(w.G1.Number, w.G2.Number, w.GC.Number)},
		{"?nobill=1&vendor=" + g9wID(w.Shree) + "&month=2026-10", g9wSorted(w.GC.Number)},
	} {
		rows := g9wRecords(t, "bills CSV"+tc.query, g9wGet(t, env, env.Priya, g9wBillsPath+tc.query), g9wBillsHeader)
		if got := g9wFirst(rows); !slices.Equal(got, tc.want) {
			t.Errorf("bills CSV%s rows = %q, want %q", tc.query, got, tc.want)
		}
	}
}

// GRN-032 (GRN spec §12; CD-282 Q11; lessons §11): the Received items CSV
// has one row per line of Priya's approved and closed GRNs. G1's rejected
// line reads back every column, one assertion each, from the snapshots
// (MS-ANGLE-50's code, name and HSN and the KGS unit's code changed after
// the GRN); the item is one column, "{name} ({code})". The query string's
// rejected=1, item, vendor and month narrow the rows.
func TestGRNWeb_ItemsCSV(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	rows := g9wRecords(t, "Priya's items CSV", g9wGet(t, env, env.Priya, g9wItemsPath), g9wItemsHeader)
	if got, want := g9wFirst(rows), g9wSorted(w.G1.Number, w.G1.Number, w.G2.Number, w.GC.Number, w.G3.Number); !slices.Equal(got, want) {
		t.Errorf("Priya's rows = %q, want %q (one per line; never %s, TECH-SITE)", got, want, w.GT.Number)
	}
	var g1a []string
	for _, r := range rows {
		if len(r) > 9 && r[0] == w.G1.Number && r[9] != "" {
			g1a = r
		}
	}
	g9wWantRow(t, "G1's line 1", g9wItemsHeader, g1a, []string{
		w.G1.Number, "2026-10-24", "Shree Steel Traders", w.RefPO.Number, "MS Angle 50x50x5 (MS-ANGLE-50)", "7216",
		g9wQty(t, "400"), g9wQty(t, "390"), g9wQty(t, "10"), "Bent pieces", "KGS", g9wRate(t, "62.5"), g9wMoney(t, "24375"),
	})

	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"?rejected=1", g9wSorted(w.G1.Number)},
		{"?item=" + g9wID(w.Wire), g9wSorted(w.G1.Number, w.G3.Number)},
		{"?vendor=" + g9wID(w.Bharat), g9wSorted(w.G2.Number)},
		{"?month=2026-10", g9wSorted(w.G1.Number, w.G1.Number, w.G2.Number, w.GC.Number)},
		{"?month=2026-11&item=" + g9wID(w.Wire), g9wSorted(w.G3.Number)},
	} {
		rows := g9wRecords(t, "items CSV"+tc.query, g9wGet(t, env, env.Priya, g9wItemsPath+tc.query), g9wItemsHeader)
		if got := g9wFirst(rows); !slices.Equal(got, tc.want) {
			t.Errorf("items CSV%s rows = %q, want %q", tc.query, got, tc.want)
		}
	}
	rej := g9wRecords(t, "items CSV?rejected=1", g9wGet(t, env, env.Priya, g9wItemsPath+"?rejected=1"), g9wItemsHeader)
	if len(rej) != 1 || rej[0][9] != "Bent pieces" {
		t.Errorf("items CSV?rejected=1 = %q, want only G1's rejected line", rej)
	}
}

// More than csvio.MaxExportRows rows is the generic export's refusal, with
// its status and text, as the purchase pending and expense CSVs do (AD-13
// ruling): limit+1 rows, 50,001 approved GRNs for Bills to enter and
// 50,001 lines of an approved GRN for Received items, and no CSV. The rows
// beyond the first GRN are plain SQL copies: no public path makes 50,000.
func TestGRNWeb_ReportsCSVTooManyRows(t *testing.T) {
	ctx := context.Background()
	env := grntest.New(t)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved())
	grn := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "10"}}, doctype.StateApproved)
	n := csvio.MaxExportRows
	for _, q := range []string{
		`WITH RECURSIVE k(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM k WHERE i < ?)
		INSERT INTO goods_receipt (state, created_at, created_by, updated_at, updated_by, purchase_order_id, vendor_id, site_id, project_id,
			receipt_date, challan_kind, challan_no, accepted_value, snap_vendor_name, snap_vendor_gstin, snap_po_number, snap_po_date)
		SELECT state, created_at, created_by, updated_at, updated_by, purchase_order_id, vendor_id, site_id, project_id,
			receipt_date, challan_kind, challan_no, accepted_value, snap_vendor_name, snap_vendor_gstin, snap_po_number, snap_po_date
		FROM k, goods_receipt WHERE goods_receipt.id = ?`,
		`WITH RECURSIVE k(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM k WHERE i < ?)
		INSERT INTO goods_receipt_line (parent_id, line_no, po_line_id, uom_id, received_qty, rejected_qty, accepted_qty,
			snap_item_code, snap_item_name, snap_hsn_sac, snap_uom_code, snap_rate, snap_is_service)
		SELECT parent_id, line_no + i, po_line_id, uom_id, received_qty, rejected_qty, accepted_qty,
			snap_item_code, snap_item_name, snap_hsn_sac, snap_uom_code, snap_rate, snap_is_service
		FROM k, goods_receipt_line WHERE goods_receipt_line.parent_id = ?`,
	} {
		if _, err := env.DB.SQL().ExecContext(ctx, q, n, grn.ID); err != nil {
			t.Fatalf("copy rows: %v", err)
		}
	}

	dt, _ := doctype.Lookup(goodsreceipt.DocType)
	var buf bytes.Buffer
	_, want := csvio.NewExporter(env.DB, env.Auth).Export(ctx, &buf, env.Admin, dt, list.Query{})
	var se interface{ Status() int }
	if want == nil || !errors.As(want, &se) {
		t.Fatalf("generic export err = %v, want its too-many-rows refusal", want)
	}
	for _, tc := range []struct {
		path   string
		header []string
	}{{g9wBillsPath, g9wBillsHeader}, {g9wItemsPath, g9wItemsHeader}} {
		rec := g9wGet(t, env, env.Admin, tc.path)
		if rec.Code != se.Status() {
			t.Errorf("GET %s status = %d, want the generic export's %d", tc.path, rec.Code, se.Status())
		}
		if body := html.UnescapeString(rec.Body.String()); !strings.Contains(body, want.Error()) {
			t.Errorf("GET %s body = %q, want the generic export's %q", tc.path, body[:min(len(body), 200)], want.Error())
		}
		if strings.Contains(rec.Body.String(), strings.Join(tc.header, ",")) {
			t.Errorf("GET %s: a refused export wrote the CSV", tc.path)
		}
	}
}
