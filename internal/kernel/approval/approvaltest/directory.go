package approvaltest

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// Emp returns the database id of the fixture employee code ("EMP-007"), or 0.
func (e *Env) Emp(code string) int64 {
	for _, emp := range e.F.Employees {
		if emp.Code == code {
			return emp.ID
		}
	}
	return 0
}

// empOf returns the fixture employee id linked to user u, or 0.
func (e *Env) empOf(u auth.UserID) int64 {
	for _, emp := range e.F.Employees {
		if emp.UserID == u {
			return emp.ID
		}
	}
	return 0
}

// Unlink removes the employee's user link on the shared directory (APR-048).
func (e *Env) Unlink(t testing.TB, code string) {
	t.Helper()
	e.Dir.Unlink(e.Emp(code))
}
