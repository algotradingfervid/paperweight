package app_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// MOD-PO P20 (CD-308: a page never 500s on a stored total). A draft under
// reverse charge (grand = net = ₹9,00,00,00,000.00, one valid line) is saved
// on the Terms step with reverse charge turned off: the grand total would be
// ₹10,62,00,00,000.00, and the step save commits (the error is on "lines",
// outside Only). Validate leaves the totals cleared, so the PO page draws
// the stored PO with 200, never the 500 that drawing an over-limit total
// gives ("Amount is too large").
func TestApp_OrderPageOverTotalLimit(t *testing.T) {
	w := newWorld(t)
	admin, _, _ := w.canonical()
	ctx := context.Background()
	id := func(table, code string) string {
		t.Helper()
		return w.text(`SELECT CAST(id AS TEXT) FROM `+table+` WHERE code = ?`, code)
	}
	values := map[string]string{
		"vendor_id": id("party", "SHREE"), "site_id": id("site", "METRO-SITE"), "project_id": id("project", "PRJ-METRO"),
		"order_date": "2026-10-24", "reverse_charge": "true",
	}
	line := map[string]string{"item_id": id("item", "MS-ANGLE-50"), "uom_id": id("uom", "KGS"), "qty": "100",
		"price": "90000000", "tax_code_id": id("tax_code", "GST18"), "hsn_sac": "7216"}
	res, err := w.app.Engine().Do(ctx, admin, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: values, Lines: []map[string]string{line}})
	if err != nil {
		t.Fatalf("save the reverse-charge draft: %v", err)
	}
	lineID := w.text(`SELECT CAST(id AS TEXT) FROM purchase_order_line WHERE parent_id = ?`, res.ID)
	if _, err := w.app.Engine().Do(ctx, admin, purchase.DocType, res.ID, doctype.ActionSave, doc.ActionInput{
		Version: res.Version, Values: map[string]string{"reverse_charge": "false"},
		Lines: []map[string]string{{"id": lineID, "item_id": line["item_id"]}}, Only: []string{"reverse_charge"},
	}); err != nil {
		t.Fatalf("step save, reverse charge off: %v", err)
	}

	page := "/doc/purchase.Order/" + strconv.FormatInt(res.ID, 10)
	r := w.signedIn(adminEmail).get(page)
	if r.status != http.StatusOK {
		t.Fatalf("GET %s = %d (%q), want 200", page, r.status, r.pageText())
	}
	if strings.Contains(r.pageText(), "Amount is too large") {
		t.Errorf("GET %s draws %q, want the stored (cleared) total", page, "Amount is too large")
	}
}
