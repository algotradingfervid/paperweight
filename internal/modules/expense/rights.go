package expense

import "github.com/vardhan/internaltools/internal/kernel/auth"

// RoleFallbackApprover is the canonical role migration 10370 creates (spec §6,
// CD-238): read and approve (all) on claims and advances, and nothing else.
const RoleFallbackApprover = "Fallback Approver"

// defaultRights are spec §11's rows, the same for both DocTypes. There is no
// Administrator row: Administrator holds every right through is_admin. Each
// call returns new maps, so the two DocTypes share nothing.
func defaultRights() map[string]map[string]string {
	return map[string]map[string]string{
		auth.RoleEmployee: {
			"read": "team", "create": "own", "write": "own", "submit": "own",
			"amend": "own", "print": "own", "delete": "own", "approve": "team",
		},
		"Director":           {"read": "all", "approve": "all", "cancel": "all", "print": "all", "export": "all"},
		RoleFallbackApprover: {"read": "all", "approve": "all"},
	}
}
