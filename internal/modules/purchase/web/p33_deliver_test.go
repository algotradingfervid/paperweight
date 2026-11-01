package purchaseweb

import (
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// P33 · the document page's delivery address (CD-337 (1), CD-356): Details
// adds Delivery address and Delivery contact pairs under docscreen's own
// Deliver to row, read from the site id stored on the PO.

const (
	p33Address = "Gate 2, Hinjewadi, Phase 1, Pune - 411057, Maharashtra (27)"
	p33Contact = "Raj Patil, +919876543210"
)

// p33SetDelivery gives site code the delivery address and contact d through
// the site master's own Update.
func p33SetDelivery(t *testing.T, env *purchasetest.Env, code string, d site.Delivery) {
	t.Helper()
	svc := site.New(mstkit.Env{DB: env.DB, Auth: env.Auth, Engine: env.Engine, Auditor: audit.Default, Clock: env.Clock})
	var st site.Site
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var n int64
		if err := tx.SQL().QueryRowContext(t.Context(), `SELECT id FROM site WHERE code = ?`, code).Scan(&n); err != nil {
			return err
		}
		var err error
		st, err = svc.Get(t.Context(), tx, n)
		return err
	}); err != nil {
		t.Fatalf("get site %s: %v", code, err)
	}
	if _, err := svc.Update(t.Context(), env.Admin, st.ID, st.Version, site.Input{
		Code: st.Code, Name: st.Name, BranchID: st.BranchID, ProjectID: st.ProjectID, Type: st.Type, Delivery: &d,
	}); err != nil {
		t.Fatalf("set delivery of site %s: %v", code, err)
	}
}

// p33Pune is a Maharashtra delivery address (METRO-SITE's branch PUNE) with
// a contact.
var p33Pune = site.Delivery{
	Line1: "Gate 2, Hinjewadi", Line2: "Phase 1", City: "Pune", StateCode: "27", Pincode: "411057",
	ContactName: "Raj Patil", ContactMobile: "9876543210",
}

// CD-337 (1): with METRO-SITE given a delivery address and contact, the PO
// page shows them under Deliver to, for Ravi, for Priya (restricted to
// PRJ-METRO) and for a user whose only rights are read on purchase.Order
// and purchase.Request, with no right on the site master: the PO's read is
// the gate (CD-356 (2)). The site's name stays docscreen's Deliver to row.
func TestPO_PageDeliveryAddress(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	po := w.order(t, doctype.StateApproved)
	p33SetDelivery(t, env, "METRO-SITE", p33Pune)
	reader := w.user(t, "p33reader", nil)
	for _, p := range []auth.Principal{env.Ravi, env.Priya, reader} {
		page := w.page(t, p, po.ID)
		if v, ok := page.pair("Deliver to"); !ok || v == "" {
			t.Errorf("%s: Deliver to = %q, %v; want the site", p.Name, v, ok)
		}
		if v, _ := page.pair("Delivery address"); v != p33Address {
			t.Errorf("%s: Delivery address = %q, want %q", p.Name, v, p33Address)
		}
		if v, _ := page.pair("Delivery contact"); v != p33Contact {
			t.Errorf("%s: Delivery contact = %q, want %q", p.Name, v, p33Contact)
		}
	}

	// Only a contact: no address pair. Only an address: no contact pair.
	p33SetDelivery(t, env, "METRO-SITE", site.Delivery{ContactName: "Raj Patil"})
	page := w.page(t, env.Ravi, po.ID)
	if v, ok := page.pair("Delivery address"); ok {
		t.Errorf("contact only: Delivery address = %q, want no pair", v)
	}
	if v, _ := page.pair("Delivery contact"); v != "Raj Patil" {
		t.Errorf("contact only: Delivery contact = %q, want %q", v, "Raj Patil")
	}
	addrOnly := p33Pune
	addrOnly.ContactName, addrOnly.ContactMobile = "", ""
	p33SetDelivery(t, env, "METRO-SITE", addrOnly)
	page = w.page(t, env.Ravi, po.ID)
	if v, _ := page.pair("Delivery address"); v != p33Address {
		t.Errorf("address only: Delivery address = %q, want %q", v, p33Address)
	}
	if v, ok := page.pair("Delivery contact"); ok {
		t.Errorf("address only: Delivery contact = %q, want no pair", v)
	}
}

// CD-337 (1): a site with no delivery address or contact adds no pair; the
// page shows the site's name only, as before.
func TestPO_PageDeliveryNameOnly(t *testing.T) {
	w := newP15bWeb(t, false)
	po := w.order(t, doctype.StateDraft)
	page := w.page(t, w.env.Ravi, po.ID)
	if v, ok := page.pair("Deliver to"); !ok || v == "" {
		t.Errorf("Deliver to = %q, %v; want the site", v, ok)
	}
	for _, label := range []string{"Delivery address", "Delivery contact"} {
		if v, ok := page.pair(label); ok {
			t.Errorf("%s = %q on a site with none, want no pair", label, v)
		}
	}
}

// Hooks spec §4.3 (a hidden field is never rendered): when the screen's Hide
// hides Deliver to, its address and contact are left out too.
func TestPO_PageDeliveryHonoursHide(t *testing.T) {
	env := purchasetest.New(t)
	w := p15aHiding(t, env, "site_id")
	po := purchasetest.NewOrder(t, env)
	p33SetDelivery(t, env, "METRO-SITE", p33Pune)
	page := w.page(t, env.Ravi, po.ID)
	for _, label := range []string{"Delivery address", "Delivery contact"} {
		if v, ok := page.pair(label); ok {
			t.Errorf("%s = %q with site_id hidden, want no pair (PO %s)", label, v, strconv.FormatInt(po.ID, 10))
		}
	}
}
