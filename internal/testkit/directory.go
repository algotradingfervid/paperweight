package testkit

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
)

// Directory returns an auth.Directory fake filled from fx (WP0 addition):
// users linked to employees, managers set, employees with status Left
// inactive, and project and site codes by database id.
func Directory(fx Fixtures) *authfake.Directory {
	dir := authfake.NewDirectory()
	empIDs := map[string]int64{}
	for _, e := range fx.Employees {
		empIDs[e.Code] = e.ID
	}
	for _, e := range fx.Employees {
		dir.Link(e.UserID, e.ID)
		if e.ManagerCode != "" {
			dir.SetManager(e.ID, empIDs[e.ManagerCode])
		}
		if e.Status == "Left" {
			dir.SetActive(e.ID, false)
		}
	}
	for _, p := range fx.Projects {
		dir.SetProjectCode(p.ID, p.Code)
	}
	for _, s := range fx.Sites {
		dir.SetSiteCode(s.ID, s.Code)
	}
	return dir
}

// Principal returns the principal of the fixture user with email, from
// Fixtures.Users (no database read in Wave 1, CD-72).
func Principal(t testing.TB, email string) auth.Principal {
	t.Helper()
	for _, u := range canonical().Users {
		if u.Email == email {
			return auth.Principal{ID: u.ID, Name: u.Name, Roles: u.Roles}
		}
	}
	t.Fatalf("testkit.Principal: no fixture user with email %q; use one from docs/tests/core/00-fixtures.md", email)
	return auth.Principal{}
}
