package item_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The MST-079…MST-083 messages name UoM codes, so these tests save through
// the Service against the seeded UoMs (the item Input carries UoM ids).

func factor(t *testing.T, s string) types.Factor {
	t.Helper()
	f, err := types.ParseFactor(s)
	if err != nil {
		t.Fatalf("types.ParseFactor(%q): %v", s, err)
	}
	return f
}

// boltInput is BOLT-M12 "Hex Bolt M12x60", goods, HSN 7318, stock UoM NOS.
func boltInput(t *testing.T, env *msttest.Env, rows ...item.Conversion) item.Input {
	t.Helper()
	return item.Input{
		Code: "BOLT-M12", Name: "Hex Bolt M12x60", Type: goods, HSNSAC: "7318",
		StockUoMID: uomID(t, env, "NOS"), IsStock: true, Conversions: rows,
	}
}

// conversions reads the item's conversion table.
func conversions(t *testing.T, env *msttest.Env, id int64) []item.Conversion {
	t.Helper()
	ctx := context.Background()
	var got []item.Conversion
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		got, err = item.New(env.Kit).Conversions(ctx, tx, id)
		return err
	}); err != nil {
		t.Fatalf("Conversions(%d): %v", id, err)
	}
	return got
}

// itemFactor reads Factor(itemID, uomID).
func itemFactor(t *testing.T, env *msttest.Env, itemID, uomID int64) (types.Factor, bool) {
	t.Helper()
	ctx := context.Background()
	var f types.Factor
	var ok bool
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		f, ok, err = item.New(env.Kit).Factor(ctx, tx, itemID, uomID)
		return err
	}); err != nil {
		t.Fatalf("Factor(%d, %d): %v", itemID, uomID, err)
	}
	return f, ok
}

func getItem(t *testing.T, env *msttest.Env, id int64) item.Item {
	t.Helper()
	ctx := context.Background()
	var it item.Item
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		it, err = item.New(env.Kit).Get(ctx, tx, id)
		return err
	}); err != nil {
		t.Fatalf("Get(%d): %v", id, err)
	}
	return it
}

func TestItemUoM_StockRowAuto(t *testing.T) { // MST-079
	env := msttest.New(t)
	saved, err := item.New(env.Kit).Create(context.Background(), env.Kavya, boltInput(t, env))
	if err != nil {
		t.Fatalf("Create(BOLT-M12, no rows): %v", err)
	}
	nos := uomID(t, env, "NOS")
	want := []item.Conversion{{UoMID: nos, Factor: factor(t, "1")}}
	if got := conversions(t, env, saved.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("Conversions(BOLT-M12) = %+v; want [(NOS %d, 1)] %+v", got, nos, want)
	}
	if f, ok := itemFactor(t, env, saved.ID, nos); !ok || f != factor(t, "1") {
		t.Errorf("Factor(BOLT-M12, NOS) = %d, %v; want %d, true", f, ok, factor(t, "1"))
	}
}

func TestItemUoM_StockFactorOne(t *testing.T) { // MST-080
	env := msttest.New(t)
	nos := uomID(t, env, "NOS")
	_, err := item.New(env.Kit).Create(context.Background(), env.Kavya,
		boltInput(t, env, item.Conversion{UoMID: nos, Factor: factor(t, "2")}))
	wantOnlyMessage(t, "Create(BOLT-M12, row (NOS, 2))", err, "The conversion factor for the stock UoM must be 1.")
	if n := countItems(t, env.DB); n != 0 {
		t.Errorf("item rows after refused create = %d; want 0", n)
	}
}

func TestItemUoM_FactorPositive(t *testing.T) { // MST-081
	env := msttest.New(t)
	nos, box := uomID(t, env, "NOS"), uomID(t, env, "BOX")
	for _, c := range []struct {
		shown string
		f     types.Factor
	}{
		{"0", 0},
		{"-100", -100_000_000_000},
	} {
		_, err := item.New(env.Kit).Create(context.Background(), env.Kavya, boltInput(t, env,
			item.Conversion{UoMID: nos, Factor: factor(t, "1")},
			item.Conversion{UoMID: box, Factor: c.f},
		))
		wantOnlyMessage(t, "Create(BOLT-M12, rows (NOS, 1), (BOX, "+c.shown+"))", err, "Row 2: Conversion factor must be more than 0.")
	}
	if n := countItems(t, env.DB); n != 0 {
		t.Errorf("item rows after refused creates = %d; want 0", n)
	}
}

func TestItemUoM_NoDuplicateUoM(t *testing.T) { // MST-082
	env := msttest.New(t)
	nos, box := uomID(t, env, "NOS"), uomID(t, env, "BOX")
	_, err := item.New(env.Kit).Create(context.Background(), env.Kavya, boltInput(t, env,
		item.Conversion{UoMID: nos, Factor: factor(t, "1")},
		item.Conversion{UoMID: box, Factor: factor(t, "100")},
		item.Conversion{UoMID: box, Factor: factor(t, "50")},
	))
	wantOnlyMessage(t, "Create(BOLT-M12, rows (BOX, 100), (BOX, 50))", err, "UoM BOX is entered more than once. Keep one row per UoM.")
	if n := countItems(t, env.DB); n != 0 {
		t.Errorf("item rows after refused create = %d; want 0", n)
	}
}

func TestItemUoM_PurchaseUoMNeedsRow(t *testing.T) { // MST-083
	env := msttest.New(t)
	ctx := context.Background()
	svc := item.New(env.Kit)
	nos, box := uomID(t, env, "NOS"), uomID(t, env, "BOX")

	// Purchase UoM BOX with no BOX row.
	in := boltInput(t, env)
	in.PurchaseUoMID = box
	_, err := svc.Create(ctx, env.Kavya, in)
	wantOnlyMessage(t, "Create(BOLT-M12, purchase BOX, no BOX row)", err,
		"Purchase UoM BOX has no conversion for this item. Add a row such as 1 BOX = 100 NOS.")
	if n := countItems(t, env.DB); n != 0 {
		t.Errorf("item rows after refused create = %d; want 0", n)
	}

	// With row (BOX, 100) it saves.
	in.Conversions = []item.Conversion{{UoMID: box, Factor: factor(t, "100")}}
	saved, err := svc.Create(ctx, env.Kavya, in)
	if err != nil {
		t.Fatalf("Create(BOLT-M12, purchase BOX, row (BOX, 100)): %v", err)
	}
	if got := getItem(t, env, saved.ID); got.PurchaseUoMID != box || got.StockUoMID != nos {
		t.Errorf("BOLT-M12 = {StockUoMID %d, PurchaseUoMID %d}; want {NOS %d, BOX %d}", got.StockUoMID, got.PurchaseUoMID, nos, box)
	}
	if f, ok := itemFactor(t, env, saved.ID, box); !ok || f != factor(t, "100") {
		t.Errorf("Factor(BOLT-M12, BOX) = %d, %v; want %d, true", f, ok, factor(t, "100"))
	}
	if f, ok := itemFactor(t, env, saved.ID, nos); !ok || f != factor(t, "1") {
		t.Errorf("Factor(BOLT-M12, NOS) = %d, %v; want %d, true (stock row added)", f, ok, factor(t, "1"))
	}

	// A blank purchase UoM defaults to the stock UoM.
	plain := item.Input{Code: "NUT-M12", Name: "Hex Nut M12", Type: goods, HSNSAC: "7318", StockUoMID: nos, IsStock: true}
	saved, err = svc.Create(ctx, env.Kavya, plain)
	if err != nil {
		t.Fatalf("Create(NUT-M12, blank purchase UoM): %v", err)
	}
	if got := getItem(t, env, saved.ID); got.PurchaseUoMID != nos {
		t.Errorf("NUT-M12.PurchaseUoMID = %d; want the stock UoM NOS %d", got.PurchaseUoMID, nos)
	}
}

func TestItemUoM_StockUoMChangeResets(t *testing.T) { // MST-089
	env := msttest.New(t)
	ctx := context.Background()
	svc := item.New(env.Kit)

	g, err := uom.New(env.Kit).Create(ctx, env.Kavya, uom.Input{Code: "GRAM", Name: "Grams", UQC: "GMS"})
	if err != nil {
		t.Fatalf("Create(UoM GRAM): %v", err)
	}
	gram, box, nos := g.ID, uomID(t, env, "BOX"), uomID(t, env, "NOS")

	rows := []item.Conversion{{UoMID: gram, Factor: factor(t, "1")}, {UoMID: box, Factor: factor(t, "2")}}
	in := item.Input{Code: "TEST-POWDER", Name: "Test powder", Type: goods, HSNSAC: "2523", StockUoMID: gram, IsStock: true, Conversions: rows}
	saved, err := svc.Create(ctx, env.Kavya, in)
	if err != nil {
		t.Fatalf("Create(stock GRAM, rows (GRAM, 1), (BOX, 2)): %v", err)
	}
	if len(saved.Warnings) != 0 {
		t.Errorf("Create Saved.Warnings = %q; want none", saved.Warnings)
	}
	if got := conversions(t, env, saved.ID); !reflect.DeepEqual(got, rows) {
		t.Errorf("Conversions before the change = %+v; want %+v", got, rows)
	}

	// The page posts the old rows with the new stock UoM.
	in.StockUoMID = nos
	upd, err := svc.Update(ctx, env.Kavya, saved.ID, saved.Version, in)
	if err != nil {
		t.Fatalf("Update(stock UoM GRAM → NOS): %v", err)
	}
	wantWarn := []string{"Stock UoM changed. Re-enter the other conversions."}
	if !reflect.DeepEqual(upd.Warnings, wantWarn) {
		t.Errorf("Update Saved.Warnings = %q; want %q", upd.Warnings, wantWarn)
	}
	if upd.Version != saved.Version+1 {
		t.Errorf("Update Saved.Version = %d; want %d", upd.Version, saved.Version+1)
	}
	want := []item.Conversion{{UoMID: nos, Factor: factor(t, "1")}}
	if got := conversions(t, env, saved.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("Conversions after the change = %+v; want [(NOS %d, 1)] %+v", got, nos, want)
	}
	if got := getItem(t, env, saved.ID); got.StockUoMID != nos {
		t.Errorf("StockUoMID after the change = %d; want NOS %d", got.StockUoMID, nos)
	}
	for _, u := range []struct {
		code string
		id   int64
	}{{"GRAM", gram}, {"BOX", box}} {
		if f, ok := itemFactor(t, env, saved.ID, u.id); ok {
			t.Errorf("Factor(item, %s) = %d, true after the reset; want no row", u.code, f)
		}
	}
}
