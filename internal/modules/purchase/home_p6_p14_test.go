package purchase_test

import (
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// These cases need P6 (RefreshOrderStatus on approve; PendingToOrder counting
// held and ordered rows). They fail until P6 is merged; P14 lists them in its
// hand-in.

// p14TwoLineRequest is Suresh's submitted METRO-SITE request for 100 kg
// MS-ANGLE-50 and 50 kg WIRE-18G, with its line ids.
func p14TwoLineRequest(t *testing.T, env *purchasetest.Env) (id, angle, wire int64) {
	t.Helper()
	id = p14Request(t, env, env.Suresh, "METRO-SITE", "PRJ-METRO",
		[]map[string]string{p5Line(t, env, "MS-ANGLE-50", "KGS", "100"), p5Line(t, env, "WIRE-18G", "KGS", "50")}, true, nil)
	lines := p5Load(t, env, purchase.RequestDocType, id).Lines
	return id, lines[0].ID, lines[1].ID
}

// p14OrderFor is Priya's PO for qty of item on request line rl, at stop.
func p14OrderFor(t *testing.T, env *purchasetest.Env, item, qty string, rl int64, stop doctype.State) int64 {
	t.Helper()
	l := p2Line(t, env, item, "KGS", qty, "62.5")
	l["request_line_id"] = strconv.FormatInt(rl, 10)
	return p14Metro(t, env, env.Priya, []map[string]string{l}, stop, nil)
}

// UX §5.2 my-requests, spec §20.3: the row's order status follows
// RefreshOrderStatus: Part ordered with one of two rows on an approved PO,
// Ordered with both. The request stays open (submitted), so it stays listed.
func TestPurchaseHome_P6MyRequestsOrderStatus(t *testing.T) {
	env := purchasetest.New(t)
	req, angle, wire := p14TwoLineRequest(t, env)
	meta := func() []string {
		v := p14QueueOf(t, env, env.Suresh, purchase.QueueMyRequests, 1)
		if len(v.Rows) != 1 || v.Rows[0].ID != req {
			t.Fatalf("Suresh's my-requests = %v, want [%d]", p14IDs(v), req)
		}
		return v.Rows[0].Meta
	}
	p14OrderFor(t, env, "MS-ANGLE-50", "100", angle, doctype.StateApproved)
	if m := meta(); len(m) != 2 || m[1] != "Part ordered" {
		t.Errorf("meta with one row ordered = %v, want [METRO-SITE Part ordered]", m)
	}
	p14OrderFor(t, env, "WIRE-18G", "50", wire, doctype.StateApproved)
	if m := meta(); len(m) != 2 || m[1] != "Ordered" {
		t.Errorf("meta with both rows ordered = %v, want [METRO-SITE Ordered]", m)
	}
}

// Spec §20.2, §25 Requests to order: only rows with pending to order above 0
// count. A row held by a draft PO has none, so one row is left to order; once
// the other is on an approved PO the request leaves the queue and the card.
func TestPurchaseHome_P6RequestsToOrderPending(t *testing.T) {
	env := purchasetest.New(t)
	req, angle, wire := p14TwoLineRequest(t, env)
	p14OrderFor(t, env, "MS-ANGLE-50", "100", angle, doctype.StateDraft) // held
	p14WantRows(t, "requests-to-order with one row held", p14QueueOf(t, env, env.Priya, purchase.QueueRequestsToOrder, 1), []p14Row{
		p14RequestRow(t, env, req, doctype.StateApproved, "Suresh Patil", "METRO-SITE", "1 row to order"),
	})
	p14OrderFor(t, env, "WIRE-18G", "50", wire, doctype.StateApproved)
	if v := p14QueueOf(t, env, env.Priya, purchase.QueueRequestsToOrder, 1); v.Total != 0 || len(v.Rows) != 0 {
		t.Errorf("requests-to-order with nothing pending = %v, want none", p14IDs(v))
	}
	if c, _ := p14Card(p14HomeOf(t, env, env.Priya), purchase.QueueRequestsToOrder); c.Count != 0 {
		t.Errorf("Requests to order = %d, want 0", c.Count)
	}
}
