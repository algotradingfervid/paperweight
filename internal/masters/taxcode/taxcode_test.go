package taxcode_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Unit tests use the literal 06-masters.md values (MST-094…MST-098).

const (
	taxableZeroMsg = "A Taxable tax code needs a GST rate above 0. Use Nil-Rated or Exempted for 0%."
	notifiedTail   = "% is not a notified rate. Use one of 0.1, 0.25, 1, 1.5, 3, 5, 6, 7.5, 12, 18, 28, 40."
)

// pct parses a percentage through NUM's parser (CD-33).
func pct(t *testing.T, s string) types.Percent {
	t.Helper()
	p, err := types.ParsePercent(s)
	if err != nil {
		t.Fatalf("ParsePercent(%q): %v", s, err)
	}
	return p
}

// wantOneError checks that err is a 422 *mstkit.ValidationError holding
// exactly one header error on field with message msg.
func wantOneError(t *testing.T, what string, err error, field, msg string) {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("%s: err = %v (%T); want *mstkit.ValidationError %q", what, err, err, msg)
		return
	}
	if ve.Status() != 422 {
		t.Errorf("%s: Status() = %d; want 422", what, ve.Status())
	}
	want := doctype.FieldError{Field: field, Message: msg}
	if got := ve.FieldErrors(); len(got) != 1 || got[0] != want {
		t.Errorf("%s: FieldErrors() = %+v; want [%+v]", what, got, want)
	}
}

func TestTaxCode_TaxableNeedsRate(t *testing.T) { // MST-094
	_, err := taxcode.Validate(taxcode.Input{Code: "GST0T", Name: "GST 0% Taxable", Treatment: taxcode.Taxable, Rate: 0})
	wantOneError(t, "Validate(GST0T Taxable 0)", err, "rate", taxableZeroMsg)
}

func TestTaxCode_NonTaxableZero(t *testing.T) { // MST-095
	for _, tr := range []taxcode.Treatment{taxcode.Exempted, taxcode.NilRated, taxcode.NonGST, taxcode.ZeroRated} {
		out, err := taxcode.Validate(taxcode.Input{Code: "T5", Name: "Treatment at 5%", Treatment: tr, Rate: pct(t, "5")})
		if err != nil {
			t.Errorf("Validate(%s, 5%%): %v; want nil", tr, err)
			continue
		}
		if out.Rate != 0 {
			t.Errorf("Validate(%s, 5%%).Rate = %s; want 0%%", tr, types.FormatPercent(out.Rate))
		}
		if out.Treatment != tr {
			t.Errorf("Validate(%s, 5%%).Treatment = %q; want %q", tr, out.Treatment, tr)
		}
	}

	// The treatment values are the five 06 names (D-MST-16).
	for tr, want := range map[taxcode.Treatment]string{
		taxcode.Taxable: "Taxable", taxcode.NilRated: "Nil-Rated", taxcode.Exempted: "Exempted",
		taxcode.NonGST: "Non-GST", taxcode.ZeroRated: "Zero-Rated",
	} {
		if string(tr) != want {
			t.Errorf("Treatment %q; want %q", tr, want)
		}
	}
}

func TestTaxCode_AllowedRates(t *testing.T) { // MST-096
	var want []types.Percent
	for _, s := range []string{"0", "0.1", "0.25", "1", "1.5", "3", "5", "6", "7.5", "12", "18", "28", "40"} {
		want = append(want, pct(t, s))
	}
	if got := taxcode.AllowedRates(); !reflect.DeepEqual(got, want) {
		t.Errorf("AllowedRates() = %v; want %v", got, want)
	}

	// The seeded rates, then the allowed but unseeded ones.
	for _, s := range []string{"0.25", "3", "5", "12", "18", "28", "40", "0.1", "1", "1.5", "6", "7.5"} {
		r := pct(t, s)
		out, err := taxcode.Validate(taxcode.Input{Code: "GSTX", Name: "GST " + s + "%", Treatment: taxcode.Taxable, Rate: r})
		if err != nil {
			t.Errorf("Validate(Taxable %s%%): %v; want nil", s, err)
			continue
		}
		if out.Rate != r || out.Treatment != taxcode.Taxable {
			t.Errorf("Validate(Taxable %s%%) = {Treatment %q, Rate %s}; want {Taxable, %s%%}", s, out.Treatment, types.FormatPercent(out.Rate), s)
		}
	}

	for _, c := range []struct {
		shown string
		rate  types.Percent
	}{
		{"10", 10_000},
		{"110", 110_000},
		{"-5", -5_000},
		{"18.5", 18_500},
	} {
		_, err := taxcode.Validate(taxcode.Input{Code: "GSTX", Name: "GST " + c.shown + "%", Treatment: taxcode.Taxable, Rate: c.rate})
		wantOneError(t, "Validate(Taxable "+c.shown+"%)", err, "rate", "GST rate "+c.shown+notifiedTail)
	}
}

func TestTaxCode_Split(t *testing.T) { // MST-098
	for _, c := range []struct {
		code             string
		rate             string
		cgst, sgst, igst string
	}{
		{"GST18", "18", "9", "9", "18"},
		{"GST0.25", "0.25", "0.125", "0.125", "0.25"},
		{"GST3", "3", "1.5", "1.5", "3"},
	} {
		cgst, sgst, igst := taxcode.Split(pct(t, c.rate))
		if cgst != pct(t, c.cgst) || sgst != pct(t, c.sgst) || igst != pct(t, c.igst) {
			t.Errorf("%s: Split(%s%%) = CGST %s, SGST %s, IGST %s; want %s%%, %s%%, %s%%", c.code, c.rate,
				types.FormatPercent(cgst), types.FormatPercent(sgst), types.FormatPercent(igst), c.cgst, c.sgst, c.igst)
		}
	}

	// Every allowed rate halves exactly in thousandths of a percent (Task 9 Step 3).
	for _, r := range taxcode.AllowedRates() {
		cgst, sgst, igst := taxcode.Split(r)
		if r%2 != 0 || cgst != r/2 || sgst != r/2 || cgst+sgst != r || igst != r {
			t.Errorf("Split(%s) = %d, %d, %d; want %d, %d, %d (rate %d even)", types.FormatPercent(r), cgst, sgst, igst, r/2, r/2, r, r)
		}
	}
}

func TestTaxCode_Seed(t *testing.T) { // MST-097
	env := msttest.New(t)
	ctx := context.Background()
	svc := taxcode.New(env.Kit)

	seeded := []struct {
		code      string
		treatment taxcode.Treatment
		rate      string
	}{
		{"GST0.25", taxcode.Taxable, "0.25"},
		{"GST3", taxcode.Taxable, "3"},
		{"GST5", taxcode.Taxable, "5"},
		{"GST12", taxcode.Taxable, "12"},
		{"GST18", taxcode.Taxable, "18"},
		{"GST28", taxcode.Taxable, "28"},
		{"GST40", taxcode.Taxable, "40"},
		{"NIL", taxcode.NilRated, "0"},
		{"EXEMPT", taxcode.Exempted, "0"},
		{"NONGST", taxcode.NonGST, "0"},
	}

	var wantCodes []string
	for _, s := range seeded {
		wantCodes = append(wantCodes, s.code)
	}
	sort.Strings(wantCodes)

	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		// Exactly these ten codes.
		rows, err := tx.SQL().QueryContext(ctx, `SELECT code FROM tax_code ORDER BY code`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		var gotCodes []string
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				return err
			}
			gotCodes = append(gotCodes, c)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if !reflect.DeepEqual(gotCodes, wantCodes) {
			t.Errorf("tax_code codes = %v; want %v", gotCodes, wantCodes)
		}

		// Each with its treatment and rate, enabled.
		for _, s := range seeded {
			tc, ok, err := svc.ByCode(ctx, tx, s.code)
			if err != nil {
				return err
			}
			if !ok {
				t.Errorf("ByCode(%s): not found; want the seeded tax code", s.code)
				continue
			}
			want := pct(t, s.rate)
			if tc.Code != s.code || tc.Treatment != s.treatment || tc.Rate != want || tc.Disabled || tc.Name == "" {
				t.Errorf("seeded %s = {Code %q, Name %q, Treatment %q, Rate %s, Disabled %v}; want {%q, non-empty, %q, %s%%, false}",
					s.code, tc.Code, tc.Name, tc.Treatment, types.FormatPercent(tc.Rate), tc.Disabled, s.code, s.treatment, s.rate)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("read seed: %v", err)
	}
}

func TestTaxCode_TreatmentMustBeKnown(t *testing.T) {
	const treatmentMsg = "Treatment must be Taxable, Nil-Rated, Exempted, Non-GST or Zero-Rated."
	for _, tr := range []taxcode.Treatment{"Done", ""} {
		for _, rate := range []string{"18", "0"} {
			_, err := taxcode.Validate(taxcode.Input{Code: "GSTX", Name: "GST " + rate + "%", Treatment: tr, Rate: pct(t, rate)})
			wantOneError(t, "Validate(Treatment \""+string(tr)+"\", "+rate+"%)", err, "treatment", treatmentMsg)
		}
	}

	for _, c := range []struct {
		treatment taxcode.Treatment
		rate      string
	}{
		{taxcode.Taxable, "18"},
		{taxcode.NilRated, "0"},
		{taxcode.Exempted, "0"},
		{taxcode.NonGST, "0"},
		{taxcode.ZeroRated, "0"},
	} {
		out, err := taxcode.Validate(taxcode.Input{Code: "GSTX", Name: "GST " + c.rate + "%", Treatment: c.treatment, Rate: pct(t, c.rate)})
		if err != nil {
			t.Errorf("Validate(%s, %s%%): %v; want nil", c.treatment, c.rate, err)
			continue
		}
		if out.Treatment != c.treatment {
			t.Errorf("Validate(%s, %s%%).Treatment = %q; want %q", c.treatment, c.rate, out.Treatment, c.treatment)
		}
	}
}
