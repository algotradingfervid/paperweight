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

func (*Services) importUoM(context.Context, *db.Tx, auth.Principal, map[string]string) (int64, []doctype.FieldError, error) {
	return 0, nil, errors.New("allmasters: UoM import not implemented")
}

func (*Services) importTaxCode(context.Context, *db.Tx, auth.Principal, map[string]string) (int64, []doctype.FieldError, error) {
	return 0, nil, errors.New("allmasters: Tax Code import not implemented")
}

func (*Services) importItem(context.Context, *db.Tx, auth.Principal, map[string]string) (int64, []doctype.FieldError, error) {
	return 0, nil, errors.New("allmasters: Item import not implemented")
}

func (*Services) importExpenseType(context.Context, *db.Tx, auth.Principal, map[string]string) (int64, []doctype.FieldError, error) {
	return 0, nil, errors.New("allmasters: Expense Type import not implemented")
}
