package goodsreceipt

import (
	"context"
	"database/sql"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
	"github.com/vardhan/internaltools/internal/types"
)

// G7's stubs (bill details). G7 deletes only its own functions from this
// file and adds its own files.

// copyBillOnAmend: nil until G7 moves the bill details to an approved
// amendment (CD-272 Gap 1).
func copyBillOnAmend(context.Context, *db.Tx, Deps, int64) error { return nil }

// BillInput is what the Bill details form posts, as typed. SaveBill parses
// each value after its load, right, version and state checks (CD-259), so
// a bad date never skips a 403 or a 409.
type BillInput struct {
	InvoiceNo, InvoiceDate, InvoiceAmount string
}

// BillView is a GRN's bill details (GRN spec §3.3): the stored row, or a
// zero view at version 0 when there is none. For an approved GRN with a tax
// invoice at the gate and no row, InvoiceNo and InvoiceDate are the
// challan's and Note says they were copied (CD-272 Gap 5); nothing is
// stored. UpdatedBy and UpdatedAt are the row's, for the card's "Last
// changed by" line.
type BillView struct {
	InvoiceNo     string
	InvoiceDate   types.Date
	InvoiceAmount sql.Null[types.Money]
	Version       int
	UpdatedBy     types.UserID
	UpdatedAt     time.Time
	Note          string
}

// Bill: not implemented until G7.
func Bill(context.Context, *db.Tx, int64) (BillView, error) {
	return BillView{}, errs.ErrNotImplemented
}

// SaveBill: not implemented until G7.
func (s *Service) SaveBill(context.Context, auth.Principal, int64, int, BillInput) ([]string, error) {
	return nil, errs.ErrNotImplemented
}
