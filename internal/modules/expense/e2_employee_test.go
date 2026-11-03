package expense_test

import (
	"context"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Spec §5's employee texts (claims; an advance says advances).
const (
	msgNoEmployee      = "Your user isn't linked to an employee. Ask your administrator."
	msgNoEmployeeAdmin = "Your user isn't linked to an employee. Enter the employee this is for, or link your user to an employee." // R12
	msgOnlyOwnClaims   = "You can only raise claims for yourself."
	msgOnlyOwnAdvances = "You can only raise advances for yourself."
)

// TestExp_EmployeeRules: a blank employee defaults to the updater's, with
// the snapshots; naming another employee is refused for anyone but an
// Administrator, also when the employee edits a draft an Administrator
// entered for them (the check reads updated_by, not created_by). The same
// for an advance.
func TestExp_EmployeeRules(t *testing.T) { // EXP-001
	e := expensetest.New(t)
	deepak, admin := e.P(t, e2Deepak), e.P(t, e2Admin)
	emp7, emp6 := e2Employee(t, e, "EMP-007"), e2Employee(t, e, "EMP-006")

	type kind struct {
		dt, refusal string
		input       func(header expensetest.Values) doc.ActionInput
		draft       func(p auth.Principal, header expensetest.Values) doc.Result
		read        func(id int64) (employeeID int64, name, code string)
	}
	kinds := []kind{
		{
			dt: expense.ClaimDocType, refusal: msgOnlyOwnClaims,
			input: func(h expensetest.Values) doc.ActionInput { return e.ClaimInput(t, deepak, h) },
			draft: func(p auth.Principal, h expensetest.Values) doc.Result { return e.DraftClaim(t, p, h) },
			read: func(id int64) (int64, string, string) {
				c := e2ReadClaim(t, e, id)
				return c.employeeID, c.snapName, c.snapCode
			},
		},
		{
			dt: expense.AdvanceDocType, refusal: msgOnlyOwnAdvances,
			input: func(h expensetest.Values) doc.ActionInput { return e.AdvanceInput(t, deepak, h) },
			draft: func(p auth.Principal, h expensetest.Values) doc.Result { return e.DraftAdvance(t, p, h) },
			read: func(id int64) (int64, string, string) {
				a := e2ReadAdvance(t, e, id)
				return a.employeeID, a.snapName, a.snapCode
			},
		},
	}
	for _, k := range kinds {
		t.Run(k.dt, func(t *testing.T) {
			// Blank: Deepak's own employee, with the snapshots.
			res := k.draft(deepak, expensetest.Values{"employee_id": ""})
			if id, name, code := k.read(res.ID); id != emp7 || code != "EMP-007" || name != "Deepak Nair" {
				t.Errorf("blank employee saved as employee %d, snapshot %q %q; want EMP-007 (%d), \"Deepak Nair\" \"EMP-007\"",
					id, name, code, emp7)
			}

			// Naming Kavya on a new document.
			e.WantRefused(t, k.refusal, func() error {
				_, err := e.Create(deepak, k.dt, k.input(expensetest.Values{"employee_id": e2ID(emp6)}))
				return err
			})

			// A draft the Administrator entered for Deepak: Deepak names Kavya.
			byAdmin := k.draft(admin, expensetest.Values{"employee_id": e2ID(emp7)})
			if id, name, code := k.read(byAdmin.ID); id != emp7 || code != "EMP-007" || name != "Deepak Nair" {
				t.Errorf("admin's draft for EMP-007 stored employee %d, snapshot %q %q; want %d, \"Deepak Nair\" \"EMP-007\"",
					id, name, code, emp7)
			}
			e.WantRefused(t, k.refusal, func() error {
				_, err := e.Edit(t, deepak, k.dt, byAdmin.ID, expensetest.Values{"employee_id": e2ID(emp6)})
				return err
			})

			// Keeping EMP-007 saves (on a second such draft, so this step
			// doesn't depend on the refusal above).
			kept := k.draft(admin, expensetest.Values{"employee_id": e2ID(emp7)})
			e2Must(t, "Deepak keeps EMP-007 on admin's draft")(e.Edit(t, deepak, k.dt, kept.ID,
				expensetest.Values{"employee_id": e2ID(emp7), "purpose": "Pune Metro site visit, day 2"}))
			if id, _, _ := k.read(kept.ID); id != emp7 {
				t.Errorf("after Deepak's save the employee is %d, want EMP-007 (%d)", id, emp7)
			}
		})
	}
}

// TestExp_EmployeeAndLeavers: a user with no employee is refused; a leaver's
// claim pending when EMP-008 left is approved, and another is rejected,
// revised and saved (employee_id sets AllowBlocked, and pending and later
// states skip the employee check); an Administrator back-enters a claim for
// the leaver.
func TestExp_EmployeeAndLeavers(t *testing.T) { // EXP-003
	ctx := context.Background()
	e := expensetest.New(t)
	admin := e.P(t, e2Admin)

	t.Run("no linked employee", func(t *testing.T) {
		uid, _, err := e.Auth.CreateUser(ctx, admin, auth.NewUser{
			Email: "nolink@vardhan.test", Name: "Nolan Link", Password: "Correct-Horse-Battery-42",
		})
		if err != nil {
			t.Fatalf("create a user with no employee: %v", err)
		}
		nolink, _, err := e.Auth.LoadPrincipal(ctx, uid)
		if err != nil {
			t.Fatalf("load the new user: %v", err)
		}
		e.WantRefused(t, msgNoEmployee, func() error {
			_, err := e.Create(nolink, expense.ClaimDocType, e.ClaimInput(t, nolink, expensetest.Values{"employee_id": ""}))
			return err
		})
		e.WantRefused(t, msgNoEmployee, func() error {
			_, err := e.Create(nolink, expense.AdvanceDocType, e.AdvanceInput(t, nolink, expensetest.Values{"employee_id": ""}))
			return err
		})
	})

	t.Run("leaver", func(t *testing.T) {
		emp8 := e2Employee(t, e, "EMP-008")
		e.AddRoles(t, e2Meena, roleFallbackApprover) // EMP-008 has no manager
		meena := e.P(t, e2Meena)

		// EMP-008 is Active while the two claims are entered and submitted.
		e2SetEmployee(t, e, emp8, employee.Active)
		var pending []int64
		for range 2 {
			c := e.DraftClaim(t, admin, expensetest.Values{"employee_id": e2ID(emp8)})
			res := e2Must(t, "admin submits the claim for EMP-008")(e.Act(t, admin, expense.ClaimDocType, c.ID, doc.ActionSubmit, ""))
			if res.State != doc.StatePending {
				t.Fatalf("claim for EMP-008 after submit is %q, want pending", res.State)
			}
			pending = append(pending, c.ID)
		}
		e2SetEmployee(t, e, emp8, employee.Left)

		// The first is approved: no MST-120 refusal, no employee refusal.
		res := e2Must(t, "approve the leaver's pending claim")(e.Act(t, meena, expense.ClaimDocType, pending[0], doc.ActionApprove, ""))
		if res.State != doc.StateApproved {
			t.Errorf("leaver's claim after approve is %q, want approved", res.State)
		}

		// The second is rejected, revised to draft and saved.
		e2Must(t, "reject the leaver's claim")(e.Act(t, meena, expense.ClaimDocType, pending[1], doc.ActionReject, "Wrong amount on line 1."))
		res = e2Must(t, "revise the leaver's claim")(e.Act(t, admin, expense.ClaimDocType, pending[1], doc.ActionRevise, ""))
		if res.State != doc.StateDraft {
			t.Fatalf("leaver's claim after revise is %q, want draft", res.State)
		}
		e2Must(t, "save the leaver's revised claim")(e.Edit(t, admin, expense.ClaimDocType, pending[1], nil,
			expensetest.Values{"amount": "900.00"}))
		if c := e2ReadClaim(t, e, pending[1]); c.employeeID != emp8 || c.snapCode != "EMP-008" || c.snapName != "Former Staff" || e2Int(c.total) != 90000 {
			t.Errorf("revised claim: employee %d, snapshot %q %q, total %d; want EMP-008 (%d), \"Former Staff\" \"EMP-008\", 90000",
				c.employeeID, c.snapName, c.snapCode, e2Int(c.total), emp8)
		}

		// Back-entry for the leaver, dated in the last FY.
		const march = "2026-03-31"
		back := e2Must(t, "admin back-enters a claim for EMP-008")(e.Create(admin, expense.ClaimDocType,
			e.ClaimInput(t, admin, expensetest.Values{"employee_id": e2ID(emp8), "claim_date": march},
				expensetest.Values{"expense_date": march})))
		if c := e2ReadClaim(t, e, back.ID); c.employeeID != emp8 || c.snapCode != "EMP-008" || c.snapName != "Former Staff" {
			t.Errorf("back-entered claim: employee %d, snapshot %q %q; want EMP-008 (%d), \"Former Staff\" \"EMP-008\"",
				c.employeeID, c.snapName, c.snapCode, emp8)
		}
		res = e2Must(t, "admin submits the back-entered claim")(e.Act(t, admin, expense.ClaimDocType, back.ID, doc.ActionSubmit, ""))
		if res.State != doc.StatePending {
			t.Errorf("back-entered claim after submit is %q, want pending", res.State)
		}
	})
}

// e2SetEmployee sets the employee's status through the Employee master as
// admin: Active re-joins, Left relieves on the business date.
func e2SetEmployee(t *testing.T, e *expensetest.Env, id int64, status employee.Status) {
	t.Helper()
	ctx := context.Background()
	var emp employee.Employee
	if err := e.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		emp, err = e.Masters.Employee.Get(ctx, tx, id)
		return err
	}); err != nil {
		t.Fatalf("read employee %d: %v", id, err)
	}
	admin := e.P(t, e2Admin)
	var err error
	if status == employee.Active {
		// Re-joining needs an enabled user (MST-112): old@ is disabled.
		if err := e.DB.Write(ctx, func(tx *db.Tx) error { return e.Auth.EnableUser(ctx, tx, emp.UserID) }); err != nil {
			t.Fatalf("enable the user of %s: %v", emp.Code, err)
		}
		err = e.Masters.Employee.SetDisabled(ctx, admin, id, emp.Version, false)
	} else {
		_, err = e.Masters.Employee.Update(ctx, admin, id, emp.Version, employee.Input{
			Code: emp.Code, Name: emp.Name, BranchID: emp.BranchID, UserID: emp.UserID, ManagerID: emp.ManagerID,
			Joining: emp.Joining, Birth: emp.Birth, Status: employee.Left,
			Relieving: types.Date{Year: 2026, Month: time.October, Day: 24},
		})
	}
	if err != nil {
		t.Fatalf("set employee %s %s: %v", emp.Code, status, err)
	}
}
