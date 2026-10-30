package auth

import (
	"context"
	"fmt"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Restricted reports whether p has at least one restriction on dimension,
// read through tx; false for the Administrator (CD-174). An unknown
// dimension is a caller bug, an error for everyone.
func (a *Auth) Restricted(ctx context.Context, tx *db.Tx, p Principal, dimension string) (bool, error) {
	ids, err := restrictionList(ctx, tx, p, dimension)
	return len(ids) > 0, err
}

// ValueAllowedTx reports whether p may use the value id of dimension, read
// through tx: true for the Administrator and for a dimension p isn't
// restricted on, otherwise whether id is in p's list (CD-216 item 7). An
// unknown dimension is a caller bug, an error for everyone.
func (a *Auth) ValueAllowedTx(ctx context.Context, tx *db.Tx, p Principal, dimension string, id int64) (bool, error) {
	ids, err := restrictionList(ctx, tx, p, dimension)
	if err != nil {
		return false, err
	}
	return len(ids) == 0 || slices.Contains(ids, id), nil
}

// restrictionList returns p's values on dimension, read through tx; none for
// the Administrator.
func restrictionList(ctx context.Context, tx *db.Tx, p Principal, dimension string) ([]int64, error) {
	if err := ValidDimension(dimension); err != nil {
		return nil, err
	}
	if isAdmin(p) {
		return nil, nil
	}
	return dbq.New(tx.SQL()).ListUserRestrictions(ctx, dbq.ListUserRestrictionsParams{UserID: int64(p.ID), Dimension: dimension})
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
