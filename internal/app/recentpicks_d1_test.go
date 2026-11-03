package app_test

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// vendorRecent is the recent picks waiting in the PO's vendor picker.
func vendorRecent(t *testing.T, r *response) []string {
	t.Helper()
	tpl := r.doc.Find(`.picker:has(input[type="hidden"][name="vendor_id"]) template[data-picker-recent]`)
	if tpl.Length() == 0 {
		return nil
	}
	h, err := tpl.Html()
	if err != nil {
		t.Fatal(err)
	}
	inner, err := goquery.NewDocumentFromReader(strings.NewReader(h))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	inner.Find(`[role="option"]`).Each(func(_ int, o *goquery.Selection) { out = append(out, o.AttrOr("data-label", "")) })
	return out
}

// UI v2 §5.8 with CD-218, through the app's wiring and the real party
// service: the vendor field (LinkRole vendor) offers Priya's recent vendors
// only while they are still vendors; a party that has lost the role would be
// refused on save, so it is not offered.
func TestApp_RecentVendorsKeepTheVendorRole(t *testing.T) {
	w := newWorld(t)
	w.canonical()
	ctx := context.Background()
	priya := w.principal(priyaID)
	id := func(table, code string) string {
		t.Helper()
		return w.text(`SELECT CAST(id AS TEXT) FROM `+table+` WHERE code = ?`, code)
	}
	for _, v := range []string{"BHARAT", "SHREE"} { // SHREE newest
		if _, err := w.app.Engine().Do(ctx, priya, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: map[string]string{
			"vendor_id": id("party", v), "site_id": id("site", "METRO-SITE"), "project_id": id("project", "PRJ-METRO"), "order_date": "2026-10-24",
		}}); err != nil {
			t.Fatalf("save a draft to %s: %v", v, err)
		}
	}
	b := w.signedIn(priyaEmail)
	want := []string{"SHREE Shree Steel Traders", "BHARAT Bharat Tools"}
	if got := vendorRecent(t, b.get("/doc/"+purchase.DocType+"/new")); !slices.Equal(got, want) {
		t.Fatalf("recent vendors = %q, want %q", got, want)
	}

	// BHARAT becomes a customer only (the service refuses this while orders
	// use it, so the row changes directly).
	if err := w.app.DB().Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE party SET is_vendor = 0, is_customer = 1 WHERE code = 'BHARAT'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := vendorRecent(t, b.get("/doc/"+purchase.DocType+"/new")); !slices.Equal(got, want[:1]) {
		t.Errorf("after BHARAT lost the vendor role: recent vendors = %q, want %q", got, want[:1])
	}
}

// The role check fails closed: only Party has roles, and only vendor and
// customer are roles.
func TestApp_LinkRoleFailsClosed(t *testing.T) {
	w := newWorld(t)
	w.canonical()
	ctx := context.Background()
	pt, _ := doctype.Lookup(party.DocType)
	site, _ := doctype.Lookup("Site")
	idOf := func(code string) int64 {
		t.Helper()
		id, err := strconv.ParseInt(w.text(`SELECT CAST(id AS TEXT) FROM party WHERE code = ?`, code), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	shreeID, bharatID := idOf("SHREE"), idOf("BHARAT") // SHREE a vendor only; BHARAT made a customer only
	if err := w.app.DB().Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE party SET is_vendor = 0, is_customer = 1 WHERE code = 'BHARAT'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		target *doctype.DocType
		id     int64
		role   string
		want   bool
	}{
		{pt, shreeID, "vendor", true}, {pt, shreeID, "customer", false},
		{pt, bharatID, "vendor", false}, {pt, bharatID, "customer", true},
		{pt, shreeID, "", false}, {pt, shreeID, "Vendor", false},
		{pt, bharatID, "", false}, {pt, bharatID, "Vendor", false}, {site, shreeID, "vendor", false},
	} {
		var got bool
		err := w.app.DB().Read(ctx, func(tx *db.Tx) error {
			var err error
			got, err = w.app.LinkRole()(ctx, tx, c.target, c.id, c.role)
			return err
		})
		if err != nil || got != c.want {
			t.Errorf("%s %d %q: = %v, %v; want %v", c.target.Name, c.id, c.role, got, err, c.want)
		}
	}
}

// pickerLines is the .picker__line texts of the recent picks waiting in
// field's picker, in order.
func pickerLines(t *testing.T, r *response, field string) []string {
	t.Helper()
	h, err := r.doc.Find(`.picker:has(input[type="hidden"][name="` + field + `"]) template[data-picker-recent]`).Html()
	if err != nil {
		t.Fatal(err)
	}
	inner, err := goquery.NewDocumentFromReader(strings.NewReader(h))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	inner.Find(`[role="option"]`).Each(func(_ int, o *goquery.Selection) {
		out = append(out, strings.Join(strings.Fields(o.Find(".picker__line").Text()), " "))
	})
	return out
}

// R8 review, through the app's wiring: the line (GSTIN and city) is a
// party's alone. A site whose id is a party's with a primary billing address
// shows no line in its recent picks, while the vendor's shows its own.
func TestApp_LinkLineOnlyForParties(t *testing.T) {
	w := newWorld(t)
	w.canonical()
	ctx := context.Background()
	priya := w.principal(priyaID)
	id := func(table, code string) string {
		t.Helper()
		return w.text(`SELECT CAST(id AS TEXT) FROM `+table+` WHERE code = ?`, code)
	}
	site := id("site", "METRO-SITE")
	if n := w.count(`SELECT count(*) FROM address WHERE party_id = ? AND is_primary_billing = 1 AND disabled = 0`, site); n != 1 {
		t.Fatalf("party %s has %d primary billing addresses: the fixture no longer gives the site's id a party with a line", site, n)
	}
	if _, err := w.app.Engine().Do(ctx, priya, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: map[string]string{
		"vendor_id": id("party", "SHREE"), "site_id": site, "project_id": id("project", "PRJ-METRO"), "order_date": "2026-10-24",
	}}); err != nil {
		t.Fatalf("save a draft: %v", err)
	}
	r := w.signedIn(priyaEmail).get("/doc/" + purchase.DocType + "/new")
	gstin := w.text(`SELECT gstin FROM address WHERE party_id = ? AND is_primary_billing = 1`, id("party", "SHREE"))
	if got := pickerLines(t, r, "vendor_id"); len(got) != 1 || !strings.HasPrefix(got[0], gstin+" · ") {
		t.Errorf("vendor recent lines = %q, want SHREE's GSTIN and city", got)
	}
	if got := pickerLines(t, r, "site_id"); !slices.Equal(got, []string{""}) {
		t.Errorf("site recent lines = %q, want one pick with no line", got)
	}
}
