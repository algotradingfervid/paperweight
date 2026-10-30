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
	list, err := f.restrictionList(p, dimension)
	return list != nil, err
}

// ValueAllowedTx reports whether id is in p's Restrict list on dimension, like
// the real service: no list allows every value (CD-216 item 7).
func (f *Fake) ValueAllowedTx(ctx context.Context, tx *db.Tx, p auth.Principal, dimension string, id int64) (bool, error) {
	list, err := f.restrictionList(p, dimension)
	if err != nil {
		return false, err
	}
	return list == nil || slices.Contains(list, id), nil
}

// restrictionList returns p's Restrict list on dimension, nil when p has none.
func (f *Fake) restrictionList(p auth.Principal, dimension string) ([]int64, error) {
	if err := auth.ValidDimension(dimension); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if slices.Contains(f.rolesLocked(p), auth.RoleAdministrator) {
		return nil, nil
	}
	limit := f.restricted[p.ID]
	if auth.Dimension(dimension) == auth.DimProject {
		return limit.projects, nil
	}
	return limit.sites, nil
}
