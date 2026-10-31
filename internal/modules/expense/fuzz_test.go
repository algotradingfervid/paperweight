package expense_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// FuzzExp_Invariants (EXP-037; spec §4, lessons §8, §10): random sequences
// of save, submit, approve, reject, revise, cancel, close, amend, mark as paid
// and undo paid over Deepak's claims and advances, with paise amounts.
// A refusal is an expected outcome that must change nothing; any other error
// fails. After every step:
//   - a claim's total equals the sum of its lines, and an advance's amount the
//     sum of its lines;
//   - an advance's used equals the sum of less_advance over its approved and
//     closed claims (recompute, D-DOC-13), and each of those claims holds a
//     number in less_advance (written at submit, CD-264 6);
//   - left (amount − used) is never below 0.
//
// The input is read three bytes at a time: op, x, y (fuzzOp). The seed corpus
// runs in every normal test run.
func FuzzExp_Invariants(f *testing.F) {
	const (
		newAdvance = iota
		newClaim
		editClaim
		submit
		approve
		reject
		revise
		cancel
		closeDoc
		amend
		markPaid
		undoPaid
		numOps
	)
	// Docs x: even is advance x/2, odd is claim x/2 (modulo the count).
	seeds := [][]byte{
		// Paid ₹10,047.99 advance; a ₹1,238.00 claim typing ₹150.05 approved,
		// cancelled, amended and approved again.
		{newAdvance, 200, 0, submit, 0, 0, approve, 0, 0, markPaid, 0, 0,
			newClaim, 100, 5, submit, 1, 0, approve, 1, 0, cancel, 1, 0, amend, 1, 0, submit, 3, 0, approve, 3, 0},
		// Less advance blank (as much as the advance allows): two claims of
		// ₹3,155.35 against a ₹49.99 advance; the second approve meets what's
		// left. Then close the first claim and the advance.
		{newAdvance, 0, 0, submit, 0, 0, approve, 0, 0, markPaid, 0, 0,
			newClaim, 255, 4, newClaim, 255, 8, submit, 1, 0, submit, 3, 0, approve, 1, 0, approve, 3, 0,
			closeDoc, 1, 0, closeDoc, 0, 0, undoPaid, 0, 0},
		// Not paid: submit refused; reject, revise, edit, resubmit after paid;
		// the advance's cancel blocked, then refused while paid.
		{newAdvance, 120, 0, submit, 0, 0, approve, 0, 0, newClaim, 40, 2, submit, 1, 0,
			markPaid, 0, 0, submit, 1, 0, reject, 1, 0, revise, 1, 0, editClaim, 200, 5, submit, 1, 0, approve, 1, 0,
			cancel, 0, 0, undoPaid, 0, 0, cancel, 1, 0, undoPaid, 0, 0, cancel, 0, 0},
		// Two advances; claims naming each, one typed above the total and one
		// above what's left.
		{newAdvance, 10, 0, newAdvance, 90, 0, submit, 0, 0, submit, 2, 0, approve, 0, 0, approve, 2, 0,
			markPaid, 0, 0, markPaid, 1, 0, newClaim, 1, 1, newClaim, 200, 2, submit, 1, 0, submit, 3, 0,
			approve, 1, 0, approve, 3, 0, newClaim, 70, 26, submit, 5, 0, approve, 5, 0},
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, in []byte) {
		const maxSteps = 24
		e := expensetest.New(t)
		ctx := context.Background()
		deepak, meena := e.P(t, e4Deepak), e.P(t, e4Meena)
		var advances, claims []int64

		// pick is the document x names, or ok false when there is none.
		pick := func(x byte) (dt string, id int64, ok bool) {
			list, dt := advances, expense.AdvanceDocType
			if x%2 == 1 {
				list, dt = claims, expense.ClaimDocType
			}
			if len(list) == 0 {
				return "", 0, false
			}
			return dt, list[int(x/2)%len(list)], true
		}
		// amount is a positive paise amount from b: ₹1.00 to ₹3,155.35.
		amount := func(b byte) types.Money { return types.Money(int64(b)*1237 + 100) }
		advanceAmount := func(b byte) types.Money { return types.Money((int64(b) + 1) * 4999) }
		// header names an advance from y (none when y%3 == 0) and a less
		// advance (blank when y%4 == 0, else paise from y).
		header := func(y byte) expensetest.Values {
			v := expensetest.Values{}
			if y%3 != 0 && len(advances) > 0 {
				v["advance_id"] = e4ID(advances[int(y)%len(advances)])
			}
			if y%4 != 0 {
				v["less_advance"] = types.Money(int64(y) * 3001).Plain()
			}
			return v
		}
		version := func(dt string, id int64) int { return e6Load(t, e, dt, id).Version }

		for step := 0; step+2 < len(in) && step/3 < maxSteps; step += 3 {
			op, x, y := in[step]%numOps, in[step+1], in[step+2]
			before := e.Fingerprint(t)
			what := fmt.Sprintf("step %d: op %d x %d y %d", step/3, op, x, y)
			var err error
			act := func(a doc.Action, p string, reason string) {
				dt, id, ok := pick(x)
				if !ok {
					return
				}
				what += fmt.Sprintf(" (%s %s %d)", a, dt, id)
				var res doc.Result
				res, err = e.Engine.Do(ctx, e.P(t, p), dt, id, a, doc.ActionInput{Version: version(dt, id), Reason: reason})
				if err == nil && a == doc.ActionAmend {
					if dt == expense.ClaimDocType {
						claims = append(claims, res.ID)
					} else {
						advances = append(advances, res.ID)
					}
				}
			}
			switch op {
			case newAdvance:
				var res doc.Result
				res, err = e.Create(deepak, expense.AdvanceDocType,
					e.AdvanceInput(t, deepak, nil, expensetest.Values{"line_amount": advanceAmount(x).Plain()}))
				if err == nil {
					advances = append(advances, res.ID)
				}
			case newClaim:
				var res doc.Result
				res, err = e.Create(deepak, expense.ClaimDocType,
					e.ClaimInput(t, deepak, header(y), expensetest.Values{"amount": amount(x).Plain()}))
				if err == nil {
					claims = append(claims, res.ID)
				}
			case editClaim:
				if len(claims) > 0 {
					id := claims[int(x)%len(claims)]
					what += fmt.Sprintf(" (edit claim %d)", id)
					h := header(y)
					if _, ok := h["less_advance"]; !ok {
						h["less_advance"] = ""
					}
					_, err = e.Edit(t, deepak, expense.ClaimDocType, id, h, expensetest.Values{"amount": amount(x ^ y).Plain()})
				}
			case submit:
				act(doc.ActionSubmit, e4Deepak, "")
			case approve:
				act(doc.ActionApprove, e4Priya, "")
			case reject:
				act(doc.ActionReject, e4Priya, "Not needed for this trip.")
			case revise:
				act(doc.ActionRevise, e4Deepak, "")
			case cancel:
				act(doc.ActionCancel, e4Meena, "Booked twice.")
			case closeDoc:
				act(doc.ActionClose, e4Meena, "Settled in full.")
			case amend:
				act(doc.ActionAmend, e4Deepak, "")
			case markPaid, undoPaid:
				if len(advances) > 0 {
					id := advances[int(x)%len(advances)]
					what += fmt.Sprintf(" (paid %d)", id)
					if op == markPaid {
						on, _ := types.DateFromISO(expensetest.Date)
						err = e.Service.MarkPaid(ctx, meena, id, version(expense.AdvanceDocType, id), on)
					} else {
						err = e.Service.UndoPaid(ctx, meena, id, version(expense.AdvanceDocType, id))
					}
				}
			}
			if err != nil {
				var st interface{ Status() int }
				if !errors.As(err, &st) || st.Status() >= 500 {
					t.Fatalf("%s: %v (%T), want success or a refusal", what, err, err)
				}
				if after := e.Fingerprint(t); after != before {
					t.Fatalf("%s: refused with %q but wrote:\nbefore %s\nafter  %s", what, err, before, after)
				}
			}
			fuzzInvariants(t, e, what)
		}
	})
}

// fuzzInvariants fails the fuzz case when any invariant of FuzzExp_Invariants
// is broken after what.
func fuzzInvariants(t *testing.T, e *expensetest.Env, what string) {
	t.Helper()
	checks := []struct{ name, q string }{
		{"claim total isn't the sum of its lines", `SELECT c.id || ': total ' || COALESCE(c.total, 'NULL') || ', lines ' ||
			(SELECT COALESCE(SUM(l.amount), 0) FROM expense_claim_line l WHERE l.parent_id = c.id)
			FROM expense_claim c
			WHERE COALESCE(c.total, -1) != (SELECT COALESCE(SUM(l.amount), 0) FROM expense_claim_line l WHERE l.parent_id = c.id)`},
		{"advance amount isn't the sum of its lines", `SELECT a.id || ': amount ' || COALESCE(a.amount, 'NULL')
			FROM employee_advance a
			WHERE COALESCE(a.amount, -1) != (SELECT COALESCE(SUM(l.amount), 0) FROM employee_advance_line l WHERE l.parent_id = a.id)`},
		{"used isn't the sum over approved and closed claims", `SELECT a.id || ': used ' || a.used_amount || ', claims ' ||
			(SELECT COALESCE(SUM(c.less_advance), 0) FROM expense_claim c WHERE c.advance_id = a.id AND c.state IN ('approved', 'closed'))
			FROM employee_advance a
			WHERE a.used_amount != (SELECT COALESCE(SUM(c.less_advance), 0) FROM expense_claim c
				WHERE c.advance_id = a.id AND c.state IN ('approved', 'closed'))`},
		{"approved or closed claim on an advance has no less_advance", `SELECT c.id || ' on advance ' || c.advance_id
			FROM expense_claim c WHERE c.advance_id IS NOT NULL AND c.state IN ('approved', 'closed') AND c.less_advance IS NULL`},
		{"left is below 0", `SELECT a.id || ': amount ' || a.amount || ', used ' || a.used_amount
			FROM employee_advance a WHERE COALESCE(a.amount, 0) - a.used_amount < 0`},
	}
	err := e.DB.Read(context.Background(), func(tx *db.Tx) error {
		for _, c := range checks {
			rows, err := tx.SQL().QueryContext(context.Background(), c.q)
			if err != nil {
				return fmt.Errorf("%s: %w", c.name, err)
			}
			for rows.Next() {
				var s string
				if err := rows.Scan(&s); err != nil {
					_ = rows.Close()
					return err
				}
				t.Errorf("%s: %s: %s", what, c.name, s)
			}
			if err := rows.Close(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("%s: read the invariants: %v", what, err)
	}
	if t.Failed() {
		t.FailNow()
	}
}
