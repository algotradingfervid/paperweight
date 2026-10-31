package expense_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// E11 · Document link targets are read through auth (CD-312). Every read
// check on an advance or claim builds the record as AUTH's Info does (owner,
// employee, projects, sites), so a user restricted to a project still reads
// the documents her scope gives her. On the real auth.Service.

// e11Project is the project the restricted users are limited to; no
// document in these cases names it.
const e11Project = "PRJ-TECHPARK"

// e11WantGone fails unless err is doc.Gone(): hidden looks the same as
// missing (CD-245).
func e11WantGone(t *testing.T, what string, err error) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) || de.Kind != doc.KindNotFound || de.Message != doc.Gone().Message {
		t.Errorf("%s: error = %v, want doc.Gone()", what, err)
	}
}

// e11Restrict limits the user with email to e11Project and checks that the
// restriction is live (the user no longer reads the PRJ-METRO project) and
// that the user reads document dt id as AUTH's Info describes it.
func e11Restrict(t *testing.T, e *expensetest.Env, email, dt string, id int64) {
	t.Helper()
	e.S4Restrict(t, email, e11Project)
	p := e.P(t, email)
	metro := e5Project(t, e, "PRJ-METRO")
	err := e.Auth.Can(t.Context(), p, project.DocType, auth.RightRead,
		&auth.RecordRef{DocType: project.DocType, ID: metro, ProjectIDs: []int64{metro}})
	if !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("setup: %s reads project PRJ-METRO: %v, want not found (restricted)", email, err)
	}
	if err := e.Auth.Can(t.Context(), p, dt, auth.RightRead, e5Ref(t, e, dt, id)); err != nil {
		t.Fatalf("setup: %s reads %s %d: %v, want nil", email, dt, id, err)
	}
}

// Priya, restricted to a project, still reads her report Deepak's advance:
// AdvanceChoices lists it by number, LeftOn returns its left and
// AdvanceWarnings names it. Kavya, restricted too and outside Deepak's
// team, gets none of it.
func TestE11_RestrictedReadsAdvance(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya, kavya := e.P(t, e4Deepak), e.P(t, e4Priya), e.P(t, e4Kavya)
	emp := e4Employee(t, e, "EMP-007")
	paid := e.PaidAdvance(t, deepak, e4Rs(10000)) // EA/26-27/00001
	if n := e4Number(t, e, expense.AdvanceDocType, paid.ID); n != e4EA1 {
		t.Fatalf("the paid advance is %q, want %s", n, e4EA1)
	}
	b := e.DraftAdvance(t, deepak, nil)
	e11Restrict(t, e, e4Priya, expense.AdvanceDocType, paid.ID)
	e.S4Restrict(t, e4Kavya, e11Project)

	ids, printed := e4Choices(t, e, priya, emp)
	if !slices.Equal(ids, []int64{paid.ID}) {
		t.Errorf("AdvanceChoices(priya) ids = %v, want [%d]", ids, paid.ID)
	}
	e7Contains(t, printed, e4EA1)
	if left := e4LeftOn(t, e, priya, paid.ID); left != e4Rs(10000) {
		t.Errorf("LeftOn(priya) = %d, want %d", left, e4Rs(10000))
	}
	e4WantWarnings(t, "priya, restricted", e4Warnings(t, e, priya, b.ID),
		fmt.Sprintf(e4FmtWarning, "Deepak Nair", "10,000.00", e4EA1))

	ids, printed = e4Choices(t, e, kavya, emp)
	if len(ids) != 0 {
		t.Errorf("AdvanceChoices(kavya) ids = %v, want none", ids)
	}
	expensetest.WantNotNamed(t, printed, e4EA1)
	_, err := e.Service.LeftOn(t.Context(), kavya, paid.ID)
	e11WantGone(t, "LeftOn(kavya)", err)
	ws, err := e.Service.AdvanceWarnings(t.Context(), kavya, b.ID)
	e11WantGone(t, "AdvanceWarnings(kavya)", err)
	expensetest.WantNotNamed(t, e6Joined(ws), e4EA1)
}

// Priya, restricted to a project, reads Deepak's claim's warnings, and they
// name his other claim. Kavya, restricted too, gets doc.Gone.
func TestE11_RestrictedReadsClaim(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya, kavya := e.P(t, e4Deepak), e.P(t, e4Priya), e.P(t, e4Kavya)
	line := expensetest.Values{"expense_date": "2026-10-20", "amount": "1250.00"}
	old := e.ApprovedClaim(t, deepak, nil, line)
	if n := e4Number(t, e, expense.ClaimDocType, old.ID); n != e4EX1 {
		t.Fatalf("the old claim is %q, want %s", n, e4EX1)
	}
	claim := e.DraftClaim(t, deepak, nil, line)
	e11Restrict(t, e, e4Priya, expense.ClaimDocType, claim.ID)
	e.S4Restrict(t, e4Kavya, e11Project)

	e6WantWarnings(t, "priya, restricted", e6Warnings(t, e, priya, claim.ID),
		"Line 1: This looks the same as a line on "+e4EX1+".")

	ws, err := e.Service.ClaimWarnings(t.Context(), kavya, claim.ID)
	e11WantGone(t, "ClaimWarnings(kavya)", err)
	expensetest.WantNotNamed(t, e6Joined(ws), e4EX1)
}

// Deepak, restricted to a project his documents don't name, prints his claim
// with his advance's number. Suresh, who prints every claim but can't read
// the advance, restricted too, sees no advance number.
func TestE11_RestrictedPrintShowsAdvance(t *testing.T) {
	e, claimID, advanceID := e7World(t)
	e11Restrict(t, e, e7Deepak, expense.AdvanceDocType, advanceID)
	e7Contains(t, e7Print(t, e, e.P(t, e7Deepak), expense.ClaimDocType, claimID), "Advance: "+e7AdvanceNo)

	admin := e.P(t, e7Admin)
	if err := e.Auth.CreateRole(t.Context(), admin, "Claim Auditor"); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := e.Auth.SetRights(t.Context(), admin, "Claim Auditor", expense.ClaimDocType,
		map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightPrint: auth.ScopeAll}); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	e.AddRoles(t, e7Suresh, "Claim Auditor")
	e.S4Restrict(t, e7Suresh, e11Project)
	expensetest.WantNotNamed(t, e7Print(t, e, e.P(t, e7Suresh), expense.ClaimDocType, claimID), e7AdvanceNo)
}
