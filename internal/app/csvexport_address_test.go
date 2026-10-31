package app_test

import (
	"context"
	"encoding/csv"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// CD-318: the purchase order export answers 200 although Address, the
// vendor_address_id target, has no code column. The cell is the address's
// display text for a viewer who may read addresses, and blank for one who
// may read and export orders only (CD-233). Real auth.Service, real mstkit
// link check.
func TestApp_PurchaseOrderExportAddressCell(t *testing.T) {
	w := newWorld(t)
	admin, fx, _ := w.canonical()
	ctx := context.Background()
	var vendor, address int64
	var site, project int64
	for _, p := range fx.Parties {
		if p.Code == "SHREE" {
			vendor, address = p.ID, p.Addresses[1].ID // Thane
		}
	}
	for _, s := range fx.Sites {
		if s.Code == "METRO-SITE" {
			site = s.ID
		}
	}
	for _, p := range fx.Projects {
		if p.Code == "PRJ-METRO" {
			project = p.ID
		}
	}
	if vendor == 0 || address == 0 || site == 0 || project == 0 {
		t.Fatalf("fixture ids: vendor %d address %d site %d project %d, want all set", vendor, address, site, project)
	}
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	if _, err := w.app.Engine().Do(ctx, admin, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: map[string]string{
		"vendor_id": id(vendor), "vendor_address_id": id(address), "site_id": id(site), "project_id": id(project), "order_date": "2026-10-24",
	}}); err != nil {
		t.Fatalf("create order draft: %v", err)
	}

	const orderOnly, withAddress = "Order Exporter", "Order And Address Exporter"
	orderRights := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightExport: auth.ScopeAll}
	for _, c := range []struct {
		role, email string
		address     bool
	}{
		{orderOnly, "orderonly@vardhan.test", false},
		{withAddress, "withaddress@vardhan.test", true},
	} {
		if err := w.app.Auth().CreateRole(ctx, admin, c.role); err != nil {
			t.Fatalf("CreateRole(%s): %v", c.role, err)
		}
		if err := w.app.Auth().SetRights(ctx, admin, c.role, purchase.DocType, orderRights); err != nil {
			t.Fatalf("SetRights(%s, order): %v", c.role, err)
		}
		if c.address {
			if err := w.app.Auth().SetRights(ctx, admin, c.role, "Address", map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}); err != nil {
				t.Fatalf("SetRights(%s, Address): %v", c.role, err)
			}
		}
		if _, _, err := w.app.Auth().CreateUser(ctx, admin, auth.NewUser{Email: c.email, Name: c.role, Password: testPassword, Roles: []string{c.role}}); err != nil {
			t.Fatalf("CreateUser(%s): %v", c.email, err)
		}
	}

	path := "/doc/" + purchase.DocType + "/export.csv"
	for _, c := range []struct {
		who, email, want string
	}{
		{"admin", adminEmail, "Thane"},
		{"order and address reader", "withaddress@vardhan.test", "Thane"},
		{"order reader only", "orderonly@vardhan.test", ""},
	} {
		r := w.signedIn(c.email).get(path)
		if r.status != http.StatusOK {
			t.Fatalf("%s GET %s = %d %q, want 200", c.who, path, r.status, r.pageText())
		}
		rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(r.body, byteOrderMark))).ReadAll()
		if err != nil || len(rows) != 2 {
			t.Fatalf("%s export = %q (%v), want a header and one order", c.who, r.body, err)
		}
		col := slices.Index(rows[0], "Vendor address")
		if col < 0 {
			t.Fatalf("%s export header %v has no Vendor address", c.who, rows[0])
		}
		if got := rows[1][col]; got != c.want {
			t.Errorf("%s: Vendor address cell = %q, want %q", c.who, got, c.want)
		}
		if c.want == "" && strings.Contains(r.body, "Thane") {
			t.Errorf("%s export names the address: %q", c.who, r.body)
		}
	}
}
