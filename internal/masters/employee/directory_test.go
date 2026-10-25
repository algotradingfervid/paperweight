package employee_test

import (
	"context"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
)

func (w *world) directory() *employee.Directory {
	return &employee.Directory{Employees: w.svc, Projects: project.New(w.env.Kit), Sites: site.New(w.env.Kit)}
}

func (w *world) read(t *testing.T, fn func(ctx context.Context, tx *db.Tx) error) {
	t.Helper()
	ctx := context.Background()
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatalf("read: %v", err)
	}
}

// TestDirectory_UsersAndReports (no case ID; M10, CD-54): UserForEmployee is
// ok false for a Left employee, one with no user and a missing id;
// EmployeeForUser skips Left employees; DirectReports is active employees
// only, ordered by code (created out of code order here).
func TestDirectory_UsersAndReports(t *testing.T) {
	w := newWorld(t)
	d := w.directory()
	deepak, old := userID(t, "deepak@vardhan.test"), userID(t, "old@vardhan.test")

	w.create(t, w.input("EMP-002", "Meena Iyer"))
	mgr := w.ids["EMP-002"]
	for _, c := range []struct {
		code, name string
		user       auth.UserID
		left       bool
	}{
		{"EMP-007", "Deepak Nair", deepak, false},
		{"EMP-005", "Suresh Patil", 0, false},
		{"EMP-003", "Ravi Kulkarni", 0, false},
		{"EMP-004", "Former Lead", 0, true},
		{"EMP-008", "Former Staff", old, true},
	} {
		in := w.input(c.code, c.name)
		in.UserID, in.ManagerID = c.user, mgr
		if c.left {
			in.Status, in.Relieving = employee.Left, date(2026, time.March, 31)
		}
		w.create(t, in)
	}

	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		for _, c := range []struct {
			name string
			id   int64
			want auth.UserID
			ok   bool
		}{
			{"active EMP-007", w.ids["EMP-007"], deepak, true},
			{"Left EMP-008", w.ids["EMP-008"], 0, false},
			{"EMP-005 without user", w.ids["EMP-005"], 0, false},
			{"missing id", 9999, 0, false},
		} {
			u, ok, err := d.UserForEmployee(ctx, tx, c.id)
			if err != nil || u != c.want || ok != c.ok {
				t.Errorf("UserForEmployee(%s) = (%d, %v, %v); want (%d, %v, nil)", c.name, u, ok, err, c.want, c.ok)
			}
		}
		if id, ok, err := d.EmployeeForUser(ctx, tx, deepak); err != nil || !ok || id != w.ids["EMP-007"] {
			t.Errorf("EmployeeForUser(deepak) = (%d, %v, %v); want (%d EMP-007, true, nil)", id, ok, err, w.ids["EMP-007"])
		}
		if id, ok, err := d.EmployeeForUser(ctx, tx, old); err != nil || ok {
			t.Errorf("EmployeeForUser(old@) = (%d, %v, %v); want (0, false, nil): EMP-008 is Left", id, ok, err)
		}

		want := []int64{w.ids["EMP-003"], w.ids["EMP-005"], w.ids["EMP-007"]}
		got, err := d.DirectReports(ctx, tx, mgr)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("Directory.DirectReports(EMP-002) = (%v, %v); want (%v = EMP-003, EMP-005, EMP-007; Left EMP-004, EMP-008 left out)", got, err, want)
		}
		got, err = w.svc.DirectReports(ctx, tx, mgr)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("Service.DirectReports(EMP-002) = (%v, %v); want %v", got, err, want)
		}
		return nil
	})
}

// TestDirectory_Codes (no case ID; M10): ProjectCodes and SiteCodes map the
// ids asked for to their codes and leave out ids with no row.
func TestDirectory_Codes(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	d := w.directory()

	prj := map[string]int64{}
	for _, c := range []struct{ code, name string }{{"PRJ-TECHPARK", "Tech Park"}, {"PRJ-METRO", "Metro Line 3"}} {
		s, err := d.Projects.Create(ctx, w.env.Kavya, project.Input{Code: c.code, Name: c.name, BranchID: w.branchID})
		if err != nil {
			t.Fatalf("create project %s: %v", c.code, err)
		}
		prj[c.code] = s.ID
	}
	sites := map[string]int64{}
	for _, c := range []struct{ code, name string }{{"PUNE-WH", "Pune Warehouse"}, {"CHAKAN-WH", "Chakan Warehouse"}} {
		s, err := d.Sites.Create(ctx, w.env.Kavya, site.Input{Code: c.code, Name: c.name, BranchID: w.branchID, Type: site.Warehouse})
		if err != nil {
			t.Fatalf("create site %s: %v", c.code, err)
		}
		sites[c.code] = s.ID
	}

	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		wantP := map[int64]string{prj["PRJ-METRO"]: "PRJ-METRO", prj["PRJ-TECHPARK"]: "PRJ-TECHPARK"}
		got, err := d.ProjectCodes(ctx, tx, []int64{prj["PRJ-METRO"], 9999, prj["PRJ-TECHPARK"]})
		if err != nil || !maps.Equal(got, wantP) {
			t.Errorf("ProjectCodes = (%v, %v); want %v", got, err, wantP)
		}
		if got, err := d.ProjectCodes(ctx, tx, []int64{prj["PRJ-TECHPARK"]}); err != nil || !maps.Equal(got, map[int64]string{prj["PRJ-TECHPARK"]: "PRJ-TECHPARK"}) {
			t.Errorf("ProjectCodes(PRJ-TECHPARK only) = (%v, %v); want only PRJ-TECHPARK", got, err)
		}
		wantS := map[int64]string{sites["PUNE-WH"]: "PUNE-WH", sites["CHAKAN-WH"]: "CHAKAN-WH"}
		got, err = d.SiteCodes(ctx, tx, []int64{sites["CHAKAN-WH"], sites["PUNE-WH"], 9999})
		if err != nil || !maps.Equal(got, wantS) {
			t.Errorf("SiteCodes = (%v, %v); want %v", got, err, wantS)
		}
		return nil
	})
}
