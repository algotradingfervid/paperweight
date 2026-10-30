package allmasters_test

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/db"
)

func seeded(t *testing.T) (*msttest.Env, msttest.Masters, *allmasters.Services) {
	t.Helper()
	env := msttest.New(t)
	return env, msttest.SeedMasters(t, env), allmasters.New(env.Kit)
}

func read(t *testing.T, env *msttest.Env, fn func(ctx context.Context, tx *db.Tx) error) {
	t.Helper()
	ctx := context.Background()
	if err := env.DB.Read(ctx, func(tx *db.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatalf("read: %v", err)
	}
}

// TestMaster_LookupReportsState: each master's registered Lookup gives both
// Code and Name, so DOC can show {Code} {Name} (CD-68), and the state its rule
// sets (Project Completed and Employee Left are Blocked).
func TestMaster_LookupReportsState(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()

	var p project.Project
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		var err error
		p, err = svc.Project.Get(ctx, tx, m.Projects["PRJ-METRO"])
		return err
	})
	if _, err := svc.Project.Update(ctx, env.Admin, p.ID, p.Version,
		project.Input{Code: p.Code, Name: p.Name, BranchID: p.BranchID, Status: project.Completed}); err != nil {
		t.Fatalf("complete PRJ-METRO: %v", err)
	}

	for _, c := range []struct {
		docType string
		id      int64
		want    doctype.MasterInfo
	}{
		{party.DocType, m.Parties["SHREE"], doctype.MasterInfo{Code: "SHREE", Name: "Shree Steel Traders"}},
		{item.DocType, m.Items["MS-ANGLE-50"], doctype.MasterInfo{Code: "MS-ANGLE-50", Name: "MS Angle 50x50x5"}},
		{project.DocType, m.Projects["PRJ-METRO"], doctype.MasterInfo{Code: "PRJ-METRO", Name: "Pune Metro Depot",
			Blocked: "Project PRJ-METRO is Completed. Reopen it or pick another project."}},
		{employee.DocType, m.Employees["EMP-008"], doctype.MasterInfo{Code: "EMP-008", Name: "Former Staff",
			Blocked: "Employee EMP-008 has left. Pick an active employee."}},
	} {
		dt, ok := doctype.Lookup(c.docType)
		if !ok || dt.Master == nil {
			t.Fatalf("doctype.Lookup(%s) = %v, %v; want a master", c.docType, dt, ok)
		}
		read(t, env, func(ctx context.Context, tx *db.Tx) error {
			got, found, err := dt.Master.Lookup(ctx, tx, c.id)
			if err != nil || !found || got != c.want {
				t.Errorf("%s Lookup = %+v, %v, %v; want %+v", c.docType, got, found, err, c.want)
			}
			return nil
		})
	}
}

// TestDirectory_FixtureChain has no case ID (CD-13): the real directory over
// the seeded employees (M10).
func TestDirectory_FixtureChain(t *testing.T) {
	env, m, svc := seeded(t)
	emp := func(codes ...string) []int64 {
		out := []int64{}
		for _, c := range codes {
			out = append(out, m.Employees[c])
		}
		return out
	}
	d := svc.Directory
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		if id, ok, err := d.EmployeeForUser(ctx, tx, env.Priya.ID); err != nil || !ok || id != m.Employees["EMP-004"] {
			t.Errorf("EmployeeForUser(priya) = %d, %v, %v; want EMP-004", id, ok, err)
		}
		old := env.Fx.Users[7]
		if old.Email != "old@vardhan.test" {
			t.Fatalf("Fx.Users[7] = %s; want old@vardhan.test", old.Email)
		}
		if id, ok, err := d.EmployeeForUser(ctx, tx, old.ID); err != nil || ok {
			t.Errorf("EmployeeForUser(old@) = %d, %v, %v; want ok false (EMP-008 left)", id, ok, err)
		}
		if u, ok, err := d.UserForEmployee(ctx, tx, m.Employees["EMP-003"]); err != nil || !ok || u != env.Ravi.ID {
			t.Errorf("UserForEmployee(EMP-003) = %d, %v, %v; want Ravi", u, ok, err)
		}
		for _, c := range []struct {
			name string
			fn   func(context.Context, *db.Tx, int64) ([]int64, error)
			of   string
			want []int64
		}{
			{"ManagerChain", d.ManagerChain, "EMP-004", emp("EMP-003", "EMP-002")},
			{"ManagerChain", d.ManagerChain, "EMP-002", emp()},
			{"DirectReports", d.DirectReports, "EMP-003", emp("EMP-004", "EMP-005")},
			{"DirectReports", d.DirectReports, "EMP-004", emp("EMP-007")},
		} {
			got, err := c.fn(ctx, tx, m.Employees[c.of])
			if err != nil || !slices.Equal(got, c.want) {
				t.Errorf("%s(%s) = %v, %v; want %v", c.name, c.of, got, err, c.want)
			}
		}

		ids := func(codes map[string]int64) []int64 {
			out := []int64{}
			for _, id := range codes {
				out = append(out, id)
			}
			return out
		}
		reverse := func(codes map[string]int64) map[int64]string {
			out := map[int64]string{}
			for c, id := range codes {
				out[id] = c
			}
			return out
		}
		projects, err := d.ProjectCodes(ctx, tx, ids(m.Projects))
		if want := reverse(m.Projects); err != nil || !reflect.DeepEqual(projects, want) || len(want) != 2 {
			t.Errorf("ProjectCodes = %v, %v; want %v", projects, err, want)
		}
		sites, err := d.SiteCodes(ctx, tx, ids(m.Sites))
		if want := reverse(m.Sites); err != nil || !reflect.DeepEqual(sites, want) || len(want) != 3 {
			t.Errorf("SiteCodes = %v, %v; want %v", sites, err, want)
		}
		return nil
	})
}

// TestLetterhead_ForBranch has no case ID (CD-30, CD-154): New wires the
// branch's address lines; LogoURL stays nil for WP3, so the brand logo shows.
func TestLetterhead_ForBranch(t *testing.T) {
	env, m, svc := seeded(t)
	if svc.Letterhead.LogoURL != nil {
		t.Error("Letterhead.LogoURL is set; WP3 sets it")
	}
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		got, err := svc.Letterhead.ForBranch(ctx, tx, m.Branches["PUNE"])
		if err != nil {
			t.Fatalf("ForBranch(PUNE): %v", err)
		}
		if got.LegalName != "Vardhan Infra Projects Pvt Ltd" || got.GSTIN != "27AAFCV2449G1Z7" ||
			got.StateName != "Maharashtra" || got.StateCode != "27" || got.BranchName != "Pune Works" || got.LogoURL != brand.DocumentLogoURL {
			t.Errorf("ForBranch(PUNE) = %+v", got)
		}
		want := []string{"Main Road", "Pune - 411019", "Maharashtra (27)", "GSTIN: 27AAFCV2449G1Z7"}
		if !slices.Equal(got.Address, want) {
			t.Errorf("ForBranch(PUNE).Address = %q; want %q", got.Address, want)
		}
		return nil
	})
}
