package auth_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// Task 17 fix round (AD-1 rulings): rename, versions, reset and seeding.

const t17xChanged = "Someone else changed Stores after you opened it. Reload it and make your changes again."

func t17xVersion(t *testing.T, w *authtest.World, role string) int {
	t.Helper()
	return t7Count(t, w, `SELECT version FROM roles WHERE name = ?`, role)
}

func t17xPermVersion(t *testing.T, w *authtest.World) int {
	t.Helper()
	return t7Count(t, w, `SELECT perm_version FROM auth_meta WHERE id = 1`)
}

func TestRoles_RenameStoredBumpedAudited(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	id := t3RoleID(t, w.DB, "Stores")
	v0, p0, n0 := t17xVersion(t, w, "Stores"), t17xPermVersion(t, w), len(w.Audit.Entries())

	if err := w.Auth.RenameRole(ctx, admin, "Stores", "Warehouse"); err != nil {
		t.Fatalf("RenameRole(Stores, Warehouse) = %v, want nil", err)
	}
	if n := t7Count(t, w, `SELECT COUNT(*) FROM roles WHERE id = ? AND name = 'Warehouse' AND name_key = 'warehouse'`, id); n != 1 {
		t.Fatalf("role %d named Warehouse = %d rows, want 1", id, n)
	}
	if v := t17xVersion(t, w, "Warehouse"); v != v0+1 {
		t.Errorf("version after rename = %d, want %d", v, v0+1)
	}
	if p := t17xPermVersion(t, w); p <= p0 {
		t.Errorf("perm_version after rename = %d, want > %d", p, p0)
	}
	es := w.Audit.Entries()[n0:]
	want := []audit.Entry{{
		DocType: "Role", DocID: id, Action: "rename", By: admin.ID, At: authtest.T0,
		Changes: []audit.Change{{Field: "name", Before: "Stores", After: "Warehouse", Op: "set"}},
	}}
	if len(es) == 1 && es[0].At.Equal(authtest.T0) {
		es[0].At = authtest.T0
	}
	if !reflect.DeepEqual(es, want) {
		t.Errorf("audit entries = %+v, want %+v", es, want)
	}
}

func TestRoles_IDMethodsRefuseStaleVersion(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	id := t3RoleID(t, w.DB, "Stores")
	stale := int64(t17xVersion(t, w, "Stores"))
	if err := w.Auth.RenameRole(ctx, admin, "Stores", "Stores"); err != nil {
		t.Fatalf("RenameRole to the same name = %v", err)
	}
	read := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}
	if err := w.Auth.SetRights(ctx, admin, "Stores", authtest.DTItem, read); err != nil {
		t.Fatalf("SetRights(Stores, Item) = %v", err)
	}
	if v := int64(t17xVersion(t, w, "Stores")); v != stale+1 {
		t.Fatalf("version after a rights save = %d, want %d", v, stale+1)
	}
	n0 := len(w.Audit.Entries())

	t7WantText(t, "RenameRoleID(stale)", w.Auth.RenameRoleID(ctx, admin, id, "Warehouse", stale), t17xChanged)
	t7WantText(t, "DeleteRoleID(stale)", w.Auth.DeleteRoleID(ctx, admin, id, stale), t17xChanged)
	t7WantText(t, "SetRightsID(stale)", w.Auth.SetRightsID(ctx, admin, id, authtest.DTOrder, read, stale), t17xChanged)
	if n := len(w.Audit.Entries()); n != n0 {
		t.Errorf("audit entries after stale writes = %d, want %d", n, n0)
	}
	if n := t7Count(t, w, `SELECT COUNT(*) FROM roles WHERE id = ? AND name = 'Stores'`, id); n != 1 {
		t.Errorf("Stores after stale writes: %d rows, want 1", n)
	}

	if err := w.Auth.RenameRoleID(ctx, admin, id, "Warehouse", stale+1); err != nil {
		t.Fatalf("RenameRoleID(current version) = %v, want nil", err)
	}
}

func TestRights_ResetMasterKeepsLinkedRead(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	read := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}
	// A production matrix: Purchase User creates POs, which link to Site.
	w.SetRights(t, "Purchase User", authtest.DTSite, read)
	v0 := t17xVersion(t, w, "Purchase User")

	if err := w.Auth.ResetRights(ctx, admin, authtest.DTSite, auth.SeedOptions{LinkedMasterRead: true}); err != nil {
		t.Fatalf("ResetRights(Site, LinkedMasterRead) = %v", err)
	}
	if got, err := w.Auth.RoleRights(ctx, "Purchase User", authtest.DTSite); err != nil || !reflect.DeepEqual(got, read) {
		t.Errorf("RoleRights(Purchase User, Site) after reset = (%v, %v), want (%v, nil)", got, err, read)
	}
	if v := t17xVersion(t, w, "Purchase User"); v != v0 {
		t.Errorf("Purchase User version after an unchanged reset = %d, want %d", v, v0)
	}

	// The test seed has no derived reads, so a plain reset drops the row.
	n0 := len(w.Audit.Entries())
	if err := w.Auth.ResetRights(ctx, admin, authtest.DTSite, auth.SeedOptions{}); err != nil {
		t.Fatalf("ResetRights(Site) = %v", err)
	}
	if got, _ := w.Auth.RoleRights(ctx, "Purchase User", authtest.DTSite); len(got) != 0 {
		t.Errorf("RoleRights(Purchase User, Site) after a plain reset = %v, want none", got)
	}
	if v := t17xVersion(t, w, "Purchase User"); v != v0+1 {
		t.Errorf("Purchase User version after a changing reset = %d, want %d", v, v0+1)
	}
	// One reset entry per role that loses its derived read: the creators of
	// PO (Purchase User), GRN (Stores) and Claim (Employee).
	var ids []int64
	for _, e := range w.Audit.Entries()[n0:] {
		if e.Action != "reset" || !reflect.DeepEqual(e.Changes, []audit.Change{{Field: authtest.DTSite + ".read", Before: "all", After: "none", Op: "set"}}) {
			t.Errorf("audit entry after the plain reset = %+v, want a reset of %s.read all → none", e, authtest.DTSite)
		}
		ids = append(ids, e.DocID)
	}
	wantIDs := []int64{t3RoleID(t, w.DB, "Purchase User"), t3RoleID(t, w.DB, "Stores"), t3RoleID(t, w.DB, "Employee")}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Errorf("roles audited by the plain reset = %v, want %v", ids, wantIDs)
	}
}

func TestSeed_DeletedRoleNotResurrected(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	w.RemoveRole(t, t17Suresh, "Stores")
	if err := w.Auth.DeleteRole(ctx, admin, "Stores"); err != nil {
		t.Fatalf("DeleteRole(Stores) = %v", err)
	}
	if err := w.Auth.SeedDefaults(ctx, auth.SeedOptions{LinkedMasterRead: true}); err != nil {
		t.Fatalf("SeedDefaults again = %v", err)
	}
	for _, dt := range []string{authtest.DTOrder, authtest.DTReceipt, authtest.DTSite} {
		if err := w.Auth.ResetRights(ctx, admin, dt, auth.SeedOptions{LinkedMasterRead: true}); err != nil {
			t.Fatalf("ResetRights(%s) = %v", dt, err)
		}
	}
	if n := t7Count(t, w, `SELECT COUNT(*) FROM roles WHERE name_key = 'stores'`); n != 0 {
		t.Errorf("roles named Stores after re-seed and resets = %d, want 0", n)
	}
	// The other roles still get their rows.
	orderPM := map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightApprove: auth.ScopeAll, auth.RightCancel: auth.ScopeAll,
		auth.RightAmend: auth.ScopeAll, auth.RightPrint: auth.ScopeAll, auth.RightExport: auth.ScopeAll,
	}
	if got, err := w.Auth.RoleRights(ctx, "Purchase Manager", authtest.DTOrder); err != nil || !reflect.DeepEqual(got, orderPM) {
		t.Errorf("RoleRights(Purchase Manager, PO) after reset = (%v, %v), want (%v, nil)", got, err, orderPM)
	}
}

func TestSeed_RenamedRoleNotResurrected(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	if err := w.Auth.RenameRole(ctx, admin, "Stores", "Warehouse"); err != nil {
		t.Fatalf("RenameRole(Stores, Warehouse) = %v", err)
	}
	if err := w.Auth.SeedDefaults(ctx, auth.SeedOptions{LinkedMasterRead: true}); err != nil {
		t.Fatalf("SeedDefaults again = %v", err)
	}
	if err := w.Auth.ResetRights(ctx, admin, authtest.DTReceipt, auth.SeedOptions{LinkedMasterRead: true}); err != nil {
		t.Fatalf("ResetRights(%s) = %v", authtest.DTReceipt, err)
	}
	if n := t7Count(t, w, `SELECT COUNT(*) FROM roles WHERE name_key = 'stores'`); n != 0 {
		t.Errorf("roles named Stores after rename, re-seed and reset = %d, want 0", n)
	}
	// The reset leaves the renamed role's rows on other DocTypes alone.
	want := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}
	if got, err := w.Auth.RoleRights(ctx, "Warehouse", authtest.DTOrder); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("RoleRights(Warehouse, PO) after reset = (%v, %v), want (%v, nil)", got, err, want)
	}
}
