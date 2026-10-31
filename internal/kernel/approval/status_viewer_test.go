package approval_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// m19StatusFor is the document's approval status for viewer, read in one
// db.Read, with Status's own line read in the same transaction.
func m19StatusFor(t *testing.T, e *at.Env, viewer auth.Principal, d at.Doc) (got, plain approval.Status) {
	t.Helper()
	at.MustOK(t, e.DB.Read(t.Context(), func(tx *db.Tx) (err error) {
		if got, err = e.A.StatusFor(t.Context(), tx, viewer, d.Type, d.ID); err != nil {
			return err
		}
		plain, err = e.A.Status(t.Context(), tx, d.Type, d.ID)
		return err
	}))
	return got, plain
}

// m19Want checks the viewer's Roles and Why, and that StatusFor keeps
// Status's Line, Pending and WaitingOn, so the adapter can use it alone.
func m19Want(t *testing.T, label string, e *at.Env, viewer auth.Principal, d at.Doc, roles, why string) {
	t.Helper()
	got, plain := m19StatusFor(t, e, viewer, d)
	if got.Roles != roles || got.Why != why {
		t.Errorf("%s: StatusFor(%s) Roles, Why = %q, %q; want %q, %q", label, viewer.Name, got.Roles, got.Why, roles, why)
	}
	if got.Line != plain.Line || got.Pending != plain.Pending || got.WaitingOn != plain.WaitingOn {
		t.Errorf("%s: StatusFor(%s) = {Line:%q Pending:%v WaitingOn:%q}, want Status's {Line:%q Pending:%v WaitingOn:%q}",
			label, viewer.Name, got.Line, got.Pending, got.WaitingOn, plain.Line, plain.Pending, plain.WaitingOn)
	}
}

// HOOK-031 (CD-263 G9(a), CD-266). Roles names why the viewer is eligible:
// the request's roles the viewer holds now, joined, or "{employee}'s
// reporting manager", which wins when both apply. Why is WhyText of the rule
// the request was opened under. Both are empty unless the document is pending.
func TestApprovalStatus_RolesAndWhy(t *testing.T) {
	t.Run("role rule lists only the rule's roles the viewer holds", func(t *testing.T) {
		e := at.New(t)
		d := e.NewDoc(t, at.Order, 42, at.Rs(60_000), e.Priya) // R1: Purchase Manager, then R2's Director
		m19Want(t, "draft", e, e.Ravi, d, "", "")
		at.MustOK(t, e.Submit(e.Priya, d))

		m19Want(t, "Ravi (Purchase Manager)", e, e.Ravi, d, "Purchase Manager", "₹50,000 or more")
		m19Want(t, "Meena (Director)", e, e.Meena, d, "Director", "₹50,000 or more")
		m19Want(t, "Priya (no approver role)", e, e.Priya, d, "", "₹50,000 or more")
		m19Want(t, "Kavya (Masters Manager)", e, e.Kavya, d, "", "₹50,000 or more")

		// Roles are read now, not from the Principal (CD-86).
		e.Auth.AddRole(e.Ravi.ID, "Director")
		m19Want(t, "Ravi given Director", e, e.Ravi, d, "Purchase Manager or Director", "₹50,000 or more")
		e.RemoveRole(t, e.Ravi.ID, "Purchase Manager")
		e.RemoveRole(t, e.Ravi.ID, "Director")
		m19Want(t, "Ravi without his roles", e, e.Ravi, d, "", "₹50,000 or more")
	})

	t.Run("a higher rule's request lists only its own roles", func(t *testing.T) {
		e := at.New(t)
		d := e.NewDoc(t, at.Order, 43, at.Rs(6_00_000), e.Priya) // R2: Director only
		at.MustOK(t, e.Submit(e.Priya, d))
		m19Want(t, "Meena (Director)", e, e.Meena, d, "Director", "₹5,00,000 or more")
		m19Want(t, "Ravi (Purchase Manager, not on R2)", e, e.Ravi, d, "", "₹5,00,000 or more")
	})

	t.Run("reporting-manager rule", func(t *testing.T) {
		e := at.New(t)
		d := e.NewDoc(t, at.Claim, 10, at.Rs(3_450), e.Deepak) // E1 from ₹0: Priya, fallback Director
		at.MustOK(t, e.Submit(e.Deepak, d))

		m19Want(t, "Priya (Deepak's manager)", e, e.Priya, d, "Deepak Nair's reporting manager", "Any amount")
		m19Want(t, "Meena (fallback Director)", e, e.Meena, d, "Director", "Any amount")
		m19Want(t, "Ravi (neither)", e, e.Ravi, d, "", "Any amount")

		e.Auth.AddRole(e.Priya.ID, "Director")
		m19Want(t, "Priya, manager and Director", e, e.Priya, d, "Deepak Nair's reporting manager", "Any amount")
	})

	t.Run("empty unless pending", func(t *testing.T) {
		e := at.New(t)
		approved := e.NewDoc(t, at.Order, 44, at.Rs(60_000), e.Priya)
		at.MustOK(t, e.Submit(e.Priya, approved))
		at.MustOK(t, e.Approve(e.Ravi, approved))
		m19Want(t, "approved order", e, e.Ravi, approved, "", "")
		m19Want(t, "approved order", e, e.Meena, approved, "", "")

		rejected := e.NewDoc(t, at.Claim, 11, at.Rs(3_450), e.Deepak)
		at.MustOK(t, e.Submit(e.Deepak, rejected))
		at.MustOK(t, e.Reject(e.Priya, rejected, "Missing the bill"))
		m19Want(t, "rejected claim", e, e.Priya, rejected, "", "")
		m19Want(t, "rejected claim", e, e.Meena, rejected, "", "")

		withdrawn := e.NewDoc(t, at.Claim, 12, at.Rs(3_450), e.Deepak)
		at.MustOK(t, e.Submit(e.Deepak, withdrawn))
		at.MustOK(t, e.Withdraw(e.Deepak, withdrawn))
		m19Want(t, "withdrawn claim", e, e.Priya, withdrawn, "", "")
	})
}
