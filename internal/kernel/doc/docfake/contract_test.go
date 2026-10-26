package docfake_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// TestParity_DocFakeReads checks that docfake's Info, Load and References
// return what the real engine returns once given the engine's data: an
// existing document, a missing id and a referenced master row (CD-125). Do is
// not compared: the fake only returns what SetResult set, and the doctest
// contract suite runs against the real engine alone.
func TestParity_DocFakeReads(t *testing.T) {
	h := fixturetest.New(t)
	ctx := t.Context()
	id := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000", Rate: "62.5000"}).ID
	party := h.M.Party["SHREE"]
	const missing = 9999

	var (
		rec              doc.Record
		info             doctype.Info
		loadErr, infoErr error
		refs, none       []doc.Reference
		first            string
		isRef, noneRef   bool
	)
	err := h.DB.Read(ctx, func(tx *db.Tx) (err error) {
		if rec, err = h.Engine.Load(ctx, tx, fixture.Order, id); err != nil {
			return err
		}
		if info, err = h.Engine.Info(ctx, tx, fixture.Order, id); err != nil {
			return err
		}
		_, loadErr = h.Engine.Load(ctx, tx, fixture.Order, missing)
		_, infoErr = h.Engine.Info(ctx, tx, fixture.Order, missing)
		if refs, err = h.Engine.References(ctx, tx, "fixture_party", party, 10); err != nil {
			return err
		}
		if none, err = h.Engine.References(ctx, tx, "fixture_party", missing, 10); err != nil {
			return err
		}
		if first, isRef, err = h.Engine.IsReferenced(ctx, tx, "fixture_party", party); err != nil {
			return err
		}
		_, noneRef, err = h.Engine.IsReferenced(ctx, tx, "fixture_party", missing)
		return err
	})
	if err != nil {
		t.Fatalf("engine read: %v", err)
	}
	if len(refs) == 0 || !isRef || noneRef {
		t.Fatalf("engine: References(SHREE) = %+v, IsReferenced = %v, unreferenced IsReferenced = %v; want a reference to SHREE only", refs, isRef, noneRef)
	}

	f := docfake.New()
	f.SetRecord(rec)
	for _, r := range refs {
		f.AddReference("fixture_party", party, r)
	}

	t.Run("existing", func(t *testing.T) {
		if got, err := f.Load(ctx, nil, fixture.Order, id); err != nil || !reflect.DeepEqual(got, rec) {
			t.Errorf("fake Load = (%+v, %v), want the engine's (%+v, nil)", got, err, rec)
		}
		if got, err := f.Info(ctx, nil, fixture.Order, id); err != nil || !reflect.DeepEqual(got, info) {
			t.Errorf("fake Info = (%+v, %v), want the engine's (%+v, nil)", got, err, info)
		}
	})

	t.Run("missing", func(t *testing.T) {
		_, fl := f.Load(ctx, nil, fixture.Order, missing)
		_, fi := f.Info(ctx, nil, fixture.Order, missing)
		sameError(t, "Load", fl, loadErr)
		sameError(t, "Info", fi, infoErr)
	})

	t.Run("references", func(t *testing.T) {
		if got, err := f.References(ctx, nil, "fixture_party", party, 10); err != nil || !reflect.DeepEqual(got, refs) {
			t.Errorf("fake References = (%+v, %v), want the engine's (%+v, nil)", got, err, refs)
		}
		if got, err := f.References(ctx, nil, "fixture_party", missing, 10); err != nil || !reflect.DeepEqual(got, none) {
			t.Errorf("fake References of an unreferenced row = (%+v, %v), want the engine's (%+v, nil)", got, err, none)
		}
		if got, ok, err := f.IsReferenced(ctx, nil, "fixture_party", party); err != nil || got != first || !ok {
			t.Errorf("fake IsReferenced = (%q, %v, %v), want the engine's (%q, true, nil)", got, ok, err, first)
		}
		if _, ok, err := f.IsReferenced(ctx, nil, "fixture_party", missing); err != nil || ok {
			t.Errorf("fake IsReferenced of an unreferenced row = (%v, %v), want the engine's (false, nil)", ok, err)
		}
	})
}

// sameError checks the fake's error matches the engine's: same text, same
// HTTP status and both doctype.ErrNotFound.
func sameError(t *testing.T, op string, got, want error) {
	t.Helper()
	if want == nil || !errors.Is(want, doctype.ErrNotFound) {
		t.Fatalf("engine %s of a missing id error = %v, want doctype.ErrNotFound", op, want)
	}
	if got == nil || got.Error() != want.Error() || !errors.Is(got, doctype.ErrNotFound) {
		t.Errorf("fake %s of a missing id error = %v, want the engine's %q matching doctype.ErrNotFound", op, got, want)
	}
	var gh, wh doctype.HTTPError
	if !errors.As(got, &gh) || !errors.As(want, &wh) || gh.Status() != wh.Status() {
		t.Errorf("fake %s of a missing id: HTTP status differs from the engine's (fake %v, engine %v)", op, got, want)
	}
}
