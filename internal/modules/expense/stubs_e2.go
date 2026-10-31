package expense

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// E2 (Claim and advance Validate) replaces this file.

// validateClaim is the claim's Validate rules (spec §5 save rows, totals,
// snapshots, defaults). The stub adds no rule.
func validateClaim(_ context.Context, _ *db.Tx, _ Deps, _ int64) ([]doctype.FieldError, error) {
	return nil, nil
}

// validateAdvance is the advance's Validate rules. The stub adds no rule.
func validateAdvance(_ context.Context, _ *db.Tx, _ Deps, _ int64) ([]doctype.FieldError, error) {
	return nil, nil
}

// submitClaimFields is the claim's submit-only field rules (company bill GSTIN,
// invoice no., Billed to). The stub adds no rule.
func submitClaimFields(_ context.Context, _ *db.Tx, _ Deps, _ int64) ([]doctype.FieldError, error) {
	return nil, nil
}
