package goodsreceipt

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/errs"
)

// G6's stub file (PO picker, draft from PO). No hook or route calls G6's
// functions; S1's step handler does, once G6 builds them. G6 deletes this
// file and adds its own (names and fields as AD-12 ruled for G6).

// POChoice is one row of step 1's PO picker (UX doc §5.2).
type POChoice struct {
	ID         int64  // purchase_order.id
	Display    string // naming.Label of the PO, e.g. PO/26-27/00001
	VendorName string // the PO's snap_vendor_name
	SiteName   string
	Pending    int // lines with pending above 0
	Lines      int // all lines ("{p} of {n} lines pending")
}

// POChoices: not implemented until G6.
func (s *Service) POChoices(context.Context, auth.Principal, bool, string, int) ([]POChoice, error) {
	return nil, errs.ErrNotImplemented
}

// DraftFromPO: not implemented until G6.
func (s *Service) DraftFromPO(context.Context, auth.Principal, int64) (map[string]string, []map[string]string, error) {
	return nil, nil, errs.ErrNotImplemented
}
