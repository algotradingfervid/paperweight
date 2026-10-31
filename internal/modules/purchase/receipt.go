package purchase

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Pending is a PO line's pending quantity, never stored (spec §3.2):
// max(0, qty − already − received). Frozen for MOD-GRN (CD-276).
func Pending(qty, already, received types.Qty) types.Qty {
	return max(0, qty-already-received)
}

// ReceiptLine is one PO line's quantities, as receiptStatusOf reads them.
type ReceiptLine struct{ Qty, Already, Received types.Qty }

// receiptStatusOf is the PO's receipt_status (spec §6.3): not_received when no
// line has anything received (already_received counts), fully_received when
// every line's pending is 0, and part_received otherwise. P2's Validate calls
// it on every draft save.
func receiptStatusOf(lines []ReceiptLine) string {
	anything, full := false, true
	for _, l := range lines {
		if l.Already+l.Received > 0 {
			anything = true
		}
		if Pending(l.Qty, l.Already, l.Received) > 0 {
			full = false
		}
	}
	switch {
	case !anything:
		return "not_received"
	case full:
		return "fully_received"
	default:
		return "part_received"
	}
}

// RefreshReceiptStatus writes the PO's receipt_status from accepted, each PO
// line's accepted total as the caller computed it (spec §6.3; GRN's hooks run
// before the kernel's roll-up, so received_qty is not read). A line missing
// from the map counts 0; a key that isn't one of the PO's lines is a plain
// internal error and nothing is written. The write is a plain UPDATE (CD-280
// D3). For a closed PO it then refreshes the linked requests (P6, CD-276).
// Frozen for MOD-GRN (CD-276).
func RefreshReceiptStatus(ctx context.Context, tx *db.Tx, poID int64, accepted map[int64]types.Qty) error {
	q := dbq.New(tx.SQL())
	rows, err := q.ReceiptLines(ctx, poID)
	if err != nil {
		return err
	}
	lines := make([]ReceiptLine, 0, len(rows))
	onPO := make(map[int64]bool, len(rows))
	for _, r := range rows {
		onPO[r.ID] = true
		lines = append(lines, ReceiptLine{Qty: types.Qty(r.Qty), Already: types.Qty(r.AlreadyReceived), Received: accepted[r.ID]})
	}
	for id := range accepted {
		if !onPO[id] {
			return fmt.Errorf("purchase: refresh receipt status of PO %d: line %d is not on it", poID, id)
		}
	}
	if err := q.SetReceiptStatus(ctx, dbq.SetReceiptStatusParams{ReceiptStatus: receiptStatusOf(lines), ID: poID}); err != nil {
		return err
	}
	state, err := q.OrderState(ctx, poID)
	if err != nil {
		return err
	}
	if state == string(doctype.StateClosed) {
		return refreshClosedRequests(ctx, tx, poID, accepted)
	}
	return nil
}
