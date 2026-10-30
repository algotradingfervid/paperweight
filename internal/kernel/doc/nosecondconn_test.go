package doc_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// CD-213: every test here is a non-Administrator on the real auth.Service with
// the database pool cut to one connection, so a Tx method that opens its own
// AUTH read fails naming the call site. Setup runs on the full pool.

const (
	nsPO1          = "PO/26-27/00001"
	nsAmount       = types.Money(94_164_00) // the 00-fixtures reference PO
	nsOrderRoles   = "Purchase Manager or Director"
	nsNoRightTo    = "You don't have permission to %s %s. Ask your administrator for a role that allows it."
	nsOtherProject = "PRJ-TECHPARK"
)

// nsEnv is fixturetest's harness with the real AUTH and APR behind Engine.
type nsEnv struct {
	*fixturetest.H
	A   *auth.Auth
	Apr *approval.Approvals
}

// nsNew returns h with the fixture users, the default rights matrix, the
// fixture approval rules, Priya restricted to PRJ-METRO, and an Engine on the
// real AUTH and APR. They need an InfoSource before the engine that serves as
// one exists, so a first engine with neither provides it (as approvaltest does).
func nsNew(t *testing.T) *nsEnv {
	t.Helper()
	ctx := t.Context()
	h := fixturetest.New(t)
	deps := doc.Deps{DB: h.DB, Clock: h.Clock, Naming: h.Naming, Numbers: fixturetest.Numbers(), Files: h.Files}
	info := doc.NewEngine(deps)
	dir := testkit.Directory(h.F)
	a := auth.New(auth.Deps{
		DB: h.DB, Clock: h.Clock, Directory: dir, Audit: auditfake.New(), Refs: &authtest.Refs{}, Info: info,
	})
	if err := a.SeedDefaults(ctx, auth.SeedOptions{}); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	for _, u := range h.F.Users {
		got, _, err := a.LoadPrincipal(ctx, u.ID)
		if err != nil || len(got.Roles) != len(u.Roles) {
			t.Fatalf("user %s: LoadPrincipal = (%+v, %v), want roles %v", u.Email, got, err, u.Roles)
		}
	}
	if err := a.ChangeRestrictions(ctx, h.Admin, h.Priya.ID, auth.DimProject, []int64{h.M.Project["PRJ-METRO"]}, nil, nil); err != nil {
		t.Fatalf("restrict Priya: %v", err)
	}
	rule := `INSERT INTO approval_rules (doctype, from_amount, approver_type, approver_role, fallback_role, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, '2026-10-24T05:00:00.000Z', 1, '2026-10-24T05:00:00.000Z', 1)`
	h.Exec(t, rule, fixture.Order, 50_000_00, "role", "Purchase Manager", "")
	h.Exec(t, rule, fixture.Order, 5_00_000_00, "role", "Director", "")
	h.Exec(t, rule, fixture.Claim, 0, "reporting_manager", "", "Director")
	apr := approval.New(approval.Deps{DB: h.DB, Clock: h.Clock, Auth: a, Info: info, Directory: dir})
	deps.Auth, deps.Approval = a, apr
	h.Engine = doc.NewEngine(deps)
	return &nsEnv{H: h, A: a, Apr: apr}
}

// do runs Engine.Do on the pool of one.
func (e *nsEnv) do(t *testing.T, p auth.Principal, dt string, id int64, a doc.Action, in doc.ActionInput) (doc.Result, error) {
	t.Helper()
	var res doc.Result
	err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) (err error) {
		res, err = e.Engine.Do(ctx, p, dt, id, a, in)
		return err
	})
	return res, err
}

// mustDo is do for a call that has to succeed.
func (e *nsEnv) mustDo(t *testing.T, p auth.Principal, dt string, id int64, a doc.Action, in doc.ActionInput) doc.Result {
	t.Helper()
	res, err := e.do(t, p, dt, id, a, in)
	if err != nil {
		t.Fatalf("Do(%s %d, %s) as %s: %v, want ok", dt, id, a, p.Name, err)
	}
	return res
}

// draftOrder is an unsubmitted order by Priya on PRJ-METRO.
func (e *nsEnv) draftOrder(t *testing.T) doc.Doc {
	t.Helper()
	return e.Load(t, fixture.Order, e.NewOrder(t, e.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000", Rate: "62.5000"}).ID)
}

// deepakClaim is a draft claim by Deepak (EMP-007, reporting to Priya).
func (e *nsEnv) deepakClaim(t *testing.T) doc.Doc {
	t.Helper()
	return e.Load(t, fixture.Claim, e.NewClaim(t, e.Deepak, "EMP-007", fixturetest.CL{Type: "Travel", Amount: "1500.00"}).ID)
}

// pendingClaim is deepakClaim submitted, waiting for Priya or a Director.
func (e *nsEnv) pendingClaim(t *testing.T) doc.Doc {
	t.Helper()
	d := e.deepakClaim(t)
	e.MustDo(t, t.Context(), e.Deepak, fixture.Claim, d.ID, doc.ActionSubmit, doc.ActionInput{Version: d.Version})
	return e.Load(t, fixture.Claim, d.ID)
}

// latest reads the document's latest approval request.
func (e *nsEnv) latest(t *testing.T, dt string, id int64) approval.Request {
	t.Helper()
	var r approval.Request
	var ok bool
	if err := e.DB.Read(t.Context(), func(tx *db.Tx) (err error) {
		r, ok, err = e.Apr.LatestRequest(t.Context(), tx, dt, id)
		return err
	}); err != nil || !ok {
		t.Fatalf("LatestRequest(%s, %d) = (%v, %v), want a request", dt, id, ok, err)
	}
	return r
}

// wantText fails unless err matches target (errors.Is) and has exactly msg.
func wantText(t *testing.T, what string, err, target error, msg string) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("%s: err = %v (%T), want errors.Is %v", what, err, err, target)
		return
	}
	if err.Error() != msg {
		t.Errorf("%s: message = %q, want %q", what, err.Error(), msg)
	}
}

// Create as a user restricted on project: Auth.CanTx on the record, Auth.Restricted
// and CheckValues all run inside the write.
func TestNoSecondConn_DocCreate(t *testing.T) {
	e := nsNew(t)
	res := e.mustDo(t, e.Priya, fixture.Order, 0, doc.ActionSave, doc.ActionInput{Values: map[string]string{
		"supplier_id": fmt.Sprint(e.M.Party["SHREE"]), "project_id": fmt.Sprint(e.M.Project["PRJ-METRO"]),
		"order_date": "2026-10-24", "amount": "0",
	}})
	if res.State != doc.StateDraft || res.Version != 1 {
		t.Errorf("Result = %+v, want a draft at version 1", res)
	}
	if d := e.Load(t, fixture.Order, res.ID); d.CreatedBy != e.Priya.ID {
		t.Errorf("CreatedBy = %d, want Priya (%d)", d.CreatedBy, e.Priya.ID)
	}
}

// A create outside the user's project is refused by Auth.CanTx on the proposed
// record: a 404, because the draft would be hidden from them.
func TestNoSecondConn_DocCreateOutsideRestriction(t *testing.T) {
	e := nsNew(t)
	_, err := e.do(t, e.Priya, fixture.Order, 0, doc.ActionSave, doc.ActionInput{Values: map[string]string{
		"supplier_id": fmt.Sprint(e.M.Party["SHREE"]), "project_id": fmt.Sprint(e.M.Project[nsOtherProject]),
		"order_date": "2026-10-24", "amount": "0",
	}})
	wantText(t, "create on PRJ-TECHPARK", err, auth.ErrNotFound, auth.MsgNotFound)
}

// Save: Auth.CanTx on the stored record and CheckValues.
func TestNoSecondConn_DocSave(t *testing.T) {
	e := nsNew(t)
	d := e.draftOrder(t)
	in := e.In(d)
	in.Values["remarks"] = "changed"
	res := e.mustDo(t, e.Priya, fixture.Order, d.ID, doc.ActionSave, in)
	if res.Version != d.Version+1 {
		t.Errorf("Version = %d, want %d", res.Version, d.Version+1)
	}
	if got := e.Load(t, fixture.Order, d.ID).Header["remarks"]; got != "changed" {
		t.Errorf("remarks = %v, want changed", got)
	}
}

// A save moving the draft outside the user's project passes CanTx on the stored
// record and is refused by CheckValues, with the allowed codes.
func TestNoSecondConn_DocSaveRefusedByCheckValues(t *testing.T) {
	e := nsNew(t)
	d := e.draftOrder(t)
	in := e.In(d)
	in.Values["project_id"] = fmt.Sprint(e.M.Project[nsOtherProject])
	_, err := e.do(t, e.Priya, fixture.Order, d.ID, doc.ActionSave, in)
	wantText(t, "save onto PRJ-TECHPARK", err, auth.ErrForbidden, "You can only use Project PRJ-METRO. Choose one of your allowed projects.")
	var ve *auth.ValueError
	if !errors.As(err, &ve) {
		t.Errorf("err = %T, want *auth.ValueError", err)
	}
}

// Submit under a rule: the real Evaluate lists eligible approvers (UsersWithRight
// and HasRole) inside the write and the document waits for approval.
func TestNoSecondConn_DocSubmitNeedsApproval(t *testing.T) {
	e := nsNew(t)
	r := e.RefPO(t)
	res := e.mustDo(t, e.Priya, fixture.Order, r.ID, doc.ActionSubmit, doc.ActionInput{Version: r.Version})
	if res.State != doc.StatePending {
		t.Errorf("State = %q, want %q", res.State, doc.StatePending)
	}
	got := e.latest(t, fixture.Order, r.ID)
	if got.Outcome != "" || got.Amount != nsAmount || !slices.Equal(got.ApproverRoles, []string{"Purchase Manager", "Director"}) {
		t.Errorf("request = %+v, want open for %d by Purchase Manager and Director", got, nsAmount)
	}
}

// Approve: CanTx, CheckApprover and Record's second CheckApprover, all in the write.
func TestNoSecondConn_DocApprove(t *testing.T) {
	e := nsNew(t)
	d := e.PendingOrder(t)
	res := e.mustDo(t, e.Ravi, fixture.Order, d.ID, doc.ActionApprove, doc.ActionInput{Version: d.Version})
	if res.State != doc.StateApproved || res.Number != nsPO1 {
		t.Errorf("Result = %+v, want approved as %s", res, nsPO1)
	}
	if got := e.latest(t, fixture.Order, d.ID); got.Outcome != approval.OutcomeApproved || got.DecidedBy != e.Ravi.ID {
		t.Errorf("request = %+v, want approved by Ravi", got)
	}
}

// A user who can read the order but holds no approver role is refused by
// CheckApprover through HasRole.
func TestNoSecondConn_DocApproveRefused(t *testing.T) {
	e := nsNew(t)
	d := e.PendingOrder(t)
	_, err := e.do(t, e.Suresh, fixture.Order, d.ID, doc.ActionApprove, doc.ActionInput{Version: d.Version})
	wantText(t, "approve by Stores", err, approval.ErrNotEligible,
		fmt.Sprintf("Only a user with the %s role can approve Draft %d (₹94,164.00).", nsOrderRoles, d.ID))
	if got := e.Load(t, fixture.Order, d.ID).State; got != doc.StatePending {
		t.Errorf("State = %q, want %q", got, doc.StatePending)
	}
}

// Reject: the same checks with a reason.
func TestNoSecondConn_DocReject(t *testing.T) {
	e := nsNew(t)
	d := e.PendingOrder(t)
	res := e.mustDo(t, e.Ravi, fixture.Order, d.ID, doc.ActionReject, doc.ActionInput{Version: d.Version, Reason: "Rate too high"})
	if res.State != doc.StateRejected {
		t.Errorf("State = %q, want %q", res.State, doc.StateRejected)
	}
	if got := e.latest(t, fixture.Order, d.ID); got.Outcome != approval.OutcomeRejected || got.Reason != "Rate too high" {
		t.Errorf("request = %+v, want rejected with the reason", got)
	}
}

// Save on a record with EmployeeField: own through created_by and the employee,
// the path that reads the Directory.
func TestNoSecondConn_DocClaimSaveOwn(t *testing.T) {
	e := nsNew(t)
	d := e.deepakClaim(t)
	in := e.In(d)
	in.Values["remarks"] = "taxi"
	res := e.mustDo(t, e.Deepak, fixture.Claim, d.ID, doc.ActionSave, in)
	if res.Version != d.Version+1 {
		t.Errorf("Version = %d, want %d", res.Version, d.Version+1)
	}
}

// EmployeeField scope refusals: the manager reads the report's claim (team) but
// writes only their own; the manager's manager doesn't see it at all.
func TestNoSecondConn_DocClaimScopeRefusals(t *testing.T) {
	e := nsNew(t)
	d := e.deepakClaim(t)
	in := e.In(d)
	in.Values["remarks"] = "edited"
	_, err := e.do(t, e.Priya, fixture.Claim, d.ID, doc.ActionSave, in)
	wantText(t, "manager save", err, auth.ErrForbidden, "You can only change Expense Claims you created.")
	_, err = e.do(t, e.Ravi, fixture.Claim, d.ID, doc.ActionSave, in)
	wantText(t, "manager's manager save", err, auth.ErrNotFound, auth.MsgNotFound)
}

// Submit and approve of a claim under the reporting-manager rule: Evaluate's
// manager lookup and the approver's team scope on the record.
func TestNoSecondConn_DocClaimSubmitAndManagerApproves(t *testing.T) {
	e := nsNew(t)
	d := e.deepakClaim(t)
	res := e.mustDo(t, e.Deepak, fixture.Claim, d.ID, doc.ActionSubmit, doc.ActionInput{Version: d.Version})
	if res.State != doc.StatePending {
		t.Fatalf("State = %q, want %q", res.State, doc.StatePending)
	}
	if got := e.latest(t, fixture.Claim, d.ID); got.ManagerUser != e.Priya.ID {
		t.Errorf("ManagerUser = %d, want Priya (%d)", got.ManagerUser, e.Priya.ID)
	}
	res = e.mustDo(t, e.Priya, fixture.Claim, d.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
	if res.State != doc.StateApproved {
		t.Errorf("State = %q, want %q", res.State, doc.StateApproved)
	}
}

// A receipt on an order line: create and save both run CheckLink on the order.
func TestNoSecondConn_DocSaveWithLink(t *testing.T) {
	e := nsNew(t)
	order := e.ApprovedOrder(t, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000", Rate: "62.5000"})
	res := e.mustDo(t, e.Suresh, fixture.Receipt, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"receipt_date": "2026-10-24"},
		Lines:  []map[string]string{{"order_line_id": fmt.Sprint(order.Lines[0].ID), "qty": "5.000"}},
	})
	in := e.In(e.Load(t, fixture.Receipt, res.ID))
	in.Lines[0]["qty"] = "6.000"
	saved := e.mustDo(t, e.Suresh, fixture.Receipt, res.ID, doc.ActionSave, in)
	if saved.Version != res.Version+1 {
		t.Errorf("Version = %d, want %d", saved.Version, res.Version+1)
	}
}

// A linked order outside the user's project is refused by CheckLink.
func TestNoSecondConn_DocLinkRefused(t *testing.T) {
	e := nsNew(t)
	order := e.ApprovedOrder(t, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000", Rate: "62.5000"})
	if err := e.A.ChangeRestrictions(t.Context(), e.Admin, e.Suresh.ID, auth.DimProject, []int64{e.M.Project[nsOtherProject]}, nil, nil); err != nil {
		t.Fatalf("restrict Suresh: %v", err)
	}
	_, err := e.do(t, e.Suresh, fixture.Receipt, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"receipt_date": "2026-10-24"},
		Lines:  []map[string]string{{"order_line_id": fmt.Sprint(order.Lines[0].ID), "qty": "5.000"}},
	})
	wantText(t, "receipt on a hidden order", err, auth.ErrForbidden, "You can't use Purchase Order "+nsPO1+". It isn't available to you.")
	var le *auth.LinkError
	if !errors.As(err, &le) {
		t.Errorf("err = %T, want *auth.LinkError", err)
	}
}

// history runs doc.History on the pool of one.
func (e *nsEnv) history(t *testing.T, p auth.Principal, dt string, id int64) ([]audit.Entry, error) {
	t.Helper()
	var out []audit.Entry
	err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) (err error) {
		out, err = doc.History(ctx, e.DB, e.A, p, dt, id)
		return err
	})
	return out, err
}

// History reads the record's rights inside its read transaction.
func TestNoSecondConn_DocHistory(t *testing.T) {
	e := nsNew(t)
	d := e.draftOrder(t)
	got, err := e.history(t, e.Priya, fixture.Order, d.ID)
	if err != nil {
		t.Fatalf("History as Priya: %v, want ok", err)
	}
	if len(got) != 1 || got[0].Action != "create" {
		t.Errorf("History = %+v, want the create entry", got)
	}
}

// History of a record outside the user's project is a 404.
func TestNoSecondConn_DocHistoryRefused(t *testing.T) {
	e := nsNew(t)
	r := e.NewOrderWith(t, e.Admin, map[string]string{"project_id": fmt.Sprint(e.M.Project[nsOtherProject])})
	got, err := e.history(t, e.Priya, fixture.Order, r.ID)
	wantText(t, "History of a hidden order", err, auth.ErrNotFound, auth.MsgNotFound)
	if got != nil {
		t.Errorf("History = %+v, want nil", got)
	}
}

// evaluate runs Evaluate in a write transaction on the pool of one.
func (e *nsEnv) evaluate(t *testing.T, dt string, id int64, amount types.Money, p auth.Principal) (approval.Requirement, error) {
	t.Helper()
	var req approval.Requirement
	err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		return e.DB.Write(ctx, func(tx *db.Tx) (err error) {
			req, err = e.Apr.Evaluate(ctx, tx, dt, id, amount, p)
			return err
		})
	})
	return req, err
}

// Evaluate in a tx: eligible runs UsersWithRight (approve, read) and HasRole.
func TestNoSecondConn_ApprovalEvaluate(t *testing.T) {
	e := nsNew(t)
	r := e.RefPO(t)
	req, err := e.evaluate(t, fixture.Order, r.ID, nsAmount, e.Priya)
	if err != nil {
		t.Fatalf("Evaluate: %v, want ok", err)
	}
	if !req.Needed || !slices.Equal(req.ApproverRoles, []string{"Purchase Manager", "Director"}) {
		t.Errorf("Requirement = %+v, want Needed by Purchase Manager and Director", req)
	}
}

// Evaluate's manager lookup in a tx: UsersWithRight on the employee's record.
func TestNoSecondConn_ApprovalEvaluateManager(t *testing.T) {
	e := nsNew(t)
	d := e.deepakClaim(t)
	req, err := e.evaluate(t, fixture.Claim, d.ID, 1500_00, e.Deepak)
	if err != nil {
		t.Fatalf("Evaluate: %v, want ok", err)
	}
	if !req.Needed || req.ManagerUser != e.Priya.ID {
		t.Errorf("Requirement = %+v, want Needed with ManagerUser Priya (%d)", req, e.Priya.ID)
	}
}

// Evaluate refuses when the role holders lack the approve right.
func TestNoSecondConn_ApprovalEvaluateNoApprover(t *testing.T) {
	e := nsNew(t)
	r := e.RefPO(t)
	e.Exec(t, `UPDATE approval_rules SET approver_role = 'Stores' WHERE doctype = ?`, fixture.Order)
	_, err := e.evaluate(t, fixture.Order, r.ID, nsAmount, e.Priya)
	wantText(t, "Evaluate", err, approval.ErrNoApprover,
		"No one else can approve this Purchase Order. Give the Stores role to another active user, then submit again.")
}

// CheckApprover in a tx: allowed for a role holder, refused for a reader
// without the role.
func TestNoSecondConn_ApprovalCheckApprover(t *testing.T) {
	e := nsNew(t)
	d := e.PendingOrder(t)
	var ravi, suresh error
	err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		return e.DB.Write(ctx, func(tx *db.Tx) error {
			ravi = e.Apr.CheckApprover(ctx, tx, e.Ravi, fixture.Order, d.ID)
			suresh = e.Apr.CheckApprover(ctx, tx, e.Suresh, fixture.Order, d.ID)
			return nil
		})
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if ravi != nil {
		t.Errorf("CheckApprover(Ravi) = %v, want nil", ravi)
	}
	wantText(t, "CheckApprover(Suresh)", suresh, approval.ErrNotEligible,
		fmt.Sprintf("Only a user with the %s role can approve Draft %d (₹94,164.00).", nsOrderRoles, d.ID))
}

// inbox runs InboxOfType on the pool of one.
func (e *nsEnv) inbox(t *testing.T, p auth.Principal) (types.Result[approval.InboxItem], []approval.InboxType) {
	t.Helper()
	var res types.Result[approval.InboxItem]
	var kinds []approval.InboxType
	err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) (err error) {
		res, kinds, err = e.Apr.InboxOfType(ctx, p, "", types.Page{})
		return err
	})
	if err != nil {
		t.Fatalf("InboxOfType(%s): %v, want ok", p.Name, err)
	}
	return res, kinds
}

// InboxOfType runs holdsAny (HasRoleTx) and canApprove (CanTx, eligible) inside its
// read transaction.
func TestNoSecondConn_ApprovalInbox(t *testing.T) {
	e := nsNew(t)
	d := e.PendingOrder(t)
	res, kinds := e.inbox(t, e.Ravi)
	if res.Total != 1 || len(res.Rows) != 1 || res.Rows[0].DocID != d.ID {
		t.Errorf("Ravi's inbox = %+v, want order %d", res, d.ID)
	}
	want := []approval.InboxType{{DocType: fixture.Order, Label: "Purchase Order", Count: 1}}
	if !slices.Equal(kinds, want) {
		t.Errorf("Ravi's inbox types = %+v, want %+v", kinds, want)
	}
}

// A user with no approver role sees nothing: HasRole's answer alone decides.
func TestNoSecondConn_ApprovalInboxEmpty(t *testing.T) {
	e := nsNew(t)
	e.PendingOrder(t)
	if res, kinds := e.inbox(t, e.Suresh); res.Total != 0 || len(kinds) != 0 {
		t.Errorf("Suresh's inbox = %+v, %+v, want empty", res, kinds)
	}
}

// The reporting manager's inbox holds the claim: team scope on the record.
func TestNoSecondConn_ApprovalInboxManager(t *testing.T) {
	e := nsNew(t)
	d := e.pendingClaim(t)
	if res, _ := e.inbox(t, e.Priya); res.Total != 1 || res.Rows[0].DocID != d.ID {
		t.Errorf("Priya's inbox = %+v, want claim %d", res, d.ID)
	}
}

// EligibleApprovers in a read and in a write transaction: Meena and Ravi, the
// holders of the snapshot's roles who can read and approve.
func TestNoSecondConn_ApprovalEligibleApprovers(t *testing.T) {
	e := nsNew(t)
	d := e.PendingOrder(t)
	want := []types.UserID{e.Meena.ID, e.Ravi.ID}
	for _, c := range []struct {
		name string
		run  func(context.Context, func(*db.Tx) error) error
	}{{"read", e.DB.Read}, {"write", e.DB.Write}} {
		t.Run(c.name, func(t *testing.T) {
			var got []types.UserID
			err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
				return c.run(ctx, func(tx *db.Tx) (err error) {
					got, err = e.Apr.EligibleApprovers(ctx, tx, fixture.Order, d.ID)
					return err
				})
			})
			if err != nil || !slices.Equal(got, want) {
				t.Errorf("EligibleApprovers = (%v, %v), want (%v, nil)", got, err, want)
			}
		})
	}
}
