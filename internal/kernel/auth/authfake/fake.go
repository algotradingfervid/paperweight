// Package authfake is an in-memory auth.Service and auth.Directory for tests.
package authfake

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

var _ auth.Service = (*Fake)(nil)

type docRight struct {
	doctype string
	right   auth.Right
}

type userDocRight struct {
	user auth.UserID
	docRight
}

type grant struct {
	role string
	docRight
}

type record struct {
	doctype string
	id      int64
}

type restriction struct{ projects, sites []int64 } // nil = unrestricted

// Fake implements auth.Service. It denies everything until rights are seeded.
type Fake struct {
	mu         sync.Mutex
	allowed    map[userDocRight]bool // true = Allow (scope all), false = Deny
	grants     map[grant]auth.Scope
	added      map[auth.UserID]map[string]bool // roles from AddRole
	removed    map[auth.UserID]map[string]bool // roles taken away with RemoveRole
	restricted map[auth.UserID]restriction
	hidden     map[record]bool
	scopes     map[string]auth.Predicate
	users      map[docRight][]auth.UserID
	infos      map[auth.UserID]auth.UserInfo
	dir        auth.Directory
}

func New() *Fake {
	return &Fake{
		allowed:    map[userDocRight]bool{},
		grants:     map[grant]auth.Scope{},
		added:      map[auth.UserID]map[string]bool{},
		removed:    map[auth.UserID]map[string]bool{},
		restricted: map[auth.UserID]restriction{},
		hidden:     map[record]bool{},
		scopes:     map[string]auth.Predicate{},
		users:      map[docRight][]auth.UserID{},
		infos:      map[auth.UserID]auth.UserInfo{},
	}
}

// denied is the real service's 403 (CD-157): the registered DocType's
// labels, or its name when it isn't registered.
func denied(name string, r auth.Right) error {
	label, plural := name, name
	if dt, ok := doctype.Lookup(name); ok {
		label, plural = dt.Label, dt.PluralLabel
	}
	return &auth.DeniedError{Right: r, Label: label, Plural: plural}
}

// inapplicable reports whether r doesn't apply to the registered DocType
// name, which the real service refuses before grants (CD-173).
func inapplicable(name string, r auth.Right) bool {
	dt, ok := doctype.Lookup(name)
	return ok && !auth.Applies(dt, r)
}

// Seeding helpers (CD-56, CD-86).

// Grant gives role the right r on doctype with scope s; doctype "*" means every DocType.
func (f *Fake) Grant(role, doctype string, r auth.Right, s auth.Scope) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants[grant{role, docRight{doctype, r}}] = s
}

func (f *Fake) AddRole(u auth.UserID, role string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	setFlag(f.added, u, role, true)
	setFlag(f.removed, u, role, false)
}

// RemoveRole takes role away from u, including a role on the Principal; a no-op when u doesn't hold it.
func (f *Fake) RemoveRole(u auth.UserID, role string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	setFlag(f.added, u, role, false)
	setFlag(f.removed, u, role, true)
}

func setFlag(m map[auth.UserID]map[string]bool, u auth.UserID, role string, on bool) {
	if !on {
		delete(m[u], role)
		return
	}
	if m[u] == nil {
		m[u] = map[string]bool{}
	}
	m[u][role] = true
}

// Restrict limits u to records whose projects and sites are in the lists; a nil list leaves that dimension open.
// A non-nil empty list means "restricted to nothing", a state the real service can't hold.
func (f *Fake) Restrict(u auth.UserID, projects, sites []int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restricted[u] = restriction{projects: slices.Clone(projects), sites: slices.Clone(sites)}
}

// Hide makes the record invisible to everyone: Can on it returns auth.ErrNotFound and CheckLink to it a *auth.LinkError (CD-125).
func (f *Fake) Hide(doctype string, id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hidden[record{doctype, id}] = true
}

// Allow gives u the rights on doctype with scope all.
func (f *Fake) Allow(u auth.UserID, doctype string, rights ...auth.Right) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range rights {
		f.allowed[userDocRight{u, docRight{doctype, r}}] = true
	}
}

// Deny takes the rights on doctype away from u, whatever its roles grant.
func (f *Fake) Deny(u auth.UserID, doctype string, rights ...auth.Right) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range rights {
		f.allowed[userDocRight{u, docRight{doctype, r}}] = false
	}
}

// SetScope sets the Predicate that Scope returns for doctype.
func (f *Fake) SetScope(doctype string, p auth.Predicate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p.Args = slices.Clone(p.Args)
	f.scopes[doctype] = p
}

// SetUsers fixes the list UsersWithRight returns for (doctype, r).
func (f *Fake) SetUsers(doctype string, r auth.Right, users []auth.UserID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[docRight{doctype, r}] = slices.Clone(users)
}

// UseDirectory sets the directory that scope team uses for direct reports.
func (f *Fake) UseDirectory(d auth.Directory) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dir = d
}

func (f *Fake) AddUser(info auth.UserInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.infos[info.ID] = info
}

// rolesLocked returns p's roles: p.Roles plus AddRole roles, minus RemoveRole roles.
func (f *Fake) rolesLocked(p auth.Principal) []string {
	var roles []string
	for _, r := range p.Roles {
		if !f.removed[p.ID][r] {
			roles = append(roles, r)
		}
	}
	for r := range f.added[p.ID] {
		if !slices.Contains(roles, r) {
			roles = append(roles, r)
		}
	}
	return roles
}

var scopeRank = map[auth.Scope]int{auth.ScopeOwn: 1, auth.ScopeTeam: 2, auth.ScopeAll: 3}

// scopeLocked returns the widest scope p has for (doctype, r), or false.
func (f *Fake) scopeLocked(p auth.Principal, doctype string, r auth.Right) (auth.Scope, bool) {
	if allow, set := f.allowed[userDocRight{p.ID, docRight{doctype, r}}]; set {
		return auth.ScopeAll, allow
	}
	var best auth.Scope
	for _, role := range f.rolesLocked(p) {
		for _, dt := range []string{doctype, "*"} {
			if s, ok := f.grants[grant{role, docRight{dt, r}}]; ok && scopeRank[s] > scopeRank[best] {
				best = s
			}
		}
	}
	return best, best != ""
}

func outside(ids, allowed []int64) bool {
	if allowed == nil {
		return false
	}
	for _, id := range ids {
		if !slices.Contains(allowed, id) {
			return true
		}
	}
	return false
}

// docType returns the registered DocType, or for a name that isn't
// registered a stand-in with the name as its labels and no columns, and
// false. Team scope and own-by-employee need a registered DocType with an
// EmployeeField.
func docType(name string) (*doctype.DocType, bool) {
	if dt, ok := doctype.Lookup(name); ok {
		return dt, true
	}
	return &doctype.DocType{Name: name, Label: name, PluralLabel: name}, false
}

// accessLocked returns what the real service decides p's r on name from:
// the read and r grants, the Restrict lists and, with a Directory and
// withEmployee, p's employee and reports. The Administrator holds every
// right unrestricted. The decision itself is auth.Access's (CD-182).
func (f *Fake) accessLocked(p auth.Principal, name string, r auth.Right) auth.Access {
	if slices.Contains(f.rolesLocked(p), auth.RoleAdministrator) {
		return auth.Access{User: p.ID, Read: auth.ScopeAll, Right: auth.ScopeAll}
	}
	grantOf := func(r auth.Right) auth.Scope {
		if s, ok := f.scopeLocked(p, name, r); ok {
			return s
		}
		return ""
	}
	acc := auth.Access{User: p.ID, Read: grantOf(auth.RightRead), Right: grantOf(r), Allowed: map[auth.Dimension][]int64{}}
	limit := f.restricted[p.ID]
	if limit.projects != nil {
		acc.Allowed[auth.DimProject] = slices.Clone(limit.projects)
	}
	if limit.sites != nil {
		acc.Allowed[auth.DimSite] = slices.Clone(limit.sites)
	}
	return acc
}

// withEmployee adds p's employee and direct reports from dir to acc, as the
// real service reads them from its Directory.
func withEmployee(ctx context.Context, dir auth.Directory, acc *auth.Access) error {
	emp, linked, err := dir.EmployeeForUser(ctx, nil, acc.User)
	if err != nil || !linked {
		return err
	}
	reports, err := dir.DirectReports(ctx, nil, emp)
	if err != nil {
		return err
	}
	acc.Employee, acc.Reports, acc.HasEmployee = emp, reports, true
	return nil
}

// Can decides like the real service through auth.Access.Can; a Hide record
// is also not found for anyone who can read the DocType, and so is a record
// of an unregistered DocType with an id outside the Restrict lists.
func (f *Fake) Can(ctx context.Context, p auth.Principal, doctype string, r auth.Right, rec *auth.RecordRef) error {
	if inapplicable(doctype, r) {
		return denied(doctype, r)
	}
	dt, registered := docType(doctype)
	f.mu.Lock()
	acc := f.accessLocked(p, doctype, r)
	hidden := rec != nil && f.hidden[record{doctype, rec.ID}]
	dir := f.dir
	f.mu.Unlock()

	if !registered && rec != nil && (outside(rec.ProjectIDs, acc.Allowed[auth.DimProject]) || outside(rec.SiteIDs, acc.Allowed[auth.DimSite])) {
		hidden = true
	}
	if hidden && acc.Read != "" {
		return auth.ErrNotFound
	}
	if dir != nil && rec != nil && acc.Read != "" && dt.EmployeeField != "" && rec.EmployeeID != 0 {
		if err := withEmployee(ctx, dir, &acc); err != nil {
			return err
		}
	}
	return acc.Can(dt, r, rec)
}

// Scope builds the real service's filter through auth.Access.Scope, or
// returns the SetScope Predicate for doctype (empty for an unregistered
// DocType, which has no table) once the grants allow it.
func (f *Fake) Scope(ctx context.Context, p auth.Principal, doctype string, r auth.Right) (auth.Predicate, error) {
	if inapplicable(doctype, r) {
		return auth.Predicate{}, denied(doctype, r)
	}
	dt, registered := docType(doctype)
	f.mu.Lock()
	acc := f.accessLocked(p, doctype, r)
	set, isSet := f.scopes[doctype]
	isSet = isSet || !registered
	dir := f.dir
	f.mu.Unlock()

	scoped := slices.ContainsFunc([]auth.Scope{acc.Read, acc.Right}, func(s auth.Scope) bool { return s == auth.ScopeOwn || s == auth.ScopeTeam })
	if dir != nil && !isSet && scoped && dt.EmployeeField != "" {
		if err := withEmployee(ctx, dir, &acc); err != nil {
			return auth.Predicate{}, err
		}
	}
	pred, err := acc.Scope(dt, r)
	if err != nil || !isSet {
		return pred, err
	}
	set.Args = slices.Clone(set.Args)
	return set, nil
}

// CheckValues rejects proposed project and site ids outside p's Restrict lists
// with the real service's *auth.ValueError (CD-162).
func (f *Fake) CheckValues(ctx context.Context, p auth.Principal, name string, values map[string][]int64) error {
	dt, ok := doctype.Lookup(name)
	if !ok {
		return nil
	}
	f.mu.Lock()
	limit := f.restricted[p.ID]
	dir := f.dir
	f.mu.Unlock()
	check := func(fields []string, allowed []int64, d auth.Dimension) error {
		for _, field := range fields {
			for _, id := range values[field] {
				if outside([]int64{id}, allowed) {
					return valueError(ctx, dir, d, allowed)
				}
			}
		}
		return nil
	}
	if err := check(dt.ProjectFields, limit.projects, auth.DimProject); err != nil {
		return err
	}
	return check(dt.SiteFields, limit.sites, auth.DimSite)
}

// valueError lists the allowed codes from dir, sorted (CD-149). Without a
// Directory the fake has no codes, so it lists the ids as strings, sorted.
func valueError(ctx context.Context, dir auth.Directory, d auth.Dimension, ids []int64) error {
	var codes []string
	if dir == nil {
		// The ids are sorted below as text, so "10" comes before "9".
		for _, id := range ids {
			codes = append(codes, strconv.FormatInt(id, 10))
		}
	} else {
		codesFor := dir.SiteCodes
		if d == auth.DimProject {
			codesFor = dir.ProjectCodes
		}
		m, err := codesFor(ctx, nil, ids)
		if err != nil {
			return err
		}
		for _, c := range m {
			codes = append(codes, c)
		}
	}
	slices.Sort(codes)
	return &auth.ValueError{Dim: d, Allowed: codes}
}

// CheckLink refuses a Hide record, or a target p can't read (the
// Administrator reads every DocType, as in Can).
func (f *Fake) CheckLink(ctx context.Context, p auth.Principal, target string, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hidden[record{target, id}] || f.accessLocked(p, target, auth.RightRead).Read == "" {
		label := target
		if dt, ok := doctype.Lookup(target); ok {
			label = dt.Label
		}
		return &auth.LinkError{Label: label, Display: fmt.Sprintf("#%d", id)} // CD-125
	}
	return nil
}

// UsersWithRight returns the SetUsers list without disabled users, or like
// the real service every enabled user for whom Can on rec is nil, sorted.
// A right that doesn't apply to the DocType lists nobody (CD-173).
func (f *Fake) UsersWithRight(ctx context.Context, doctype string, r auth.Right, rec *auth.RecordRef) ([]auth.UserID, error) {
	if inapplicable(doctype, r) {
		return nil, nil
	}
	f.mu.Lock()
	candidates, set := f.users[docRight{doctype, r}]
	candidates = slices.Clone(candidates)
	if !set {
		for u := range f.added {
			candidates = append(candidates, u)
		}
		for u := range f.infos {
			candidates = append(candidates, u)
		}
		for k := range f.allowed {
			candidates = append(candidates, k.user)
		}
	}
	var enabled []auth.UserID
	for _, u := range candidates {
		if info, known := f.infos[u]; (!known || info.Enabled) && !slices.Contains(enabled, u) {
			enabled = append(enabled, u)
		}
	}
	f.mu.Unlock()

	var out []auth.UserID
	for _, u := range enabled {
		if !set {
			err := f.Can(ctx, auth.Principal{ID: u}, doctype, r, rec)
			if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
		}
		out = append(out, u)
	}
	slices.Sort(out)
	return out, nil
}

// HasRole reports whether u holds role through AddRole (and not since removed).
func (f *Fake) HasRole(ctx context.Context, u auth.UserID, role string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.added[u][role], nil
}

func (f *Fake) User(ctx context.Context, tx *db.Tx, u auth.UserID) (auth.UserInfo, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.infos[u]
	return info, ok, nil
}

func (f *Fake) DisableUser(ctx context.Context, tx *db.Tx, u auth.UserID, reason string) error {
	return f.setEnabled(u, false)
}

func (f *Fake) EnableUser(ctx context.Context, tx *db.Tx, u auth.UserID) error {
	return f.setEnabled(u, true)
}

func (f *Fake) setEnabled(u auth.UserID, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.infos[u]
	if !ok {
		return auth.ErrNotFound
	}
	info.Enabled = on
	f.infos[u] = info
	return nil
}
