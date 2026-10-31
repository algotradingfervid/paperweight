package expenseweb

import (
	"testing"

	"github.com/vardhan/internaltools/internal/modules/expense"
)

// A claim with no approval history and every receipt leaves its
// "Approved … by …" and "lines without a receipt" values blank (UX §5.13:
// the receipt line shows only when > 0).
func TestS4Impl_OpenBlankValues(t *testing.T) {
	if got := approvedText(expense.OpenClaimRow{ApprovedBy: "Priya Shah"}); got != "" {
		t.Errorf("approvedText with no approval = %q, want blank", got)
	}
	if got := noReceiptText(0); got != "" {
		t.Errorf("noReceiptText(0) = %q, want blank", got)
	}
}
