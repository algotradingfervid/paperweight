package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// CheckSiteProject is P2's site-and-project rule, which P5's request Validate
// reuses (MOD-PO P2 Do), seen by the external tests.
func CheckSiteProject(ctx context.Context, tx *db.Tx, siteID, projectID int64) (int64, *doc.FieldError, error) {
	return checkSiteProject(ctx, tx, siteID, projectID)
}

// AmountOrder is the Order Amount hook's function (P2, validate_order.go).
func AmountOrder(ctx context.Context, tx *db.Tx, id int64) (types.Money, error) {
	return amountOrder(ctx, tx, id)
}
