package goodsreceipt

import (
	"context"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// G3's stubs (effects and receipt status). G3 deletes this file and adds its
// own. The PO's received_qty is the kernel roll-up's either way.

// applyReceipt: nil until G3 refreshes the PO's receipt status on approve.
func applyReceipt(context.Context, *db.Tx, Deps, int64) error { return nil }

// reverseReceipt: nil until G3 refreshes the PO's receipt status on cancel.
func reverseReceipt(context.Context, *db.Tx, Deps, int64) error { return nil }
