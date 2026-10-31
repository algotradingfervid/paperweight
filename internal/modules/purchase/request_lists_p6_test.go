package purchase_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-PO P6 Do 7, PendingToOrder (PO spec §20.2): pending to order is
// max(0, qty − ordered − held) summed over the row's PO lines, never below 0
// when a PO over-orders, and 0 on a closed request. One call answers every
// row asked for, keyed by request line id.
func TestRequest_PendingToOrder(t *testing.T) {
	env := purchasetest.New(t)
	req := p6Request(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"), []map[string]string{
		p5Line(t, env, "CEMENT-OPC", "BAG", "40"), p5Line(t, env, "MS-ANGLE-50", "KGS", "100"), p5Line(t, env, "WIRE-18G", "KGS", "50")})
	closed := p6CementRequest(t, env, "40")
	p5Must(t, "Suresh closes the second request")(p5Do(t, env, env.Suresh, closed.ID, doctype.ActionClose, p5CloseReason))
	bags, kg, wire, shut := req.Lines[0].ID, req.Lines[1].ID, req.Lines[2].ID, closed.Lines[0].ID

	p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "15", bags)}, doctype.StateDraft) // held 15
	p6Order(t, env, env.Arjun, nil, []map[string]string{
		p6Bag(t, env, "10", bags), // ordered 10
		p6Link(p2Line(t, env, "MS-ANGLE-50", "KGS", "120", "62.5"), kg), // over-ordered: 120 of 100
	}, doctype.StateApproved)

	got := p6Pending(t, env, bags, kg, wire, shut)
	want := map[int64]types.Qty{bags: p6Q(t, "15"), kg: 0, wire: p6Q(t, "50"), shut: 0}
	if len(got) != len(want) {
		t.Errorf("PendingToOrder = %v, want %v", got, want)
	}
	for id, w := range want {
		if q, ok := got[id]; !ok || q != w {
			t.Errorf("pending to order of row %d = %s (present %v), want %s", id, q.Plain(), ok, w.Plain())
		}
	}
}

// MOD-PO P6 Do 7, HeldBy and LinkedOrders (PO spec §20.4; CD-280 D6): HeldBy
// is every non-cancelled PO line linking the request, oldest PO first and in
// line order, with the PO's display, state and owner and the line's qty;
// LinkedOrders is every non-cancelled PO linking it, oldest first, with its
// vendor name, state and receipt status. A cancelled PO and a PO on another
// request are left out; a closed one stays (callers filter by state).
func TestRequest_HeldByAndLinkedOrders(t *testing.T) {
	env := purchasetest.New(t)
	req := p6Request(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"), []map[string]string{
		p5Line(t, env, "CEMENT-OPC", "BAG", "40"), p5Line(t, env, "MS-ANGLE-50", "KGS", "100")})
	elsewhere := p6CementRequest(t, env, "40")
	bags, kg := req.Lines[0].ID, req.Lines[1].ID

	a := p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "10", bags)}, doctype.StateDraft)
	b := p6Order(t, env, env.Arjun, p2Header(t, env, "BHARAT", "METRO-SITE", "PRJ-METRO"), []map[string]string{
		p6Link(p2Line(t, env, "MS-ANGLE-50", "KGS", "20", "62.5"), kg), p6Bag(t, env, "10", bags)}, doctype.StateApproved)
	c := p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "5", bags)}, doctype.StateApproved)
	p2Must(t, "Ravi cancels C")(p2Do(t, env, env.Ravi, c.ID, doc.ActionCancel, p6CancelReason))
	d := p6Order(t, env, env.Priya, nil, []map[string]string{p6Link(p2Line(t, env, "MS-ANGLE-50", "KGS", "5", "62.5"), kg)}, doctype.StateApproved)
	p2Must(t, "Priya closes D")(p2Do(t, env, env.Priya, d.ID, doc.ActionClose, p6CloseReason))
	p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "40", elsewhere.Lines[0].ID)}, doctype.StateDraft)

	b = p2Load(t, env, b.ID)
	want := []purchase.Holder{
		{RequestLineID: bags, POID: a.ID, PODisplay: p6Label(a), State: doctype.StateDraft, Owner: env.Priya.ID, Qty: p6Q(t, "10")},
		{RequestLineID: kg, POID: b.ID, PODisplay: b.Number, State: doctype.StateApproved, Owner: env.Arjun.ID, Qty: p6Q(t, "20")},
		{RequestLineID: bags, POID: b.ID, PODisplay: b.Number, State: doctype.StateApproved, Owner: env.Arjun.ID, Qty: p6Q(t, "10")},
		{RequestLineID: kg, POID: d.ID, PODisplay: d.Number, State: doctype.StateClosed, Owner: env.Priya.ID, Qty: p6Q(t, "5")},
	}
	if got := p6HeldBy(t, env, req.ID); !p6SameHolders(got, want) {
		t.Errorf("HeldBy =\n%+v\nwant\n%+v", got, want)
	}
	wantLinked := []purchase.LinkedOrder{
		{ID: a.ID, Display: p6Label(a), VendorName: "Shree Steel Traders", State: doctype.StateDraft, ReceiptStatus: "not_received"},
		{ID: b.ID, Display: b.Number, VendorName: "Bharat Tools", State: doctype.StateApproved, ReceiptStatus: "not_received"},
		{ID: d.ID, Display: d.Number, VendorName: "Shree Steel Traders", State: doctype.StateClosed, ReceiptStatus: "not_received"},
	}
	if got := p6Linked(t, env, req.ID); !p6SameLinked(got, wantLinked) {
		t.Errorf("LinkedOrders =\n%+v\nwant\n%+v", got, wantLinked)
	}
}

// MOD-PO P6 Do 7, OpenRequestLines (PO spec §20.4, §20.6; P15c reads it), on
// the real auth.Service: the submitted requests in Auth.Scope(p, Request,
// read) for that site and project, oldest needed-by first, each with its
// requester, needed-by date and rows (item, unit, pending to order). A row's
// holders are only the PO lines whose lineOrdered gives ordered or held above
// 0: a closed PO with nothing received and a cancelled PO are left out.
// Draft, closed and cancelled requests and requests for another site or
// project aren't listed. Deepak, who holds only read own (Employee), sees only
// his own request; Priya's project restriction (PRJ-METRO) hides PR-B,
// which Arjun (no restrictions) sees.
func TestRequest_OpenRequestLines(t *testing.T) {
	env := purchasetest.New(t)
	header := func(site, project, neededBy string) map[string]string {
		h := p5Header(t, env, site, project)
		h["required_by"] = neededBy
		return h
	}
	// PR-A (Suresh, needed by 31-10) and PR-C (Deepak, needed by 28-10) are
	// for METRO-SITE / PRJ-METRO; PR-A is raised first.
	prA := p6Request(t, env, env.Suresh, header("METRO-SITE", "PRJ-METRO", "2026-10-31"), []map[string]string{
		p5Line(t, env, "CEMENT-OPC", "BAG", "40"), p5Line(t, env, "MS-ANGLE-50", "KGS", "100")})
	prC := p6Request(t, env, env.Deepak, header("METRO-SITE", "PRJ-METRO", "2026-10-28"), []map[string]string{
		p5Line(t, env, "WIRE-18G", "KGS", "50")})
	// Not listed for METRO-SITE / PRJ-METRO: PR-B (another site and project),
	// PR-D (another site), a draft, a closed and a cancelled request.
	prB := p6Request(t, env, env.Deepak, header("PUNE-WH", "PRJ-TECHPARK", "2026-10-26"), []map[string]string{
		p5Line(t, env, "CEMENT-OPC", "BAG", "10")})
	p6Request(t, env, env.Suresh, header("PUNE-WH", "PRJ-METRO", "2026-10-25"), []map[string]string{p5Line(t, env, "CEMENT-OPC", "BAG", "5")})
	p5Must(t, "a draft request")(p5Create(t, env, env.Suresh, header("METRO-SITE", "PRJ-METRO", "2026-10-25"),
		[]map[string]string{p5Line(t, env, "CEMENT-OPC", "BAG", "5")}))
	shut := p6Request(t, env, env.Suresh, header("METRO-SITE", "PRJ-METRO", "2026-10-25"), []map[string]string{p5Line(t, env, "CEMENT-OPC", "BAG", "5")})
	p5Must(t, "close a request")(p5Do(t, env, env.Suresh, shut.ID, doctype.ActionClose, p5CloseReason))
	gone := p6Request(t, env, env.Suresh, header("METRO-SITE", "PRJ-METRO", "2026-10-25"), []map[string]string{p5Line(t, env, "CEMENT-OPC", "BAG", "5")})
	p5Must(t, "cancel a request")(p5Do(t, env, env.Suresh, gone.ID, doctype.ActionCancel, p5CancelReason))

	bags, kg := prA.Lines[0].ID, prA.Lines[1].ID
	x := p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "25", bags)}, doctype.StateDraft) // holds 25
	y := p6Order(t, env, env.Priya, nil, []map[string]string{p6Link(p2Line(t, env, "MS-ANGLE-50", "KGS", "30", "62.5"), kg)}, doctype.StateApproved)
	p2Must(t, "close Y with nothing received")(p2Do(t, env, env.Priya, y.ID, doc.ActionClose, p6CloseReason)) // counts 0
	z := p6Order(t, env, env.Priya, nil, []map[string]string{p6Link(p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "62.5"), kg)}, doctype.StateApproved)
	p2Must(t, "cancel Z")(p2Do(t, env, env.Ravi, z.ID, doc.ActionCancel, p6CancelReason)) // counts 0
	w := p6Order(t, env, env.Arjun, nil, []map[string]string{p6Link(p2Line(t, env, "MS-ANGLE-50", "KGS", "20", "62.5"), kg)}, doctype.StateApproved)
	w = p2Load(t, env, w.ID)

	wantC := fmt.Sprintf("%s by %d Deepak Nair, needed by 2026-10-28: [1 WIRE-18G KGS pending 50 on []]", prC.Number, env.Deepak.ID)
	wantA := fmt.Sprintf("%s by %d Suresh Patil, needed by 2026-10-31: [1 CEMENT-OPC BAG pending 15 on [%s draft %d 25] | 2 MS-ANGLE-50 KGS pending 80 on [%s approved %d 20]]",
		prA.Number, env.Suresh.ID, p6Label(x), env.Priya.ID, w.Number, env.Arjun.ID)
	wantB := fmt.Sprintf("%s by %d Deepak Nair, needed by 2026-10-26: [1 CEMENT-OPC BAG pending 10 on []]", prB.Number, env.Deepak.ID)
	metro, metroPrj := p5ID(t, masterID(t, env.DB, "site", "METRO-SITE")), p5ID(t, masterID(t, env.DB, "project", "PRJ-METRO"))
	pune, tech := p5ID(t, masterID(t, env.DB, "site", "PUNE-WH")), p5ID(t, masterID(t, env.DB, "project", "PRJ-TECHPARK"))

	for _, c := range []struct {
		who           string
		p             auth.Principal
		site, project int64
		want          []string
	}{
		{"Arjun, METRO-SITE", env.Arjun, metro, metroPrj, []string{wantC, wantA}},
		{"Priya, METRO-SITE", env.Priya, metro, metroPrj, []string{wantC, wantA}},
		{"Deepak (read own), METRO-SITE", env.Deepak, metro, metroPrj, []string{wantC}},
		{"Arjun, PUNE-WH / PRJ-TECHPARK", env.Arjun, pune, tech, []string{wantB}},
		{"Priya (PRJ-METRO only), PUNE-WH / PRJ-TECHPARK", env.Priya, pune, tech, nil},
	} {
		got, err := env.Service.OpenRequestLines(t.Context(), c.p, c.site, c.project)
		if err != nil {
			t.Errorf("%s: OpenRequestLines: %v", c.who, err)
			continue
		}
		if s := p6OpenStrings(got); !slices.Equal(s, c.want) {
			t.Errorf("%s: OpenRequestLines =\n%s\nwant\n%s", c.who, strings.Join(s, "\n"), strings.Join(c.want, "\n"))
		}
		for _, o := range got {
			if o.ID != prA.ID && o.ID != prB.ID && o.ID != prC.ID {
				t.Errorf("%s: listed request %d %q", c.who, o.ID, o.Display)
			}
		}
	}
}

// p6OpenStrings renders OpenRequestLines for comparison: request, requester,
// needed-by, and per row its line no, item code, unit, pending and holders.
func p6OpenStrings(os []purchase.OpenRequest) []string {
	var out []string
	for _, o := range os {
		var rows []string
		for _, r := range o.Rows {
			var hs []string
			for _, h := range r.Holders {
				hs = append(hs, fmt.Sprintf("%s %s %d %s", h.PODisplay, h.State, h.Owner, h.Qty.Plain()))
			}
			rows = append(rows, fmt.Sprintf("%d %s %s pending %s on [%s]", r.LineNo, r.ItemCode, r.UoMCode, r.Pending.Plain(), strings.Join(hs, ", ")))
		}
		out = append(out, fmt.Sprintf("%s by %d %s, needed by %s: [%s]", o.Display, o.RequesterID, o.RequesterName, o.NeededBy.ISO(), strings.Join(rows, " | ")))
	}
	return out
}
