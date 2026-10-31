package purchase_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// p23Readable is ReadableOrders for p's read scope (asked before the tx, as
// the doc comment says), applied to ids inside one read.
func p23Readable(t *testing.T, env *purchasetest.Env, p auth.Principal, ids ...int64) []bool {
	t.Helper()
	ctx := t.Context()
	pred, err := env.Auth.Scope(ctx, p, purchase.DocType, auth.RightRead)
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}
	var got []bool
	err = env.DB.Read(ctx, func(tx *db.Tx) error {
		readable, err := purchase.ReadableOrders(ctx, tx, pred)
		if err != nil {
			return err
		}
		for _, id := range ids {
			got = append(got, readable(id))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ReadableOrders: %v", err)
	}
	return got
}

// TestPO_ReadableOrders: Priya (restricted to PRJ-METRO) reads her METRO PO
// and not Arjun's TECHPARK PO, nor an id that doesn't exist; the
// Administrator's empty predicate reads both (MOD-PO P23, for GRN G6).
func TestPO_ReadableOrders(t *testing.T) {
	env := purchasetest.New(t)
	metro := p7Priya(t, env, "SHREE", "01-06-2026", purchasetest.StopAtDraft(), p7Line(t, env, "BOLT-M12", "NOS", "10", "")).ID
	tech := p7Arjun(t, env, "SHREE", "2026-06-01", false, p7Line(t, env, "BOLT-M12", "NOS", "10", "")).ID
	missing := metro + tech + 1000

	pred, err := env.Auth.Scope(t.Context(), env.Priya, purchase.DocType, auth.RightRead)
	if err != nil || pred.SQL == "" {
		t.Fatalf("Priya's read scope = %+v, %v; want a non-empty predicate", pred, err)
	}
	if got := p23Readable(t, env, env.Priya, metro, tech, missing); !got[0] || got[1] || got[2] {
		t.Errorf("Priya: METRO %d, TECHPARK %d, missing %d readable = %v, want [true false false]", metro, tech, missing, got)
	}

	pred, err = env.Auth.Scope(t.Context(), env.Admin, purchase.DocType, auth.RightRead)
	if err != nil || pred.SQL != "" {
		t.Fatalf("Administrator's read scope = %+v, %v; want the empty predicate", pred, err)
	}
	if got := p23Readable(t, env, env.Admin, metro, tech); !got[0] || !got[1] {
		t.Errorf("Administrator: METRO %d, TECHPARK %d readable = %v, want [true true]", metro, tech, got)
	}
}
