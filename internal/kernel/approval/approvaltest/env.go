// Package approvaltest is the APR test harness: a real SQLite file, the shared
// auth and directory fakes seeded from the canonical fixtures (CD-56), a
// doctype.InfoSource over stand-in document rows, and a reference driver that
// runs the §5 DOC ↔ APR call protocol (CD-23) in the CD-52 check order inside
// one db.Write per action.
package approvaltest

import (
	"context"
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// Fixture project ids (testkit.Seed inserts PRJ-METRO, then PRJ-TECHPARK).
const (
	ProjMetro    int64 = 1
	ProjTechPark int64 = 2
)

// Rs is rupees as Money (paise).
func Rs(rupees int64) types.Money { return types.Money(rupees * 100) }

// Env is one test's harness.
type Env struct {
	DB    *db.DB
	A     *approval.Approvals
	F     testkit.Fixtures
	Auth  *authfake.Fake      // shared fake (CD-56)
	Dir   *authfake.Directory // testkit.Directory(F)
	Docs  *Docs
	Clock *Clock
	Audit interface {
		audit.Writer
		Entries() []audit.Entry
	}
	Anil, Meena, Ravi, Priya, Suresh, Kavya, Deepak auth.Principal
	R1, R2, E1                                      int64

	nextUser auth.UserID
}

// New builds the harness: testkit DB and seed, shared fakes, approval.New,
// harness tables and the seed rules R1, R2, E1.
func New(t testing.TB) *Env {
	t.Helper()
	d := testkit.NewDB(t)
	fx := testkit.Seed(t, d)
	dir := testkit.Directory(fx)
	e := &Env{
		DB: d, F: fx, Dir: dir, Auth: newAuth(fx, dir), Docs: &Docs{}, Clock: testkit.Clock(), Audit: auditfake.New(),
		Anil:     testkit.Principal(t, "admin@vardhan.test"),
		Meena:    testkit.Principal(t, "meena@vardhan.test"),
		Ravi:     testkit.Principal(t, "ravi@vardhan.test"),
		Priya:    testkit.Principal(t, "priya@vardhan.test"),
		Suresh:   testkit.Principal(t, "suresh@vardhan.test"),
		Kavya:    testkit.Principal(t, "kavya@vardhan.test"),
		Deepak:   testkit.Principal(t, "deepak@vardhan.test"),
		nextUser: 1001,
	}
	MustOK(t, d.Write(context.Background(), func(tx *db.Tx) error {
		for _, q := range harnessTables {
			if _, err := tx.SQL().ExecContext(context.Background(), q); err != nil {
				return err
			}
		}
		return nil
	}))
	e.A = approval.New(approval.Deps{
		DB: d, Clock: e.Clock, Auth: e.Auth, Info: e.Docs, Directory: e.Dir, Audit: e.Audit,
		Lookup: Lookup, All: DocTypes,
	})
	e.R1 = e.AddRule(t, approval.RuleInput{DocType: Order, FromAmount: Rs(50_000), ApproverType: approval.ApproverRole, Role: "Purchase Manager"})
	e.R2 = e.AddRule(t, approval.RuleInput{DocType: Order, FromAmount: Rs(5_00_000), ApproverType: approval.ApproverRole, Role: "Director"})
	e.E1 = e.AddRule(t, approval.RuleInput{DocType: Claim, FromAmount: 0, ApproverType: approval.ApproverManager, FallbackRole: "Director"})
	return e
}

// AddRule inserts a rule directly, bypassing the Administrator check.
func (e *Env) AddRule(t testing.TB, in approval.RuleInput) int64 {
	t.Helper()
	var id int64
	self := int64(0)
	if in.AllowSelfApproval {
		self = 1
	}
	// Raw SQL, not dbq.InsertRule: approvaltest may not import approval/dbq (CD-85).
	now := types.TimestampOf(e.Clock.Now()).String()
	MustOK(t, e.DB.Write(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(),
			`INSERT INTO approval_rules (doctype, from_amount, approver_type, approver_role, fallback_role, allow_self_approval,
				created_at, created_by, updated_at, updated_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			in.DocType, int64(in.FromAmount), string(in.ApproverType), in.Role, in.FallbackRole, self,
			now, int64(e.Anil.ID), now, int64(e.Anil.ID)).Scan(&id)
	}))
	return id
}

// RemoveRules deletes every rule of a DocType.
func (e *Env) RemoveRules(t testing.TB, dt string) {
	t.Helper()
	e.exec(t, `DELETE FROM approval_rules WHERE doctype = ?`, dt)
}

// AddUser adds an enabled user with roles to the auth fake (ids from 1001).
func (e *Env) AddUser(t testing.TB, name, email string, roles ...string) auth.Principal {
	t.Helper()
	id := e.nextUser
	e.nextUser++
	e.Auth.AddUser(auth.UserInfo{ID: id, Email: email, Name: name, Enabled: true})
	for _, r := range roles {
		e.Auth.AddRole(id, r)
	}
	return auth.Principal{ID: id, Name: name, Roles: roles}
}

// RemoveRole takes role away from u from the next call on (CD-86).
func (e *Env) RemoveRole(t testing.TB, u types.UserID, role string) {
	t.Helper()
	e.Auth.RemoveRole(u, role)
}

// Disable disables user u.
func (e *Env) Disable(t testing.TB, u types.UserID) {
	t.Helper()
	MustOK(t, e.Auth.DisableUser(context.Background(), nil, u, "test"))
}

// Doc names a harness document.
type Doc struct {
	Type string
	ID   int64
}

type docSpec struct {
	project int64
	empCode string
	party   string
	state   doctype.State
	number  string
}

// DocOpt changes how NewDoc creates a document.
type DocOpt func(*docSpec)

// Project sets the project (default ProjMetro for Order; none otherwise).
func Project(id int64) DocOpt { return func(s *docSpec) { s.project = id } }

// ForEmployee sets the claimant (default for Claim: the creator's employee).
func ForEmployee(code string) DocOpt { return func(s *docSpec) { s.empCode = code } }

// Party sets the party name shown on inbox cards (CD-55).
func Party(name string) DocOpt { return func(s *docSpec) { s.party = name } }

// InState creates the document directly in state with number (APR-055/057).
func InState(state doctype.State, number string) DocOpt {
	return func(s *docSpec) { s.state, s.number = state, number }
}

// NewDoc creates a document row (a draft unless InState says otherwise).
func (e *Env) NewDoc(t testing.TB, dt string, id int64, amount types.Money, creator auth.Principal, opts ...DocOpt) Doc {
	t.Helper()
	s := docSpec{state: doctype.StateDraft}
	if dt == Order {
		s.project = ProjMetro
	}
	for _, o := range opts {
		o(&s)
	}
	var emp int64
	switch {
	case s.empCode != "":
		emp = e.Emp(s.empCode)
	case dt == Claim:
		emp = e.empOf(creator.ID)
	}
	e.exec(t, `INSERT INTO aprtest_docs (doctype, id, state, number, amount, created_by, employee_id, project_id, party)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		dt, id, string(s.state), s.number, int64(amount), int64(creator.ID), emp, s.project, s.party)
	return Doc{Type: dt, ID: id}
}

// SetAmount changes the document's amount (as a save would).
func (e *Env) SetAmount(t testing.TB, d Doc, m types.Money) {
	t.Helper()
	e.exec(t, `UPDATE aprtest_docs SET amount = ? WHERE doctype = ? AND id = ?`, int64(m), d.Type, d.ID)
}

func (e *Env) exec(t testing.TB, q string, args ...any) {
	t.Helper()
	MustOK(t, e.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), q, args...)
		return err
	}))
}

type actSpec struct {
	comment   string
	failAfter error
}

// ActOpt changes how Approve runs.
type ActOpt func(*actSpec)

// Comment is the approver's comment.
func Comment(s string) ActOpt { return func(a *actSpec) { a.comment = s } }

// FailAfterRecord makes the driver return err after Record, so the
// transaction rolls back.
func FailAfterRecord(err error) ActOpt { return func(a *actSpec) { a.failAfter = err } }

// Submit runs DOC's submit: Evaluate → Open (always) → if no approval is
// needed, approve without CheckApprover (CD-23). Rights before the
// from-state guard, as in DOC's pipeline.
func (e *Env) Submit(p auth.Principal, d Doc) error {
	ctx := context.Background()
	return e.DB.Write(ctx, func(tx *db.Tx) error {
		if err := e.can(ctx, tx, p, d, "submit"); err != nil {
			return err
		}
		if _, err := e.guard(ctx, tx, d, doctype.StateDraft); err != nil {
			return err
		}
		amount, err := docAmount(ctx, tx, d)
		if err != nil {
			return err
		}
		req, err := e.A.Evaluate(ctx, tx, d.Type, d.ID, amount, p)
		if err != nil {
			return err
		}
		if err := e.A.Open(ctx, tx, d.Type, d.ID, req); err != nil {
			return err
		}
		if req.Needed {
			return setState(ctx, tx, d, doctype.StatePending)
		}
		return e.approve(ctx, tx, p, d, actSpec{})
	})
}

// Approve runs DOC's approve: visibility → CheckApprover → from-state guard →
// number, effects → Record (CD-52). No Can(approve).
func (e *Env) Approve(p auth.Principal, d Doc, opts ...ActOpt) error {
	var s actSpec
	for _, o := range opts {
		o(&s)
	}
	ctx := context.Background()
	return e.DB.Write(ctx, func(tx *db.Tx) error {
		if err := e.checkDecider(ctx, tx, p, d); err != nil {
			return err
		}
		return e.approve(ctx, tx, p, d, s)
	})
}

// Reject runs DOC's reject: visibility → CheckApprover → from-state guard →
// Record with the reason (APR checks it).
func (e *Env) Reject(p auth.Principal, d Doc, reason string) error {
	ctx := context.Background()
	return e.DB.Write(ctx, func(tx *db.Tx) error {
		if err := e.checkDecider(ctx, tx, p, d); err != nil {
			return err
		}
		if err := e.A.Record(ctx, tx, d.Type, d.ID, doctype.ActionReject, p, reason); err != nil {
			return err
		}
		return setState(ctx, tx, d, doctype.StateRejected)
	})
}

// Revise runs DOC's revise: rejected → draft (needs write).
func (e *Env) Revise(p auth.Principal, d Doc) error {
	return e.back(p, d, doctype.StateRejected, doctype.ActionRevise, "write")
}

// Withdraw runs DOC's withdraw: pending_approval → draft (R2; needs submit).
func (e *Env) Withdraw(p auth.Principal, d Doc) error {
	return e.back(p, d, doctype.StatePending, doctype.ActionWithdraw, "submit")
}

func (e *Env) back(p auth.Principal, d Doc, from doctype.State, action doctype.Action, right auth.Right) error {
	ctx := context.Background()
	return e.DB.Write(ctx, func(tx *db.Tx) error {
		if err := e.can(ctx, tx, p, d, right); err != nil {
			return err
		}
		if _, err := e.guard(ctx, tx, d, from); err != nil {
			return err
		}
		if err := e.A.Record(ctx, tx, d.Type, d.ID, action, p, ""); err != nil {
			return err
		}
		return setState(ctx, tx, d, doctype.StateDraft)
	})
}

// checkDecider is DOC's part before approve and reject (CD-52): a hidden
// record is a 404 and APR is not called; then CheckApprover; then the
// from-state guard.
func (e *Env) checkDecider(ctx context.Context, tx *db.Tx, p auth.Principal, d Doc) error {
	if err := e.can(ctx, tx, p, d, "read"); err != nil {
		return err
	}
	if err := e.A.CheckApprover(ctx, tx, p, d.Type, d.ID); err != nil {
		return err
	}
	_, err := e.guard(ctx, tx, d, doctype.StatePending)
	return err
}

// can is DOC's rights check on the record, its error returned unchanged.
func (e *Env) can(ctx context.Context, tx *db.Tx, p auth.Principal, d Doc, right auth.Right) error {
	in, err := e.Docs.Info(ctx, tx, d.Type, d.ID)
	if err != nil {
		return err
	}
	ref := &auth.RecordRef{DocType: in.DocType, ID: in.ID, CreatedBy: in.CreatedBy, EmployeeID: in.EmployeeID, ProjectIDs: in.ProjectIDs, SiteIDs: in.SiteIDs}
	return e.Auth.Can(ctx, p, d.Type, right, ref)
}

// approve is the tail of approve and of a submit that needs no approval:
// stand-in numbering and effects, Record, then the approved state.
func (e *Env) approve(ctx context.Context, tx *db.Tx, p auth.Principal, d Doc, s actSpec) error {
	var n int
	if err := tx.SQL().QueryRowContext(ctx,
		`INSERT INTO aprtest_counters (prefix, n) VALUES (?, 1) ON CONFLICT (prefix) DO UPDATE SET n = n + 1 RETURNING n`,
		prefixes[d.Type]).Scan(&n); err != nil {
		return err
	}
	if _, err := tx.SQL().ExecContext(ctx,
		`UPDATE aprtest_docs SET number = ? WHERE doctype = ? AND id = ?`, fmt.Sprintf("%s%05d", prefixes[d.Type], n), d.Type, d.ID); err != nil {
		return err
	}
	if _, err := tx.SQL().ExecContext(ctx,
		`INSERT INTO aprtest_effects (doctype, id, n) VALUES (?, ?, 1) ON CONFLICT (doctype, id) DO UPDATE SET n = n + 1`, d.Type, d.ID); err != nil {
		return err
	}
	if err := e.A.Record(ctx, tx, d.Type, d.ID, doctype.ActionApprove, p, s.comment); err != nil {
		return err
	}
	if s.failAfter != nil {
		return s.failAfter
	}
	return setState(ctx, tx, d, doctype.StateApproved)
}

// prefixes are the stand-in number series (naming is NAM's; not used here).
var prefixes = map[string]string{Order: "PO/26-27/", Receipt: "GR/26-27/", Claim: "EX/26-27/"}

// guard is DOC's from-state guard.
func (e *Env) guard(ctx context.Context, tx *db.Tx, d Doc, want doctype.State) (doctype.Info, error) {
	in, err := e.Docs.Info(ctx, tx, d.Type, d.ID)
	if err != nil {
		return in, err
	}
	if in.State != want {
		return in, fmt.Errorf("approvaltest: %s is %s, not %s", in.Display, in.State, want)
	}
	return in, nil
}

func setState(ctx context.Context, tx *db.Tx, d Doc, s doctype.State) error {
	_, err := tx.SQL().ExecContext(ctx, `UPDATE aprtest_docs SET state = ? WHERE doctype = ? AND id = ?`, string(s), d.Type, d.ID)
	return err
}

func (e *Env) info(t testing.TB, d Doc) doctype.Info {
	t.Helper()
	var in doctype.Info
	MustOK(t, e.DB.Read(context.Background(), func(tx *db.Tx) (err error) {
		in, err = e.Docs.Info(context.Background(), tx, d.Type, d.ID)
		return err
	}))
	return in
}

// State is the document's state.
func (e *Env) State(t testing.TB, d Doc) doctype.State { t.Helper(); return e.info(t, d).State }

// Number is the document's number ("" when none).
func (e *Env) Number(t testing.TB, d Doc) string { t.Helper(); return e.info(t, d).Number }

// Title is Info.Display: the number, else "Draft {id}".
func (e *Env) Title(t testing.TB, d Doc) string { t.Helper(); return e.info(t, d).Display }

// Effects is how many times the document's effects were applied.
func (e *Env) Effects(t testing.TB, d Doc) int {
	t.Helper()
	var n int
	MustOK(t, e.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(),
			`SELECT COALESCE((SELECT n FROM aprtest_effects WHERE doctype = ? AND id = ?), 0)`, d.Type, d.ID).Scan(&n)
	}))
	return n
}

// Latest is the document's latest approval request.
func (e *Env) Latest(t testing.TB, d Doc) (approval.Request, bool) {
	t.Helper()
	var r approval.Request
	var ok bool
	MustOK(t, e.DB.Read(context.Background(), func(tx *db.Tx) (err error) {
		r, ok, err = e.A.LatestRequest(context.Background(), tx, d.Type, d.ID)
		return err
	}))
	return r, ok
}

// History is the document's approval history, oldest first.
func (e *Env) History(t testing.TB, d Doc) []approval.HistoryRow {
	t.Helper()
	var h []approval.HistoryRow
	MustOK(t, e.DB.Read(context.Background(), func(tx *db.Tx) (err error) {
		h, err = e.A.History(context.Background(), tx, d.Type, d.ID)
		return err
	}))
	return h
}

// InboxTitles are the titles in p's inbox, in inbox order.
func (e *Env) InboxTitles(t testing.TB, p auth.Principal) []string {
	t.Helper()
	res, err := e.A.Inbox(context.Background(), p, types.Page{Number: 1, Size: 100})
	MustOK(t, err)
	var titles []string
	for _, it := range res.Rows {
		titles = append(titles, it.Title)
	}
	return titles
}
