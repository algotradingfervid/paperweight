package purchase

import (
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// HookDeps are what the hooks and the frozen tx calls need beyond their tx.
type HookDeps struct {
	Clock    clock.Clock
	Comments *modkit.Comments
}

// wiring holds each database's HookDeps (CD-284). Every hook reads it first
// with Get(tx); nothing wired for the tx's database is a plain internal error.
var wiring modkit.Wiring[HookDeps]

// Wire sets the hook dependencies for d, replacing any earlier value.
// internal/app wires its database in app.New, and each purchasetest.Env its own.
func Wire(d *db.DB, v HookDeps) { wiring.Wire(d, v) }
