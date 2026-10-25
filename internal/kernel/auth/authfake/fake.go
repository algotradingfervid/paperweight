// Package authfake is an in-memory auth.Service and auth.Directory for tests.
package authfake

import (
	"context"
	"fmt"
	"slices"
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

// forbidden wraps auth.ErrForbidden with the 00-fixtures.md 403 text.
type forbidden struct{ msg string }

func (e *forbidden) Error() string { return e.msg }
func (e *forbidden) Unwrap() error { return auth.ErrForbidden }

func denied(doctype string, r auth.Right) error {
	return &forbidden{msg: fmt.Sprintf("You don't have permission to %s %s. Ask your administrator for a role that allows it.", r, doctype)}
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

func (f *Fake) Can(ctx context.Context, p auth.Principal, doctype string, r auth.Right, rec *auth.RecordRef) error {
	f.mu.Lock()
	scope, ok := f.scopeLocked(p, doctype, r)
	hidden := rec != nil && f.hidden[record{doctype, rec.ID}]
	limit := f.restricted[p.ID]
	dir := f.dir
	f.mu.Unlock()

	if !ok {
		return denied(doctype, r)
	}
	if rec == nil {
		return nil
	}
	if hidden || outside(rec.ProjectIDs, limit.projects) || outside(rec.SiteIDs, limit.sites) {
		return auth.ErrNotFound
	}
	if scope == auth.ScopeAll || rec.CreatedBy == p.ID {
		return nil
	}
	if scope == auth.ScopeTeam && dir != nil {
		emp, linked, err := dir.EmployeeForUser(ctx, nil, p.ID)
		if err != nil {
			return err
		}
		if linked {
			reports, err := dir.DirectReports(ctx, nil, emp)
			if err != nil {
				return err
			}
			if slices.Contains(reports, rec.EmployeeID) {
				return nil
			}
		}
	}
	return auth.ErrNotFound
}

func (f *Fake) Scope(ctx context.Context, p auth.Principal, doctype string, r auth.Right) (auth.Predicate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.scopeLocked(p, doctype, r); !ok {
		return auth.Predicate{}, denied(doctype, r)
	}
	pred := f.scopes[doctype]
	pred.Args = slices.Clone(pred.Args)
	return pred, nil
}

// CheckValues rejects proposed project and site ids outside p's Restrict lists.
func (f *Fake) CheckValues(ctx context.Context, p auth.Principal, name string, values map[string][]int64) error {
	dt, ok := doctype.Lookup(name)
	if !ok {
		return nil
	}
	f.mu.Lock()
	limit := f.restricted[p.ID]
	f.mu.Unlock()
	check := func(fields []string, allowed []int64, what string) error {
		for _, field := range fields {
			for _, id := range values[field] {
				if outside([]int64{id}, allowed) {
					return fmt.Errorf("You can't use %s %d. Choose one you have access to: %w", what, id, auth.ErrForbidden)
				}
			}
		}
		return nil
	}
	if err := check(dt.ProjectFields, limit.projects, "project"); err != nil {
		return err
	}
	return check(dt.SiteFields, limit.sites, "site")
}

func (f *Fake) CheckLink(ctx context.Context, p auth.Principal, target string, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, canRead := f.scopeLocked(p, target, auth.RightRead)
	if f.hidden[record{target, id}] || !canRead {
		label := target
		if dt, ok := doctype.Lookup(target); ok {
			label = dt.Label
		}
		return &auth.LinkError{Label: label, Display: fmt.Sprintf("#%d", id)} // CD-125
	}
	return nil
}

// UsersWithRight returns the SetUsers list, or every user holding a granted role, sorted, without disabled users.
func (f *Fake) UsersWithRight(ctx context.Context, doctype string, r auth.Right, rec *auth.RecordRef) ([]auth.UserID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	candidates, set := f.users[docRight{doctype, r}]
	if !set {
		for u, roles := range f.added {
			for role := range roles {
				_, one := f.grants[grant{role, docRight{doctype, r}}]
				_, all := f.grants[grant{role, docRight{"*", r}}]
				if (one || all) && !slices.Contains(candidates, u) {
					candidates = append(candidates, u)
				}
			}
		}
	}
	var out []auth.UserID
	for _, u := range candidates {
		if info, known := f.infos[u]; known && !info.Enabled {
			continue
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
