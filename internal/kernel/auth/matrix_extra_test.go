package auth_test

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/testkit"
)

func TestSetRights_Guards(t *testing.T) {
	const dtName = "t3.Guard"
	t3Register(doctype.DocType{Name: dtName, Label: "Guard Order", PluralLabel: "Guard Orders"})
	a, d, _, rec := t3Auth(t)
	ctx := context.Background()
	admin := testkit.Principal(t, "admin@vardhan.test")
	priya := testkit.Principal(t, "priya@vardhan.test")
	row := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}
	v0 := t3PermVersion(t, d)

	if err := a.SetRights(ctx, priya, "Stores", dtName, row); !errors.Is(err, auth.ErrAdminOnlyRoles) {
		t.Errorf("SetRights by non-admin = %v, want ErrAdminOnlyRoles", err)
	}
	for _, role := range []string{auth.RoleAdministrator, "administrator", "ADMINISTRATOR"} {
		if err := a.SetRights(ctx, admin, role, dtName, row); !errors.Is(err, auth.ErrAdminRoleFixed) {
			t.Errorf("SetRights(%q) = %v, want ErrAdminRoleFixed", role, err)
		}
	}

	for _, tc := range []struct {
		rights map[auth.Right]auth.Scope
		msg    string
	}{
		{map[auth.Right]auth.Scope{auth.RightRead: ""}, "Scope for read must be own, team or all."},
		{map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightExport: "some"}, "Scope for export must be own, team or all."},
	} {
		err := a.SetRights(ctx, admin, "Stores", dtName, tc.rights)
		t3WantProblem(t, "SetRights with bad scope", err, tc.msg)
	}
	if got := t3Rows(t, d, "Stores", dtName); len(got) != 0 {
		t.Errorf("role_rights(Stores) after refused saves = %v, want none", got)
	}
	if v := t3PermVersion(t, d); v != v0 {
		t.Errorf("perm_version after refused saves = %d, want %d", v, v0)
	}
	if n := len(rec.Entries()); n != 0 {
		t.Errorf("audit entries after refused saves = %d, want 0", n)
	}
}

func TestSetRights_NoChangeNoAuditNoBump(t *testing.T) {
	const dtName = "t3.NoChange"
	t3Register(doctype.DocType{
		Name: dtName, Label: "Same Order", PluralLabel: "Same Orders",
		DefaultRights: map[string]map[string]string{"Stores": {"read": "all", "print": "own"}},
	})
	a, d, _, rec := t3Auth(t)
	ctx := context.Background()
	admin := testkit.Principal(t, "admin@vardhan.test")
	if err := a.SeedDefaults(ctx, auth.SeedOptions{}); err != nil {
		t.Fatalf("SeedDefaults = %v", err)
	}
	v0 := t3PermVersion(t, d)
	same := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightPrint: auth.ScopeOwn}
	if err := a.SetRights(ctx, admin, "Stores", dtName, same); err != nil {
		t.Fatalf("SetRights(unchanged) = %v", err)
	}
	if v := t3PermVersion(t, d); v != v0 {
		t.Errorf("perm_version after unchanged SetRights = %d, want %d", v, v0)
	}
	if n := len(rec.Entries()); n != 0 {
		t.Errorf("audit entries after unchanged SetRights = %d, want 0", n)
	}
	if got, want := t3Rows(t, d, "Stores", dtName), map[string]string{"read": "all", "print": "own"}; !maps.Equal(got, want) {
		t.Errorf("role_rights(Stores) = %v, want %v", got, want)
	}
}
