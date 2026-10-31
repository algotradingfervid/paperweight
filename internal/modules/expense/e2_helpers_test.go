package expense_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// E2's shared test helpers (Claim and advance Validate).

// The 00-fixtures users E2's cases use.
const (
	e2Admin  = "admin@vardhan.test"  // Administrator, EMP-001
	e2Meena  = "meena@vardhan.test"  // Director, EMP-002; Employee in the expense seed
	e2Priya  = "priya@vardhan.test"  // EMP-004, Deepak's reporting manager
	e2Kavya  = "kavya@vardhan.test"  // Masters Manager, EMP-006
	e2Deepak = "deepak@vardhan.test" // Employee, EMP-007
)

// e2Scan runs q in a read tx and scans its one row into dest.
func e2Scan(t *testing.T, e *expensetest.Env, q string, args []any, dest ...any) {
	t.Helper()
	ctx := context.Background()
	err := e.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, q, args...).Scan(dest...)
	})
	if err != nil {
		t.Fatalf("%s %v: %v", q, args, err)
	}
}

// e2Employee is the database id of the fixture employee with code.
func e2Employee(t *testing.T, e *expensetest.Env, code string) int64 {
	t.Helper()
	for _, emp := range e.F.Employees {
		if emp.Code == code {
			return emp.ID
		}
	}
	t.Fatalf("no fixture employee %s", code)
	return 0
}

// e2Branch is the database id of the fixture branch with code.
func e2Branch(t *testing.T, e *expensetest.Env, code string) int64 {
	t.Helper()
	for _, b := range e.F.Branches {
		if b.Code == code {
			return b.ID
		}
	}
	t.Fatalf("no fixture branch %s", code)
	return 0
}

// e2Type is the expense type id with code as a form value.
func e2Type(t *testing.T, e *expensetest.Env, code string) string {
	t.Helper()
	id, ok := e.F.ExpenseTypeIDs[code]
	if !ok {
		t.Fatalf("no expense type %s", code)
	}
	return e2ID(id)
}

func e2ID(id int64) string { return strconv.FormatInt(id, 10) }

// e2Messages are err's message and its field messages.
func e2Messages(err error) []string {
	if err == nil {
		return nil
	}
	out := []string{err.Error()}
	var he doctype.HTTPError
	if errors.As(err, &he) {
		for _, f := range he.FieldErrors() {
			out = append(out, f.Message)
		}
	}
	return out
}

// e2Has reports whether err carries text as its message or a field message.
func e2Has(err error, text string) bool {
	for _, m := range e2Messages(err) {
		if m == text {
			return true
		}
	}
	return false
}

// e2Must returns a check that fails the test when the engine's error isn't
// nil, for e2Must(t, what)(e.Edit(...)).
func e2Must(t *testing.T, what string) func(doc.Result, error) doc.Result {
	t.Helper()
	return func(res doc.Result, err error) doc.Result {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v (messages %q)", what, err, e2Messages(err))
		}
		return res
	}
}

// e2Claim is a stored claim header, as the database holds it.
type e2Claim struct {
	employeeID                int64
	snapName, snapCode, state string
	total, net, less          *int64
	periodFrom, periodTo      *string
}

func e2ReadClaim(t *testing.T, e *expensetest.Env, id int64) e2Claim {
	t.Helper()
	var c e2Claim
	e2Scan(t, e, `SELECT COALESCE(employee_id, 0), snap_employee_name, snap_employee_code, state,
		total, net_payable, less_advance, period_from, period_to FROM expense_claim WHERE id = ?`, []any{id},
		&c.employeeID, &c.snapName, &c.snapCode, &c.state, &c.total, &c.net, &c.less, &c.periodFrom, &c.periodTo)
	return c
}

// e2Advance is a stored advance header.
type e2Advance struct {
	employeeID         int64
	snapName, snapCode string
	amount             *int64
}

func e2ReadAdvance(t *testing.T, e *expensetest.Env, id int64) e2Advance {
	t.Helper()
	var a e2Advance
	e2Scan(t, e, `SELECT COALESCE(employee_id, 0), snap_employee_name, snap_employee_code, amount
		FROM employee_advance WHERE id = ?`, []any{id}, &a.employeeID, &a.snapName, &a.snapCode, &a.amount)
	return a
}

// e2Line is a stored claim line.
type e2Line struct {
	typeCode, typeName, projectCode, siteCode string
	hsnSAC, billedGSTIN                       string
	taxCodeID, billedToID, gstAmount          *int64
}

// e2ReadLine reads line lineNo (1-based) of claim id.
func e2ReadLine(t *testing.T, e *expensetest.Env, id int64, lineNo int) e2Line {
	t.Helper()
	var l e2Line
	e2Scan(t, e, `SELECT snap_type_code, snap_type_name, snap_project_code, snap_site_code, hsn_sac,
		snap_billed_gstin, tax_code_id, billed_to_id, gst_amount
		FROM expense_claim_line WHERE parent_id = ? AND line_no = ?`, []any{id, lineNo},
		&l.typeCode, &l.typeName, &l.projectCode, &l.siteCode, &l.hsnSAC, &l.billedGSTIN, &l.taxCodeID, &l.billedToID, &l.gstAmount)
	return l
}

// e2Int is *p, or -1 for NULL, for messages and comparisons.
func e2Int(p *int64) int64 {
	if p == nil {
		return -1
	}
	return *p
}

// e2Str is *p, or "NULL".
func e2Str(p *string) string {
	if p == nil {
		return "NULL"
	}
	return *p
}
