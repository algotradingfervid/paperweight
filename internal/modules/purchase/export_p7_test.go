package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DefaultPrice is P7's defaultPrice (lastprice.go), which P2's Validate calls
// for a line saved with a blank price, seen by the external tests: the
// package-internal tests can't build documents, because purchasetest imports
// purchase (MOD-PO P7, PO spec §24.2).
func DefaultPrice(ctx context.Context, tx *db.Tx, vendorID, itemID, uomID, projectID int64) (types.Rate, types.Percent, bool, error) {
	return defaultPrice(ctx, tx, vendorID, itemID, uomID, projectID)
}
