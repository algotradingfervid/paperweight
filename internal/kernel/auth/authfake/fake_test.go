package authfake_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
)

// An unregistered DocType keeps the fake's Restrict lists (CD-157): a record
// outside them is not found, one inside is allowed, and Scope returns the
// SetScope Predicate (empty by default) once the grants allow it.
func TestFake_UnregisteredDocTypeRestrict(t *testing.T) {
	ctx := context.Background()
	const dt = "authfake.Unregistered"
	f := authfake.New()
	p := auth.Principal{ID: 4}
	f.Allow(4, dt, auth.RightRead)
	f.Restrict(4, []int64{10}, []int64{20})

	cases := []struct {
		what  string
		rec   *auth.RecordRef
		found bool
	}{
		{"inside both", &auth.RecordRef{DocType: dt, ID: 1, CreatedBy: 3, ProjectIDs: []int64{10}, SiteIDs: []int64{20}}, true},
		{"no ids", &auth.RecordRef{DocType: dt, ID: 2, CreatedBy: 3}, true},
		{"project outside", &auth.RecordRef{DocType: dt, ID: 3, CreatedBy: 3, ProjectIDs: []int64{11}}, false},
		{"site outside", &auth.RecordRef{DocType: dt, ID: 4, CreatedBy: 3, SiteIDs: []int64{21}}, false},
	}
	for _, c := range cases {
		err := f.Can(ctx, p, dt, auth.RightRead, c.rec)
		if c.found && err != nil {
			t.Errorf("Can on record %s: err = %v, want nil", c.what, err)
		}
		if !c.found && !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("Can on record %s: err = %v, want auth.ErrNotFound", c.what, err)
		}
	}

	f.Grant("Own Reader", dt, auth.RightRead, auth.ScopeOwn)
	f.AddRole(5, "Own Reader")
	pred, err := f.Scope(ctx, auth.Principal{ID: 5}, dt, auth.RightRead)
	if err != nil || pred.SQL != "" || len(pred.Args) != 0 {
		t.Errorf("Scope(own grant, unregistered) = ({%q, %v}, %v), want an empty Predicate and nil", pred.SQL, pred.Args, err)
	}
	f.SetScope(dt, auth.Predicate{SQL: "x = ?", Args: []any{1}})
	if pred, err := f.Scope(ctx, auth.Principal{ID: 5}, dt, auth.RightRead); err != nil || pred.SQL != "x = ?" {
		t.Errorf("Scope after SetScope = ({%q, %v}, %v), want the SetScope Predicate", pred.SQL, pred.Args, err)
	}
}
