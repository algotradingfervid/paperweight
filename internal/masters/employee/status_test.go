package employee_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

const (
	// msgSeparateSaves refuses a save that changes user_id unless the
	// employee is Active before and after (CD-179, CD-181).
	msgSeparateSaves = "Change the user and the status in separate saves."
	msgNoManager     = "Reporting manager not found. Pick one from the list." // CD-170
)

// TestEmployee_LeftClearingUserDisablesOldUser: an Active → Left save that
// also clears the user is refused (CD-179, CD-181: the user changes only on
// an Active → Active save); EMP-007 and deepak are unchanged. Before CD-179
// this save was accepted and disabled deepak.
func TestEmployee_LeftClearingUserDisablesOldUser(t *testing.T) { // MST-119, CD-181
	w := newWorld(t)
	w.seedFixtureEmployees(t)
	deepak := userID(t, "deepak@vardhan.test")
	id, version := w.ids["EMP-007"], w.versions["EMP-007"]

	err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving, in.UserID = employee.Left, date(2026, time.October, 24), 0
	})
	wantUserFieldError(t, err, msgSeparateSaves)
	if e := w.get(t, id); e.Status != employee.Active || e.UserID != deepak || e.Version != version || e.Relieving != (types.Date{}) {
		t.Errorf("EMP-007 after refused save = {Status %q, UserID %d, Version %d, Relieving %v}; want {Active, %d, %d, blank}", e.Status, e.UserID, e.Version, e.Relieving, deepak, version)
	}
	if !w.userEnabled(t, deepak) {
		t.Errorf("deepak disabled after the refused save; want enabled")
	}
	if hasAction(w.entries(t, "User", int64(deepak)), "disable") {
		t.Errorf("User deepak audit has a disable entry after the refused save; want none")
	}
}

// TestEmployee_LeftWithOtherEmployeesUser: an Active → Left save that links
// the user of another active employee is refused, and that user stays
// enabled (MST-119 must not disable someone else's user).
func TestEmployee_LeftWithOtherEmployeesUser(t *testing.T) { // MST-119
	w := newWorld(t)
	w.seedFixtureEmployees(t)
	deepak, ravi := userID(t, "deepak@vardhan.test"), userID(t, "ravi@vardhan.test") // ravi is EMP-003's
	id, version := w.ids["EMP-007"], w.versions["EMP-007"]

	err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving, in.UserID = employee.Left, date(2026, time.October, 24), ravi
	})
	if !w.userEnabled(t, ravi) {
		t.Errorf("ravi (EMP-003's user) disabled by EMP-007's Left save; want enabled")
	}
	wantFieldError(t, err, msgSeparateSaves)
	if e := w.get(t, id); e.Status != employee.Active || e.UserID != deepak || e.Version != version {
		t.Errorf("EMP-007 after refused save = {Status %q, UserID %d, Version %d}; want {Active, %d, %d}", e.Status, e.UserID, e.Version, deepak, version)
	}
	if !w.userEnabled(t, deepak) {
		t.Errorf("deepak disabled after the refused save; want enabled")
	}
}

// TestEmployee_RejoinWithOtherUser: re-joining with a different user is
// refused (CD-179, CD-181), so re-joining always re-enables the user that
// leaving disabled. Before CD-179 this save was accepted with suresh.
func TestEmployee_RejoinWithOtherUser(t *testing.T) { // MST-119, CD-181
	w := newWorld(t)
	deepak, suresh := userID(t, "deepak@vardhan.test"), userID(t, "suresh@vardhan.test")
	in := w.input("EMP-007", "Deepak Nair")
	in.UserID = deepak
	w.create(t, in)
	id := w.ids["EMP-007"]

	if err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Left, date(2026, time.October, 24)
	}); err != nil {
		t.Fatalf("mark EMP-007 Left: %v", err)
	}
	if w.userEnabled(t, deepak) {
		t.Fatalf("deepak enabled after EMP-007 Left; want disabled")
	}
	version := w.versions["EMP-007"]
	err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving, in.UserID = employee.Active, types.Date{}, suresh
	})
	wantUserFieldError(t, err, msgSeparateSaves)
	if e := w.get(t, id); e.Status != employee.Left || e.UserID != deepak || e.Version != version {
		t.Errorf("EMP-007 after refused re-join = {Status %q, UserID %d, Version %d}; want {Left, %d, %d}", e.Status, e.UserID, e.Version, deepak, version)
	}
	if w.userEnabled(t, deepak) {
		t.Errorf("deepak enabled after the refused re-join; want still disabled")
	}
	if !w.userEnabled(t, suresh) {
		t.Errorf("suresh disabled after the refused re-join; want enabled")
	}

	// Re-joining with the same user is the way back, and re-enables deepak.
	if err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Active, types.Date{}
	}); err != nil {
		t.Fatalf("re-join EMP-007 with deepak: %v", err)
	}
	if !w.userEnabled(t, deepak) {
		t.Errorf("deepak disabled after EMP-007 re-joined with deepak; want enabled")
	}
}

// TestEmployee_LeftWithLeftManagerUpdates: MST-116 applies only to Active
// employees, so a Left employee whose manager left later can still be
// corrected; re-joining under that manager is still refused.
func TestEmployee_LeftWithLeftManagerUpdates(t *testing.T) { // MST-116
	w := newWorld(t)
	w.create(t, w.input("EMP-002", "Meena Iyer"))
	in := w.input("EMP-007", "Deepak Nair")
	in.ManagerID = w.ids["EMP-002"]
	w.create(t, in)

	if err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Left, date(2026, time.March, 31)
	}); err != nil {
		t.Fatalf("mark EMP-007 Left: %v", err)
	}
	if err := w.update(t, "EMP-002", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Left, date(2026, time.June, 30)
	}); err != nil {
		t.Fatalf("mark EMP-002 Left: %v", err)
	}

	fixed := date(2026, time.April, 30)
	if err := w.update(t, "EMP-007", func(in *employee.Input) { in.Relieving = fixed }); err != nil {
		t.Fatalf("correct Left EMP-007's relieving date: %v; want saved", err)
	}
	if e := w.get(t, w.ids["EMP-007"]); e.Relieving != fixed || e.ManagerID != w.ids["EMP-002"] || e.Status != employee.Left {
		t.Errorf("EMP-007 = {Relieving %v, ManagerID %d, Status %q}; want {%v, %d, Left}", e.Relieving, e.ManagerID, e.Status, fixed, w.ids["EMP-002"])
	}

	err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Active, types.Date{}
	})
	wantFieldError(t, err, "Reporting manager EMP-002 has left. Pick an active manager.")
}

// TestEmployee_SetDisabledIsLeftAndRejoin: SetDisabled(true) marks an active
// employee (with a relieving date) Left, disabling the user, audited;
// SetDisabled(false) re-joins: Active, relieving date cleared, user enabled.
func TestEmployee_SetDisabledIsLeftAndRejoin(t *testing.T) { // MST-118, MST-119
	w := newWorld(t)
	ctx := context.Background()
	deepak := userID(t, "deepak@vardhan.test")
	relieving := date(2026, time.October, 24)
	in := w.input("EMP-007", "Deepak Nair")
	in.UserID, in.Relieving = deepak, relieving
	saved := w.create(t, in)
	id := saved.ID

	if err := w.svc.SetDisabled(ctx, w.env.Kavya, id, saved.Version, true); err != nil {
		t.Fatalf("SetDisabled(EMP-007, true): %v", err)
	}
	e := w.get(t, id)
	if e.Status != employee.Left || !e.Disabled || e.Relieving != relieving {
		t.Errorf("EMP-007 after SetDisabled(true) = {Status %q, Disabled %v, Relieving %v}; want {Left, true, %v}", e.Status, e.Disabled, e.Relieving, relieving)
	}
	if w.userEnabled(t, deepak) {
		t.Errorf("deepak enabled after SetDisabled(EMP-007, true); want disabled")
	}
	if !hasChange(w.entries(t, employee.DocType, id), "status", "Active", "Left") {
		t.Errorf("Employee EMP-007 audit has no status Active → Left change after SetDisabled(true)")
	}
	if !hasAction(w.entries(t, "User", int64(deepak)), "disable") {
		t.Errorf("User deepak audit has no disable entry")
	}

	if err := w.svc.SetDisabled(ctx, w.env.Kavya, id, e.Version, false); err != nil {
		t.Fatalf("SetDisabled(EMP-007, false): %v", err)
	}
	e = w.get(t, id)
	if e.Status != employee.Active || e.Disabled || e.Relieving != (types.Date{}) {
		t.Errorf("EMP-007 after SetDisabled(false) = {Status %q, Disabled %v, Relieving %v}; want {Active, false, blank}", e.Status, e.Disabled, e.Relieving)
	}
	if !w.userEnabled(t, deepak) {
		t.Errorf("deepak disabled after SetDisabled(EMP-007, false); want enabled")
	}
	if !hasChange(w.entries(t, employee.DocType, id), "status", "Left", "Active") {
		t.Errorf("Employee EMP-007 audit has no status Left → Active change after SetDisabled(false)")
	}
	if !hasAction(w.entries(t, "User", int64(deepak)), "enable") {
		t.Errorf("User deepak audit has no enable entry")
	}
}

// TestEmployee_DisabledMirrorsStatusInDB: CHECK (disabled = (status =
// 'Left')) refuses writes that bypass the service, through the generic
// mstkit.Env.SetDisabledTx and through raw SQL.
func TestEmployee_DisabledMirrorsStatusInDB(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	saved := w.create(t, w.input("EMP-007", "Deepak Nair"))
	id := saved.ID

	wantCheck := func(what string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "CHECK") {
			t.Errorf("%s: err = %v; want a CHECK constraint failure", what, err)
		}
	}
	wantCheck("mstkit SetDisabledTx(Employee, true) on Active", w.env.DB.Write(ctx, func(tx *db.Tx) error {
		return w.env.Kit.SetDisabledTx(ctx, tx, w.env.Kavya, employee.Kind, id, saved.Version, true)
	}))
	for _, stmt := range []string{
		`UPDATE employee SET disabled = 1 WHERE id = ?`,
		`UPDATE employee SET status = 'Left', relieving_date = '2026-10-24' WHERE id = ?`,
	} {
		wantCheck(stmt, w.env.DB.Write(ctx, func(tx *db.Tx) error {
			_, err := tx.SQL().ExecContext(ctx, stmt, id)
			return err
		}))
	}
	if e := w.get(t, id); e.Status != employee.Active || e.Disabled || e.Version != saved.Version {
		t.Errorf("EMP-007 after refused writes = {Status %q, Disabled %v, Version %d}; want {Active, false, %d}", e.Status, e.Disabled, e.Version, saved.Version)
	}
}

// TestEmployee_LinkAndBirthErrorsTogether: a bad manager and a future date
// of birth come back as both messages in one error (CD-170 link checks are
// collected, not returned before the date rule).
func TestEmployee_LinkAndBirthErrorsTogether(t *testing.T) { // MST-110, CD-170
	w := newWorld(t)
	in := w.input("EMP-009", "Asha Menon")
	in.ManagerID = 9999
	in.Birth = date(2026, time.November, 1)
	_, err := w.svc.Create(context.Background(), w.env.Kavya, in)
	wantFieldErrors(t, err,
		doctype.FieldError{Field: "manager_id", Message: msgNoManager},
		doctype.FieldError{Field: "birth_date", Message: msgBirthFuture},
	)
	w.wantNoCode(t, "EMP-009")
}

// wantFieldErrors checks err is a 422 *mstkit.ValidationError carrying
// exactly want, in any order.
func wantFieldErrors(t *testing.T, err error, want ...doctype.FieldError) {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v (%T); want *mstkit.ValidationError %+v", err, err, want)
	}
	if ve.Status() != 422 {
		t.Errorf("Status() = %d; want 422", ve.Status())
	}
	got := slices.Clone(ve.FieldErrors())
	key := func(a, b doctype.FieldError) int {
		return strings.Compare(a.Field+"\x00"+a.Message, b.Field+"\x00"+b.Message)
	}
	slices.SortFunc(got, key)
	want = slices.Clone(want)
	slices.SortFunc(want, key)
	if !slices.Equal(got, want) {
		t.Errorf("FieldErrors() = %+v; want %+v", got, want)
	}
}
