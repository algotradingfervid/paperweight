package item

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/masters/item/dbq"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Conversion is one row of an item's UoM table: 1 UoMID = Factor stock units.
type Conversion struct {
	UoMID  int64
	Factor types.Factor
}

const (
	msgStockFactor   = "The conversion factor for the stock UoM must be 1."
	msgRowPrefix     = "Row %d: "
	msgRowUoMBlank   = "Row %d: UoM is required."
	msgDuplicateUoM  = "UoM %s is entered more than once. Keep one row per UoM."
	msgPurchaseNoRow = "Purchase UoM %s has no conversion for this item. Add a row such as 1 %s = 100 %s."
	msgRowNotFound   = "Row %d: UoM not found. Pick one from the list."
	msgRowDisabled   = msgRowPrefix + msgUoMDisabled
	msgStockReset    = "Stock UoM changed. Re-enter the other conversions."
)

// checkFactors adds the errors of rows with a blank UoM and of their factors
// (MST-080, MST-081, MST-085, uom.CheckFactor; rows are numbered from 1 as
// entered) and returns rows with the stock row added last when it was
// missing (MST-079), so the entered rows keep their numbers.
func checkFactors(es *mstkit.Errors, stockUoMID int64, rows []Conversion) []Conversion {
	for i, c := range rows {
		if c.UoMID == 0 {
			es.AddLine(i+1, "uom_id", fmt.Sprintf(msgRowUoMBlank, i+1))
		}
		if err := uom.CheckFactor(c.Factor); err != nil {
			es.AddLine(i+1, "factor", fmt.Sprintf(msgRowPrefix, i+1)+err.Error())
		} else if c.UoMID == stockUoMID && c.Factor != uom.FactorOne {
			es.AddLine(i+1, "factor", msgStockFactor)
		}
	}
	if hasRow(rows, stockUoMID) {
		return rows
	}
	return append(rows[:len(rows):len(rows)], Conversion{UoMID: stockUoMID, Factor: uom.FactorOne})
}

// checkRows adds an error for a row whose UoM doesn't exist or, when not in
// kept, is disabled (MST-093; the stock UoM's are reported on the stock UoM)
// and for each UoM entered more than once (MST-082). lookup returns a UoM.
func checkRows(es *mstkit.Errors, in Input, kept []Conversion, lookup func(id int64) (uom.UoM, bool, error)) error {
	seen := map[int64]int{}
	for i, c := range in.Conversions {
		seen[c.UoMID]++
		u, ok, err := lookup(c.UoMID)
		if err != nil {
			return err
		}
		switch {
		case !ok && c.UoMID != in.StockUoMID:
			es.AddLine(i+1, "uom_id", fmt.Sprintf(msgRowNotFound, i+1))
		case ok && seen[c.UoMID] == 2:
			es.AddLine(i+1, "uom_id", fmt.Sprintf(msgDuplicateUoM, u.Code))
		case ok && u.Disabled && c.UoMID != in.StockUoMID && !hasRow(kept, c.UoMID):
			es.AddLine(i+1, "uom_id", fmt.Sprintf(msgRowDisabled, i+1, u.Code))
		}
	}
	return nil
}

func hasRow(rows []Conversion, uomID int64) bool {
	for _, c := range rows {
		if c.UoMID == uomID {
			return true
		}
	}
	return false
}

// writeConversions replaces the item's conversion rows with rows.
func writeConversions(ctx context.Context, tx *db.Tx, itemID int64, rows []Conversion) error {
	q := dbq.New(tx.SQL())
	if err := q.DeleteItemUoMs(ctx, itemID); err != nil {
		return err
	}
	for i, c := range rows {
		if err := q.InsertItemUoM(ctx, dbq.InsertItemUoMParams{
			ParentID: itemID, LineNo: int64(i + 1), UomID: c.UoMID, Factor: int64(c.Factor),
		}); err != nil {
			return err
		}
	}
	return nil
}

// Conversions reads the item's conversion table in row order.
func (s *Service) Conversions(ctx context.Context, tx *db.Tx, itemID int64) ([]Conversion, error) {
	rows, err := dbq.New(tx.SQL()).ListItemUoMs(ctx, itemID)
	if err != nil {
		return nil, err
	}
	out := make([]Conversion, len(rows))
	for i, r := range rows {
		out[i] = Conversion{UoMID: r.UomID, Factor: types.Factor(r.Factor)}
	}
	return out, nil
}

// Factor is the item's factor for uomID; false when the item has no row for it.
func (s *Service) Factor(ctx context.Context, tx *db.Tx, itemID, uomID int64) (types.Factor, bool, error) {
	f, err := dbq.New(tx.SQL()).GetItemUoMFactor(ctx, dbq.GetItemUoMFactorParams{ParentID: itemID, UomID: uomID})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return types.Factor(f), true, nil
}
