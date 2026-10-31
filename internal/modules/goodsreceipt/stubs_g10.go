package goodsreceipt

import (
	"context"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// G10's stub (vendor says the rest won't come). G10 deletes this file and
// adds its own.

// restNotComing: nil until G10 calls purchase.SetRestNotComing (on true from
// ApplyEffects, false from ReverseEffects).
func restNotComing(context.Context, *db.Tx, Deps, int64, bool) error { return nil }
