package purchase_test

import (
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// p5WantRequestRights are the purchase.Request rows of 00-fixtures.md (PO
// spec §20.6, CD-253, CD-255): approve is Employee's (own) and Purchase
// Manager's and Director's (all); it only closes, since requests aren't
// approvable.
func p5WantRequestRights() map[string]map[string]string {
	return map[string]map[string]string{
		"Employee":         scoped("own", "read", "create", "write", "submit", "cancel", "delete", "approve"),
		"Stores":           scoped("all", "read"),
		"Purchase User":    scoped("all", "read"),
		"Purchase Manager": scoped("all", "read", "approve"),
		"Director":         scoped("all", "read", "approve"),
	}
}

// p5Fake is an authfake seeded with purchase.Request's registered
// DefaultRights and the fixture restrictions (Suresh to PUNE-WH and
// METRO-SITE, Priya to PRJ-METRO), as the real env is.
func p5Fake(t *testing.T, env *purchasetest.Env) *authfake.Fake {
	t.Helper()
	f := authfake.New()
	for role, rs := range lookup(t, purchase.RequestDocType).DefaultRights {
		for r, s := range rs {
			f.Grant(role, purchase.RequestDocType, auth.Right(r), auth.Scope(s))
		}
	}
	f.Restrict(env.Suresh.ID, nil, []int64{p5ID(t, masterID(t, env.DB, "site", "PUNE-WH")), p5ID(t, masterID(t, env.DB, "site", "METRO-SITE"))})
	f.Restrict(env.Priya.ID, []int64{p5ID(t, masterID(t, env.DB, "project", "PRJ-METRO"))}, nil)
	return f
}

// p5SameDecision fails unless the real service and the fake decide alike:
// both allow, or both refuse with the same kind and text.
func p5SameDecision(t *testing.T, what string, real, fake error) {
	t.Helper()
	kind := func(err error) string {
		switch {
		case err == nil:
			return "allowed"
		case errors.Is(err, auth.ErrNotFound):
			return "not found"
		case errors.Is(err, auth.ErrForbidden):
			return "forbidden"
		}
		return "other"
	}
	if kind(real) != kind(fake) || (real != nil && fake != nil && real.Error() != fake.Error()) {
		t.Errorf("%s: real auth = %v (%s), authfake = %v (%s); want the same decision", what, real, kind(real), fake, kind(fake))
	}
}

// PO-067 (PO spec §20.6, §26; CD-253, CD-255, CD-280 D2): purchase.Request's
// DefaultRights equal the purchase.Request rows of 00-fixtures.md for every
// role, approve included. The real auth.Service seeds exactly these rows
// (so ValidateRow accepts approve on a non-approvable DocType whose
// CloseRight is approve, M0-24), and authfake seeded with them decides every
// right like the real service for a holder of each role alone.
func TestRequest_DefaultRights(t *testing.T) {
	dt := lookup(t, purchase.RequestDocType)
	if dt.Approvable || dt.CloseRight != "approve" {
		t.Fatalf("%s Approvable %v, CloseRight %q; want not approvable, CloseRight approve (CD-253, CD-255)", dt.Name, dt.Approvable, dt.CloseRight)
	}
	want := p5WantRequestRights()
	if !maps.EqualFunc(dt.DefaultRights, want, maps.Equal) {
		t.Errorf("%s DefaultRights = %v\nwant %v", dt.Name, dt.DefaultRights, want)
	}

	env := purchasetest.New(t)
	for role, rights := range want {
		got, err := env.Auth.RoleRights(t.Context(), role, dt.Name)
		if err != nil {
			t.Fatalf("RoleRights(%s, %s): %v", role, dt.Name, err)
		}
		w := map[auth.Right]auth.Scope{}
		for r, s := range rights {
			w[auth.Right(r)] = auth.Scope(s)
		}
		if !maps.Equal(got, w) {
			t.Errorf("real auth: %s on %s = %v, want %v", role, dt.Name, got, w)
		}
	}

	f := p5Fake(t, env)
	roles := slices.Sorted(maps.Keys(want))
	for i, role := range roles {
		p := auth.Principal{ID: auth.UserID(9000 + i), Name: role + " holder", Roles: []string{role}}
		for _, r := range append(slices.Clone(auth.AllRights), auth.Right("close")) {
			realErr := env.Auth.Can(t.Context(), p, dt.Name, r, nil)
			fakeErr := f.Can(t.Context(), p, dt.Name, r, nil)
			p5SameDecision(t, role+" "+string(r), realErr, fakeErr)
			if r == auth.RightApprove {
				_, holds := want[role]["approve"]
				if holds != (realErr == nil) {
					t.Errorf("real auth: %s approve = %v, want allowed %v", role, realErr, holds)
				}
			}
		}
	}
}

// PO-049 (PO spec §20.6, §26), on the real auth.Service: Suresh (sites
// PUNE-WH and METRO-SITE) raises a request for METRO-SITE; for TECH-SITE he
// gets AUTH's "You can only use…" text and nothing is written. Listing
// requests through Auth.Scope, he sees his own and Deepak's PR-B (PUNE-WH),
// never Ravi's TECH-SITE request, and Can agrees row by row.
func TestRequest_SiteRestrictionReal(t *testing.T) {
	env := purchasetest.New(t)
	mine := p5Must(t, "Suresh raises METRO-SITE")(p5Create(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"), p5Lines(t, env)))

	before := p5CountAll(t, env)
	_, err := p5Create(t, env, env.Suresh, p5Header(t, env, "TECH-SITE", "PRJ-TECHPARK"), p5Lines(t, env))
	p5WantDenied(t, "Suresh raises TECH-SITE", err, p5SureshSites)
	p5SameCounts(t, "Suresh raises TECH-SITE", before, env)

	prB := p5Must(t, "Deepak raises PR-B")(p5Create(t, env, env.Deepak, p5Header(t, env, "PUNE-WH", "PRJ-TECHPARK"), p5Lines(t, env)))
	tech := p5Must(t, "Ravi raises TECH-SITE")(p5Create(t, env, env.Ravi, p5Header(t, env, "TECH-SITE", "PRJ-TECHPARK"), p5Lines(t, env)))
	p5Must(t, "submit PR-B")(p5Do(t, env, env.Deepak, prB.ID, doctype.ActionSubmit, ""))

	pred, err := env.Auth.Scope(t.Context(), env.Suresh, purchase.RequestDocType, auth.RightRead)
	if err != nil {
		t.Fatalf("Scope(Suresh, read): %v", err)
	}
	q := `SELECT id FROM purchase_request`
	if pred.SQL != "" {
		q += ` WHERE ` + pred.SQL
	}
	rows, err := env.DB.SQL().QueryContext(t.Context(), q+` ORDER BY id`, pred.Args...)
	if err != nil {
		t.Fatalf("list as Suresh (%q %v): %v", pred.SQL, pred.Args, err)
	}
	var got []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got = append(got, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if want := []int64{mine.ID, prB.ID}; !slices.Equal(got, want) {
		t.Errorf("Suresh's list = %v, want %v (his METRO-SITE request and PR-B; never Ravi's TECH-SITE %d)", got, want, tech.ID)
	}
	for _, c := range []struct {
		id      int64
		visible bool
	}{{mine.ID, true}, {prB.ID, true}, {tech.ID, false}} {
		err := env.Auth.Can(t.Context(), env.Suresh, purchase.RequestDocType, auth.RightRead, p5Ref(t, p5Load(t, env, purchase.RequestDocType, c.id)))
		if c.visible && err != nil {
			t.Errorf("Can(Suresh, read, request %d) = %v, want allowed", c.id, err)
		}
		if !c.visible {
			p5WantNotFound(t, "Suresh reads Ravi's TECH-SITE request", err)
		}
	}
}

// PO-050 (PO spec §20.3, §20.6, §26; CD-253, CD-255, CD-280 D2), on the
// real auth.Service through the engine: Suresh edits his draft PR-A and
// closes it once submitted. Priya (Purchase User) gets AUTH's 403 on edit and
// on close; Ravi (Purchase Manager) gets AUTH's 403 on edit but closes a
// submitted request; Meena (Director) closes one. Deepak, another employee,
// can't read Suresh's request, so AUTH answers him not-found (CD-245) on edit
// and on close. Every refusal writes nothing and leaves the request as it
// was. Then the same decisions on authfake match the real service for every
// right on PR-A.
func TestRequest_Rights(t *testing.T) {
	env := purchasetest.New(t)
	draft := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())

	p5Must(t, "Suresh edits his draft")(p5Save(t, env, env.Suresh, draft.ID, map[string]string{"purpose": "Slab casting, block B"}, nil))
	if got := p5Plain(p5Load(t, env, purchase.RequestDocType, draft.ID).Header["purpose"]); got != "Slab casting, block B" {
		t.Errorf("purpose after Suresh's edit = %q, want the new text", got)
	}

	refuse := func(what string, id int64, err error, want string, state doctype.State, before p5Counts) {
		t.Helper()
		if want == p5NotFound {
			p5WantNotFound(t, what, err)
		} else {
			p5WantDenied(t, what, err, want)
		}
		p5SameCounts(t, what, before, env)
		d := p5Load(t, env, purchase.RequestDocType, id)
		if d.State != state || p5Plain(d.Header["purpose"]) != "Slab casting, block B" {
			t.Errorf("%s: request %d is %s with purpose %v, want %s unchanged", what, id, d.State, d.Header["purpose"], state)
		}
	}
	for _, c := range []struct {
		who  string
		want string
	}{
		{"Deepak", p5NotFound},
		{"Priya", p5OwnOnlyEdit},
		{"Ravi", p5OwnOnlyEdit},
	} {
		before := p5CountAll(t, env)
		version := p5Version(t, env, draft.ID)
		_, err := p5Save(t, env, p5Who(env, c.who), draft.ID, map[string]string{"purpose": c.who + " was here"}, nil)
		refuse(c.who+" edits PR-A", draft.ID, err, c.want, doctype.StateDraft, before)
		if v := p5Version(t, env, draft.ID); v != version {
			t.Errorf("%s edits PR-A: version %d, want %d unchanged", c.who, v, version)
		}
	}

	p5Must(t, "Suresh submits PR-A")(p5Do(t, env, env.Suresh, draft.ID, doctype.ActionSubmit, ""))
	for _, c := range []struct {
		who  string
		want string
	}{
		{"Deepak", p5NotFound},
		{"Priya", p5OwnOnlyClose},
	} {
		before := p5CountAll(t, env)
		_, err := p5Do(t, env, p5Who(env, c.who), draft.ID, doctype.ActionClose, p5CloseReason)
		refuse(c.who+" closes PR-A", draft.ID, err, c.want, doctype.StateApproved, before)
	}

	// The real-service decisions on PR-A, mirrored by authfake.
	f := p5Fake(t, env)
	ref := p5Ref(t, p5Load(t, env, purchase.RequestDocType, draft.ID))
	for _, who := range []string{"Suresh", "Deepak", "Priya", "Ravi", "Meena"} {
		p := p5Who(env, who)
		for _, r := range append(slices.Clone(auth.AllRights), auth.Right("close")) {
			p5SameDecision(t, who+" "+string(r)+" on PR-A", env.Auth.Can(t.Context(), p, purchase.RequestDocType, r, ref),
				f.Can(t.Context(), p, purchase.RequestDocType, r, ref))
		}
	}
	for _, c := range []struct {
		who          string
		write, close string // "" = allowed
	}{
		{"Suresh", "", ""},
		{"Deepak", p5NotFound, p5NotFound},
		{"Priya", p5OwnOnlyEdit, p5OwnOnlyClose},
		{"Ravi", p5OwnOnlyEdit, ""},
		{"Meena", "", ""},
	} {
		p := p5Who(env, c.who)
		for _, svc := range []struct {
			name string
			can  func(r auth.Right) error
		}{
			{"real auth", func(r auth.Right) error { return env.Auth.Can(t.Context(), p, purchase.RequestDocType, r, ref) }},
			{"authfake", func(r auth.Right) error { return f.Can(t.Context(), p, purchase.RequestDocType, r, ref) }},
		} {
			for _, x := range []struct {
				r    auth.Right
				want string
			}{{auth.RightWrite, c.write}, {auth.RightApprove, c.close}} {
				if c.who == "Meena" && x.r == auth.RightWrite {
					continue // Meena's write is not part of PO-050
				}
				err := svc.can(x.r)
				what := svc.name + ": " + c.who + " " + string(x.r) + " on PR-A"
				switch x.want {
				case "":
					if err != nil {
						t.Errorf("%s = %v, want allowed", what, err)
					}
				case p5NotFound:
					p5WantNotFound(t, what, err)
				default:
					p5WantDenied(t, what, err, x.want)
				}
			}
		}
	}

	// Closing: Suresh his own, Ravi and Meena any submitted request.
	for _, c := range []struct {
		who string
		id  int64
	}{
		{"Ravi", purchasetest.NewRequest(t, env).ID},
		{"Meena", purchasetest.NewRequest(t, env).ID},
		{"Suresh", draft.ID},
	} {
		res, err := p5Do(t, env, p5Who(env, c.who), c.id, doctype.ActionClose, p5CloseReason)
		if err != nil {
			t.Errorf("%s closes request %d: %v, want closed", c.who, c.id, err)
			continue
		}
		d := p5Load(t, env, purchase.RequestDocType, c.id)
		if res.State != doctype.StateClosed || d.State != doctype.StateClosed || d.StateReason != p5CloseReason {
			t.Errorf("%s closes request %d: state %s (stored %s, reason %q), want closed with %q", c.who, c.id, res.State, d.State, d.StateReason, p5CloseReason)
		}
	}
}

// p5Who is the fixture principal named who.
func p5Who(env *purchasetest.Env, who string) auth.Principal {
	return map[string]auth.Principal{
		"Admin": env.Admin, "Meena": env.Meena, "Ravi": env.Ravi, "Priya": env.Priya,
		"Suresh": env.Suresh, "Kavya": env.Kavya, "Deepak": env.Deepak, "Arjun": env.Arjun,
	}[who]
}
