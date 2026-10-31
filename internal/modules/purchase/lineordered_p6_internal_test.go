package purchase

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-PO P6 Do 1 (PO spec §20.2 "What a PO line counts towards its request
// row"): draft, pending and rejected hold qty; approved orders qty; closed
// orders min(qty, already_received + received_qty), so a short close frees
// the rest; cancelled counts nothing.
func TestLineOrdered(t *testing.T) {
	const k = types.Qty(1000) // 1 unit in thousandths
	for _, c := range []struct {
		state                  doctype.State
		qty, already, received types.Qty
		ordered, held          types.Qty
	}{
		{doctype.StateDraft, 40 * k, 5 * k, 0, 0, 40 * k},
		{doctype.StatePending, 40 * k, 0, 0, 0, 40 * k},
		{doctype.StateRejected, 40 * k, 0, 0, 0, 40 * k},
		{doctype.StateApproved, 40 * k, 5 * k, 20 * k, 40 * k, 0},
		{doctype.StateClosed, 40 * k, 5 * k, 20 * k, 25 * k, 0},
		{doctype.StateClosed, 40 * k, 0, 0, 0, 0},
		{doctype.StateClosed, 40 * k, 10 * k, 45 * k, 40 * k, 0}, // over-received: capped at qty
		{doctype.StateClosed, 2500, 0, 1500, 1500, 0},            // fractional
		{doctype.StateCancelled, 40 * k, 5 * k, 20 * k, 0, 0},
	} {
		o, h := lineOrdered(c.state, c.qty, c.already, c.received)
		if o != c.ordered || h != c.held {
			t.Errorf("lineOrdered(%s, %s, %s, %s) = %s, %s; want %s, %s", c.state, c.qty.Plain(), c.already.Plain(), c.received.Plain(),
				o.Plain(), h.Plain(), c.ordered.Plain(), c.held.Plain())
		}
	}
}
