package docfake_test

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
)

// TestDocFake_RefusesReassign checks that docfake refuses Reassign and
// EligibleOwners (CD-186): a faithful fake would copy the engine's state,
// caller and target checks (M0-13), so callers use the real engine over a test
// DB. It refuses even for a stored draft and its owner.
func TestDocFake_RefusesReassign(t *testing.T) {
	h := fixturetest.New(t)
	ctx := t.Context()
	f := docfake.New()
	id := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000", Rate: "62.5000"}).ID
	var rec doc.Record
	if err := h.DB.Read(ctx, func(tx *db.Tx) (err error) {
		rec, err = h.Engine.Load(ctx, tx, fixture.Order, id)
		return err
	}); err != nil {
		t.Fatalf("Load: %v", err)
	}
	f.SetRecord(rec)

	if err := f.Reassign(ctx, h.Priya, fixture.Order, id, rec.Version, h.Ravi.ID); !errors.Is(err, errs.ErrNotImplemented) {
		t.Errorf("Reassign = %v, want errs.ErrNotImplemented", err)
	}
	err := h.DB.Read(ctx, func(tx *db.Tx) error {
		owners, err := f.EligibleOwners(ctx, tx, h.Priya, fixture.Order, id)
		if owners != nil {
			t.Errorf("EligibleOwners = %v, want nil", owners)
		}
		return err
	})
	if !errors.Is(err, errs.ErrNotImplemented) {
		t.Errorf("EligibleOwners = %v, want errs.ErrNotImplemented", err)
	}
}
