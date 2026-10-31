package goodsreceipt

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

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

// BillsReport is Bills to enter for p (spec §12): every approved or closed
// GRN p may read (Auth.Scope, AUTH-110) that matches f, oldest receipt
// first, labelled from the GRN's own snapshots; the PO number is blank when
// p can't read that PO (CD-340).
func (s *Service) BillsReport(ctx context.Context, p auth.Principal, f BillsFilter) ([]BillRow, error) {
	var out []BillRow
	err := s.scoped(ctx, p, func(tx *db.Tx, in, po func(int64) bool) error {
		rows, err := dbq.New(tx.SQL()).G9Bills(ctx, dbq.G9BillsParams{
			Doctype: DocType, NoBill: flag(f.NoBill), VendorID: f.VendorID, Month: f.Month,
		})
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !in(r.ID) {
				continue
			}
			received, err := isoDate(r.ReceiptDate)
			if err != nil {
				return err
			}
			invoiced, err := isoDate(r.InvoiceDate)
			if err != nil {
				return err
			}
			out = append(out, BillRow{
				ID: r.ID, Number: r.Number, ReceiptDate: received,
				Vendor: r.VendorName, GSTIN: r.VendorGstin, PONumber: poNumber(po, r.PoID, r.PoNumber),
				ChallanNo: r.ChallanNo, InvoiceNo: r.InvoiceNo, InvoiceDate: invoiced,
				InvoiceTotal:  sql.Null[types.Money]{V: types.Money(r.InvoiceAmount.Int64), Valid: r.InvoiceAmount.Valid},
				AcceptedValue: types.Money(r.AcceptedValue), Photos: int(r.Photos),
			})
		}
		return nil
	})
	return out, err
}

// ItemsReport is Received items for p (spec §12, CD-282 Q11): every line of
// an approved or closed GRN p may read (Auth.Scope, AUTH-110) that matches
// f, newest receipt first, labelled from the GRN's and line's snapshots.
// The PO number is blank when p can't read that PO (CD-340). The line's
// accepted value is accepted × its rate, as Validate sums it.
func (s *Service) ItemsReport(ctx context.Context, p auth.Principal, f ItemsFilter) ([]ItemRow, error) {
	var out []ItemRow
	err := s.scoped(ctx, p, func(tx *db.Tx, in, po func(int64) bool) error {
		rows, err := dbq.New(tx.SQL()).G9Items(ctx, dbq.G9ItemsParams{
			RejectedOnly: flag(f.RejectedOnly), VendorID: f.VendorID, ItemID: f.ItemID, Month: f.Month,
		})
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !in(r.GrnID) {
				continue
			}
			date, err := isoDate(r.ReceiptDate)
			if err != nil {
				return err
			}
			accepted, rate := types.Qty(r.AcceptedQty), types.Rate(r.Rate)
			value, err := gstcalc.LineAmount(accepted, rate)
			if err != nil {
				return err
			}
			out = append(out, ItemRow{
				GRNID: r.GrnID, LineID: r.LineID, Number: r.Number, Date: date,
				Vendor: r.VendorName, PONumber: poNumber(po, r.PoID, r.PoNumber),
				ItemCode: r.ItemCode, ItemName: r.ItemName, HSN: r.HsnSac,
				Received: types.Qty(r.ReceivedQty), Accepted: accepted, Rejected: types.Qty(r.RejectedQty),
				Reason: r.RejectReason, UoM: r.UomCode, Rate: rate, AcceptedValue: value,
			})
		}
		return nil
	})
	return out, err
}

// CanExport is AUTH's export check on the Goods Receipt (CD-145): nil when
// p may export, else AUTH's refusal. The reports' CSVs check it before
// reading any row.
func (s *Service) CanExport(ctx context.Context, p auth.Principal) error {
	return s.d.Auth.Can(ctx, p, DocType, auth.RightExport, nil)
}

// scoped runs fn in one read transaction with in, which reports whether a
// GRN id is in p's read scope, and po, whether a PO id is in p's read scope
// on purchase (CD-340, as POChoices; never a link check, CD-312). Both
// scopes are asked before the transaction opens (Scope has no Tx twin). The
// GRN scope's refusal is returned, never read as every row; the PO scope's
// refusal is no readable PO.
func (s *Service) scoped(ctx context.Context, p auth.Principal, fn func(tx *db.Tx, in, po func(int64) bool) error) error {
	pred, err := s.d.Auth.Scope(ctx, p, DocType, auth.RightRead)
	if err != nil {
		return err
	}
	poPred, err := s.d.Auth.Scope(ctx, p, purchase.DocType, auth.RightRead)
	noPO := errors.Is(err, auth.ErrForbidden)
	if err != nil && !noPO {
		return err
	}
	return s.d.DB.Read(ctx, func(tx *db.Tx) error {
		po := func(int64) bool { return false }
		if !noPO {
			var err error
			if po, err = scopedOrders(ctx, tx, poPred); err != nil {
				return err
			}
		}
		if pred.SQL == "" {
			return fn(tx, func(int64) bool { return true }, po)
		}
		rows, err := tx.SQL().QueryContext(ctx, `SELECT "goods_receipt".id FROM "goods_receipt" WHERE `+pred.SQL, pred.Args...) //nolint:gosec // G202: AUTH's predicate, with its args
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		ids := map[int64]bool{}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids[id] = true
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return fn(tx, func(id int64) bool { return ids[id] }, po)
	})
}

// poNumber is number, the GRN's snapshot of PO id, when readable(id), else
// blank (CD-340, CD-233).
func poNumber(readable func(int64) bool, id int64, number string) string {
	if readable(id) {
		return number
	}
	return ""
}

// isoDate is the Date of ISO text iso, the zero Date for blank.
func isoDate(iso string) (types.Date, error) {
	if iso == "" {
		return types.Date{}, nil
	}
	return types.DateFromISO(iso)
}

// flag is b as a query's 0 or 1.
func flag(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
