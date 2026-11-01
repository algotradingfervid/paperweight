package purchase_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// P33 · Deliver to with the site's delivery address (CD-337 (1), CD-356).
// The print's Deliver to block is the site's name, then its delivery
// address lines, then its contact, read from the site id stored on the PO.

// p33SetDelivery gives site code the delivery address and contact d through
// the site master's own Update, as the site's delivery form does.
func p33SetDelivery(t *testing.T, env *purchasetest.Env, code string, d site.Delivery) {
	t.Helper()
	svc := site.New(p2Kit(env))
	var st site.Site
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		st, err = svc.Get(t.Context(), tx, p2Int(t, masterID(t, env.DB, "site", code)))
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

// p33Pune is a delivery address in Maharashtra (METRO-SITE's branch PUNE)
// with a contact.
var p33Pune = site.Delivery{
	Line1: "Gate 2, Hinjewadi", Line2: "Phase 1", City: "Pune", StateCode: "27", Pincode: "411057",
	ContactName: "Raj Patil", ContactMobile: "9876543210",
}

// p33DeliverTo are the lines of the print's Deliver to block.
func p33DeliverTo(html string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`<p class="address">([^<]*)</p>`).FindAllStringSubmatch(p8Section(html, "section", "deliver-to"), -1) {
		out = append(out, m[1])
	}
	return out
}

// CD-337 (1): the reference PO's print, with METRO-SITE given a delivery
// address and contact, prints the site's name, the address lines as
// address.DisplayText gives them, then the contact line, under Deliver to.
// Priya is restricted to PRJ-METRO: the PO's read and print are the only
// gate, with no separate site check (CD-356 (2)). With only an address, or
// only a contact, the other part is left out. The address is the site's
// current one: a change on the master shows on the next print.
func TestPO_PrintDeliverToAddress(t *testing.T) {
	env := purchasetest.New(t)
	id := p8Reference(t, env)
	p33SetDelivery(t, env, "METRO-SITE", p33Pune)

	got := p33DeliverTo(p8Print(t, env, env.Priya, id))
	want := []string{"METRO-SITE", "Gate 2, Hinjewadi", "Phase 1", "Pune - 411057", "Maharashtra (27)", "Contact: Raj Patil, +919876543210"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("deliver-to lines = %q, want %q", got, want)
	}

	addrOnly := p33Pune
	addrOnly.ContactName, addrOnly.ContactMobile, addrOnly.Line2 = "", "", ""
	p33SetDelivery(t, env, "METRO-SITE", addrOnly)
	got = p33DeliverTo(p8Print(t, env, env.Priya, id))
	want = []string{"METRO-SITE", "Gate 2, Hinjewadi", "Pune - 411057", "Maharashtra (27)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("address only: deliver-to lines = %q, want %q", got, want)
	}

	p33SetDelivery(t, env, "METRO-SITE", site.Delivery{ContactName: "Raj Patil"})
	got = p33DeliverTo(p8Print(t, env, env.Priya, id))
	want = []string{"METRO-SITE", "Contact: Raj Patil"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("contact only: deliver-to lines = %q, want %q", got, want)
	}
}

// CD-337 (1): a site with no delivery address or contact prints its name
// only, as before (the golden po_approved holds the same block).
func TestPO_PrintDeliverToNameOnly(t *testing.T) {
	env := purchasetest.New(t)
	id := p8Reference(t, env)
	if got := p33DeliverTo(p8Print(t, env, env.Priya, id)); strings.Join(got, "|") != "METRO-SITE" {
		t.Errorf("deliver-to lines = %q, want only the site's name", got)
	}
	p33SetDelivery(t, env, "METRO-SITE", p33Pune)
	p33SetDelivery(t, env, "METRO-SITE", site.Delivery{}) // removed again
	if got := p33DeliverTo(p8Print(t, env, env.Priya, id)); strings.Join(got, "|") != "METRO-SITE" {
		t.Errorf("after removing the address: deliver-to lines = %q, want only the site's name", got)
	}
}
