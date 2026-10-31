package expense

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// E9 (Mark as paid) replaces this file.

// errPaidNotBuilt is what Mark as paid answers until E9 builds it: never
// success where the real code may refuse.
var errPaidNotBuilt = errors.New("expense: Mark as paid isn't built yet (MOD-EXP E9)")

// checkNotPaid refuses cancelling an advance marked as paid (ReverseEffects).
// The stub adds no rule.
func checkNotPaid(_ context.Context, _ *db.Tx, _ int64) error {
	return nil
}

// MarkPaid sets the advance's paid_on (spec §4, CD-271 G1).
func (s *Service) MarkPaid(_ context.Context, _ auth.Principal, _ int64, _ int, _ types.Date) error {
	return errPaidNotBuilt
}

// UndoPaid clears the advance's paid_on (spec §4, CD-271 G1).
func (s *Service) UndoPaid(_ context.Context, _ auth.Principal, _ int64, _ int) error {
	return errPaidNotBuilt
}
