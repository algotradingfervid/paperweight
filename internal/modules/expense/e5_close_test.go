package expense_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/testkit"
)

// e5FakeEngine is the real engine over e's database with authfake in place of
// auth.Service, the fake holding 00-fixtures.md's expense.Claim rows (the
// rows EXP-022 checks against the real service).
func e5FakeEngine(t *testing.T, e *expensetest.Env) doc.Engine {
	t.Helper()
	f := authfake.New()
	f.UseDirectory(testkit.Directory(e.F))
	for role, rights := range fixtureRights() {
		for r, s := range rights {
			f.Grant(role, expense.ClaimDocType, auth.Right(r), auth.Scope(s))
		}
	}
	return doc.NewEngine(doc.Deps{
		DB: e.DB, Clock: e.Clock, Auth: f, Naming: naming.New(e.DB, e.Clock, f, audit.Default),
		Approval: e.Approvals, Files: e.Files, Audit: audit.Default,
	})
}

// EXP-020: the claimant can't close their own approved claim: close needs
// `cancel` (CloseRight), which Employee doesn't hold, so AUTH's 403 for
// cancel and nothing changes. Asserted on the real auth.Service and on
// authfake; the Director closes through both.
func TestExp_CloseRight(t *testing.T) {
	e := expensetest.New(t)
	deepak, meena := e.P(t, e5Deepak), e.P(t, e5Meena)
	engines := []struct {
		name string
		eng  doc.Engine
	}{
		{"real auth.Service", e.Engine},
		{"authfake", e5FakeEngine(t, e)},
	}
	for _, x := range engines {
		t.Run(x.name, func(t *testing.T) {
			claim := e.ApprovedClaim(t, deepak, nil)
			e.WantRefused(t, e5CloseDeniedClaim, func() error {
				_, err := x.eng.Do(t.Context(), deepak, expense.ClaimDocType, claim.ID, doc.ActionClose,
					doc.ActionInput{Version: claim.Version, Reason: "Paid by NEFT 1234 on 25-10-2026"})
				return err
			})
			e5WantState(t, e, expense.ClaimDocType, claim.ID, doctype.StateApproved, claim.Number)

			res, err := x.eng.Do(t.Context(), meena, expense.ClaimDocType, claim.ID, doc.ActionClose,
				doc.ActionInput{Version: claim.Version, Reason: "Paid by NEFT 1234 on 25-10-2026"})
			if err != nil {
				t.Fatalf("the Director closes: %v", err)
			}
			if res.State != doc.StateClosed {
				t.Errorf("the Director's close: state %s, want closed", res.State)
			}
		})
	}
}

// EXP-021: the Director closes an approved claim with how it was paid; a
// closed claim can't be cancelled or amended (DOC's closed text, R6), and
// nothing changes.
func TestExp_ClosedIsFinal(t *testing.T) {
	e := expensetest.New(t)
	deepak, meena, admin := e.P(t, e5Deepak), e.P(t, e5Meena), e.P(t, e5Admin)

	claim := e.ApprovedClaim(t, deepak, nil)
	if claim.Number != "EX/26-27/00001" {
		t.Fatalf("approved claim number %q, want EX/26-27/00001", claim.Number)
	}
	const why = "Paid by NEFT 1234 on 25-10-2026"
	res := e5Must(t, e, meena, expense.ClaimDocType, claim.ID, doc.ActionClose, doc.ActionInput{Version: claim.Version, Reason: why})
	if state, reason := e5StateReason(t, e, "expense_claim", claim.ID); state != string(doctype.StateClosed) || reason != why {
		t.Errorf("stored state, reason = %q, %q; want closed, %q", state, reason, why)
	}

	const closed = "EX/26-27/00001 is closed and can't be changed."
	tries := []struct {
		who    string
		p      auth.Principal
		action doc.Action
	}{
		{"the Director cancels", meena, doc.ActionCancel},
		{"the Administrator cancels", admin, doc.ActionCancel},
		{"the claimant amends", deepak, doc.ActionAmend},
		{"the Administrator amends", admin, doc.ActionAmend},
	}
	for _, try := range tries {
		t.Run(try.who, func(t *testing.T) {
			e.WantRefused(t, closed, func() error {
				_, err := e5Do(e, try.p, expense.ClaimDocType, claim.ID, try.action,
					doc.ActionInput{Version: res.Version, Reason: "Entered twice by mistake"})
				return err
			})
			e5WantState(t, e, expense.ClaimDocType, claim.ID, doctype.StateClosed, "EX/26-27/00001")
		})
	}
}
