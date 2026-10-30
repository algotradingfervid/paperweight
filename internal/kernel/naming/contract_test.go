package naming_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/kernel/naming/namingtest"
	"github.com/vardhan/internaltools/internal/testkit"
)

// TestContract_NamingReal runs the fake-vs-real suite on the real Namer over
// a migrated test database (WP3 Task 1, CD-182).
func TestContract_NamingReal(t *testing.T) {
	namingtest.Run(t, func(t *testing.T) (naming.Service, namingtest.Setup) {
		d := testkit.NewDB(t)
		roles := authfake.New()
		roles.AddRole(1, "Administrator")
		n := naming.New(d, testkit.Clock(), roles, auditfake.New())
		return n, namingtest.Setup{
			DB:    d,
			Admin: n,
			By:    auth.Principal{ID: 1, Name: "Anil Kapoor", Roles: []string{"Administrator"}},
		}
	})
}
