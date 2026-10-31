package purchaseweb_test

import (
	"bytes"
	"context"
	"errors"
	"html"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// p9f1Exec runs fixture SQL q in one write.
func p9f1Exec(t *testing.T, env *purchasetest.Env, q string, args ...any) {
	t.Helper()
	err := env.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), q, args...)
		return err
	})
	if err != nil {
		t.Fatalf("fixture SQL: %v", err)
	}
}

// PO-038 (PO spec §13; CD-299): the CSV is csvio's export format. It starts
// with the UTF-8 BOM, ends every line with CRLF, and escapes a text cell a
// spreadsheet would run as a formula as csvio.FormatCell does (item name
// and vendor here).
func TestPO_PendingExportFormat(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env)
	p9f1Exec(t, env, `UPDATE purchase_order_line SET snap_item_name = '=SUM(A1)' WHERE id = ?`, po.Lines[0].ID)
	p9f1Exec(t, env, `UPDATE purchase_order SET snap_vendor_name = '+Vendor' WHERE id = ?`, po.ID)

	rec := p9Get(t, env, env.Admin, p9ExportPath)
	body := rec.Body.Bytes()
	if !bytes.HasPrefix(body, []byte(p9BOM)) {
		t.Errorf("body starts %q, want the BOM", body[:min(len(body), 8)])
	}
	lines := strings.SplitAfter(string(body), "\n")
	if last := lines[len(lines)-1]; last != "" {
		t.Errorf("body ends %q, want a line end", last)
	}
	if len(lines) < 3 {
		t.Errorf("body = %q, want a header and a row", body)
	}
	for _, l := range lines[:len(lines)-1] {
		if !strings.HasSuffix(l, "\r\n") {
			t.Errorf("line %q doesn't end with CRLF", l)
		}
	}
	rows := p9Records(t, "Admin's CSV", rec)
	if len(rows) != 1 {
		t.Fatalf("rows = %q, want one", rows)
	}
	if want := csvio.EscapeFormula("=SUM(A1)"); rows[0][8] != want || want != "'=SUM(A1)" {
		t.Errorf("Item name = %q, want %q", rows[0][8], want)
	}
	if want := csvio.EscapeFormula("+Vendor"); rows[0][4] != want || want != "'+Vendor" {
		t.Errorf("Vendor = %q, want %q", rows[0][4], want)
	}
}

// PO-038 (PO spec §13, §27.1): a line with no required_by and no Expected on
// has blank Required by and Expected on cells; its order date is set.
func TestPO_PendingExportBlankDates(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env)
	rows := p9Records(t, "Admin's CSV", p9Get(t, env, env.Admin, p9ExportPath))
	if len(rows) != 1 || rows[0][0] != po.Number {
		t.Fatalf("rows = %q, want PO %s only", rows, po.Number)
	}
	if got := rows[0][1:4]; !slices.Equal(got, []string{"2026-10-24", "", ""}) {
		t.Errorf("Order date, Required by, Expected on = %q, want [2026-10-24, blank, blank]", got)
	}
}

// PO-038 (PO spec §13; UX §5.19): the CSV is every row, not one page: a PO
// with purchase.PendingPageSize+1 lines has that many CSV rows.
func TestPO_PendingExportAllPages(t *testing.T) {
	env := purchasetest.New(t)
	n := purchase.PendingPageSize + 1
	lines := make([]map[string]string, n)
	for i := range lines {
		lines[i] = map[string]string{
			"item_id": p9ID(t, env, "item", "WIRE-18G"), "uom_id": p9ID(t, env, "uom", "KGS"),
			"qty": "1", "price": "1", "tax_code_id": p9ID(t, env, "tax_code", "GST18"), "hsn_sac": "7217",
		}
	}
	purchasetest.NewOrder(t, env, purchasetest.OrderLines(lines...))
	if rows := p9Records(t, "Admin's CSV", p9Get(t, env, env.Admin, p9ExportPath)); len(rows) != n {
		t.Errorf("CSV rows = %d, want %d (every line, not page 1)", len(rows), n)
	}
}

// PO-038 (PO spec §13): only overdue=1 is the overdue filter; overdue=0 is
// every row, overdue and not.
func TestPO_PendingExportOverdueZero(t *testing.T) {
	env := purchasetest.New(t)
	late := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"required_by": "28-10-2026"}))
	later := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"required_by": "05-11-2026"}))
	env.SetNow(t, time.Date(2026, time.October, 31, 10, 30, 0, 0, types.IST))
	want := []string{late.Number, later.Number}
	slices.Sort(want)
	if got := p9Numbers(p9Records(t, "CSV?overdue=0", p9Get(t, env, env.Admin, p9ExportPath+"?overdue=0"))); !slices.Equal(got, want) {
		t.Errorf("CSV?overdue=0 rows = %q, want %q (every row)", got, want)
	}
	if got := p9Numbers(p9Records(t, "CSV?overdue=1", p9Get(t, env, env.Admin, p9ExportPath+"?overdue=1"))); !slices.Equal(got, []string{late.Number}) {
		t.Errorf("CSV?overdue=1 rows = %q, want %q", got, []string{late.Number})
	}
}

// More than csvio.MaxExportRows rows is the generic export's refusal, with
// its status and text (AD-13 ruling, as expense's CSVs): 50,001 POs for the
// generic export, 50,001 pending lines on one approved PO for the pending
// CSV, and no CSV.
func TestPO_PendingExportTooManyRows(t *testing.T) {
	ctx := context.Background()
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env)
	n := csvio.MaxExportRows
	p9f1Exec(t, env, `WITH RECURSIVE k(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM k WHERE i < ?)
		INSERT INTO purchase_order (state, created_at, created_by, updated_at, updated_by)
		SELECT 'draft', created_at, created_by, updated_at, updated_by FROM k, purchase_order WHERE purchase_order.id = ?`, n, po.ID)
	p9f1Exec(t, env, `WITH RECURSIVE k(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM k WHERE i < ?)
		INSERT INTO purchase_order_line (parent_id, line_no, item_id, uom_id, qty, rate, snap_item_code, snap_item_name, snap_uom_code)
		SELECT parent_id, line_no + i, item_id, uom_id, qty, rate, snap_item_code, snap_item_name, snap_uom_code
		FROM k, purchase_order_line WHERE purchase_order_line.id = ?`, n, po.Lines[0].ID)

	dt, _ := doctype.Lookup(purchase.DocType)
	var buf bytes.Buffer
	_, want := csvio.NewExporter(env.DB, env.Auth).Export(ctx, &buf, env.Admin, dt, list.Query{})
	var se interface{ Status() int }
	if want == nil || !errors.As(want, &se) {
		t.Fatalf("generic export err = %v, want its too-many-rows refusal", want)
	}

	rec := p9Get(t, env, env.Admin, p9ExportPath)
	if rec.Code != se.Status() {
		t.Errorf("status = %d, want the generic export's %d", rec.Code, se.Status())
	}
	if body := html.UnescapeString(rec.Body.String()); !strings.Contains(body, want.Error()) {
		t.Errorf("body = %q, want the generic export's %q", body[:min(len(body), 200)], want.Error())
	}
	if strings.Contains(rec.Body.String(), strings.Join(p9Header, ",")) {
		t.Errorf("a refused export wrote the CSV")
	}
}
