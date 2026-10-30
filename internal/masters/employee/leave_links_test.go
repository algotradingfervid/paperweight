package employee_test

import (
	"context"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	msgMeenaLeft   = "Reporting manager EMP-002 has left. Pick an active manager."                         // MST-116
	msgDeepakStays = "EMP-007 (Deepak Nair) still reports to EMP-004. Move them to another manager first." // MST-117
	msgAshaStays   = "EMP-009 (Asha Menon) still reports to EMP-007. Move them to another manager first."  // MST-117
	msgLoopTwo     = "This would make a reporting loop (EMP-002 → EMP-003 → EMP-002). Pick another manager."
)

// leavePaths are the two public leave paths (CD-204 item 1): Update to status
// Left, and SetDisabled(true).
var leavePaths = []struct {
	name  string
	leave func(*testing.T, *world, string) error
}{
	{"Update", func(t *testing.T, w *world, code string) error { return w.update(t, code, leave) }},
	{"SetDisabled", func(t *testing.T, w *world, code string) error { return w.setDisabled(t, code, true) }},
}

// createBLR adds the second branch BLR, enabled.
func (w *world) createBLR(t *testing.T) mstkit.Saved {
	t.Helper()
	b, err := company.New(w.env.Kit).Create(context.Background(), w.env.Kavya, company.BranchInput{Code: "BLR", Name: "Bengaluru Site Office", StateCode: "29", GSTIN: "29AAFCV2449G1Z3"})
	if err != nil {
		t.Fatalf("create branch BLR: %v", err)
	}
	return b
}

func (w *world) disableBranch(t *testing.T, b mstkit.Saved) {
	t.Helper()
	if err := company.New(w.env.Kit).SetDisabled(context.Background(), w.env.Kavya, b.ID, b.Version, true); err != nil {
		t.Fatalf("disable branch %d: %v", b.ID, err)
	}
}

// leaver creates EMP-007 with user deepak and a relieving date (SetDisabled(true)
// needs one, MST-118), at branch.
func (w *world) leaver(t *testing.T, branch int64) auth.UserID {
	t.Helper()
	deepak := userID(t, "deepak@vardhan.test")
	in := w.input("EMP-007", "Deepak Nair")
	in.BranchID, in.UserID, in.Relieving = branch, deepak, date(2026, time.October, 24)
	w.create(t, in)
	return deepak
}

// exec writes stmt straight to the database: the states these tests start from
// are ones the service refuses to save (MST-114, MST-117).
func (w *world) exec(t *testing.T, stmt string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := w.env.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, stmt, args...)
		return err
	}); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}

// markLeft sets code Left directly, as a manager who left before their report.
func (w *world) markLeft(t *testing.T, code string) {
	t.Helper()
	w.exec(t, `UPDATE employee SET status = 'Left', disabled = 1, relieving_date = '2026-06-30' WHERE id = ?`, w.ids[code])
}

// leaverUnderLeftManager is leaver at PUNE, reporting to EMP-002, who has left.
func (w *world) leaverUnderLeftManager(t *testing.T) auth.UserID {
	t.Helper()
	w.create(t, w.input("EMP-002", "Meena Iyer"))
	deepak := userID(t, "deepak@vardhan.test")
	in := w.input("EMP-007", "Deepak Nair")
	in.UserID, in.ManagerID, in.Relieving = deepak, w.ids["EMP-002"], date(2026, time.October, 24)
	w.create(t, in)
	w.markLeft(t, "EMP-002")
	return deepak
}

// wantLeft checks EMP-007's leave was saved in full: Left, the user disabled,
// the user's version stored (CD-189), both audited.
func (w *world) wantLeft(t *testing.T, u auth.UserID) {
	t.Helper()
	id := w.ids["EMP-007"]
	if e := w.get(t, id); e.Status != employee.Left || !e.Disabled {
		t.Errorf("EMP-007 = {Status %q, Disabled %v}; want {Left, true}", e.Status, e.Disabled)
	}
	if w.userEnabled(t, u) {
		t.Errorf("user %d enabled after EMP-007 left; want disabled", u)
	}
	w.wantLeaveVersion(t, "EMP-007", w.userVersion(t, u))
	if !hasChange(w.entries(t, employee.DocType, id), "status", "Active", "Left") {
		t.Errorf("Employee EMP-007 audit has no status Active → Left change")
	}
	if !hasAction(w.entries(t, "User", int64(u)), "disable") {
		t.Errorf("user %d audit has no disable entry", u)
	}
}

// wantRefused checks save fails with msg and changes nothing: no employee,
// user or user version moved, and no audit entry on code or u.
func (w *world) wantRefused(t *testing.T, code string, u auth.UserID, msg string, save func() error) {
	t.Helper()
	before, entries := w.snapshot(t), len(w.entries(t, employee.DocType, w.ids[code]))
	wantFieldError(t, save(), msg)
	if after := w.snapshot(t); after.dump != before.dump {
		t.Errorf("state changed by the refused save:\nbefore\n%s\nafter\n%s", before.dump, after.dump)
	}
	if n := len(w.entries(t, employee.DocType, w.ids[code])); n != entries {
		t.Errorf("%s has %d audit entries after the refused save; want %d", code, n, entries)
	}
	if u != 0 && hasAction(w.entries(t, "User", int64(u)), "disable") {
		t.Errorf("user %d audit has a disable entry after the refused save; want none", u)
	}
}

// TestEmployee_LeaveAtDisabledBranch (CD-204 item 1): a disabled branch never
// keeps a leaver's user enabled, on either leave path.
func TestEmployee_LeaveAtDisabledBranch(t *testing.T) {
	for _, path := range leavePaths {
		t.Run(path.name, func(t *testing.T) {
			w := newWorld(t)
			blr := w.createBLR(t)
			deepak := w.leaver(t, blr.ID)
			w.disableBranch(t, blr)

			if err := path.leave(t, w, "EMP-007"); err != nil {
				t.Fatalf("EMP-007 leaves at disabled BLR: %v; want saved", err)
			}
			w.wantLeft(t, deepak)
			if e := w.get(t, w.ids["EMP-007"]); e.BranchID != blr.ID {
				t.Errorf("EMP-007 BranchID = %d; want %d (BLR, unchanged)", e.BranchID, blr.ID)
			}
		})
	}
}

// TestEmployee_LeaveUnderLeftManager (CD-204 item 1): a manager who has left
// never keeps a leaver's user enabled. MST-116 applies to Active saves only, so
// this holds through that scope, not a skip. MST-117 stops the service reaching
// this state, so the manager is marked Left directly.
func TestEmployee_LeaveUnderLeftManager(t *testing.T) {
	for _, path := range leavePaths {
		t.Run(path.name, func(t *testing.T) {
			w := newWorld(t)
			deepak := w.leaverUnderLeftManager(t)

			if err := path.leave(t, w, "EMP-007"); err != nil {
				t.Fatalf("EMP-007 leaves under Left EMP-002: %v; want saved", err)
			}
			w.wantLeft(t, deepak)
			if e := w.get(t, w.ids["EMP-007"]); e.ManagerID != w.ids["EMP-002"] {
				t.Errorf("EMP-007 ManagerID = %d; want %d (EMP-002, unchanged)", e.ManagerID, w.ids["EMP-002"])
			}
		})
	}
}

// TestEmployee_LeaveInsideExistingLoop (CD-204 item 1): the loop walk is the
// unchanged-manager check that can refuse a leave. The service never saves a
// loop (MST-114), so one is written directly; EMP-002 is Left so that MST-117
// doesn't refuse first. A stored manager_id = id passes the self-manager check
// (MST-113) too, and an employee isn't their own report (MST-117); a real
// report still refuses, even with a code after the employee's own.
func TestEmployee_LeaveInsideExistingLoop(t *testing.T) {
	for _, path := range leavePaths {
		t.Run(path.name, func(t *testing.T) {
			w := newWorld(t)
			deepak := w.leaverUnderLeftManager(t)
			w.exec(t, `UPDATE employee SET manager_id = ? WHERE id = ?`, w.ids["EMP-007"], w.ids["EMP-002"])

			if err := path.leave(t, w, "EMP-007"); err != nil {
				t.Fatalf("EMP-007 leaves inside a loop: %v; want saved", err)
			}
			w.wantLeft(t, deepak)
		})
		t.Run(path.name+" self row", func(t *testing.T) {
			w := newWorld(t)
			deepak := w.leaver(t, w.branchID)
			w.exec(t, `UPDATE employee SET manager_id = id WHERE id = ?`, w.ids["EMP-007"])
			in := w.ins["EMP-007"]
			in.ManagerID = w.ids["EMP-007"]
			w.ins["EMP-007"] = in

			if err := path.leave(t, w, "EMP-007"); err != nil {
				t.Fatalf("EMP-007 leaves with manager_id = id: %v; want saved", err)
			}
			w.wantLeft(t, deepak)
		})
		t.Run(path.name+" self row with a report", func(t *testing.T) {
			w := newWorld(t)
			deepak := w.leaver(t, w.branchID)
			report := w.input("EMP-009", "Asha Menon")
			report.ManagerID = w.ids["EMP-007"]
			w.create(t, report)
			w.exec(t, `UPDATE employee SET manager_id = id WHERE id = ?`, w.ids["EMP-007"])
			in := w.ins["EMP-007"]
			in.ManagerID = w.ids["EMP-007"]
			w.ins["EMP-007"] = in

			w.wantRefused(t, "EMP-007", deepak, msgAshaStays, func() error { return path.leave(t, w, "EMP-007") })
		})
	}
}

// TestEmployee_LeaveChangingBranch (CD-204 item 1): a new branch is checked on
// a leave, and the refused leave changes nothing.
func TestEmployee_LeaveChangingBranch(t *testing.T) {
	cases := []struct {
		name string
		to   func(blr int64) int64
		msg  string
	}{
		{"disabled", func(blr int64) int64 { return blr }, msgBranchBLROff},
		{"unknown", func(int64) int64 { return 9999 }, msgNoBranch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			blr := w.createBLR(t)
			w.disableBranch(t, blr)
			deepak := w.leaver(t, w.branchID)

			w.wantRefused(t, "EMP-007", deepak, c.msg, func() error {
				return w.update(t, "EMP-007", func(in *employee.Input) {
					leave(in)
					in.BranchID = c.to(blr.ID)
				})
			})
			if e := w.get(t, w.ids["EMP-007"]); e.Status != employee.Active || e.BranchID != w.branchID {
				t.Errorf("EMP-007 = {Status %q, BranchID %d}; want {Active, %d}", e.Status, e.BranchID, w.branchID)
			}
			if !w.userEnabled(t, deepak) {
				t.Errorf("deepak disabled by the refused leave; want enabled")
			}
		})
	}
}

// TestEmployee_LeaveChangingManager (CD-204 item 1): a new manager is checked
// on a leave, and the refused leave changes nothing.
func TestEmployee_LeaveChangingManager(t *testing.T) {
	t.Run("unknown", func(t *testing.T) {
		w := newWorld(t)
		deepak := w.leaver(t, w.branchID)
		w.wantRefused(t, "EMP-007", deepak, msgNoManager, func() error {
			return w.update(t, "EMP-007", func(in *employee.Input) {
				leave(in)
				in.ManagerID = 9999
			})
		})
	})
	t.Run("loop", func(t *testing.T) {
		w := newWorld(t)
		w.create(t, w.input("EMP-002", "Meena Iyer"))
		ravi := w.input("EMP-003", "Ravi Kulkarni")
		ravi.ManagerID = w.ids["EMP-002"]
		w.create(t, ravi)
		w.wantRefused(t, "EMP-002", 0, msgLoopTwo, func() error {
			return w.update(t, "EMP-002", func(in *employee.Input) {
				leave(in)
				in.ManagerID = w.ids["EMP-003"]
			})
		})
	})
}

// TestEmployee_LeaveChangingOneLink: the link a leave keeps stays unchecked
// when it changes the other one (CD-204 item 1 skips each on its own).
func TestEmployee_LeaveChangingOneLink(t *testing.T) {
	t.Run("new manager at a disabled branch", func(t *testing.T) {
		w := newWorld(t)
		blr := w.createBLR(t)
		w.create(t, w.input("EMP-002", "Meena Iyer"))
		deepak := w.leaver(t, blr.ID)
		w.disableBranch(t, blr)

		if err := w.update(t, "EMP-007", func(in *employee.Input) {
			leave(in)
			in.ManagerID = w.ids["EMP-002"]
		}); err != nil {
			t.Fatalf("leave with a new manager at disabled BLR: %v; want saved", err)
		}
		w.wantLeft(t, deepak)
	})
	t.Run("new branch inside a loop", func(t *testing.T) {
		w := newWorld(t)
		blr := w.createBLR(t)
		deepak := w.leaverUnderLeftManager(t)
		w.exec(t, `UPDATE employee SET manager_id = ? WHERE id = ?`, w.ids["EMP-007"], w.ids["EMP-002"])

		if err := w.update(t, "EMP-007", func(in *employee.Input) {
			leave(in)
			in.BranchID = blr.ID
		}); err != nil {
			t.Fatalf("leave to a new branch inside a loop: %v; want saved", err)
		}
		w.wantLeft(t, deepak)
	})
}

// TestEmployee_OtherSavesCheckLinks (CD-204 item 1): only a leave skips the
// link checks; every other save keeps today's.
func TestEmployee_OtherSavesCheckLinks(t *testing.T) {
	t.Run("Active edit at a disabled branch", func(t *testing.T) {
		w := newWorld(t)
		blr := w.createBLR(t)
		deepak := w.leaver(t, blr.ID)
		w.disableBranch(t, blr)
		w.wantRefused(t, "EMP-007", deepak, msgBranchBLROff, func() error {
			return w.update(t, "EMP-007", func(in *employee.Input) { in.Name = "Deepak K Nair" })
		})
	})
	t.Run("Active edit under a manager who left", func(t *testing.T) {
		w := newWorld(t)
		w.create(t, w.input("EMP-002", "Meena Iyer"))
		in := w.input("EMP-007", "Deepak Nair")
		in.ManagerID = w.ids["EMP-002"]
		w.create(t, in)
		w.markLeft(t, "EMP-002")
		w.wantRefused(t, "EMP-007", 0, msgMeenaLeft, func() error {
			return w.update(t, "EMP-007", func(in *employee.Input) { in.Name = "Deepak K Nair" })
		})
	})
	t.Run("Left create at a disabled branch", func(t *testing.T) {
		w := newWorld(t)
		blr := w.createBLR(t)
		w.disableBranch(t, blr)
		in := w.input("EMP-007", "Deepak Nair")
		in.BranchID = blr.ID
		leave(&in)
		_, err := w.svc.Create(context.Background(), w.env.Kavya, in)
		wantFieldError(t, err, msgBranchBLROff)
		w.wantNoCode(t, "EMP-007")
	})

	// A leaver whose branch was disabled after the leave: the disable entry the
	// leave wrote is expected, so these check the snapshot alone.
	left := func(t *testing.T) *world {
		t.Helper()
		w := newWorld(t)
		blr := w.createBLR(t)
		w.leaver(t, blr.ID)
		if err := w.update(t, "EMP-007", leave); err != nil {
			t.Fatalf("mark EMP-007 Left: %v", err)
		}
		w.disableBranch(t, blr)
		return w
	}
	nothingChanged := func(t *testing.T, w *world, msg string, save func() error) {
		t.Helper()
		before := w.snapshot(t)
		wantFieldError(t, save(), msg)
		if after := w.snapshot(t); after.dump != before.dump {
			t.Errorf("state changed by the refused save:\nbefore\n%s\nafter\n%s", before.dump, after.dump)
		}
	}
	t.Run("Left to Left edit at a disabled branch", func(t *testing.T) {
		w := left(t)
		nothingChanged(t, w, msgBranchBLROff, func() error {
			return w.update(t, "EMP-007", func(in *employee.Input) { in.Relieving = date(2026, time.October, 20) })
		})
	})
	t.Run("re-join by Update at a disabled branch", func(t *testing.T) {
		w := left(t)
		nothingChanged(t, w, msgBranchBLROff, func() error { return w.update(t, "EMP-007", rejoin) })
	})
	t.Run("re-join by SetDisabled at a disabled branch", func(t *testing.T) {
		w := left(t)
		nothingChanged(t, w, msgBranchBLROff, func() error { return w.setDisabled(t, "EMP-007", false) })
	})
}

// TestEmployee_LeaveKeepsOtherRules: skipping the link checks skips nothing
// else; MST-117 and MST-118 still refuse a leave at a disabled branch.
func TestEmployee_LeaveKeepsOtherRules(t *testing.T) {
	t.Run("reportees", func(t *testing.T) { // MST-117
		for _, path := range leavePaths {
			t.Run(path.name, func(t *testing.T) {
				w := newWorld(t)
				blr := w.createBLR(t)
				priya := userID(t, "priya@vardhan.test")
				boss := w.input("EMP-004", "Priya Shah")
				boss.BranchID, boss.UserID, boss.Relieving = blr.ID, priya, date(2026, time.October, 24)
				w.create(t, boss)
				report := w.input("EMP-007", "Deepak Nair")
				report.ManagerID = w.ids["EMP-004"]
				w.create(t, report)
				w.disableBranch(t, blr)

				w.wantRefused(t, "EMP-004", priya, msgDeepakStays, func() error { return path.leave(t, w, "EMP-004") })
			})
		}
	})
	t.Run("relieving date", func(t *testing.T) { // MST-118
		for _, path := range leavePaths {
			t.Run(path.name, func(t *testing.T) {
				w := newWorld(t)
				blr := w.createBLR(t)
				in := w.input("EMP-007", "Deepak Nair")
				in.BranchID = blr.ID
				w.create(t, in)
				w.disableBranch(t, blr)

				leaveNoDate := func(in *employee.Input) { in.Status = employee.Left }
				w.wantRefused(t, "EMP-007", 0, msgEnterRelieving, func() error {
					if path.name == "Update" {
						return w.update(t, "EMP-007", leaveNoDate)
					}
					return path.leave(t, w, "EMP-007")
				})
			})
		}
	})
}
