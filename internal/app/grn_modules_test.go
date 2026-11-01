package app_test

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
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

// MOD-GRN S4, GUX-035 (UX doc §5.10, CD-282 Q8): a user with read on the
// GRN (Suresh, Stores) has the nav entry Goods receipt reports right after
// Goods Receipts, opening /reports/grn with both reports, and the reports
// open on the real router; one without (Deepak) has no entry, and every
// report URL answers AUTH's 403.
func TestApp_GRNReportsNav(t *testing.T) {
	w := newWorld(t)
	w.canonical()
	suresh := w.signedIn(sureshEmail)
	_, links := navGroups(suresh)
	if want := []string{"/doc/" + goodsreceipt.DocType, "/reports/grn"}; !slices.Equal(links["Goods receipt"], want) {
		t.Errorf("Suresh's Goods receipt group = %q, want %q", links["Goods receipt"], want)
	}
	if got := strings.TrimSpace(suresh.get("/approvals").doc.Find(`nav.app-sidebar a[href="/reports/grn"]`).Text()); got != "Goods receipt reports" {
		t.Errorf("nav entry = %q, want Goods receipt reports", got)
	}
	r := suresh.get("/reports/grn")
	if r.status != http.StatusOK || r.doc.Find(`main a[href="/reports/grn/bills"]`).Length() != 1 || r.doc.Find(`main a[href="/reports/grn/items"]`).Length() != 1 {
		t.Errorf("GET /reports/grn as Suresh = %d, want 200 with links to both reports", r.status)
	}
	for _, path := range []string{"/reports/grn/bills", "/reports/grn/items", "/reports/grn/bills/filter", "/reports/grn/items/filter"} {
		if got := suresh.get(path).status; got != http.StatusOK {
			t.Errorf("GET %s as Suresh = %d, want 200", path, got)
		}
	}

	deepak := w.signedIn(deepakEmail)
	if _, links := navGroups(deepak); slices.Contains(links["Goods receipt"], "/reports/grn") {
		t.Errorf("Deepak (no GRN read) has the reports entry: %q", links["Goods receipt"])
	}
	for _, path := range []string{"/reports/grn", "/reports/grn/bills", "/reports/grn/items", "/reports/grn/bills/filter", "/reports/grn/items/filter"} {
		if got := deepak.get(path).status; got != http.StatusForbidden {
			t.Errorf("GET %s as Deepak = %d, want 403", path, got)
		}
	}
}
