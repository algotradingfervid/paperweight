package expense

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// E4 (Advance settlement and invariants) replaces this file.

// settleOnSave checks the claim's advance on save and returns the effective
// less advance for net_payable. E2's validateClaim calls it. The stub adds no
// rule and returns 0.
//
//nolint:unused // E2's validateClaim calls it
func settleOnSave(_ context.Context, _ *db.Tx, _ Deps, _ int64, _ types.Money) (types.Money, []doctype.FieldError, error) {
	return 0, nil, nil
}

// submitAdvanceFields is the claim's submit-only advance field rules. The
// stub adds no rule.
func submitAdvanceFields(_ context.Context, _ *db.Tx, _ Deps, _ int64) ([]doctype.FieldError, error) {
	return nil, nil
}

// checkAdvanceLeft checks the advance still covers less_advance and writes the
// effective amount (submit and approve). The stub adds no rule.
func checkAdvanceLeft(_ context.Context, _ *db.Tx, _ Deps, _ int64) error {
	return nil
}

// applyClaim recomputes the named advance's used_amount on approval. The stub
// changes nothing.
func applyClaim(_ context.Context, _ *db.Tx, _ Deps, _ int64) error {
	return nil
}

// reverseClaim recomputes the named advance's used_amount on cancel. The stub
// changes nothing.
func reverseClaim(_ context.Context, _ *db.Tx, _ Deps, _ int64) error {
	return nil
}
