package auth

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// User administration for authweb (CD-143). These are methods on *Auth, not
// on the §5 Service interface.

// ListUsers returns every user in ascending ID order. Only an Administrator
// may list them.
func (a *Auth) ListUsers(ctx context.Context, actor Principal) ([]UserInfo, error) {
	if !isAdmin(actor) {
		return nil, ErrAdminOnlyUsers
	}
	var out []UserInfo
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		rows, err := dbq.New(tx.SQL()).ListUsers(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			out = append(out, UserInfo{ID: UserID(r.ID), Email: r.Email, Name: r.Name, Enabled: r.Enabled != 0})
		}
		return nil
	})
	return out, err
}

// UserForEdit returns user id, the version its page posts back (CD-94) and
// its restricted values per dimension, each sorted, read together. Users may
// read themselves; anyone else needs an Administrator.
func (a *Auth) UserForEdit(ctx context.Context, actor Principal, id UserID) (UserInfo, int64, map[Dimension][]int64, error) {
	if actor.ID != id && !isAdmin(actor) {
		return UserInfo{}, 0, nil, ErrAdminOnlyField
	}
	var (
		u   dbq.GetUserRow
		out map[Dimension][]int64
	)
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		var err error
		if u, err = getUser(ctx, q, id); err != nil {
			return err
		}
		out, err = restrictions(ctx, q, id)
		return err
	})
	if err != nil {
		return UserInfo{}, 0, nil, err
	}
	return UserInfo{ID: id, Email: u.Email, Name: u.Name, Enabled: u.Enabled != 0}, u.Version, out, nil
}

// UserAccess returns user id, its version (CD-94) and its restricted values
// per dimension with their labels, read together (CD-146). Only an
// Administrator may read it (ErrAdminOnlyAccess); an unknown id is
// ErrNotFound.
func (a *Auth) UserAccess(ctx context.Context, actor Principal, id UserID) (UserInfo, int64, map[Dimension][]Restriction, error) {
	if !isAdmin(actor) {
		return UserInfo{}, 0, nil, ErrAdminOnlyAccess
	}
	var (
		u   dbq.GetUserRow
		out map[Dimension][]Restriction
	)
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		if u, err = getUser(ctx, dbq.New(tx.SQL()), id); err != nil {
			return err
		}
		out, err = a.restrictionList(ctx, tx, id)
		return err
	})
	if err != nil {
		return UserInfo{}, 0, nil, err
	}
	return UserInfo{ID: id, Email: u.Email, Name: u.Name, Enabled: u.Enabled != 0}, u.Version, out, nil
}

// UserRestrictions returns u's restricted values per dimension, each sorted.
func (a *Auth) UserRestrictions(ctx context.Context, u UserID) (map[Dimension][]int64, error) {
	var out map[Dimension][]int64
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		out, err = restrictions(ctx, dbq.New(tx.SQL()), u)
		return err
	})
	return out, err
}

// ChangeRestrictions adds and removes values of user id on dimension dim
// (project or site). Only an Administrator may do it. The set is read and
// written in one write transaction, refused when version is set and the user
// changed since (CD-94); a change bumps the user's version and perm_version
// and is audited.
func (a *Auth) ChangeRestrictions(ctx context.Context, actor Principal, id UserID, dim Dimension, add, remove []int64, version *int64) error {
	if !isAdmin(actor) {
		return ErrAdminOnlyAccess
	}
	if ValidDimension(string(dim)) != nil {
		return ErrBadForm
	}
	return a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		u, err := getUser(ctx, q, id)
		if err != nil {
			return err
		}
		if version != nil && u.Version != *version {
			return UserChangedError(u.Name)
		}
		before, err := q.ListUserRestrictions(ctx, dbq.ListUserRestrictionsParams{UserID: int64(id), Dimension: string(dim)})
		if err != nil {
			return err
		}
		after := slices.DeleteFunc(slices.Clone(before), func(v int64) bool { return slices.Contains(remove, v) })
		after = sortedIDs(append(after, add...))
		if slices.Equal(before, after) {
			return nil
		}
		if err := setRestrictions(ctx, q, id, dim, after); err != nil {
			return err
		}
		b, err := a.codesText(ctx, tx, dim, before)
		if err != nil {
			return err
		}
		af, err := a.codesText(ctx, tx, dim, after)
		if err != nil {
			return err
		}
		now := a.clock.Now()
		if err := q.TouchUser(ctx, dbq.TouchUserParams{At: FormatTime(now), By: byArg(actor.ID), ID: int64(id)}); err != nil {
			return err
		}
		if err := q.BumpPermVersion(ctx); err != nil {
			return err
		}
		return a.audit.Append(ctx, tx, audit.Entry{
			DocType: "User", DocID: int64(id), Action: "update",
			Changes: []audit.Change{{Field: string(dim) + "s", Before: b, After: af, Op: "set"}},
			By:      actor.ID, At: now,
		})
	})
}

// codesText shows ids of dimension d as "[CODE, CODE]" in ID order, with the
// codes looked up through Directory inside tx; an ID without a code shows as
// its number.
func (a *Auth) codesText(ctx context.Context, tx *db.Tx, d Dimension, ids []int64) (string, error) {
	codesFor := a.dir.SiteCodes
	if d == DimProject {
		codesFor = a.dir.ProjectCodes
	}
	m, err := codesFor(ctx, tx, ids)
	if err != nil {
		return "", err
	}
	s := make([]string, len(ids))
	for i, id := range ids {
		if s[i] = m[id]; s[i] == "" {
			s[i] = strconv.FormatInt(id, 10)
		}
	}
	return "[" + strings.Join(s, ", ") + "]", nil
}
