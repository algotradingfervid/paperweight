package authfake

import (
	"context"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Restricted reports whether p has a Restrict list on dimension, like the
// real service: an unknown dimension is an error, and the Administrator is
// never restricted (CD-174).
func (f *Fake) Restricted(ctx context.Context, tx *db.Tx, p auth.Principal, dimension string) (bool, error) {
	if err := auth.ValidDimension(dimension); err != nil {
		return false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if slices.Contains(f.rolesLocked(p), auth.RoleAdministrator) {
		return false, nil
	}
	limit := f.restricted[p.ID]
	if auth.Dimension(dimension) == auth.DimProject {
		return limit.projects != nil, nil
	}
	return limit.sites != nil, nil
}
