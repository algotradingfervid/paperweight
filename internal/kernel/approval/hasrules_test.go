package approval_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// HasRules (CD-280 D4, CD-258 Q7(a)) answers whether an enabled approval rule
// exists for a DocType, the rules submit evaluates. It takes no Principal,
// so every caller (a non-admin's page included) can ask; it reads through
// the caller's tx, here a write tx on a
// one-connection pool so a second connection would stall.
func hasRules(t *testing.T, e *at.Env, dt string) bool {
	t.Helper()
	var got bool
	err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		return e.DB.Write(ctx, func(tx *db.Tx) error {
			var err error
			got, err = e.A.HasRules(ctx, tx, dt)
			return err
		})
	})
	at.MustOK(t, err)
	return got
}

func TestHasRules(t *testing.T) {
	e := at.New(t) // seed rules: R1 and R2 on Order, E1 on Claim; none on Receipt
	if hasRules(t, e, at.Receipt) {
		t.Error("Receipt with rules only on Order and Claim: HasRules = true, want false")
	}
	if !hasRules(t, e, at.Order) {
		t.Error("Order with R1 and R2: HasRules = false, want true")
	}

	e.RemoveRules(t, at.Order)
	e.RemoveRules(t, at.Claim)
	if hasRules(t, e, at.Order) {
		t.Error("no rules at all: HasRules(Order) = true, want false")
	}

	id := e.AddRule(t, approval.RuleInput{DocType: at.Receipt, FromAmount: 0, ApproverType: approval.ApproverRole, Role: "Director"})
	if !hasRules(t, e, at.Receipt) {
		t.Error("one rule on Receipt: HasRules = false, want true")
	}
	if hasRules(t, e, at.Order) {
		t.Error("a rule only on Receipt: HasRules(Order) = true, want false")
	}

	// A disabled rule doesn't stop submit approving at once, so it doesn't count.
	at.MustOK(t, e.A.SetRuleEnabled(context.Background(), e.Anil, id, false))
	if hasRules(t, e, at.Receipt) {
		t.Error("only a disabled rule on Receipt: HasRules = true, want false")
	}
	at.MustOK(t, e.A.SetRuleEnabled(context.Background(), e.Anil, id, true))
	if !hasRules(t, e, at.Receipt) {
		t.Error("the rule on Receipt enabled again: HasRules = false, want true")
	}
}
