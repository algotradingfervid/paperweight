package expense_test

import (
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// TestAdv_OtherAdvanceWarning: money left on another of the employee's
// approved, paid, not closed advances warns on the advance's check and
// approval pages, only for a viewer who can read it (EXP-060; CD-271 G7,
// CD-281 d; CD-305 (1)). On the real auth.Service.
func TestAdv_OtherAdvanceWarning(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya, meena, ravi := e.P(t, e4Deepak), e.P(t, e4Priya), e.P(t, e4Meena), e.P(t, e4Ravi)

	paid := e.PaidAdvance(t, deepak, e4Rs(10000)) // EA/26-27/00001
	e.ApprovedClaim(t, deepak, e4On(paid.ID, "6000.00"), expensetest.Values{"amount": "6000.00"})
	e4WantUsed(t, e, paid.ID, "₹6,000.00 used", e4Rs(6000), e4Rs(4000))
	unpaid := e.ApprovedAdvance(t, deepak, e4Rs(2000)) // approved, not paid: doesn't warn
	closed := e.PaidAdvance(t, deepak, e4Rs(3000))     // paid, then closed: doesn't warn
	e6Do(t, e, meena, expense.AdvanceDocType, closed.ID, doc.ActionClose, "₹3,000 returned by NEFT 1234 on 30-11-2026")
	others := []string{e4Number(t, e, expense.AdvanceDocType, paid.ID), e4Number(t, e, expense.AdvanceDocType, unpaid.ID),
		e4Number(t, e, expense.AdvanceDocType, closed.ID)}
	if others[0] != e4EA1 {
		t.Fatalf("the paid advance is %q, want %s", others[0], e4EA1)
	}

	named := fmt.Sprintf(e4FmtWarning, "Deepak Nair", "4,000.00", e4EA1)
	b := e.DraftAdvance(t, deepak, nil)
	e4WantWarnings(t, "deepak on the check page", e4Warnings(t, e, deepak, b.ID), named)

	if res := e6Do(t, e, deepak, expense.AdvanceDocType, b.ID, doc.ActionSubmit, ""); res.State != doc.StatePending {
		t.Fatalf("submit: %q, want pending (a warning never refuses)", res.State)
	}
	e4WantWarnings(t, "priya on the approval page", e4Warnings(t, e, priya, b.ID), named)
	if res := e6Do(t, e, priya, expense.AdvanceDocType, b.ID, doc.ActionApprove, ""); res.State != doc.StateApproved {
		t.Fatalf("approve: %q, want approved (a warning never refuses)", res.State)
	}

	// A viewer who reads the new advance but not the paid one. Ravi reads a
	// record he created (own); Deepak isn't his direct report, so he can't
	// read EA/26-27/00001. created_by is set by a fixture write, since only an
	// Administrator (who reads everything) may enter another employee's advance.
	err := e.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(), `UPDATE employee_advance SET created_by = ? WHERE id = ?`, int64(ravi.ID), b.ID)
		return err
	})
	if err != nil {
		t.Fatalf("set created_by: %v", err)
	}
	for id, want := range map[int64]bool{b.ID: true, paid.ID: false} {
		info := e6Load(t, e, expense.AdvanceDocType, id).Info
		ref := &auth.RecordRef{DocType: expense.AdvanceDocType, ID: id, CreatedBy: info.CreatedBy, EmployeeID: info.EmployeeID}
		if got := e.Auth.Can(t.Context(), ravi, expense.AdvanceDocType, auth.RightRead, ref) == nil; got != want {
			t.Fatalf("setup: ravi reads advance %d = %v, want %v", id, got, want)
		}
	}
	ws := e4Warnings(t, e, ravi, b.ID)
	// An advance the viewer can't read gives no warning: neither its number
	// nor its amount appears (CD-305 (1)).
	e4WantWarnings(t, "ravi, who can't read the paid advance", ws)
	expensetest.WantNotNamed(t, e6Joined(ws), append(others, "4,000.00")...)
}
