package docfake_test

import (
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
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

// TestParity_DocFakeProblems checks Problems, ActionInput.Only and
// Result.Problems on the fake (CD-205). docfake reads like its other reads
// (CD-186): it returns what SetProblems stored and computes nothing. What the
// fake must match:
//
//   - a missing record is the engine's not-found error, even with problems set;
//   - a draft returns what SetProblems stored, given the engine's output;
//   - a stored draft with nothing set returns none, as the engine does for a
//     clean draft, so pages over a seeded record render;
//   - a document past draft returns none, even with problems set: the engine
//     never reports problems past draft, so the fake can't either;
//   - Do with Only set still returns ErrNotImplemented unless SetResult was
//     called: the fake doesn't scope anything.
func TestParity_DocFakeProblems(t *testing.T) {
	h := fixturetest.New(t)
	ctx := t.Context()
	bad := h.NewOrderWith(t, h.Priya, map[string]string{"supplier_id": strconv.FormatInt(h.M.Party["RAJKOT"], 10)}).ID
	clean := h.NewOrder(t, h.Priya).ID
	approved := h.ApprovedOrder(t, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000", Rate: "62.5000"}).ID
	fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
	const missing = 9999

	var (
		badRec, cleanRec, apRec doc.Record
		badPr, cleanPr, apPr    []doctype.FieldError
		missErr                 error
	)
	err := h.DB.Read(ctx, func(tx *db.Tx) (err error) {
		if badRec, err = h.Engine.Load(ctx, tx, fixture.Order, bad); err != nil {
			return err
		}
		if cleanRec, err = h.Engine.Load(ctx, tx, fixture.Order, clean); err != nil {
			return err
		}
		if apRec, err = h.Engine.Load(ctx, tx, fixture.Order, approved); err != nil {
			return err
		}
		if badPr, err = h.Engine.Problems(ctx, tx, fixture.Order, bad); err != nil {
			return err
		}
		if cleanPr, err = h.Engine.Problems(ctx, tx, fixture.Order, clean); err != nil {
			return err
		}
		if apPr, err = h.Engine.Problems(ctx, tx, fixture.Order, approved); err != nil {
			return err
		}
		_, missErr = h.Engine.Problems(ctx, tx, fixture.Order, missing)
		return nil
	})
	if err != nil {
		t.Fatalf("engine read: %v", err)
	}
	if len(badPr) == 0 || len(cleanPr) != 0 || len(apPr) != 0 {
		t.Fatalf("engine: Problems bad = %+v, clean = %+v, approved = %+v; want only the bad draft's", badPr, cleanPr, apPr)
	}

	f := docfake.New()
	f.SetRecord(badRec)
	f.SetRecord(cleanRec)
	f.SetRecord(apRec)
	f.SetProblems(fixture.Order, bad, badPr)
	f.SetProblems(fixture.Order, approved, badPr)
	f.SetProblems(fixture.Order, missing, badPr)

	t.Run("draft with problems", func(t *testing.T) {
		got, err := f.Problems(ctx, nil, fixture.Order, bad)
		if err != nil || !reflect.DeepEqual(got, badPr) {
			t.Errorf("fake Problems = (%+v, %v), want the engine's (%+v, nil)", got, err, badPr)
		}
		got[0].Message = "changed by the caller"
		again, _ := f.Problems(ctx, nil, fixture.Order, bad)
		if !reflect.DeepEqual(again, badPr) {
			t.Errorf("fake Problems shares its slice with callers: %+v", again)
		}
	})
	t.Run("clean draft", func(t *testing.T) {
		if got, err := f.Problems(ctx, nil, fixture.Order, clean); err != nil || len(got) != 0 {
			t.Errorf("fake Problems = (%+v, %v), want none", got, err)
		}
	})
	t.Run("past draft", func(t *testing.T) {
		if got, err := f.Problems(ctx, nil, fixture.Order, approved); err != nil || len(got) != 0 {
			t.Errorf("fake Problems = (%+v, %v), want none, as the engine", got, err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		_, err := f.Problems(ctx, nil, fixture.Order, missing)
		sameError(t, "Problems", err, missErr)
	})
}

// CD-186, CD-205: Do with Only refuses unless told otherwise, records Only
// as a copy, and returns Result.Problems as a copy.
func TestDocFake_DoWithOnly(t *testing.T) {
	f := docfake.New()
	only := []string{"remarks"}
	in := doc.ActionInput{Version: 1, Only: only}
	if _, err := f.Do(t.Context(), auth.Principal{}, fixture.Order, 1, doc.ActionSave, in); !errors.Is(err, errs.ErrNotImplemented) {
		t.Errorf("Do with Only and no SetResult error = %v, want errs.ErrNotImplemented", err)
	}
	only[0] = "changed by the caller"
	if calls := f.Calls(); len(calls) != 1 || !reflect.DeepEqual(calls[0].Input.Only, []string{"remarks"}) {
		t.Errorf("Calls = %+v, want one with Only [remarks]", calls)
	}

	problems := []doctype.FieldError{{Field: "supplier_id", Message: "Supplier is required."}}
	f.SetResult(fixture.Order, 1, doc.ActionSave, doc.Result{ID: 1, Problems: problems}, nil)
	got, err := f.Do(t.Context(), auth.Principal{}, fixture.Order, 1, doc.ActionSave, in)
	if err != nil || !reflect.DeepEqual(got.Problems, problems) {
		t.Fatalf("Do = (%+v, %v), want the SetResult Problems", got, err)
	}
	got.Problems[0].Message = "changed by the caller"
	again, _ := f.Do(t.Context(), auth.Principal{}, fixture.Order, 1, doc.ActionSave, in)
	if again.Problems[0].Message != "Supplier is required." {
		t.Errorf("Do shares Result.Problems with callers: %+v", again.Problems)
	}
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
