package purchase

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// errNotImplementedP6 is what P6's stubs answer until P6 builds them.
var errNotImplementedP6 = errors.New("purchase: not implemented (P6)")

// lineOrdered is what one PO line counts towards its request row (PO spec
// §20.2). Stub until P6.
func lineOrdered(state doctype.State, qty, already, received types.Qty) (ordered, held types.Qty) { //nolint:unused // built by P6
	return 0, 0
}

// Holder is one non-cancelled PO line linking a request row (MOD-PO P6 Do 7).
type Holder struct {
	RequestLineID, POID int64
	PODisplay           string
	State               doctype.State
	Owner               types.UserID
	Qty                 types.Qty
}

// OpenRequest is one submitted request with its rows, as Add from requests
// lists it (MOD-PO P6 Do 7).
type OpenRequest struct {
	ID            int64
	Display       string
	RequesterID   types.UserID
	RequesterName string
	NeededBy      types.Date // zero when blank
	Rows          []OpenRequestRow
}

// OpenRequestRow is one request row with its pending to order and the
// holders whose lineOrdered gives ordered or held above 0.
type OpenRequestRow struct {
	RequestLineID int64
	LineNo        int
	ItemID        int64
	ItemCode      string
	Description   string
	UoMID         int64
	UoMCode       string
	Qty           types.Qty
	Pending       types.Qty
	Holders       []Holder
}

// PendingToOrder is each request row's pending to order (PO spec §20.2).
// Stub until P6.
func PendingToOrder(ctx context.Context, tx *db.Tx, requestLineIDs []int64) (map[int64]types.Qty, error) {
	return nil, errNotImplementedP6
}

// HeldBy is every non-cancelled PO line linking the request, oldest first.
// Stub until P6.
func HeldBy(ctx context.Context, tx *db.Tx, requestID int64) ([]Holder, error) {
	return nil, errNotImplementedP6
}

// OpenRequestLines lists the submitted requests p may read for the site and
// project, oldest needed-by first. Stub until P6.
func (s *Service) OpenRequestLines(ctx context.Context, p auth.Principal, siteID, projectID int64) ([]OpenRequest, error) {
	return nil, errNotImplementedP6
}
