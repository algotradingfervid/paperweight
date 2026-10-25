package naming

import (
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/types"
)

// Shared fixtures (00-fixtures.md). Used from Task 2 on.

func day(y int, m time.Month, d int) types.Date { return types.Date{Year: y, Month: m, Day: d} }

// fixedClock is a clock.Clock at a chosen instant, for the few cases that
// need one other than testkit.Clock().
type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

// newRoles returns authfake seeded with the fixture users' roles (CD-56).
func newRoles() *authfake.Fake {
	f := authfake.New()
	f.AddRole(1, "Administrator")
	f.AddRole(3, "Purchase Manager")
	f.AddRole(3, "Employee")
	f.AddRole(4, "Purchase User")
	f.AddRole(4, "Employee")
	return f
}

//nolint:unused // shared fixtures, used by later tasks' tests
var (
	admin = auth.Principal{ID: 1, Name: "Anil Kapoor", Roles: []string{"Administrator"}}
	ravi  = auth.Principal{ID: 3, Name: "Ravi Kulkarni", Roles: []string{"Purchase Manager", "Employee"}}
	priya = auth.Principal{ID: 4, Name: "Priya Shah", Roles: []string{"Purchase User", "Employee"}}
)
