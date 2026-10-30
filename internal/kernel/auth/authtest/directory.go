package authtest

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// DirectorySetup builds the org a directory row needs through each side's own
// path: seeding helpers on the fake, the master services on the real one
// (CD-198). Rows use only the ids it returns and build only states the real
// path allows (MST-116, MST-117).
type DirectorySetup interface {
	// Employee adds an active employee with no user and returns its id.
	// managerID is an active employee, or 0 for none.
	Employee(t *testing.T, code string, managerID int64) int64
	// User returns the id of the enabled fixture user with email, who has no
	// employee yet.
	User(t *testing.T, email string) auth.UserID
	// Link makes u the user of the active employee employeeID.
	Link(t *testing.T, employeeID int64, u auth.UserID)
	// Leave marks the employee Left.
	Leave(t *testing.T, employeeID int64)
	// Project adds a project with code and returns its id.
	Project(t *testing.T, code string) int64
	// Site adds a site with code and returns its id.
	Site(t *testing.T, code string) int64
	// Read calls fn with the tx to pass to Directory methods.
	Read(t *testing.T, fn func(ctx context.Context, tx *db.Tx))
}

// RunDirectory runs the auth.Directory contract rows against the directory
// newDir returns; each row gets a fresh directory and DirectorySetup.
func RunDirectory(t *testing.T, newDir func(t *testing.T) (auth.Directory, DirectorySetup)) {
	t.Helper()
	for _, row := range directoryRows {
		t.Run(row.name, func(t *testing.T) {
			d, s := newDir(t)
			row.run(t, d, s)
		})
	}
}

// noSuchID is an id neither side ever hands out.
const noSuchID int64 = 999_999_999

// Fixture users; on the real side these exist and have no employee yet.
const (
	dirUserDeepak = "deepak@vardhan.test"
	dirUserSuresh = "suresh@vardhan.test"
)

var directoryRows = []struct {
	name string
	run  func(t *testing.T, d auth.Directory, s DirectorySetup)
}{
	{"M10 EmployeeForUser active employee found", rowEmployeeForUserActive},
	{"M10 EmployeeForUser Left employee not found", rowEmployeeForUserLeft},
	{"M10 EmployeeForUser unlinked user not found", rowEmployeeForUserUnlinked},
	{"CD-54 UserForEmployee linked found", rowUserForEmployeeLinked},
	{"CD-54 UserForEmployee no user", rowUserForEmployeeNoUser},
	{"CD-54 UserForEmployee Left employee", rowUserForEmployeeLeft},
	{"M10 ManagerChain direct manager first", rowManagerChainOrder},
	{"M10 ManagerChain top of chain empty", rowManagerChainTop},
	{"M10 ManagerChain unknown employee", rowManagerChainUnknown},
	{"M10 DirectReports active only by code", rowDirectReportsByCode},
	{"CD-54 DirectReports Left report omitted", rowDirectReportsLeft},
	{"M10 ProjectCodes", rowProjectCodes},
	{"M10 SiteCodes", rowSiteCodes},
}

func wantEmployeeForUser(ctx context.Context, t *testing.T, d auth.Directory, tx *db.Tx, u auth.UserID, wantID int64, wantOK bool) {
	t.Helper()
	id, ok, err := d.EmployeeForUser(ctx, tx, u)
	if id != wantID || ok != wantOK || err != nil {
		t.Errorf("EmployeeForUser(%d) = (%d, %v, %v); want (%d, %v, nil)", u, id, ok, err, wantID, wantOK)
	}
}

func wantUserForEmployee(ctx context.Context, t *testing.T, d auth.Directory, tx *db.Tx, id int64, wantUser auth.UserID, wantOK bool) {
	t.Helper()
	u, ok, err := d.UserForEmployee(ctx, tx, id)
	if u != wantUser || ok != wantOK || err != nil {
		t.Errorf("UserForEmployee(%d) = (%d, %v, %v); want (%d, %v, nil)", id, u, ok, err, wantUser, wantOK)
	}
}

func rowEmployeeForUserActive(t *testing.T, d auth.Directory, s DirectorySetup) {
	deepak, suresh := s.User(t, dirUserDeepak), s.User(t, dirUserSuresh)
	first, second := s.Employee(t, "EMP-007", 0), s.Employee(t, "EMP-005", 0)
	s.Link(t, first, deepak)
	s.Link(t, second, suresh)
	s.Read(t, func(ctx context.Context, tx *db.Tx) {
		wantEmployeeForUser(ctx, t, d, tx, deepak, first, true)
		wantEmployeeForUser(ctx, t, d, tx, suresh, second, true)
	})
}

func rowEmployeeForUserLeft(t *testing.T, d auth.Directory, s DirectorySetup) {
	u := s.User(t, dirUserDeepak)
	e := s.Employee(t, "EMP-007", 0)
	s.Link(t, e, u)
	s.Leave(t, e)
	s.Read(t, func(ctx context.Context, tx *db.Tx) {
		wantEmployeeForUser(ctx, t, d, tx, u, 0, false)
	})
}

func rowEmployeeForUserUnlinked(t *testing.T, d auth.Directory, s DirectorySetup) {
	linked, unlinked := s.User(t, dirUserDeepak), s.User(t, dirUserSuresh)
	s.Link(t, s.Employee(t, "EMP-007", 0), linked)
	s.Read(t, func(ctx context.Context, tx *db.Tx) {
		wantEmployeeForUser(ctx, t, d, tx, unlinked, 0, false)
	})
}

func rowUserForEmployeeLinked(t *testing.T, d auth.Directory, s DirectorySetup) {
	deepak, suresh := s.User(t, dirUserDeepak), s.User(t, dirUserSuresh)
	first, second := s.Employee(t, "EMP-007", 0), s.Employee(t, "EMP-005", 0)
	s.Link(t, first, deepak)
	s.Link(t, second, suresh)
	s.Read(t, func(ctx context.Context, tx *db.Tx) {
		wantUserForEmployee(ctx, t, d, tx, first, deepak, true)
		wantUserForEmployee(ctx, t, d, tx, second, suresh, true)
	})
}

func rowUserForEmployeeNoUser(t *testing.T, d auth.Directory, s DirectorySetup) {
	e := s.Employee(t, "EMP-005", 0)
	s.Link(t, s.Employee(t, "EMP-007", 0), s.User(t, dirUserDeepak))
	s.Read(t, func(ctx context.Context, tx *db.Tx) {
		wantUserForEmployee(ctx, t, d, tx, e, 0, false)
	})
}

func rowUserForEmployeeLeft(t *testing.T, d auth.Directory, s DirectorySetup) {
	e := s.Employee(t, "EMP-007", 0)
	s.Link(t, e, s.User(t, dirUserDeepak))
	s.Leave(t, e)
	s.Read(t, func(ctx context.Context, tx *db.Tx) {
		wantUserForEmployee(ctx, t, d, tx, e, 0, false)
	})
}

func rowManagerChainOrder(t *testing.T, d auth.Directory, s DirectorySetup) {
	top := s.Employee(t, "EMP-002", 0)
	mid := s.Employee(t, "EMP-003", top)
	lead := s.Employee(t, "EMP-004", mid)
	e := s.Employee(t, "EMP-007", lead)
	s.Read(t, func(ctx context.Context, tx *db.Tx) {
		want := []int64{lead, mid, top}
		got, err := d.ManagerChain(ctx, tx, e)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("ManagerChain(EMP-007) = (%v, %v); want (%v = EMP-004, EMP-003, EMP-002; nil)", got, err, want)
		}
	})
}

func rowManagerChainTop(t *testing.T, d auth.Directory, s DirectorySetup) {
	top := s.Employee(t, "EMP-002", 0)
	s.Employee(t, "EMP-003", top)
	s.Read(t, func(ctx context.Context, tx *db.Tx) {
		got, err := d.ManagerChain(ctx, tx, top)
		if err != nil || len(got) != 0 {
			t.Errorf("ManagerChain(EMP-002) = (%v, %v); want (empty, nil): it has no manager", got, err)
		}
	})
}

func rowManagerChainUnknown(t *testing.T, d auth.Directory, s DirectorySetup) {
	s.Employee(t, "EMP-002", 0)
	s.Read(t, func(ctx context.Context, tx *db.Tx) {
		got, err := d.ManagerChain(ctx, tx, noSuchID)
		if !errors.Is(err, auth.ErrNotFound) || len(got) != 0 {
			t.Errorf("ManagerChain(unknown) = (%v, %v); want (empty, auth.ErrNotFound)", got, err)
		}
	})
}

func rowDirectReportsByCode(t *testing.T, d auth.Directory, s DirectorySetup) {
	boss := s.Employee(t, "EMP-001", 0)
	mgr := s.Employee(t, "EMP-002", boss)
	// Created out of code order, so id order is not code order.
	r7 := s.Employee(t, "EMP-007", mgr)
	r5 := s.Employee(t, "EMP-005", mgr)
	r3 := s.Employee(t, "EMP-003", mgr)
	s.Employee(t, "EMP-009", r7) // an indirect report
	s.Read(t, func(ctx context.Context, tx *db.Tx) {
		want := []int64{r3, r5, r7}
		got, err := d.DirectReports(ctx, tx, mgr)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("DirectReports(EMP-002) = (%v, %v); want (%v = EMP-003, EMP-005, EMP-007; nil)", got, err, want)
		}
	})
}

func rowDirectReportsLeft(t *testing.T, d auth.Directory, s DirectorySetup) {
	mgr := s.Employee(t, "EMP-002", 0)
	gone := s.Employee(t, "EMP-003", mgr)
	stays := s.Employee(t, "EMP-005", mgr)
	s.Leave(t, gone)
	s.Read(t, func(ctx context.Context, tx *db.Tx) {
		want := []int64{stays}
		got, err := d.DirectReports(ctx, tx, mgr)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("DirectReports(EMP-002) = (%v, %v); want (%v = EMP-005 only, Left EMP-003 omitted; nil)", got, err, want)
		}
	})
}

func rowProjectCodes(t *testing.T, d auth.Directory, s DirectorySetup) {
	wantCodes(t, s, d.ProjectCodes, s.Project(t, "PRJ-TECHPARK"), "PRJ-TECHPARK", s.Project(t, "PRJ-METRO"), "PRJ-METRO")
}

func rowSiteCodes(t *testing.T, d auth.Directory, s DirectorySetup) {
	wantCodes(t, s, d.SiteCodes, s.Site(t, "PUNE-WH"), "PUNE-WH", s.Site(t, "CHAKAN-WH"), "CHAKAN-WH")
}

// wantCodes checks one codes method: ids asked for are mapped, an unknown id
// and ids not asked for are omitted, and an empty input maps nothing.
func wantCodes(t *testing.T, s DirectorySetup, codes func(context.Context, *db.Tx, []int64) (map[int64]string, error), a int64, codeA string, b int64, codeB string) {
	t.Helper()
	s.Read(t, func(ctx context.Context, tx *db.Tx) {
		for _, c := range []struct {
			name string
			ids  []int64
			want map[int64]string
		}{
			{"both known and one unknown", []int64{b, noSuchID, a}, map[int64]string{a: codeA, b: codeB}},
			{"one known", []int64{a}, map[int64]string{a: codeA}},
			{"only unknown", []int64{noSuchID}, map[int64]string{}},
			{"empty", []int64{}, map[int64]string{}},
			{"nil", nil, map[int64]string{}},
		} {
			got, err := codes(ctx, tx, c.ids)
			if err != nil || !maps.Equal(got, c.want) {
				t.Errorf("%s: codes(%v) = (%v, %v); want (%v, nil)", c.name, c.ids, got, err, c.want)
			}
		}
	})
}
