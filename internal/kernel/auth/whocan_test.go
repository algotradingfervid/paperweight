package auth_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// Task 19 interface: WhoCan's refusals (unknown DocType, inapplicable
// right), a role whose only holder is disabled, ordering by role name, and
// no audit entry (read-only). AUTH-120 itself is TestWhoCan_RolesAndUsers
// in web/p2_whocan_test.go.
func TestWhoCan_ErrorsAndEmptyRole(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)

	if _, err := w.Auth.WhoCan(ctx, "authtest.Nope", auth.RightRead); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("WhoCan(unknown DocType) err = %v, want auth.ErrNotFound", err)
	}

	_, err := w.Auth.WhoCan(ctx, authtest.DTItem, auth.RightApprove)
	const notApplicable = "Approve does not apply to Item. Clear it."
	if err == nil || err.Error() != notApplicable {
		t.Fatalf("WhoCan(Item, approve) err = %v, want %q", err, notApplicable)
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != 400 {
		t.Fatalf("WhoCan(Item, approve) err = %#v, want a 400 doctype.HTTPError", err)
	}

	// "Claims Auditor" sorts before Director and holds approve (all) on
	// Expense Claim; its only member, Former Staff, is disabled.
	w.SetRights(t, "Claims Auditor", authtest.DTClaim, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightApprove: auth.ScopeAll,
	})
	w.AddRole(t, "old@vardhan.test", "Claims Auditor")
	before := len(w.Audit.Entries())

	got, err := w.Auth.WhoCan(ctx, authtest.DTClaim, auth.RightApprove)
	if err != nil {
		t.Fatalf("WhoCan(Claim, approve): %v", err)
	}
	var roles []string
	for _, g := range got {
		roles = append(roles, g.Role+" · "+string(g.Scope))
	}
	wantRoles := []string{"Administrator · all", "Claims Auditor · all", "Director · all", "Employee · team"}
	if !reflect.DeepEqual(roles, wantRoles) {
		t.Fatalf("WhoCan(Claim, approve) roles = %q, want %q", roles, wantRoles)
	}
	if n := len(got[1].Users); n != 0 {
		t.Fatalf("Claims Auditor users = %+v, want none (Former Staff is disabled)", got[1].Users)
	}
	if after := len(w.Audit.Entries()); after != before {
		t.Fatalf("WhoCan wrote %d audit entries, want 0 (read-only)", after-before)
	}
}
