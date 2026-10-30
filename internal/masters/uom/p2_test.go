package uom_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstfixture"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

const nbsp = "\u00a0"

func parseQty(t *testing.T, s string) types.Qty {
	t.Helper()
	q, err := types.ParseQty(s)
	if err != nil {
		t.Fatalf("types.ParseQty(%q): %v", s, err)
	}
	return q
}

func parseFactor(t *testing.T, s string) types.Factor {
	t.Helper()
	f, err := uom.ParseFactor(s)
	if err != nil {
		t.Fatalf("uom.ParseFactor(%q): %v", s, err)
	}
	return f
}

func TestUoM_SeedUQC(t *testing.T) { // MST-078
	env := msttest.New(t)
	ctx := context.Background()
	svc := uom.New(env.Kit)

	// The seed: exactly the eleven rows, enabled.
	if n := countUoMs(t, env.DB); n != len(seeded) {
		t.Errorf("seeded uom rows = %d; want %d", n, len(seeded))
	}
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		for _, s := range seeded {
			u, ok, err := svc.ByCode(ctx, tx, s.code)
			if err != nil {
				return err
			}
			if !ok {
				t.Errorf("ByCode(%s): not found; want the seeded UoM", s.code)
				continue
			}
			if u.Code != s.code || u.Name != s.name || u.UQC != s.uqc || u.WholeNumber != s.whole || u.Disabled {
				t.Errorf("seeded %s = {Code %q, Name %q, UQC %q, WholeNumber %v, Disabled %v}; want {%q, %q, %q, %v, false}",
					s.code, u.Code, u.Name, u.UQC, u.WholeNumber, u.Disabled, s.code, s.name, s.uqc, s.whole)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("read seed: %v", err)
	}
}

func TestItemUoM_FactorPrecision(t *testing.T) { // MST-085
	env := msttest.New(t)
	ctx := context.Background()
	gram, err := uom.New(env.Kit).Create(ctx, env.Kavya, uom.Input{Code: "GRAM", Name: "Grams", UQC: "GMS"})
	if err != nil {
		t.Fatalf("Create(GRAM): %v", err)
	}
	var kgs uom.UoM
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		kgs, _, err = uom.New(env.Kit).ByCode(ctx, tx, "KGS")
		return err
	}); err != nil {
		t.Fatalf("ByCode(KGS): %v", err)
	}
	items := item.New(env.Kit)
	saved, err := items.Create(ctx, env.Kavya, item.Input{
		Code: "WIRE-18G", Name: "Binding Wire 18G", Type: item.TypeGoods, HSNSAC: "7217", StockUoMID: kgs.ID, IsStock: true,
		Conversions: []item.Conversion{{UoMID: gram.ID, Factor: parseFactor(t, "0.001")}},
	})
	if err != nil {
		t.Fatalf("Create(WIRE-18G, row (GRAM, 0.001)): %v", err)
	}
	var stored types.Factor
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		stored, _, err = items.Factor(ctx, tx, saved.ID, gram.ID)
		return err
	}); err != nil {
		t.Fatalf("Factor(WIRE-18G, GRAM): %v", err)
	}
	if got, want := uom.ToStock(parseQty(t, "1234"), stored), parseQty(t, "1.234"); got != want {
		t.Errorf("ToStock(1234 GRAM, stored factor) = %s KGS; want %s", types.FormatQty(got), types.FormatQty(want))
	}

	const msg = "Conversion factor can have at most 6 decimal places."
	if _, err := uom.ParseFactor("0.0000001"); err == nil || err.Error() != msg {
		t.Errorf("ParseFactor(0.0000001) err = %v; want %q", err, msg)
	}
}

// TestUoM_ParseFactorDecimals has no case ID (MST Task 8 review): ParseFactor
// counts decimal places itself, with trailing zeros not counted (as NUM does),
// and every other refusal keeps NUM's message.
func TestUoM_ParseFactorDecimals(t *testing.T) {
	const decimals = "Conversion factor can have at most 6 decimal places."
	for _, c := range []struct{ in, want string }{
		{"0.0000001", decimals},
		{".1234567", decimals},
		{"1.1234567890", decimals}, // past NUM's own 9-decimal limit
		{" 1.1234567 ", decimals},
		{nbsp + "1.1234567" + nbsp, decimals}, // NUM's blanks
		{"abc.1234567", `"abc.1234567" is not a valid number. Write it like 1234.50.`},
		{"abc.1234567890", `"abc.1234567890" is not a valid number. Write it like 1234.50.`},
		{"1.1234567x", `"1.1234567x" is not a valid number. Write it like 1234.50.`},
		{"1.1234567\n", "\"1.1234567\n\" is not a valid number. Write it like 1234.50."}, // NUM doesn't trim a newline
		{"-1.1234567", "Conversion factor must be more than 0."},
		{"-1.1234567890", "Conversion factor must be more than 0."},
		{"2000000.1234567", "Conversion factor is too large. The maximum is 10,00,000."},
		{"1000000.0000000001", "Conversion factor is too large. The maximum is 10,00,000."},
		{"10,00,000.0000000001", "Conversion factor is too large. The maximum is 10,00,000."},
		{"0", "Conversion factor must be more than 0."},
	} {
		if _, err := uom.ParseFactor(c.in); err == nil || err.Error() != c.want {
			t.Errorf("ParseFactor(%q) err = %v; want %q", c.in, err, c.want)
		}
	}
	for in, want := range map[string]types.Factor{
		"0.000001":     1_000,
		"1.5":          1_500_000_000,
		"1.5000000":    1_500_000_000,
		"2.1234560000": 2_123_456_000,
	} {
		if got, err := uom.ParseFactor(in); err != nil || got != want {
			t.Errorf("ParseFactor(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
}

// TestUoM_CheckFactorParity has no case ID (MST Task 10 review): CheckFactor
// keeps its own copies of the parsers' messages, so each refusal must read the
// same as the parser's for the same value.
func TestUoM_CheckFactorParity(t *testing.T) {
	for _, c := range []struct {
		in string
		f  types.Factor
	}{
		{"0", 0},
		{"-5", -5 * uom.FactorOne},
		{"1000001", types.MaxFactor + uom.FactorOne},
		{"0.0000001", 100},
	} {
		_, parsed := uom.ParseFactor(c.in)
		checked := uom.CheckFactor(c.f)
		if parsed == nil || checked == nil || parsed.Error() != checked.Error() {
			t.Errorf("value %s: ParseFactor err = %v, CheckFactor err = %v; want the same message", c.in, parsed, checked)
		}
	}
	// Both accept the boundaries.
	for in, f := range map[string]types.Factor{"1000000": types.MaxFactor, "0.000001": 1_000} {
		got, err := uom.ParseFactor(in)
		if err != nil || got != f {
			t.Errorf("ParseFactor(%s) = %d, %v; want %d", in, got, err, f)
		}
		if err := uom.CheckFactor(f); err != nil {
			t.Errorf("CheckFactor(%s): %v; want nil", in, err)
		}
	}
}

// TestUoM_RateToStockOutOfRange has no case ID (MST Task 8 review): it pins the
// documented result outside RateToStock's bounds.
func TestUoM_RateToStockOutOfRange(t *testing.T) {
	if got := uom.RateToStock(types.Rate(1<<62), 1_000); got != 0 {
		t.Errorf("RateToStock(overflow) = %d; want 0", got)
	}
	if got := uom.RateToStock(types.MaxRate, 0); got != 0 {
		t.Errorf("RateToStock(factor 0) = %d; want 0", got)
	}
	// The largest rate at the smallest factor still fits.
	if got, want := uom.RateToStock(types.MaxRate, 1_000), types.MaxRate*1_000_000; got != want {
		t.Errorf("RateToStock(MaxRate, 0.000001) = %d; want %d", got, want)
	}
}

func TestUoM_WholeNumberToleratesDust(t *testing.T) { // MST-088
	nos, kgs := uom.UoM{Code: "NOS", WholeNumber: true}, uom.UoM{Code: "KGS"}
	f := parseFactor(t, "0.6")
	if fe := uom.CheckLineQty(1, parseQty(t, "3333.333"), kgs, nos, f); fe != nil {
		t.Errorf("CheckLineQty(3333.333 KGS) = %+v; want nil (1999.9998 rounds to 2000 NOS)", fe)
	}
	want := doctype.FieldError{Field: "qty", Line: 1,
		Message: "Row 1: 3,333.9 KGS = 2,000.34 NOS, but NOS must be a whole number. Change the quantity."}
	if fe := uom.CheckLineQty(1, parseQty(t, "3333.9"), kgs, nos, f); fe == nil || *fe != want {
		t.Errorf("CheckLineQty(3333.9 KGS) = %+v; want %+v", fe, want)
	}
}

// flagWorld is a seeded Env for the whole-number flag.
type flagWorld struct {
	env *msttest.Env
	m   msttest.Masters
	svc *uom.Service
}

func (w flagWorld) header() mstfixture.Header {
	return mstfixture.Header{
		VendorID: w.m.Parties["SHREE"], AddressID: w.m.Addresses["Mumbai"], BranchID: w.m.Branches["PUNE"],
		Date: types.Date{Year: 2026, Month: time.October, Day: 24},
	}
}

// order saves a new one-line order.
func (w flagWorld) order(t *testing.T, code, unit string, qty types.Qty) int64 {
	t.Helper()
	res, err := mstfixture.Save(context.Background(), w.env, w.env.Kavya, 0, w.header(), []mstfixture.Line{{
		ItemID: w.m.Items[code], UoMID: w.m.UoMs[unit], TaxCodeID: w.m.TaxCodes["GST18"], Qty: qty, Rate: 62_5000,
	}})
	if err != nil {
		t.Fatalf("Save(%s %s): %v", code, unit, err)
	}
	return res.ID
}

func (w flagWorld) get(t *testing.T, code string) uom.UoM {
	t.Helper()
	ctx := context.Background()
	var u uom.UoM
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		u, _, err = w.svc.ByCode(ctx, tx, code)
		return err
	}); err != nil {
		t.Fatalf("ByCode(%s): %v", code, err)
	}
	return u
}

// setWhole saves the UoM with the flag at whole.
func (w flagWorld) setWhole(t *testing.T, code string, whole bool) error {
	t.Helper()
	u := w.get(t, code)
	_, err := w.svc.Update(context.Background(), w.env.Kavya, u.ID, u.Version,
		uom.Input{Code: u.Code, Name: u.Name, UQC: u.UQC, WholeNumber: whole})
	return err
}

func TestUoM_WholeNumberFlagChange(t *testing.T) { // MST-092
	env := msttest.New(t)
	w := flagWorld{env: env, m: msttest.SeedMasters(t, env), svc: uom.New(env.Kit)}

	// A draft doesn't stop the change; its next save is refused instead.
	id := w.order(t, "CABLE-4C", "MTR", parseQty(t, "12.5"))
	if err := w.setWhole(t, "MTR", true); err != nil {
		t.Fatalf("turn whole on with only a draft: %v", err)
	}
	line := mstfixture.Line{ItemID: w.m.Items["CABLE-4C"], UoMID: w.m.UoMs["MTR"], TaxCodeID: w.m.TaxCodes["GST18"], Qty: parseQty(t, "12.5"), Rate: 62_5000}
	_, err := mstfixture.Save(context.Background(), env, env.Kavya, id, w.header(), []mstfixture.Line{line})
	var he doctype.HTTPError
	if !errors.As(err, &he) || len(he.FieldErrors()) != 1 || he.FieldErrors()[0].Message != "Row 1: Quantity 12.5 can't be a fraction for UoM MTR. Enter a whole number." {
		t.Errorf("resave of the draft: err = %v; want the MST-086 message", err)
	}
	if err := w.setWhole(t, "MTR", false); err != nil {
		t.Fatalf("turn whole off: %v", err)
	}

	number := mstfixture.Approve(t, env, id)
	if number != "PO/26-27/00001" {
		t.Errorf("order number = %q; want PO/26-27/00001", number)
	}
	want := doctype.FieldError{Field: "whole_number",
		Message: "MTR is used with fractional quantities (e.g. PO/26-27/00001). Create a new UoM instead."}
	check := func(what string) {
		t.Helper()
		before := w.get(t, "MTR")
		err := w.setWhole(t, "MTR", true)
		var ve *mstkit.ValidationError
		if !errors.As(err, &ve) || len(ve.Fields) != 1 || ve.Fields[0] != want {
			t.Fatalf("%s: turn whole on: err = %v; want %+v", what, err, want)
		}
		if after := w.get(t, "MTR"); after.WholeNumber || after.Version != before.Version {
			t.Errorf("%s: MTR after the refusal = %+v; want unchanged %+v", what, after, before)
		}
	}
	check("approved order")

	// A stale page gets the conflict, not the MST-092 refusal.
	stale := w.get(t, "MTR")
	const staleMsg = "Someone else changed UoM MTR after you opened it. Reload it and make your changes again."
	_, err = w.svc.Update(context.Background(), env.Kavya, stale.ID, stale.Version-1,
		uom.Input{Code: stale.Code, Name: stale.Name, UQC: stale.UQC, WholeNumber: true})
	if !errors.As(err, &he) || he.Status() != http.StatusConflict || err.Error() != staleMsg {
		t.Errorf("stale turn whole on: err = %v; want 409 %q", err, staleMsg)
	}

	mstfixture.Cancel(t, env, id)
	check("cancelled order")

	// The refusal is only about turning the flag on, for that UoM, with a
	// fractional quantity: other edits save, and so do other UoMs.
	mtr := w.get(t, "MTR")
	if _, err := w.svc.Update(context.Background(), env.Kavya, mtr.ID, mtr.Version,
		uom.Input{Code: mtr.Code, Name: "Metres", UQC: mtr.UQC}); err != nil {
		t.Errorf("rename MTR: %v", err)
	}
	mstfixture.Approve(t, env, w.order(t, "MS-ANGLE-50", "KGS", parseQty(t, "12")))
	if err := w.setWhole(t, "KGS", true); err != nil {
		t.Errorf("turn whole on for KGS, used with 12: %v", err)
	}
	if err := w.setWhole(t, "LTR", true); err != nil {
		t.Errorf("turn whole on for unused LTR: %v", err)
	}

	// A UoM already whole has nothing to turn on, so a fractional line stored
	// before can't freeze it.
	if err := env.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), `UPDATE uom SET whole_number = 1 WHERE code = 'MTR'`)
		return err
	}); err != nil {
		t.Fatalf("seed whole MTR: %v", err)
	}
	mtr = w.get(t, "MTR")
	if _, err := w.svc.Update(context.Background(), env.Kavya, mtr.ID, mtr.Version,
		uom.Input{Code: mtr.Code, Name: "Meters", UQC: mtr.UQC, WholeNumber: true}); err != nil {
		t.Errorf("rename whole MTR with a stored fractional line: %v", err)
	}
}
