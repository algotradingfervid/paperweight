package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// P7's stub (last price). P7 deletes this file and adds lastprice.go.

// defaultPrice is the last price for vendor, item, UoM and project; ok false
// (no price) until P7, so P2's Validate leaves the price as it is.
func defaultPrice(context.Context, *db.Tx, int64, int64, int64, int64) (types.Rate, types.Percent, bool, error) { //nolint:unused // called by P2, built by P7
	return 0, 0, false, nil
}
