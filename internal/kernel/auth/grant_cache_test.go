package auth_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/testkit"
)

// The cached matrix is keyed by perm_version, so a SetRights shows in the next
// grant without any reload step.
func TestGrant_ReflectsSetRightsImmediately(t *testing.T) {
	const dtName = "t3.GrantCache"
	doctype.Register(doctype.DocType{
		Name: dtName, Label: "Grant Order", PluralLabel: "Grant Orders",
		DefaultRights: map[string]map[string]string{
			"Purchase User": {"read": "all", "export": "all", "delete": "own"},
		},
	})
	a, _, _, _ := t3Auth(t)
	ctx := context.Background()
	admin := testkit.Principal(t, "admin@vardhan.test")
	if err := a.SeedDefaults(ctx, auth.SeedOptions{}); err != nil {
		t.Fatalf("SeedDefaults = %v", err)
	}

	roles := []string{"Stores", "Purchase User"}
	check := func(when string, r auth.Right, want auth.Scope) {
		t.Helper()
		got, err := auth.Grant(a, ctx, roles, dtName, r)
		if err != nil || got != want {
			t.Errorf("%s: grant(%v, %s, %s) = (%q, %v), want (%q, nil)", when, roles, dtName, r, got, err, want)
		}
	}
	check("seeded", auth.RightExport, auth.ScopeAll)
	check("seeded", auth.RightDelete, auth.ScopeOwn)
	check("seeded", auth.RightWrite, "")

	if err := a.SetRights(ctx, admin, "Purchase User", dtName, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightDelete: auth.ScopeAll,
	}); err != nil {
		t.Fatalf("SetRights = %v", err)
	}
	check("after SetRights", auth.RightExport, "")
	check("after SetRights", auth.RightDelete, auth.ScopeAll)

	// The widest scope over the roles wins.
	if err := a.SetRights(ctx, admin, "Stores", dtName, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightExport: auth.ScopeOwn,
	}); err != nil {
		t.Fatalf("SetRights(Stores) = %v", err)
	}
	check("after SetRights(Stores)", auth.RightExport, auth.ScopeOwn)
	check("after SetRights(Stores)", auth.RightRead, auth.ScopeAll)
}
