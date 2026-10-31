package approval_test

import (
	"testing"

	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
)

// HOOK-031, CD-290, CD-292 (5): the reporting-manager form names the employee
// whose manager the rule matched (the claimant), not who submitted the claim,
// and reads "Reporting manager" when that employee has no linked user.
func TestApprovalStatus_ManagerFormNamesClaimant(t *testing.T) {
	t.Run("claim filed for another employee", func(t *testing.T) {
		e := at.New(t)
		d := e.NewDoc(t, at.Claim, 10, at.Rs(3_450), e.Anil, at.ForEmployee("EMP-007")) // Anil files for Deepak; E1: Priya
		at.MustOK(t, e.Submit(e.Anil, d))
		m19Want(t, "Priya (Deepak's manager), Anil submitted", e, e.Priya, d, "Deepak Nair's reporting manager", "Any amount")
	})

	t.Run("claimant without a linked user", func(t *testing.T) {
		e := at.New(t)
		d := e.NewDoc(t, at.Claim, 11, at.Rs(3_450), e.Anil, at.ForEmployee("EMP-007"))
		e.Unlink(t, "EMP-007") // Deepak's employee has no user; his manager Priya still matches
		at.MustOK(t, e.Submit(e.Anil, d))
		if r, ok := e.Latest(t, d); !ok || r.ManagerUser != e.Priya.ID || r.Claimant != 0 {
			t.Fatalf("request = %+v, %v; want ManagerUser Priya and no claimant user", r, ok)
		}
		m19Want(t, "Priya (manager of an unlinked claimant)", e, e.Priya, d, "Reporting manager", "Any amount")
	})
}
