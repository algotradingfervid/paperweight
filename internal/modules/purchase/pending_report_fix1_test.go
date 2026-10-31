package purchase_test

import (
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// PO-037 (PO spec §13): pending is qty less already received less received,
// so a line of 50 with 20 already received and 30 received has no row; its
// sibling line (50, 20 already, nothing received) has 30 pending.
func TestPO_PendingCountsAlreadyReceived(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"earlier_po_no": "OLD-7"}), purchasetest.OrderLines(
		p9Line(t, env, "MS-ANGLE-50", "KGS", "50", "62.5", "7216", map[string]string{"already_received": "20"}),
		p9Line(t, env, "WIRE-18G", "KGS", "50", "96", "7217", map[string]string{"already_received": "20"}),
	))
	purchasetest.SetReceived(t, env, po.Lines[0].ID, p9Qty(t, "30"))

	v := p9Report(t, env, env.Priya, purchase.PendingFilter{})
	p9Want(t, "50 = 20 already + 30 received", v, po.Lines[1].ID)
	if got, ok := p9Find(t, "sibling line", v, po.Lines[1].ID); ok && (got.Pending != p9Qty(t, "30") || got.Received != p9Qty(t, "20")) {
		t.Errorf("sibling row = %+v, want 30 pending, 20 received", got)
	}
}

// PO-037 (PO spec §13; CD-299): rows on the same due date go by PO number,
// then line number: two POs of two lines each, both due 28-10-2026, list
// the lower number's lines 1 and 2 before the other PO's.
func TestPO_PendingOrderByNumber(t *testing.T) {
	env := purchasetest.New(t)
	two := func() doc.Doc {
		return purchasetest.NewOrder(t, env,
			purchasetest.OrderHeader(map[string]string{"required_by": "28-10-2026"}),
			purchasetest.OrderLines(
				p9Line(t, env, "WIRE-18G", "KGS", "1", "1", "7217", nil),
				p9Line(t, env, "WIRE-18G", "KGS", "2", "1", "7217", nil),
			))
	}
	first, second := two(), two()
	if first.Number > second.Number {
		first, second = second, first
	}
	want := []int64{first.Lines[0].ID, first.Lines[1].ID, second.Lines[0].ID, second.Lines[1].ID}
	var got []int64
	for _, r := range p9Report(t, env, env.Priya, purchase.PendingFilter{}).Rows {
		got = append(got, r.LineID)
	}
	if !slices.Equal(got, want) {
		t.Errorf("row order = %v, want %v (PO %s lines 1 and 2, then PO %s)", got, want, first.Number, second.Number)
	}
}

// PO-081 (PO spec §27.1; CD-299): "today" is the IST business date. At
// 31-10-2026 00:30 IST (30-10 19:00 UTC) a line due 30-10 is Overdue and
// counts in OverdueCount; one due 31-10 (today) isn't.
func TestPO_PendingOverdueUsesISTDate(t *testing.T) {
	env := purchasetest.New(t)
	oct30 := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"required_by": "30-10-2026"}))
	oct31 := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"required_by": "31-10-2026"}))
	env.SetNow(t, time.Date(2026, time.October, 30, 19, 0, 0, 0, time.UTC))

	v := p9Report(t, env, env.Priya, purchase.PendingFilter{})
	if got, ok := p9Find(t, "due 30-10", v, oct30.Lines[0].ID); ok && !got.Overdue {
		t.Errorf("due 30-10 at 31-10 00:30 IST: Overdue = false, want true")
	}
	if got, ok := p9Find(t, "due 31-10", v, oct31.Lines[0].ID); ok && got.Overdue {
		t.Errorf("due 31-10 at 31-10 00:30 IST: Overdue = true, want false (due today)")
	}
	p9Want(t, "overdue only", p9Report(t, env, env.Priya, purchase.PendingFilter{OverdueOnly: true}), oct30.Lines[0].ID)
	if n := p9Overdue(t, env, env.Priya); n != 1 {
		t.Errorf("OverdueCount at 31-10 00:30 IST = %d, want 1", n)
	}
}
