package company_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// TestBranch_DisableWithSites: only enabled sites hold a branch, named by code
// in sorted order, and the refused branch doesn't move. The other branch's
// sites don't count.
func TestBranch_DisableWithSites(t *testing.T) { // MST-108
	ctx := context.Background()
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	co, sites := company.New(env.Kit), site.New(env.Kit)
	pune := func() company.Branch {
		t.Helper()
		var b company.Branch
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			b, err = co.Get(ctx, tx, m.Branches["PUNE"])
			return err
		}); err != nil {
			t.Fatalf("read PUNE: %v", err)
		}
		return b
	}
	disableSite := func(code string) {
		t.Helper()
		var st site.Site
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			st, err = sites.Get(ctx, tx, m.Sites[code])
			return err
		}); err != nil {
			t.Fatalf("read %s: %v", code, err)
		}
		if err := sites.SetDisabled(ctx, env.Kavya, st.ID, st.Version, true); err != nil {
			t.Fatalf("disable %s: %v", code, err)
		}
	}
	wantRefused := func(msg string) {
		t.Helper()
		before := pune()
		err := co.SetDisabled(ctx, env.Kavya, before.ID, before.Version, true)
		var refused *mstkit.DeleteRefusedError
		if !errors.As(err, &refused) || refused.Message != msg {
			t.Fatalf("disable PUNE = %v (%T); want refusal %q", err, err, msg)
		}
		if refused.Status() != 422 || refused.FieldErrors() != nil {
			t.Errorf("refusal = status %d, fields %+v; want 422 with no field", refused.Status(), refused.FieldErrors())
		}
		if after := pune(); after != before {
			t.Errorf("PUNE after refusal = %+v; want %+v", after, before)
		}
	}

	wantRefused("Disable the branch's sites first: METRO-SITE, PUNE-WH.")
	disableSite("PUNE-WH")
	wantRefused("Disable the branch's sites first: METRO-SITE.")
	disableSite("METRO-SITE")

	b := pune()
	if err := co.SetDisabled(ctx, env.Kavya, b.ID, b.Version, true); err != nil {
		t.Fatalf("disable PUNE with its sites disabled: %v", err)
	}
	if !pune().Disabled {
		t.Error("PUNE is still enabled")
	}
}
