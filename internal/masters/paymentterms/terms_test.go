package paymentterms

import (
	"errors"
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
)

// Unit tests use the literal 06-masters.md values (MST-124…MST-128).

// pct parses a percentage through NUM's parser (CD-33).
func pct(t *testing.T, s string) types.Percent {
	t.Helper()
	p, err := types.ParsePercent(s)
	if err != nil {
		t.Fatalf("ParsePercent(%q): %v", s, err)
	}
	return p
}

// money parses an amount through NUM's parser (CD-33).
func money(t *testing.T, s string) types.Money {
	t.Helper()
	m, err := types.ParseMoney(s)
	if err != nil {
		t.Fatalf("ParseMoney(%q): %v", s, err)
	}
	return m
}

// date reads a stored-form date.
func date(t *testing.T, s string) types.Date {
	t.Helper()
	d, err := types.DateFromISO(s)
	if err != nil {
		t.Fatalf("DateFromISO(%q): %v", s, err)
	}
	return d
}

// row builds a Row from a percent text and days.
func row(t *testing.T, portion string, days int) Row {
	t.Helper()
	return Row{Portion: pct(t, portion), Days: days}
}

// wantMsg checks that err is a 422 error whose text is exactly msg.
func wantMsg(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil; want %q", msg)
	}
	if err.Error() != msg {
		t.Errorf("err = %q; want %q", err.Error(), msg)
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != 422 {
		t.Errorf("err %T is not a 422 doctype.HTTPError", err)
	}
}

func TestPaymentTerms_Total100(t *testing.T) {
	wantMsg(t, ValidateRows([]Row{row(t, "50", 30)}),
		"Payment portions add up to 50%. They must total 100%.")

	if err := ValidateRows([]Row{row(t, "50", 0), row(t, "50", 30)}); err != nil {
		t.Errorf("[50@0, 50@30]: %v; want nil", err)
	}
	if err := ValidateRows([]Row{row(t, "33.33", 0), row(t, "33.33", 30), row(t, "33.34", 60)}); err != nil {
		t.Errorf("[33.33@0, 33.33@30, 33.34@60]: %v; want nil", err)
	}

	wantMsg(t, ValidateRows([]Row{row(t, "33.33", 0), row(t, "33.33", 30), row(t, "33.33", 60)}),
		"Payment portions add up to 99.99%. They must total 100%.")
}

func TestPaymentTerms_DaysNonNegative(t *testing.T) {
	const msg = "Row 1: Days must be a whole number, 0 or more."
	for _, days := range []string{"-30", "7.5"} {
		t.Run(days, func(t *testing.T) {
			rows, err := ParseRows([]RawRow{{Portion: "100", Days: days}})
			wantMsg(t, err, msg)
			if rows != nil {
				t.Errorf("rows = %+v; want nil on error", rows)
			}
		})
	}

	rows, err := ParseRows([]RawRow{{Portion: "50", Days: "0"}, {Portion: "50", Days: "30"}})
	if err != nil {
		t.Fatalf("ParseRows([50@0, 50@30]): %v", err)
	}
	want := []Row{row(t, "50", 0), row(t, "50", 30)}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("ParseRows = %+v; want %+v", rows, want)
	}
}

func TestPaymentTerms_NoDuplicateRows(t *testing.T) {
	wantMsg(t, ValidateRows([]Row{row(t, "50", 30), row(t, "50", 30)}),
		"Row 2 repeats row 1 (30 days). Merge them into one row of 100%.")
}

func TestPaymentTerms_PortionPositive(t *testing.T) {
	wantMsg(t, ValidateRows([]Row{row(t, "0", 0), row(t, "100", 30)}),
		"Row 1: Portion must be more than 0%.")
}

func TestPaymentTerms_DueDates(t *testing.T) {
	got := Schedule([]Row{row(t, "50", 0), row(t, "50", 30)},
		date(t, "2026-01-31"), money(t, "₹1,18,000.00"))
	want := []Due{
		{Date: date(t, "2026-01-31"), Amount: money(t, "₹59,000.00")},
		{Date: date(t, "2026-03-02"), Amount: money(t, "₹59,000.00")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Schedule([50@0, 50@30], 2026-01-31, ₹1,18,000.00) = %+v; want %+v", got, want)
	}

	// Leap year: 2024-02-15 + 30 days.
	total := money(t, "₹1,18,000.00")
	got = Schedule([]Row{row(t, "100", 30)}, date(t, "2024-02-15"), total)
	want = []Due{{Date: date(t, "2024-03-16"), Amount: total}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Schedule([100@30], 2024-02-15) = %+v; want %+v", got, want)
	}
}
