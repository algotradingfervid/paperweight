package auth

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
)

// Restricted reports whether p has at least one restriction on dimension,
// read through tx; false for the Administrator (CD-174).
//
// Stub: CD-174 is not built yet.
func (a *Auth) Restricted(ctx context.Context, tx *db.Tx, p Principal, dimension string) (bool, error) {
	return false, fmt.Errorf("auth.Restricted: %w", errs.ErrNotImplemented)
}

// Forbidden returns a 403 error showing msg that errors.Is ErrForbidden
// (CD-174).
//
// Stub: CD-174 is not built yet.
func Forbidden(msg string) error {
	return fmt.Errorf("auth.Forbidden: %w", errs.ErrNotImplemented)
}
