package grntest

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// MOD-GRN G7 helpers (bill details).

// BillClerk creates a user whose only role, "Bill Clerk", holds read and
// write on goodsreceipt.Receipt at scope all and nothing else, through the
// real AUTH, and returns the principal. A rights test needs a user holding
// only the right the rule names (lessons §11): read to see the GRN, write
// for the bill.
func (e *Env) BillClerk(t testing.TB) auth.Principal {
	t.Helper()
	ctx := context.Background()
	if err := e.Auth.CreateRole(ctx, e.Admin, "Bill Clerk"); err != nil {
		t.Fatalf("grntest: create the Bill Clerk role: %v", err)
	}
	if err := e.Auth.SetRights(ctx, e.Admin, "Bill Clerk", goodsreceipt.DocType,
		map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightWrite: auth.ScopeAll}); err != nil {
		t.Fatalf("grntest: Bill Clerk rights: %v", err)
	}
	id, _, err := e.Auth.CreateUser(ctx, e.Admin, auth.NewUser{ //nolint:gosec // G101: a test user's password
		Email: "bills@vardhan.test", Name: "Bill Clerk", Password: "Late invoices go here 42", Roles: []string{"Bill Clerk"},
	})
	if err != nil {
		t.Fatalf("grntest: create the Bill Clerk user: %v", err)
	}
	p, _, err := e.Auth.LoadPrincipal(ctx, id)
	if err != nil {
		t.Fatalf("grntest: load the Bill Clerk user: %v", err)
	}
	return p
}

// OwnReceiver creates a user whose only role, "Own Receiver", holds read,
// create, write and submit on goodsreceipt.Receipt at scope own and read on
// purchase.Order at scope all (to post a PO, CD-70), through the real AUTH,
// and returns the principal: a GRN write right that depends on the record's
// creator (G7 review).
func (e *Env) OwnReceiver(t testing.TB) auth.Principal {
	t.Helper()
	ctx := context.Background()
	if err := e.Auth.CreateRole(ctx, e.Admin, "Own Receiver"); err != nil {
		t.Fatalf("grntest: create the Own Receiver role: %v", err)
	}
	own := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn, auth.RightCreate: auth.ScopeOwn, auth.RightWrite: auth.ScopeOwn, auth.RightSubmit: auth.ScopeOwn}
	if err := e.Auth.SetRights(ctx, e.Admin, "Own Receiver", goodsreceipt.DocType, own); err != nil {
		t.Fatalf("grntest: Own Receiver rights on the GRN: %v", err)
	}
	if err := e.Auth.SetRights(ctx, e.Admin, "Own Receiver", purchase.DocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}); err != nil {
		t.Fatalf("grntest: Own Receiver rights on the PO: %v", err)
	}
	id, _, err := e.Auth.CreateUser(ctx, e.Admin, auth.NewUser{ //nolint:gosec // G101: a test user's password
		Email: "ownrcv@vardhan.test", Name: "Own Receiver", Password: "Trucks at the gate 42", Roles: []string{"Own Receiver"},
	})
	if err != nil {
		t.Fatalf("grntest: create the Own Receiver user: %v", err)
	}
	p, _, err := e.Auth.LoadPrincipal(ctx, id)
	if err != nil {
		t.Fatalf("grntest: load the Own Receiver user: %v", err)
	}
	return p
}
