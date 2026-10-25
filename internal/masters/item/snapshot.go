package item

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Snapshot is what a document line copies from its item in UoM uomID.
type Snapshot struct {
	Code, Name, HSNSAC, UoMCode, StockUoMCode string
	Factor                                    types.Factor
	LineWhole, StockWhole                     bool
	TaxCodeID                                 int64
	Rate                                      types.Percent
}

// Snapshot reads the item, its line and stock UoMs, the factor for uomID and
// its default tax code's rate (0 when it has none). An item without a
// conversion for uomID is an error.
func (s *Service) Snapshot(ctx context.Context, tx *db.Tx, itemID, uomID int64) (Snapshot, error) {
	it, err := s.Get(ctx, tx, itemID)
	if err != nil {
		return Snapshot{}, err
	}
	f, ok, err := s.Factor(ctx, tx, itemID, uomID)
	if err != nil {
		return Snapshot{}, err
	}
	if !ok {
		return Snapshot{}, fmt.Errorf("item %s has no conversion for UoM %d", it.Code, uomID)
	}
	uoms := uom.New(s.env)
	line, err := uoms.Get(ctx, tx, uomID)
	if err != nil {
		return Snapshot{}, err
	}
	stock, err := uoms.Get(ctx, tx, it.StockUoMID)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{
		Code: it.Code, Name: it.Name, HSNSAC: it.HSNSAC, UoMCode: line.Code, StockUoMCode: stock.Code,
		Factor: f, LineWhole: line.WholeNumber, StockWhole: stock.WholeNumber, TaxCodeID: it.DefaultTaxCodeID,
	}
	if it.DefaultTaxCodeID != 0 {
		tc, err := taxcode.New(s.env).Snapshot(ctx, tx, it.DefaultTaxCodeID)
		if err != nil {
			return Snapshot{}, err
		}
		snap.Rate = tc.Rate
	}
	return snap, nil
}
