package expense_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// A claim line given a receipt loses its no-receipt reason on the save, so
// no stale reason sits beside the receipt (AD ruling on S6, CD-302; EXP-066).
// A line with no receipt keeps its reason, and one with neither is still
// refused at submit (EXP-006).
func TestExp_ReceiptClearsReason(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e3Deepak)

	// Two lines, each with expensetest's reason `Paid in cash.`.
	res := e.DraftClaim(t, deepak, nil, nil, nil)
	bill := e.MustAttach(t, deepak, expense.ClaimDocType, res.ID, "bill.jpg", filestest.JPEG(2048))
	// The post keeps line 2's reason, as the line page does when a photo is
	// added and the reason box is left as it was.
	e3Edit(t, e, deepak, res.ID, map[int]expensetest.Values{2: {"receipt_sha256": bill.SHA256}})
	lines := e.LoadDoc(t, expense.ClaimDocType, res.ID).Lines
	if got := lines[1]; got["receipt_sha256"] != bill.SHA256 || got["no_receipt_reason"] != "" {
		t.Errorf("line 2 with a receipt = receipt %q, reason %q; want %s and no reason", got["receipt_sha256"], got["no_receipt_reason"], bill.SHA256)
	}
	if got := lines[0]; got["receipt_sha256"] != "" || got["no_receipt_reason"] != "Paid in cash." {
		t.Errorf("line 1 with no receipt = receipt %q, reason %q; want none and `Paid in cash.`", got["receipt_sha256"], got["no_receipt_reason"])
	}

	// Neither: still refused at submit, unchanged.
	none := e.DraftClaim(t, deepak, nil, expensetest.Values{"no_receipt_reason": ""})
	e.WantRefused(t, e3NoReceipt(1), func() error {
		_, err := e.Engine.Do(context.Background(), deepak, expense.ClaimDocType, none.ID, doc.ActionSubmit, doc.ActionInput{Version: none.Version})
		e3WantField(t, err, 1, "no_receipt_reason", e3NoReceipt(1))
		return err
	})
}
