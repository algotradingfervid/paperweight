package allmasters

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// catalogueImporters is the Catalogue group's RowFuncs (Task 21).
func catalogueImporters() map[string]rowFunc {
	return map[string]rowFunc{
		uom.DocType:         (*Services).importUoM,
		taxcode.DocType:     (*Services).importTaxCode,
		item.DocType:        (*Services).importItem,
		expensetype.DocType: (*Services).importExpenseType,
	}
}

func (s *Services) importUoM(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	saved, err := s.UoM.CreateTx(ctx, tx, p, uom.Input{
		Code: row["code"], Name: row["name"], UQC: row["uqc"], WholeNumber: cellBool(row["whole_number"]),
	})
	return rowResult(ctx, tx, p, row, saved, err, s.UoM.SetDisabledTx)
}

func (s *Services) importTaxCode(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	rate, err := cellValue(row["rate"], types.ParsePercent)
	if err != nil {
		return 0, nil, err
	}
	saved, err := s.TaxCode.CreateTx(ctx, tx, p, taxcode.Input{
		Code: row["code"], Name: row["name"], Rate: rate,
		Treatment: taxcode.Treatment(cellSelect(taxcode.DocType, "treatment", row["treatment"])),
	})
	return rowResult(ctx, tx, p, row, saved, err, s.TaxCode.SetDisabledTx)
}

func (s *Services) importItem(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	uomID, err1 := cellInt64(row["stock_uom_id"])
	taxCodeID, err2 := cellInt64(row["default_tax_code_id"])
	rate, err3 := cellValue(row["standard_rate"], types.ParseRate)
	if err := errors.Join(err1, err2, err3); err != nil {
		return 0, nil, err
	}
	itemType := item.ItemType(cellSelect(item.DocType, "item_type", row["item_type"]))
	saved, err := s.Item.CreateTx(ctx, tx, p, item.Input{
		Code: row["code"], Name: row["name"], Type: itemType, HSNSAC: row["hsn_sac"],
		StockUoMID: uomID, DefaultTaxCodeID: taxCodeID, StandardRate: rate,
		IsStock: itemType == item.TypeGoods, // the template has no stock column
	})
	return rowResult(ctx, tx, p, row, saved, err, s.Item.SetDisabledTx)
}

func (s *Services) importExpenseType(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	taxCodeID, err := cellInt64(row["default_tax_code_id"])
	if err != nil {
		return 0, nil, err
	}
	saved, err := s.ExpenseType.CreateTx(ctx, tx, p, expensetype.Input{
		Code: row["code"], Name: row["name"], DefaultHSNSAC: row["default_hsn_sac"], DefaultTaxCodeID: taxCodeID,
	})
	return rowResult(ctx, tx, p, row, saved, err, s.ExpenseType.SetDisabledTx)
}
