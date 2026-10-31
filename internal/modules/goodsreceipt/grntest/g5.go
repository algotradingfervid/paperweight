package grntest

import (
	"context"
	"database/sql"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/types"
)

// Unlock clears the period lock through modkit's settings, as the
// Administrator (PO spec §14.2: unlocking clears it; GRN-023).
func (e *Env) Unlock(t testing.TB) {
	t.Helper()
	ctx := context.Background()
	kit := modkit.New(modkit.Deps{DB: e.DB, Auth: e.Auth, Audit: audit.Default, Clock: e.Clock})
	s, err := kit.GetSettings(ctx, e.Admin)
	if err != nil {
		t.Fatalf("grntest: read the business settings: %v", err)
	}
	in := modkit.SettingsInput{LockedUpTo: sql.Null[types.Date]{}, DefaultPOTerms: s.DefaultPOTerms}
	if err := kit.SaveSettings(ctx, e.Admin, s.Version, in); err != nil {
		t.Fatalf("grntest: clear the period lock: %v", err)
	}
}
