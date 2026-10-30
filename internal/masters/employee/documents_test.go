package employee_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/mstfixture"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// searchCodes returns the codes Search offers Kavya, and whether each is disabled.
func searchCodes(t *testing.T, env *msttest.Env, text string, includeDisabled bool) map[string]bool {
	t.Helper()
	got, err := employee.New(env.Kit).Search(context.Background(), env.Kavya, text, includeDisabled)
	if err != nil {
		t.Fatalf("Search(%q, %v): %v", text, includeDisabled, err)
	}
	out := make(map[string]bool, len(got))
	for _, c := range got {
		out[c.Code] = c.Disabled
	}
	return out
}

// TestEmployee_LeftHidden: a Left employee isn't offered by Search unless asked
// for, and a draft made before they left fails on save with MST-120's text, as
// does a new one. Plan Task 19 uses EMP-007: EMP-008 is Left from the start,
// so no earlier draft can name them.
func TestEmployee_LeftHidden(t *testing.T) { // MST-120
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	ctx := context.Background()
	id := m.Employees["EMP-007"]

	if got := searchCodes(t, env, "", false); len(got) != 7 {
		t.Errorf("Search before the leave = %v; want the 7 active employees", got)
	}
	if got, want := searchCodes(t, env, "Deepak", false), map[string]bool{"EMP-007": false}; !reflect.DeepEqual(got, want) {
		t.Errorf(`Search("Deepak") = %v; want %v`, got, want)
	}

	h := mstfixture.Header{VendorID: m.Parties["SHREE"], BranchID: m.Branches["PUNE"], EmployeeID: id, Date: date(2026, time.October, 24)}
	saved, err := mstfixture.Save(ctx, env, env.Kavya, 0, h, nil)
	if err != nil {
		t.Fatalf("save draft while active: %v", err)
	}

	svc := employee.New(env.Kit)
	var e employee.Employee
	if err := env.DB.Read(ctx, func(tx *db.Tx) (err error) {
		e, err = svc.Get(ctx, tx, id)
		return err
	}); err != nil {
		t.Fatalf("get EMP-007: %v", err)
	}
	in := employee.Input{Code: e.Code, Name: e.Name, BranchID: e.BranchID, UserID: e.UserID, ManagerID: e.ManagerID, Joining: e.Joining}
	leave(&in)
	if _, err := svc.Update(ctx, env.Kavya, id, e.Version, in); err != nil {
		t.Fatalf("mark EMP-007 Left: %v", err)
	}

	if got := searchCodes(t, env, "", false); len(got) != 6 {
		t.Errorf("Search after the leave = %v; want 6 without EMP-007 and EMP-008", got)
	} else if _, ok := got["EMP-007"]; ok {
		t.Errorf("Search after the leave = %v; EMP-007 is offered", got)
	}
	if disabled, ok := searchCodes(t, env, "EMP-007", true)["EMP-007"]; !ok || !disabled {
		t.Errorf("Search with disabled: EMP-007 present %v, disabled %v; want both", ok, disabled)
	}

	want := doctype.FieldError{Field: "employee_id", Message: msgEmpLeft}
	_, err = mstfixture.Save(ctx, env, env.Kavya, saved.ID, h, nil)
	wantDocFieldError(t, "existing draft", err, want)
	_, err = mstfixture.Save(ctx, env, env.Kavya, 0, h, nil)
	wantDocFieldError(t, "new draft", err, want)
}

// TestEmployee_SearchNeedsRead: Search goes through the kernel list, so a
// missing read right refuses it.
func TestEmployee_SearchNeedsRead(t *testing.T) {
	env := msttest.New(t)
	noRoles := env.Kavya
	noRoles.Roles = nil
	_, err := employee.New(env.Kit).Search(context.Background(), noRoles, "", false)
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != http.StatusForbidden {
		t.Errorf("Search with no roles = %v; want a 403", err)
	}
}
