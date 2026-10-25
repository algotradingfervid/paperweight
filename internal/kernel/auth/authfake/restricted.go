package authfake

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
)

// Restricted reports whether p has a Restrict list on dimension, like the
// real service (CD-174).
//
// Stub: CD-174 is not built yet.
func (f *Fake) Restricted(ctx context.Context, tx *db.Tx, p auth.Principal, dimension string) (bool, error) {
	return false, fmt.Errorf("authfake.Restricted: %w", errs.ErrNotImplemented)
}
