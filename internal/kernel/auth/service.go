package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
)

var _ Service = (*Auth)(nil)

// UserState is the account state loaded with a Principal.
type UserState struct {
	Email                       string
	Enabled, MustChangePassword bool
}

// LoadPrincipal returns u's principal (roles sorted) and account state;
// ErrNotFound if the user does not exist.
func (a *Auth) LoadPrincipal(ctx context.Context, u UserID) (Principal, UserState, error) {
	var p Principal
	var st UserState
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		row, err := q.GetPrincipalUser(ctx, int64(u))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		roles, err := q.ListPrincipalRoles(ctx, int64(u))
		if err != nil {
			return err
		}
		slices.Sort(roles)
		p = Principal{ID: u, Name: row.Name, Roles: roles}
		st = UserState{Email: row.Email, Enabled: row.Enabled != 0, MustChangePassword: row.MustChangePassword != 0}
		return nil
	})
	return p, st, err
}

// User returns u's info; ok is false with a nil error when u does not exist
// (CD-12).
func (a *Auth) User(ctx context.Context, tx *db.Tx, u UserID) (UserInfo, bool, error) {
	row, err := dbq.New(tx.SQL()).GetPrincipalUser(ctx, int64(u))
	if errors.Is(err, sql.ErrNoRows) {
		return UserInfo{}, false, nil
	}
	if err != nil {
		return UserInfo{}, false, err
	}
	return UserInfo{ID: u, Email: row.Email, Name: row.Name, Enabled: row.Enabled != 0}, true, nil
}

// HasRole reports whether u holds role.
func (a *Auth) HasRole(ctx context.Context, u UserID, role string) (bool, error) {
	var n int64
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		n, err = dbq.New(tx.SQL()).CountUserRole(ctx, dbq.CountUserRoleParams{UserID: int64(u), Role: role})
		return err
	})
	return n > 0, err
}

// restrictions returns u's allowed value IDs per dimension (absent = no
// restriction).
func restrictions(ctx context.Context, q *dbq.Queries, u UserID) (map[Dimension][]int64, error) {
	rows, err := q.ListPrincipalRestrictions(ctx, int64(u))
	if err != nil {
		return nil, err
	}
	out := map[Dimension][]int64{}
	for _, r := range rows {
		out[Dimension(r.Dimension)] = append(out[Dimension(r.Dimension)], r.ValueID)
	}
	return out, nil
}

func lookup(name string) (*doctype.DocType, error) {
	dt, ok := doctype.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("auth: unknown DocType %q", name)
	}
	return dt, nil
}

func denied(dt *doctype.DocType, r Right) *DeniedError {
	return &DeniedError{Right: r, Label: dt.Label, Plural: dt.PluralLabel}
}

// Can reports whether p may use r on doctype, for rec or (rec nil) at all.
// A record p can't see is ErrNotFound (404); a missing right is a
// *DeniedError (403).
func (a *Auth) Can(ctx context.Context, p Principal, doctypeName string, r Right, rec *RecordRef) error {
	dt, err := lookup(doctypeName)
	if err != nil {
		return err
	}
	if !Applies(dt, r) {
		return denied(dt, r)
	}
	if slices.Contains(p.Roles, RoleAdministrator) {
		return nil
	}
	readScope, err := a.grant(ctx, p.Roles, dt.Name, RightRead)
	if err != nil {
		return err
	}
	rScope, err := a.grant(ctx, p.Roles, dt.Name, r)
	if err != nil {
		return err
	}
	if readScope == "" {
		return denied(dt, r)
	}
	if rec == nil {
		if rScope == "" {
			return denied(dt, r)
		}
		return nil
	}
	var isOwn, isTeam, restrictionsOK bool
	if err := a.db.Read(ctx, func(tx *db.Tx) error {
		isOwn = rec.CreatedBy == p.ID
		if dt.EmployeeField != "" && rec.EmployeeID != 0 {
			me, found, err := a.dir.EmployeeForUser(ctx, tx, p.ID)
			if err != nil {
				return err
			}
			if found {
				isOwn = isOwn || rec.EmployeeID == me
				reports, err := a.dir.DirectReports(ctx, tx, me)
				if err != nil {
					return err
				}
				isTeam = slices.Contains(reports, rec.EmployeeID)
			}
		}
		allowed, err := restrictions(ctx, dbq.New(tx.SQL()), p.ID)
		if err != nil {
			return err
		}
		restrictionsOK = recordAllowed(dt, rec, allowed)
		return nil
	}); err != nil {
		return err
	}
	inScope := func(s Scope) bool {
		switch s {
		case ScopeAll:
			return true
		case ScopeOwn:
			return isOwn
		case ScopeTeam:
			return isOwn || isTeam
		}
		return false
	}
	if !inScope(readScope) || !restrictionsOK {
		return ErrNotFound
	}
	if rScope == "" {
		return denied(dt, r)
	}
	if r != RightCreate && !inScope(rScope) {
		e := denied(dt, r)
		e.OwnOnly, e.TeamOnly = rScope == ScopeOwn, rScope == ScopeTeam
		return e
	}
	return nil
}

// recordAllowed applies the restrictions strictly: on each restricted
// dimension dt declares, rec needs at least one value and every value allowed.
func recordAllowed(dt *doctype.DocType, rec *RecordRef, allowed map[Dimension][]int64) bool {
	for _, d := range []Dimension{DimProject, DimSite} {
		ok, restricted := allowed[d]
		if !restricted || len(dimFields(dt, d)) == 0 {
			continue
		}
		ids := rec.SiteIDs
		if d == DimProject {
			ids = rec.ProjectIDs
		}
		if len(ids) == 0 {
			return false
		}
		for _, id := range ids {
			if !slices.Contains(ok, id) {
				return false
			}
		}
	}
	return true
}

// UsersWithRight returns the enabled users for whom Can is nil, ascending ID.
func (a *Auth) UsersWithRight(ctx context.Context, doctypeName string, r Right, rec *RecordRef) ([]UserID, error) {
	var ids []int64
	if err := a.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		ids, err = dbq.New(tx.SQL()).ListEnabledUserIDs(ctx)
		return err
	}); err != nil {
		return nil, err
	}
	var out []UserID
	for _, id := range ids {
		p, _, err := a.LoadPrincipal(ctx, UserID(id))
		if err != nil {
			return nil, err
		}
		err = a.Can(ctx, p, doctypeName, r, rec)
		switch {
		case err == nil:
			out = append(out, p.ID)
		case errors.Is(err, ErrForbidden), errors.Is(err, ErrNotFound):
		default:
			return nil, err
		}
	}
	return out, nil
}

// CheckValues checks proposed Project and Site IDs, keyed by the DocType's
// ProjectFields/SiteFields entries, against p's restrictions (CD-12).
func (a *Auth) CheckValues(ctx context.Context, p Principal, doctypeName string, values map[string][]int64) error {
	dt, err := lookup(doctypeName)
	if err != nil {
		return err
	}
	return a.db.Read(ctx, func(tx *db.Tx) error {
		allowed, err := restrictions(ctx, dbq.New(tx.SQL()), p.ID)
		if err != nil {
			return err
		}
		for _, d := range []Dimension{DimProject, DimSite} {
			ok, restricted := allowed[d]
			if !restricted {
				continue
			}
			for _, f := range dimFields(dt, d) {
				for _, v := range values[f] {
					if !slices.Contains(ok, v) {
						return a.valueError(ctx, tx, d, ok)
					}
				}
			}
		}
		return nil
	})
}

func (a *Auth) valueError(ctx context.Context, tx *db.Tx, d Dimension, ids []int64) error {
	codesFor := a.dir.SiteCodes
	if d == DimProject {
		codesFor = a.dir.ProjectCodes
	}
	m, err := codesFor(ctx, tx, ids)
	if err != nil {
		return err
	}
	codes := make([]string, 0, len(m))
	for _, c := range m {
		codes = append(codes, c)
	}
	slices.Sort(codes)
	return &ValueError{Dim: d, Allowed: codes}
}

// CheckLink reports whether p may link to target record id (CD-12): missing
// is ErrNotFound; not readable is a *LinkError.
func (a *Auth) CheckLink(ctx context.Context, p Principal, target string, id int64) error {
	var info doctype.Info
	if err := a.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		info, err = a.info.Info(ctx, tx, target, id)
		return err
	}); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	ref := RecordRef{
		DocType: target, ID: id, CreatedBy: info.CreatedBy, EmployeeID: info.EmployeeID,
		ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs,
	}
	err := a.Can(ctx, p, target, RightRead, &ref)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrNotFound):
		return &LinkError{Label: info.DocLabel, Display: info.Display}
	}
	return err
}

// ReadableDocTypes returns the registered DocTypes p may read (navigation).
func (a *Auth) ReadableDocTypes(ctx context.Context, p Principal) ([]string, error) {
	var out []string
	for _, dt := range doctype.All() {
		err := a.Can(ctx, p, dt.Name, RightRead, nil)
		switch {
		case err == nil:
			out = append(out, dt.Name)
		case !errors.Is(err, ErrForbidden):
			return nil, err
		}
	}
	return out, nil
}

// Scope is built in Task 6.
func (a *Auth) Scope(ctx context.Context, p Principal, doctypeName string, r Right) (Predicate, error) {
	return Predicate{}, errs.ErrNotImplemented
}

// --- BEGIN Task 7 stubs: delete on merge with Task 7's users.go ---

// DisableUser is built in Task 7.
func (a *Auth) DisableUser(ctx context.Context, tx *db.Tx, u UserID, reason string) error {
	return errs.ErrNotImplemented
}

// EnableUser is built in Task 7.
func (a *Auth) EnableUser(ctx context.Context, tx *db.Tx, u UserID) error {
	return errs.ErrNotImplemented
}

// --- END Task 7 stubs ---
