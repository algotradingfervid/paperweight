package goodsreceipt

import (
	"context"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// G5: warnings and notes (GRN spec §4, §7; CD-234, CD-238, CD-272 Gaps 4
// and 13). They refuse nothing. Pending is purchase.Pending over the
// accepted quantity of other approved or closed GRNs, so a GRN's own
// accepted never counts against itself, as a draft or approved. Accepted is
// compared, so goods rejected at the gate never warn (CD-238). Line texts
// number the PO line (AD-12, UX §5.4 P2).

// Warnings are GRN id's over-delivery warnings, in line order, then the
// receipt-before-order warning. They name only the GRN's own PO snapshot.
func Warnings(ctx context.Context, tx *db.Tx, id int64) ([]string, error) {
	q := dbq.New(tx.SQL())
	h, err := q.G5Header(ctx, id)
	if err != nil {
		return nil, err
	}
	lines, err := q.G5Lines(ctx, id)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range lines {
		pending := purchase.Pending(types.Qty(l.Qty), types.Qty(l.AlreadyReceived), types.Qty(l.OtherAccepted))
		if accepted := types.Qty(l.AcceptedQty); accepted > pending {
			out = append(out, fmt.Sprintf(msgOverPending, l.PoLineNo,
				types.FormatQtyUoM(accepted, l.UomCode), types.FormatQtyUoM(pending, l.UomCode), h.SnapPoNumber))
		}
	}
	if h.ReceiptDate.String < h.SnapPoDate {
		date, err := types.DateFromISO(h.SnapPoDate)
		if err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf(msgBeforeOrder, types.FormatDate(date)))
	}
	return out, nil
}

// Notes are GRN id's information lines for the check and document pages,
// in this order: every pending PO line received exactly at its pending and
// nothing else (Gap 4), an unregistered vendor above ₹50,000.00 accepted,
// and every line's accepted 0 with an e-way bill no. (Gap 13). A GRN with
// no lines gets neither line note (AD ruling).
func Notes(ctx context.Context, tx *db.Tx, id int64) ([]string, error) {
	q := dbq.New(tx.SQL())
	h, err := q.G5Header(ctx, id)
	if err != nil {
		return nil, err
	}
	lines, err := q.G5Lines(ctx, id)
	if err != nil {
		return nil, err
	}
	poLines, err := q.G5POLines(ctx, dbq.G5POLinesParams{GrnID: id, PoID: h.PurchaseOrderID.Int64})
	if err != nil {
		return nil, err
	}
	accepted := make(map[int64]int64, len(lines))
	allZero := true
	for _, l := range lines {
		accepted[l.PoLineID.Int64] = l.AcceptedQty
		allZero = allZero && l.AcceptedQty == 0
	}
	asOrdered, pendingLines := true, 0
	for _, pl := range poLines {
		pending := purchase.Pending(types.Qty(pl.Qty), types.Qty(pl.AlreadyReceived), types.Qty(pl.OtherAccepted))
		if pending > 0 {
			pendingLines++
			asOrdered = asOrdered && types.Qty(accepted[pl.ID]) == pending
		}
	}
	var out []string
	if asOrdered && len(lines) > 0 && len(lines) == pendingLines {
		out = append(out, noteAsOrdered)
	}
	if h.SnapVendorGstin == "" && h.AcceptedValue.Int64 > unregEWayAbove {
		out = append(out, noteUnreg)
	}
	if allZero && len(lines) > 0 && h.EwayBillNo != "" {
		out = append(out, noteRejectEWay)
	}
	return out, nil
}

// LineWarnings are the over-delivery warnings for typed accepted quantities
// on PO poID's lines, keyed by PO line id, with GRN grnID's own accepted not
// counted (S2). A line with nothing typed is accepted 0, which never warns.
func LineWarnings(ctx context.Context, tx *db.Tx, poID, grnID int64, accepted map[int64]types.Qty) (map[int64]string, error) {
	poLines, err := dbq.New(tx.SQL()).G5POLines(ctx, dbq.G5POLinesParams{GrnID: grnID, PoID: poID})
	if err != nil {
		return nil, err
	}
	out := map[int64]string{}
	for _, pl := range poLines {
		pending := purchase.Pending(types.Qty(pl.Qty), types.Qty(pl.AlreadyReceived), types.Qty(pl.OtherAccepted))
		if a := accepted[pl.ID]; a > pending {
			out[pl.ID] = fmt.Sprintf(msgOverPending, pl.LineNo, types.FormatQtyUoM(a, pl.SnapUomCode),
				types.FormatQtyUoM(pending, pl.SnapUomCode), naming.Label(pl.PoNumber.String, poID))
		}
	}
	return out, nil
}

// DuplicateChallan is GRN id's duplicate challan warning, or "" (CD-337
// (6)): the other GRNs of its vendor, not cancelled, with its challan no.
// (trimmed, any case), named in id order when p can read each of them, else
// the "another receipt" sentence alone (CD-233, CD-324). It refuses nothing:
// the caller must already have checked p's read on GRN id, because the text
// shows that GRN's own challan no.
func DuplicateChallan(ctx context.Context, tx *db.Tx, p auth.Principal, id int64) (string, error) {
	d, err := wiring.Get(tx)
	if err != nil {
		return "", err
	}
	rows, err := dbq.New(tx.SQL()).G11Duplicates(ctx, id)
	if err != nil || len(rows) == 0 {
		return "", err
	}
	no := rows[0].ChallanNo
	var names []string
	for _, r := range rows {
		err := d.Auth.CanTx(ctx, tx, p, DocType, auth.RightRead, grnRef(r.ID, r.CreatedBy, r.ProjectID, r.SiteID))
		if errors.Is(err, auth.ErrNotFound) || errors.Is(err, auth.ErrForbidden) {
			return fmt.Sprintf(msgChallanDupHidden, no), nil
		}
		if err != nil {
			return "", err
		}
		names = append(names, naming.Label(r.Number, r.ID))
	}
	return fmt.Sprintf(msgChallanDup, no, andList(names)), nil
}
