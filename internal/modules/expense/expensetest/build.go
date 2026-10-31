package expensetest

import (
	"context"
	"maps"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Date is the 00-fixtures business date of every document the builders make.
const Date = "2026-10-24"

// Values are form values by field name. Builders merge them over their
// defaults, so a case states only what it cares about (research T2); a value
// of "" sends the field blank.
type Values = map[string]string

// DraftClaim saves a new claim as p. Header defaults: p's employee, the
// business date, purpose `Pune Metro site visit`. With no lines it has one
// default line; each line defaults to TRAVEL, ₹1,000.00 on the business date
// with a no-receipt reason.
func (e *Env) DraftClaim(t testing.TB, p auth.Principal, header Values, lines ...Values) doc.Result {
	t.Helper()
	in := doc.ActionInput{Values: merge(Values{
		"employee_id": e.employeeOf(p), "claim_date": Date, "purpose": "Pune Metro site visit",
	}, header)}
	if len(lines) == 0 {
		lines = []Values{nil}
	}
	for _, l := range lines {
		in.Lines = append(in.Lines, merge(Values{
			"expense_date": Date, "expense_type_id": e.expenseType(t, "TRAVEL"), "amount": "1000.00",
			"no_receipt_reason": "Paid in cash.",
		}, l))
	}
	return e.do(t, p, expense.ClaimDocType, 0, doc.ActionSave, in)
}

// ApprovedClaim is DraftClaim, submitted by p and approved by the claimant's
// reporting manager. A claimant with no manager needs a Fallback Approver:
// such a case drives the steps itself.
func (e *Env) ApprovedClaim(t testing.TB, p auth.Principal, header Values, lines ...Values) doc.Result {
	t.Helper()
	return e.approve(t, p, expense.ClaimDocType, e.DraftClaim(t, p, header, lines...))
}

// DraftAdvance saves a new advance as p. Header defaults: p's employee, the
// business date, purpose `Pune Metro site visit`. With no lines it has one
// default line: TRAVEL, ₹5,000.00.
func (e *Env) DraftAdvance(t testing.TB, p auth.Principal, header Values, lines ...Values) doc.Result {
	t.Helper()
	in := doc.ActionInput{Values: merge(Values{
		"employee_id": e.employeeOf(p), "advance_date": Date, "purpose": "Pune Metro site visit",
	}, header)}
	if len(lines) == 0 {
		lines = []Values{nil}
	}
	for _, l := range lines {
		in.Lines = append(in.Lines, merge(Values{"expense_type_id": e.expenseType(t, "TRAVEL"), "line_amount": "5000.00"}, l))
	}
	return e.do(t, p, expense.AdvanceDocType, 0, doc.ActionSave, in)
}

// ApprovedAdvance is an advance of amount for p's employee, submitted by p
// and approved by p's reporting manager.
func (e *Env) ApprovedAdvance(t testing.TB, p auth.Principal, amount types.Money) doc.Result {
	t.Helper()
	res := e.DraftAdvance(t, p, nil, Values{"line_amount": amount.Plain()})
	return e.approve(t, p, expense.AdvanceDocType, res)
}

// PaidAdvance is ApprovedAdvance marked as paid on its advance date. It sets
// paid_on with a fixture query, not E9's Mark as paid, so only the Mark as
// paid cases depend on E9; the version and audit log don't change.
func (e *Env) PaidAdvance(t testing.TB, p auth.Principal, amount types.Money) doc.Result {
	t.Helper()
	res := e.ApprovedAdvance(t, p, amount)
	err := e.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(),
			`UPDATE employee_advance SET paid_on = advance_date WHERE id = ?`, res.ID)
		return err
	})
	if err != nil {
		t.Fatalf("expensetest: mark advance %d paid: %v", res.ID, err)
	}
	return res
}

// Against sets draft claim claimID against advanceID for less, saved as p
// with the claim's stored version and every stored line value, so only
// advance_id and less_advance change.
func (e *Env) Against(t testing.TB, p auth.Principal, claimID, advanceID int64, less types.Money) doc.Result {
	t.Helper()
	var rec doc.Record
	err := e.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		rec, err = e.Engine.Load(context.Background(), tx, expense.ClaimDocType, claimID)
		return err
	})
	if err != nil {
		t.Fatalf("expensetest: load claim %d: %v", claimID, err)
	}
	in := doc.ActionInput{Version: rec.Version, Values: Values{
		"advance_id": strconv.FormatInt(advanceID, 10), "less_advance": less.Plain(),
	}}
	for _, l := range rec.Lines {
		in.Lines = append(in.Lines, maps.Clone(l)) // each line in full: a line sent with only its id is blank
	}
	return e.do(t, p, expense.ClaimDocType, claimID, doc.ActionSave, in)
}

// UsedLeft reads the advance's used amount and what is left on it
// (amount − used; research T3).
func (e *Env) UsedLeft(t testing.TB, advanceID int64) (used, left types.Money) {
	t.Helper()
	var amount int64
	err := e.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(),
			`SELECT COALESCE(amount, 0), used_amount FROM employee_advance WHERE id = ?`, advanceID).Scan(&amount, &used)
	})
	if err != nil {
		t.Fatalf("expensetest: read advance %d: %v", advanceID, err)
	}
	return used, types.Money(amount) - used
}

// approve submits res as p and, when the submit leaves it pending, approves
// it as the claimant's reporting manager.
func (e *Env) approve(t testing.TB, p auth.Principal, dt string, res doc.Result) doc.Result {
	t.Helper()
	res = e.do(t, p, dt, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	if res.State != doc.StatePending {
		return res
	}
	return e.do(t, e.managerOf(t, p), dt, res.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
}

func (e *Env) do(t testing.TB, p auth.Principal, dt string, id int64, a doc.Action, in doc.ActionInput) doc.Result {
	t.Helper()
	res, err := e.Engine.Do(context.Background(), p, dt, id, a, in)
	if err != nil {
		t.Fatalf("expensetest: %s %s %d as %s: %v", a, dt, id, p.Name, err)
	}
	return res
}

// employeeOf is the id of p's employee as a form value, "" when p has none.
func (e *Env) employeeOf(p auth.Principal) string {
	for _, emp := range e.F.Employees {
		if emp.UserID == p.ID {
			return strconv.FormatInt(emp.ID, 10)
		}
	}
	return ""
}

// managerOf is the user of p's employee's reporting manager.
func (e *Env) managerOf(t testing.TB, p auth.Principal) auth.Principal {
	t.Helper()
	var manager string
	for _, emp := range e.F.Employees {
		if emp.UserID == p.ID {
			manager = emp.ManagerCode
		}
	}
	for _, emp := range e.F.Employees {
		if manager != "" && emp.Code == manager {
			for _, u := range e.F.Users {
				if u.ID == emp.UserID {
					return e.P(t, u.Email)
				}
			}
		}
	}
	t.Fatalf("expensetest: %s has no reporting manager with a user; approve as a Fallback Approver in the case", p.Name)
	return auth.Principal{}
}

func (e *Env) expenseType(t testing.TB, code string) string {
	t.Helper()
	id, ok := e.F.ExpenseTypeIDs[code]
	if !ok {
		t.Fatalf("expensetest: no expense type %s", code)
	}
	return strconv.FormatInt(id, 10)
}

// merge is over on a copy of defaults.
func merge(defaults, over Values) Values {
	out := maps.Clone(defaults)
	maps.Copy(out, over)
	return out
}
