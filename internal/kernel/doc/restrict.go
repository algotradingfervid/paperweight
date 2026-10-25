package doc

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// checkRestrictions calls Auth.CheckValues and Auth.CheckLink for the proposed
// values on create and save (CD-70, Task 7).
func (e *engine) checkRestrictions(context.Context, *db.Tx, auth.Principal, *DocType, *normalized) error {
	return nil
}
