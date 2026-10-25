package auth

import (
	"context"
	"errors"
)

// Task 17 stubs (test-writer). The implementer replaces these bodies;
// DisplayName and EnabledUsers may move to users.go (plan Task 17).

var errT17NotImplemented = errors.New("auth: not implemented (WP1-AUTH Task 17)")

// CreateRole adds a role; names are unique by name_key (RoleExistsError).
func (a *Auth) CreateRole(ctx context.Context, actor Principal, name string) error {
	return errT17NotImplemented
}

// RenameRole renames role; the Administrator role is fixed.
func (a *Auth) RenameRole(ctx context.Context, actor Principal, role, newName string) error {
	return errT17NotImplemented
}

// DeleteRole deletes role and its matrix rows; refused while it is assigned.
func (a *Auth) DeleteRole(ctx context.Context, actor Principal, role string) error {
	return errT17NotImplemented
}

// ResetRights deletes the matrix rows of doctype, then runs SeedDefaults.
func (a *Auth) ResetRights(ctx context.Context, actor Principal, doctype string, opts SeedOptions) error {
	return errT17NotImplemented
}

// DisplayName is u's name, with " (disabled)" for a disabled user.
func (a *Auth) DisplayName(ctx context.Context, u UserID) (string, error) {
	return "", errT17NotImplemented
}

// EnabledUsers lists the enabled users.
func (a *Auth) EnabledUsers(ctx context.Context) ([]UserID, error) {
	return nil, errT17NotImplemented
}
