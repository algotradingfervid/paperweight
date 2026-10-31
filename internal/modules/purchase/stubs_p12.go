package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/errs"
)

// P12's stub (copy). P12 deletes this file and adds copy.go.

// Copy creates a draft copy of PO poID owned by p (PO spec §24.1); not
// built until P12.
func (s *Service) Copy(_ context.Context, _ auth.Principal, _ int64) (newID int64, err error) {
	return 0, errs.ErrNotImplemented
}
