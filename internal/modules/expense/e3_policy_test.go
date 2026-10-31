package expense_test

import (
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// EXP-010: on the real files.Store, expense.Claim takes the claim policy
// (MaxCount 100, spec §3.3): 21 distinct files are accepted, the 101st is
// refused with the count from the policy, and another DocType still refuses
// the 21st with the text at 20 (§15 item 2). Each file is a tiny PDF of a
// different size, so no two share bytes.
func TestExp_ReceiptPolicy(t *testing.T) {
	const (
		limit100 = "A document can have at most 100 attachments. Remove one first."
		limit20  = "A document can have at most 20 attachments. Remove one first."
	)
	e := expensetest.New(t)
	deepak := e.P(t, e3Deepak)

	claim := e.DraftClaim(t, deepak, nil)
	attach := func(dt string, id int64, i int) error {
		_, err := e.AttachFile(t, deepak, dt, id, fmt.Sprintf("r%03d.pdf", i), filestest.PDF(100+i))
		return err
	}
	for i := 1; i <= 21; i++ {
		if err := attach(expense.ClaimDocType, claim.ID, i); err != nil {
			t.Fatalf("claim: attach #%d: %v, want accepted", i, err)
		}
	}
	if got := e.FilesOf(t, expense.ClaimDocType, claim.ID); len(got) != 21 {
		t.Fatalf("claim files after 21 = %d, want 21", len(got))
	}
	for i := 22; i <= 100; i++ {
		if err := attach(expense.ClaimDocType, claim.ID, i); err != nil {
			t.Fatalf("claim: attach #%d: %v, want accepted", i, err)
		}
	}
	e.WantRefused(t, limit100, func() error {
		err := attach(expense.ClaimDocType, claim.ID, 101)
		e3WantFiles(t, err, files.KindLimit, limit100)
		return err
	})
	if got := e.FilesOf(t, expense.ClaimDocType, claim.ID); len(got) != 100 {
		t.Errorf("claim files after the refusal = %d, want 100", len(got))
	}

	// Another DocType keeps the default policy.
	adv := e.DraftAdvance(t, deepak, nil)
	for i := 1; i <= 20; i++ {
		if err := attach(expense.AdvanceDocType, adv.ID, 200+i); err != nil {
			t.Fatalf("advance: attach #%d: %v, want accepted", i, err)
		}
	}
	e.WantRefused(t, limit20, func() error {
		err := attach(expense.AdvanceDocType, adv.ID, 221)
		e3WantFiles(t, err, files.KindLimit, limit20)
		return err
	})
}
