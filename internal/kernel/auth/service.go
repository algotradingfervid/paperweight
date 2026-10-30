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
	return UserInfo{ID: u, Email: row.Email, Name: row.Name, Enabled: row.Enabled != 0, Version: int(row.Version)}, true, nil
}

// UserByEmail is User by normalised email; a disabled user is returned for
// the caller to judge (CD-199).
func (a *Auth) UserByEmail(ctx context.Context, tx *db.Tx, email string) (UserInfo, bool, error) {
	id, err := dbq.New(tx.SQL()).UserIDByEmail(ctx, NormalizeEmail(email))
	if errors.Is(err, sql.ErrNoRows) {
		return UserInfo{}, false, nil
	}
	if err != nil {
		return UserInfo{}, false, err
	}
	return a.User(ctx, tx, UserID(id))
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
	acc := Access{User: p.ID, Read: readScope, Right: rScope}
	if readScope != "" && rec != nil {
		if err := a.db.Read(ctx, func(tx *db.Tx) error {
			var err error
			if dt.EmployeeField != "" && rec.EmployeeID != 0 {
				if acc.Employee, acc.Reports, acc.HasEmployee, err = a.employee(ctx, tx, p.ID); err != nil {
					return err
				}
			}
			acc.Allowed, err = restrictions(ctx, dbq.New(tx.SQL()), p.ID)
			return err
		}); err != nil {
			return err
		}
	}
	return acc.Can(dt, r, rec)
}

// Access is what Can and Scope decide from once the grants, the employee
// and the restrictions are read. authfake fills it from its seeds and calls
// the same methods, so the two can't drift (CD-182).
type Access struct {
	User        UserID
	Read, Right Scope   // widest grants for read and for the right asked; "" = none
	Employee    int64   // User's employee, when HasEmployee
	Reports     []int64 // that employee's active direct reports
	HasEmployee bool
	Allowed     map[Dimension][]int64 // restrictions; an absent dimension is unrestricted
}

// Can is auth.Service.Can after the Applies and Administrator checks: a
// record outside read scope or the restrictions is ErrNotFound; a missing
// right, or a record outside its scope, is a *DeniedError.
func (a Access) Can(dt *doctype.DocType, r Right, rec *RecordRef) error {
	if a.Read == "" {
		return denied(dt, r)
	}
	if rec == nil {
		if a.Right == "" {
			return denied(dt, r)
		}
		return nil
	}
	isOwn := rec.CreatedBy == a.User
	var isTeam bool
	if dt.EmployeeField != "" && rec.EmployeeID != 0 && a.HasEmployee {
		isOwn = isOwn || rec.EmployeeID == a.Employee
		isTeam = slices.Contains(a.Reports, rec.EmployeeID)
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
	if !inScope(a.Read) || !recordAllowed(dt, rec, a.Allowed) {
		return ErrNotFound
	}
	if a.Right == "" {
		return denied(dt, r)
	}
	if r != RightCreate && !inScope(a.Right) {
		e := denied(dt, r)
		e.OwnOnly, e.TeamOnly = a.Right == ScopeOwn, a.Right == ScopeTeam
		return e
	}
	return nil
}

// employee returns u's employee and that employee's active direct reports,
// read from the Directory on every call (never cached); found is false when u
// has no employee.
func (a *Auth) employee(ctx context.Context, tx *db.Tx, u UserID) (me int64, reports []int64, found bool, err error) {
	me, found, err = a.dir.EmployeeForUser(ctx, tx, u)
	if err != nil || !found {
		return 0, nil, false, err
	}
	reports, err = a.dir.DirectReports(ctx, tx, me)
	if err != nil {
		return 0, nil, false, err
	}
	return me, reports, true, nil
}

// recordAllowed applies the restrictions strictly: on each restricted
// dimension dt declares, rec needs at least one value and every value allowed.
func recordAllowed(dt *doctype.DocType, rec *RecordRef, allowed map[Dimension][]int64) bool {
	for _, d := range []Dimension{DimProject, DimSite} {
		allowedIDs, restricted := allowed[d]
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
			if !slices.Contains(allowedIDs, id) {
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
		if errors.Is(err, ErrNotFound) {
			continue // deleted since the list was read
		}
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
			allowedIDs, restricted := allowed[d]
			if !restricted {
				continue
			}
			for _, f := range dimFields(dt, d) {
				for _, v := range values[f] {
					if !slices.Contains(allowedIDs, v) {
						return a.valueError(ctx, tx, d, allowedIDs)
					}
				}
			}
		}
		return nil
	})
}

// codes returns the Directory's codes for d's value IDs.
func (a *Auth) codes(ctx context.Context, tx *db.Tx, d Dimension, ids []int64) (map[int64]string, error) {
	if d == DimProject {
		return a.dir.ProjectCodes(ctx, tx, ids)
	}
	return a.dir.SiteCodes(ctx, tx, ids)
}

func (a *Auth) valueError(ctx context.Context, tx *db.Tx, d Dimension, ids []int64) error {
	m, err := a.codes(ctx, tx, d, ids)
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

// AllowedRights reports, for each right that applies to doctype, whether Can
// allows it for rec (the UI's buttons, AUTH-084).
func (a *Auth) AllowedRights(ctx context.Context, p Principal, doctypeName string, rec *RecordRef) (map[Right]bool, error) {
	dt, err := lookup(doctypeName)
	if err != nil {
		return nil, err
	}
	out := map[Right]bool{}
	for _, r := range AllRights {
		if !Applies(dt, r) {
			continue
		}
		err := a.Can(ctx, p, dt.Name, r, rec)
		switch {
		case err == nil:
			out[r] = true
		case errors.Is(err, ErrForbidden), errors.Is(err, ErrNotFound):
			out[r] = false
		default:
			return nil, err
		}
	}
	return out, nil
}

// DefaultValue returns p's only allowed value on dimension dim; ok is false
// unless exactly one value is allowed (AUTH-100).
func (a *Auth) DefaultValue(ctx context.Context, p Principal, dim Dimension) (int64, bool, error) {
	var ids []int64
	if err := a.db.Read(ctx, func(tx *db.Tx) error {
		allowed, err := restrictions(ctx, dbq.New(tx.SQL()), p.ID)
		ids = allowed[dim]
		return err
	}); err != nil {
		return 0, false, err
	}
	if len(ids) != 1 {
		return 0, false, nil
	}
	return ids[0], true, nil
}

// RestrictionLabels returns u's allowed codes per restricted dimension, with
// " (disabled)" on values the dimension's master reports disabled (AUTH-105).
func (a *Auth) RestrictionLabels(ctx context.Context, u UserID) (map[Dimension][]string, error) {
	var out map[Dimension][]string
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		out, err = a.restrictionLabels(ctx, tx, u)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// restrictionLabels is RestrictionLabels inside the caller's transaction.
func (a *Auth) restrictionLabels(ctx context.Context, tx *db.Tx, u UserID) (map[Dimension][]string, error) {
	out := map[Dimension][]string{}
	allowed, err := restrictions(ctx, dbq.New(tx.SQL()), u)
	if err != nil {
		return nil, err
	}
	for d, ids := range allowed {
		codes, err := a.codes(ctx, tx, d, ids)
		if err != nil {
			return nil, err
		}
		master, hasMaster := masterFor(d)
		for _, id := range ids {
			label, ok := codes[id]
			if !ok {
				continue
			}
			if hasMaster {
				info, found, err := master.Master.Lookup(ctx, tx, id)
				if err != nil {
					return nil, err
				}
				if found && info.Disabled {
					label += " (disabled)"
				}
			}
			out[d] = append(out[d], label)
		}
	}
	return out, nil
}
