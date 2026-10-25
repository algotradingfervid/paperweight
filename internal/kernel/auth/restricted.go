package auth

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Restricted reports whether p has at least one restriction on dimension,
// read through tx; false for the Administrator (CD-174). An unknown
// dimension is a caller bug, an error for everyone.
func (a *Auth) Restricted(ctx context.Context, tx *db.Tx, p Principal, dimension string) (bool, error) {
	if err := ValidDimension(dimension); err != nil {
		return false, err
	}
	if isAdmin(p) {
		return false, nil
	}
	ids, err := dbq.New(tx.SQL()).ListUserRestrictions(ctx, dbq.ListUserRestrictionsParams{UserID: int64(p.ID), Dimension: dimension})
	return len(ids) > 0, err
}

// ValidDimension returns an error naming d unless it is a restriction
// dimension (project or site).
func ValidDimension(d string) error {
	if Dimension(d) != DimProject && Dimension(d) != DimSite {
		return fmt.Errorf("auth: unknown dimension %q", d)
	}
	return nil
}

// Forbidden returns a 403 error showing msg that errors.Is ErrForbidden
// (CD-174).
func Forbidden(msg string) error {
	return &forbiddenError{msg: msg}
}
