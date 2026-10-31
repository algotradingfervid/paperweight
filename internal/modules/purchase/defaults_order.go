package purchase

import (
	"context"
	"database/sql"

	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// The defaults Validate applies on save when a field is blank (PO spec §8
// "Defaults"). A value cleared on purpose comes back on the next save.

// vendorAddress is the address the PO keeps for vendorID: the vendor's
// primary billing address when the vendor changed since the last save
// (snap_vendor_id differs, CD-280 D7 (d)) or none is chosen; otherwise the
// chosen one, whose owner PartySnapshot checks (CD-176). 0 when the vendor
// has no primary billing address.
func vendorAddress(ctx context.Context, tx *db.Tx, h dbq.OrderForValidateRow) (int64, error) {
	if h.VendorAddressID != 0 && (h.SnapVendorID == 0 || h.SnapVendorID == h.VendorID) {
		return h.VendorAddressID, nil
	}
	all, err := addresses.ForParty(ctx, tx, h.VendorID, false)
	if err != nil {
		return 0, err
	}
	for _, a := range all {
		if a.PrimaryBilling {
			return a.ID, nil
		}
	}
	return 0, nil
}

// paymentTerms is the PO's payment terms: the chosen ones, else the vendor's
// (MST-050; 0 when it has none or they are disabled). When the vendor
// changed since the last save (snap_vendor_id differs), terms equal to the
// old vendor's default (as set, even if now disabled) were not chosen and
// take the new vendor's (CD-309 (4)).
func paymentTerms(ctx context.Context, tx *db.Tx, h dbq.OrderForValidateRow) (int64, error) {
	if h.PaymentTermsID != 0 && h.SnapVendorID != 0 && h.SnapVendorID != h.VendorID {
		old, err := parties.Get(ctx, tx, h.SnapVendorID)
		if err != nil {
			return 0, err
		}
		if old.DefaultPaymentTermsID == h.PaymentTermsID {
			h.PaymentTermsID = 0
		}
	}
	if h.PaymentTermsID != 0 {
		return h.PaymentTermsID, nil
	}
	d, err := parties.Defaults(ctx, tx, h.VendorID)
	return d.PaymentTermsID, err
}

// lineDefaults fills a line's blank values from its item (PO spec §3.2): the
// description, HSN/SAC, the stock UoM and the default tax code; a blank
// price takes the last price for this vendor, item, UoM and project (P7's
// defaultPrice, with its discount when the discount is blank), else the
// item's standard rate; a blank discount is 0.
func lineDefaults(ctx context.Context, tx *db.Tx, h dbq.OrderForValidateRow, projectID int64, it item.Item, l *dbq.OrderLinesForValidateRow) error {
	if l.Description == "" {
		l.Description = it.Name
	}
	if l.HsnSac == "" {
		l.HsnSac = it.HSNSAC
	}
	if l.UomID == 0 {
		l.UomID = it.StockUoMID
	}
	if l.TaxCodeID == 0 {
		l.TaxCodeID = it.DefaultTaxCodeID
	}
	if !l.Price.Valid {
		price, discount, ok, err := defaultPrice(ctx, tx, h.VendorID, it.ID, l.UomID, projectID)
		if err != nil {
			return err
		}
		switch {
		case ok:
			l.Price = valid(int64(price))
			if !l.DiscountPct.Valid {
				l.DiscountPct = valid(int64(discount))
			}
		case it.StandardRate != 0:
			l.Price = valid(int64(it.StandardRate))
		}
	}
	if !l.DiscountPct.Valid {
		l.DiscountPct = valid(0)
	}
	return nil
}

func valid(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }

// nullID is a link column's value: 0 is NULL.
func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }
