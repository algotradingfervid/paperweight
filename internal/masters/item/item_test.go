package item_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Unit tests use the literal 06-masters.md values (MST-069…MST-083);
// integration tests use the canonical fixtures (M7).

// The item types (Task 10 Produces: Goods, Service). Written as conversions
// so the tests don't depend on the constants' Go names.
const (
	goods   = item.ItemType("Goods")
	service = item.ItemType("Service")
)

// stockUoMID stands in for a stock UoM id in pure Validate calls.
const stockUoMID int64 = 1

// validationErr returns err as a 422 *mstkit.ValidationError, or fails.
func validationErr(t *testing.T, what string, err error) *mstkit.ValidationError {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("%s: err = %v (%T); want *mstkit.ValidationError", what, err, err)
		return nil
	}
	if ve.Status() != 422 {
		t.Errorf("%s: Status() = %d; want 422", what, ve.Status())
	}
	return ve
}

// wantMessage checks that err is a 422 *mstkit.ValidationError with a
// field error whose message is msg (other errors may be present).
func wantMessage(t *testing.T, what string, err error, msg string) {
	t.Helper()
	ve := validationErr(t, what, err)
	if ve == nil {
		return
	}
	for _, fe := range ve.FieldErrors() {
		if fe.Message == msg {
			return
		}
	}
	t.Errorf("%s: FieldErrors() = %+v; want one with message %q", what, ve.FieldErrors(), msg)
}

// wantOnlyMessage checks that err is a 422 *mstkit.ValidationError holding
// exactly one field error, with message msg.
func wantOnlyMessage(t *testing.T, what string, err error, msg string) {
	t.Helper()
	ve := validationErr(t, what, err)
	if ve == nil {
		return
	}
	if got := ve.FieldErrors(); len(got) != 1 || got[0].Message != msg {
		t.Errorf("%s: FieldErrors() = %+v; want exactly one with message %q", what, got, msg)
	}
}

// goodsInput is MS-ANGLE-50 "MS Angle 50x50x5" with HSN hsn.
func goodsInput(hsn string) item.Input {
	return item.Input{Code: "MS-ANGLE-50", Name: "MS Angle 50x50x5", Type: goods, HSNSAC: hsn, StockUoMID: stockUoMID, IsStock: true}
}

// serviceInput is SVC-CRANE "Crane hire" with SAC sac.
func serviceInput(sac string) item.Input {
	return item.Input{Code: "SVC-CRANE", Name: "Crane hire", Type: service, HSNSAC: sac, StockUoMID: stockUoMID}
}

func TestItem_RequiredFields(t *testing.T) { // MST-069
	const (
		typeMsg  = "Item type is required (Goods or Service)."
		stockMsg = "Stock UoM is required."
	)
	noType := goodsInput("7214")
	noType.Type = ""
	_, err := item.Validate(noType)
	wantMessage(t, "Validate(no type)", err, typeMsg)

	noStock := goodsInput("7214")
	noStock.StockUoMID = 0
	_, err = item.Validate(noStock)
	wantOnlyMessage(t, "Validate(no stock UoM)", err, stockMsg)

	neither := goodsInput("7214")
	neither.Type, neither.StockUoMID = "", 0
	_, err = item.Validate(neither)
	wantMessage(t, "Validate(no type, no stock UoM)", err, typeMsg)
	wantMessage(t, "Validate(no type, no stock UoM)", err, stockMsg)

	if _, err := item.Validate(goodsInput("7214")); err != nil {
		t.Errorf("Validate(complete goods item): %v; want nil", err)
	}
}

func TestItem_GoodsNeedHSN(t *testing.T) { // MST-070
	_, err := item.Validate(goodsInput(""))
	wantOnlyMessage(t, "Validate(MS-ANGLE-50 without HSN)", err, "HSN code is required for goods. Enter a 4, 6 or 8 digit HSN code.")
}

func TestItem_HSNLength(t *testing.T) { // MST-071
	for _, hsn := range []string{"7214", "721420", "72142090"} {
		out, err := item.Validate(goodsInput(hsn))
		if err != nil {
			t.Errorf("Validate(HSN %q): %v; want nil", hsn, err)
			continue
		}
		if out.HSNSAC != hsn {
			t.Errorf("Validate(HSN %q).HSNSAC = %q; want %q", hsn, out.HSNSAC, hsn)
		}
	}
	for _, hsn := range []string{"100", "72142", "7214209010", "72A4", "7214 20"} {
		_, err := item.Validate(goodsInput(hsn))
		wantOnlyMessage(t, "Validate(HSN "+hsn+")", err, "HSN code must be 4, 6 or 8 digits.")
	}
}

func TestItem_GoodsHSNNot99(t *testing.T) { // MST-072
	_, err := item.Validate(goodsInput("998719"))
	wantOnlyMessage(t, "Validate(goods HSN 998719)", err, "Codes starting 99 are service (SAC) codes. Set the item type to Service or use a goods HSN.")
}

func TestItem_ServiceSAC(t *testing.T) { // MST-073
	for _, sac := range []string{"", "997313"} {
		out, err := item.Validate(serviceInput(sac))
		if err != nil {
			t.Errorf("Validate(SVC-CRANE SAC %q): %v; want nil", sac, err)
			continue
		}
		if out.HSNSAC != sac || out.Type != service {
			t.Errorf("Validate(SVC-CRANE SAC %q) = {Type %q, HSNSAC %q}; want {Service, %q}", sac, out.Type, out.HSNSAC, sac)
		}
	}
	for _, sac := range []string{"9973", "721420"} {
		_, err := item.Validate(serviceInput(sac))
		wantOnlyMessage(t, "Validate(SVC-CRANE SAC "+sac+")", err, "SAC code must be 6 digits starting with 99.")
	}
}

// uomID returns the id of the UoM with code.
func uomID(t *testing.T, env *msttest.Env, code string) int64 {
	t.Helper()
	ctx := context.Background()
	var u uom.UoM
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var ok bool
		var err error
		u, ok, err = uom.New(env.Kit).ByCode(ctx, tx, code)
		if err == nil && !ok {
			t.Fatalf("UoM %s not found", code)
		}
		return err
	}); err != nil {
		t.Fatalf("UoM %s: %v", code, err)
	}
	return u.ID
}

// taxCode returns the seeded tax code with code.
func taxCode(t *testing.T, env *msttest.Env, code string) taxcode.TaxCode {
	t.Helper()
	ctx := context.Background()
	var tc taxcode.TaxCode
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var ok bool
		var err error
		tc, ok, err = taxcode.New(env.Kit).ByCode(ctx, tx, code)
		if err == nil && !ok {
			t.Fatalf("tax code %s not found", code)
		}
		return err
	}); err != nil {
		t.Fatalf("tax code %s: %v", code, err)
	}
	return tc
}

// countItems counts item rows.
func countItems(t *testing.T, d *db.DB) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := d.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM item`).Scan(&n)
	}); err != nil {
		t.Fatalf("count item: %v", err)
	}
	return n
}

func TestItem_DefaultTaxCodeEnabled(t *testing.T) { // MST-074
	env := msttest.New(t)
	ctx := context.Background()
	svc := item.New(env.Kit)

	gst18 := taxCode(t, env, "GST18")
	kgs := uomID(t, env, "KGS")
	in := item.Input{Code: "MS-ANGLE-50", Name: "MS Angle 50x50x5", Type: goods, HSNSAC: "7216", StockUoMID: kgs, DefaultTaxCodeID: gst18.ID, IsStock: true}

	// While GST18 is enabled the item saves with it.
	saved, err := svc.Create(ctx, env.Kavya, in)
	if err != nil {
		t.Fatalf("Create(MS-ANGLE-50, GST18 enabled): %v", err)
	}
	var got item.Item
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		got, err = svc.Get(ctx, tx, saved.ID)
		return err
	}); err != nil {
		t.Fatalf("Get(MS-ANGLE-50): %v", err)
	}
	if got.DefaultTaxCodeID != gst18.ID {
		t.Errorf("Get(MS-ANGLE-50).DefaultTaxCodeID = %d; want GST18 %d", got.DefaultTaxCodeID, gst18.ID)
	}

	if err := taxcode.New(env.Kit).SetDisabled(ctx, env.Kavya, gst18.ID, gst18.Version, true); err != nil {
		t.Fatalf("disable GST18: %v", err)
	}
	const msg = "Tax code GST18 is disabled. Pick another tax code."

	// A new item can't take the disabled tax code.
	in.Code, in.Name, in.HSNSAC = "MS-ANGLE-65", "MS Angle 65x65x6", "7216"
	_, err = svc.Create(ctx, env.Kavya, in)
	wantOnlyMessage(t, "Create(MS-ANGLE-65, GST18 disabled)", err, msg)
	if n := countItems(t, env.DB); n != 1 {
		t.Errorf("item rows after refused create = %d; want 1 (nothing stored)", n)
	}

	// Nor can an existing item be set to it.
	gst5 := taxCode(t, env, "GST5")
	other := item.Input{Code: "BOLT-M12", Name: "Hex Bolt M12x60", Type: goods, HSNSAC: "7318", StockUoMID: uomID(t, env, "NOS"), DefaultTaxCodeID: gst5.ID, IsStock: true}
	bolt, err := svc.Create(ctx, env.Kavya, other)
	if err != nil {
		t.Fatalf("Create(BOLT-M12, GST5): %v", err)
	}
	other.DefaultTaxCodeID = gst18.ID
	_, err = svc.Update(ctx, env.Kavya, bolt.ID, bolt.Version, other)
	wantOnlyMessage(t, "Update(BOLT-M12 → GST18 disabled)", err, msg)
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		got, err = svc.Get(ctx, tx, bolt.ID)
		return err
	}); err != nil {
		t.Fatalf("Get(BOLT-M12): %v", err)
	}
	if got.DefaultTaxCodeID != gst5.ID || got.Version != bolt.Version {
		t.Errorf("BOLT-M12 after refused update = {DefaultTaxCodeID %d, Version %d}; want {GST5 %d, %d} (unchanged)",
			got.DefaultTaxCodeID, got.Version, gst5.ID, bolt.Version)
	}

	// A no-op save of the item that took GST18 while enabled is refused too:
	// the check runs before the no-change skip (CD-208).
	in.Code, in.Name = "MS-ANGLE-50", "MS Angle 50x50x5"
	_, err = svc.Update(ctx, env.Kavya, saved.ID, saved.Version, in)
	wantOnlyMessage(t, "Update(MS-ANGLE-50 unchanged, GST18 disabled since)", err, msg)
}

func TestItem_NegativeStandardRate(t *testing.T) { // CD-220
	const msg = "Standard Rate can't be negative."
	wantRefused := func(what string, err error) {
		t.Helper()
		wantOnlyMessage(t, what, err, msg)
		if ve := validationErr(t, what, err); ve != nil {
			if got := ve.FieldErrors(); got[0].Field != "standard_rate" {
				t.Errorf("%s: field = %q; want standard_rate", what, got[0].Field)
			}
		}
	}
	env := msttest.New(t)
	ctx := context.Background()
	svc := item.New(env.Kit)
	in := item.Input{Code: "BOLT-M12", Name: "Hex Bolt", Type: goods, HSNSAC: "7318", StockUoMID: uomID(t, env, "NOS"), IsStock: true}

	negative := in
	negative.StandardRate = -1
	_, err := svc.Create(ctx, env.Kavya, negative)
	wantRefused("Create(rate -0.0001)", err)

	saved, err := svc.Create(ctx, env.Kavya, in) // a zero rate is none (CD-37)
	if err != nil {
		t.Fatalf("Create(rate 0): %v", err)
	}
	_, err = svc.Update(ctx, env.Kavya, saved.ID, saved.Version, negative)
	wantRefused("Update(rate -0.0001)", err)
}
