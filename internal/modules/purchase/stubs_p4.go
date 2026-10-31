package purchase

import "github.com/vardhan/internaltools/internal/types"

// P4's stubs (pending and receipt status). P4 deletes this file and adds
// receipt.go; P2's Validate calls receiptStatusOf.

// ReceiptLine is one PO line's quantities, as receiptStatusOf reads them.
type ReceiptLine struct{ Qty, Already, Received types.Qty }

// receiptStatusOf is P4's; the stub answers the empty status, which no
// caller uses before P4.
func receiptStatusOf([]ReceiptLine) string { return "" } //nolint:unused // called by P2, built by P4
