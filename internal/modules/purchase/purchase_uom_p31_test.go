package purchase_test

import (
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-PO P31 (CD-337 (3)): a blank unit on a PO or PR line defaults to the
// item's purchase UoM, which the Item master sets to the stock UoM when none
// is chosen. A unit the user chose is kept, and so is a line's unit when its
// item changes (AD-13 ruling): a new item without that unit is refused.

// p31PurchaseBox makes Box BOLT-M12's purchase UoM through the item master,
// keeping its conversions (1 Box = 100 Nos).
func p31PurchaseBox(t *testing.T, env *purchasetest.Env) {
	t.Helper()
	ctx := t.Context()
	items := item.New(p2Kit(env))
	var it item.Item
	var convs []item.Conversion
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		if it, err = items.Get(ctx, tx, p2Int(t, masterID(t, env.DB, "item", "BOLT-M12"))); err != nil {
			return err
		}
		convs, err = items.Conversions(ctx, tx, it.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := items.Update(ctx, env.Admin, it.ID, it.Version, item.Input{Code: it.Code, Name: it.Name, Type: it.Type, HSNSAC: it.HSNSAC,
		StockUoMID: it.StockUoMID, PurchaseUoMID: p2Int(t, masterID(t, env.DB, "uom", "BOX")), DefaultTaxCodeID: it.DefaultTaxCodeID,
		IsStock: it.IsStock, StandardRate: it.StandardRate, Conversions: convs}); err != nil {
		t.Fatalf("make Box BOLT-M12's purchase UoM: %v", err)
	}
}

// p31OrderLine is a PO line of 2 BOLT-M12 at ₹1,200 in unit ("" posts it blank).
func p31OrderLine(t *testing.T, env *purchasetest.Env, unit string) map[string]string {
	t.Helper()
	l := p2Line(t, env, "BOLT-M12", "NOS", "2", "1200")
	l["uom_id"] = ""
	if unit != "" {
		l["uom_id"] = masterID(t, env.DB, "uom", unit)
	}
	return l
}

// p31Qty is a quantity literal as stored.
func p31Qty(t *testing.T, s string) string {
	t.Helper()
	q, err := types.ParseQty(s)
	if err != nil {
		t.Fatal(err)
	}
	return strconv.FormatInt(int64(q), 10)
}

func TestPO_BlankUoMDefaults(t *testing.T) {
	cases := []struct {
		name             string
		purchaseBox      bool
		posted, want     string
		factor, stockQty string
	}{
		{"purchase UoM set, blank unit", true, "", "BOX", "100", "200"},
		{"no purchase UoM, blank unit", false, "", "NOS", "1", "2"},
		{"purchase UoM set, unit chosen", true, "NOS", "NOS", "1", "2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := purchasetest.New(t)
			if c.purchaseBox {
				p31PurchaseBox(t, env)
			}
			res := p2Must(t, c.name)(p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"),
				[]map[string]string{p31OrderLine(t, env, c.posted)}))
			got := p2LineSnap(t, env, res.ID, "uom_id", "snap_uom_code", "snap_factor", "snap_stock_qty")
			want := map[string]string{"uom_id": masterID(t, env.DB, "uom", c.want), "snap_uom_code": c.want,
				"snap_factor": strconv.FormatInt(p2Int(t, c.factor)*int64(uom.FactorOne), 10), "snap_stock_qty": p31Qty(t, c.stockQty)}
			if len(got) != 1 || !mapsEqual(got[0], want) {
				t.Errorf("lines = %v, want one line %v", got, want)
			}
		})
	}
}

func TestRequest_BlankUoMDefaults(t *testing.T) {
	cases := []struct {
		name         string
		purchaseBox  bool
		posted, want string
	}{
		{"purchase UoM set, blank unit", true, "", "BOX"},
		{"no purchase UoM, blank unit", false, "", "NOS"},
		{"purchase UoM set, unit chosen", true, "NOS", "NOS"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := purchasetest.New(t)
			if c.purchaseBox {
				p31PurchaseBox(t, env)
			}
			res := p5Must(t, c.name)(p5Create(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"),
				[]map[string]string{p5Line(t, env, "BOLT-M12", c.posted, "2")}))
			lines := p5Load(t, env, purchase.RequestDocType, res.ID).Lines
			if len(lines) != 1 || p5Plain(lines[0].Values["uom_id"]) != masterID(t, env.DB, "uom", c.want) {
				t.Errorf("lines = %+v, want one line in %s", lines, c.want)
			}
		})
	}
}

// A draft line whose item changes keeps its unit, even one defaulted from the
// old item's purchase UoM; the new item has no Box conversion, so the save is
// refused.
func TestPO_ItemChangeKeepsUoM(t *testing.T) {
	env := purchasetest.New(t)
	p31PurchaseBox(t, env)
	res := p2Must(t, "blank-unit draft")(p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"),
		[]map[string]string{p31OrderLine(t, env, "")}))
	line := p2Load(t, env, res.ID).Lines[0]
	_, err := p2Save(t, env, env.Priya, res.ID, nil, []map[string]string{{
		"id": strconv.FormatInt(line.ID, 10), "item_id": masterID(t, env.DB, "item", "MS-ANGLE-50"),
		"uom_id": p2Plain(line.Values["uom_id"]), "qty": "2", "price": "1200", "tax_code_id": masterID(t, env.DB, "tax_code", "GST18"),
	}})
	p2WantInvalid(t, "BOLT-M12 line changed to MS-ANGLE-50", err, doctype.FieldError{Field: "uom_id", Line: 1,
		Message: "Line 1: Item MS-ANGLE-50 has no conversion for UoM BOX. Pick another UoM."})
}
