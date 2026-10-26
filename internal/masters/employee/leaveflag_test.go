package employee_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

const (
	msgStatusOptions = "Status must be Active or Left."               // CD-166
	msgNoBranch      = "Branch not found. Pick one from the list."    // CD-170
	msgBranchBLROff  = "Branch BLR is disabled. Pick another branch." // CD-170 pattern
)

// leaveFlags reads employee.user_disabled_on_leave for every employee (CD-185).
func (w *world) leaveFlags() (map[int64]bool, error) {
	ctx := context.Background()
	flags := map[int64]bool{}
	err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, `SELECT id, user_disabled_on_leave FROM employee ORDER BY id`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, flag int64
			if err := rows.Scan(&id, &flag); err != nil {
				return err
			}
			flags[id] = flag != 0
		}
		return rows.Err()
	})
	return flags, err
}

// wantLeaveFlag checks code's user_disabled_on_leave column (CD-185). A read
// error is reported, not fatal, so the behaviour checks after it still run.
func (w *world) wantLeaveFlag(t *testing.T, code string, want bool) {
	t.Helper()
	flags, err := w.leaveFlags()
	if err != nil {
		t.Errorf("read %s's user_disabled_on_leave: %v; want %v (CD-185 column)", code, err, want)
		return
	}
	if got := flags[w.ids[code]]; got != want {
		t.Errorf("%s user_disabled_on_leave = %v; want %v", code, got, want)
	}
}

// adminSetEnabled enables or disables u as the Administrator through the real
// auth.Service (Users screen path, audited as "update", not "enable"/"disable").
func (w *world) adminSetEnabled(t *testing.T, u auth.UserID, enabled bool) {
	t.Helper()
	a, ok := w.env.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Kit.Auth = %T; want the real *auth.Auth", w.env.Kit.Auth)
	}
	if _, err := a.UpdateUser(context.Background(), w.env.Admin, u, auth.UserPatch{Enabled: &enabled}); err != nil {
		t.Fatalf("Administrator sets user %d enabled = %v: %v", u, enabled, err)
	}
}

// countAction counts the audit entries of user u with action.
func (w *world) countAction(t *testing.T, u auth.UserID, action string) int {
	t.Helper()
	n := 0
	for _, e := range w.entries(t, "User", int64(u)) {
		if e.Action == action {
			n++
		}
	}
	return n
}

// setDisabled calls SetDisabled on code at its current version and keeps the
// new input and version on success.
func (w *world) setDisabled(t *testing.T, code string, disabled bool) error {
	t.Helper()
	e := w.get(t, w.ids[code])
	if err := w.svc.SetDisabled(context.Background(), w.env.Kavya, e.ID, e.Version, disabled); err != nil {
		return err
	}
	e = w.get(t, e.ID)
	w.versions[code] = e.Version
	w.ins[code] = employee.Input{
		Code: e.Code, Name: e.Name, BranchID: e.BranchID, UserID: e.UserID, ManagerID: e.ManagerID,
		Joining: e.Joining, Birth: e.Birth, Relieving: e.Relieving, Status: e.Status,
	}
	return nil
}

func leave(in *employee.Input)  { in.Status, in.Relieving = employee.Left, date(2026, time.October, 24) }
func rejoin(in *employee.Input) { in.Status, in.Relieving = employee.Active, types.Date{} }

// TestEmployee_LeaveFlagRejoinReenables (CD-185 (a)): leaving an employee
// whose user is enabled disables it and sets user_disabled_on_leave;
// re-joining re-enables that user and clears the flag. Through Update and
// through SetDisabled.
func TestEmployee_LeaveFlagRejoinReenables(t *testing.T) { // MST-119, CD-185
	w := newWorld(t)
	deepak := userID(t, "deepak@vardhan.test")
	in := w.input("EMP-007", "Deepak Nair")
	in.UserID = deepak
	w.create(t, in)
	w.wantLeaveFlag(t, "EMP-007", false)

	for _, path := range []struct {
		name          string
		leave, rejoin func() error
	}{
		{"Update",
			func() error { return w.update(t, "EMP-007", leave) },
			func() error { return w.update(t, "EMP-007", rejoin) }},
		{"SetDisabled",
			func() error {
				// SetDisabled(true) keeps the relieving date already saved (MST-118).
				if err := w.update(t, "EMP-007", func(in *employee.Input) { in.Relieving = date(2026, time.October, 24) }); err != nil {
					return err
				}
				return w.setDisabled(t, "EMP-007", true)
			},
			func() error { return w.setDisabled(t, "EMP-007", false) }},
	} {
		enables := w.countAction(t, deepak, "enable")
		if err := path.leave(); err != nil {
			t.Fatalf("%s: EMP-007 leaves: %v", path.name, err)
		}
		if w.userEnabled(t, deepak) {
			t.Errorf("%s: deepak enabled after EMP-007 left; want disabled", path.name)
		}
		w.wantLeaveFlag(t, "EMP-007", true)

		if err := path.rejoin(); err != nil {
			t.Fatalf("%s: EMP-007 re-joins with deepak: %v; want saved", path.name, err)
		}
		if !w.userEnabled(t, deepak) {
			t.Errorf("%s: deepak disabled after EMP-007 re-joined; want enabled", path.name)
		}
		if got := w.countAction(t, deepak, "enable"); got != enables+1 {
			t.Errorf("%s: User deepak enable entries = %d after re-join; want %d", path.name, got, enables+1)
		}
		w.wantLeaveFlag(t, "EMP-007", false)
	}
}

// TestEmployee_AdminDisabledUserOtherSavesAllowed (CD-185 (d)): after an
// Administrator disables the user of an Active employee, saves that keep the
// user and the status (relieving date, manager, branch) still succeed:
// CD-185 drops the post-state checkUserLink.
func TestEmployee_AdminDisabledUserOtherSavesAllowed(t *testing.T) { // MST-112, CD-185
	ctx := context.Background()
	w := newWorld(t)
	blr, err := company.New(w.env.Kit).Create(ctx, w.env.Kavya, company.BranchInput{Code: "BLR", Name: "Bengaluru Site Office", StateCode: "29", GSTIN: "29AAFCV2449G1Z3"})
	if err != nil {
		t.Fatalf("create branch BLR: %v", err)
	}
	w.create(t, w.input("EMP-003", "Ravi Kulkarni"))
	w.create(t, w.input("EMP-004", "Priya Shah"))
	deepak := userID(t, "deepak@vardhan.test")
	in := w.input("EMP-007", "Deepak Nair")
	in.UserID, in.ManagerID = deepak, w.ids["EMP-004"]
	w.create(t, in)

	w.adminSetEnabled(t, deepak, false)

	for _, c := range []struct {
		name string
		edit func(*employee.Input)
	}{
		{"relieving date", func(in *employee.Input) { in.Relieving = date(2026, time.October, 31) }},
		{"manager EMP-003", func(in *employee.Input) { in.ManagerID = w.ids["EMP-003"] }},
		{"branch BLR", func(in *employee.Input) { in.BranchID = blr.ID }},
	} {
		if err := w.update(t, "EMP-007", c.edit); err != nil {
			t.Errorf("Active EMP-007 (user deepak disabled by the Administrator), change %s: %v; want saved", c.name, err)
		}
	}
	e := w.get(t, w.ids["EMP-007"])
	if e.Status != employee.Active || e.UserID != deepak || e.ManagerID != w.ids["EMP-003"] || e.BranchID != blr.ID || e.Relieving != date(2026, time.October, 31) {
		t.Errorf("EMP-007 = {Status %q, UserID %d, ManagerID %d, BranchID %d, Relieving %v}; want {Active, %d, %d, %d, 2026-10-31}",
			e.Status, e.UserID, e.ManagerID, e.BranchID, e.Relieving, deepak, w.ids["EMP-003"], blr.ID)
	}
	if w.userEnabled(t, deepak) {
		t.Errorf("deepak enabled after EMP-007's saves; want still disabled (the Administrator's choice)")
	}
	if n := w.countAction(t, deepak, "enable"); n != 0 {
		t.Errorf("User deepak has %d enable entries; want none", n)
	}
	w.wantLeaveFlag(t, "EMP-007", false)
}

// TestEmployee_AdminDisabledUserRejoinRefused (CD-185 (b), the bypass it
// closes): an Administrator disables the user of Active EMP-007; EMP-007
// leaves (the user stays disabled, the flag stays clear); re-joining with that
// user is refused with MST-112's text on user_id, through Update and through
// SetDisabled, and the user is not re-enabled. Once the Administrator
// enables the user again, the re-join saves without an enable of its own.
func TestEmployee_AdminDisabledUserRejoinRefused(t *testing.T) { // MST-112, MST-119, CD-185
	w := newWorld(t)
	deepak := userID(t, "deepak@vardhan.test")
	in := w.input("EMP-007", "Deepak Nair")
	in.UserID, in.Relieving = deepak, date(2026, time.October, 24) // SetDisabled(true) needs it (MST-118)
	w.create(t, in)
	id := w.ids["EMP-007"]

	w.adminSetEnabled(t, deepak, false)
	if err := w.setDisabled(t, "EMP-007", true); err != nil {
		t.Fatalf("EMP-007 leaves (SetDisabled true) with its user disabled by the Administrator: %v; want saved", err)
	}
	if e := w.get(t, id); e.Status != employee.Left || e.UserID != deepak {
		t.Fatalf("EMP-007 = {Status %q, UserID %d}; want {Left, %d}", e.Status, e.UserID, deepak)
	}
	if w.userEnabled(t, deepak) {
		t.Errorf("deepak enabled after EMP-007 left; want disabled")
	}
	if n := w.countAction(t, deepak, "disable"); n != 0 {
		t.Errorf("User deepak has %d disable entries after EMP-007 left; want none (it was already disabled)", n)
	}
	w.wantLeaveFlag(t, "EMP-007", false)

	version := w.versions["EMP-007"]
	for _, path := range []struct {
		name   string
		rejoin func() error
	}{
		{"Update", func() error { return w.update(t, "EMP-007", rejoin) }},
		{"SetDisabled", func() error { return w.setDisabled(t, "EMP-007", false) }},
	} {
		t.Run(path.name, func(t *testing.T) {
			wantUserFieldError(t, path.rejoin(), msgPickUser)
			if e := w.get(t, id); e.Status != employee.Left || e.UserID != deepak || e.Version != version {
				t.Errorf("EMP-007 after the refused re-join = {Status %q, UserID %d, Version %d}; want {Left, %d, %d}", e.Status, e.UserID, e.Version, deepak, version)
			}
			if w.userEnabled(t, deepak) {
				t.Errorf("deepak enabled by EMP-007's refused re-join; want still disabled")
			}
			if n := w.countAction(t, deepak, "enable"); n != 0 {
				t.Errorf("User deepak has %d enable entries; want none", n)
			}
		})
	}

	// The Administrator enables deepak again: the re-join saves, and enables
	// nothing itself (the flag was never set).
	w.adminSetEnabled(t, deepak, true)
	if err := w.update(t, "EMP-007", rejoin); err != nil {
		t.Fatalf("re-join EMP-007 after the Administrator enabled deepak: %v; want saved", err)
	}
	if e := w.get(t, id); e.Status != employee.Active || e.UserID != deepak {
		t.Errorf("EMP-007 = {Status %q, UserID %d}; want {Active, %d}", e.Status, e.UserID, deepak)
	}
	if !w.userEnabled(t, deepak) {
		t.Errorf("deepak disabled after the re-join; want enabled")
	}
	if n := w.countAction(t, deepak, "enable"); n != 0 {
		t.Errorf("User deepak has %d enable entries after the re-join; want none (EnableUser only when the flag was set)", n)
	}
	w.wantLeaveFlag(t, "EMP-007", false)
}

// TestEmployee_LeftCreateNeverSetsFlag (CD-185 (c)): a create with status
// Left never sets user_disabled_on_leave, so re-joining with its disabled user
// is refused like the bypass; with an enabled user the re-join saves without
// an enable, and a later leave is a real one that sets the flag.
func TestEmployee_LeftCreateNeverSetsFlag(t *testing.T) { // MST-112, MST-119, CD-185
	w := newWorld(t)
	old, deepak := userID(t, "old@vardhan.test"), userID(t, "deepak@vardhan.test")
	in := w.input("EMP-008", "Former Staff")
	in.UserID, in.Status, in.Relieving = old, employee.Left, date(2026, time.March, 31)
	w.create(t, in)
	id := w.ids["EMP-008"]
	w.wantLeaveFlag(t, "EMP-008", false)
	version := w.versions["EMP-008"]

	for _, path := range []struct {
		name   string
		rejoin func() error
	}{
		{"Update", func() error { return w.update(t, "EMP-008", rejoin) }},
		{"SetDisabled", func() error { return w.setDisabled(t, "EMP-008", false) }},
	} {
		t.Run(path.name, func(t *testing.T) {
			wantUserFieldError(t, path.rejoin(), msgPickUser)
			if e := w.get(t, id); e.Status != employee.Left || e.UserID != old || e.Version != version {
				t.Errorf("EMP-008 after the refused re-join = {Status %q, UserID %d, Version %d}; want {Left, %d, %d}", e.Status, e.UserID, e.Version, old, version)
			}
			if w.userEnabled(t, old) {
				t.Errorf("old@ enabled by EMP-008's refused re-join; want still disabled")
			}
			if n := w.countAction(t, old, "enable"); n != 0 {
				t.Errorf("User old@ has %d enable entries; want none", n)
			}
		})
	}

	// A Left create with an enabled, unlinked user.
	in = w.input("EMP-007", "Deepak Nair")
	in.UserID, in.Status, in.Relieving = deepak, employee.Left, date(2026, time.March, 31)
	w.create(t, in)
	w.wantLeaveFlag(t, "EMP-007", false)
	if err := w.update(t, "EMP-007", rejoin); err != nil {
		t.Fatalf("re-join Left-created EMP-007 with the enabled deepak: %v; want saved", err)
	}
	if !w.userEnabled(t, deepak) {
		t.Errorf("deepak disabled after EMP-007 re-joined; want enabled")
	}
	if n := w.countAction(t, deepak, "enable"); n != 0 {
		t.Errorf("User deepak has %d enable entries after the re-join; want none (the flag was never set)", n)
	}
	if err := w.update(t, "EMP-007", leave); err != nil {
		t.Fatalf("EMP-007 leaves: %v", err)
	}
	if w.userEnabled(t, deepak) {
		t.Errorf("deepak enabled after EMP-007 left; want disabled")
	}
	w.wantLeaveFlag(t, "EMP-007", true)
}

// TestEmployee_BranchLinkMessages (CD-170): blank, unknown and disabled
// branch on create and update; an unknown reporting manager on update.
func TestEmployee_BranchLinkMessages(t *testing.T) { // MST-109, CD-170
	ctx := context.Background()
	w := newWorld(t)
	co := company.New(w.env.Kit)
	blr, err := co.Create(ctx, w.env.Kavya, company.BranchInput{Code: "BLR", Name: "Bengaluru Site Office", StateCode: "29", GSTIN: "29AAFCV2449G1Z3"})
	if err != nil {
		t.Fatalf("create branch BLR: %v", err)
	}
	if err := co.SetDisabled(ctx, w.env.Kavya, blr.ID, blr.Version, true); err != nil {
		t.Fatalf("disable branch BLR: %v", err)
	}
	w.create(t, w.input("EMP-007", "Deepak Nair"))
	id, version := w.ids["EMP-007"], w.versions["EMP-007"]

	cases := []struct {
		name   string
		branch int64
		want   doctype.FieldError
	}{
		{"blank", 0, doctype.FieldError{Field: "branch_id", Message: msgBranchRequired}},
		{"unknown 9999", 9999, doctype.FieldError{Field: "branch_id", Message: msgNoBranch}},
		{"disabled BLR", blr.ID, doctype.FieldError{Field: "branch_id", Message: msgBranchBLROff}},
	}
	for _, c := range cases {
		t.Run("create "+c.name, func(t *testing.T) {
			in := w.input("EMP-009", "Asha Menon")
			in.BranchID = c.branch
			_, err := w.svc.Create(ctx, w.env.Kavya, in)
			wantFieldErrors(t, err, c.want)
			w.wantNoCode(t, "EMP-009")
		})
		t.Run("update "+c.name, func(t *testing.T) {
			err := w.update(t, "EMP-007", func(in *employee.Input) { in.BranchID = c.branch })
			wantFieldErrors(t, err, c.want)
			if e := w.get(t, id); e.BranchID != w.branchID || e.Version != version {
				t.Errorf("EMP-007 = {BranchID %d, Version %d}; want {%d, %d}", e.BranchID, e.Version, w.branchID, version)
			}
		})
	}

	err = w.update(t, "EMP-007", func(in *employee.Input) { in.ManagerID = 9999 })
	wantFieldErrors(t, err, doctype.FieldError{Field: "manager_id", Message: msgNoManager})
	if e := w.get(t, id); e.ManagerID != 0 || e.Version != version {
		t.Errorf("EMP-007 = {ManagerID %d, Version %d}; want {0, %d}", e.ManagerID, e.Version, version)
	}
}

// TestEmployee_StatusMustBeActiveOrLeft (CD-166): a blank or unknown status
// is refused on create and update with the Select pattern, on status.
func TestEmployee_StatusMustBeActiveOrLeft(t *testing.T) { // CD-166
	ctx := context.Background()
	w := newWorld(t)
	w.create(t, w.input("EMP-007", "Deepak Nair"))
	id, version := w.ids["EMP-007"], w.versions["EMP-007"]
	want := doctype.FieldError{Field: "status", Message: msgStatusOptions}

	for _, s := range []employee.Status{"", "Retired", "active"} {
		t.Run("create "+string(s), func(t *testing.T) {
			in := w.input("EMP-009", "Asha Menon")
			in.Status = s
			_, err := w.svc.Create(ctx, w.env.Kavya, in)
			wantFieldErrors(t, err, want)
			w.wantNoCode(t, "EMP-009")
		})
		t.Run("update "+string(s), func(t *testing.T) {
			err := w.update(t, "EMP-007", func(in *employee.Input) { in.Status = s })
			wantFieldErrors(t, err, want)
			if e := w.get(t, id); e.Status != employee.Active || e.Version != version {
				t.Errorf("EMP-007 = {Status %q, Version %d}; want {Active, %d}", e.Status, e.Version, version)
			}
		})
	}
}

// TestEmployee_ManagerChain: ManagerChain is the managers above an employee,
// direct manager first, ending at the top (fixture chain EMP-007 → EMP-004
// → EMP-003 → EMP-002); empty for an employee with no manager; a refused
// loop leaves it unchanged. DirectReports is active reports by code.
func TestEmployee_ManagerChain(t *testing.T) {
	w := newWorld(t)
	w.seedFixtureEmployees(t)
	d := w.directory()
	ids := func(codes ...string) []int64 {
		out := []int64{}
		for _, c := range codes {
			out = append(out, w.ids[c])
		}
		return out
	}
	check := func() {
		t.Helper()
		w.read(t, func(ctx context.Context, tx *db.Tx) error {
			for _, c := range []struct {
				code string
				want []int64
			}{
				{"EMP-007", ids("EMP-004", "EMP-003", "EMP-002")},
				{"EMP-004", ids("EMP-003", "EMP-002")},
				{"EMP-002", ids()},
			} {
				got, err := w.svc.ManagerChain(ctx, tx, w.ids[c.code])
				if err != nil || !slices.Equal(got, c.want) {
					t.Errorf("Service.ManagerChain(%s) = (%v, %v); want %v", c.code, got, err, c.want)
				}
				got, err = d.ManagerChain(ctx, tx, w.ids[c.code])
				if err != nil || !slices.Equal(got, c.want) {
					t.Errorf("Directory.ManagerChain(%s) = (%v, %v); want %v", c.code, got, err, c.want)
				}
			}
			if got, err := w.svc.DirectReports(ctx, tx, w.ids["EMP-003"]); err != nil || !slices.Equal(got, ids("EMP-004", "EMP-005")) {
				t.Errorf("DirectReports(EMP-003) = (%v, %v); want %v (EMP-004, EMP-005)", got, err, ids("EMP-004", "EMP-005"))
			}
			if got, err := w.svc.DirectReports(ctx, tx, w.ids["EMP-004"]); err != nil || !slices.Equal(got, ids("EMP-007")) {
				t.Errorf("DirectReports(EMP-004) = (%v, %v); want %v (EMP-007)", got, err, ids("EMP-007"))
			}
			return nil
		})
	}
	check()
	if err := w.update(t, "EMP-002", func(in *employee.Input) { in.ManagerID = w.ids["EMP-007"] }); err == nil {
		t.Fatalf("EMP-002 manager EMP-007 saved; want the MST-114 loop refusal")
	}
	check()
}
