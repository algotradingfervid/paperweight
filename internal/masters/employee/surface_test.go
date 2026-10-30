package employee_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	// msgDeleteReportee is mstkit's refusal (MST-014) when the FK stops the
	// delete: DOC's References lists documents only, so a reportee shows as
	// "another record".
	msgDeleteReportee = "Cannot delete Employee EMP-004: it is used in another record. Disable it instead."
	// msgDeleteDenied is auth.DeniedError for a user without the delete right.
	msgDeleteDenied = "You don't have permission to delete Employee. Ask your administrator for a role that allows it."
	// msgCodeTaken is mstkit.CheckCodeFree for EMP-004 (Priya Shah).
	msgCodeTaken  = "Code EMP-004 is already used by Employee Priya Shah. Choose another code."
	msgCodeChange = "Only a Masters Manager can change a code."                   // MST-008
	msgLastAdmin  = "At least one enabled user must have the Administrator role." // auth.ErrLastAdmin
)

// deletePaths are the two public ways to delete an employee.
var deletePaths = []struct {
	name string
	del  func(w *world, p auth.Principal, id int64, version int) error
}{
	{"Delete", func(w *world, p auth.Principal, id int64, version int) error {
		return w.svc.Delete(context.Background(), p, id, version)
	}},
	{"DeleteTx", func(w *world, p auth.Principal, id int64, version int) error {
		ctx := context.Background()
		return w.env.DB.Write(ctx, func(tx *db.Tx) error {
			return w.svc.DeleteTx(ctx, tx, p, id, version)
		})
	}},
}

func (w *world) deleteEntries(t *testing.T, id int64) []audit.Entry {
	t.Helper()
	var out []audit.Entry
	for _, e := range w.entries(t, employee.DocType, id) {
		if e.Action == "delete" {
			out = append(out, e)
		}
	}
	return out
}

// TestEmployee_LookupUnknownID: the Master lookup DOC uses reports an id with
// no row as not found, with no error and an empty MasterInfo.
func TestEmployee_LookupUnknownID(t *testing.T) {
	w := newWorld(t)
	w.create(t, w.input("EMP-007", "Deepak Nair"))

	if info, found := w.lookup(t, w.ids["EMP-007"]+1); found || info != (doctype.MasterInfo{}) {
		t.Errorf("Lookup(unknown id) = (%+v, %v); want ({}, false)", info, found)
	}
}

// TestEmployee_DeleteUnreferenced is MST-013 for Employee on both paths: a
// page at an old version is refused (CD-94), then the row goes and the audit
// trail records the deletion.
func TestEmployee_DeleteUnreferenced(t *testing.T) {
	for _, p := range deletePaths {
		t.Run(p.name, func(t *testing.T) {
			w := newWorld(t)
			w.visitTables(t) // DOC walks the test document that links to Employee
			w.create(t, w.input("EMP-009", "Asha Menon"))
			id, opened := w.ids["EMP-009"], w.versions["EMP-009"]
			if err := w.update(t, "EMP-009", func(in *employee.Input) { in.Birth = date(1990, time.May, 14) }); err != nil {
				t.Fatalf("update EMP-009: %v", err)
			}
			current := w.versions["EMP-009"]

			wantStale(t, p.name+"(EMP-009, old version)", p.del(w, w.env.Kavya, id, opened))
			if e := w.get(t, id); e.Code != "EMP-009" || e.Version != current {
				t.Errorf("EMP-009 after stale delete = {Code %q, Version %d}; want {EMP-009, %d}", e.Code, e.Version, current)
			}
			if es := w.deleteEntries(t, id); len(es) != 0 {
				t.Errorf("delete audit entries after stale delete = %+v; want none", es)
			}

			if err := p.del(w, w.env.Kavya, id, current); err != nil {
				t.Fatalf("%s(EMP-009, current version): %v", p.name, err)
			}
			w.wantNoCode(t, "EMP-009")
			w.read(t, func(ctx context.Context, tx *db.Tx) error {
				if _, err := w.svc.Get(ctx, tx, id); !errors.Is(err, doctype.ErrNotFound) {
					t.Errorf("Get(deleted EMP-009) = %v; want doctype.ErrNotFound", err)
				}
				return nil
			})

			es := w.deleteEntries(t, id)
			if len(es) != 1 {
				t.Fatalf("delete audit entries = %+v; want exactly one", es)
			}
			if es[0].By != w.env.Kavya.ID {
				t.Errorf("delete audit By = %d; want Kavya %d", es[0].By, w.env.Kavya.ID)
			}
			want := []audit.Change{
				{Op: audit.OpSet, Field: "code", Before: "EMP-009"},
				{Op: audit.OpSet, Field: "disabled", Before: "false"},
				{Op: audit.OpSet, Field: "name", Before: "Asha Menon"},
			}
			if !slices.Equal(es[0].Changes, want) {
				t.Errorf("delete audit Changes = %+v; want %+v", es[0].Changes, want)
			}
		})
	}
}

// TestEmployee_DeleteRefusedWhileReportedTo is MST-014 for a master reference
// on both paths: EMP-007 reports to EMP-004, so EMP-004 stays and nothing is
// audited, until EMP-007 moves to another manager.
func TestEmployee_DeleteRefusedWhileReportedTo(t *testing.T) {
	for _, p := range deletePaths {
		t.Run(p.name, func(t *testing.T) {
			w := newWorld(t)
			w.visitTables(t) // DOC walks the test document that links to Employee
			w.seedFixtureEmployees(t)
			id, version := w.ids["EMP-004"], w.versions["EMP-004"]

			err := p.del(w, w.env.Kavya, id, version)
			var refused *mstkit.DeleteRefusedError
			if !errors.As(err, &refused) || err.Error() != msgDeleteReportee {
				t.Fatalf("%s(EMP-004) = %v (%T); want *mstkit.DeleteRefusedError %q", p.name, err, err, msgDeleteReportee)
			}
			if refused.Status() != 422 {
				t.Errorf("Status() = %d; want 422", refused.Status())
			}
			if e := w.get(t, id); e.Code != "EMP-004" || e.Version != version {
				t.Errorf("EMP-004 after refused delete = {Code %q, Version %d}; want {EMP-004, %d}", e.Code, e.Version, version)
			}
			if es := w.deleteEntries(t, id); len(es) != 0 {
				t.Errorf("delete audit entries after refused delete = %+v; want none", es)
			}

			if err := w.update(t, "EMP-007", func(in *employee.Input) { in.ManagerID = w.ids["EMP-003"] }); err != nil {
				t.Fatalf("move EMP-007 to EMP-003: %v", err)
			}
			if err := p.del(w, w.env.Kavya, id, version); err != nil {
				t.Errorf("%s(EMP-004) with no reportees: %v; want deleted", p.name, err)
			}
			w.wantNoCode(t, "EMP-004")
		})
	}
}

// TestEmployee_DeleteNeedsRight: Priya holds only read on Employee; each path
// refuses her and leaves the row. DeleteTx checks the right itself because a
// Tx caller never passes through Env.Write.
func TestEmployee_DeleteNeedsRight(t *testing.T) {
	for _, p := range deletePaths {
		t.Run(p.name, func(t *testing.T) {
			w := newWorld(t)
			w.visitTables(t) // DOC walks the test document that links to Employee
			w.create(t, w.input("EMP-009", "Asha Menon"))
			id, version := w.ids["EMP-009"], w.versions["EMP-009"]

			err := p.del(w, w.env.Priya, id, version)
			if !errors.Is(err, auth.ErrForbidden) || err.Error() != msgDeleteDenied {
				t.Errorf("%s(Priya) = %v; want auth.ErrForbidden %q", p.name, err, msgDeleteDenied)
			}
			if e := w.get(t, id); e.Code != "EMP-009" || e.Version != version {
				t.Errorf("EMP-009 after refused delete = {Code %q, Version %d}; want {EMP-009, %d}", e.Code, e.Version, version)
			}
			if es := w.deleteEntries(t, id); len(es) != 0 {
				t.Errorf("delete audit entries after refused delete = %+v; want none", es)
			}
		})
	}
}

// TestEmployee_ByCode: the code is upper-cased and trimmed first; a found
// employee is what Get returns, a Left one included; an unknown code is
// (zero, false, nil).
func TestEmployee_ByCode(t *testing.T) {
	w := newWorld(t)
	w.seedFixtureEmployees(t)
	deepak := userID(t, "deepak@vardhan.test")

	byCode := func(code string) (employee.Employee, bool) {
		t.Helper()
		var e employee.Employee
		var ok bool
		w.read(t, func(ctx context.Context, tx *db.Tx) error {
			var err error
			e, ok, err = w.svc.ByCode(ctx, tx, code)
			return err
		})
		return e, ok
	}

	e, ok := byCode("  emp-007 ")
	if !ok {
		t.Fatalf("ByCode(\"  emp-007 \") not found; want EMP-007")
	}
	if e != w.get(t, w.ids["EMP-007"]) {
		t.Errorf("ByCode(\"  emp-007 \") = %+v; want %+v", e, w.get(t, w.ids["EMP-007"]))
	}
	if e.ID != w.ids["EMP-007"] || e.Code != "EMP-007" || e.Name != "Deepak Nair" || e.UserID != deepak ||
		e.ManagerID != w.ids["EMP-004"] || e.BranchID != w.branchID || e.Joining != joining || e.Status != employee.Active || e.Disabled {
		t.Errorf("ByCode(EMP-007) = %+v; want the fixture row", e)
	}

	left, ok := byCode("EMP-008")
	if !ok {
		t.Fatalf("ByCode(EMP-008) not found; want the Left employee")
	}
	if left.Code != "EMP-008" || left.Status != employee.Left || !left.Disabled || left.Relieving != date(2026, time.March, 31) {
		t.Errorf("ByCode(EMP-008) = %+v; want {EMP-008, Left, disabled, relieving 2026-03-31}", left)
	}

	if e, ok := byCode("EMP-099"); ok || e != (employee.Employee{}) {
		t.Errorf("ByCode(EMP-099) = (%+v, %v); want the zero Employee and false", e, ok)
	}
}

// TestEmployee_WalksStopOnExistingCycle: the service never saves a cycle
// (MST-114), so one is written directly. The walks are bounded by
// count(employee) (chain.go): saving EMP-007 under a cycle it is not part of
// ends the loop check at the bound and adds no new loop, so it is saved, and
// ManagerChain stops after three steps.
func TestEmployee_WalksStopOnExistingCycle(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	w.create(t, w.input("EMP-002", "Meena Iyer"))
	ravi := w.input("EMP-003", "Ravi Kulkarni")
	ravi.ManagerID = w.ids["EMP-002"]
	w.create(t, ravi)
	w.create(t, w.input("EMP-007", "Deepak Nair"))
	if err := w.env.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE employee SET manager_id = ? WHERE id = ?`, w.ids["EMP-003"], w.ids["EMP-002"])
		return err
	}); err != nil {
		t.Fatalf("write the cycle EMP-002 ↔ EMP-003: %v", err)
	}

	if err := w.update(t, "EMP-007", func(in *employee.Input) { in.ManagerID = w.ids["EMP-003"] }); err != nil {
		t.Fatalf("EMP-007 manager EMP-003: %v; want saved", err)
	}
	if e := w.get(t, w.ids["EMP-007"]); e.ManagerID != w.ids["EMP-003"] {
		t.Errorf("EMP-007 ManagerID = %d; want %d (EMP-003)", e.ManagerID, w.ids["EMP-003"])
	}

	want := []int64{w.ids["EMP-003"], w.ids["EMP-002"], w.ids["EMP-003"]}
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		got, err := w.svc.ManagerChain(ctx, tx, w.ids["EMP-007"])
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("ManagerChain(EMP-007) = (%v, %v); want %v", got, err, want)
		}
		return nil
	})
}

// TestEmployee_UpdateStaleLeave: a leave posted at an old version is refused
// before syncUser runs (CD-94): the user stays enabled, no version is stored
// and nothing else changes.
func TestEmployee_UpdateStaleLeave(t *testing.T) { // CD-94, CD-189
	w := newWorld(t)
	deepak := userID(t, "deepak@vardhan.test")
	in := w.input("EMP-009", "Deepak Nair")
	in.UserID = deepak
	w.create(t, in)
	id, opened := w.ids["EMP-009"], w.versions["EMP-009"]
	if err := w.update(t, "EMP-009", func(in *employee.Input) { in.Birth = date(1990, time.May, 14) }); err != nil {
		t.Fatalf("update EMP-009: %v", err)
	}

	stale := w.ins["EMP-009"]
	leave(&stale)
	before := w.snapshot(t)
	_, err := w.svc.Update(context.Background(), w.env.Kavya, id, opened, stale)
	wantStale(t, "Update(EMP-009 leaves, old version)", err)
	if !w.userEnabled(t, deepak) {
		t.Errorf("deepak disabled by the stale leave; want enabled")
	}
	if n := w.countAction(t, deepak, "disable"); n != 0 {
		t.Errorf("User deepak has %d disable entries after the stale leave; want none", n)
	}
	w.wantLeaveVersion(t, "EMP-009", 0)
	if after := w.snapshot(t); after.dump != before.dump {
		t.Errorf("state changed by the stale leave:\nbefore\n%s\nafter\n%s", before.dump, after.dump)
	}
}

// TestEmployee_UpdateStaleRejoin: EMP-009 left and disabled deepak, then a
// Left → Left edit moved its version; a re-join posted at the old version is
// refused before syncUser runs (CD-94): deepak stays disabled, the stored
// version stays and nothing else changes.
func TestEmployee_UpdateStaleRejoin(t *testing.T) { // CD-94, CD-189
	w := newWorld(t)
	deepak := userID(t, "deepak@vardhan.test")
	in := w.input("EMP-009", "Deepak Nair")
	in.UserID = deepak
	w.create(t, in)
	if err := w.update(t, "EMP-009", leave); err != nil {
		t.Fatalf("EMP-009 leaves: %v", err)
	}
	id, opened, stored := w.ids["EMP-009"], w.versions["EMP-009"], w.userVersion(t, deepak)
	if err := w.update(t, "EMP-009", func(in *employee.Input) { in.Birth = date(1990, time.May, 14) }); err != nil {
		t.Fatalf("Left → Left edit of EMP-009: %v", err)
	}
	w.wantLeaveVersion(t, "EMP-009", stored)

	stale := w.ins["EMP-009"]
	rejoin(&stale)
	before := w.snapshot(t)
	_, err := w.svc.Update(context.Background(), w.env.Kavya, id, opened, stale)
	wantStale(t, "Update(EMP-009 re-joins, old version)", err)
	if w.userEnabled(t, deepak) {
		t.Errorf("deepak enabled by the stale re-join; want disabled")
	}
	if n := w.countAction(t, deepak, "enable"); n != 0 {
		t.Errorf("User deepak has %d enable entries after the stale re-join; want none", n)
	}
	w.wantLeaveVersion(t, "EMP-009", stored)
	if after := w.snapshot(t); after.dump != before.dump {
		t.Errorf("state changed by the stale re-join:\nbefore\n%s\nafter\n%s", before.dump, after.dump)
	}
}

// wantCodeError checks err is wantFieldError's single error with msg, on code.
func wantCodeError(t *testing.T, err error, msg string) {
	t.Helper()
	wantFieldError(t, err, msg)
	var ve *mstkit.ValidationError
	if errors.As(err, &ve) && ve.FieldErrors()[0].Field != "code" {
		t.Errorf("FieldErrors()[0].Field = %q; want code", ve.FieldErrors()[0].Field)
	}
}

// TestEmployee_UpdateToTakenCode: Update refuses another employee's code with
// mstkit's message, not the database's UNIQUE failure.
func TestEmployee_UpdateToTakenCode(t *testing.T) {
	w := newWorld(t)
	w.seedFixtureEmployees(t)

	before := w.snapshot(t)
	err := w.update(t, "EMP-007", func(in *employee.Input) { in.Code = "EMP-004" })
	wantCodeError(t, err, msgCodeTaken)
	if after := w.snapshot(t); after.dump != before.dump {
		t.Errorf("state changed by the refused save:\nbefore\n%s\nafter\n%s", before.dump, after.dump)
	}
}

// TestEmployee_CodeChangeNeedsMastersManager is MST-008 for Employee: ravi
// (Purchase Manager) is given Employee write in this test and can't change
// the code; kavya (Masters Manager) can.
func TestEmployee_CodeChangeNeedsMastersManager(t *testing.T) { // MST-008
	w := newWorld(t)
	ctx := context.Background()
	a, ok := w.env.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Kit.Auth = %T; want the real *auth.Auth", w.env.Kit.Auth)
	}
	rights := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightWrite: auth.ScopeAll}
	if err := a.SetRights(ctx, w.env.Admin, "Purchase Manager", employee.DocType, rights); err != nil {
		t.Fatalf("give Purchase Manager Employee write: %v", err)
	}
	w.create(t, w.input("EMP-009", "Asha Menon"))
	id, version := w.ids["EMP-009"], w.versions["EMP-009"]
	in := w.ins["EMP-009"]
	in.Code = "EMP-010"

	before := w.snapshot(t)
	_, err := w.svc.Update(ctx, w.env.Ravi, id, version, in)
	wantCodeError(t, err, msgCodeChange)
	if after := w.snapshot(t); after.dump != before.dump {
		t.Errorf("state changed by the refused save:\nbefore\n%s\nafter\n%s", before.dump, after.dump)
	}

	if _, err := w.svc.Update(ctx, w.env.Kavya, id, version, in); err != nil {
		t.Fatalf("Kavya changes EMP-009 to EMP-010: %v; want saved", err)
	}
	if e := w.get(t, id); e.Code != "EMP-010" {
		t.Errorf("code after Kavya's save = %q; want EMP-010", e.Code)
	}
}

// TestEmployee_LastAdminCantLeave: admin@ is the only enabled Administrator,
// so EMP-001's leave fails in DisableUser with AUTH's text on both leave
// paths; the transaction rolls back and no version is stored.
func TestEmployee_LastAdminCantLeave(t *testing.T) { // MST-119, CD-189
	for _, path := range []struct {
		name  string
		leave func(t *testing.T, w *world) error
	}{
		{"Update", func(t *testing.T, w *world) error { return w.update(t, "EMP-001", leave) }},
		{"SetDisabled", func(t *testing.T, w *world) error { return w.setDisabled(t, "EMP-001", true) }},
	} {
		t.Run(path.name, func(t *testing.T) {
			w := newWorld(t)
			admin := userID(t, "admin@vardhan.test")
			in := w.input("EMP-001", "Anil Kapoor")
			in.UserID, in.Relieving = admin, date(2026, time.October, 24) // SetDisabled(true) needs it (MST-118)
			w.create(t, in)

			before := w.snapshot(t)
			err := path.leave(t, w)
			if !errors.Is(err, auth.ErrLastAdmin) || err.Error() != msgLastAdmin {
				t.Errorf("EMP-001 leaves = %v; want auth.ErrLastAdmin %q", err, msgLastAdmin)
			}
			if after := w.snapshot(t); after.dump != before.dump {
				t.Errorf("state changed by the refused leave:\nbefore\n%s\nafter\n%s", before.dump, after.dump)
			}
			if !w.userEnabled(t, admin) {
				t.Errorf("admin disabled by the refused leave; want enabled")
			}
			w.wantLeaveVersion(t, "EMP-001", 0)
		})
	}
}
