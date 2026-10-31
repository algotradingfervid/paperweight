package purchase

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The PO print (PO spec §9): filled from the stored snapshots, on the
// delivery site's branch letterhead, read only through the print page's tx.
// Remarks, comments, the sent record, Expected on and the rest rows are
// never printed.

func init() { print.Register(DocType, printOrder) }

// printOrder is the purchase order's print view.
func printOrder(ctx context.Context, tx *db.Tx, id int64) (templ.Component, error) {
	q := dbq.New(tx.SQL())
	h, err := q.OrderPrint(ctx, id)
	if err != nil {
		return nil, err
	}
	if !h.BranchID.Valid {
		return nil, &doc.Error{Kind: doc.KindInvalid, Message: msgPrintNoSite}
	}
	d := print.TradeDoc{
		Title: "Purchase Order",
		Party: print.Party{
			Label: "Vendor", Name: h.SnapVendorName, GSTIN: h.SnapVendorGstin,
			Address: addressLines(h.SnapVendorAddress, h.SnapVendorGstin),
		},
		Taxable: types.Money(h.NetTotal), GrandTotal: types.Money(h.GrandTotal),
		RoundedTotal: types.Money(h.RoundedTotal), HasRoundedTotal: true,
		Terms: h.Terms, AmendsNumber: h.AmendsNumber.String,
	}
	if h.OrderDate.Valid {
		if d.Date, err = types.DateFromISO(h.OrderDate.String); err != nil {
			return nil, err
		}
	}
	d.DeliverTo = []string{h.SnapSiteName}
	if d.Refs, err = orderRefs(h); err != nil {
		return nil, err
	}
	if h.SnapVendorName != "" && h.SnapPlaceState != "" {
		d.PlaceReason = gstcalc.PlaceReason(h.SnapVendorCategory, h.SnapVendorName, h.SnapVendorState, h.SnapPlaceState)
	}
	if h.ReverseCharge == 1 {
		tax := types.Money(h.CgstTotal + h.SgstTotal + h.IgstTotal)
		d.ReverseChargeNote = "Reverse charge applies. GST of " + types.FormatINR(tax) + " is payable by us, not to the vendor."
	}
	lines, err := q.OrderPrintLines(ctx, id)
	if err != nil {
		return nil, err
	}
	for i, l := range lines {
		desc := l.Description
		if desc == l.SnapItemName {
			desc = "" // the item column already names it
		}
		d.Lines = append(d.Lines, print.TradeLine{
			Sr: i + 1, Item: strings.TrimSpace(l.SnapItemCode + " " + l.SnapItemName), Description: desc,
			HSN: l.HsnSac, Qty: types.Qty(l.Qty.Int64), UoM: l.SnapUomCode,
			Rate: types.Rate(l.Rate.Int64), Amount: types.Money(l.Amount.Int64),
		})
	}
	d.Taxes = taxRows(lines, h.ReverseCharge == 1)
	if d.ApprovedBy, d.ApprovedAt, err = orderApprovedBy(ctx, q, id); err != nil {
		return nil, err
	}
	if d.Letterhead, err = print.LetterheadFor(ctx, tx, h.BranchID.Int64); err != nil {
		return nil, err
	}
	return print.TradeDocument(d), nil
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

// orderRefs are the refs in spec §9's order, each left out when blank.
func orderRefs(h dbq.OrderPrintRow) ([]print.DocRef, error) {
	var by string
	if h.RequiredBy.Valid && h.RequiredBy.String != "" {
		d, err := types.DateFromISO(h.RequiredBy.String)
		if err != nil {
			return nil, err
		}
		by = types.FormatDate(d)
	}
	var refs []print.DocRef
	for _, r := range []print.DocRef{
		{Label: "Required by", Value: by},
		{Label: "Payment terms", Value: h.SnapTermsName},
		{Label: "Vendor quote ref", Value: h.VendorQuoteRef},
		{Label: "Earlier PO no.", Value: h.EarlierPoNo},
	} {
		if r.Value != "" {
			refs = append(refs, r)
		}
	}
	return refs, nil
}

// taxRows are the CGST and SGST rows, or the IGST row, for each GST rate on
// the lines, in the order the rates first appear, from the line snapshots. A
// rate with no tax prints no row. On a reverse-charge PO each name carries
// msgPrintReverseCharge (CD-316 (4)).
func taxRows(lines []dbq.OrderPrintLinesRow, reverseCharge bool) []print.TaxRow {
	type sums struct{ cgst, sgst, igst types.Money }
	byRate := map[types.Percent]*sums{}
	var rates []types.Percent
	for _, l := range lines {
		r := types.Percent(l.SnapGstRate)
		s, ok := byRate[r]
		if !ok {
			s = &sums{}
			byRate[r] = s
			rates = append(rates, r)
		}
		s.cgst += types.Money(l.Cgst.Int64)
		s.sgst += types.Money(l.Sgst.Int64)
		s.igst += types.Money(l.Igst.Int64)
	}
	var suffix string
	if reverseCharge {
		suffix = msgPrintReverseCharge
	}
	var rows []print.TaxRow
	for _, r := range rates {
		s := byRate[r]
		if s.cgst != 0 || s.sgst != 0 {
			rows = append(rows, print.TaxRow{Name: "CGST" + suffix, Rate: r / 2, Amount: s.cgst}, print.TaxRow{Name: "SGST" + suffix, Rate: r / 2, Amount: s.sgst})
		}
		if s.igst != 0 {
			rows = append(rows, print.TaxRow{Name: "IGST" + suffix, Rate: r, Amount: s.igst})
		}
	}
	return rows
}

// orderApprovedBy is the name and time of the PO's latest approval, the
// submitter's when no rule applied (PO-046); blank when never approved.
func orderApprovedBy(ctx context.Context, q *dbq.Queries, id int64) (string, time.Time, error) {
	r, err := q.OrderApprovedBy(ctx, dbq.OrderApprovedByParams{Doctype: DocType, DocID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, nil
	}
	if err != nil {
		return "", time.Time{}, err
	}
	at, err := types.ParseTimestamp(r.At)
	if err != nil {
		return "", time.Time{}, err
	}
	return r.UserName, at.Time(), nil
}
