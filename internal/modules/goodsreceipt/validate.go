package goodsreceipt

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// validateReceipt is the GRN's Validate (GRN spec §3.1, §3.2, §4 save rows,
// §8). It runs on every save and again on submit and approve, and reads
// inside the tx (CD-94). It first writes every derived value from the
// current inputs (writeCopies, then accepted_value), so a step save or an
// amendment that keeps its writes despite field errors stores nothing stale
// (CD-302). Masters disabled after the PO aren't checked: the copies are
// read straight from the PO and its lines (spec §8).
func validateReceipt(ctx context.Context, tx *db.Tx, d Deps, id int64) ([]doctype.FieldError, error) {
	q := dbq.New(tx.SQL())
	today := types.Today(d.Clock)
	if err := writeCopies(ctx, q, id, today); err != nil {
		return nil, err
	}
	h, err := q.G2Receipt(ctx, id)
	if err != nil {
		return nil, err
	}
	lines, err := q.G2Lines(ctx, id)
	if err != nil {
		return nil, err
	}

	errs := checkDates(h, today.ISO())
	poErrs, foreign := checkPO(h, lines)
	errs = append(errs, poErrs...)
	for _, l := range lines {
		errs = append(errs, checkLine(l, foreign[l.LineNo])...)
	}

	// accepted_value is the sum over the stored lines, cleared above the
	// money maximum (CD-308, CD-302). A line amount past the maximum is an
	// error from LineAmount, so the total is over too.
	var total types.Money
	over := false
	for _, l := range lines {
		amount, err := gstcalc.LineAmount(types.Qty(l.AcceptedQty), types.Rate(l.SnapRate))
		over = over || err != nil
		total += amount
	}
	over = over || total > types.MaxMoney
	if over {
		errs = append(errs, doctype.FieldError{Field: "lines", Message: msgTotalTooLarge})
	}
	if err := q.G2SetAcceptedValue(ctx, dbq.G2SetAcceptedValueParams{
		AcceptedValue: sql.NullInt64{Int64: int64(total), Valid: !over}, ID: id,
	}); err != nil {
		return nil, err
	}
	return errs, nil
}

// checkDates: the receipt date (defaulted to today by writeCopies) is not
// after today, the business date, and the challan date is not after it.
// Dates are ISO text, so they compare as strings.
func checkDates(h dbq.G2ReceiptRow, today string) []doctype.FieldError {
	var errs []doctype.FieldError
	if h.ReceiptDate.String > today {
		errs = append(errs, doctype.FieldError{Field: "receipt_date", Message: msgReceiptFuture})
	}
	if h.ChallanDate.Valid && h.ChallanDate.String > h.ReceiptDate.String {
		errs = append(errs, doctype.FieldError{Field: "challan_date", Message: msgChallanAfter})
	}
	return errs
}

// checkPO checks purchase_order_id against the stored lines. A blank PO is
// refused. Lines that all belong to one other PO mean the PO was changed on
// a draft with lines, detected from the lines, never a stored marker
// (CD-302). Otherwise each line not on the PO is refused with the PO's
// display, and a PO line already on an earlier line is refused on the later
// one, {m} its PO line number (CD-279 (14)). A po_line_id that doesn't
// resolve is not on the PO, so the foreign key is never the message. It
// returns the line numbers refused as not on the PO.
func checkPO(h dbq.G2ReceiptRow, lines []dbq.G2LinesRow) ([]doctype.FieldError, map[int64]bool) {
	if !h.PurchaseOrderID.Valid {
		return []doctype.FieldError{{Field: "purchase_order_id", Message: msgChoosePO}}, nil
	}
	po := h.PurchaseOrderID.Int64
	if changedPO(po, lines) {
		return []doctype.FieldError{{Field: "purchase_order_id", Message: msgDeleteDraft}}, nil
	}
	var errs []doctype.FieldError
	foreign := map[int64]bool{}
	seen := map[int64]bool{}
	for _, l := range lines {
		n := int(l.LineNo)
		if l.PoID.Int64 != po {
			errs = append(errs, doctype.FieldError{Field: "po_line_id", Line: n,
				Message: fmt.Sprintf(msgLineNotOnPO, n, naming.Label(h.PoNumber.String, po))})
			foreign[l.LineNo] = true
			continue
		}
		if seen[l.PoLineID.Int64] {
			errs = append(errs, doctype.FieldError{Field: "po_line_id", Line: n, Message: fmt.Sprintf(msgPOLineTwice, n, l.PoLineNo)})
			continue
		}
		seen[l.PoLineID.Int64] = true
	}
	return errs, foreign
}

// changedPO reports whether there are lines and every one is a line of the
// same PO, other than po.
func changedPO(po int64, lines []dbq.G2LinesRow) bool {
	if len(lines) == 0 {
		return false
	}
	other := lines[0].PoID
	for _, l := range lines {
		if !l.PoID.Valid || l.PoID != other {
			return false
		}
	}
	return other.Int64 != po
}

// checkLine checks one line's quantities: rejected not above received,
// received and rejected against the whole-number units (uom.CheckLineQty,
// whose text is kept and put on the GRN's column), and accepted not above
// twice the PO line's quantity (CD-272 Gap 3; exactly twice passes). A line
// refused as not on the PO (foreign) is not checked against another PO's
// line, and a line whose PO line doesn't resolve has no unit or quantity to
// check against; rejected above received reads only the line itself. The
// texts number the line by its PO line (pn), FieldError.Line keeps the
// posted position (AD-12, UX §5.4 P2).
func checkLine(l dbq.G2LinesRow, foreign bool) []doctype.FieldError {
	n, pn := int(l.LineNo), int(l.PoLineNo)
	var errs []doctype.FieldError
	if l.RejectedQty > l.ReceivedQty {
		errs = append(errs, doctype.FieldError{Field: "rejected_qty", Line: n, Message: fmt.Sprintf(msgRejectedAbove, pn)})
	}
	if foreign || !l.PoID.Valid {
		return errs
	}
	lineUoM := uom.UoM{Code: l.UomCode, WholeNumber: l.LineWhole != 0}
	stockUoM := uom.UoM{Code: l.StockUomCode, WholeNumber: l.StockWhole != 0}
	for _, c := range []struct {
		field string
		qty   int64
	}{{"received_qty", l.ReceivedQty}, {"rejected_qty", l.RejectedQty}} {
		if fe := uom.CheckLineQty(pn, types.Qty(c.qty), lineUoM, stockUoM, types.Factor(l.Factor)); fe != nil {
			errs = append(errs, doctype.FieldError{Field: c.field, Line: n, Message: fe.Message})
		}
	}
	if l.AcceptedQty > 2*l.OrderedQty {
		errs = append(errs, doctype.FieldError{Field: "received_qty", Line: n, Message: fmt.Sprintf(msgTwiceOrdered, pn,
			types.FormatQtyUoM(types.Qty(l.AcceptedQty), l.UomCode), types.FormatQtyUoM(types.Qty(l.OrderedQty), l.UomCode))})
	}
	return errs
}
