package modkit_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// Messages (M0-7 AD rulings): the admin-only refusal is its own text
// (CD-147, CD-152); stale is DOC's DOC-069 wording with the label
// "Business settings".
const (
	msgAdminOnly = "Only an administrator can change business settings."
	msgStale     = "Someone else changed Business settings after you opened it. Reload it and make your changes again."

	adminEmail  = "admin@vardhan.test"  // Administrator
	meenaEmail  = "meena@vardhan.test"  // Director
	raviEmail   = "ravi@vardhan.test"   // Purchase Manager, Employee
	deepakEmail = "deepak@vardhan.test" // Employee

	terms = "Payment within 30 days of the invoice."
)

var (
	mar31 = types.Date{Year: 2026, Month: 3, Day: 31}
	jun30 = types.Date{Year: 2026, Month: 6, Day: 30}
)

// env is modkit on the canonical fixtures, the real auth.Service (PO-039 is
// restriction-free but role-dependent, so it runs on the real AUTH) and an
// audit recorder.
type env struct {
	db  *db.DB
	kit *modkit.Kit
	rec *auditfake.Recorder
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d := testkit.NewDB(t)
	testkit.Seed(t, d)
	a := auth.New(auth.Deps{DB: d, Clock: testkit.Clock()})
	rec := auditfake.New()
	return &env{db: d, rec: rec, kit: modkit.New(modkit.Deps{DB: d, Auth: a, Audit: rec, Clock: testkit.Clock()})}
}

func lock(d types.Date) sql.Null[types.Date] { return sql.Null[types.Date]{V: d, Valid: true} }

// get reads the settings as the Administrator.
func (e *env) get(t *testing.T) modkit.Settings {
	t.Helper()
	s, err := e.kit.GetSettings(t.Context(), testkit.Principal(t, adminEmail))
	if err != nil {
		t.Fatalf("GetSettings as Administrator: %v", err)
	}
	return s
}

// save saves in as the Administrator from version and fails on any error.
func (e *env) save(t *testing.T, version int, in modkit.SettingsInput) {
	t.Helper()
	if err := e.kit.SaveSettings(t.Context(), testkit.Principal(t, adminEmail), version, in); err != nil {
		t.Fatalf("SaveSettings(version %d, %+v) as Administrator: %v", version, in, err)
	}
}

// wantRefusal checks err is an HTTP error with status and exactly msg.
func wantRefusal(t *testing.T, call string, err error, status int, msg string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: nil error, want %d %q", call, status, msg)
		return
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		t.Errorf("%s: error %q (%T) is not a doctype.HTTPError, want %d %q", call, err, err, status, msg)
		return
	}
	if he.Status() != status || err.Error() != msg {
		t.Errorf("%s: %d %q, want %d %q", call, he.Status(), err.Error(), status, msg)
	}
}

func wantForbidden(t *testing.T, call string, err error) {
	t.Helper()
	wantRefusal(t, call, err, 403, msgAdminOnly)
	if err != nil && !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("%s: %q does not match auth.ErrForbidden", call, err)
	}
}

func wantSettings(t *testing.T, call string, got, want modkit.Settings) {
	t.Helper()
	if got != want {
		t.Errorf("%s: settings %+v, want %+v", call, got, want)
	}
}

// dateText reports whether an audited date value shows d. The stored text
// (ISO) and the display form (dd-mm-yyyy) are both accepted: the rulings name
// neither.
func dateText(got string, d types.Date) bool {
	return got == d.ISO() || got == types.FormatDate(d)
}

// wantChanges checks e's changes are exactly the listed fields, each a "set"
// from before to after. A date field's value is compared with dateText.
type change struct {
	field         string
	before, after string
	dates         [2]*types.Date // non-nil: compare that side with dateText
}

func wantChanges(t *testing.T, call string, e audit.Entry, want []change) {
	t.Helper()
	got := map[string]audit.Change{}
	for _, c := range e.Changes {
		if c.Op != audit.OpSet {
			t.Errorf("%s: change %+v has Op %q, want %q", call, c, c.Op, audit.OpSet)
		}
		got[c.Field] = c
	}
	if len(got) != len(want) || len(e.Changes) != len(want) {
		t.Errorf("%s: changes %+v, want exactly the fields %+v", call, e.Changes, want)
	}
	side := func(v, plain string, d *types.Date) bool {
		if d != nil {
			return dateText(v, *d)
		}
		return v == plain
	}
	for _, w := range want {
		c, ok := got[w.field]
		if !ok {
			t.Errorf("%s: no change for %s in %+v", call, w.field, e.Changes)
			continue
		}
		if !side(c.Before, w.before, w.dates[0]) || !side(c.After, w.after, w.dates[1]) {
			t.Errorf("%s: %s changed %q → %q, want %q → %q (dates %v)", call, w.field, c.Before, c.After, w.before, w.after, w.dates)
		}
	}
}

// PO-039: only an Administrator reads or saves business settings; each save
// is audited with before and after, locking and unlocking included; a stale
// version is refused and changes nothing (CD-94).
func TestSettings_AdminOnlyAudited(t *testing.T) {
	e := newEnv(t)
	ctx := t.Context()
	admin := testkit.Principal(t, adminEmail)

	// Seeded by the migration: no lock, no terms, version 1.
	initial := modkit.Settings{Version: 1}
	wantSettings(t, "GetSettings on a new database", e.get(t), initial)

	t.Run("non_admin_refused", func(t *testing.T) {
		for _, email := range []string{meenaEmail, raviEmail, deepakEmail} {
			p := testkit.Principal(t, email)
			_, err := e.kit.GetSettings(ctx, p)
			wantForbidden(t, "GetSettings as "+email, err)
			err = e.kit.SaveSettings(ctx, p, 1, modkit.SettingsInput{LockedUpTo: lock(mar31), DefaultPOTerms: terms})
			wantForbidden(t, "SaveSettings as "+email, err)
		}
		wantSettings(t, "settings after the refused saves", e.get(t), initial)
		if n := len(e.rec.Entries()); n != 0 {
			t.Errorf("%d audit entries after refused saves, want 0: %+v", n, e.rec.Entries())
		}
	})

	// The role is read from AUTH's tables inside the transaction (HasRoleTx),
	// not taken from the principal's Roles: a principal claiming Administrator
	// that AUTH doesn't know as one is refused.
	t.Run("role_read_from_auth", func(t *testing.T) {
		forged := testkit.Principal(t, deepakEmail)
		forged.Roles = append(forged.Roles, auth.RoleAdministrator)
		_, err := e.kit.GetSettings(ctx, forged)
		wantForbidden(t, "GetSettings as Deepak claiming Administrator", err)
		err = e.kit.SaveSettings(ctx, forged, 1, modkit.SettingsInput{LockedUpTo: lock(mar31), DefaultPOTerms: terms})
		wantForbidden(t, "SaveSettings as Deepak claiming Administrator", err)
		wantSettings(t, "settings after the forged saves", e.get(t), initial)
		if n := len(e.rec.Entries()); n != 0 {
			t.Errorf("%d audit entries after forged saves, want 0", n)
		}
	})

	t.Run("admin_saves_audited", func(t *testing.T) {
		// Lock the period and set the terms.
		e.save(t, 1, modkit.SettingsInput{LockedUpTo: lock(mar31), DefaultPOTerms: terms})
		wantSettings(t, "after locking", e.get(t), modkit.Settings{LockedUpTo: lock(mar31), DefaultPOTerms: terms, Version: 2})

		// Move the lock forward; the terms stay.
		e.save(t, 2, modkit.SettingsInput{LockedUpTo: lock(jun30), DefaultPOTerms: terms})
		wantSettings(t, "after moving the lock", e.get(t), modkit.Settings{LockedUpTo: lock(jun30), DefaultPOTerms: terms, Version: 3})

		// Unlock by clearing the date.
		e.save(t, 3, modkit.SettingsInput{DefaultPOTerms: terms})
		wantSettings(t, "after unlocking", e.get(t), modkit.Settings{DefaultPOTerms: terms, Version: 4})

		entries := e.rec.Entries()
		if len(entries) != 3 {
			t.Fatalf("%d audit entries after three saves, want 3 (one each): %+v", len(entries), entries)
		}
		for i, en := range entries {
			if en.By != admin.ID {
				t.Errorf("audit entry %d By = %d, want the Administrator %d", i, en.By, admin.ID)
			}
		}
		wantChanges(t, "lock audit", entries[0], []change{
			{field: "locked_up_to", dates: [2]*types.Date{nil, &mar31}},
			{field: "default_po_terms", before: "", after: terms},
		})
		wantChanges(t, "move audit", entries[1], []change{
			{field: "locked_up_to", dates: [2]*types.Date{&mar31, &jun30}},
		})
		wantChanges(t, "unlock audit", entries[2], []change{
			{field: "locked_up_to", after: "", dates: [2]*types.Date{&jun30, nil}},
		})
	})

	t.Run("stale_refused", func(t *testing.T) {
		before := e.get(t)
		n := len(e.rec.Entries())
		err := e.kit.SaveSettings(ctx, admin, before.Version-1, modkit.SettingsInput{LockedUpTo: lock(mar31), DefaultPOTerms: "Stale terms."})
		wantRefusal(t, "SaveSettings from an old version", err, 409, msgStale)
		wantSettings(t, "settings after the stale save", e.get(t), before)
		if got := len(e.rec.Entries()); got != n {
			t.Errorf("audit entries %d after the stale save, want %d (none added)", got, n)
		}
	})
}

// Two Administrators save from the same version at once: exactly one wins,
// the other gets the stale text, version moves by one and one audit entry is
// written (DOC-071's shape; the version is the UPDATE's condition, CD-94).
func TestSettings_ParallelSavesOneWins(t *testing.T) {
	e := newEnv(t)
	admin := testkit.Principal(t, adminEmail)
	inputs := []modkit.SettingsInput{
		{LockedUpTo: lock(mar31), DefaultPOTerms: "Terms from the first tab."},
		{LockedUpTo: lock(jun30), DefaultPOTerms: "Terms from the second tab."},
	}
	errs := make([]error, len(inputs))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, in := range inputs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = e.kit.SaveSettings(context.Background(), admin, 1, in)
		}()
	}
	close(start)
	wg.Wait()

	winner := -1
	for i, err := range errs {
		if err == nil {
			if winner >= 0 {
				t.Errorf("saves %d and %d both succeeded from version 1", winner, i)
			}
			winner = i
			continue
		}
		wantRefusal(t, "the losing save", err, 409, msgStale)
	}
	if winner < 0 {
		t.Fatalf("no save succeeded: %v", errs)
	}
	want := modkit.Settings{LockedUpTo: inputs[winner].LockedUpTo, DefaultPOTerms: inputs[winner].DefaultPOTerms, Version: 2} //nolint:gosec // G602: winner is 0 or 1, checked above
	wantSettings(t, "settings after the race", e.get(t), want)
	if n := len(e.rec.Entries()); n != 1 {
		t.Errorf("%d audit entries after the race, want 1", n)
	}
}

// CD-213, CD-224: GetSettings and SaveSettings take no second connection,
// for an Administrator and for a refused user alike (the role check is
// HasRoleTx in the caller's transaction).
func TestSettings_NoSecondConnection(t *testing.T) {
	e := newEnv(t)
	admin := testkit.Principal(t, adminEmail)
	deepak := testkit.Principal(t, deepakEmail)
	in := modkit.SettingsInput{LockedUpTo: lock(mar31), DefaultPOTerms: terms}

	err := testkit.NoSecondConn(t, e.db, func(ctx context.Context) error {
		return e.kit.SaveSettings(ctx, deepak, 1, in)
	})
	wantForbidden(t, "SaveSettings as Deepak on one connection", err)
	err = testkit.NoSecondConn(t, e.db, func(ctx context.Context) error {
		_, err := e.kit.GetSettings(ctx, deepak)
		return err
	})
	wantForbidden(t, "GetSettings as Deepak on one connection", err)

	if err := testkit.NoSecondConn(t, e.db, func(ctx context.Context) error {
		return e.kit.SaveSettings(ctx, admin, 1, in)
	}); err != nil {
		t.Fatalf("SaveSettings as Administrator on one connection: %v", err)
	}
	var got modkit.Settings
	if err := testkit.NoSecondConn(t, e.db, func(ctx context.Context) error {
		var err error
		got, err = e.kit.GetSettings(ctx, admin)
		return err
	}); err != nil {
		t.Fatalf("GetSettings as Administrator on one connection: %v", err)
	}
	wantSettings(t, "GetSettings on one connection", got, modkit.Settings{LockedUpTo: lock(mar31), DefaultPOTerms: terms, Version: 2})
}

// AD ruling (company's unchanged-save rule, CD-208): a save whose values
// equal the stored ones succeeds and writes nothing: the version stays and no
// audit entry is added. Checked on the seeded row and after a real save.
func TestSettings_UnchangedSaveWritesNothing(t *testing.T) {
	e := newEnv(t)

	e.save(t, 1, modkit.SettingsInput{})
	wantSettings(t, "after an unchanged save of the seeded row", e.get(t), modkit.Settings{Version: 1})
	if n := len(e.rec.Entries()); n != 0 {
		t.Errorf("%d audit entries after an unchanged save of the seeded row, want 0", n)
	}

	in := modkit.SettingsInput{LockedUpTo: lock(mar31), DefaultPOTerms: terms}
	e.save(t, 1, in)
	e.save(t, 2, in)
	wantSettings(t, "after saving the same values again", e.get(t), modkit.Settings{LockedUpTo: lock(mar31), DefaultPOTerms: terms, Version: 2})
	if n := len(e.rec.Entries()); n != 1 {
		t.Errorf("%d audit entries, want 1 (the real save only)", n)
	}
}
