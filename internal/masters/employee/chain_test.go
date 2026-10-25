package employee_test

import (
	"context"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/masters/employee"
)

// Messages copied verbatim from docs/tests/core/06-masters.md.
const (
	msgSelfManager  = "An employee can't report to themselves."                                                         // MST-113
	msgLoop         = "This would make a reporting loop (EMP-002 → EMP-004 → EMP-003 → EMP-002). Pick another manager." // MST-114
	msgManagerLeft  = "Reporting manager EMP-008 has left. Pick an active manager."                                     // MST-116
	msgHasReportees = "EMP-007 (Deepak Nair) still reports to EMP-004. Move them to another manager first."             // MST-117
)

func TestEmployee_NoSelfManager(t *testing.T) { // MST-113
	w := newWorld(t)
	w.create(t, w.input("EMP-007", "Deepak Nair"))
	id, version := w.ids["EMP-007"], w.versions["EMP-007"]

	err := w.update(t, "EMP-007", func(in *employee.Input) { in.ManagerID = id })
	wantFieldError(t, err, msgSelfManager)
	if e := w.get(t, id); e.ManagerID != 0 || e.Version != version {
		t.Errorf("EMP-007 after refused save = {ManagerID %d, Version %d}; want {0, %d}", e.ManagerID, e.Version, version)
	}
}

func TestEmployee_NoManagerCycle(t *testing.T) { // MST-114
	w := newWorld(t)
	w.seedFixtureEmployees(t) // EMP-004 → EMP-003 → EMP-002
	id, version := w.ids["EMP-002"], w.versions["EMP-002"]

	err := w.update(t, "EMP-002", func(in *employee.Input) { in.ManagerID = w.ids["EMP-004"] })
	wantFieldError(t, err, msgLoop)
	if e := w.get(t, id); e.ManagerID != 0 || e.Version != version {
		t.Errorf("EMP-002 after refused save = {ManagerID %d, Version %d}; want {0, %d}", e.ManagerID, e.Version, version)
	}

	// A manager outside the chain is fine: EMP-002 may report to EMP-001.
	if err := w.update(t, "EMP-002", func(in *employee.Input) { in.ManagerID = w.ids["EMP-001"] }); err != nil {
		t.Errorf("EMP-002 manager EMP-001: %v; want saved", err)
	}
}

func TestEmployee_ManagerActive(t *testing.T) { // MST-116
	w := newWorld(t)
	w.seedFixtureEmployees(t) // EMP-008 is Left
	id, version := w.ids["EMP-007"], w.versions["EMP-007"]

	err := w.update(t, "EMP-007", func(in *employee.Input) { in.ManagerID = w.ids["EMP-008"] })
	wantFieldError(t, err, msgManagerLeft)
	if e := w.get(t, id); e.ManagerID != w.ids["EMP-004"] || e.Version != version {
		t.Errorf("EMP-007 after refused save = {ManagerID %d, Version %d}; want {%d (EMP-004), %d}", e.ManagerID, e.Version, w.ids["EMP-004"], version)
	}

	// A new employee can't take the Left manager either.
	in := w.input("EMP-009", "Asha Menon")
	in.ManagerID = w.ids["EMP-008"]
	_, err = w.svc.Create(context.Background(), w.env.Kavya, in)
	wantFieldError(t, err, msgManagerLeft)
	w.wantNoCode(t, "EMP-009")
}

func TestEmployee_LeftWithReportees(t *testing.T) { // MST-117
	w := newWorld(t)
	w.seedFixtureEmployees(t) // EMP-007 (Deepak Nair) reports to EMP-004
	id, version := w.ids["EMP-004"], w.versions["EMP-004"]
	priya := userID(t, "priya@vardhan.test")

	err := w.update(t, "EMP-004", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Left, date(2026, time.October, 24)
	})
	wantFieldError(t, err, msgHasReportees)
	if e := w.get(t, id); e.Status != employee.Active || e.Disabled || e.Version != version {
		t.Errorf("EMP-004 after refused Left = {Status %q, Disabled %v, Version %d}; want {Active, false, %d}", e.Status, e.Disabled, e.Version, version)
	}
	if !w.userEnabled(t, priya) {
		t.Errorf("priya disabled after the refused Left; want enabled")
	}

	// Once EMP-007 reports to EMP-003, EMP-004 may leave.
	if err := w.update(t, "EMP-007", func(in *employee.Input) { in.ManagerID = w.ids["EMP-003"] }); err != nil {
		t.Fatalf("move EMP-007 to EMP-003: %v", err)
	}
	if err := w.update(t, "EMP-004", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Left, date(2026, time.October, 24)
	}); err != nil {
		t.Errorf("EMP-004 Left with no reportees: %v; want saved", err)
	}
}
