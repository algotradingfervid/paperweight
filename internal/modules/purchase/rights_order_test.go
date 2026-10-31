package purchase_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// p3WantNotFound asserts err is AUTH's 404 for a record p can't read
// (CD-245: hidden and missing answer alike).
func p3WantNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, doctype.ErrNotFound) || p3Status(err) != 404 || err.Error() != auth.MsgNotFound {
		t.Errorf("%s: err = %v (status %d), want 404 %q", what, err, p3Status(err), auth.MsgNotFound)
	}
}

// PO-013 (PO spec §10, §18), on the real auth.Service: Priya (restricted to
// PRJ-METRO) lists and opens only PRJ-METRO POs; a PRJ-TECHPARK PO answers as
// not found. Arjun, the same role unrestricted, sees every PO, so the
// restriction is what hides them.
func TestPO_ProjectRestrictionReal(t *testing.T) {
	env := purchasetest.New(t)
	small := p3SmallLine(t, env)
	metro := p3Order(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), small, false)
	metroWH := p3Order(t, env, env.Priya, p2Header(t, env, "SHREE", "PUNE-WH", "PRJ-METRO"), small, true)
	tech := p3Order(t, env, env.Admin, p2Header(t, env, "SHREE", "TECH-SITE", "PRJ-TECHPARK"), small, false)
	techWH := p3Order(t, env, env.Admin, p2Header(t, env, "SHREE", "PUNE-WH", "PRJ-TECHPARK"), small, true)

	want := []int64{metro, metroWH}
	slices.Sort(want)
	if got := p3ListIDs(t, env, env.Priya); !slices.Equal(got, want) {
		t.Errorf("Priya's list = %v, want only the PRJ-METRO POs %v", got, want)
	}
	all := []int64{metro, metroWH, tech, techWH}
	slices.Sort(all)
	if got := p3ListIDs(t, env, env.Arjun); !slices.Equal(got, all) {
		t.Errorf("Arjun's list = %v, want every PO %v", got, all)
	}
	for _, id := range []int64{metro, metroWH} {
		if err := p3Open(t, env, env.Priya, id); err != nil {
			t.Errorf("Priya opens PRJ-METRO PO %d: %v, want allowed", id, err)
		}
	}
	for _, id := range []int64{tech, techWH} {
		p3WantNotFound(t, "Priya opens a PRJ-TECHPARK PO", p3Open(t, env, env.Priya, id))
		if err := p3Open(t, env, env.Arjun, id); err != nil {
			t.Errorf("Arjun opens PRJ-TECHPARK PO %d: %v, want allowed", id, err)
		}
	}
}

// PO-014 (PO spec §10, §18; AUTH-095), on the real auth.Service: Suresh
// (sites PUNE-WH and METRO-SITE) opens the METRO-SITE PO and not the
// TECH-SITE one. Project and site restrictions apply together: once he is
// also restricted to PRJ-METRO, a PUNE-WH PO on PRJ-TECHPARK is hidden too.
func TestPO_SiteRestrictionReal(t *testing.T) {
	env := purchasetest.New(t)
	small := p3SmallLine(t, env)
	metro := p3Order(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), small, false)
	tech := p3Order(t, env, env.Admin, p2Header(t, env, "SHREE", "TECH-SITE", "PRJ-TECHPARK"), small, false)
	techWH := p3Order(t, env, env.Admin, p2Header(t, env, "SHREE", "PUNE-WH", "PRJ-TECHPARK"), small, false)

	if err := p3Open(t, env, env.Suresh, metro); err != nil {
		t.Errorf("Suresh opens the METRO-SITE PO: %v, want allowed", err)
	}
	p3WantNotFound(t, "Suresh opens the TECH-SITE PO", p3Open(t, env, env.Suresh, tech))
	if err := p3Open(t, env, env.Suresh, techWH); err != nil {
		t.Errorf("Suresh opens the PUNE-WH PO on PRJ-TECHPARK: %v, want allowed (no project restriction yet)", err)
	}
	want := []int64{metro, techWH}
	slices.Sort(want)
	if got := p3ListIDs(t, env, env.Suresh); !slices.Equal(got, want) {
		t.Errorf("Suresh's list = %v, want his sites' POs %v", got, want)
	}

	prjMetro := p2Int(t, masterID(t, env.DB, "project", "PRJ-METRO"))
	if err := env.Auth.ChangeRestrictions(t.Context(), env.Admin, env.Suresh.ID, auth.DimProject, []int64{prjMetro}, nil, nil); err != nil {
		t.Fatalf("restrict Suresh to PRJ-METRO: %v", err)
	}
	if err := p3Open(t, env, env.Suresh, metro); err != nil {
		t.Errorf("Suresh (sites and PRJ-METRO) opens the METRO-SITE PO: %v, want allowed", err)
	}
	p3WantNotFound(t, "Suresh (sites and PRJ-METRO) opens the PUNE-WH PO on PRJ-TECHPARK", p3Open(t, env, env.Suresh, techWH))
	if got := p3ListIDs(t, env, env.Suresh); !slices.Equal(got, []int64{metro}) {
		t.Errorf("Suresh's list with both restrictions = %v, want only %d", got, metro)
	}
}

// PO-058 (PO spec §10, §21.1, §26), on the real auth.Service: only the
// owner edits, submits, withdraws, revises and deletes a draft. Ravi
// (Purchase Manager: no write, submit or delete) and Arjun (Purchase User:
// write, submit and delete own only) get 403 on each and change nothing;
// Priya, the owner, does each. Arjun does the same on his own draft, so his
// refusals are about ownership. A user who can't read the draft gets 404
// (CD-245), not 403.
func TestPO_OwnerOnlyReal(t *testing.T) {
	env := purchasetest.New(t)
	id := p3Order(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), p2RefLines(t, env), true)

	// refused runs a as each of Ravi and Arjun: 403, no row changed.
	refused := func(a doc.Action, values map[string]string) {
		t.Helper()
		for _, p := range []auth.Principal{env.Ravi, env.Arjun} {
			before, state, v := p2CountAll(t, env), p2Load(t, env, id).State, p2Version(t, env, id)
			var err error
			if a == doc.ActionSave {
				_, err = p2Save(t, env, p, id, values, nil)
			} else {
				_, err = p2Do(t, env, p, id, a, "")
			}
			var de *auth.DeniedError
			if p3Status(err) != 403 || !errors.As(err, &de) || de.OwnOnly != (p.ID == env.Arjun.ID) {
				t.Errorf("%s %s on Priya's PO: err = %v (status %d), want AUTH's 403 (own-only for Arjun)", p.Name, a, err, p3Status(err))
			}
			p2SameCounts(t, p.Name+" "+string(a), before, env)
			if got := p2Load(t, env, id); got.State != state || got.Version != v {
				t.Errorf("%s %s: PO state %q version %d, want %q %d unchanged", p.Name, a, got.State, got.Version, state, v)
			}
		}
	}
	owner := func(a doc.Action, values map[string]string, want doctype.State) {
		t.Helper()
		var err error
		if a == doc.ActionSave {
			_, err = p2Save(t, env, env.Priya, id, values, nil)
		} else {
			_, err = p2Do(t, env, env.Priya, id, a, "")
		}
		if err != nil {
			t.Fatalf("Priya %s her PO: %v", a, err)
		}
		if got := p2Load(t, env, id).State; got != want {
			t.Fatalf("after Priya %s: state %q, want %q", a, got, want)
		}
	}

	edit := map[string]string{"remarks": "Call before delivery"}
	refused(doc.ActionSave, edit)
	owner(doc.ActionSave, edit, doctype.StateDraft)
	if got := p2Snap(t, env, id, "remarks")["remarks"]; got != edit["remarks"] {
		t.Errorf("remarks after Priya's edit = %q, want %q", got, edit["remarks"])
	}
	refused(doc.ActionSubmit, nil)
	owner(doc.ActionSubmit, nil, doctype.StatePending)
	refused(doc.ActionWithdraw, nil)
	owner(doc.ActionWithdraw, nil, doctype.StateDraft)
	owner(doc.ActionSubmit, nil, doctype.StatePending)
	p2Must(t, "Ravi rejects")(p2Do(t, env, env.Ravi, id, doc.ActionReject, "Rate too high"))
	refused(doc.ActionRevise, nil)
	owner(doc.ActionRevise, nil, doctype.StateDraft)
	refused(doc.ActionDelete, nil)
	if _, err := p2Do(t, env, env.Priya, id, doc.ActionDelete, ""); err != nil {
		t.Fatalf("Priya deletes her draft: %v", err)
	}
	if n := countRows(t, env.DB, "purchase_order", "id = ?", id); n != 0 {
		t.Errorf("Priya's deleted draft still has %d row(s)", n)
	}

	// Arjun's own draft on PRJ-TECHPARK: he edits it, submits and withdraws
	// it, revises it after Ravi's reject; Priya (restricted to PRJ-METRO)
	// can't read it, so each of her attempts is a 404.
	tech := p3Order(t, env, env.Arjun, p2Header(t, env, "SHREE", "TECH-SITE", "PRJ-TECHPARK"), p2RefLines(t, env), true)
	hidden := func(a doc.Action) {
		t.Helper()
		before, v := p2CountAll(t, env), p2Version(t, env, tech)
		var err error
		if a == doc.ActionSave {
			_, err = p2Save(t, env, env.Priya, tech, map[string]string{"remarks": "Mine now"}, nil)
		} else {
			_, err = p2Do(t, env, env.Priya, tech, a, "")
		}
		p3WantNotFound(t, "Priya "+string(a)+" Arjun's PRJ-TECHPARK PO", err)
		p2SameCounts(t, "Priya "+string(a), before, env)
		if got := p2Version(t, env, tech); got != v {
			t.Errorf("Priya %s: version %d, want %d unchanged", a, got, v)
		}
	}
	arjun := func(a doc.Action, reason string) {
		t.Helper()
		var err error
		if a == doc.ActionSave {
			_, err = p2Save(t, env, env.Arjun, tech, map[string]string{"remarks": "Gate 3"}, nil)
		} else {
			_, err = p2Do(t, env, env.Arjun, tech, a, reason)
		}
		if err != nil {
			t.Fatalf("Arjun %s his PO: %v", a, err)
		}
	}
	hidden(doc.ActionSave)
	arjun(doc.ActionSave, "")
	hidden(doc.ActionSubmit)
	arjun(doc.ActionSubmit, "")
	hidden(doc.ActionWithdraw)
	arjun(doc.ActionWithdraw, "")
	arjun(doc.ActionSubmit, "")
	p2Must(t, "Ravi rejects Arjun's PO")(p2Do(t, env, env.Ravi, tech, doc.ActionReject, "Rate too high"))
	hidden(doc.ActionRevise)
	arjun(doc.ActionRevise, "")
	hidden(doc.ActionDelete)
	arjun(doc.ActionDelete, "")
	if n := countRows(t, env.DB, "purchase_order", "id = ?", tech); n != 0 {
		t.Errorf("Arjun's deleted draft still has %d row(s)", n)
	}
}
