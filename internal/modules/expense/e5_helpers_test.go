package expense_test

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// The fixture users of the E5 cases (00-fixtures.md).
const (
	e5Admin  = "admin@vardhan.test"  // Anil Kapoor, Administrator, EMP-001
	e5Meena  = "meena@vardhan.test"  // Meena Iyer, Director (+ Employee in the expense seed), EMP-002, no manager
	e5Ravi   = "ravi@vardhan.test"   // Ravi Kulkarni, EMP-003, reports to EMP-002
	e5Priya  = "priya@vardhan.test"  // Priya Shah, EMP-004, reports to EMP-003
	e5Kavya  = "kavya@vardhan.test"  // Kavya Rao, EMP-006, reports to EMP-002
	e5Deepak = "deepak@vardhan.test" // Deepak Nair, EMP-007, reports to EMP-004
)

// APR's and AUTH's texts the E5 cases expect (CD-142): the producing
// package's wording, never a copy of our own.
const (
	e5NoOneElseClaim   = "No one else can approve this Expense Claim. Give the Fallback Approver role to another active user, then submit again."
	e5NoOneElseAdvance = "No one else can approve this Employee Advance. Give the Fallback Approver role to another active user, then submit again."
	e5ReasonRequired   = "Enter a reason for rejecting."
	e5CloseDeniedClaim = "You don't have permission to cancel Expense Claim. Ask your administrator for a role that allows it."
	e5CloseDeniedAdv   = "You don't have permission to cancel Employee Advance. Ask your administrator for a role that allows it."
	e5ExportDenied     = "You don't have permission to export Expense Claim. Ask your administrator for a role that allows it."
	e5OwnClaimFallback = "You can't approve your own Expense Claim. Ask another Fallback Approver to approve it."
)

// e5NotEligible is APR's not-eligible text for a request with a reporting
// manager (approval/errors.go notEligible): "Only {manager} or a user with
// the Fallback Approver role can approve {display} ({amount})."
func e5NotEligible(manager, display, amount string) string {
	return "Only " + manager + " or a user with the Fallback Approver role can approve " + display + " (" + amount + ")."
}

// e5Do runs one engine action and returns its error.
func e5Do(e *expensetest.Env, p auth.Principal, dt string, id int64, a doc.Action, in doc.ActionInput) (doc.Result, error) {
	return e.Engine.Do(context.Background(), p, dt, id, a, in)
}

// e5Must runs one engine action that must succeed.
func e5Must(t *testing.T, e *expensetest.Env, p auth.Principal, dt string, id int64, a doc.Action, in doc.ActionInput) doc.Result {
	t.Helper()
	res, err := e5Do(e, p, dt, id, a, in)
	if err != nil {
		t.Fatalf("%s %s %d as %s: %v", a, dt, id, p.Name, err)
	}
	return res
}

// e5Submit submits res as p through the engine; E2's Validate computes the
// claim's total or the advance's amount.
func e5Submit(t *testing.T, e *expensetest.Env, p auth.Principal, dt string, res doc.Result) (doc.Result, error) {
	t.Helper()
	return e5Do(e, p, dt, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
}

// e5MustSubmit is e5Submit that must leave the document pending.
func e5MustSubmit(t *testing.T, e *expensetest.Env, p auth.Principal, dt string, res doc.Result) doc.Result {
	t.Helper()
	out, err := e5Submit(t, e, p, dt, res)
	if err != nil {
		t.Fatalf("submit %s %d as %s: %v", dt, res.ID, p.Name, err)
	}
	if out.State != doc.StatePending {
		t.Fatalf("submit %s %d as %s: state %s, want pending", dt, res.ID, p.Name, out.State)
	}
	return out
}

// e5Exec runs one fixture statement in a write transaction.
func e5Exec(t *testing.T, e *expensetest.Env, q string, args ...any) {
	t.Helper()
	if err := e.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), q, args...)
		return err
	}); err != nil {
		t.Fatalf("fixture %q: %v", q, err)
	}
}

// e5Eligible is APR's list of who may approve the document now.
func e5Eligible(t *testing.T, e *expensetest.Env, dt string, id int64) []types.UserID {
	t.Helper()
	var users []types.UserID
	if err := e.DB.Read(context.Background(), func(tx *db.Tx) (err error) {
		users, err = e.Approvals.EligibleApprovers(context.Background(), tx, dt, id)
		return err
	}); err != nil {
		t.Fatalf("EligibleApprovers(%s, %d): %v", dt, id, err)
	}
	return users
}

// e5WantEligible fails unless exactly the users with emails are eligible.
func e5WantEligible(t *testing.T, e *expensetest.Env, dt string, id int64, emails ...string) {
	t.Helper()
	want := []types.UserID{}
	for _, m := range emails {
		want = append(want, e5UID(t, m))
	}
	slices.Sort(want)
	if got := e5Eligible(t, e, dt, id); !slices.Equal(got, want) {
		t.Errorf("eligible approvers of %s %d = %v, want %v (%v)", dt, id, got, want, emails)
	}
}

// e5UID is the fixture user's id.
func e5UID(t *testing.T, email string) types.UserID {
	t.Helper()
	return testkit.Principal(t, email).ID
}

// e5Emp is the database id of fixture employee code.
func e5Emp(t *testing.T, e *expensetest.Env, code string) int64 {
	t.Helper()
	for _, emp := range e.F.Employees {
		if emp.Code == code {
			return emp.ID
		}
	}
	t.Fatalf("no fixture employee %s", code)
	return 0
}

// e5Project is the database id of fixture project code.
func e5Project(t *testing.T, e *expensetest.Env, code string) int64 {
	t.Helper()
	for _, p := range e.F.Projects {
		if p.Code == code {
			return p.ID
		}
	}
	t.Fatalf("no fixture project %s", code)
	return 0
}

// e5Info is the document as the engine describes it.
func e5Info(t *testing.T, e *expensetest.Env, dt string, id int64) doctype.Info {
	t.Helper()
	var in doctype.Info
	if err := e.DB.Read(context.Background(), func(tx *db.Tx) (err error) {
		in, err = e.Engine.Info(context.Background(), tx, dt, id)
		return err
	}); err != nil {
		t.Fatalf("Info(%s, %d): %v", dt, id, err)
	}
	return in
}

// e5WantState fails unless the stored document is in state with number.
func e5WantState(t *testing.T, e *expensetest.Env, dt string, id int64, state doctype.State, number string) {
	t.Helper()
	in := e5Info(t, e, dt, id)
	if in.State != state || in.Number != number {
		t.Errorf("%s %d is %s %q, want %s %q", dt, id, in.State, in.Number, state, number)
	}
}

// e5Ref is the document as AUTH sees it, built from the engine's Info as the
// document page does (docscreen recordRef).
func e5Ref(t *testing.T, e *expensetest.Env, dt string, id int64) *auth.RecordRef {
	t.Helper()
	in := e5Info(t, e, dt, id)
	return &auth.RecordRef{
		DocType: in.DocType, ID: in.ID, CreatedBy: in.CreatedBy,
		EmployeeID: in.EmployeeID, ProjectIDs: in.ProjectIDs, SiteIDs: in.SiteIDs,
	}
}

// e5Open is opening the document page: AUTH's read on the record (the page's
// check, docscreen View).
func e5Open(t *testing.T, e *expensetest.Env, p auth.Principal, dt string, id int64) error {
	t.Helper()
	return e.Auth.Can(context.Background(), p, dt, auth.RightRead, e5Ref(t, e, dt, id))
}

// e5ListIDs is p's list of dt through the real list service (auth.Scope),
// ids sorted.
func e5ListIDs(t *testing.T, e *expensetest.Env, p auth.Principal, dt string) []int64 {
	t.Helper()
	typ, ok := doctype.Lookup(dt)
	if !ok {
		t.Fatalf("%s not registered", dt)
	}
	res, err := (&list.Service{DB: e.DB, Auth: e.Auth}).List(context.Background(), p, typ, list.Query{Page: 1, PageSize: 500})
	if err != nil {
		t.Fatalf("list %s as %s: %v", dt, p.Name, err)
	}
	ids := []int64{}
	for _, r := range res.Rows {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	return ids
}

// e5WantList fails unless p's list of dt is exactly ids.
func e5WantList(t *testing.T, e *expensetest.Env, p auth.Principal, dt string, ids ...int64) {
	t.Helper()
	want := slices.Clone(ids)
	slices.Sort(want)
	if got := e5ListIDs(t, e, p, dt); !slices.Equal(got, want) {
		t.Errorf("%s's %s list = %v, want %v", p.Name, dt, got, want)
	}
}

// e5Request is the document's latest approval request.
func e5Request(t *testing.T, e *expensetest.Env, dt string, id int64) approval.Request {
	t.Helper()
	var r approval.Request
	var ok bool
	if err := e.DB.Read(context.Background(), func(tx *db.Tx) (err error) {
		r, ok, err = e.Approvals.LatestRequest(context.Background(), tx, dt, id)
		return err
	}); err != nil {
		t.Fatalf("LatestRequest(%s, %d): %v", dt, id, err)
	}
	if !ok {
		t.Fatalf("%s %d has no approval request", dt, id)
	}
	return r
}

// e5Stuck lists the document ids of dt on Stuck approvals (APR-043), as the
// Administrator sees them.
func e5Stuck(t *testing.T, e *expensetest.Env, dt string) []int64 {
	t.Helper()
	items, err := e.Approvals.Stuck(context.Background(), e.P(t, e5Admin))
	if err != nil {
		t.Fatalf("Stuck: %v", err)
	}
	ids := []int64{}
	for _, it := range items {
		if it.DocType == dt {
			ids = append(ids, it.DocID)
		}
	}
	return ids
}

// e5StateReason is the stored state and state_reason of a claim.
func e5StateReason(t *testing.T, e *expensetest.Env, table string, id int64) (state, reason string) {
	t.Helper()
	var r sql.NullString
	if err := e.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(),
			`SELECT state, state_reason FROM `+table+` WHERE id = ?`, id).Scan(&state, &r)
	}); err != nil {
		t.Fatalf("read %s %d: %v", table, id, err)
	}
	return state, r.String
}

// e5Load is the stored record.
func e5Load(t *testing.T, e *expensetest.Env, dt string, id int64) doc.Record {
	t.Helper()
	var rec doc.Record
	if err := e.DB.Read(context.Background(), func(tx *db.Tx) (err error) {
		rec, err = e.Engine.Load(context.Background(), tx, dt, id)
		return err
	}); err != nil {
		t.Fatalf("Load(%s, %d): %v", dt, id, err)
	}
	return rec
}
