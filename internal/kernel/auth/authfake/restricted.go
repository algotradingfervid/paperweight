package authfake

import (
	"context"
	"fmt"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Restricted reports whether p has a Restrict list on dimension, like the
// real service: an unknown dimension is an error, and the Administrator is
// never restricted (CD-174).
func (f *Fake) Restricted(ctx context.Context, tx *db.Tx, p auth.Principal, dimension string) (bool, error) {
	d := auth.Dimension(dimension)
	if d != auth.DimProject && d != auth.DimSite {
		return false, fmt.Errorf("authfake: unknown dimension %q", dimension)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if slices.Contains(f.rolesLocked(p), auth.RoleAdministrator) {
		return false, nil
	}
	limit := f.restricted[p.ID]
	if d == auth.DimProject {
		return limit.projects != nil, nil
	}
	return limit.sites != nil, nil
}
