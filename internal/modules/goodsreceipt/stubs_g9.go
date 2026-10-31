package goodsreceipt

import (
	"context"
	"database/sql"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/errs"
	"github.com/vardhan/internaltools/internal/types"
)

// G9's stub file (reports). No hook calls G9's functions; grnweb's report
// stubs answer 501 until G9 and S4 build them. G9 deletes this file and adds
// its own.

// BillsFilter narrows Bills to enter (spec §12): NoBill keeps GRNs with no
// bill details yet; a 0 VendorID is every vendor; Month is "YYYY-MM" of the
// receipt date, blank every month.
type BillsFilter struct {
	NoBill   bool
	VendorID int64
	Month    string
}

// BillRow is one approved or closed GRN in Bills to enter: spec §12's
// columns. InvoiceNo, InvoiceDate and InvoiceTotal are the bill details,
// blank, zero and invalid when there are none; Photos is the GRN's
// attachment count.
type BillRow struct {
	ID                      int64
	Number                  string
	ReceiptDate             types.Date
	Vendor, GSTIN, PONumber string
	ChallanNo, InvoiceNo    string
	InvoiceDate             types.Date
	InvoiceTotal            sql.Null[types.Money]
	AcceptedValue           types.Money
	Photos                  int
}

// ItemsFilter narrows Received items (spec §12, CD-282 Q11): a 0 id is
// every vendor or item; Month is "YYYY-MM" of the receipt date, blank every
// month; RejectedOnly keeps lines with rejected above 0.
type ItemsFilter struct {
	VendorID, ItemID int64
	Month            string
	RejectedOnly     bool
}

// ItemRow is one line of an approved or closed GRN in Received items: spec
// §12's columns, the item as its code and name.
type ItemRow struct {
	GRNID, LineID                int64
	Number                       string
	Date                         types.Date
	Vendor, PONumber             string
	ItemCode, ItemName, HSN      string
	Received, Accepted, Rejected types.Qty
	Reason, UoM                  string
	Rate                         types.Rate
	AcceptedValue                types.Money
}

// BillsReport is Bills to enter for p (plan G9): not built yet.
func (s *Service) BillsReport(context.Context, auth.Principal, BillsFilter) ([]BillRow, error) {
	return nil, errs.ErrNotImplemented
}

// ItemsReport is Received items for p (plan G9): not built yet.
func (s *Service) ItemsReport(context.Context, auth.Principal, ItemsFilter) ([]ItemRow, error) {
	return nil, errs.ErrNotImplemented
}
