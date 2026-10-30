package authfake_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// dirSetup is authtest.DirectorySetup through the fake's seeding helpers. Ids
// start far from the real side's, so a row can't lean on either id space.
type dirSetup struct {
	dir  *authfake.Directory
	next int64
}

func (s *dirSetup) id() int64 {
	s.next++
	return s.next
}

func (s *dirSetup) Employee(t *testing.T, code string, managerID int64) int64 {
	t.Helper()
	id := s.id()
	s.dir.SetEmployeeCode(id, code)
	s.dir.SetManager(id, managerID)
	return id
}

func (s *dirSetup) User(t *testing.T, _ string) auth.UserID {
	t.Helper()
	return auth.UserID(s.id())
}

func (s *dirSetup) Link(t *testing.T, employeeID int64, u auth.UserID) {
	t.Helper()
	s.dir.Link(u, employeeID)
}

func (s *dirSetup) Leave(t *testing.T, employeeID int64) {
	t.Helper()
	s.dir.SetActive(employeeID, false)
}

func (s *dirSetup) Project(t *testing.T, code string) int64 {
	t.Helper()
	id := s.id()
	s.dir.SetProjectCode(id, code)
	return id
}

func (s *dirSetup) Site(t *testing.T, code string) int64 {
	t.Helper()
	id := s.id()
	s.dir.SetSiteCode(id, code)
	return id
}

func (s *dirSetup) Read(t *testing.T, fn func(ctx context.Context, tx *db.Tx)) {
	t.Helper()
	fn(context.Background(), nil)
}

// CD-198: authfake.NewDirectory passes the same suite as employee.Directory.
func TestDirectoryContract_Authfake(t *testing.T) {
	authtest.RunDirectory(t, func(t *testing.T) (auth.Directory, authtest.DirectorySetup) {
		dir := authfake.NewDirectory()
		return dir, &dirSetup{dir: dir, next: 5000}
	})
}
