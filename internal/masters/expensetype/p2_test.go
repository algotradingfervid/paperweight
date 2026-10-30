package expensetype_test

import (
	"context"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/mstfixture"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

func TestExpenseType_DefaultsCopied(t *testing.T) { // MST-123
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	ctx := context.Background()
	lodging, gst18, gst5 := m.ExpenseTypes["LODGING"], m.TaxCodes["GST18"], m.TaxCodes["GST5"]

	svc := expensetype.New(env.Kit)
	var et expensetype.ExpenseType
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		et, err = svc.Get(ctx, tx, lodging)
		return err
	}); err != nil {
		t.Fatalf("Get(LODGING): %v", err)
	}
	if _, err := svc.Update(ctx, env.Kavya, lodging, et.Version, expensetype.Input{
		Code: et.Code, Name: et.Name, DefaultHSNSAC: "996311", DefaultTaxCodeID: gst18,
	}); err != nil {
		t.Fatalf("Update(LODGING defaults): %v", err)
	}

	header := mstfixture.Header{
		VendorID: m.Parties["SHREE"], AddressID: m.Addresses["Mumbai"], BranchID: m.Branches["PUNE"],
		Date: types.Date{Year: 2026, Month: time.October, Day: 24},
	}
	stored := func(id int64) (hsnSAC string, taxCodeID int64) {
		t.Helper()
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			return tx.SQL().QueryRowContext(ctx, `SELECT hsn_sac, COALESCE(tax_code_id, 0) FROM mst_order_line WHERE parent_id = ?`, id).Scan(&hsnSAC, &taxCodeID)
		}); err != nil {
			t.Fatalf("read line of order %d: %v", id, err)
		}
		return hsnSAC, taxCodeID
	}

	// A new line that picks LODGING starts from its defaults.
	line := mstfixture.Line{ExpenseTypeID: lodging, Qty: 1000, Rate: 2000_0000}
	res, err := mstfixture.Save(ctx, env, env.Kavya, 0, header, []mstfixture.Line{line})
	if err != nil {
		t.Fatalf("Save(LODGING line): %v", err)
	}
	if hsnSAC, taxCodeID := stored(res.ID); hsnSAC != "996311" || taxCodeID != gst18 {
		t.Errorf("line = SAC %q, tax code %d; want 996311, GST18 %d", hsnSAC, taxCodeID, gst18)
	}

	// Both stay editable on the line.
	line.HSNSAC, line.TaxCodeID = "996332", gst5
	if _, err := mstfixture.Save(ctx, env, env.Kavya, res.ID, header, []mstfixture.Line{line}); err != nil {
		t.Fatalf("Save(edited line): %v", err)
	}
	if hsnSAC, taxCodeID := stored(res.ID); hsnSAC != "996332" || taxCodeID != gst5 {
		t.Errorf("edited line = SAC %q, tax code %d; want 996332, GST5 %d", hsnSAC, taxCodeID, gst5)
	}

	// A disabled default isn't copied (CD-180); the SAC still is, and the
	// expense type keeps the tax code for when it is enabled again.
	taxes := taxcode.New(env.Kit)
	var tc taxcode.TaxCode
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		tc, err = taxes.Get(ctx, tx, gst18)
		return err
	}); err != nil {
		t.Fatalf("Get(GST18): %v", err)
	}
	if err := taxes.SetDisabled(ctx, env.Kavya, gst18, tc.Version, true); err != nil {
		t.Fatalf("disable GST18: %v", err)
	}
	line.HSNSAC, line.TaxCodeID = "", 0
	res, err = mstfixture.Save(ctx, env, env.Kavya, 0, header, []mstfixture.Line{line})
	if err != nil {
		t.Fatalf("Save(LODGING line, GST18 disabled): %v", err)
	}
	if hsnSAC, taxCodeID := stored(res.ID); hsnSAC != "996311" || taxCodeID != 0 {
		t.Errorf("line with GST18 disabled = SAC %q, tax code %d; want 996311, none", hsnSAC, taxCodeID)
	}
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		et, err = svc.Get(ctx, tx, lodging)
		return err
	}); err != nil {
		t.Fatalf("Get(LODGING): %v", err)
	}
	if et.DefaultTaxCodeID != gst18 {
		t.Errorf("LODGING's stored default = %d; want GST18 %d", et.DefaultTaxCodeID, gst18)
	}
}
