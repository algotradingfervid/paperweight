package purchase

import "github.com/vardhan/internaltools/internal/platform/db"

// DepsFor reads the package's one modkit.Wiring[HookDeps] for tx's database,
// as every hook does, so the external tests can see what each world was wired
// with (MOD-PO P1 "Service shape", CD-284).
func DepsFor(tx *db.Tx) (HookDeps, error) { return wiring.Get(tx) }
