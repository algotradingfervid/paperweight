package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"sync"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// ValidateRow checks one role × DocType row: inapplicable rights first (in
// AllRights order), then team scope (CD-11), then read-required.
func ValidateRow(dt *doctype.DocType, rights map[Right]Scope) error {
	for _, r := range AllRights {
		if _, ok := rights[r]; ok && !Applies(dt, r) {
			return NotApplicableError(r, dt.Label)
		}
	}
	if dt.EmployeeField == "" {
		for _, s := range rights {
			if s == ScopeTeam {
				return TeamNotApplicableError(dt.Label)
			}
		}
	}
	if _, ok := rights[RightRead]; !ok && len(rights) > 0 {
		return ReadRequiredError(dt.Label)
	}
	return nil
}

// SeedOptions controls SeedDefaults. LinkedMasterRead (production, CD-10)
// also grants read (all) on every master a creatable DocType links to.
type SeedOptions struct{ LinkedMasterRead bool }

// SeedDefaults writes each registered DocType's DefaultRights for every
// (role, DocType) that has no row yet, then bumps perm_version. Rows that
// exist (seeded or edited) are left alone. Only the first seed creates
// missing roles (auth_meta.seeded); later seeds add rows only for roles that
// exist, so a role an administrator deleted stays deleted.
func (a *Auth) SeedDefaults(ctx context.Context, opts SeedOptions) error {
	return a.db.Write(ctx, func(tx *db.Tx) error {
		return a.seed(ctx, dbq.New(tx.SQL()), opts, "")
	})
}

// seed is SeedDefaults inside the caller's write transaction. A non-empty
// only limits the rows to that DocType and never creates a role
// (ResetRights).
func (a *Auth) seed(ctx context.Context, q *dbq.Queries, opts SeedOptions, only string) error {
	seeded, err := q.GetSeeded(ctx)
	if err != nil {
		return err
	}
	create := seeded == 0 && only == ""
	type creator struct {
		role string
		dt   *doctype.DocType
	}
	var creators []creator
	for _, dt := range doctype.All() {
		for _, role := range slices.Sorted(maps.Keys(dt.DefaultRights)) {
			rights, err := parseDefaults(dt.DefaultRights[role])
			if err == nil {
				err = ValidateRow(dt, rights)
			}
			if err != nil {
				return fmt.Errorf("auth.SeedDefaults: DocType %s, role %s: %w", dt.Name, role, err)
			}
			if only == "" || dt.Name == only {
				if err := a.seedRow(ctx, q, role, dt.Name, rights, create); err != nil {
					return err
				}
			}
			if _, ok := rights[RightCreate]; ok {
				creators = append(creators, creator{role, dt})
			}
		}
	}
	if opts.LinkedMasterRead {
		for _, c := range creators {
			for _, m := range linkedMasters(c.dt) {
				if only != "" && m != only {
					continue
				}
				if err := a.seedRow(ctx, q, c.role, m, map[Right]Scope{RightRead: ScopeAll}, create); err != nil {
					return err
				}
			}
		}
	}
	if create {
		if err := q.SetSeeded(ctx); err != nil {
			return err
		}
	}
	return q.BumpPermVersion(ctx)
}

// parseDefaults turns DefaultRights strings into rights and scopes.
func parseDefaults(in map[string]string) (map[Right]Scope, error) {
	out := make(map[Right]Scope, len(in))
	for r, s := range in {
		if !slices.Contains(AllRights, Right(r)) {
			return nil, fmt.Errorf("unknown right %q", r)
		}
		if _, ok := scopeRank[Scope(s)]; !ok {
			return nil, fmt.Errorf("unknown scope %q for right %s", s, r)
		}
		out[Right(r)] = Scope(s)
	}
	return out, nil
}

// linkedMasters returns the names of the masters dt links to, sorted.
func linkedMasters(dt *doctype.DocType) []string {
	var targets []string
	for _, f := range dt.Fields {
		if f.Link != "" {
			targets = append(targets, f.Link)
		}
	}
	for _, l := range dt.Links {
		targets = append(targets, l.Target)
	}
	var out []string
	for _, t := range targets {
		if m, ok := doctype.Lookup(t); ok && m.Master != nil && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

// seedRow inserts role's row on dt (customised 0) only if none exists. A
// missing role is created when create is set and skipped otherwise.
func (a *Auth) seedRow(ctx context.Context, q *dbq.Queries, role, dt string, rights map[Right]Scope, create bool) error {
	r, err := q.GetRoleByName(ctx, role)
	var id int64
	switch {
	case errors.Is(err, sql.ErrNoRows) && !create:
		return nil
	case errors.Is(err, sql.ErrNoRows):
		at := FormatTime(a.clock.Now())
		if id, err = q.InsertRole(ctx, dbq.InsertRoleParams{Name: role, At: at}); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		id = r.ID
	}
	n, err := q.CountRoleDocType(ctx, dbq.CountRoleDocTypeParams{RoleID: id, Doctype: dt})
	if err != nil || n > 0 {
		return err
	}
	if err := q.InsertRoleDocType(ctx, dbq.InsertRoleDocTypeParams{RoleID: id, Doctype: dt}); err != nil {
		return err
	}
	return insertRights(ctx, q, id, dt, rights)
}

func insertRights(ctx context.Context, q *dbq.Queries, roleID int64, dt string, rights map[Right]Scope) error {
	for _, r := range AllRights {
		if s, ok := rights[r]; ok {
			if err := q.InsertRoleRight(ctx, dbq.InsertRoleRightParams{
				RoleID: roleID, Doctype: dt, RightName: string(r), Scope: string(s),
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// SetRights replaces role's rights on doctype, marks the row customised,
// bumps the role's version and perm_version and writes one audit entry; a
// save that changes nothing writes nothing. Only an Administrator may do
// it, and the Administrator role itself is fixed.
func (a *Auth) SetRights(ctx context.Context, actor Principal, role, doctypeName string, rights map[Right]Scope) error {
	return a.setRights(ctx, actor, roleRef{name: role}, doctypeName, rights)
}

// SetRightsID is SetRights for role roleID at the posted version.
func (a *Auth) SetRightsID(ctx context.Context, actor Principal, roleID int64, doctypeName string, rights map[Right]Scope, version int64) error {
	return a.setRights(ctx, actor, roleRef{id: roleID, version: &version}, doctypeName, rights)
}

func (a *Auth) setRights(ctx context.Context, actor Principal, ref roleRef, doctypeName string, rights map[Right]Scope) error {
	if !isAdmin(actor) {
		return ErrAdminOnlyRoles
	}
	dt, ok := doctype.Lookup(doctypeName)
	if !ok {
		return ErrNotFound
	}
	for rt := range rights {
		if !slices.Contains(AllRights, rt) {
			return ErrBadForm
		}
	}
	for _, rt := range AllRights {
		if s, ok := rights[rt]; ok && scopeRank[s] == 0 {
			return ScopeInvalidError(rt)
		}
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
		if err := ValidateRow(dt, rights); err != nil {
			return err
		}
		before, err := roleRights(ctx, q, r.Name, doctypeName)
		if err != nil || maps.Equal(before, rights) {
			return err
		}
		if err := replaceRights(ctx, q, r.ID, doctypeName, rights); err != nil {
			return err
		}
		return a.recordRights(ctx, tx, q, actor, r.ID, rightsChanges(doctypeName, before, rights))
	})
}

// CopyRightsID replaces role targetID's rights, at the posted version, with
// role sourceID's on every registered DocType: a DocType where the source has
// none is cleared, and one that doesn't change is untouched. The source is
// read inside the write and its version isn't posted (CD-94, CD-200). One
// audit entry covers every changed DocType; a copy that changes nothing
// writes nothing.
func (a *Auth) CopyRightsID(ctx context.Context, actor Principal, targetID, sourceID, version int64) error {
	if !isAdmin(actor) {
		return ErrAdminOnlyRoles
	}
	return a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		target, source, err := copyRoles(ctx, q, roleRef{id: targetID, version: &version}, sourceID)
		if err != nil {
			return err
		}
		var changes []audit.Change
		for _, dt := range doctype.All() {
			before, err := roleRights(ctx, q, target.Name, dt.Name)
			if err != nil {
				return err
			}
			rights, err := roleRights(ctx, q, source.Name, dt.Name)
			if err != nil {
				return err
			}
			if maps.Equal(before, rights) {
				continue
			}
			if err := ValidateRow(dt, rights); err != nil {
				return err
			}
			if err := replaceRights(ctx, q, target.ID, dt.Name, rights); err != nil {
				return err
			}
			changes = append(changes, rightsChanges(dt.Name, before, rights)...)
		}
		if len(changes) == 0 {
			return nil
		}
		return a.recordRights(ctx, tx, q, actor, target.ID, changes)
	})
}

// copyRoles loads and checks both roles of a copy, the target first.
func copyRoles(ctx context.Context, q *dbq.Queries, ref roleRef, sourceID int64) (target, source dbq.Role, err error) {
	if target, err = ref.load(ctx, q); err != nil {
		return target, source, err
	}
	if target.IsAdmin != 0 {
		return target, source, ErrAdminRoleFixed
	}
	if sourceID == target.ID {
		return target, source, ErrCopySourceSame
	}
	source, err = roleRef{id: sourceID}.load(ctx, q)
	if errors.Is(err, ErrNotFound) {
		return target, source, ErrCopySourceMissing
	}
	if err == nil && source.IsAdmin != 0 {
		err = ErrCopySourceAdmin
	}
	return target, source, err
}

// replaceRights makes rights the role's whole row on doctypeName and marks
// the row customised.
func replaceRights(ctx context.Context, q *dbq.Queries, roleID int64, doctypeName string, rights map[Right]Scope) error {
	if err := q.MarkRoleDocTypeCustomised(ctx, dbq.MarkRoleDocTypeCustomisedParams{RoleID: roleID, Doctype: doctypeName}); err != nil {
		return err
	}
	if err := q.DeleteRoleRights(ctx, dbq.DeleteRoleRightsParams{RoleID: roleID, Doctype: doctypeName}); err != nil {
		return err
	}
	return insertRights(ctx, q, roleID, doctypeName, rights)
}

// recordRights bumps the role's version and perm_version once and writes the
// one audit entry of a rights change.
func (a *Auth) recordRights(ctx context.Context, tx *db.Tx, q *dbq.Queries, actor Principal, roleID int64, changes []audit.Change) error {
	now := a.clock.Now()
	if err := q.BumpRoleVersion(ctx, dbq.BumpRoleVersionParams{At: FormatTime(now), ID: roleID}); err != nil {
		return err
	}
	if err := q.BumpPermVersion(ctx); err != nil {
		return err
	}
	return a.audit.Append(ctx, tx, audit.Entry{
		DocType: "Role", DocID: roleID, Action: "rights", Changes: changes,
		By: actor.ID, At: now,
	})
}

// rightsChanges is one audit change per right whose scope on doctypeName
// differs between before and after.
func rightsChanges(doctypeName string, before, after map[Right]Scope) []audit.Change {
	var changes []audit.Change
	for _, rt := range AllRights {
		b, af := scopeText(before[rt]), scopeText(after[rt])
		if b != af {
			changes = append(changes, audit.Change{Field: doctypeName + "." + string(rt), Before: b, After: af, Op: "set"})
		}
	}
	return changes
}

func scopeText(s Scope) string {
	if s == "" {
		return "none"
	}
	return string(s)
}

// RoleRights returns role's rights on doctype (empty when it has none).
func (a *Auth) RoleRights(ctx context.Context, role, doctypeName string) (map[Right]Scope, error) {
	var out map[Right]Scope
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		out, err = roleRights(ctx, dbq.New(tx.SQL()), role, doctypeName)
		return err
	})
	return out, err
}

func roleRights(ctx context.Context, q *dbq.Queries, role, doctypeName string) (map[Right]Scope, error) {
	rows, err := q.ListRoleRightsByName(ctx, dbq.ListRoleRightsByNameParams{Role: role, Doctype: doctypeName})
	if err != nil {
		return nil, err
	}
	out := make(map[Right]Scope, len(rows))
	for _, row := range rows {
		out[Right(row.RightName)] = Scope(row.Scope)
	}
	return out, nil
}

// matrix is role name → DocType → right → scope.
type matrix map[string]map[string]map[Right]Scope

// matrixCache holds the whole matrix for one perm_version.
type matrixCache struct {
	mu      sync.Mutex
	version int64
	m       matrix
}

// grant returns the widest scope any of roles holds for r on doctype ("" =
// none), read in tx. The matrix is cached by perm_version, read once per call.
func (a *Auth) grant(ctx context.Context, tx *db.Tx, roles []string, doctypeName string, r Right) (Scope, error) {
	m, err := a.loadMatrix(ctx, tx)
	if err != nil {
		return "", err
	}
	var s Scope
	for _, role := range roles {
		s = Wider(s, m[role][doctypeName][r])
	}
	return s, nil
}

func (a *Auth) loadMatrix(ctx context.Context, tx *db.Tx) (matrix, error) {
	q := dbq.New(tx.SQL())
	v, err := q.GetPermVersion(ctx)
	if err != nil {
		return nil, err
	}
	a.cache.mu.Lock()
	var m matrix
	if a.cache.m != nil && a.cache.version == v {
		m = a.cache.m
	}
	a.cache.mu.Unlock()
	if m != nil {
		return m, nil
	}
	rows, err := q.ListAllRoleRights(ctx)
	if err != nil {
		return nil, err
	}
	m = matrix{}
	for _, row := range rows {
		if m[row.Role] == nil {
			m[row.Role] = map[string]map[Right]Scope{}
		}
		if m[row.Role][row.Doctype] == nil {
			m[row.Role][row.Doctype] = map[Right]Scope{}
		}
		m[row.Role][row.Doctype][Right(row.RightName)] = Scope(row.Scope)
	}
	// A writable tx may hold a perm_version bump that rolls back; the next
	// committed change reuses that number, so an entry stored here would
	// answer for it with the wrong grants.
	if !tx.Writable() {
		a.cache.mu.Lock()
		a.cache.version, a.cache.m = v, m
		a.cache.mu.Unlock()
	}
	return m, nil
}
