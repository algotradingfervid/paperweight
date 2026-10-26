package employee_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

const msgPickUser = "Pick an existing, enabled user." // MST-112

// wantUserFieldError is wantFieldError with the error on user_id.
func wantUserFieldError(t *testing.T, err error, msg string) {
	t.Helper()
	wantFieldError(t, err, msg)
	var ve *mstkit.ValidationError
	if errors.As(err, &ve) && len(ve.FieldErrors()) == 1 && ve.FieldErrors()[0].Field != "user_id" {
		t.Errorf("FieldErrors()[0].Field = %q; want user_id", ve.FieldErrors()[0].Field)
	}
}

// snap is the employee rows and every user's enabled flag, for "nothing
// changed" checks and the property test's invariants.
type snap struct {
	dump    string
	emps    map[int64]snapEmp
	ids     []int64 // employee ids in id order
	enabled map[auth.UserID]bool
}

type snapEmp struct {
	code    string
	status  employee.Status
	user    auth.UserID
	version int
}

func (w *world) snapshot(t *testing.T) snap {
	t.Helper()
	ctx := context.Background()
	s := snap{emps: map[int64]snapEmp{}, enabled: map[auth.UserID]bool{}}
	var b strings.Builder
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, `SELECT id, code, status, IFNULL(user_id, 0), IFNULL(manager_id, 0),
			IFNULL(relieving_date, ''), disabled, version FROM employee ORDER BY id`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, user, manager, disabled int64
			var code, status, relieving string
			var version int
			if err := rows.Scan(&id, &code, &status, &user, &manager, &relieving, &disabled, &version); err != nil {
				return err
			}
			fmt.Fprintf(&b, "emp %d %s %s user=%d mgr=%d rel=%s dis=%d v=%d\n", id, code, status, user, manager, relieving, disabled, version)
			s.emps[id] = snapEmp{code: code, status: employee.Status(status), user: auth.UserID(user), version: version}
			s.ids = append(s.ids, id)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		urows, err := tx.SQL().QueryContext(ctx, `SELECT id, enabled FROM users ORDER BY id`)
		if err != nil {
			return err
		}
		defer func() { _ = urows.Close() }()
		for urows.Next() {
			var id, enabled int64
			if err := urows.Scan(&id, &enabled); err != nil {
				return err
			}
			fmt.Fprintf(&b, "user %d enabled=%d\n", id, enabled)
			s.enabled[auth.UserID(id)] = enabled != 0
		}
		return urows.Err()
	}); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	s.dump = b.String()
	return s
}

// wantUserRefusal checks err is a 422 *mstkit.ValidationError with an error
// on user_id (any message).
func wantUserRefusal(t *testing.T, what string, err error) {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("%s: err = %v (%T); want *mstkit.ValidationError on user_id", what, err, err)
	}
	if ve.Status() != 422 {
		t.Errorf("%s: Status() = %d; want 422", what, ve.Status())
	}
	for _, fe := range ve.FieldErrors() {
		if fe.Field == "user_id" {
			return
		}
	}
	t.Errorf("%s: FieldErrors() = %+v; want one on user_id", what, ve.FieldErrors())
}

// TestEmployee_CreateLeftLeavesUsersAlone: a create with status Left never
// enables or disables a user (CD-181). 06 and the plan don't say whether a
// Left create may take the user of an active employee; per the director's
// brief it is refused (on user_id) and saves only when the user is unlinked.
func TestEmployee_CreateLeftLeavesUsersAlone(t *testing.T) { // MST-111, MST-119, CD-181
	ctx := context.Background()
	w := newWorld(t)
	w.seedFixtureEmployees(t)
	ravi := userID(t, "ravi@vardhan.test") // EMP-003's, active
	before := w.snapshot(t)
	in := w.input("EMP-020", "Old Ravi Record")
	in.UserID, in.Status, in.Relieving = ravi, employee.Left, date(2025, time.January, 1)
	_, err := w.svc.Create(ctx, w.env.Kavya, in)
	if !w.userEnabled(t, ravi) {
		t.Errorf("ravi (EMP-003's user) disabled by a Left create of EMP-020; want enabled")
	}
	wantUserRefusal(t, "Create(EMP-020, Left, ravi)", err)
	w.wantNoCode(t, "EMP-020")
	if after := w.snapshot(t); after.dump != before.dump {
		t.Errorf("state changed by the refused create:\nbefore\n%s\nafter\n%s", before.dump, after.dump)
	}

	// Unlinked users: the create saves and the user's state stays as it was.
	w = newWorld(t)
	deepak, old := userID(t, "deepak@vardhan.test"), userID(t, "old@vardhan.test")
	for _, c := range []struct {
		code    string
		user    auth.UserID
		enabled bool
	}{
		{"EMP-007", deepak, true},
		{"EMP-008", old, false},
	} {
		in := w.input(c.code, "Former "+c.code)
		in.UserID, in.Status, in.Relieving = c.user, employee.Left, date(2026, time.March, 31)
		saved := w.create(t, in)
		if e := w.get(t, saved.ID); e.Status != employee.Left || e.UserID != c.user {
			t.Errorf("%s = {Status %q, UserID %d}; want {Left, %d}", c.code, e.Status, e.UserID, c.user)
		}
		if got := w.userEnabled(t, c.user); got != c.enabled {
			t.Errorf("user %d enabled = %v after a Left create of %s; want %v (unchanged)", c.user, got, c.code, c.enabled)
		}
		es := w.entries(t, "User", int64(c.user))
		if hasAction(es, "disable") || hasAction(es, "enable") {
			t.Errorf("User %d audit has an enable or disable entry after a Left create of %s; want none", c.user, c.code)
		}
	}
}

// TestEmployee_LeftUserChangeRefused: a Left → Left save that changes user_id
// (to another user, a disabled unlinked user or none) is refused (CD-181).
// The reviewer's sequence (swap to the disabled old@, then re-join) can't
// end with old@ enabled.
func TestEmployee_LeftUserChangeRefused(t *testing.T) { // MST-119, CD-181
	w := newWorld(t)
	deepak, suresh, old := userID(t, "deepak@vardhan.test"), userID(t, "suresh@vardhan.test"), userID(t, "old@vardhan.test")
	in := w.input("EMP-007", "Deepak Nair")
	in.UserID = deepak
	saved := w.create(t, in)
	if err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Left, date(2026, time.October, 24)
	}); err != nil {
		t.Fatalf("mark EMP-007 Left: %v", err)
	}
	version := w.versions["EMP-007"]

	for _, c := range []struct {
		name string
		user auth.UserID
	}{
		{"disabled unlinked old@", old},
		{"enabled unlinked suresh", suresh},
		{"no user", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := w.snapshot(t)
			err := w.update(t, "EMP-007", func(in *employee.Input) { in.UserID = c.user })
			wantUserFieldError(t, err, msgSeparateSaves)
			if e := w.get(t, saved.ID); e.Status != employee.Left || e.UserID != deepak || e.Version != version {
				t.Errorf("EMP-007 = {Status %q, UserID %d, Version %d}; want {Left, %d, %d}", e.Status, e.UserID, e.Version, deepak, version)
			}
			if after := w.snapshot(t); after.dump != before.dump {
				t.Errorf("state changed by the refused save:\nbefore\n%s\nafter\n%s", before.dump, after.dump)
			}
		})
	}

	// A Left → Left save keeping the user still saves.
	if err := w.update(t, "EMP-007", func(in *employee.Input) { in.Relieving = date(2026, time.October, 20) }); err != nil {
		t.Fatalf("correct Left EMP-007's relieving date: %v; want saved", err)
	}
	// Re-joining (with whatever user the save above kept) re-enables deepak only.
	_ = w.update(t, "EMP-007", func(in *employee.Input) { in.Status, in.Relieving = employee.Active, types.Date{} })
	if w.userEnabled(t, old) {
		t.Errorf("old@ enabled after the Left swap and re-join; want still disabled")
	}
	if e := w.get(t, saved.ID); e.Status != employee.Active || e.UserID != deepak {
		t.Errorf("EMP-007 after re-join = {Status %q, UserID %d}; want {Active, %d}", e.Status, e.UserID, deepak)
	}
	if !w.userEnabled(t, deepak) {
		t.Errorf("deepak disabled after EMP-007 re-joined; want enabled")
	}
}

// TestEmployee_UserMustExist: an unknown user (999) and the disabled
// old@vardhan.test are refused on an Active create and on an Active → Active
// user change; re-joining with the same, now disabled, user is allowed and
// re-enables it (CD-181).
func TestEmployee_UserMustExist(t *testing.T) { // MST-112
	ctx := context.Background()
	w := newWorld(t)
	old := userID(t, "old@vardhan.test")
	users := []struct {
		name string
		id   auth.UserID
	}{{"unknown 999", 999}, {"disabled old@", old}}

	for i, u := range users {
		t.Run("create "+u.name, func(t *testing.T) {
			code := fmt.Sprintf("EMP-%03d", 11+i)
			in := w.input(code, "Asha Menon")
			in.UserID = u.id
			_, err := w.svc.Create(ctx, w.env.Kavya, in)
			wantUserFieldError(t, err, msgPickUser)
			w.wantNoCode(t, code)
		})
	}
	if w.userEnabled(t, old) {
		t.Errorf("old@ enabled after the refused creates; want disabled")
	}

	w = newWorld(t) // a fresh world, so a create wrongly saved above can't hold old@
	saved := w.create(t, w.input("EMP-009", "Asha Menon"))
	for _, u := range users {
		t.Run("change to "+u.name, func(t *testing.T) {
			err := w.update(t, "EMP-009", func(in *employee.Input) { in.UserID = u.id })
			wantUserFieldError(t, err, msgPickUser)
			if e := w.get(t, saved.ID); e.UserID != 0 || e.Version != saved.Version || e.Status != employee.Active {
				t.Errorf("EMP-009 = {UserID %d, Version %d, Status %q}; want {0, %d, Active}", e.UserID, e.Version, e.Status, saved.Version)
			}
		})
	}
	if w.userEnabled(t, old) {
		t.Errorf("old@ enabled after the refused changes; want disabled")
	}
	suresh := userID(t, "suresh@vardhan.test")
	if err := w.update(t, "EMP-009", func(in *employee.Input) { in.UserID = suresh }); err != nil {
		t.Fatalf("Active → Active change of EMP-009 to the enabled, unlinked suresh: %v; want saved", err)
	}

	// The exception: re-joining with the same user that leaving disabled.
	deepak := userID(t, "deepak@vardhan.test")
	in := w.input("EMP-007", "Deepak Nair")
	in.UserID = deepak
	w.create(t, in)
	if err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Left, date(2026, time.October, 24)
	}); err != nil {
		t.Fatalf("mark EMP-007 Left: %v", err)
	}
	if w.userEnabled(t, deepak) {
		t.Fatalf("deepak enabled after EMP-007 Left; want disabled")
	}
	if err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Active, types.Date{}
	}); err != nil {
		t.Fatalf("re-join EMP-007 with the disabled deepak: %v; want saved", err)
	}
	if !w.userEnabled(t, deepak) {
		t.Errorf("deepak disabled after EMP-007 re-joined; want enabled")
	}
}

// TestEmployee_UserChangeUniqueAmongActive: MST-111 holds on an Active →
// Active user change and on a re-join (CD-181).
func TestEmployee_UserChangeUniqueAmongActive(t *testing.T) { // MST-111
	ctx := context.Background()
	w := newWorld(t)
	w.seedFixtureEmployees(t)
	deepak, suresh := userID(t, "deepak@vardhan.test"), userID(t, "suresh@vardhan.test")
	version := w.versions["EMP-005"]

	err := w.update(t, "EMP-005", func(in *employee.Input) { in.UserID = deepak })
	wantUserFieldError(t, err, msgUserLinked)
	if e := w.get(t, w.ids["EMP-005"]); e.UserID != suresh || e.Version != version {
		t.Errorf("EMP-005 = {UserID %d, Version %d}; want {%d, %d}", e.UserID, e.Version, suresh, version)
	}

	// EMP-007 leaves; an Administrator re-enables deepak; EMP-009 takes it.
	if err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Left, date(2026, time.October, 24)
	}); err != nil {
		t.Fatalf("mark EMP-007 Left: %v", err)
	}
	if err := w.env.DB.Write(ctx, func(tx *db.Tx) error {
		return w.env.Kit.Auth.EnableUser(ctx, tx, deepak)
	}); err != nil {
		t.Fatalf("EnableUser(deepak): %v", err)
	}
	in := w.input("EMP-009", "Deepak Nair")
	in.UserID = deepak
	w.create(t, in)

	version = w.versions["EMP-007"]
	err = w.update(t, "EMP-007", func(in *employee.Input) { in.Status, in.Relieving = employee.Active, types.Date{} })
	wantUserFieldError(t, err, "User deepak@vardhan.test is already linked to employee EMP-009.")
	if e := w.get(t, w.ids["EMP-007"]); e.Status != employee.Left || e.Version != version {
		t.Errorf("EMP-007 = {Status %q, Version %d}; want {Left, %d}", e.Status, e.Version, version)
	}
	if !w.userEnabled(t, deepak) {
		t.Errorf("deepak disabled after EMP-007's refused re-join; want enabled (EMP-009's)")
	}
}

// staleEmp is DOC's stale wording for EMP-009 (mstkit msgStale, M2).
const staleEmp = "Someone else changed Employee EMP-009 after you opened it. Reload it and make your changes again."

func wantStale(t *testing.T, what string, err error) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) {
		t.Errorf("%s = %T %v; want *doc.Error", what, err, err)
		return
	}
	if de.Kind != doc.KindStale || err.Error() != staleEmp {
		t.Errorf("%s = {Kind %q, %q}; want {%q, %q}", what, de.Kind, err.Error(), doc.KindStale, staleEmp)
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != 409 {
		t.Errorf("%s is not a 409 doctype.HTTPError", what)
	}
}

// TestEmployee_SetDisabledNoopChecksVersionAndRights: SetDisabledTx calls
// Auth.Can and CheckVersion before its no-op short-circuit (CD-181).
func TestEmployee_SetDisabledNoopChecksVersionAndRights(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	saved := w.create(t, w.input("EMP-009", "Asha Menon"))
	id := saved.ID
	tx := func(p auth.Principal, version int, disabled bool) error {
		return w.env.DB.Write(ctx, func(tx *db.Tx) error {
			return w.svc.SetDisabledTx(ctx, tx, p, id, version, disabled)
		})
	}

	// Active row, SetDisabled(false) is a no-op.
	wantStale(t, "SetDisabledTx(Active EMP-009, stale version, false)", tx(w.env.Kavya, saved.Version+5, false))
	wantStale(t, "SetDisabled(Active EMP-009, stale version, false)", w.svc.SetDisabled(ctx, w.env.Kavya, id, saved.Version+5, false))
	if err := tx(w.env.Priya, saved.Version, false); !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("SetDisabledTx(Priya, Active EMP-009, false) = %v; want auth.ErrForbidden", err)
	}
	if err := tx(w.env.Kavya, saved.Version, false); err != nil {
		t.Errorf("SetDisabledTx(Kavya, current version, false) on Active = %v; want nil", err)
	}

	// Left row, SetDisabled(true) is a no-op.
	in := w.input("EMP-009", "Asha Menon")
	in.Relieving = date(2026, time.October, 24)
	s2, err := w.svc.Update(ctx, w.env.Kavya, id, saved.Version, in)
	if err != nil {
		t.Fatalf("set EMP-009's relieving date: %v", err)
	}
	if err := w.svc.SetDisabled(ctx, w.env.Kavya, id, s2.Version, true); err != nil {
		t.Fatalf("SetDisabled(EMP-009, true): %v", err)
	}
	left := w.get(t, id)
	wantStale(t, "SetDisabledTx(Left EMP-009, stale version, true)", tx(w.env.Kavya, s2.Version, true))
	if err := tx(w.env.Priya, left.Version, true); !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("SetDisabledTx(Priya, Left EMP-009, true) = %v; want auth.ErrForbidden", err)
	}
	if e := w.get(t, id); e.Version != left.Version || e.Status != employee.Left {
		t.Errorf("EMP-009 = {Version %d, Status %q}; want {%d, Left}", e.Version, e.Status, left.Version)
	}
}

// TestEmployee_UserLinkPropertyRandomWrites runs random public writes over
// the fixture employees and users (fixed seed), mixed with an Administrator
// disabling or enabling a random user through auth.Service, and checks the
// CD-185 user-link invariants after every step:
//
//   - at most one Active employee per user (MST-111);
//   - user_disabled_on_leave set ⇒ the employee is Left and has a user;
//   - only the written employee's flag changes; a create never sets it; a
//     leave sets it exactly when it found the user enabled, and disables the
//     user; a re-join clears it and leaves the user enabled;
//   - a user goes enabled → disabled only by the Administrator or by the
//     written employee leaving, and disabled → enabled only by the
//     Administrator or by the written employee re-joining with its flag set;
//   - a re-join whose user is disabled while the flag is clear is refused;
//   - an Active employee's user is disabled only when the Administrator
//     disabled it (CD-185 allows that; nothing else may);
//   - a refusal is a *mstkit.ValidationError and changes nothing.
func TestEmployee_UserLinkPropertyRandomWrites(t *testing.T) { // MST-111, MST-112, MST-119, CD-185
	ctx := context.Background()
	w := newWorld(t)
	w.seedFixtureEmployees(t)
	rng := rand.New(rand.NewPCG(14, 185))

	// EMP-001 and EMP-006 hold the Administrator's and Kavya's users; a Left
	// save would disable the acting user, so they and those users stay out.
	var pool []int64
	for _, c := range []string{"EMP-002", "EMP-003", "EMP-004", "EMP-005", "EMP-007", "EMP-008"} {
		pool = append(pool, w.ids[c])
	}
	users := []auth.UserID{0, 2, 3, 4, 5, 7, 8, 999}
	adminUsers := []auth.UserID{2, 3, 4, 5, 7, 8}
	relieving := []types.Date{date(2025, time.January, 31), date(2026, time.March, 31), date(2026, time.October, 24)}
	byAdmin := map[auth.UserID]bool{} // disabled by the Administrator's last change
	var steps []string
	failf := func(format string, args ...any) {
		t.Helper()
		t.Fatalf("%s\nsteps:\n%s", fmt.Sprintf(format, args...), strings.Join(steps, "\n"))
	}
	flagsNow := func() map[int64]bool {
		t.Helper()
		f, err := w.leaveFlags()
		if err != nil {
			failf("read user_disabled_on_leave: %v (CD-185 column)", err)
		}
		return f
	}
	pickManager := func(s snap) int64 {
		if rng.IntN(3) == 0 {
			return 0
		}
		return s.ids[rng.IntN(len(s.ids))]
	}
	inputOf := func(e employee.Employee) employee.Input {
		return employee.Input{
			Code: e.Code, Name: e.Name, BranchID: e.BranchID, UserID: e.UserID, ManagerID: e.ManagerID,
			Joining: e.Joining, Birth: e.Birth, Relieving: e.Relieving, Status: e.Status,
		}
	}
	setStatus := func(in *employee.Input, s employee.Status) {
		in.Status, in.Relieving = s, types.Date{}
		if s == employee.Left {
			in.Relieving = relieving[rng.IntN(len(relieving))]
		}
	}

	const n = 800
	var ok [4]int
	var bypassRefused, flagRejoins, leavesFound int
	for step := range n {
		before, bflags := w.snapshot(t), flagsNow()
		var written int64 // the employee written (0 for the Administrator step)
		var created bool  // written is a new employee
		var leaving, rejoining bool
		var adminUser auth.UserID
		var adminEnable bool
		var desc string
		var err error
		op := []int{0, 0, 1, 1, 1, 1, 2, 2, 3}[rng.IntN(9)] // Create, Update, SetDisabled, Administrator
		switch op {
		case 0: // Create
			in := w.input(fmt.Sprintf("EMP-%03d", 100+step), "Temp Staff")
			setStatus(&in, []employee.Status{employee.Active, employee.Left}[rng.IntN(2)])
			in.UserID, in.ManagerID = users[rng.IntN(len(users))], pickManager(before)
			desc = fmt.Sprintf("Create(%s, %s, user %d, manager %d, relieving %v)", in.Code, in.Status, in.UserID, in.ManagerID, in.Relieving)
			var s mstkit.Saved
			if s, err = w.svc.Create(ctx, w.env.Kavya, in); err == nil {
				written, created = s.ID, true
				pool = append(pool, s.ID)
			}
		case 1: // Update
			id := pool[rng.IntN(len(pool))]
			e := w.get(t, id)
			in := inputOf(e)
			var what []string
			for range 1 + rng.IntN(2) {
				switch rng.IntN(4) {
				case 0:
					if in.Status == employee.Active {
						setStatus(&in, employee.Left)
					} else {
						setStatus(&in, employee.Active)
					}
					what = append(what, "status "+string(in.Status))
				case 1:
					in.UserID = users[rng.IntN(len(users))]
					what = append(what, fmt.Sprintf("user %d", in.UserID))
				case 2:
					in.Relieving = relieving[rng.IntN(len(relieving))]
					what = append(what, fmt.Sprintf("relieving %v", in.Relieving))
				case 3:
					in.ManagerID = pickManager(before)
					what = append(what, fmt.Sprintf("manager %d", in.ManagerID))
				}
			}
			desc = fmt.Sprintf("Update(%s %s user %d flag %v → %s)", e.Code, e.Status, e.UserID, bflags[id], strings.Join(what, ", "))
			written = id
			leaving = e.Status == employee.Active && in.Status == employee.Left
			rejoining = e.Status == employee.Left && in.Status == employee.Active
			if rejoining && in.UserID == e.UserID && e.UserID != 0 && !bflags[id] && !before.enabled[e.UserID] {
				desc += " [bypass]"
			}
			_, err = w.svc.Update(ctx, w.env.Kavya, id, e.Version, in)
		case 2: // SetDisabled
			id := pool[rng.IntN(len(pool))]
			e := w.get(t, id)
			disabled := rng.IntN(2) == 0
			desc = fmt.Sprintf("SetDisabled(%s %s user %d flag %v, %v)", e.Code, e.Status, e.UserID, bflags[id], disabled)
			written = id
			leaving = e.Status == employee.Active && disabled
			rejoining = e.Status == employee.Left && !disabled
			if rejoining && e.UserID != 0 && !bflags[id] && !before.enabled[e.UserID] {
				desc += " [bypass]"
			}
			err = w.svc.SetDisabled(ctx, w.env.Kavya, id, e.Version, disabled)
		case 3: // the Administrator disables or enables a user (auth.Service)
			adminUser, adminEnable = adminUsers[rng.IntN(len(adminUsers))], rng.IntN(2) == 0
			desc = fmt.Sprintf("Administrator sets user %d enabled = %v", adminUser, adminEnable)
			w.adminSetEnabled(t, adminUser, adminEnable)
		}
		after, aflags := w.snapshot(t), flagsNow()
		steps = append(steps, fmt.Sprintf("%3d %s → err %v", step, desc, err))
		bypass := strings.HasSuffix(desc, "[bypass]")

		if err != nil {
			var ve *mstkit.ValidationError
			if !errors.As(err, &ve) {
				failf("step %d: %s refused with %T %v; want a *mstkit.ValidationError", step, desc, err, err)
			}
			if after.dump != before.dump || !maps.Equal(aflags, bflags) {
				failf("step %d: refused %s changed state:\nbefore\n%s%v\nafter\n%s%v", step, desc, before.dump, bflags, after.dump, aflags)
			}
			if bypass {
				bypassRefused++
			}
			continue
		}
		if bypass {
			failf("step %d: %s saved; want refused: its user is disabled and the flag is clear (CD-185)", step, desc)
		}
		ok[op]++

		// Flags: set only on a Left employee with a user; only the written
		// employee's changes, as its transition says.
		for _, id := range after.ids {
			e := after.emps[id]
			if aflags[id] && (e.status != employee.Left || e.user == 0) {
				failf("step %d: %s has user_disabled_on_leave set but is {%s, user %d}", step, e.code, e.status, e.user)
			}
			if id == written {
				continue
			}
			if aflags[id] != bflags[id] {
				failf("step %d: %s changed %s's user_disabled_on_leave %v → %v", step, desc, e.code, bflags[id], aflags[id])
			}
		}
		if op == 3 && !maps.Equal(after.emps, before.emps) {
			failf("step %d: %s changed employee rows:\nbefore\n%s\nafter\n%s", step, desc, before.dump, after.dump)
		}
		var wu auth.UserID
		if written != 0 {
			wu = after.emps[written].user
			got := aflags[written]
			switch {
			case created:
				if got {
					failf("step %d: %s set user_disabled_on_leave on create", step, desc)
				}
			case leaving:
				if want := wu != 0 && before.enabled[wu]; got != want {
					failf("step %d: %s left with user %d enabled = %v; user_disabled_on_leave = %v, want %v", step, desc, wu, before.enabled[wu], got, want)
				}
				if wu != 0 && after.enabled[wu] {
					failf("step %d: %s left; its user %d is still enabled", step, desc, wu)
				}
				if got {
					leavesFound++
				}
			case rejoining:
				if got {
					failf("step %d: %s re-joined; user_disabled_on_leave still set", step, desc)
				}
				if wu != 0 && !after.enabled[wu] {
					failf("step %d: %s re-joined; its user %d is disabled", step, desc, wu)
				}
				if bflags[written] {
					flagRejoins++
				}
			default:
				if got != bflags[written] {
					failf("step %d: %s changed user_disabled_on_leave %v → %v without a status change", step, desc, bflags[written], got)
				}
			}
		}

		// Users: who may disable or enable whom.
		for u, en := range before.enabled {
			switch {
			case en && !after.enabled[u]:
				byAdminStep, byLeave := op == 3 && u == adminUser && !adminEnable, leaving && u == wu
				if !byAdminStep && !byLeave {
					failf("step %d: user %d was disabled by %s", step, u, desc)
				}
				byAdmin[u] = op == 3
			case !en && after.enabled[u]:
				byAdminStep, byRejoin := op == 3 && u == adminUser && adminEnable, rejoining && u == wu && bflags[written]
				if !byAdminStep && !byRejoin {
					failf("step %d: user %d was enabled by %s (only the Administrator, or a re-join whose flag was set)", step, u, desc)
				}
				delete(byAdmin, u)
			}
		}

		activeBy := map[auth.UserID]int64{}
		for _, id := range after.ids {
			e := after.emps[id]
			if e.status != employee.Active || e.user == 0 {
				continue
			}
			if other, dup := activeBy[e.user]; dup {
				failf("step %d: user %d linked to active %s and %s", step, e.user, after.emps[other].code, e.code)
			}
			activeBy[e.user] = id
			if !after.enabled[e.user] && !byAdmin[e.user] {
				failf("step %d: active %s has user %d, disabled but not by the Administrator", step, e.code, e.user)
			}
		}
	}
	t.Logf("%d steps: successful Create %d, Update %d, SetDisabled %d, Administrator %d; leaves that set the flag %d, re-joins with the flag set %d, bypass re-joins refused %d",
		n, ok[0], ok[1], ok[2], ok[3], leavesFound, flagRejoins, bypassRefused)
	if ok[0] == 0 || ok[1] == 0 || ok[2] == 0 || ok[3] == 0 || leavesFound == 0 || flagRejoins == 0 || bypassRefused == 0 {
		failf("coverage: successful {Create %d, Update %d, SetDisabled %d, Administrator %d}, flag-setting leaves %d, flag re-joins %d, bypass refusals %d; want at least one of each",
			ok[0], ok[1], ok[2], ok[3], leavesFound, flagRejoins, bypassRefused)
	}
}
