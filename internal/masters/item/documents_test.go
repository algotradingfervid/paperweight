package item_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstfixture"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/types"
)

// Integration tests use the canonical fixtures (M7) and mstfixture.Order.

const (
	wholeQty        types.Qty = 1000 // 1.000
	stockLockedMsg            = "Stock UoM can't change after the item is used. Create a new item."
	stockChangedMsg           = "Stock UoM changed. Re-enter the other conversions."
)

var orderDate = types.Date{Year: 2026, Month: time.October, Day: 24}

// world is a seeded Env.
type world struct {
	env *msttest.Env
	m   msttest.Masters
	svc *item.Service
}

func newWorld(t *testing.T) world {
	t.Helper()
	env := msttest.New(t)
	return world{env: env, m: msttest.SeedMasters(t, env), svc: item.New(env.Kit)}
}

// shree is a header for SHREE's Mumbai address at branch PUNE.
func (w world) shree() mstfixture.Header {
	return mstfixture.Header{
		VendorID: w.m.Parties["SHREE"], AddressID: w.m.Addresses["Mumbai"],
		BranchID: w.m.Branches["PUNE"], Date: orderDate,
	}
}

// line is qty of item id in UoM unit at ₹62.5000.
func (w world) line(id int64, unit string, qty types.Qty) mstfixture.Line {
	return mstfixture.Line{ItemID: id, UoMID: w.m.UoMs[unit], TaxCodeID: w.m.TaxCodes["GST18"], Qty: qty, Rate: 62_5000}
}

// draft saves a new one-line order.
func (w world) draft(t *testing.T, l mstfixture.Line) int64 {
	t.Helper()
	res, err := mstfixture.Save(context.Background(), w.env, w.env.Kavya, 0, w.shree(), []mstfixture.Line{l})
	if err != nil {
		t.Fatalf("Save(new order): %v", err)
	}
	return res.ID
}

// resave saves the draft again with line l.
func (w world) resave(t *testing.T, id int64, l mstfixture.Line) error {
	t.Helper()
	_, err := mstfixture.Save(context.Background(), w.env, w.env.Kavya, id, w.shree(), []mstfixture.Line{l})
	return err
}

// input is the stored item as the page posts it back, with its version.
func (w world) input(t *testing.T, id int64) (item.Input, int) {
	t.Helper()
	it := getItem(t, w.env, id)
	return item.Input{
		Code: it.Code, Name: it.Name, Type: it.Type, HSNSAC: it.HSNSAC,
		StockUoMID: it.StockUoMID, PurchaseUoMID: it.PurchaseUoMID, DefaultTaxCodeID: it.DefaultTaxCodeID,
		IsStock: it.IsStock, StandardRate: it.StandardRate, Conversions: conversions(t, w.env, id),
	}, it.Version
}

// wantSaveError checks that a refused save holds the field error want.
func wantSaveError(t *testing.T, what string, err error, want doctype.FieldError) {
	t.Helper()
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("%s: err = %v (%T); want an error with field errors", what, err, err)
	}
	for _, fe := range he.FieldErrors() {
		if fe == want {
			return
		}
	}
	t.Errorf("%s: field errors = %+v; want %+v among them", what, he.FieldErrors(), want)
}

func TestUoM_WholeNumberLineQty(t *testing.T) { // MST-086
	w := newWorld(t)
	ctx := context.Background()
	bolt := w.m.Items["BOLT-M12"]

	_, err := mstfixture.Save(ctx, w.env, w.env.Kavya, 0, w.shree(), []mstfixture.Line{w.line(bolt, "NOS", 3400)})
	wantSaveError(t, "qty 3.4 NOS", err, doctype.FieldError{Field: "qty", Line: 1,
		Message: "Row 1: Quantity 3.4 can't be a fraction for UoM NOS. Enter a whole number."})

	id := w.draft(t, w.line(bolt, "NOS", 3*wholeQty))
	if got := mstfixture.Snapshot(t, w.env, id).Lines[0].StockQty; got != 3*wholeQty {
		t.Errorf("stock qty of 3 NOS = %s; want 3", types.FormatQty(got))
	}
}

func TestUoM_WholeNumberStockQty(t *testing.T) { // MST-087
	w := newWorld(t)
	ctx := context.Background()
	saved, err := w.svc.Create(ctx, w.env.Kavya, item.Input{
		Code: "TEST-CLIP", Name: "Test clip", Type: goods, HSNSAC: "7318", StockUoMID: w.m.UoMs["NOS"],
		PurchaseUoMID: w.m.UoMs["KGS"], IsStock: true, Conversions: []item.Conversion{{UoMID: w.m.UoMs["KGS"], Factor: factor(t, "12")}},
	})
	if err != nil {
		t.Fatalf("Create(TEST-CLIP): %v", err)
	}

	id := w.draft(t, w.line(saved.ID, "KGS", 1250))
	if got := mstfixture.Snapshot(t, w.env, id).Lines[0].StockQty; got != 15*wholeQty {
		t.Errorf("stock qty of 1.25 KGS = %s; want 15", types.FormatQty(got))
	}

	_, err = mstfixture.Save(ctx, w.env, w.env.Kavya, 0, w.shree(), []mstfixture.Line{w.line(saved.ID, "KGS", 1300)})
	wantSaveError(t, "qty 1.3 KGS", err, doctype.FieldError{Field: "qty", Line: 1,
		Message: "Row 1: 1.3 KGS = 15.6 NOS, but NOS must be a whole number. Change the quantity."})
}

func TestItemUoM_StockUoMLocked(t *testing.T) { // MST-090
	w := newWorld(t)
	ctx := context.Background()

	t.Run("used on an approved order", func(t *testing.T) {
		angle := w.m.Items["MS-ANGLE-50"]
		mstfixture.Approve(t, w.env, w.draft(t, w.line(angle, "KGS", wholeQty)))

		in, version := w.input(t, angle)
		rows := in.Conversions
		in.StockUoMID = w.m.UoMs["TON"]
		_, err := w.svc.Update(ctx, w.env.Kavya, angle, version, in)
		wantOnlyMessage(t, "Update(KGS → TON)", err, stockLockedMsg)
		wantSaveError(t, "Update(KGS → TON)", err, doctype.FieldError{Field: "stock_uom_id", Message: stockLockedMsg})

		got := getItem(t, w.env, angle)
		if got.StockUoMID != w.m.UoMs["KGS"] || got.Version != version {
			t.Errorf("item after the refusal = {Stock %d, Version %d}; want {KGS %d, %d}", got.StockUoMID, got.Version, w.m.UoMs["KGS"], version)
		}
		if after := conversions(t, w.env, angle); !reflect.DeepEqual(after, rows) {
			t.Errorf("conversions after the refusal = %+v; want %+v", after, rows)
		}

		// A stale page gets the conflict, not the lock (CD-180 D4 pattern).
		_, err = w.svc.Update(ctx, w.env.Kavya, angle, version-1, in)
		var de *doc.Error
		if !errors.As(err, &de) || de.Kind != doc.KindStale {
			t.Errorf("Update(KGS → TON, stale version) = %v (%T); want DOC's stale error", err, err)
		}

		// A blank or unknown stock UoM is a change too, so it gets the lock.
		for _, c := range []struct {
			what string
			id   int64
		}{{"blank", 0}, {"unknown", 999999}} {
			in.StockUoMID = c.id
			_, err := w.svc.Update(ctx, w.env.Kavya, angle, version, in)
			wantOnlyMessage(t, "Update("+c.what+" stock UoM)", err, stockLockedMsg)
			wantSaveError(t, "Update("+c.what+" stock UoM)", err, doctype.FieldError{Field: "stock_uom_id", Message: stockLockedMsg})
		}

		// Other edits stay allowed.
		in.StockUoMID = w.m.UoMs["KGS"]
		in.Name = "MS Angle 50 x 50 x 5"
		if _, err := w.svc.Update(ctx, w.env.Kavya, angle, version, in); err != nil {
			t.Errorf("Update(name only): %v", err)
		}
	})

	t.Run("a cancelled order still counts", func(t *testing.T) {
		bolt := w.m.Items["BOLT-M12"]
		id := w.draft(t, w.line(bolt, "NOS", wholeQty))
		mstfixture.Approve(t, w.env, id)
		mstfixture.Cancel(t, w.env, id)

		in, version := w.input(t, bolt)
		in.StockUoMID = w.m.UoMs["KGS"]
		_, err := w.svc.Update(ctx, w.env.Kavya, bolt, version, in)
		wantOnlyMessage(t, "Update(NOS → KGS)", err, stockLockedMsg)
	})

	t.Run("a draft whose line UoM lost its conversion", func(t *testing.T) {
		wire := w.m.Items["WIRE-18G"]
		id := w.draft(t, w.line(wire, "KGS", wholeQty))

		in, version := w.input(t, wire)
		in.StockUoMID = w.m.UoMs["TON"]
		saved, err := w.svc.Update(ctx, w.env.Kavya, wire, version, in)
		if err != nil {
			t.Fatalf("Update(KGS → TON) with only a draft: %v", err)
		}
		if !reflect.DeepEqual(saved.Warnings, []string{stockChangedMsg}) {
			t.Errorf("Saved.Warnings = %q; want %q", saved.Warnings, stockChangedMsg)
		}

		err = w.resave(t, id, w.line(wire, "KGS", wholeQty))
		wantSaveError(t, "draft's next save", err, doctype.FieldError{Field: "uom_id", Line: 1,
			Message: "Row 1: Item WIRE-18G has no conversion for UoM KGS. Pick another UoM."})
	})

	t.Run("a draft whose line UoM kept its conversion", func(t *testing.T) {
		created, err := w.svc.Create(ctx, w.env.Kavya, item.Input{
			Code: "TEST-PIPE", Name: "Test pipe", Type: goods, HSNSAC: "7318", StockUoMID: w.m.UoMs["KGS"], IsStock: true,
			Conversions: []item.Conversion{{UoMID: w.m.UoMs["NOS"], Factor: factor(t, "2")}},
		})
		if err != nil {
			t.Fatalf("Create(TEST-PIPE): %v", err)
		}
		id := w.draft(t, w.line(created.ID, "NOS", 3*wholeQty))
		if got := mstfixture.Snapshot(t, w.env, id).Lines[0]; got.Factor != factor(t, "2") || got.StockQty != 6*wholeQty {
			t.Fatalf("snapshot before the change = %+v; want factor 2, 6 KGS", got)
		}

		in, version := w.input(t, created.ID)
		in.StockUoMID = w.m.UoMs["NOS"]
		if _, err := w.svc.Update(ctx, w.env.Kavya, created.ID, version, in); err != nil {
			t.Fatalf("Update(KGS → NOS) with only a draft: %v", err)
		}

		// The next save re-validates and takes the new factor.
		if err := w.resave(t, id, w.line(created.ID, "NOS", 3*wholeQty)); err != nil {
			t.Fatalf("draft's next save: %v", err)
		}
		if got := mstfixture.Snapshot(t, w.env, id).Lines[0]; got.Factor != factor(t, "1") || got.StockQty != 3*wholeQty {
			t.Errorf("snapshot after the change = %+v; want factor 1, 3 NOS", got)
		}
	})
}

func TestItemUoM_FactorSnapshot(t *testing.T) { // MST-091
	w := newWorld(t)
	ctx := context.Background()
	bolt := w.m.Items["BOLT-M12"]

	approved := w.draft(t, w.line(bolt, "BOX", 2*wholeQty))
	mstfixture.Approve(t, w.env, approved)
	want := mstfixture.SnapLine{ItemCode: "BOLT-M12", Factor: factor(t, "100"), StockQty: 200 * wholeQty}
	if got := mstfixture.Snapshot(t, w.env, approved).Lines[0]; got != want {
		t.Fatalf("approved snapshot = %+v; want %+v", got, want)
	}

	in, version := w.input(t, bolt)
	box := -1
	for i, c := range in.Conversions {
		if c.UoMID == w.m.UoMs["BOX"] {
			box = i
		}
	}
	if box < 0 {
		t.Fatalf("BOLT-M12 conversions = %+v; want a BOX row", in.Conversions)
	}
	in.Conversions[box].Factor = factor(t, "50")
	if _, err := w.svc.Update(ctx, w.env.Kavya, bolt, version, in); err != nil {
		t.Fatalf("Update(BOX factor 50) on a used item: %v", err)
	}

	if got := mstfixture.Snapshot(t, w.env, approved).Lines[0]; got != want {
		t.Errorf("approved snapshot after the change = %+v; want %+v", got, want)
	}
	fresh := w.draft(t, w.line(bolt, "BOX", 2*wholeQty))
	wantNew := mstfixture.SnapLine{ItemCode: "BOLT-M12", Factor: factor(t, "50"), StockQty: 100 * wholeQty}
	if got := mstfixture.Snapshot(t, w.env, fresh).Lines[0]; got != wantNew {
		t.Errorf("new order's snapshot = %+v; want %+v", got, wantNew)
	}
}
