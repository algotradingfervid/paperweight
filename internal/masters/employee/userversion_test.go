package employee_test

import (
	"context"
	"database/sql"
	"slices"
	"strconv"
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

// leaveVersions reads employee.user_version_on_leave for every employee
// (CD-189); NULL is an invalid sql.NullInt64.
func (w *world) leaveVersions() (map[int64]sql.NullInt64, error) {
	ctx := context.Background()
	vs := map[int64]sql.NullInt64{}
	err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, `SELECT id, user_version_on_leave FROM employee ORDER BY id`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int64
			var v sql.NullInt64
			if err := rows.Scan(&id, &v); err != nil {
				return err
			}
			vs[id] = v
		}
		return rows.Err()
	})
	return vs, err
}

// wantLeaveVersion checks code's user_version_on_leave column (CD-189): want
// 0 means NULL. A read error is reported, not fatal, so the behaviour checks
// after it still run.
func (w *world) wantLeaveVersion(t *testing.T, code string, want int64) {
	t.Helper()
	vs, err := w.leaveVersions()
	if err != nil {
		t.Errorf("read %s's user_version_on_leave: %v; want %s (CD-189 column)", code, err, nullText(want))
		return
	}
	got := vs[w.ids[code]]
	if (want == 0 && got.Valid) || (want != 0 && (!got.Valid || got.Int64 != want)) {
		t.Errorf("%s user_version_on_leave = %s; want %s", code, nullText2(got), nullText(want))
	}
}

func nullText(v int64) string {
	if v == 0 {
		return "NULL"
	}
	return "users.version " + strconv.FormatInt(v, 10)
}

func nullText2(v sql.NullInt64) string {
	if !v.Valid {
		return "NULL"
	}
	return strconv.FormatInt(v.Int64, 10)
}

// userVersion reads users.version of u.
func (w *world) userVersion(t *testing.T, u auth.UserID) int64 {
	t.Helper()
	ctx := context.Background()
	var v int64
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT version FROM users WHERE id = ?`, int64(u)).Scan(&v)
	}); err != nil {
		t.Fatalf("read users.version of %d: %v", u, err)
	}
	return v
}

// adminPatch applies p to user u as the Administrator through the real
// auth.Service (Users screen path, audited as "update", not "enable"/"disable").
func (w *world) adminPatch(t *testing.T, u auth.UserID, p auth.UserPatch) {
	t.Helper()
	a, ok := w.env.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Kit.Auth = %T; want the real *auth.Auth", w.env.Kit.Auth)
	}
	if _, err := a.UpdateUser(context.Background(), w.env.Admin, u, p); err != nil {
		t.Fatalf("Administrator updates user %d with %+v: %v", u, p, err)
	}
}

// adminSetEnabled enables or disables u as the Administrator (adminPatch).
func (w *world) adminSetEnabled(t *testing.T, u auth.UserID, enabled bool) {
	t.Helper()
	w.adminPatch(t, u, auth.UserPatch{Enabled: &enabled})
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

// rejoinPaths are the two public re-join paths for code (CD-189: Update, or
// SetDisabled(false)).
func (w *world) rejoinPaths(t *testing.T, code string) []struct {
	name   string
	rejoin func() error
} {
	return []struct {
		name   string
		rejoin func() error
	}{
		{"Update", func() error { return w.update(t, code, rejoin) }},
		{"SetDisabled", func() error { return w.setDisabled(t, code, false) }},
	}
}

// wantRejoinRefused re-joins Left code (user u, disabled) through Update and
// through SetDisabled(false) and wants each refused with MST-112's text on
// user_id, changing nothing: the employee stays Left at its version, u stays
// disabled with no new "enable" entry, and user_version_on_leave keeps stored
// (0 = NULL).
func (w *world) wantRejoinRefused(t *testing.T, code string, u auth.UserID, stored int64) {
	t.Helper()
	id, version := w.ids[code], w.versions[code]
	for _, path := range w.rejoinPaths(t, code) {
		t.Run(path.name, func(t *testing.T) {
			before, enables := w.snapshot(t), w.countAction(t, u, "enable")
			wantUserFieldError(t, path.rejoin(), msgPickUser)
			if e := w.get(t, id); e.Status != employee.Left || e.UserID != u || e.Version != version {
				t.Errorf("%s after the refused re-join = {Status %q, UserID %d, Version %d}; want {Left, %d, %d}", code, e.Status, e.UserID, e.Version, u, version)
			}
			if w.userEnabled(t, u) {
				t.Errorf("user %d enabled by %s's refused re-join; want still disabled (the Administrator's choice)", u, code)
			}
			if n := w.countAction(t, u, "enable"); n != enables {
				t.Errorf("user %d has %d enable entries after %s's refused re-join; want %d (no EnableUser)", u, n, code, enables)
			}
			if after := w.snapshot(t); after.dump != before.dump {
				t.Errorf("state changed by the refused re-join:\nbefore\n%s\nafter\n%s", before.dump, after.dump)
			}
			w.wantLeaveVersion(t, code, stored)
		})
	}
}

// TestEmployee_LeaveStoresUserVersionRejoinReenables (CD-189, the normal
// path): leaving an employee whose user is enabled disables it and stores the
// user's users.version right after DisableUser in user_version_on_leave;
// re-joining with no Administrator change in between calls EnableUser (one
// "enable" entry), leaves the user enabled and clears the column. Through
// Update and through SetDisabled.
func TestEmployee_LeaveStoresUserVersionRejoinReenables(t *testing.T) { // MST-119, CD-189
	w := newWorld(t)
	deepak := userID(t, "deepak@vardhan.test")
	in := w.input("EMP-007", "Deepak Nair")
	in.UserID = deepak
	w.create(t, in)
	w.wantLeaveVersion(t, "EMP-007", 0)

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
		enables, disables := w.countAction(t, deepak, "enable"), w.countAction(t, deepak, "disable")
		userBefore := w.userVersion(t, deepak)
		if err := path.leave(); err != nil {
			t.Fatalf("%s: EMP-007 leaves: %v", path.name, err)
		}
		if w.userEnabled(t, deepak) {
			t.Errorf("%s: deepak enabled after EMP-007 left; want disabled", path.name)
		}
		if got := w.countAction(t, deepak, "disable"); got != disables+1 {
			t.Errorf("%s: User deepak disable entries = %d after the leave; want %d", path.name, got, disables+1)
		}
		stored := w.userVersion(t, deepak)
		if stored == userBefore {
			t.Errorf("%s: users.version of deepak = %d after the leave's DisableUser; want it moved", path.name, stored)
		}
		w.wantLeaveVersion(t, "EMP-007", stored)

		if err := path.rejoin(); err != nil {
			t.Fatalf("%s: EMP-007 re-joins with deepak: %v; want saved", path.name, err)
		}
		if !w.userEnabled(t, deepak) {
			t.Errorf("%s: deepak disabled after EMP-007 re-joined; want enabled", path.name)
		}
		if got := w.countAction(t, deepak, "enable"); got != enables+1 {
			t.Errorf("%s: User deepak enable entries = %d after re-join; want %d", path.name, got, enables+1)
		}
		w.wantLeaveVersion(t, "EMP-007", 0)
	}
}

// TestEmployee_AdminReenableThenDisableRejoinRefused (CD-189, the bypass the
// CD-185 flag left open): EMP-007 leaves and disables deepak; the
// Administrator enables deepak, then disables him again; re-joining EMP-007
// through Update and through SetDisabled is refused with MST-112's text on
// user_id, deepak stays disabled and gets no "enable" entry. The
// Administrator then enables deepak by hand, and the re-join saves without an
// EnableUser of its own and clears the column.
func TestEmployee_AdminReenableThenDisableRejoinRefused(t *testing.T) { // MST-112, MST-119, CD-189
	w := newWorld(t)
	deepak := userID(t, "deepak@vardhan.test")
	in := w.input("EMP-007", "Deepak Nair")
	in.UserID = deepak
	w.create(t, in)
	if err := w.update(t, "EMP-007", leave); err != nil {
		t.Fatalf("EMP-007 leaves: %v", err)
	}
	if w.userEnabled(t, deepak) {
		t.Fatalf("deepak enabled after EMP-007 left; want disabled")
	}
	stored := w.userVersion(t, deepak)
	w.wantLeaveVersion(t, "EMP-007", stored)

	w.adminSetEnabled(t, deepak, true)
	w.adminSetEnabled(t, deepak, false)
	if w.userEnabled(t, deepak) {
		t.Fatalf("deepak enabled after the Administrator disabled him again; want disabled")
	}
	w.wantRejoinRefused(t, "EMP-007", deepak, stored)

	w.wantAdminEnableThenRejoin(t, "EMP-007", deepak)
}

// wantAdminEnableThenRejoin: the Administrator enables u by hand; code's
// re-join (Update) then saves, calls no EnableUser (no "enable" entry, u's
// users.version unchanged), leaves u enabled and clears the column.
func (w *world) wantAdminEnableThenRejoin(t *testing.T, code string, u auth.UserID) {
	t.Helper()
	w.adminSetEnabled(t, u, true)
	enables, uv := w.countAction(t, u, "enable"), w.userVersion(t, u)
	if err := w.update(t, code, rejoin); err != nil {
		t.Fatalf("re-join %s after the Administrator enabled user %d: %v; want saved", code, u, err)
	}
	if e := w.get(t, w.ids[code]); e.Status != employee.Active || e.UserID != u {
		t.Errorf("%s = {Status %q, UserID %d}; want {Active, %d}", code, e.Status, e.UserID, u)
	}
	if !w.userEnabled(t, u) {
		t.Errorf("user %d disabled after %s re-joined; want enabled", u, code)
	}
	if n := w.countAction(t, u, "enable"); n != enables {
		t.Errorf("user %d has %d enable entries after %s re-joined; want %d (the user was enabled: no EnableUser)", u, n, code, enables)
	}
	if got := w.userVersion(t, u); got != uv {
		t.Errorf("users.version of %d = %d after %s re-joined; want %d (untouched)", u, got, code, uv)
	}
	w.wantLeaveVersion(t, code, 0)
}

// TestEmployee_AdminEditAfterLeaveRejoinRefused (CD-189, conservative by
// design): after EMP-007's leave disabled deepak, an Administrator edit of
// deepak's name or email (UpdateUserFields, which moves users.version)
// leaves him disabled but makes the re-join refused with MST-112's text on
// user_id, through Update and SetDisabled; an Administrator enables him by
// hand and the re-join then saves without an EnableUser.
func TestEmployee_AdminEditAfterLeaveRejoinRefused(t *testing.T) { // MST-112, MST-119, CD-189
	name, email := "Deepak R Nair", "deepak.nair@vardhan.test"
	for _, c := range []struct {
		name  string
		patch auth.UserPatch
	}{
		{"name", auth.UserPatch{Name: &name}},
		{"email", auth.UserPatch{Email: &email}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			deepak := userID(t, "deepak@vardhan.test")
			in := w.input("EMP-007", "Deepak Nair")
			in.UserID = deepak
			w.create(t, in)
			if err := w.update(t, "EMP-007", leave); err != nil {
				t.Fatalf("EMP-007 leaves: %v", err)
			}
			stored := w.userVersion(t, deepak)
			w.wantLeaveVersion(t, "EMP-007", stored)

			w.adminPatch(t, deepak, c.patch)
			if w.userEnabled(t, deepak) {
				t.Fatalf("deepak enabled by the Administrator's %s edit; want still disabled", c.name)
			}
			if got := w.userVersion(t, deepak); got == stored {
				t.Fatalf("users.version of deepak = %d after the %s edit; want it moved (UpdateUserFields)", got, c.name)
			}
			w.wantRejoinRefused(t, "EMP-007", deepak, stored)

			w.wantAdminEnableThenRejoin(t, "EMP-007", deepak)
		})
	}
}

// TestEmployee_AdminReenabledRejoinNoEnable (CD-189): EMP-007's leave
// disabled deepak and stored his version; the Administrator enabled deepak
// again; the re-join saves, reads deepak enabled and calls no EnableUser (no
// "enable" entry, no version change), and clears the column. Through Update
// and through SetDisabled.
func TestEmployee_AdminReenabledRejoinNoEnable(t *testing.T) { // MST-119, CD-189
	w := newWorld(t)
	deepak := userID(t, "deepak@vardhan.test")
	in := w.input("EMP-007", "Deepak Nair")
	in.UserID, in.Relieving = deepak, date(2026, time.October, 24) // SetDisabled(true) needs it (MST-118)
	w.create(t, in)

	for _, path := range []struct {
		name          string
		leave, rejoin func() error
	}{
		{"Update",
			func() error { return w.update(t, "EMP-007", leave) },
			func() error { return w.update(t, "EMP-007", rejoin) }},
		{"SetDisabled",
			func() error {
				if err := w.update(t, "EMP-007", func(in *employee.Input) { in.Relieving = date(2026, time.October, 24) }); err != nil {
					return err
				}
				return w.setDisabled(t, "EMP-007", true)
			},
			func() error { return w.setDisabled(t, "EMP-007", false) }},
	} {
		if err := path.leave(); err != nil {
			t.Fatalf("%s: EMP-007 leaves: %v", path.name, err)
		}
		if w.userEnabled(t, deepak) {
			t.Fatalf("%s: deepak enabled after EMP-007 left; want disabled", path.name)
		}
		w.wantLeaveVersion(t, "EMP-007", w.userVersion(t, deepak))

		w.adminSetEnabled(t, deepak, true)
		enables, uv := w.countAction(t, deepak, "enable"), w.userVersion(t, deepak)
		if err := path.rejoin(); err != nil {
			t.Fatalf("%s: EMP-007 re-joins with the enabled deepak: %v; want saved", path.name, err)
		}
		if e := w.get(t, w.ids["EMP-007"]); e.Status != employee.Active || e.UserID != deepak {
			t.Errorf("%s: EMP-007 = {Status %q, UserID %d}; want {Active, %d}", path.name, e.Status, e.UserID, deepak)
		}
		if !w.userEnabled(t, deepak) {
			t.Errorf("%s: deepak disabled after EMP-007 re-joined; want enabled", path.name)
		}
		if n := w.countAction(t, deepak, "enable"); n != enables {
			t.Errorf("%s: User deepak enable entries = %d after the re-join; want %d (deepak was enabled: no EnableUser)", path.name, n, enables)
		}
		if got := w.userVersion(t, deepak); got != uv {
			t.Errorf("%s: users.version of deepak = %d after the re-join; want %d (untouched)", path.name, got, uv)
		}
		w.wantLeaveVersion(t, "EMP-007", 0)
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
	w.wantLeaveVersion(t, "EMP-007", 0)
}

// TestEmployee_AdminDisabledUserRejoinRefused (CD-185 (b), kept by CD-189):
// an Administrator disables the user of Active EMP-007; EMP-007 leaves (no
// DisableUser: the user stays disabled and user_version_on_leave stays
// NULL); re-joining with that user is refused with MST-112's text on
// user_id, through Update and through SetDisabled, and the user is not
// re-enabled. Once the Administrator enables the user again, the re-join
// saves without an enable of its own.
func TestEmployee_AdminDisabledUserRejoinRefused(t *testing.T) { // MST-112, MST-119, CD-189
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
	w.wantLeaveVersion(t, "EMP-007", 0)

	w.wantRejoinRefused(t, "EMP-007", deepak, 0)

	// The Administrator enables deepak again: the re-join saves, and enables
	// nothing itself.
	w.wantAdminEnableThenRejoin(t, "EMP-007", deepak)
}

// TestEmployee_LeftCreateNeverStoresVersion (CD-185 (c), CD-189): a create
// with status Left leaves user_version_on_leave NULL, so re-joining with its
// disabled user is refused like the bypass; with an enabled user the re-join
// saves without an enable, and a later leave is a real one that stores the
// user's version.
func TestEmployee_LeftCreateNeverStoresVersion(t *testing.T) { // MST-112, MST-119, CD-189
	w := newWorld(t)
	old, deepak := userID(t, "old@vardhan.test"), userID(t, "deepak@vardhan.test")
	in := w.input("EMP-008", "Former Staff")
	in.UserID, in.Status, in.Relieving = old, employee.Left, date(2026, time.March, 31)
	w.create(t, in)
	w.wantLeaveVersion(t, "EMP-008", 0)

	w.wantRejoinRefused(t, "EMP-008", old, 0)

	// A Left create with an enabled, unlinked user.
	in = w.input("EMP-007", "Deepak Nair")
	in.UserID, in.Status, in.Relieving = deepak, employee.Left, date(2026, time.March, 31)
	w.create(t, in)
	w.wantLeaveVersion(t, "EMP-007", 0)
	if err := w.update(t, "EMP-007", rejoin); err != nil {
		t.Fatalf("re-join Left-created EMP-007 with the enabled deepak: %v; want saved", err)
	}
	if !w.userEnabled(t, deepak) {
		t.Errorf("deepak disabled after EMP-007 re-joined; want enabled")
	}
	if n := w.countAction(t, deepak, "enable"); n != 0 {
		t.Errorf("User deepak has %d enable entries after the re-join; want none (deepak was enabled)", n)
	}
	if err := w.update(t, "EMP-007", leave); err != nil {
		t.Fatalf("EMP-007 leaves: %v", err)
	}
	if w.userEnabled(t, deepak) {
		t.Errorf("deepak enabled after EMP-007 left; want disabled")
	}
	w.wantLeaveVersion(t, "EMP-007", w.userVersion(t, deepak))
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
