package purchaseweb

import (
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// p15dCloseNote is the info banner above Reason for closing on request id's
// close page as p, "" with none, and the field's hint.
func (w *p15dWeb) closeNote(t *testing.T, p auth.Principal, id int64) (note, hint string) {
	t.Helper()
	page := w.get(t, p, p15dDoc(purchase.RequestDocType, id)+"/close")
	if page.status != 200 {
		t.Fatalf("%s GET close of %d = %d: %s", p.Name, id, page.status, page.text())
	}
	if got := page.banners("info"); len(got) > 0 {
		note = got[0]
	}
	return note, p15dCollapse(page.doc.Find(".field__hint").First().Text())
}

// PUX-023 and PO-069's page part (spec §20.3, CD-258 Q12, CD-280 D6; UX
// §5.17): a request whose row is held by an unapproved order warns on the
// close page, in the singular for one row, naming each order with its owner;
// a held row of an approved order doesn't count, and with none there is no
// note. The hint is Say why it's no longer needed.
func TestPUX023_RequestCloseWarning(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	h := w.heldPRA(t)
	note, hint := w.closeNote(t, env.Suresh, h.pr.ID)
	want := "1 row is on an order not yet approved: " + p15dDisplay(h.draft) + " (Priya Shah). That order can't be approved until the row is removed."
	if note != want {
		t.Errorf("close note = %q, want %q", note, want)
	}
	if hint != "Say why it's no longer needed." {
		t.Errorf("close hint = %q, want Say why it's no longer needed.", hint)
	}

	// Both rows on unapproved orders: one pending (rules from ₹50,000), one
	// draft; the list is in order, joined with and.
	pr := w.prA(t)
	pending := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), purchasetest.OrderLines(map[string]string{
		"item_id": w.id(t, "item", "CEMENT-OPC"), "uom_id": w.id(t, "uom", "BAG"), "hsn_sac": "2523", "qty": "40", "price": "1500",
		"request_line_id": strconv.FormatInt(pr.Lines[0].ID, 10),
	}))
	draft := w.hold(t, purchasetest.StopAtDraft(), pr.Lines[1].ID, "WIRE-18G", "KGS", "7217", "25")
	if got := p15dState(t, w, pending.ID); got != doctype.StatePending {
		t.Fatalf("the 40 Bag order is %s, want pending", got)
	}
	note, _ = w.closeNote(t, env.Ravi, pr.ID)
	want = "2 rows are on orders not yet approved: " + p15dDisplay(pending) + " (Priya Shah) and " + p15dDisplay(draft) + " (Priya Shah). Those orders can't be approved until the rows are removed."
	if note != want {
		t.Errorf("close note, two rows = %q, want %q", note, want)
	}

	// One draft holding both rows is named once.
	both := w.prA(t)
	one := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(
		map[string]string{"item_id": w.id(t, "item", "CEMENT-OPC"), "uom_id": w.id(t, "uom", "BAG"), "hsn_sac": "2523", "qty": "40",
			"request_line_id": strconv.FormatInt(both.Lines[0].ID, 10)},
		map[string]string{"item_id": w.id(t, "item", "WIRE-18G"), "uom_id": w.id(t, "uom", "KGS"), "hsn_sac": "7217", "qty": "25",
			"request_line_id": strconv.FormatInt(both.Lines[1].ID, 10)},
	))
	note, _ = w.closeNote(t, env.Suresh, both.ID)
	want = "2 rows are on orders not yet approved: " + p15dDisplay(one) + " (Priya Shah). Those orders can't be approved until the rows are removed."
	if note != want {
		t.Errorf("close note, one order on both rows = %q, want %q", note, want)
	}

	// Ordered on an approved PO only: nothing waits on approval.
	done := w.prA(t)
	w.hold(t, purchasetest.StopAtApproved(), done.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "25")
	if note, _ := w.closeNote(t, env.Suresh, done.ID); note != "" {
		t.Errorf("close note with only an approved order = %q, want none", note)
	}
}

// PUX-023's unreadable holders (UX §5.17, CD-248 family): Deepak closes his
// own request, held by orders he can't read; one reads another order, two
// read 2 other orders, and neither is named.
func TestPUX023_RequestCloseWarningUnreadable(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	one := p15dDeepakRequest(t, w)
	w.hold(t, purchasetest.StopAtDraft(), one.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "10")
	note, _ := w.closeNote(t, env.Deepak, one.ID)
	if want := "1 row is on an order not yet approved: another order. That order can't be approved until the row is removed."; note != want {
		t.Errorf("Deepak's close note, one hidden order = %q, want %q", note, want)
	}
	w.hold(t, purchasetest.StopAtDraft(), one.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "5")
	note, _ = w.closeNote(t, env.Deepak, one.ID)
	if want := "1 row is on an order not yet approved: 2 other orders. That order can't be approved until the row is removed."; note != want {
		t.Errorf("Deepak's close note, two hidden orders = %q, want %q", note, want)
	}
}

// PUX-023: the Reason hook adds nothing to the cancel page.
func TestPUX023_RequestCancelNoNote(t *testing.T) {
	w := newP15dWeb(t)
	h := w.heldPRA(t)
	page := w.get(t, w.env.Suresh, p15dDoc(purchase.RequestDocType, h.pr.ID)+"/cancel")
	if page.status != 200 {
		t.Fatalf("GET cancel = %d: %s", page.status, page.text())
	}
	if got := page.banners("info"); len(got) != 0 {
		t.Errorf("cancel page notes = %q, want none", got)
	}
}

// p15dState is PO id's state.
func p15dState(t *testing.T, w *p15dWeb, id int64) doctype.State {
	t.Helper()
	var s string
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT state FROM purchase_order WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return doctype.State(s)
}
