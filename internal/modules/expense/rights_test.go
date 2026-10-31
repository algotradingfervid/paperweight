package expense_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
)

// roleFallbackApprover is the canonical role the expense migration creates
// (spec §6, CD-238, 00-fixtures.md).
const roleFallbackApprover = "Fallback Approver"

// canonicalRoles are 00-fixtures.md's canonical roles, Fallback Approver included.
var canonicalRoles = []string{
	auth.RoleAdministrator, "Director", "Purchase Manager", "Purchase User", "Stores",
	"Masters Manager", auth.RoleEmployee, roleFallbackApprover,
}

// fixtureRights are the 00-fixtures.md rows for expense.Claim and
// expense.Advance (spec §11): the same rows for both. Administrator holds every
// applicable right through is_admin and has no row.
func fixtureRights() map[string]map[string]string {
	return map[string]map[string]string{
		auth.RoleEmployee: {
			"read": "team", "create": "own", "write": "own", "submit": "own",
			"amend": "own", "print": "own", "delete": "own", "approve": "team",
		},
		"Director":           {"read": "all", "approve": "all", "cancel": "all", "print": "all", "export": "all"},
		roleFallbackApprover: {"read": "all", "approve": "all"},
	}
}

var expenseDocTypes = []string{expense.ClaimDocType, expense.AdvanceDocType}

func lookupDT(t *testing.T, name string) *doctype.DocType {
	t.Helper()
	dt, ok := doctype.Lookup(name)
	if !ok {
		t.Fatalf("doctype.Lookup(%q): not registered", name)
	}
	return dt
}

// EXP-036
func TestExp_DefaultRights(t *testing.T) {
	if expense.ClaimDocType != "expense.Claim" || expense.AdvanceDocType != "expense.Advance" {
		t.Fatalf("ClaimDocType, AdvanceDocType = %q, %q; want expense.Claim, expense.Advance",
			expense.ClaimDocType, expense.AdvanceDocType)
	}
	want := fixtureRights()
	for _, name := range expenseDocTypes {
		t.Run(name, func(t *testing.T) {
			dt := lookupDT(t, name)
			if _, ok := dt.DefaultRights[auth.RoleAdministrator]; ok {
				t.Errorf("DefaultRights has an %s key; Administrator holds every right through is_admin", auth.RoleAdministrator)
			}
			if got := slices.Sorted(maps.Keys(dt.DefaultRights)); !slices.Equal(got, slices.Sorted(maps.Keys(want))) {
				t.Errorf("DefaultRights roles = %q, want %q", got, slices.Sorted(maps.Keys(want)))
			}
			for role, rights := range want {
				if got := dt.DefaultRights[role]; !maps.Equal(got, rights) {
					t.Errorf("DefaultRights[%q] = %v, want %v", role, got, rights)
				}
			}
		})
	}
}

// canKind classifies a Can result for the parity comparison (the auth
// parity tests' t5Kind).
func canKind(err error) string {
	switch {
	case err == nil:
		return "nil"
	case errors.Is(err, auth.ErrNotFound):
		return "notfound"
	case errors.Is(err, auth.ErrForbidden):
		return "forbidden"
	}
	return "other: " + err.Error()
}

// EXP-022: CloseRight is cancel on both DocTypes, and for every canonical role
// the real auth.Service (rows from SeedDefaults) and authfake (rows from
// 00-fixtures.md) agree on cancel and on every right in AllRights, plus one
// unknown right, for no record and for an own, a team and another
// employee's record. Only Director and Administrator may close.
func TestDocType_CloseRightParity(t *testing.T) {
	const (
		ravi    = "ravi@vardhan.test" // EMP-003; Priya (EMP-004) and Suresh (EMP-005) report to him
		unknown = auth.Right("foo")
	)
	closers := map[string]bool{auth.RoleAdministrator: true, "Director": true}
	rights := append(slices.Clone(auth.AllRights), unknown)

	for _, name := range expenseDocTypes {
		dt := lookupDT(t, name)
		if dt.CloseRight != string(auth.RightCancel) {
			t.Errorf("%s CloseRight = %q, want %q (the right the engine checks for close)", name, dt.CloseRight, auth.RightCancel)
		}
	}

	ctx := context.Background()
	w := authtest.NewWorld(t)
	recs := map[string]*auth.RecordRef{
		"no record": nil,
		"own":       {ID: 1, CreatedBy: w.Users[ravi], EmployeeID: w.Employees["EMP-003"]},
		"team":      {ID: 2, CreatedBy: w.Users["priya@vardhan.test"], EmployeeID: w.Employees["EMP-004"]},
		"other":     {ID: 3, CreatedBy: w.Users["kavya@vardhan.test"], EmployeeID: w.Employees["EMP-006"]},
	}
	recNames := slices.Sorted(maps.Keys(recs))

	for _, role := range canonicalRoles {
		t.Run(role, func(t *testing.T) {
			w.SetRoles(t, ravi, role)
			p := w.P(t, ravi)
			if !slices.Equal(p.Roles, []string{role}) {
				t.Fatalf("ravi's roles = %q, want only %q", p.Roles, role)
			}
			f := authfake.New()
			f.UseDirectory(w.Dir)
			for _, name := range expenseDocTypes {
				for r, s := range fixtureRights()[role] {
					f.Grant(role, name, auth.Right(r), auth.Scope(s))
				}
			}

			for _, name := range expenseDocTypes {
				for _, r := range rights {
					for _, rn := range recNames {
						rec := recs[rn]
						if rec != nil {
							rec = &auth.RecordRef{DocType: name, ID: rec.ID, CreatedBy: rec.CreatedBy, EmployeeID: rec.EmployeeID}
						}
						what := fmt.Sprintf("Can(%s, %s, %s)", name, r, rn)
						realErr, fakeErr := w.Auth.Can(ctx, p, name, r, rec), f.Can(ctx, p, name, r, rec)
						if rk, fk := canKind(realErr), canKind(fakeErr); rk != fk {
							t.Errorf("%s: real = %s (%v), authfake = %s (%v)", what, rk, realErr, fk, fakeErr)
						} else if realErr != nil && realErr.Error() != fakeErr.Error() {
							t.Errorf("%s: real message %q, authfake message %q, want the same", what, realErr.Error(), fakeErr.Error())
						}
						if r == auth.RightCancel && (rn == "no record" || rn == "other") {
							if want := closers[role]; (realErr == nil) != want || (fakeErr == nil) != want {
								t.Errorf("%s (the close right): real = %v, authfake = %v; want allowed %v", what, realErr, fakeErr, want)
							}
						}
						if role == auth.RoleAdministrator && rn == "no record" {
							if want := r != unknown && auth.Applies(lookupDT(t, name), r); (realErr == nil) != want {
								t.Errorf("%s as Administrator: real = %v, want allowed %v", what, realErr, want)
							}
						}
					}
				}
			}
		})
	}
}
