package auth

import (
	"cmp"
	"context"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// WhoCanUser is one enabled user holding a role, with the restriction codes
// from RestrictionLabels (Task 19).
type WhoCanUser struct {
	ID           UserID
	Name         string
	Restrictions map[Dimension][]string
}

// WhoCanRole is one role that holds a right on a DocType, with its scope and
// its enabled users (Task 19).
type WhoCanRole struct {
	Role  string
	Scope Scope
	Users []WhoCanUser
}

// WhoCan lists the roles and users that hold r on doctype (Task 19):
// Administrator first (scope all), then every role whose grant for r is not
// empty, by name, each with its enabled users by name. An unknown DocType is
// ErrNotFound; a right that doesn't apply is a NotApplicableError. Grants,
// users and restrictions are read in one transaction; nothing is written.
func (a *Auth) WhoCan(ctx context.Context, doctypeName string, r Right) ([]WhoCanRole, error) {
	dt, ok := doctype.Lookup(doctypeName)
	if !ok {
		return nil, ErrNotFound
	}
	if !Applies(dt, r) {
		return nil, NotApplicableError(r, dt.Label)
	}
	var out []WhoCanRole
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		grants, err := q.ListAllRoleRights(ctx)
		if err != nil {
			return err
		}
		out = []WhoCanRole{{Role: RoleAdministrator, Scope: ScopeAll}}
		for _, g := range grants {
			if g.Role != RoleAdministrator && g.Doctype == dt.Name && Right(g.RightName) == r && g.Scope != "" {
				out = append(out, WhoCanRole{Role: g.Role, Scope: Scope(g.Scope)})
			}
		}
		slices.SortFunc(out[1:], func(x, y WhoCanRole) int { return cmp.Compare(x.Role, y.Role) })

		users, err := q.ListUsers(ctx)
		if err != nil {
			return err
		}
		for _, u := range users {
			if u.Enabled == 0 {
				continue
			}
			roles, err := q.ListUserRoles(ctx, u.ID)
			if err != nil {
				return err
			}
			var labels map[Dimension][]string
			for i := range out {
				if !slices.Contains(roles, out[i].Role) {
					continue
				}
				if labels == nil {
					if labels, err = a.restrictionLabels(ctx, tx, UserID(u.ID)); err != nil {
						return err
					}
				}
				out[i].Users = append(out[i].Users, WhoCanUser{ID: UserID(u.ID), Name: u.Name, Restrictions: labels})
			}
		}
		for i := range out {
			slices.SortStableFunc(out[i].Users, func(x, y WhoCanUser) int { return cmp.Compare(x.Name, y.Name) })
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
