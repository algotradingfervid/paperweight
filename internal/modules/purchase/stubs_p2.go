package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// P2's stubs (Order Validate). P2 deletes this file and adds its own.

// validateOrder: nil is "no extra rule" until P2.
func validateOrder(context.Context, *db.Tx, int64) []doctype.FieldError { return nil }

// beforeSubmitOrder: nil is "no extra rule" until P2.
func beforeSubmitOrder(context.Context, *db.Tx, int64) error { return nil }

// amountOrder is 0 until P2, below every approval rule.
func amountOrder(context.Context, *db.Tx, int64) (types.Money, error) { return 0, nil }
