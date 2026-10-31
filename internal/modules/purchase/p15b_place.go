package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// PlaceReason is PO poID's place-of-supply text (spec §7.1), as the print
// shows it, "" while its vendor or delivery site snapshot is blank;
// sql.ErrNoRows for an unknown id. It reads in the caller's tx with no right
// check, for the document page (P15b).
func PlaceReason(ctx context.Context, tx *db.Tx, poID int64) (string, error) {
	h, err := dbq.New(tx.SQL()).OrderPrint(ctx, poID)
	if err != nil {
		return "", err
	}
	return placeReasonOf(h), nil
}

// placeReasonOf is print row h's place-of-supply text, "" while its vendor
// or delivery site snapshot is blank: the page and the print share it.
func placeReasonOf(h dbq.OrderPrintRow) string {
	if h.SnapVendorName == "" || h.SnapPlaceState == "" {
		return ""
	}
	return gstcalc.PlaceReason(h.SnapVendorCategory, h.SnapVendorName, h.SnapVendorState, h.SnapPlaceState)
}
