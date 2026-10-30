package employee_test

import (
	"context"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// dirSetup is authtest.DirectorySetup through the master services.
type dirSetup struct {
	w     *world
	codes map[int64]string
}

func (s *dirSetup) Employee(t *testing.T, code string, managerID int64) int64 {
	t.Helper()
	in := s.w.input(code, "Employee "+code)
	in.ManagerID = managerID
	s.w.create(t, in)
	id := s.w.ids[code]
	s.codes[id] = code
	return id
}

func (s *dirSetup) User(t *testing.T, email string) auth.UserID {
	t.Helper()
	return userID(t, email)
}

func (s *dirSetup) Link(t *testing.T, employeeID int64, u auth.UserID) {
	t.Helper()
	if err := s.w.update(t, s.codes[employeeID], func(in *employee.Input) { in.UserID = u }); err != nil {
		t.Fatalf("link %s: %v", s.codes[employeeID], err)
	}
}

func (s *dirSetup) Leave(t *testing.T, employeeID int64) {
	t.Helper()
	if err := s.w.update(t, s.codes[employeeID], func(in *employee.Input) {
		in.Status, in.Relieving = employee.Left, date(2026, time.March, 31)
	}); err != nil {
		t.Fatalf("leave %s: %v", s.codes[employeeID], err)
	}
}

func (s *dirSetup) Project(t *testing.T, code string) int64 {
	t.Helper()
	r, err := project.New(s.w.env.Kit).Create(context.Background(), s.w.env.Kavya, project.Input{Code: code, Name: "Project " + code, BranchID: s.w.branchID})
	if err != nil {
		t.Fatalf("create project %s: %v", code, err)
	}
	return r.ID
}

func (s *dirSetup) Site(t *testing.T, code string) int64 {
	t.Helper()
	r, err := site.New(s.w.env.Kit).Create(context.Background(), s.w.env.Kavya, site.Input{Code: code, Name: "Site " + code, BranchID: s.w.branchID, Type: site.Warehouse})
	if err != nil {
		t.Fatalf("create site %s: %v", code, err)
	}
	return r.ID
}

func (s *dirSetup) Read(t *testing.T, fn func(ctx context.Context, tx *db.Tx)) {
	t.Helper()
	s.w.read(t, func(ctx context.Context, tx *db.Tx) error {
		fn(ctx, tx)
		return nil
	})
}

// CD-198: the real Directory passes the same suite as authfake.NewDirectory.
func TestDirectoryContract_Real(t *testing.T) {
	authtest.RunDirectory(t, func(t *testing.T) (auth.Directory, authtest.DirectorySetup) {
		w := newWorld(t)
		return w.directory(), &dirSetup{w: w, codes: map[int64]string{}}
	})
}
