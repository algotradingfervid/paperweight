package item_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstfixture"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const typeLockedMsg = "Item type can't change after the item is used. Create a new item."

func TestItem_TypeLocked(t *testing.T) { // MST-075
	w := newWorld(t)
	ctx := context.Background()
	svc, kavya := w.svc, w.env.Kavya
	angle := w.m.Items["MS-ANGLE-50"]
	asService := func() (item.Input, int) {
		in, v := w.input(t, angle)
		in.Type, in.HSNSAC, in.IsStock = service, "997313", false
		return in, v
	}

	// While only a draft uses it the type may change, and change back.
	order := w.draft(t, w.line(angle, "KGS", wholeQty))
	in, v := asService()
	saved, err := svc.Update(ctx, kavya, angle, v, in)
	if err != nil {
		t.Fatalf("Update(to Service, only a draft uses it): %v", err)
	}
	in.Type, in.HSNSAC, in.IsStock = goods, "7216", true
	if _, err := svc.Update(ctx, kavya, angle, saved.Version, in); err != nil {
		t.Fatalf("Update(back to Goods): %v", err)
	}

	mstfixture.Approve(t, w.env, order)

	in, v = asService()
	_, err = svc.Update(ctx, kavya, angle, v, in)
	wantSaveError(t, "Update(to Service, used)", err, doctype.FieldError{Field: "item_type", Message: typeLockedMsg})
	wantOnlyMessage(t, "Update(to Service, used)", err, typeLockedMsg)
	if got := getItem(t, w.env, angle); got.Type != goods || got.Version != v {
		t.Errorf("item after the refusal = %+v; want Goods at version %d", got, v)
	}

	// A stock UoM change in the same save gets both refusals.
	in.StockUoMID = w.m.UoMs["TON"]
	_, err = svc.Update(ctx, kavya, angle, v, in)
	wantMessage(t, "Update(type and stock UoM, used)", err, typeLockedMsg)
	wantMessage(t, "Update(type and stock UoM, used)", err, stockLockedMsg)

	// A blank or unknown type isn't a change of type: Validate's message stands.
	in, v = w.input(t, angle)
	in.Type = ""
	_, err = svc.Update(ctx, kavya, angle, v, in)
	wantOnlyMessage(t, "Update(blank type, used)", err, "Item type is required (Goods or Service).")
	in.Type = "Asset"
	_, err = svc.Update(ctx, kavya, angle, v, in)
	wantOnlyMessage(t, "Update(unknown type, used)", err, "Item type must be Goods or Service.")

	// Other fields stay editable.
	in, v = w.input(t, angle)
	in.Name = "MS Angle 50x50x6"
	if _, err := svc.Update(ctx, kavya, angle, v, in); err != nil {
		t.Errorf("Update(name, used): %v", err)
	}
}

func TestItem_ServiceNotStock(t *testing.T) { // MST-076
	in := serviceInput("997313")
	if _, err := item.Validate(in); err != nil {
		t.Fatalf("Validate(service, not stock): %v", err)
	}
	in.IsStock = true
	_, err := item.Validate(in)
	wantSaveError(t, "Validate(service, stock)", err, doctype.FieldError{Field: "is_stock", Message: "Service items can't be stock items."})
	wantOnlyMessage(t, "Validate(service, stock)", err, "Service items can't be stock items.")
}

func TestUoM_DisabledUoM(t *testing.T) { // MST-093
	const msg = "UoM BAG is disabled. Pick another UoM."
	w := newWorld(t)
	ctx := context.Background()
	svc, kavya := w.svc, w.env.Kavya
	bag, nos, bolt, cement := w.m.UoMs["BAG"], w.m.UoMs["NOS"], w.m.Items["BOLT-M12"], w.m.Items["CEMENT-OPC"]
	uoms := uom.New(w.env.Kit)

	// BOLT-M12 gets a BAG row while BAG is enabled.
	in, v := w.input(t, bolt)
	in.Conversions = append(in.Conversions, item.Conversion{UoMID: bag, Factor: factor(t, "50")})
	if _, err := svc.Update(ctx, kavya, bolt, v, in); err != nil {
		t.Fatalf("Update(add BAG row): %v", err)
	}
	var u uom.UoM
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		u, err = uoms.Get(ctx, tx, bag)
		return err
	}); err != nil {
		t.Fatalf("Get(BAG): %v", err)
	}
	if err := uoms.SetDisabled(ctx, kavya, bag, u.Version, true); err != nil {
		t.Fatalf("disable BAG: %v", err)
	}

	// A new conversion row is refused, on its row, on update and on create.
	in, v = w.input(t, w.m.Items["WIRE-18G"])
	in.Conversions = append(in.Conversions, item.Conversion{UoMID: bag, Factor: factor(t, "50")})
	_, err := svc.Update(ctx, kavya, w.m.Items["WIRE-18G"], v, in)
	wantSaveError(t, "Update(new BAG row)", err, doctype.FieldError{Field: "uom_id", Line: 2, Message: "Row 2: " + msg})
	wantOnlyMessage(t, "Update(new BAG row)", err, "Row 2: "+msg)
	fresh := item.Input{Code: "BOLT-M16", Name: "Hex Bolt M16", Type: goods, HSNSAC: "7318", StockUoMID: nos,
		Conversions: []item.Conversion{{UoMID: bag, Factor: factor(t, "50")}}}
	_, err = svc.Create(ctx, kavya, fresh)
	wantOnlyMessage(t, "Create(BAG row)", err, "Row 1: "+msg)

	// So is a new stock UoM, once, on the stock UoM.
	fresh.StockUoMID, fresh.Conversions = bag, nil
	_, err = svc.Create(ctx, kavya, fresh)
	wantSaveError(t, "Create(stock BAG)", err, doctype.FieldError{Field: "stock_uom_id", Message: msg})
	wantOnlyMessage(t, "Create(stock BAG)", err, msg)
	// A row the item already has doesn't make BAG available as its stock UoM:
	// the change resets the table, so every row is new.
	in, v = w.input(t, bolt)
	in.StockUoMID = bag
	_, err = svc.Update(ctx, kavya, bolt, v, in)
	wantOnlyMessage(t, "Update(stock UoM to its BAG row)", err, msg)

	// Rows the item already has stay: disabling BAG doesn't freeze the items
	// that use it (the case says "new"), stock row or not.
	for _, id := range []int64{bolt, cement} {
		in, v = w.input(t, id)
		in.Name += " (edited)"
		if _, err := svc.Update(ctx, kavya, id, v, in); err != nil {
			t.Errorf("Update(%s, keeps its BAG row): %v", in.Code, err)
		}
	}

	// A draft line is refused in DOC-034's wording, not the text above: CD-38
	// makes DOC the owner of the disabled-link message on a document (as in
	// MST-010), so MST-093's text applies to conversion rows only.
	line := w.line(cement, "BAG", wholeQty)
	_, err = mstfixture.Save(ctx, w.env, kavya, 0, w.shree(), []mstfixture.Line{line})
	wantSaveError(t, "Save(draft line in BAG)", err, doctype.FieldError{Field: "uom_id", Line: 1,
		Message: "Line 1: UoM BAG Bags is disabled. Pick an active uom, or ask an admin to enable it."})

	// Enabled again, it is available again.
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		u, err = uoms.Get(ctx, tx, bag)
		return err
	}); err != nil {
		t.Fatalf("Get(BAG): %v", err)
	}
	if err := uoms.SetDisabled(ctx, kavya, bag, u.Version, false); err != nil {
		t.Fatalf("enable BAG: %v", err)
	}
	if _, err := svc.Create(ctx, kavya, fresh); err != nil {
		t.Errorf("Create(stock BAG, enabled): %v", err)
	}
}

// TestItem_TaxCodeReadFailure has no case ID: a failed tax code read is an
// internal error, not a field message (CD-147).
func TestItem_TaxCodeReadFailure(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	in := item.Input{Code: "BOLT-M16", Name: "Hex Bolt M16", Type: goods, HSNSAC: "7318", StockUoMID: w.m.UoMs["NOS"], DefaultTaxCodeID: w.m.TaxCodes["GST18"]}
	err := w.env.DB.Write(ctx, func(tx *db.Tx) error {
		if _, err := tx.SQL().ExecContext(ctx, `ALTER TABLE tax_code RENAME TO tax_code_gone`); err != nil {
			t.Fatalf("rename tax_code: %v", err)
		}
		_, err := w.svc.CreateTx(ctx, tx, w.env.Kavya, in)
		return err
	})
	var ve *mstkit.ValidationError
	if err == nil || errors.As(err, &ve) {
		t.Errorf("CreateTx with an unreadable tax code: err = %v; want a plain internal error", err)
	}
}

// TestItem_UoMLinksNotFound has no case ID: an id that doesn't exist gets
// CD-170's message on its own field, never the raw foreign key error.
func TestItem_UoMLinksNotFound(t *testing.T) {
	const missing = int64(999999)
	w := newWorld(t)
	ctx := context.Background()
	in := item.Input{Code: "BOLT-M16", Name: "Hex Bolt M16", Type: goods, HSNSAC: "7318", StockUoMID: w.m.UoMs["NOS"]}

	stock := in
	stock.StockUoMID = missing
	_, err := w.svc.Create(ctx, w.env.Kavya, stock)
	wantSaveError(t, "Create(missing stock UoM)", err, doctype.FieldError{Field: "stock_uom_id", Message: "Stock UoM not found. Pick one from the list."})

	purchase := in
	purchase.PurchaseUoMID = missing
	_, err = w.svc.Create(ctx, w.env.Kavya, purchase)
	wantOnlyMessage(t, "Create(missing purchase UoM)", err, "Purchase UoM not found. Pick one from the list.")

	row := in
	row.Conversions = []item.Conversion{{UoMID: missing, Factor: factor(t, "2")}}
	_, err = w.svc.Create(ctx, w.env.Kavya, row)
	wantOnlyMessage(t, "Create(missing row UoM)", err, "Row 1: UoM not found. Pick one from the list.")
}
