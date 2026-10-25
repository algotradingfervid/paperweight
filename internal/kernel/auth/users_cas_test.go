package auth_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// An admin reset that commits between ChangeOwnPassword's verify and its
// write must survive: the stale change fails and must_change_password stays 1.
func TestUser_ChangeOwnPassword_LosesToConcurrentReset(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	admin := w.P(t, t7Admin)
	nisha := w.AddUser(t, t7Nisha, "Nisha Menon", "Employee")
	const first, reset = "Granite-River-4417", "Copper-Lantern-8823"
	if err := w.Auth.ResetPassword(ctx, admin, nisha, first); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}

	raced := false
	a := auth.New(auth.Deps{
		DB: w.DB, Clock: w.Clock, Directory: w.Dir, Audit: w.Audit, Refs: w.Refs, Info: w.Info,
		Verify: func(encoded, password string) (bool, bool, error) {
			if !raced {
				raced = true
				if err := w.Auth.ResetPassword(ctx, admin, nisha, reset); err != nil {
					t.Fatalf("ResetPassword in verify: %v", err)
				}
			}
			return auth.VerifyPassword(encoded, password)
		},
	})
	err := a.ChangeOwnPassword(ctx, nisha, first, "Velvet-Harbor-5531", "Velvet-Harbor-5531", "")
	t7WantProblem(t, "ChangeOwnPassword after reset", err, auth.ErrCurrentPasswordWrong)

	if n := t7Count(t, w, `SELECT must_change_password FROM users WHERE id = ?`, int64(nisha)); n != 1 {
		t.Errorf("must_change_password = %d, want 1", n)
	}
	hash := t7Strings(t, w, `SELECT password_hash FROM users WHERE id = ?`, int64(nisha))
	if len(hash) != 1 {
		t.Fatalf("password_hash rows = %d, want 1", len(hash))
	}
	if ok, _, err := auth.VerifyPassword(hash[0], reset); err != nil || !ok {
		t.Errorf("stored hash does not match the reset password (ok=%v, err=%v)", ok, err)
	}
}

// Roles are compared and audited by their stored names, so a different
// spelling of the roles a user already holds changes nothing.
func TestUser_UpdateRoles_StoredNames(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	admin := w.P(t, t7Admin)
	nisha := w.AddUser(t, t7Nisha, "Nisha Menon", "Employee")
	audits := len(w.Audit.Entries())

	roles := []string{"employee"}
	if _, err := w.Auth.UpdateUser(ctx, admin, nisha, auth.UserPatch{Roles: &roles}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if n := len(w.Audit.Entries()); n != audits {
		t.Errorf("audit entries = %d, want %d (no-op)", n, audits)
	}

	id, _, err := w.Auth.CreateUser(ctx, admin, auth.NewUser{
		Email: t7Kiran, Name: "Kiran Rao", Password: "Granite-River-4417", Roles: []string{"employee", "EMPLOYEE"},
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	found := false
	for _, e := range w.Audit.For("User", int64(id)) {
		for _, c := range e.Changes {
			if c.Field == "roles" {
				found = true
				if c.After != "Employee" {
					t.Errorf("audited roles = %q, want %q", c.After, "Employee")
				}
			}
		}
	}
	if !found {
		t.Error("no roles change audited for the created user")
	}
}
