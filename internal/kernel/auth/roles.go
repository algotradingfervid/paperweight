package auth

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Role administration (plan Task 17). Every method needs an Administrator,
// and the Administrator role itself is fixed. The name-based methods serve
// seeding, the CLI and tests; the web uses the ID methods, which find the
// role inside the write and refuse a stale version (CD-94).

// RoleInfo is one role as the roles page shows it.
type RoleInfo struct {
	ID      int64
	Name    string
	IsAdmin bool
	Version int64
	Users   int // users holding the role
}

// RightsMatrix returns every role (ascending ID) and the whole matrix, keyed
// by DocType then role ID, read together.
func (a *Auth) RightsMatrix(ctx context.Context, actor Principal) ([]RoleInfo, map[string]map[int64]map[Right]Scope, error) {
	if !isAdmin(actor) {
		return nil, nil, ErrAdminOnlyRoles
	}
	var roles []RoleInfo
	matrix := map[string]map[int64]map[Right]Scope{}
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		rows, err := q.ListRoles(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			roles = append(roles, RoleInfo{ID: r.ID, Name: r.Name, IsAdmin: r.IsAdmin != 0, Version: r.Version, Users: int(r.Users)})
		}
		rights, err := q.ListAllRightsByRoleID(ctx)
		if err != nil {
			return err
		}
		for _, r := range rights {
			if matrix[r.Doctype] == nil {
				matrix[r.Doctype] = map[int64]map[Right]Scope{}
			}
			if matrix[r.Doctype][r.RoleID] == nil {
				matrix[r.Doctype][r.RoleID] = map[Right]Scope{}
			}
			matrix[r.Doctype][r.RoleID][Right(r.RightName)] = Scope(r.Scope)
		}
		return nil
	})
	return roles, matrix, err
}

func docTypeRights(ctx context.Context, q *dbq.Queries, doctypeName string) (map[int64]map[Right]Scope, error) {
	rows, err := q.ListDocTypeRights(ctx, doctypeName)
	if err != nil {
		return nil, err
	}
	out := map[int64]map[Right]Scope{}
	for _, r := range rows {
		if out[r.RoleID] == nil {
			out[r.RoleID] = map[Right]Scope{}
		}
		out[r.RoleID][Right(r.RightName)] = Scope(r.Scope)
	}
	return out, nil
}

// roleRef names the role a write targets: by name (any case), or by ID with
// the version the page was opened at.
type roleRef struct {
	name    string
	id      int64
	version *int64
}

// load reads the role inside the write: missing is ErrNotFound, a version
// other than the posted one is RoleChangedError.
func (k roleRef) load(ctx context.Context, q *dbq.Queries) (dbq.Role, error) {
	var (
		r   dbq.Role
		err error
	)
	if k.name != "" {
		r, err = q.GetRoleByName(ctx, k.name)
	} else {
		r, err = q.GetRoleByID(ctx, k.id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if k.version != nil && r.Version != *k.version {
		return r, RoleChangedError(r.Name)
	}
	return r, nil
}

// roleName trims name; an empty name is ErrBadForm.
func roleName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrBadForm
	}
	return name, nil
}

// checkNameFree returns RoleExistsError when a role other than self has
// name's key.
func checkNameFree(ctx context.Context, q *dbq.Queries, name string, self int64) error {
	other, err := roleRef{name: name}.load(ctx, q)
	switch {
	case errors.Is(err, ErrNotFound):
		return nil
	case err != nil:
		return err
	case other.ID != self:
		return RoleExistsError(other.Name)
	}
	return nil
}

// CreateRole adds a role; names are unique by name_key (RoleExistsError).
func (a *Auth) CreateRole(ctx context.Context, actor Principal, name string) error {
	if !isAdmin(actor) {
		return ErrAdminOnlyRoles
	}
	name, err := roleName(name)
	if err != nil {
		return err
	}
	return a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		if err := checkNameFree(ctx, q, name, 0); err != nil {
			return err
		}
		now := a.clock.Now()
		id, err := q.CreateRole(ctx, dbq.CreateRoleParams{Name: name, At: FormatTime(now), By: byArg(actor.ID)})
		if err != nil {
			return err
		}
		return a.audit.Append(ctx, tx, audit.Entry{
			DocType: "Role", DocID: id, Action: "create",
			Changes: []audit.Change{{Field: "name", After: name, Op: "set"}},
			By:      actor.ID, At: now,
		})
	})
}

// RenameRole renames role; the Administrator role is fixed. The rename bumps
// the role's version and perm_version (the matrix is keyed by role name).
func (a *Auth) RenameRole(ctx context.Context, actor Principal, role, newName string) error {
	return a.renameRole(ctx, actor, roleRef{name: role}, newName)
}

// RenameRoleID is RenameRole for role id at the posted version.
func (a *Auth) RenameRoleID(ctx context.Context, actor Principal, id int64, newName string, version int64) error {
	return a.renameRole(ctx, actor, roleRef{id: id, version: &version}, newName)
}

func (a *Auth) renameRole(ctx context.Context, actor Principal, ref roleRef, newName string) error {
	if !isAdmin(actor) {
		return ErrAdminOnlyRoles
	}
	newName, err := roleName(newName)
	if err != nil {
		return err
	}
	return a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		r, err := ref.load(ctx, q)
		if err != nil {
			return err
		}
		if r.IsAdmin != 0 {
			return ErrAdminRoleFixed
		}
		if r.Name == newName {
			return nil
		}
		if err := checkNameFree(ctx, q, newName, r.ID); err != nil {
			return err
		}
		now := a.clock.Now()
		if err := q.RenameRole(ctx, dbq.RenameRoleParams{Name: newName, At: FormatTime(now), By: byArg(actor.ID), ID: r.ID}); err != nil {
			return err
		}
		if err := q.BumpPermVersion(ctx); err != nil {
			return err
		}
		return a.audit.Append(ctx, tx, audit.Entry{
			DocType: "Role", DocID: r.ID, Action: "rename",
			Changes: []audit.Change{{Field: "name", Before: r.Name, After: newName, Op: "set"}},
			By:      actor.ID, At: now,
		})
	})
}

// DeleteRole deletes role and its matrix rows; refused while it is assigned.
func (a *Auth) DeleteRole(ctx context.Context, actor Principal, role string) error {
	return a.deleteRole(ctx, actor, roleRef{name: role})
}

// DeleteRoleID is DeleteRole for role id at the posted version.
func (a *Auth) DeleteRoleID(ctx context.Context, actor Principal, id, version int64) error {
	return a.deleteRole(ctx, actor, roleRef{id: id, version: &version})
}

func (a *Auth) deleteRole(ctx context.Context, actor Principal, ref roleRef) error {
	if !isAdmin(actor) {
		return ErrAdminOnlyRoles
	}
	return a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		r, err := ref.load(ctx, q)
		if err != nil {
			return err
		}
		if r.IsAdmin != 0 {
			return ErrAdminRoleFixed
		}
		n, err := q.CountRoleUsers(ctx, r.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return RoleInUseError(r.Name, int(n))
		}
		// role_doctypes and role_rights go with it (ON DELETE CASCADE).
		if err := q.DeleteRole(ctx, r.ID); err != nil {
			return err
		}
		if err := q.BumpPermVersion(ctx); err != nil {
			return err
		}
		return a.audit.Append(ctx, tx, audit.Entry{
			DocType: "Role", DocID: r.ID, Action: string(doctype.ActionDelete),
			Changes: []audit.Change{{Field: "name", Before: r.Name, Op: "set"}},
			By:      actor.ID, At: a.clock.Now(),
		})
	})
}

// ResetRights deletes the matrix rows of doctype and seeds that DocType's
// defaults again for the roles that exist, in one write. Each role whose
// rights change gets its version bumped and one audit entry.
func (a *Auth) ResetRights(ctx context.Context, actor Principal, doctypeName string, opts SeedOptions) error {
	if !isAdmin(actor) {
		return ErrAdminOnlyRoles
	}
	if _, ok := doctype.Lookup(doctypeName); !ok {
		return ErrNotFound
	}
	return a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		before, err := docTypeRights(ctx, q, doctypeName)
		if err != nil {
			return err
		}
		if err := q.DeleteDocTypeRows(ctx, doctypeName); err != nil {
			return err
		}
		if err := a.seed(ctx, q, opts, doctypeName); err != nil {
			return err
		}
		after, err := docTypeRights(ctx, q, doctypeName)
		if err != nil {
			return err
		}
		now := a.clock.Now()
		ids := slices.Collect(maps.Keys(before))
		for id := range after {
			if _, ok := before[id]; !ok {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		for _, id := range ids {
			changes := rightsChanges(doctypeName, before[id], after[id])
			if len(changes) == 0 {
				continue
			}
			if err := q.BumpRoleVersion(ctx, dbq.BumpRoleVersionParams{At: FormatTime(now), ID: id}); err != nil {
				return err
			}
			if err := a.audit.Append(ctx, tx, audit.Entry{
				DocType: "Role", DocID: id, Action: "reset", Changes: changes,
				By: actor.ID, At: now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}
