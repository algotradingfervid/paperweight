package expensetest_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// stored is a document's header and lines as the database holds them.
type stored struct {
	state, number, paidOn  sql.NullString
	total, less, advanceID sql.NullInt64
	lines                  int
	lineSum                int64
	request                bool  // an approval request exists
	requestAmount          int64 // of the latest request
}

func read(t *testing.T, e *expensetest.Env, dt string, id int64) stored {
	t.Helper()
	ctx := context.Background()
	var s stored
	err := e.DB.Read(ctx, func(tx *db.Tx) error {
		q := tx.SQL()
		switch dt {
		case expense.ClaimDocType:
			if err := q.QueryRowContext(ctx, `SELECT state, number, total, less_advance, advance_id FROM expense_claim WHERE id = ?`, id).
				Scan(&s.state, &s.number, &s.total, &s.less, &s.advanceID); err != nil {
				return err
			}
			if err := q.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(amount), 0) FROM expense_claim_line WHERE parent_id = ?`, id).
				Scan(&s.lines, &s.lineSum); err != nil {
				return err
			}
		default:
			if err := q.QueryRowContext(ctx, `SELECT state, number, amount, paid_on FROM employee_advance WHERE id = ?`, id).
				Scan(&s.state, &s.number, &s.total, &s.paidOn); err != nil {
				return err
			}
			if err := q.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(amount), 0) FROM employee_advance_line WHERE parent_id = ?`, id).
				Scan(&s.lines, &s.lineSum); err != nil {
				return err
			}
		}
		err := q.QueryRowContext(ctx, `SELECT amount FROM approval_requests WHERE doctype = ? AND doc_id = ? ORDER BY id DESC LIMIT 1`, dt, id).
			Scan(&s.requestAmount)
		if err == sql.ErrNoRows {
			return nil
		}
		s.request = err == nil
		return err
	})
	if err != nil {
		t.Fatalf("read %s %d: %v", dt, id, err)
	}
	return s
}

// setComputed stores the header total (claim) or amount (advance) as the sum
// of its lines when Validate has left it NULL: E2 computes it, and this is
// the value E2 would write, so the approval amount check isn't 0 = 0 in E1.
func setComputed(t *testing.T, e *expensetest.Env, dt string, id int64) {
	t.Helper()
	q := `UPDATE expense_claim SET total = (SELECT SUM(amount) FROM expense_claim_line WHERE parent_id = ?) WHERE id = ? AND total IS NULL`
	if dt == expense.AdvanceDocType {
		q = `UPDATE employee_advance SET amount = (SELECT SUM(amount) FROM employee_advance_line WHERE parent_id = ?) WHERE id = ? AND amount IS NULL`
	}
	if err := e.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), q, id, id)
		return err
	}); err != nil {
		t.Fatalf("set the computed amount of %s %d: %v", dt, id, err)
	}
}

func mustDo(t *testing.T, e *expensetest.Env, email, dt string, id int64, a doc.Action, version int) doc.Result {
	t.Helper()
	p := e.P(t, email)
	res, err := e.Engine.Do(context.Background(), p, dt, id, a, doc.ActionInput{Version: version})
	if err != nil {
		t.Fatalf("%s %s %d as %s: %v", a, dt, id, email, err)
	}
	return res
}

// DraftClaim stays a draft: no number, no approval request, its lines stored.
func TestExpensetest_DraftClaim(t *testing.T) {
	e := expensetest.New(t)
	res := e.DraftClaim(t, e.P(t, "deepak@vardhan.test"), nil,
		expensetest.Values{"amount": "1000.00"}, expensetest.Values{"amount": "500.00"})
	s := read(t, e, expense.ClaimDocType, res.ID)
	if res.State != doc.StateDraft || s.state.String != "draft" || s.number.Valid || s.request {
		t.Errorf("DraftClaim: Result.State %q, stored state %q, number %v, approval request %v; want draft, no number, no request",
			res.State, s.state.String, s.number, s.request)
	}
	if s.lines != 2 || s.lineSum != 150000 {
		t.Errorf("DraftClaim lines = %d summing %d paise, want 2 summing 150000", s.lines, s.lineSum)
	}
}

// ApprovedClaim and PaidAdvance reach approved with the first numbers of FY
// 2026-27; PaidAdvance sets paid_on.
func TestExpensetest_ApprovedClaimAndPaidAdvance(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")

	c := e.ApprovedClaim(t, deepak, nil)
	s := read(t, e, expense.ClaimDocType, c.ID)
	if c.State != doc.StateApproved || s.state.String != "approved" || s.number.String != "EX/26-27/00001" {
		t.Errorf("ApprovedClaim: Result.State %q, stored state %q, number %q; want approved, approved, EX/26-27/00001",
			c.State, s.state.String, s.number.String)
	}

	a := e.PaidAdvance(t, deepak, types.Money(500000))
	s = read(t, e, expense.AdvanceDocType, a.ID)
	if s.state.String != "approved" || s.number.String != "EA/26-27/00001" || s.paidOn.String != expensetest.Date {
		t.Errorf("PaidAdvance: stored state %q, number %q, paid_on %v; want approved, EA/26-27/00001, %s",
			s.state.String, s.number.String, s.paidOn, expensetest.Date)
	}
	if s.lineSum != 500000 {
		t.Errorf("PaidAdvance line amounts sum to %d paise, want 500000", s.lineSum)
	}
}

// Against keeps the claim's lines and total and stores the advance and
// less_advance.
func TestExpensetest_Against(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	adv := e.PaidAdvance(t, deepak, types.Money(500000))
	c := e.DraftClaim(t, deepak, nil, expensetest.Values{"amount": "1000.00"}, expensetest.Values{"amount": "500.00"})
	before := read(t, e, expense.ClaimDocType, c.ID)

	e.Against(t, deepak, c.ID, adv.ID, types.Money(150000))
	after := read(t, e, expense.ClaimDocType, c.ID)
	if after.lines != before.lines || after.lineSum != before.lineSum || after.lines != 2 {
		t.Errorf("after Against: %d lines summing %d paise, want the %d lines summing %d it had",
			after.lines, after.lineSum, before.lines, before.lineSum)
	}
	if after.total != before.total {
		t.Errorf("after Against: total %v, want unchanged %v", after.total, before.total)
	}
	if !after.less.Valid || after.less.Int64 != 150000 || after.advanceID.Int64 != adv.ID {
		t.Errorf("after Against: less_advance %v, advance_id %v; want 150000, %d", after.less, after.advanceID, adv.ID)
	}
}

// The approval request's amount is the claim's total and the advance's
// amount (Hooks.Amount), and the number's FY follows claim_date and
// advance_date (Hooks.Date): both dated 2026-03-31 get FY 2025-26 numbers.
func TestExpensetest_AmountAndDate(t *testing.T) {
	const (
		deepak = "deepak@vardhan.test"
		priya  = "priya@vardhan.test" // Deepak's reporting manager
		march  = "2026-03-31"
	)
	e := expensetest.New(t)

	c := e.DraftClaim(t, e.P(t, deepak), expensetest.Values{"claim_date": march},
		expensetest.Values{"expense_date": march, "amount": "1234.00"})
	setComputed(t, e, expense.ClaimDocType, c.ID)
	c = mustDo(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, c.Version)
	if c.State == doc.StatePending {
		c = mustDo(t, e, priya, expense.ClaimDocType, c.ID, doc.ActionApprove, c.Version)
	}
	s := read(t, e, expense.ClaimDocType, c.ID)
	if !s.request || s.requestAmount != 123400 || s.total.Int64 != 123400 {
		t.Errorf("claim: approval request %v with amount %d, stored total %v; want a request for the total 123400", s.request, s.requestAmount, s.total)
	}
	if s.state.String != "approved" || s.number.String != "EX/25-26/00001" {
		t.Errorf("claim dated %s: state %q, number %q; want approved, EX/25-26/00001", march, s.state.String, s.number.String)
	}

	a := e.DraftAdvance(t, e.P(t, deepak), expensetest.Values{"advance_date": march}, expensetest.Values{"line_amount": "2500.00"})
	setComputed(t, e, expense.AdvanceDocType, a.ID)
	a = mustDo(t, e, deepak, expense.AdvanceDocType, a.ID, doc.ActionSubmit, a.Version)
	if a.State == doc.StatePending {
		a = mustDo(t, e, priya, expense.AdvanceDocType, a.ID, doc.ActionApprove, a.Version)
	}
	s = read(t, e, expense.AdvanceDocType, a.ID)
	if !s.request || s.requestAmount != 250000 || s.total.Int64 != 250000 {
		t.Errorf("advance: approval request %v with amount %d, stored amount %v; want a request for the amount 250000", s.request, s.requestAmount, s.total)
	}
	if s.state.String != "approved" || s.number.String != "EA/25-26/00001" {
		t.Errorf("advance dated %s: state %q, number %q; want approved, EA/25-26/00001", march, s.state.String, s.number.String)
	}
}
