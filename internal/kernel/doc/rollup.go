package doc

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// recomputeRollUps recomputes the roll-up targets of d (Task 12).
//
//nolint:unused // called by the transitions from Task 9 on
func (e *engine) recomputeRollUps(context.Context, *db.Tx, auth.Principal, *DocType, Doc, string, string) error {
	return nil
}
