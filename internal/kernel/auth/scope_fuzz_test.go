package auth_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// t18Users are the enabled fixture users a fuzz principal is drawn from.
var t18Users = []string{
	"admin@vardhan.test", "meena@vardhan.test", "ravi@vardhan.test", "priya@vardhan.test",
	"suresh@vardhan.test", "kavya@vardhan.test", "deepak@vardhan.test",
}

// t18Roles are the seven canonical roles.
var t18Roles = []string{
	"Administrator", "Purchase User", "Purchase Manager", "Director", "Stores", "Masters Manager", "Employee",
}

var (
	t18Employees = []string{"EMP-001", "EMP-002", "EMP-003", "EMP-004", "EMP-005", "EMP-006", "EMP-007", "EMP-008"}
	t18Projects  = []string{"PRJ-METRO", "PRJ-TECHPARK"}
	t18Sites     = []string{"PUNE-WH", "METRO-SITE", "TECH-SITE"}
	// t18OrderScopes and t18ClaimScopes are the "Fuzz" role's read scope
	// choices: Order has no employee field, so no team (CD-11).
	t18OrderScopes = []auth.Scope{"", auth.ScopeOwn, auth.ScopeAll}
	t18ClaimScopes = []auth.Scope{"", auth.ScopeOwn, auth.ScopeTeam, auth.ScopeAll}
)

// t18Line is one line's links: index into t18Projects / t18Sites plus one,
// 0 = NULL.
type t18Line struct{ Project, Site byte }

// t18Doc is one random document. Creator: 0 = the principal, 1 = NULL,
// n ≥ 2 = fixture user ID n-1. Employee: 0 = none, 1 = the principal's
// employee, n ≥ 2 = t18Employees[n-2]. Manager (of the document's employee):
// 0 = unchanged, 1 = the principal's employee, 2 = none, n ≥ 3 =
// t18Employees[n-3]. Left marks the document's employee as Left. Project
// and Site index t18Projects / t18Sites plus one, 0 = NULL.
type t18Doc struct {
	Creator, Employee, Manager byte
	Left                       bool
	Project, Site              byte
	Lines                      []t18Line
}

// t18Case is one fuzz input, decoded from bytes.
type t18Case struct {
	User                   byte   // index into t18Users
	Roles                  byte   // bit i: t18Roles[i]; bit 7: the ad hoc "Fuzz" role
	OrderScope, ClaimScope byte   // the "Fuzz" role's read scope on DTOrder / DTClaim
	Projects, Sites        []byte // restriction value indexes (empty = no restriction)
	Order, Claim           t18Doc
}

// t18Reader reads bytes, returning 0 once the input is used up.
type t18Reader struct{ b []byte }

func (r *t18Reader) next() byte {
	if len(r.b) == 0 {
		return 0
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v
}

func (r *t18Reader) mod(n int) byte { return byte(int(r.next()) % n) }

func (r *t18Reader) doc() t18Doc {
	d := t18Doc{
		Creator: r.mod(len(t18Users) + 2), Employee: r.mod(len(t18Employees) + 2),
		Manager: r.mod(len(t18Employees) + 3), Left: r.next()%2 == 1,
		Project: r.mod(len(t18Projects) + 1), Site: r.mod(len(t18Sites) + 1),
	}
	for range r.mod(4) {
		d.Lines = append(d.Lines, t18Line{Project: r.mod(len(t18Projects) + 1), Site: r.mod(len(t18Sites) + 1)})
	}
	return d
}

// t18Decode maps any input to a valid case; layout as t18Encode.
func t18Decode(b []byte) t18Case {
	r := &t18Reader{b: b}
	c := t18Case{
		User: r.mod(len(t18Users)), Roles: r.next(),
		OrderScope: r.mod(len(t18OrderScopes)), ClaimScope: r.mod(len(t18ClaimScopes)),
	}
	for range r.mod(4) {
		c.Projects = append(c.Projects, r.mod(len(t18Projects)))
	}
	for range r.mod(4) {
		c.Sites = append(c.Sites, r.mod(len(t18Sites)))
	}
	c.Order, c.Claim = r.doc(), r.doc()
	return c
}

// t18Encode is t18Decode's inverse, for the seed corpus.
func t18Encode(c t18Case) []byte {
	b := []byte{c.User, c.Roles, c.OrderScope, c.ClaimScope, byte(len(c.Projects))}
	b = append(b, c.Projects...)
	b = append(b, byte(len(c.Sites)))
	b = append(b, c.Sites...)
	for _, d := range []t18Doc{c.Order, c.Claim} {
		left := byte(0)
		if d.Left {
			left = 1
		}
		b = append(b, d.Creator, d.Employee, d.Manager, left, d.Project, d.Site, byte(len(d.Lines)))
		for _, l := range d.Lines {
			b = append(b, l.Project, l.Site)
		}
	}
	return b
}

// t18Codes maps restriction indexes to codes, without duplicates.
func t18Codes(all []string, idx []byte) []string {
	var out []string
	for _, i := range idx {
		if c := all[i]; !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out
}

// t18Run sets up c in a fresh World and checks, for both documents, that
// Can(read, ref) == nil ⇔ SELECT … WHERE id = ? AND <Scope(read)> returns
// the row.
func t18Run(t *testing.T, c t18Case) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	email := t18Users[c.User]
	me, _, err := w.Dir.EmployeeForUser(ctx, nil, w.Users[email])
	if err != nil {
		t.Fatalf("EmployeeForUser(%s): %v", email, err)
	}

	var roles []string
	for i, role := range t18Roles {
		if c.Roles&(1<<i) != 0 {
			roles = append(roles, role)
		}
	}
	if c.Roles&(1<<7) != 0 {
		order, claim := t18OrderScopes[c.OrderScope], t18ClaimScopes[c.ClaimScope]
		w.SetRights(t, "Fuzz", authtest.DTOrder, t18Rights(order))
		w.SetRights(t, "Fuzz", authtest.DTClaim, t18Rights(claim))
		roles = append(roles, "Fuzz")
	}
	w.SetRoles(t, email, roles...)
	w.Restrict(t, email, t18Codes(t18Projects, c.Projects), t18Codes(t18Sites, c.Sites))
	p := w.P(t, email)

	link := func(codes map[string]int64, all []string, v byte) int64 {
		if v == 0 {
			return 0
		}
		return codes[all[v-1]]
	}
	for i, tc := range []struct {
		dt, table, lines string
		doc              t18Doc
	}{
		{authtest.DTOrder, "probe_order", "probe_order_line", c.Order},
		{authtest.DTClaim, "probe_claim", "probe_claim_line", c.Claim},
	} {
		d := tc.doc
		var createdBy auth.UserID
		switch {
		case d.Creator == 0:
			createdBy = p.ID
		case d.Creator >= 2:
			createdBy = auth.UserID(d.Creator - 1)
		}
		var emp int64
		switch {
		case d.Employee == 1:
			emp = me
		case d.Employee >= 2:
			emp = w.Employees[t18Employees[d.Employee-2]]
		}
		if emp != 0 {
			switch {
			case d.Manager == 1:
				w.Dir.SetManager(emp, me)
			case d.Manager == 2:
				w.Dir.SetManager(emp, 0)
			case d.Manager >= 3:
				w.Dir.SetManager(emp, w.Employees[t18Employees[d.Manager-3]])
			}
			if d.Left {
				w.Dir.SetActive(emp, false)
			}
		}
		id := int64(100 + i)
		t6InsertDoc(t, w, tc.table, id, createdBy, emp, link(w.Projects, t18Projects, d.Project), link(w.Sites, t18Sites, d.Site))
		for j, l := range d.Lines {
			t6InsertLine(t, w, tc.lines, id*10+int64(j), id, link(w.Projects, t18Projects, l.Project), link(w.Sites, t18Sites, l.Site))
		}

		ref := t6aRef(t, w, tc.dt, id)
		canErr := w.Auth.Can(ctx, p, tc.dt, auth.RightRead, ref)
		if canErr != nil && !errors.Is(canErr, auth.ErrForbidden) && !errors.Is(canErr, auth.ErrNotFound) {
			t.Fatalf("Can(%s, %s, read) unexpected error: %v", email, tc.dt, canErr)
		}
		pred, scopeErr := w.Auth.Scope(ctx, p, tc.dt, auth.RightRead)
		var de *auth.DeniedError
		if scopeErr != nil && !errors.As(scopeErr, &de) {
			t.Fatalf("Scope(%s, %s, read) error = %v, want nil or *auth.DeniedError", email, tc.dt, scopeErr)
		}
		inScope := scopeErr == nil && t18Selected(t, w, tc.table, id, pred)
		if inScope != (canErr == nil) {
			t.Errorf("%s roles %q, restrictions %+v / %+v, %s %+v (employee %d, manager choice %d, left %v): "+
				"row returned by Scope = %v (SQL %q args %v, err %v), Can(read) = %v",
				email, p.Roles, c.Projects, c.Sites, tc.dt, *ref, emp, d.Manager, d.Left,
				inScope, pred.SQL, pred.Args, scopeErr, canErr)
		}
	}
}

// t18Rights is a valid matrix row with read at scope s ("" = no rights).
func t18Rights(s auth.Scope) map[auth.Right]auth.Scope {
	if s == "" {
		return map[auth.Right]auth.Scope{}
	}
	return map[auth.Right]auth.Scope{auth.RightRead: s}
}

// t18Selected runs SELECT … WHERE id = ? AND <pred>.
func t18Selected(t *testing.T, w *authtest.World, table string, id int64, pred auth.Predicate) bool {
	t.Helper()
	ctx := context.Background()
	q := `SELECT count(*) FROM "` + table + `" WHERE "` + table + `"."id" = ?` //nolint:gosec // G202: table is a probe constant
	if pred.SQL != "" {
		q += ` AND (` + pred.SQL + `)`
	}
	var n int
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, q, append([]any{id}, pred.Args...)...).Scan(&n)
	}); err != nil {
		t.Fatalf("run predicate %q %v: %v", pred.SQL, pred.Args, err)
	}
	return n == 1
}

// t18Seeds are the hand-picked seed cases.
var t18Seeds = []struct {
	name string
	c    t18Case
}{
	{
		// Suresh (Stores, Employee) restricted to PUNE-WH and METRO-SITE: the
		// PO has no header site, and its lines are at METRO-SITE, at no site
		// and at TECH-SITE, so the TECH-SITE line hides it; the claim's line
		// site is not a declared field, so it doesn't hide the claim.
		name: "line-only sites",
		c: t18Case{
			User: 4, Roles: 1<<4 | 1<<6, Sites: []byte{0, 1},
			Order: t18Doc{Creator: 3, Project: 1, Lines: []t18Line{{Site: 2}, {Project: 2}, {Site: 3}}},
			Claim: t18Doc{Creator: 0, Employee: 1, Project: 1, Lines: []t18Line{{Site: 3}}},
		},
	},
	{
		// Priya restricted to PRJ-METRO; both documents have an empty header
		// (no project, no site, no lines): strict restrictions hide them.
		name: "empty header",
		c: t18Case{
			User: 3, Roles: 1<<1 | 1<<6, Projects: []byte{0},
			Order: t18Doc{Creator: 0},
			Claim: t18Doc{Creator: 0, Employee: 1},
		},
	},
	{
		// Priya with team read on claims through the Fuzz role; the claim is
		// for EMP-007 (Deepak), who reports to Priya, and was created by Deepak.
		name: "team direct report",
		c: t18Case{
			User: 3, Roles: 1 << 7, OrderScope: 1, ClaimScope: 2,
			Order: t18Doc{Creator: 8, Project: 1, Site: 2},
			Claim: t18Doc{Creator: 8, Employee: 8, Manager: 1, Project: 1},
		},
	},
	{
		// Ravi (Purchase Manager, Employee): the claim's employee reports to
		// Ravi but has left, so team scope must not include it.
		name: "team report left",
		c: t18Case{
			User: 2, Roles: 1<<2 | 1<<6,
			Order: t18Doc{Creator: 1, Project: 2, Site: 3, Lines: []t18Line{{Project: 2, Site: 3}}},
			Claim: t18Doc{Creator: 6, Employee: 6, Manager: 1, Left: true, Project: 2},
		},
	},
	{
		// Deepak with own read through the Fuzz role, restricted to both
		// projects (one listed twice) and all three sites; the PO has an
		// empty line, the claim a NULL creator and a Left employee with no
		// manager.
		name: "own scope full restrictions",
		c: t18Case{
			User: 6, Roles: 1<<6 | 1<<7, OrderScope: 1, ClaimScope: 1,
			Projects: []byte{0, 1, 1}, Sites: []byte{0, 1, 2},
			Order: t18Doc{Creator: 0, Project: 2, Site: 1, Lines: []t18Line{{Site: 3}, {Project: 1, Site: 2}, {}}},
			Claim: t18Doc{Creator: 1, Employee: 9, Manager: 2, Project: 2},
		},
	},
}

// AUTH-113
func FuzzScope_CanMatchesSQL(f *testing.F) {
	for _, s := range t18Seeds {
		b := t18Encode(s.c)
		if got := t18Decode(b); fmt.Sprintf("%+v", got) != fmt.Sprintf("%+v", s.c) {
			f.Fatalf("seed %q: t18Decode(t18Encode(c)) = %+v, want %+v", s.name, got, s.c)
		}
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		t18Run(t, t18Decode(b))
	})
}
