// Package expensetest is the Expense module's test environment: a seeded
// database with the real engine, auth.Service, approval service and
// files.Store, wired as app.New wires them, plus business-step builders and
// refusal helpers (plan MOD-EXP, Global constraints, Tests; CD-271). Later
// tasks add their helpers in expensetest/<task>.go.
package expensetest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// Env is one expense test world. Its fields are the real services over DB.
type Env struct {
	DB        *db.DB
	Clock     *clock.Fixed // 2026-10-24 10:30 IST
	Auth      *auth.Auth
	Files     *files.Store
	Approvals *approval.Approvals
	Engine    doc.Engine
	Masters   *allmasters.Services
	Service   *expense.Service
	Deps      expense.Deps
	F         testkit.Fixtures // the seeded fixtures, with database ids
}

// engineRef and directoryRef break the construction cycles as app.New does.
type engineRef struct{ doc.Engine }

type directoryRef struct{ auth.Directory }

// New returns a fresh world: the fixture seed, SeedDefaults, Meena with the
// Employee role too (spec §15 item 3), both approval rules (₹0, reporting
// manager, fallback role Fallback Approver; spec §4, §6) and expense wired
// for its database. No user holds Fallback Approver: tests give it with
// AddRoles.
func New(t testing.TB) *Env {
	t.Helper()
	ctx := context.Background()
	d := testkit.NewDB(t)
	fx := testkit.Seed(t, d)
	clk := testkit.Clock()
	eng, dir := &engineRef{}, &directoryRef{}
	a := auth.New(auth.Deps{DB: d, Clock: clk, Directory: dir, Audit: audit.Default, Refs: eng, Info: eng})
	namer := naming.New(d, clk, a, audit.Default)
	store, err := files.New(files.Config{
		DataDir: testkit.DataDir(t), DB: d, Auth: a, Info: eng, Clock: clk, Audit: audit.Default,
		Policies:     map[string]files.Policy{expense.ClaimDocType: expense.ClaimPolicy},
		RemoveGuards: map[string]files.RemoveGuard{expense.ClaimDocType: expense.RemoveGuard},
	})
	if err != nil {
		t.Fatalf("expensetest.New: files.New: %v", err)
	}
	apr := approval.New(approval.Deps{DB: d, Clock: clk, Auth: a, Info: eng, Directory: dir, Audit: audit.Default})
	eng.Engine = doc.NewEngine(doc.Deps{DB: d, Clock: clk, Auth: a, Naming: namer, Approval: apr, Files: store, Audit: audit.Default})
	ms := allmasters.New(mstkit.Env{DB: d, Auth: a, Engine: eng.Engine, Auditor: audit.Default, Clock: clk})
	dir.Directory = ms.Directory
	if err := a.SeedDefaults(ctx, auth.SeedOptions{LinkedMasterRead: true}); err != nil {
		t.Fatalf("expensetest.New: SeedDefaults: %v", err)
	}
	for _, dt := range []string{expense.ClaimDocType, expense.AdvanceDocType} {
		typ, _ := doctype.Lookup(dt)
		if err := namer.EnsureSeries(ctx, typ.Name, typ.Label, typ.Series); err != nil {
			t.Fatalf("expensetest.New: EnsureSeries(%s): %v", dt, err)
		}
	}

	deps := expense.Deps{Clock: clk, Auth: a, Directory: ms.Directory, Files: store, ExpenseTypes: ms.ExpenseType}
	expense.Wire(d, deps)
	e := &Env{
		DB: d, Clock: clk, Auth: a, Files: store, Approvals: apr, Engine: eng.Engine, Masters: ms,
		Service: expense.New(d, deps), Deps: deps, F: fx,
	}
	e.AddRoles(t, "meena@vardhan.test", auth.RoleEmployee)
	admin := e.P(t, "admin@vardhan.test")
	for _, dt := range []string{expense.ClaimDocType, expense.AdvanceDocType} {
		if _, err := apr.CreateRule(ctx, admin, approval.RuleInput{
			DocType: dt, ApproverType: approval.ApproverManager, FallbackRole: expense.RoleFallbackApprover,
		}); err != nil {
			t.Fatalf("expensetest.New: approval rule for %s: %v", dt, err)
		}
	}
	return e
}

// P is the fixture user with email as AUTH loads it now, so roles a test
// added are included.
func (e *Env) P(t testing.TB, email string) auth.Principal {
	t.Helper()
	p, _, err := e.Auth.LoadPrincipal(context.Background(), testkit.Principal(t, email).ID)
	if err != nil {
		t.Fatalf("expensetest: load %s: %v", email, err)
	}
	return p
}

// AddRoles gives the fixture user with email the named roles, which must
// exist.
func (e *Env) AddRoles(t testing.TB, email string, roles ...string) {
	t.Helper()
	ctx := context.Background()
	uid := testkit.Principal(t, email).ID
	err := e.DB.Write(ctx, func(tx *db.Tx) error {
		for _, role := range roles {
			var id int64
			if err := tx.SQL().QueryRowContext(ctx, `SELECT id FROM roles WHERE name_key = lower(?)`, role).Scan(&id); err != nil {
				return fmt.Errorf("role %s: %w", role, err)
			}
			if _, err := tx.SQL().ExecContext(ctx,
				`INSERT OR IGNORE INTO user_roles (user_id, role_id) VALUES (?, ?)`, int64(uid), id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expensetest: give %s %v: %v", email, roles, err)
	}
}

// WantRefused runs do, which must fail with text (the error's message or one
// of its field messages) and write nothing: every claim's and advance's
// state, version, less advance, used amount and paid on, the line counts, the
// audit log and the attachments are as before (research T6).
func (e *Env) WantRefused(t testing.TB, text string, do func() error) {
	t.Helper()
	before := e.fingerprint(t)
	err := do()
	switch {
	case err == nil:
		t.Errorf("err = nil, want refused with %q", text)
	case !hasMessage(err, text):
		t.Errorf("err = %q (%T), want refused with %q", err, err, text)
	}
	if after := e.fingerprint(t); after != before {
		t.Errorf("the refusal wrote:\nbefore %s\nafter  %s", before, after)
	}
}

// hasMessage reports whether err's message, or one of its field messages,
// is text.
func hasMessage(err error, text string) bool {
	if err.Error() == text {
		return true
	}
	var he doctype.HTTPError
	return errors.As(err, &he) && slices.ContainsFunc(he.FieldErrors(), func(f doctype.FieldError) bool { return f.Message == text })
}

// fingerprint is what WantRefused compares.
func (e *Env) fingerprint(t testing.TB) string {
	t.Helper()
	var fp string
	err := e.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(), `SELECT
			(SELECT COUNT(*) || ' claims ' || COALESCE(group_concat(r, ','), '') FROM
				(SELECT id || '/' || state || '/' || version || '/' || COALESCE(less_advance, '-') AS r FROM expense_claim ORDER BY id))
			|| '; ' || (SELECT COUNT(*) FROM expense_claim_line) || ' claim lines; '
			|| (SELECT COUNT(*) || ' advances ' || COALESCE(group_concat(r, ','), '') FROM
				(SELECT id || '/' || state || '/' || version || '/' || used_amount || '/' || COALESCE(paid_on, '-') AS r FROM employee_advance ORDER BY id))
			|| '; ' || (SELECT COUNT(*) FROM employee_advance_line) || ' advance lines; '
			|| (SELECT COUNT(*) FROM audit_log) || ' audit rows; '
			|| (SELECT COUNT(*) FROM attachment) || ' attachments'`).Scan(&fp)
	})
	if err != nil {
		t.Fatalf("expensetest: read the state: %v", err)
	}
	return fp
}

// WantNotNamed fails when text names any of hidden: a record the viewer can't
// read is never named (CD-240, CD-248; research T7).
func WantNotNamed(t testing.TB, text string, hidden ...string) {
	t.Helper()
	for _, h := range hidden {
		if h != "" && strings.Contains(text, h) {
			t.Errorf("%q names %q, which the viewer can't read", text, h)
		}
	}
}
