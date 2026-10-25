package approval_test

import (
	"reflect"
	"testing"

	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

func eligibleFor(t *testing.T, e *at.Env, dt string, id int64) []types.UserID {
	t.Helper()
	var got []types.UserID
	at.MustOK(t, e.DB.Read(t.Context(), func(tx *db.Tx) (err error) {
		got, err = e.A.EligibleApprovers(t.Context(), tx, dt, id)
		return err
	}))
	return got
}

func TestApprovalEligible_ExcludesDisabled(t *testing.T) { // APR-041
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	// Before the disable both R1's Purchase Manager and R2's Director are eligible, sorted by user ID.
	if got, want := eligibleFor(t, e, at.Order, 42), []types.UserID{e.Meena.ID, e.Ravi.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("eligible before disable = %v, want %v (Meena, Ravi)", got, want)
	}
	e.Disable(t, e.Ravi.ID)
	if got, want := eligibleFor(t, e, at.Order, 42), []types.UserID{e.Meena.ID}; !reflect.DeepEqual(got, want) {
		t.Errorf("eligible after disabling Ravi = %v, want %v (Meena)", got, want)
	}
}
