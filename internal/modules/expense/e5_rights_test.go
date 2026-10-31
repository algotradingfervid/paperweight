package expense_test

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// e5WantNotFound fails unless err is AUTH's 404 with its text (CD-245).
func e5WantNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, auth.ErrNotFound) || err.Error() != auth.MsgNotFound {
		t.Errorf("%s: err = %v, want 404 %q", what, err, auth.MsgNotFound)
	}
}

// e5Export runs the generic CSV export of dt as p (spec §13: the generic list
// and CSV cover both registers).
func e5Export(t *testing.T, e *expensetest.Env, p auth.Principal, dt string) (int, string, error) {
	t.Helper()
	var buf bytes.Buffer
	n, err := csvio.NewExporter(e.DB, e.Auth).Export(context.Background(), &buf, p, lookupDT(t, dt), list.Query{})
	return n, buf.String(), err
}

// EXP-017: Priya, restricted to PRJ-METRO, lists, opens and approves
// Deepak's claim with no project, and one with a PRJ-TECHPARK line: claims
// declare no restriction fields (spec §3). On the real auth.Service.
func TestExp_RestrictedManagerReal(t *testing.T) {
	e := expensetest.New(t)
	ctx := context.Background()
	admin, deepak := e.P(t, e5Admin), e.P(t, e5Deepak)
	metro, techpark := e5Project(t, e, "PRJ-METRO"), e5Project(t, e, "PRJ-TECHPARK")
	if err := e.Auth.ChangeRestrictions(ctx, admin, e5UID(t, e5Priya), auth.DimProject, []int64{metro}, nil, nil); err != nil {
		t.Fatalf("restrict priya to PRJ-METRO: %v", err)
	}
	priya := e.P(t, e5Priya)
	if err := e.DB.Read(ctx, func(tx *db.Tx) error {
		ok, err := e.Auth.ValueAllowedTx(ctx, tx, priya, string(auth.DimProject), techpark)
		if err == nil && ok {
			t.Errorf("priya may use PRJ-TECHPARK; the restriction isn't in force")
		}
		return err
	}); err != nil {
		t.Fatalf("ValueAllowedTx: %v", err)
	}

	plain := e5MustSubmit(t, e, deepak, expense.ClaimDocType, e.DraftClaim(t, deepak, nil))
	tagged := e5MustSubmit(t, e, deepak, expense.ClaimDocType,
		e.DraftClaim(t, deepak, nil, expensetest.Values{"project_id": strconv.FormatInt(techpark, 10)}))

	e5WantList(t, e, priya, expense.ClaimDocType, plain.ID, tagged.ID)
	for _, c := range []struct {
		name string
		res  doc.Result
	}{{"no project", plain}, {"a PRJ-TECHPARK line", tagged}} {
		t.Run(c.name, func(t *testing.T) {
			if err := e5Open(t, e, priya, expense.ClaimDocType, c.res.ID); err != nil {
				t.Errorf("priya opens the claim: %v, want allowed", err)
			}
			e5WantEligible(t, e, expense.ClaimDocType, c.res.ID, e5Priya)
			res := e5Must(t, e, priya, expense.ClaimDocType, c.res.ID, doc.ActionApprove, doc.ActionInput{Version: c.res.Version})
			if res.State != doc.StateApproved {
				t.Errorf("priya approves: state %s, want approved", res.State)
			}
		})
	}
}

// EXP-018: own and team scope on the list; another team's claim is a 404;
// the Director lists every claim and exports the CSV; an Employee asking for
// the CSV gets AUTH's 403 for export. On the real auth.Service.
func TestExp_TeamScopeReal(t *testing.T) {
	e := expensetest.New(t)
	deepak, kavya, priya, meena := e.P(t, e5Deepak), e.P(t, e5Kavya), e.P(t, e5Priya), e.P(t, e5Meena)

	his := e.DraftClaim(t, deepak, nil)
	theirs := e.DraftClaim(t, kavya, nil)
	hers := e.DraftClaim(t, priya, nil)

	e5WantList(t, e, deepak, expense.ClaimDocType, his.ID)
	e5WantList(t, e, priya, expense.ClaimDocType, his.ID, hers.ID)
	e5WantList(t, e, kavya, expense.ClaimDocType, theirs.ID)
	e5WantList(t, e, meena, expense.ClaimDocType, his.ID, theirs.ID, hers.ID)

	if err := e5Open(t, e, deepak, expense.ClaimDocType, his.ID); err != nil {
		t.Errorf("deepak opens his claim: %v", err)
	}
	if err := e5Open(t, e, priya, expense.ClaimDocType, his.ID); err != nil {
		t.Errorf("priya opens her report's claim: %v", err)
	}
	e5WantNotFound(t, "kavya opens Deepak's claim", e5Open(t, e, kavya, expense.ClaimDocType, his.ID))
	e5WantNotFound(t, "deepak opens Kavya's claim", e5Open(t, e, deepak, expense.ClaimDocType, theirs.ID))

	t.Run("the Director exports the CSV", func(t *testing.T) {
		n, csv, err := e5Export(t, e, meena, expense.ClaimDocType)
		if err != nil || n != 3 {
			t.Errorf("the Director's export = %d rows, %v; want 3 rows", n, err)
		}
		if csv == "" {
			t.Error("the Director's export wrote nothing")
		}
	})
	t.Run("an Employee's export is AUTH's 403", func(t *testing.T) {
		n, csv, err := e5Export(t, e, deepak, expense.ClaimDocType)
		if err == nil || err.Error() != e5ExportDenied || !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("deepak's export err = %v, want 403 %q", err, e5ExportDenied)
		}
		if n != 0 || csv != "" {
			t.Errorf("deepak's refused export wrote %d rows (%d bytes), want nothing", n, len(csv))
		}
	})
}

// EXP-031: Employee Advance rights are spec §11's: each right is exercised
// once for Employee, Director and Fallback Approver; the employee can't
// close, and Kavya without Fallback Approver gets a 404 on Deepak's advance.
// On the real auth.Service.
func TestAdv_Rights(t *testing.T) {
	e := expensetest.New(t)
	ctx := context.Background()
	const dt = expense.AdvanceDocType
	deepak, priya, meena := e.P(t, e5Deepak), e.P(t, e5Priya), e.P(t, e5Meena)

	// Employee: create, write, submit (own); Employee read and approve (team).
	a1 := e.DraftAdvance(t, deepak, nil)
	rec := e5Load(t, e, dt, a1.ID)
	in := doc.ActionInput{Version: rec.Version, Values: expensetest.Values{"purpose": "Pune Metro site visit, week 2"}, Lines: rec.Lines}
	a1 = e5Must(t, e, deepak, dt, a1.ID, doc.ActionSave, in)
	if got := e5Load(t, e, dt, a1.ID).Values["purpose"]; got != "Pune Metro site visit, week 2" {
		t.Errorf("purpose after deepak's save = %q", got)
	}
	a1 = e5MustSubmit(t, e, deepak, dt, a1)
	if err := e5Open(t, e, priya, dt, a1.ID); err != nil {
		t.Errorf("priya reads her report's advance: %v", err)
	}
	a1 = e5Must(t, e, priya, dt, a1.ID, doc.ActionApprove, doc.ActionInput{Version: a1.Version})
	e5WantState(t, e, dt, a1.ID, doctype.StateApproved, "EA/26-27/00001")

	// Employee print (own).
	if err := e.Auth.Can(ctx, deepak, dt, auth.RightPrint, e5Ref(t, e, dt, a1.ID)); err != nil {
		t.Errorf("deepak prints his advance: %v", err)
	}

	// The employee can't close: AUTH's 403 for cancel, nothing written.
	e.WantRefused(t, e5CloseDeniedAdv, func() error {
		_, err := e5Do(e, deepak, dt, a1.ID, doc.ActionClose, doc.ActionInput{Version: a1.Version, Reason: "Nothing more to claim"})
		return err
	})

	// Kavya, without Fallback Approver, doesn't see Deepak's advance.
	kavya := e.P(t, e5Kavya)
	e5WantNotFound(t, "kavya opens Deepak's advance", e5Open(t, e, kavya, dt, a1.ID))
	if ids := e5ListIDs(t, e, kavya, dt); len(ids) != 0 {
		t.Errorf("kavya's advance list = %v, want none", ids)
	}

	// Director: read, print, export, close (through cancel).
	if err := e5Open(t, e, meena, dt, a1.ID); err != nil {
		t.Errorf("the Director reads Deepak's advance: %v", err)
	}
	if err := e.Auth.Can(ctx, meena, dt, auth.RightPrint, e5Ref(t, e, dt, a1.ID)); err != nil {
		t.Errorf("the Director prints Deepak's advance: %v", err)
	}
	if n, _, err := e5Export(t, e, meena, dt); err != nil || n != 1 {
		t.Errorf("the Director's advance export = %d rows, %v; want 1 row", n, err)
	}
	const why = "₹5,000 returned by NEFT 1234 on 30-11-2026"
	a1 = e5Must(t, e, meena, dt, a1.ID, doc.ActionClose, doc.ActionInput{Version: a1.Version, Reason: why})
	if state, reason := e5StateReason(t, e, "employee_advance", a1.ID); state != string(doctype.StateClosed) || reason != why {
		t.Errorf("stored state, reason = %q, %q; want closed, %q", state, reason, why)
	}

	// Director cancel; Employee amend and delete (own).
	a2 := e.ApprovedAdvance(t, deepak, types.Money(300000))
	a2 = e5Must(t, e, meena, dt, a2.ID, doc.ActionCancel, doc.ActionInput{Version: a2.Version, Reason: "Trip called off"})
	e5WantState(t, e, dt, a2.ID, doctype.StateCancelled, "EA/26-27/00002")
	am := e5Must(t, e, deepak, dt, a2.ID, doc.ActionAmend, doc.ActionInput{Version: a2.Version})
	if am.ID == a2.ID || am.State != doc.StateDraft {
		t.Errorf("deepak's amend = id %d %s, want a new draft", am.ID, am.State)
	}
	e5Must(t, e, deepak, dt, am.ID, doc.ActionDelete, doc.ActionInput{Version: am.Version})
	var left int
	if err := e.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM employee_advance WHERE id = ?`, am.ID).Scan(&left)
	}); err != nil || left != 0 {
		t.Errorf("deleted draft %d: %d rows left, %v", am.ID, left, err)
	}

	// Fallback Approver: read and approve (all).
	e.AddRoles(t, e5Kavya, expense.RoleFallbackApprover)
	kavya = e.P(t, e5Kavya)
	a3 := e5MustSubmit(t, e, deepak, dt, e.DraftAdvance(t, deepak, nil))
	if err := e5Open(t, e, kavya, dt, a3.ID); err != nil {
		t.Errorf("kavya with Fallback Approver reads Deepak's advance: %v", err)
	}
	e5WantEligible(t, e, dt, a3.ID, e5Priya, e5Kavya)
	a3 = e5Must(t, e, kavya, dt, a3.ID, doc.ActionApprove, doc.ActionInput{Version: a3.Version})
	e5WantState(t, e, dt, a3.ID, doctype.StateApproved, "EA/26-27/00003")

	// Director approve (all): Meena may approve outside her team; she approves
	// Kavya's advance as her manager.
	if err := e.Auth.Can(ctx, meena, dt, auth.RightApprove, e5Ref(t, e, dt, a3.ID)); err != nil {
		t.Errorf("the Director's approve right on Deepak's advance: %v", err)
	}
	ka := e5MustSubmit(t, e, kavya, dt, e.DraftAdvance(t, kavya, nil))
	ka = e5Must(t, e, meena, dt, ka.ID, doc.ActionApprove, doc.ActionInput{Version: ka.Version})
	e5WantState(t, e, dt, ka.ID, doctype.StateApproved, "EA/26-27/00004")
}
