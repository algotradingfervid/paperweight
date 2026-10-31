package goodsreceipt

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The GRN print (GRN spec §9): filled from the GRN's own columns and
// snapshots, on the PO's branch letterhead, read only through the print
// page's tx. Bill details and attachments are never printed.

func init() { print.Register(DocType, printReceipt) }

// challanLabels are the print's challan number and date labels for each
// stored challan_kind (CD-175, CD-285); a blank kind keeps the kernel's.
var challanLabels = map[string][2]string{
	"Delivery challan": {"Delivery challan no.", "Challan date"},
	"Tax invoice":      {"Tax invoice no.", "Invoice date"},
}

// printReceipt is the goods receipt's print view. A blank challan no. (a
// service GRN) prints the kernel's dash.
func printReceipt(ctx context.Context, tx *db.Tx, id int64) (templ.Component, error) {
	q := dbq.New(tx.SQL())
	h, err := q.G8ReceiptPrint(ctx, id)
	if err != nil {
		return nil, err
	}
	labels := challanLabels[h.ChallanKind]
	d := print.GRNDoc{
		Party: print.Party{
			Label: "Vendor", Name: h.VendorName, GSTIN: h.VendorGstin,
			Address: addressLines(h.VendorAddress, h.VendorGstin),
		},
		PONumber: h.PoNumber, ChallanNo: h.ChallanNo,
		ChallanLabel: labels[0], ChallanDateLabel: labels[1],
		VehicleNo: h.VehicleNo, Transporter: h.Transporter, EWayBillNo: h.EwayBillNo, Remarks: h.Remarks,
	}
	for _, f := range []struct {
		to *types.Date
		v  sql.NullString
	}{{&d.Date, h.ReceiptDate}, {&d.PODate, h.SnapPoDate}, {&d.ChallanDate, h.ChallanDate}} {
		if f.v.String == "" {
			continue // a zero date: the challan date line is left out
		}
		if *f.to, err = types.DateFromISO(f.v.String); err != nil {
			return nil, err
		}
	}
	lines, err := q.G8ReceiptPrintLines(ctx, id)
	if err != nil {
		return nil, err
	}
	for i, l := range lines {
		d.Lines = append(d.Lines, print.GRNLine{
			Sr: i + 1, Item: strings.TrimSpace(l.ItemCode + " " + l.ItemName), HSN: l.HsnSac, UoM: l.UomCode,
			Received: types.Qty(l.ReceivedQty), Accepted: types.Qty(l.AcceptedQty), Rejected: types.Qty(l.RejectedQty),
			RejectReason: l.RejectReason,
		})
	}
	if d.ReverseChargeNote, err = reverseChargeNote(ctx, tx, h.PurchaseOrderID); err != nil {
		return nil, err
	}
	if d.Letterhead, err = print.LetterheadFor(ctx, tx, h.BranchID); err != nil {
		return nil, err
	}
	return print.GRNDocument(d), nil
}

// reverseChargeNote is the PO print's reverse-charge note for po (GRN spec
// §7), read in the print's tx. It is blank with no PO or when ctx's
// principal can't read the PO (CD-233); with no principal, the zero one
// reads nothing.
func reverseChargeNote(ctx context.Context, tx *db.Tx, po sql.NullInt64) (string, error) {
	if !po.Valid {
		return "", nil
	}
	p, _ := auth.PrincipalFrom(ctx)
	d, err := wiring.Get(tx)
	if err != nil {
		return "", err
	}
	var link *auth.LinkError
	err = d.Auth.CheckLinkTx(ctx, tx, p, purchase.DocType, po.Int64)
	if errors.As(err, &link) {
		return "", nil // the viewer can't read the PO
	}
	if err != nil {
		return "", err
	}
	return purchase.ReverseChargeNote(ctx, tx, po.Int64)
}

// addressLines are the vendor address snapshot's lines without its last
// line when that is the vendor's GSTIN line (address.DisplayText ends with
// it), which the party block prints on its own.
func addressLines(text, gstin string) []string {
	if text == "" {
		return nil // no address snapshot, so no address lines
	}
	out := strings.Split(text, "\n")
	if n := len(out); out[n-1] == "GSTIN: "+gstin {
		out = out[:n-1]
	}
	return out
}
