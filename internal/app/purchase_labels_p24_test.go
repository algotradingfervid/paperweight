package app_test

import (
	"context"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// MOD-PO P24 (CD-325, CD-330 (5); UX §5.2, §5.14): on the real app, the
// purchase lists show order_status's label in its cell, the filter chips
// show receipt_status's and order_status's labels, and the CSV export
// keeps the stored codes.
func TestApp_PurchaseSelectLabels(t *testing.T) {
	w := newWorld(t)
	admin, _, _ := w.canonical()
	ctx := context.Background()
	id := func(table, code string) string {
		t.Helper()
		return w.text(`SELECT CAST(id AS TEXT) FROM `+table+` WHERE code = ?`, code)
	}
	header := map[string]string{"site_id": id("site", "METRO-SITE"), "project_id": id("project", "PRJ-METRO")}
	line := map[string]string{"item_id": id("item", "MS-ANGLE-50"), "uom_id": id("uom", "KGS"), "qty": "100"}
	approved := func(dt string, values, line map[string]string) {
		t.Helper()
		res, err := w.app.Engine().Do(ctx, admin, dt, 0, doctype.ActionSave, doc.ActionInput{Values: values, Lines: []map[string]string{line}})
		if err != nil {
			t.Fatalf("save %s as admin: %v", dt, err)
		}
		if _, err := w.app.Engine().Do(ctx, admin, dt, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil {
			t.Fatalf("submit %s %d as admin: %v", dt, res.ID, err)
		}
	}
	poValues := map[string]string{"vendor_id": id("party", "SHREE"), "order_date": "2026-10-24"}
	maps.Copy(poValues, header)
	poLine := map[string]string{"price": "62.5", "tax_code_id": id("tax_code", "GST18"), "hsn_sac": "7216"}
	maps.Copy(poLine, line)
	prValues := map[string]string{"request_date": "2026-10-24"}
	maps.Copy(prValues, header)
	approved(purchase.DocType, poValues, poLine)
	approved(purchase.RequestDocType, prValues, line)

	b := w.signedIn(adminEmail)
	get := func(path string) *response {
		t.Helper()
		r := b.get(path)
		if r.status != http.StatusOK {
			t.Fatalf("GET %s = %d %q", path, r.status, r.pageText())
		}
		return r
	}
	chips := func(r *response) []string {
		var out []string
		for _, s := range r.doc.Find(`ul[aria-label="Active filters"] a.chip`).EachIter() { // not the quick chips (R5)
			out = append(out, strings.Join(strings.Fields(s.Text()), " "))
		}
		return out
	}

	r := get("/doc/" + purchase.RequestDocType)
	if text := r.pageText(); !strings.Contains(text, "Not ordered") || strings.Contains(text, "not_ordered") {
		t.Errorf("request list = %q, want the Not ordered cell and no raw code", text)
	}
	r = get("/doc/" + purchase.RequestDocType + "?f=order_status:=:not_ordered")
	if c := chips(r); len(c) != 1 || !strings.HasPrefix(c[0], "Ordered: Not ordered") {
		t.Errorf("request list chips = %q, want Ordered: Not ordered", c)
	}
	r = get("/doc/" + purchase.DocType + "?f=receipt_status:=:not_received")
	if c := chips(r); len(c) != 1 || !strings.HasPrefix(c[0], "Receipt: Not received") {
		t.Errorf("order list chips = %q, want Receipt: Not received", c)
	}
	if text := r.pageText(); strings.Contains(text, "not_received") {
		t.Errorf("order list = %q, want no raw not_received", text)
	}

	for _, c := range []struct{ dt, code, label string }{
		{purchase.DocType, "not_received", "Not received"},
		{purchase.RequestDocType, "not_ordered", "Not ordered"},
	} {
		r := b.get("/doc/" + c.dt + "/export.csv")
		if r.status != http.StatusOK || !strings.Contains(r.body, ","+c.code) || strings.Contains(r.body, c.label) {
			t.Errorf("%s CSV = %d %q, want the raw %s and not %s", c.dt, r.status, r.body, c.code, c.label)
		}
	}
}
