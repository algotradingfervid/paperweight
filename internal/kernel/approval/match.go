package approval

import (
	"cmp"
	"slices"

	"github.com/vardhan/internaltools/internal/types"
)

// match picks, from the enabled rules of one DocType (any order), the rule
// with the highest FromAmount <= amount. higher are the rules with a larger
// FromAmount than matched, ascending; their approvers may also approve.
func match(rules []Rule, amount types.Money) (matched Rule, higher []Rule, ok bool) {
	for _, r := range rules {
		if r.FromAmount <= amount && (!ok || r.FromAmount > matched.FromAmount) {
			matched, ok = r, true
		}
	}
	if !ok {
		return Rule{}, nil, false
	}
	for _, r := range rules {
		if r.FromAmount > matched.FromAmount {
			higher = append(higher, r)
		}
	}
	slices.SortFunc(higher, func(a, b Rule) int { return cmp.Compare(a.FromAmount, b.FromAmount) })
	return matched, higher, true
}

// approverRoles lists the roles that may approve: the matched rule's role
// (the fallback role for a reporting-manager rule), then each higher rule's,
// without duplicates, in that order.
func approverRoles(matched Rule, higher []Rule) []string {
	var roles []string
	for _, r := range append([]Rule{matched}, higher...) {
		role := r.Role
		if r.ApproverType == ApproverManager {
			role = r.FallbackRole
		}
		if !slices.Contains(roles, role) {
			roles = append(roles, role)
		}
	}
	return roles
}
