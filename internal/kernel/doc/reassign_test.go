package doc_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// DOC-111 and DOC-112 (MOD-PO plan M0-13; PO spec §21.2; CD-247 (2), CD-253).
// Every call runs on the real auth.Service as non-Administrators (CD-213).
// Check order (AD ruling, 31 Oct): (a) load, (b) read on the record, (c) owner
// or approve, (d) version, (e) state, (f) SameOwner, (g) target: Disabled,
// CantCreate, CantSee, write as owner (CantCreate).

const (
	rsAction   = "reassign"                                                                            // the reassign audit entry's Action (DOC-112)
	rsHandover = "Handed over from %s to %s"                                                           // DOC's history text (CD-253): from, to display names
	rsPriya    = "Priya Shah"                                                                          // 00-fixtures display name of the owner
	rsDrafter  = "Order Drafter"                                                                       // a role with read and create but no write
	rsMetro    = "PRJ-METRO"                                                                           // the orders' project
	rsTechpark = "PRJ-TECHPARK"                                                                        // a project the orders aren't on
	rsStale    = "Someone else changed %s after you opened it. Reload it and make your changes again." // DOC-069
)

// rsEnv is nsNew's real AUTH environment with the reassign targets.
type rsEnv struct {
	*nsEnv
	Kiran  auth.Principal // Purchase User, unrestricted: eligible
	Nisha  auth.Principal // Purchase User, unrestricted: eligible (DOC-112's second target)
	Asha   auth.Principal // Purchase User, disabled
	Tara   auth.Principal // Purchase User restricted to PRJ-TECHPARK: can create, can't see a METRO order
	Uma    auth.Principal // Order Drafter: read and create on orders, no write
	Vikram auth.Principal // Purchase Manager restricted to PRJ-TECHPARK: holds approve, can't see a METRO order
	Former auth.UserID    // old@vardhan.test, Former Staff: disabled Employee (00-fixtures)
}

// rsNew returns nsNew with Purchase User's write, submit and delete on
// fixture.Order narrowed to own (CD-246), so a target passes write only on a
// copy of the RecordRef owned by them (CD-247 (2)), and the target users
// created through AUTH's own admin calls.
func rsNew(t *testing.T) *rsEnv {
	t.Helper()
	ctx := t.Context()
	e := nsNew(t)
	a, admin := e.A, e.Admin
	pu := map[auth.Right]auth.Scope{
		"read": auth.ScopeAll, "create": auth.ScopeAll, "write": auth.ScopeOwn, "submit": auth.ScopeOwn,
		"delete": auth.ScopeOwn, "print": auth.ScopeAll, "export": auth.ScopeAll,
	}
	if err := a.SetRights(ctx, admin, "Purchase User", fixture.Order, pu); err != nil {
		t.Fatalf("SetRights(Purchase User): %v", err)
	}
	if err := a.CreateRole(ctx, admin, rsDrafter); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := a.SetRights(ctx, admin, rsDrafter, fixture.Order, map[auth.Right]auth.Scope{"read": auth.ScopeAll, "create": auth.ScopeAll}); err != nil {
		t.Fatalf("SetRights(%s): %v", rsDrafter, err)
	}
	user := func(email, name string, roles ...string) auth.UserID {
		id, _, err := a.CreateUser(ctx, admin, auth.NewUser{Email: email, Name: name, Password: rqRealPassword, Roles: roles})
		if err != nil {
			t.Fatalf("CreateUser(%s): %v", email, err)
		}
		return id
	}
	restrict := func(u auth.UserID, code string) {
		if err := a.ChangeRestrictions(ctx, admin, u, auth.DimProject, []int64{e.M.Project[code]}, nil, nil); err != nil {
			t.Fatalf("ChangeRestrictions(%d, %s): %v", u, code, err)
		}
	}
	kiran := user("kiran@vardhan.test", "Kiran Joshi", "Purchase User")
	nisha := user("nisha@vardhan.test", "Nisha Desai", "Purchase User")
	asha := user("asha@vardhan.test", "Asha Menon", "Purchase User")
	tara := user("tara@vardhan.test", "Tara Bose", "Purchase User")
	uma := user("uma@vardhan.test", "Uma Pillai", rsDrafter)
	vikram := user("vikram@vardhan.test", "Vikram Sethi", "Purchase Manager")
	restrict(tara, rsTechpark)
	restrict(vikram, rsTechpark)
	if err := e.DB.Write(ctx, func(tx *db.Tx) error { return a.DisableUser(ctx, tx, asha, "left") }); err != nil {
		t.Fatalf("DisableUser(Asha): %v", err)
	}
	r := &rsEnv{
		nsEnv: e, Kiran: rqPrincipal(t, a, kiran), Nisha: rqPrincipal(t, a, nisha), Asha: auth.Principal{ID: asha, Name: "Asha Menon"},
		Tara: rqPrincipal(t, a, tara), Uma: rqPrincipal(t, a, uma), Vikram: rqPrincipal(t, a, vikram),
	}
	for _, u := range e.F.Users {
		if u.Email == "old@vardhan.test" {
			r.Former = u.ID
		}
	}
	if r.Former == 0 {
		t.Fatal("setup: no old@vardhan.test in the fixtures")
	}
	return r
}

// order is a draft order by Priya on PRJ-METRO in state s (forced; approved,
// closed and cancelled get the number PO/26-27/{id}).
func (e *rsEnv) order(t *testing.T, s doc.State) doc.Doc {
	t.Helper()
	d := e.draftOrder(t)
	if s != doc.StateDraft {
		number := ""
		if s == doc.StateApproved || s == doc.StateClosed || s == doc.StateCancelled {
			number = fmt.Sprintf("PO/26-27/%05d", d.ID)
		}
		e.ForceState(t, fixture.Order, d.ID, s, number)
		d = e.Load(t, fixture.Order, d.ID)
	}
	if d.State != s || d.CreatedBy != e.Priya.ID {
		t.Fatalf("setup: order State %q CreatedBy %d, want %q owned by Priya (%d)", d.State, d.CreatedBy, s, e.Priya.ID)
	}
	return d
}

// reassign runs Engine.Reassign on the pool of one (CD-213).
func (e *rsEnv) reassign(t *testing.T, p auth.Principal, id int64, version int, to types.UserID) error {
	t.Helper()
	return testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		return e.Engine.Reassign(ctx, p, fixture.Order, id, version, to)
	})
}

// rsSnap is what a refused reassign must leave as it was.
type rsSnap struct {
	Owner      types.UserID
	Version    int
	State      doc.State
	DocEntries int // the document's audit entries, which doc.History shows
	AllEntries int // every audit_log row
}

func (e *rsEnv) snap(t *testing.T, id int64) rsSnap {
	t.Helper()
	d := e.Load(t, fixture.Order, id)
	return rsSnap{d.CreatedBy, d.Version, d.State, len(e.Audit(t, fixture.Order, id)), e.AuditCount(t)}
}

// unchanged fails unless the document is as before.
func (e *rsEnv) unchanged(t *testing.T, id int64, before rsSnap) {
	t.Helper()
	if after := e.snap(t, id); after != before {
		t.Errorf("refused reassign changed the document: before %+v, after %+v", before, after)
	}
}

// wantReason fails unless err is a *doc.ReassignError with exactly reason and
// nothing else in it (it carries no user text).
func wantReason(t *testing.T, err error, reason doc.ReassignReason) {
	t.Helper()
	var re *doc.ReassignError
	if !errors.As(err, &re) {
		t.Fatalf("err = %v (%T), want *doc.ReassignError{Reason: %q}", err, err, reason)
	}
	if *re != (doc.ReassignError{Reason: reason}) {
		t.Errorf("ReassignError = %+v, want only Reason %q", *re, reason)
	}
}

// authErr is what the real AUTH answers p for right r on the stored order:
// the error a refused caller must get unchanged.
func (e *rsEnv) authErr(t *testing.T, p auth.Principal, id int64, r auth.Right) error {
	t.Helper()
	info := e.Info(t, fixture.Order, id)
	rec := &auth.RecordRef{DocType: fixture.Order, ID: id, CreatedBy: info.CreatedBy, EmployeeID: info.EmployeeID,
		ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs}
	var want error
	if err := e.DB.Read(t.Context(), func(tx *db.Tx) error {
		want = e.A.CanTx(t.Context(), tx, p, fixture.Order, r, rec)
		return nil
	}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if want == nil {
		t.Fatalf("setup: %s holds %s on order %d, want refused", p.Name, r, id)
	}
	return want
}

// doErr is what Engine.Do answers p on order id (approve needs only read
// before anything else in the pipeline): the missing or hidden answer.
func (e *rsEnv) doErr(t *testing.T, p auth.Principal, id int64) error {
	t.Helper()
	_, err := e.Engine.Do(t.Context(), p, fixture.Order, id, doc.ActionApprove, doc.ActionInput{Version: 1})
	if err == nil {
		t.Fatalf("setup: Do as %s on %d succeeded, want a 404", p.Name, id)
	}
	return err
}

// rsSame fails unless got is Do's 404 want: same text, not found, status 404.
func rsSame(t *testing.T, what string, got, want error) {
	t.Helper()
	var re *doc.ReassignError
	if errors.As(got, &re) {
		t.Fatalf("%s: err = ReassignError %+v, want a 404", what, *re)
	}
	if got == nil || got.Error() != want.Error() || !errors.Is(got, auth.ErrNotFound) || errors.Is(got, auth.ErrForbidden) {
		t.Errorf("%s: err = %v (%T), want Do's %v (%T)", what, got, got, want, want)
	}
	var gs, ws doc.HTTPError
	if !errors.As(got, &gs) || !errors.As(want, &ws) || gs.Status() != 404 || ws.Status() != 404 {
		t.Errorf("%s: err = %v, want an HTTP 404 like Do's", what, got)
	}
}

// rsForbidden fails unless got is AUTH's 403 want, unchanged.
func rsForbidden(t *testing.T, what string, got, want error) {
	t.Helper()
	var re *doc.ReassignError
	if errors.As(got, &re) {
		t.Fatalf("%s: err = ReassignError %+v, want AUTH's 403", what, *re)
	}
	if got == nil || got.Error() != want.Error() || !errors.Is(got, auth.ErrForbidden) || errors.Is(got, auth.ErrNotFound) {
		t.Errorf("%s: err = %v (%T), want AUTH's %v (%T) unchanged", what, got, got, want, want)
	}
}

// eligible runs Engine.EligibleOwners as p in a read tx on the pool of one.
func (e *rsEnv) eligible(t *testing.T, p auth.Principal, doctype string, id int64) ([]types.UserID, error) {
	t.Helper()
	var got []types.UserID
	err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		return e.DB.Read(ctx, func(tx *db.Tx) (err error) {
			got, err = e.Engine.EligibleOwners(ctx, tx, p, doctype, id)
			return err
		})
	})
	return got, err
}

// DOC-111
func TestDoc_Reassign_Rules(t *testing.T) {
	e := rsNew(t)

	t.Run("success", func(t *testing.T) {
		for _, c := range []struct {
			name  string
			state doc.State
			by    auth.Principal
		}{
			{"draft by owner", doc.StateDraft, e.Priya},
			{"draft by approver", doc.StateDraft, e.Ravi},
			{"rejected by owner", doc.StateRejected, e.Priya},
			{"rejected by approver", doc.StateRejected, e.Ravi},
		} {
			t.Run(c.name, func(t *testing.T) {
				d := e.order(t, c.state)
				before := e.snap(t, d.ID)
				if err := e.reassign(t, c.by, d.ID, d.Version, e.Kiran.ID); err != nil {
					t.Fatalf("Reassign to Kiran by %s: %v, want ok", c.by.Name, err)
				}
				after := e.Load(t, fixture.Order, d.ID)
				if after.CreatedBy != e.Kiran.ID {
					t.Errorf("created_by = %d, want Kiran (%d)", after.CreatedBy, e.Kiran.ID)
				}
				if after.Version != d.Version+1 {
					t.Errorf("Version = %d, want %d", after.Version, d.Version+1)
				}
				if after.State != c.state {
					t.Errorf("State = %q, want %q (reassign doesn't move the state)", after.State, c.state)
				}
				if n := e.AuditCount(t); n != before.AllEntries+1 {
					t.Errorf("AuditCount = %d, want %d (one new entry)", n, before.AllEntries+1)
				}
				hist, err := doc.History(t.Context(), e.DB, e.A, e.Kiran, fixture.Order, d.ID)
				if err != nil {
					t.Fatalf("History as the new owner: %v", err)
				}
				if len(hist) != before.DocEntries+1 {
					t.Fatalf("History rows = %d, want %d (one new)", len(hist), before.DocEntries+1)
				}
				last := hist[len(hist)-1]
				want := fmt.Sprintf(rsHandover, rsPriya, "Kiran Joshi")
				if last.Action != rsAction || last.By != c.by.ID || last.Reason != want {
					t.Errorf("new history row = {Action %q, By %d, Reason %q}, want {%q, %d, %q}",
						last.Action, last.By, last.Reason, rsAction, c.by.ID, want)
				}
			})
		}
	})

	t.Run("refused", func(t *testing.T) {
		for _, c := range []struct {
			name  string
			state doc.State
			by    auth.Principal
			to    func() types.UserID
			want  doc.ReassignReason
		}{
			{"approved", doc.StateApproved, e.Priya, func() types.UserID { return e.Kiran.ID }, doc.ReassignNotDraft},
			{"closed", doc.StateClosed, e.Priya, func() types.UserID { return e.Kiran.ID }, doc.ReassignNotDraft},
			{"cancelled", doc.StateCancelled, e.Ravi, func() types.UserID { return e.Kiran.ID }, doc.ReassignNotDraft},
			{"pending approval", doc.StatePending, e.Priya, func() types.UserID { return e.Kiran.ID }, doc.ReassignPending},
			{"to the owner, by approver", doc.StateDraft, e.Ravi, func() types.UserID { return e.Priya.ID }, doc.ReassignSameOwner},
			{"to the owner, by owner", doc.StateRejected, e.Priya, func() types.UserID { return e.Priya.ID }, doc.ReassignSameOwner},
			{"disabled Purchase User", doc.StateDraft, e.Priya, func() types.UserID { return e.Asha.ID }, doc.ReassignDisabled},
			// Disabled before CantCreate (CD-253): Former Staff also lacks create.
			{"disabled without create", doc.StateDraft, e.Ravi, func() types.UserID { return e.Former }, doc.ReassignDisabled},
			{"reader without create", doc.StateDraft, e.Priya, func() types.UserID { return e.Suresh.ID }, doc.ReassignCantCreate},
			// CantCreate before CantSee (CD-253): Deepak lacks both.
			{"neither create nor read", doc.StateDraft, e.Ravi, func() types.UserID { return e.Deepak.ID }, doc.ReassignCantCreate},
			// The restriction hides the order: create (the right) passes, read on the record fails.
			{"restricted to another project", doc.StateDraft, e.Priya, func() types.UserID { return e.Tara.ID }, doc.ReassignCantSee},
			// read and create pass, write on a copy owned by Uma fails.
			{"no write as owner", doc.StateDraft, e.Ravi, func() types.UserID { return e.Uma.ID }, doc.ReassignCantCreate},
		} {
			t.Run(c.name, func(t *testing.T) {
				d := e.order(t, c.state)
				before := e.snap(t, d.ID)
				err := e.reassign(t, c.by, d.ID, d.Version, c.to())
				wantReason(t, err, c.want)
				e.unchanged(t, d.ID, before)
			})
		}
	})

	// (c) The caller is the owner or holds approve (CanTx with the record),
	// checked before the version (d), the state (e) and SameOwner (f).
	t.Run("caller refused by AUTH", func(t *testing.T) {
		for _, c := range []struct {
			name  string
			by    func() auth.Principal
			state doc.State
			stale bool // post version-1
			to    func() types.UserID
		}{
			{"second Purchase User", func() auth.Principal { return e.Kiran }, doc.StateDraft, false, func() types.UserID { return e.Nisha.ID }},
			{"Stores reader", func() auth.Principal { return e.Suresh }, doc.StateDraft, false, func() types.UserID { return e.Nisha.ID }},
			{"to the owner", func() auth.Principal { return e.Kiran }, doc.StateDraft, false, func() types.UserID { return e.Priya.ID }},
			{"pending order", func() auth.Principal { return e.Kiran }, doc.StatePending, false, func() types.UserID { return e.Nisha.ID }},
			{"approved order", func() auth.Principal { return e.Suresh }, doc.StateApproved, false, func() types.UserID { return e.Nisha.ID }},
			{"stale version", func() auth.Principal { return e.Kiran }, doc.StateDraft, true, func() types.UserID { return e.Nisha.ID }},
		} {
			t.Run(c.name, func(t *testing.T) {
				d := e.order(t, c.state)
				by := c.by()
				want := e.authErr(t, by, d.ID, "approve")
				if !errors.Is(want, auth.ErrForbidden) {
					t.Fatalf("setup: AUTH's approve refusal for %s = %v, want a 403", by.Name, want)
				}
				before := e.snap(t, d.ID)
				version := d.Version
				if c.stale {
					version--
				}
				err := e.reassign(t, by, d.ID, version, c.to())
				var re *doc.ReassignError
				if errors.As(err, &re) {
					t.Fatalf("err = ReassignError %+v, want AUTH's 403", *re)
				}
				if err == nil || err.Error() != want.Error() || !errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) {
					t.Errorf("err = %v (%T), want AUTH's %v (%T) unchanged", err, err, want, want)
				}
				e.unchanged(t, d.ID, before)
			})
		}
	})

	// The owner loses the race (DOC-112's sequential case): after Ravi hands
	// Priya's draft to Kiran, Priya's reassign from the old version gets AUTH's
	// 403, because the right is checked before the version (CD-259).
	t.Run("owner after losing the draft", func(t *testing.T) {
		d := e.order(t, doc.StateDraft)
		if err := e.reassign(t, e.Ravi, d.ID, d.Version, e.Kiran.ID); err != nil {
			t.Fatalf("Reassign to Kiran by Ravi: %v, want ok", err)
		}
		want := e.authErr(t, e.Priya, d.ID, "approve")
		if !errors.Is(want, auth.ErrForbidden) {
			t.Fatalf("setup: AUTH's approve refusal for Priya = %v, want a 403", want)
		}
		before := e.snap(t, d.ID)
		err := e.reassign(t, e.Priya, d.ID, d.Version, e.Nisha.ID)
		var re *doc.ReassignError
		if errors.As(err, &re) {
			t.Fatalf("err = ReassignError %+v, want AUTH's 403", *re)
		}
		if err == nil || err.Error() != want.Error() || !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("err = %v (%T), want AUTH's %v (%T) unchanged", err, err, want, want)
		}
		e.unchanged(t, d.ID, before)
	})

	// (a) and (b): a missing id gets what Do returns for a missing document; a
	// record the caller can't read gets what Do returns for a hidden one, a 404
	// like the missing one (CD-245), before every other check.
	t.Run("missing or hidden", func(t *testing.T) {
		const missing = 999_999
		wantMissing := e.doErr(t, e.Priya, missing)
		rsSame(t, "missing id", e.reassign(t, e.Priya, missing, 1, e.Kiran.ID), wantMissing)
		rsSame(t, "missing id, by approver", e.reassign(t, e.Ravi, missing, 1, e.Kiran.ID), wantMissing)

		for _, c := range []struct {
			name  string
			by    func() auth.Principal
			state doc.State
			stale bool
			to    func() types.UserID
		}{
			// Vikram holds approve on orders, but not on this project.
			{"approver who can't see the order", func() auth.Principal { return e.Vikram }, doc.StateDraft, false, func() types.UserID { return e.Kiran.ID }},
			{"Purchase User who can't see the order", func() auth.Principal { return e.Tara }, doc.StateDraft, false, func() types.UserID { return e.Kiran.ID }},
			// (b) before (d), (e) and (f).
			{"hidden and stale", func() auth.Principal { return e.Vikram }, doc.StateDraft, true, func() types.UserID { return e.Kiran.ID }},
			{"hidden and pending", func() auth.Principal { return e.Vikram }, doc.StatePending, false, func() types.UserID { return e.Kiran.ID }},
			{"hidden, to the owner", func() auth.Principal { return e.Tara }, doc.StateDraft, false, func() types.UserID { return e.Priya.ID }},
		} {
			t.Run(c.name, func(t *testing.T) {
				d := e.order(t, c.state)
				by := c.by()
				want := e.doErr(t, by, d.ID)
				if !errors.Is(want, auth.ErrNotFound) {
					t.Fatalf("setup: Do as %s = %v, want a hidden-record 404", by.Name, want)
				}
				before := e.snap(t, d.ID)
				version := d.Version
				if c.stale {
					version--
				}
				rsSame(t, c.name, e.reassign(t, by, d.ID, version, c.to()), want)
				e.unchanged(t, d.ID, before)
			})
		}

		// The owner who can no longer see the order (moved to a project outside
		// Priya's restriction): (b) refuses even though (c) would pass.
		t.Run("owner who can't see the order", func(t *testing.T) {
			d := e.order(t, doc.StateDraft)
			e.Exec(t, `UPDATE fixture_order SET project_id = ? WHERE id = ?`, e.M.Project[rsTechpark], d.ID)
			want := e.doErr(t, e.Priya, d.ID)
			if !errors.Is(want, auth.ErrNotFound) {
				t.Fatalf("setup: Do as Priya = %v, want a hidden-record 404", want)
			}
			before := e.snap(t, d.ID)
			rsSame(t, "hidden owner", e.reassign(t, e.Priya, d.ID, d.Version, e.Kiran.ID), want)
			e.unchanged(t, d.ID, before)
		})
	})

	// (d) The version, after the caller check and before the state (e),
	// SameOwner (f) and the target (g).
	t.Run("stale version", func(t *testing.T) {
		for _, c := range []struct {
			name  string
			state doc.State
			by    func() auth.Principal
			to    func() types.UserID
		}{
			{"draft by owner", doc.StateDraft, func() auth.Principal { return e.Priya }, func() types.UserID { return e.Kiran.ID }},
			{"draft by approver", doc.StateDraft, func() auth.Principal { return e.Ravi }, func() types.UserID { return e.Kiran.ID }},
			{"approved", doc.StateApproved, func() auth.Principal { return e.Priya }, func() types.UserID { return e.Kiran.ID }},
			{"pending", doc.StatePending, func() auth.Principal { return e.Ravi }, func() types.UserID { return e.Kiran.ID }},
			{"to the owner", doc.StateDraft, func() auth.Principal { return e.Priya }, func() types.UserID { return e.Priya.ID }},
			{"to a disabled user", doc.StateDraft, func() auth.Principal { return e.Priya }, func() types.UserID { return e.Asha.ID }},
		} {
			t.Run(c.name, func(t *testing.T) {
				d := e.order(t, c.state)
				before := e.snap(t, d.ID)
				err := e.reassign(t, c.by(), d.ID, d.Version-1, c.to())
				_ = wantDocErr(t, err, doc.KindStale, fmt.Sprintf(rsStale, e.Display(t, fixture.Order, d.ID)))
				e.unchanged(t, d.ID, before)
			})
		}
	})

	// EligibleOwners: the users who pass the same target checks, without the
	// current owner. Of the fixture users only the Administrator holds create,
	// read and write on a METRO order besides Priya; of the added users only
	// Kiran and Nisha (Asha is disabled, Tara can't see it, Uma can't write).
	t.Run("eligible owners", func(t *testing.T) {
		d := e.order(t, doc.StateDraft)
		var got []types.UserID
		err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
			return e.DB.Read(ctx, func(tx *db.Tx) (err error) {
				got, err = e.Engine.EligibleOwners(ctx, tx, e.Priya, fixture.Order, d.ID)
				return err
			})
		})
		if err != nil {
			t.Fatalf("EligibleOwners: %v", err)
		}
		want := []types.UserID{e.Admin.ID, e.Kiran.ID, e.Nisha.ID}
		if got := slices.Sorted(slices.Values(got)); !slices.Equal(got, want) {
			t.Errorf("EligibleOwners = %v, want %v (Admin, Kiran, Nisha)", got, want)
		}
		// After a hand-over to Kiran, Kiran is the owner and Priya is eligible.
		if err := e.reassign(t, e.Priya, d.ID, d.Version, e.Kiran.ID); err != nil {
			t.Fatalf("Reassign to Kiran: %v", err)
		}
		err = e.DB.Read(t.Context(), func(tx *db.Tx) (err error) {
			got, err = e.Engine.EligibleOwners(t.Context(), tx, e.Kiran, fixture.Order, d.ID)
			return err
		})
		if err != nil {
			t.Fatalf("EligibleOwners after hand-over: %v", err)
		}
		want = []types.UserID{e.Admin.ID, e.Priya.ID, e.Nisha.ID}
		slices.Sort(want)
		if got := slices.Sorted(slices.Values(got)); !slices.Equal(got, want) {
			t.Errorf("EligibleOwners after hand-over = %v, want %v (Admin, Priya, Nisha)", got, want)
		}
	})

	// EligibleOwners runs Reassign's caller checks (a) to (c) before the
	// target checks (AD ruling).
	t.Run("eligible owners caller checks", func(t *testing.T) {
		d := e.order(t, doc.StateDraft)
		got, err := e.eligible(t, e.Ravi, fixture.Order, d.ID)
		want := []types.UserID{e.Admin.ID, e.Kiran.ID, e.Nisha.ID}
		if got := slices.Sorted(slices.Values(got)); err != nil || !slices.Equal(got, want) {
			t.Errorf("EligibleOwners as approver = (%v, %v), want (%v, nil)", got, err, want)
		}

		const missing = 999_999
		wantMissing := e.doErr(t, e.Priya, missing)
		got, err = e.eligible(t, e.Priya, fixture.Order, missing)
		rsSame(t, "missing id", err, wantMissing)
		if got != nil {
			t.Errorf("missing id: EligibleOwners = %v, want nil", got)
		}

		for _, c := range []struct {
			name string
			by   func() auth.Principal
		}{
			{"approver who can't see the order", func() auth.Principal { return e.Vikram }},
			{"Purchase User who can't see the order", func() auth.Principal { return e.Tara }},
		} {
			t.Run(c.name, func(t *testing.T) {
				by := c.by()
				want := e.doErr(t, by, d.ID)
				got, err := e.eligible(t, by, fixture.Order, d.ID)
				rsSame(t, c.name, err, want)
				if got != nil {
					t.Errorf("EligibleOwners = %v, want nil", got)
				}
			})
		}
		t.Run("owner who can't see the order", func(t *testing.T) {
			h := e.order(t, doc.StateDraft)
			e.Exec(t, `UPDATE fixture_order SET project_id = ? WHERE id = ?`, e.M.Project[rsTechpark], h.ID)
			want := e.doErr(t, e.Priya, h.ID)
			got, err := e.eligible(t, e.Priya, fixture.Order, h.ID)
			rsSame(t, "hidden owner", err, want)
			if got != nil {
				t.Errorf("EligibleOwners = %v, want nil", got)
			}
		})
		for _, c := range []struct {
			name string
			by   func() auth.Principal
		}{
			{"second Purchase User", func() auth.Principal { return e.Kiran }},
			{"Stores reader", func() auth.Principal { return e.Suresh }},
		} {
			t.Run(c.name, func(t *testing.T) {
				by := c.by()
				want := e.authErr(t, by, d.ID, "approve")
				got, err := e.eligible(t, by, fixture.Order, d.ID)
				rsForbidden(t, c.name, err, want)
				if got != nil {
					t.Errorf("EligibleOwners = %v, want nil", got)
				}
			})
		}
	})

	// An unknown DocType and a master get Do's gone error from both.
	t.Run("unknown or master DocType", func(t *testing.T) {
		for _, c := range []struct {
			name, doctype string
			id            func() int64
		}{
			{"unknown", "fixture.NoSuch", func() int64 { return 1 }},
			{"master", fixture.Party, func() int64 { return e.M.Party["SHREE"] }},
		} {
			t.Run(c.name, func(t *testing.T) {
				id := c.id()
				_, want := e.Engine.Do(t.Context(), e.Priya, c.doctype, id, doc.ActionApprove, doc.ActionInput{Version: 1})
				_ = wantDocErr(t, want, doc.KindNotFound, msgGone) // setup: Do's gone error
				count := e.AuditCount(t)
				err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
					return e.Engine.Reassign(ctx, e.Priya, c.doctype, id, 1, e.Kiran.ID)
				})
				_ = wantDocErr(t, err, doc.KindNotFound, msgGone)
				if !errors.Is(err, doc.ErrNotFound) {
					t.Errorf("Reassign: errors.Is(err, doc.ErrNotFound) = false (err %v)", err)
				}
				got, err := e.eligible(t, e.Priya, c.doctype, id)
				_ = wantDocErr(t, err, doc.KindNotFound, msgGone)
				if !errors.Is(err, doc.ErrNotFound) {
					t.Errorf("EligibleOwners: errors.Is(err, doc.ErrNotFound) = false (err %v)", err)
				}
				if got != nil {
					t.Errorf("EligibleOwners = %v, want nil", got)
				}
				if n := e.AuditCount(t); n != count {
					t.Errorf("AuditCount = %d, want %d", n, count)
				}
			})
		}
	})

	// The reason text is for logs: it names the reason and carries none of
	// the module's user text (PO spec §21.2 owns those).
	t.Run("ReassignError text", func(t *testing.T) {
		userText := []string{
			"can't be handed over", "Withdraw this order", "is already", "can't raise",
			"can't see this order", "can't sign in", "Pick someone",
		}
		for _, r := range []doc.ReassignReason{
			doc.ReassignPending, doc.ReassignNotDraft, doc.ReassignSameOwner,
			doc.ReassignDisabled, doc.ReassignCantCreate, doc.ReassignCantSee,
		} {
			msg := (&doc.ReassignError{Reason: r}).Error()
			if r == "" || msg == "" || !strings.Contains(msg, string(r)) {
				t.Errorf("ReassignError{%q}.Error() = %q, want a non-empty text naming the reason", r, msg)
			}
			for _, u := range userText {
				if strings.Contains(msg, u) {
					t.Errorf("ReassignError{%q}.Error() = %q carries user text %q", r, msg, u)
				}
			}
		}
	})

	// A busy database is Do's 503 (DOC-075), and nothing is written.
	t.Run("busy database", func(t *testing.T) {
		h := fixturetest.New(t)
		res := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"})
		d := h.Load(t, fixture.Order, res.ID)
		e2 := h.EngineOn(p2lOpen(t, db.WithBusyTimeout(200*time.Millisecond)))
		holder := p2lOpen(t)
		count := h.AuditCount(t)

		wait := p2lHoldWrite(t, holder, time.Second)
		err := e2.Reassign(t.Context(), h.Priya, fixture.Order, d.ID, d.Version, h.Ravi.ID)
		wait()
		fixturetest.ErrIs(t, err, doc.KindBusy, p2lBusyText)
		_ = lkHTTP(t, err, 503, p2lBusyText)
		if !errors.Is(err, db.ErrBusy) {
			t.Errorf("errors.Is(err, db.ErrBusy) = false (err %v)", err)
		}
		fixturetest.Eq(t, h.Load(t, fixture.Order, d.ID), d)
		if got := h.AuditCount(t); got != count {
			t.Errorf("AuditCount = %d, want %d (nothing written)", got, count)
		}
	})
}

// rsRuns is how many times DOC-112 repeats the race, each on a fresh draft.
const rsRuns = 10

// DOC-112 (AD ruling: two non-admin approvers race, so the loser fails only on
// the version)
func TestDoc_Reassign_Concurrent(t *testing.T) {
	e := rsNew(t)
	for run := range rsRuns {
		d := trRefPOAt(t, e.H, 5)
		before := len(e.Audit(t, fixture.Order, d.ID))
		count := e.AuditCount(t)

		type call struct {
			by auth.Principal
			to types.UserID
		}
		// Two non-admin holders of approve (Purchase Manager, Director), so the
		// loser keeps its right and fails only on the version (AD ruling: if the
		// owner lost, it would lose its right first; see the owner-loses row).
		calls := []call{{e.Ravi, e.Kiran.ID}, {e.Meena, e.Nisha.ID}}
		errs := make([]error, len(calls))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i, c := range calls {
			wg.Go(func() {
				<-start
				errs[i] = e.Engine.Reassign(t.Context(), c.by, fixture.Order, d.ID, 5, c.to)
			})
		}
		close(start)
		wg.Wait()

		winner := -1
		for i, err := range errs {
			if err == nil {
				if winner >= 0 {
					t.Fatalf("run %d: both reassigns succeeded, want exactly one", run)
				}
				winner = i
			}
		}
		if winner < 0 {
			t.Fatalf("run %d: no reassign succeeded (errs %v, %v), want exactly one", run, errs[0], errs[1])
		}
		_ = wantDocErr(t, errs[1-winner], doc.KindStale, fmt.Sprintf(rsStale, trDraft(d.ID)))

		after := e.Load(t, fixture.Order, d.ID)
		if after.Version != 6 {
			t.Errorf("run %d: Version = %d, want 6", run, after.Version)
		}
		if after.CreatedBy != calls[winner].to { //nolint:gosec // G602: winner is 0 or 1, checked above
			t.Errorf("run %d: created_by = %d, want the winner's target %d", run, after.CreatedBy, calls[winner].to) //nolint:gosec // G602: winner is 0 or 1, checked above
		}
		entries := e.Audit(t, fixture.Order, d.ID)
		if len(entries) != before+1 {
			t.Fatalf("run %d: audit entries = %d, want %d (exactly one new)", run, len(entries), before+1)
		}
		if got := rsCount(entries, rsAction); got != 1 {
			t.Errorf("run %d: reassign audit entries = %d, want 1", run, got)
		}
		if n := e.AuditCount(t); n != count+1 {
			t.Errorf("run %d: AuditCount = %d, want %d", run, n, count+1)
		}
	}
}

// rsCount counts the entries whose Action is action.
func rsCount(entries []audit.Entry, action string) int {
	n := 0
	for _, x := range entries {
		if x.Action == action {
			n++
		}
	}
	return n
}
