package purchase

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Last price (PO spec §24.2, MOD-PO P7).

// ItemUoM is a LastPricesFor key: an item in a unit.
type ItemUoM struct {
	ItemID, UoMID int64
}

// LastPrice is the newest price of an item and UoM, on the PO that paid it.
type LastPrice struct {
	POID                  int64
	PODisplay, VendorName string
	Rate                  types.Rate
	OrderDate             types.Date
}

// defaultPrice is the price and discount of the newest line (latest
// order_date, then highest id) on an approved or closed PO with the same
// vendor, item, UoM and project; ok false when there is none. Same project
// only, so the default never shows a price from a project the user can't
// read: Validate has no principal.
func defaultPrice(ctx context.Context, tx *db.Tx, vendorID, itemID, uomID, projectID int64) (types.Rate, types.Percent, bool, error) {
	row, err := dbq.New(tx.SQL()).LastPriceFor(ctx, dbq.LastPriceForParams{
		VendorID: nullID(vendorID), ItemID: nullID(itemID), UomID: nullID(uomID), ProjectID: nullID(projectID),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	return types.Rate(row.Price), types.Percent(row.DiscountPct), true, nil
}

// LastPricesFor is the newest price of each key across vendors, on approved
// and closed POs p may read (AUTH-110); a key with none is absent. Scope is
// asked before the read opens: it has no Tx twin.
func (s *Service) LastPricesFor(ctx context.Context, p auth.Principal, keys []ItemUoM) (map[ItemUoM]LastPrice, error) {
	pred, err := s.d.Auth.Scope(ctx, p, DocType, auth.RightRead)
	if err != nil {
		return nil, err
	}
	out := map[ItemUoM]LastPrice{}
	err = s.d.DB.Read(ctx, func(tx *db.Tx) error {
		readable, err := ReadableOrders(ctx, tx, pred)
		if err != nil {
			return err
		}
		q := dbq.New(tx.SQL())
		for _, k := range keys {
			rows, err := q.LastPricesForItem(ctx, dbq.LastPricesForItemParams{ItemID: nullID(k.ItemID), UomID: nullID(k.UoMID)})
			if err != nil {
				return err
			}
			for _, r := range rows {
				if !readable(r.PoID) {
					continue
				}
				on, err := types.DateFromISO(r.OrderDate)
				if err != nil {
					return err
				}
				out[k] = LastPrice{POID: r.PoID, PODisplay: r.Number, VendorName: r.SnapVendorName, Rate: types.Rate(r.Price), OrderDate: on}
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ReadableOrders reports whether a PO id is in pred, the viewer's read
// scope. pred comes from Auth.Scope(ctx, p, DocType, auth.RightRead), called
// before the tx (Scope has no Tx twin); an empty pred.SQL means every PO is
// readable. On a DeniedError from Scope the caller fails closed (treats
// nothing as readable) and must not pass an empty Predicate then. The ids
// are read inside the caller's tx.
func ReadableOrders(ctx context.Context, tx *db.Tx, pred auth.Predicate) (func(poID int64) bool, error) {
	return scopeIn(ctx, tx, "purchase_order", pred)
}
