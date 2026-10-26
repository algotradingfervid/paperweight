package approvaltest_test

import (
	"errors"
	"slices"
	"testing"

	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/testkit"
)

func TestEnv_SeedAndDoubles(t *testing.T) {
	e := at.New(t)
	ctx := t.Context()
	if e.Ravi.Name != "Ravi Kulkarni" || e.Meena.Name != "Meena Iyer" {
		t.Fatal("canonical users missing")
	}
	if e.R1 == 0 || e.R2 == 0 || e.E1 == 0 {
		t.Errorf("seed rules not stored: R1=%d R2=%d E1=%d", e.R1, e.R2, e.E1)
	}
	ok, err := e.Auth.HasRole(ctx, e.Ravi.ID, "Purchase Manager")
	if err != nil || !ok {
		t.Errorf("Ravi should hold Purchase Manager: %v", err)
	}
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	if got := e.Title(t, d); got != "Draft 42" {
		t.Errorf("title %q", got)
	}
	// On PRJ-METRO, the project Priya is restricted to (AUTH-093), so her
	// refusal is the missing approve right, not the restriction.
	var metro int64
	for _, p := range e.F.Projects {
		if p.Code == "PRJ-METRO" {
			metro = p.ID
		}
	}
	po42 := &auth.RecordRef{DocType: at.Order, ID: 42, ProjectIDs: []int64{metro}}
	if err := e.Auth.Can(ctx, e.Ravi, at.Order, "approve", po42); err != nil {
		t.Error(err)
	}
	if err := e.Auth.Can(ctx, e.Priya, at.Order, "approve", po42); !errors.Is(err, auth.ErrForbidden) {
		t.Error(err)
	}
	m, err := e.Dir.ManagerChain(ctx, nil, e.Emp("EMP-007"))
	if err != nil || len(m) == 0 || m[0] != e.Emp("EMP-004") {
		t.Errorf("Deepak's manager = %v (%v), want Priya (EMP-004)", m, err)
	}
	claim := &auth.RecordRef{DocType: at.Claim, ID: 10, CreatedBy: e.Deepak.ID, EmployeeID: e.Emp("EMP-007")}
	if err := e.Auth.Can(ctx, e.Priya, at.Claim, "approve", claim); err != nil {
		t.Errorf("team scope: %v", err) // CD-11
	}
	if err := e.Auth.Can(ctx, e.Ravi, at.Claim, "read", claim); !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("Ravi is not Deepak's manager: %v", err) // CD-54
	}
	if p := testkit.Principal(t, "priya@vardhan.test"); p.ID != e.Priya.ID {
		t.Error("principal by email (CD-72)")
	}
	// CD-86: the Administrator row seeded with Grant("Administrator", "*", …) gives Anil every right
	if err := e.Auth.Can(ctx, e.Anil, at.Receipt, "approve", &auth.RecordRef{DocType: at.Receipt, ID: 7}); err != nil {
		t.Errorf("admin wildcard: %v", err)
	}
	// CD-86: UsersWithRight skips disabled users; RemoveRole takes effect on the next call
	ids, err := e.Auth.UsersWithRight(ctx, at.Order, "approve", &auth.RecordRef{DocType: at.Order, ID: 42})
	if err != nil || !slices.Contains(ids, e.Ravi.ID) {
		t.Errorf("approvers %v (%v) lack Ravi", ids, err)
	}
	e.Disable(t, e.Ravi.ID)
	ids, err = e.Auth.UsersWithRight(ctx, at.Order, "approve", &auth.RecordRef{DocType: at.Order, ID: 42})
	if err != nil || slices.Contains(ids, e.Ravi.ID) {
		t.Errorf("disabled Ravi still in %v (%v)", ids, err)
	}
	if ok, err := e.Auth.HasRole(ctx, e.Meena.ID, "Director"); err != nil || !ok {
		t.Fatalf("Meena should hold Director before RemoveRole: %v", err)
	}
	e.RemoveRole(t, e.Meena.ID, "Director")
	if ok, _ := e.Auth.HasRole(ctx, e.Meena.ID, "Director"); ok {
		t.Error("RemoveRole had no effect")
	}
}
