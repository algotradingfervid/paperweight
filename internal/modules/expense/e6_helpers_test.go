package expense_test

import (
	"bytes"
	"context"
	"database/sql"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Helpers for the E6 tests (period lock and warnings). Names carry the e6
// prefix so the parallel MOD-EXP lanes' test files in this package can't
// clash with them.

// e6ClaimWarner is the plan's E6 read (MOD-EXP E6 Do 2): every spec §5 claim
// warning for p on claimID, read through p's checks outside any write. It is
// asserted at run time, so the tests compile before E6 builds it and fail with
// this message instead.
type e6ClaimWarner interface {
	ClaimWarnings(ctx context.Context, p auth.Principal, claimID int64) ([]string, error)
}

// e6Warnings is (*expense.Service).ClaimWarnings(ctx, p, claimID).
func e6Warnings(t *testing.T, e *expensetest.Env, p auth.Principal, claimID int64) []string {
	t.Helper()
	w, ok := any(e.Service).(e6ClaimWarner)
	if !ok {
		t.Fatalf("*expense.Service has no ClaimWarnings(ctx, p auth.Principal, claimID int64) ([]string, error) (MOD-EXP E6)")
	}
	got, err := w.ClaimWarnings(t.Context(), p, claimID)
	if err != nil {
		t.Fatalf("ClaimWarnings(%s, claim %d) error = %v, want nil", p.Name, claimID, err)
	}
	return got
}

// e6WantWarnings fails unless got holds exactly want, in any order.
func e6WantWarnings(t *testing.T, who string, got []string, want ...string) {
	t.Helper()
	g, w := slices.Clone(got), slices.Clone(want)
	slices.Sort(g)
	slices.Sort(w)
	if !slices.Equal(g, w) {
		t.Errorf("ClaimWarnings as %s =\n  %q\nwant\n  %q", who, g, w)
	}
}

// e6Do runs action a as p at the document's stored version and fails the test
// on an error.
func e6Do(t *testing.T, e *expensetest.Env, p auth.Principal, dt string, id int64, a doc.Action, reason string) doc.Result {
	t.Helper()
	res, err := e6Try(t, e, p, dt, id, a, reason)
	if err != nil {
		t.Fatalf("%s %s %d as %s: %v", a, dt, id, p.Name, err)
	}
	return res
}

// e6Try runs action a as p at the document's stored version.
func e6Try(t *testing.T, e *expensetest.Env, p auth.Principal, dt string, id int64, a doc.Action, reason string) (doc.Result, error) {
	t.Helper()
	return e.Engine.Do(t.Context(), p, dt, id, a, doc.ActionInput{Version: e6Load(t, e, dt, id).Version, Reason: reason})
}

// e6Load is the stored record.
func e6Load(t *testing.T, e *expensetest.Env, dt string, id int64) doc.Record {
	t.Helper()
	var rec doc.Record
	err := e.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		rec, err = e.Engine.Load(t.Context(), tx, dt, id)
		return err
	})
	if err != nil {
		t.Fatalf("load %s %d: %v", dt, id, err)
	}
	return rec
}

// e6Approve submits draft id as p and approves it as approver; it returns the
// approved document's number.
func e6Approve(t *testing.T, e *expensetest.Env, p, approver auth.Principal, dt string, id int64) string {
	t.Helper()
	if res := e6Do(t, e, p, dt, id, doc.ActionSubmit, ""); res.State != doc.StatePending {
		t.Fatalf("submit %s %d as %s: state %q, want pending", dt, id, p.Name, res.State)
	}
	res := e6Do(t, e, approver, dt, id, doc.ActionApprove, "")
	if res.State != doc.StateApproved || res.Number == "" {
		t.Fatalf("approve %s %d as %s: state %q, number %q, want approved and numbered", dt, id, approver.Name, res.State, res.Number)
	}
	return res.Number
}

// e6Attach attaches content to claim claimID as p, through the real
// files.Store, and returns its SHA-256 (what a line's receipt_sha256 holds).
func e6Attach(t *testing.T, e *expensetest.Env, p auth.Principal, claimID int64, name string, content []byte) string {
	t.Helper()
	info := e6Load(t, e, expense.ClaimDocType, claimID).Info
	parent := auth.RecordRef{DocType: info.DocType, ID: info.ID, CreatedBy: info.CreatedBy, EmployeeID: info.EmployeeID,
		ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs}
	sp, err := e.Files.Spool(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("spool %s: %v", name, err)
	}
	err = e.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := e.Files.Attach(t.Context(), tx, p, parent, name, sp)
		return err
	})
	if err != nil {
		t.Fatalf("attach %s to claim %d as %s: %v", name, claimID, p.Name, err)
	}
	return sp.SHA256()
}

// e6SetReceipts saves draft claim claimID as p at its stored version, every
// stored line sent in full, with line n's (1-based) receipt_sha256 set to
// receipts[n] and its no-receipt reason blanked.
func e6SetReceipts(t *testing.T, e *expensetest.Env, p auth.Principal, claimID int64, receipts map[int]string) {
	t.Helper()
	rec := e6Load(t, e, expense.ClaimDocType, claimID)
	in := doc.ActionInput{Version: rec.Version, Values: map[string]string{}}
	for i, l := range rec.Lines {
		l = maps.Clone(l)
		if h, ok := receipts[i+1]; ok {
			l["receipt_sha256"] = h
			l["no_receipt_reason"] = ""
		}
		in.Lines = append(in.Lines, l)
	}
	if _, err := e.Engine.Do(t.Context(), p, expense.ClaimDocType, claimID, doc.ActionSave, in); err != nil {
		t.Fatalf("save the receipts of claim %d as %s: %v", claimID, p.Name, err)
	}
	after := e6Load(t, e, expense.ClaimDocType, claimID)
	for n, h := range receipts {
		if got := after.Lines[n-1]["receipt_sha256"]; got != h {
			t.Fatalf("claim %d line %d receipt_sha256 = %q after the save, want %q", claimID, n, got, h)
		}
	}
}

// e6SetLock sets the period lock (PO spec §14.2) to iso as the Administrator,
// through modkit's settings service.
func e6SetLock(t *testing.T, e *expensetest.Env, iso string) {
	t.Helper()
	d, err := types.DateFromISO(iso)
	if err != nil {
		t.Fatalf("lock date %q: %v", iso, err)
	}
	kit := modkit.New(modkit.Deps{DB: e.DB, Auth: e.Auth, Audit: audit.Default, Clock: e.Clock})
	admin := e.P(t, "admin@vardhan.test")
	s, err := kit.GetSettings(t.Context(), admin)
	if err != nil {
		t.Fatalf("GetSettings as admin: %v", err)
	}
	in := modkit.SettingsInput{LockedUpTo: sql.Null[types.Date]{V: d, Valid: true}, DefaultPOTerms: s.DefaultPOTerms}
	if err := kit.SaveSettings(t.Context(), admin, s.Version, in); err != nil {
		t.Fatalf("SaveSettings(lock %s) as admin: %v", iso, err)
	}
}

// e6Employee is the id of the fixture employee with code, as a form value.
func e6Employee(t *testing.T, e *expensetest.Env, code string) string {
	t.Helper()
	for _, emp := range e.F.Employees {
		if emp.Code == code {
			return strconv.FormatInt(emp.ID, 10)
		}
	}
	t.Fatalf("no fixture employee %s", code)
	return ""
}

// e6Branch is the id of the fixture branch with code, as a form value.
func e6Branch(t *testing.T, e *expensetest.Env, code string) string {
	t.Helper()
	for _, b := range e.F.Branches {
		if b.Code == code {
			return strconv.FormatInt(b.ID, 10)
		}
	}
	t.Fatalf("no fixture branch %s", code)
	return ""
}

// e6Type is the id of the fixture expense type with code, as a form value.
func e6Type(t *testing.T, e *expensetest.Env, code string) string {
	t.Helper()
	id, ok := e.F.ExpenseTypeIDs[code]
	if !ok {
		t.Fatalf("no fixture expense type %s", code)
	}
	return strconv.FormatInt(id, 10)
}

// e6Joined is every warning in one text, for the hidden-record check
// (research T7): a number the viewer can't read appears nowhere in it.
func e6Joined(ws []string) string { return strings.Join(ws, "\n") }
