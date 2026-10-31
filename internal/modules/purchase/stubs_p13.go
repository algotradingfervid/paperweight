package purchase

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// P13's stub (create PO from a request, PO spec §20.4 item 1). P13 deletes
// this file and adds fromrequest.go.

// CreateFromRequest creates p's PO draft for vendorID from the pending rows of
// request requestID, posted at version, and returns the new PO's id.
func (*Service) CreateFromRequest(_ context.Context, _ auth.Principal, requestID int64, version int, vendorID int64) (poID int64, err error) {
	_, _, _ = requestID, version, vendorID
	return 0, errors.New("purchase: CreateFromRequest is not implemented yet (P13)")
}
