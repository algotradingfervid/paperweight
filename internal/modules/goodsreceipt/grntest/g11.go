package grntest

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
)

// MOD-GRN G11 helpers (duplicate challan warning, CD-337 (6)).

// ReadOnlyReceipts creates a user whose only role, "Receipt Read Only",
// holds read on goodsreceipt.Receipt at scope all and no other right on any
// DocType, through the real AUTH, and returns the principal: a rights test
// needs a user holding only the right the rule names (lessons §11).
func (e *Env) ReadOnlyReceipts(t testing.TB) auth.Principal {
	t.Helper()
	ctx := context.Background()
	if err := e.Auth.CreateRole(ctx, e.Admin, "Receipt Read Only"); err != nil {
		t.Fatalf("grntest: create the Receipt Read Only role: %v", err)
	}
	if err := e.Auth.SetRights(ctx, e.Admin, "Receipt Read Only", goodsreceipt.DocType,
		map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}); err != nil {
		t.Fatalf("grntest: Receipt Read Only rights: %v", err)
	}
	id, _, err := e.Auth.CreateUser(ctx, e.Admin, auth.NewUser{ //nolint:gosec // G101: a test user's password
		Email: "rcponly@vardhan.test", Name: "Receipt Read Only", Password: "Looking, not touching 42", Roles: []string{"Receipt Read Only"},
	})
	if err != nil {
		t.Fatalf("grntest: create the Receipt Read Only user: %v", err)
	}
	p, _, err := e.Auth.LoadPrincipal(ctx, id)
	if err != nil {
		t.Fatalf("grntest: load the Receipt Read Only user: %v", err)
	}
	return p
}
