package approval_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// CD-156: deleting the newest rule must not hand its id to the next one.
func TestApprovalIDs_RuleIDNotReused(t *testing.T) {
	e := at.New(t)
	in := func(rs int64) approval.RuleInput {
		return approval.RuleInput{DocType: at.Receipt, FromAmount: at.Rs(rs), ApproverType: approval.ApproverRole, Role: "Director"}
	}
	_, err := e.A.CreateRule(t.Context(), e.Anil, in(1000))
	at.MustOK(t, err)
	newest, err := e.A.CreateRule(t.Context(), e.Anil, in(2000))
	at.MustOK(t, err)
	at.MustOK(t, e.A.DeleteRule(t.Context(), e.Anil, newest.ID))
	next, err := e.A.CreateRule(t.Context(), e.Anil, in(3000))
	at.MustOK(t, err)
	if next.ID == newest.ID {
		t.Errorf("new rule reused deleted id %d", newest.ID)
	}
}

// CD-156: deleting the newest approval request must not hand its id to the
// next one. Requests have no public delete, and a submitted request is
// referenced by append-only history, so the newest request is inserted and
// deleted with direct SQL; the others go through Submit.
func TestApprovalIDs_RequestIDNotReused(t *testing.T) {
	e := at.New(t)
	at.MustOK(t, e.Submit(e.Priya, e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)))
	var newest int64
	at.MustOK(t, e.DB.Write(t.Context(), func(tx *db.Tx) error {
		err := tx.SQL().QueryRowContext(t.Context(), `INSERT INTO approval_requests (
			doctype, doc_id, rule_id, rule_from_amount, amount, approver_roles, allow_self, requester, requester_name, creator,
			submitted_at, created_at, created_by, updated_at, updated_by
		) VALUES (?, 99, ?, 0, 6000000, '[]', 0, ?, 'Priya', ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', ?, '2026-01-01T00:00:00Z', ?)
		RETURNING id`, at.Order, e.R1, e.Priya.ID, e.Priya.ID, e.Priya.ID, e.Priya.ID).Scan(&newest)
		if err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(t.Context(), `DELETE FROM approval_requests WHERE id = ?`, newest)
		return err
	}))
	d := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	r, ok := e.Latest(t, d)
	if !ok {
		t.Fatal("no request after submit")
	}
	if r.ID == newest {
		t.Errorf("new request reused deleted id %d", newest)
	}
}
