package expense

import (
	"context"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// E6 (Period lock and warnings) replaces this file.

// checkLock refuses submit, approve and cancel of a document dated in a
// locked period (modkit.CheckNotLocked). The stub adds no rule.
func checkLock(_ context.Context, _ *db.Tx, _ string, _ int64) error {
	return nil
}
