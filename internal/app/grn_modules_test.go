package app_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// MOD-GRN S0: the app wires grnweb's Module for the GRN into docscreen's
// Deps.Modules beside purchase's. Mounted only; S1 to S3 test what it does.
func TestApp_GRNModuleWired(t *testing.T) {
	w := newWorld(t)
	mods := w.app.DocDeps().Modules
	for _, dt := range []string{goodsreceipt.DocType, purchase.DocType, purchase.RequestDocType} {
		if _, ok := mods[dt]; !ok {
			t.Errorf("docscreen Deps.Modules has no %s entry", dt)
		}
	}
}

// MOD-GRN S1 (CD-283): the app wires grnweb's ReceiveLink into purchaseweb's
// Deps.ReceiveLink, so purchase shows Receive goods without importing
// goodsreceipt. On an approved PO, Suresh (Stores: create on the GRN, read
// on the PO) gets step 1's link with the PO; Deepak (no GRN rights) none.
func TestApp_ReceiveLinkWired(t *testing.T) {
	w := newWorld(t)
	admin, _, _ := w.canonical()
	id := func(table, code string) string {
		t.Helper()
		return w.text(`SELECT CAST(id AS TEXT) FROM `+table+` WHERE code = ?`, code)
	}
	ctx := context.Background()
	res, err := w.app.Engine().Do(ctx, admin, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"vendor_id": id("party", "SHREE"), "site_id": id("site", "METRO-SITE"), "project_id": id("project", "PRJ-METRO"), "order_date": "2026-10-24"},
		Lines: []map[string]string{{"item_id": id("item", "MS-ANGLE-50"), "uom_id": id("uom", "KGS"), "qty": "100",
			"price": "62.5", "tax_code_id": id("tax_code", "GST18"), "hsn_sac": "7216"}},
	})
	if err != nil {
		t.Fatalf("save PO: %v", err)
	}
	if res, err = w.app.Engine().Do(ctx, admin, purchase.DocType, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil || res.State != doctype.StateApproved {
		t.Fatalf("submit PO = %s, %v; want approved", res.State, err)
	}
	link := w.app.ReceiveLink()
	if link == nil {
		t.Fatal("purchaseweb Deps.ReceiveLink is nil")
	}
	want := "/doc/" + goodsreceipt.DocType + "/new?po=" + strconv.FormatInt(res.ID, 10)
	if href, ok := link(ctx, w.principal(sureshID), res.ID); !ok || href != want {
		t.Errorf("ReceiveLink(suresh) = %q %v, want %s true", href, ok, want)
	}
	if href, ok := link(ctx, w.principal(deepakID), res.ID); ok || href != "" {
		t.Errorf("ReceiveLink(deepak) = %q %v, want false", href, ok)
	}
}
