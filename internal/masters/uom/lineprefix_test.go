package uom_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstfixture"
	"github.com/vardhan/internaltools/internal/masters/msttest"
)

// TestUoM_LineQtyPrefixOnce has no case ID (CD-264 (8), CD-277 (4)): DOC
// passes a module's Validate errors through unchanged, so CheckLineQty's text,
// saved through the engine by mst.Order, starts with "Line {n}: " exactly once.
func TestUoM_LineQtyPrefixOnce(t *testing.T) {
	env := msttest.New(t)
	w := flagWorld{env: env, m: msttest.SeedMasters(t, env)}
	line := func(code, unit, qty string) mstfixture.Line {
		return mstfixture.Line{ItemID: w.m.Items[code], UoMID: w.m.UoMs[unit], TaxCodeID: w.m.TaxCodes["GST18"], Qty: parseQty(t, qty), Rate: 62_5000}
	}
	_, err := mstfixture.Save(context.Background(), env, env.Kavya, 0, w.header(),
		[]mstfixture.Line{line("BOLT-M12", "NOS", "2"), line("BOLT-M12", "NOS", "1.5")})
	const want = "Line 2: Quantity 1.5 can't be a fraction for UoM NOS. Enter a whole number."
	var he doctype.HTTPError
	if !errors.As(err, &he) || len(he.FieldErrors()) != 1 || he.FieldErrors()[0].Message != want {
		t.Errorf("Save(line 2 BOLT-M12 1.5 NOS): err = %v; want one field error %q", err, want)
		if he != nil {
			t.Logf("field errors: %+v", he.FieldErrors())
		}
	}
}
