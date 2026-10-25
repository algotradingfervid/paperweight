package types

import (
	"reflect"
	"testing"
	"time"
)

func fyDate(y int, m time.Month, d int) Date { return Date{Year: y, Month: m, Day: d} }

func TestFiscalYear_FromDate(t *testing.T) { // NUM-070
	cases := []struct {
		d    Date
		want string
	}{
		{fyDate(2025, time.April, 1), "2025-26"},
		{fyDate(2026, time.January, 15), "2025-26"},
		{fyDate(2026, time.March, 31), "2025-26"},
		{fyDate(2026, time.April, 1), "2026-27"},
	}
	for _, c := range cases {
		if got := FiscalYear(c.d).Label; got != c.want {
			t.Errorf("FiscalYear(%v).Label = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestFiscalYear_Labels(t *testing.T) { // NUM-071
	cases := []struct {
		d    Date
		want FY
	}{
		{fyDate(2026, time.October, 10), FY{Start: fyDate(2026, time.April, 1), End: fyDate(2027, time.March, 31), Label: "2026-27", Short: "26-27"}},
		{fyDate(1999, time.April, 1), FY{Start: fyDate(1999, time.April, 1), End: fyDate(2000, time.March, 31), Label: "1999-00", Short: "99-00"}},
		{fyDate(2099, time.April, 1), FY{Start: fyDate(2099, time.April, 1), End: fyDate(2100, time.March, 31), Label: "2099-00", Short: "99-00"}},
	}
	for _, c := range cases {
		if got := FiscalYear(c.d); !reflect.DeepEqual(got, c.want) {
			t.Errorf("FiscalYear(%v) = %+v, want %+v", c.d, got, c.want)
		}
	}
}

func TestFiscalYear_LeapDays(t *testing.T) { // NUM-072
	cases := []struct {
		d    Date
		want int
	}{
		{fyDate(2023, time.April, 1), 366}, // FY 2023-24 includes 29-02-2024
		{fyDate(2024, time.April, 1), 365}, // FY 2024-25
		{fyDate(2027, time.April, 1), 366}, // FY 2027-28 includes 29-02-2028
	}
	for _, c := range cases {
		if got := FiscalYear(c.d).Days(); got != c.want {
			t.Errorf("FiscalYear(%v).Days() = %d, want %d", c.d, got, c.want)
		}
	}
}
