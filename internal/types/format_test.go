package types

import "testing"

func TestFormat_IndianGrouping(t *testing.T) { // NUM-050
	rupees := []int64{100, 1_000, 10_000, 1_00_000, 10_00_000, 1_00_00_000, 10_00_00_000, 1_00_00_00_000}
	want := []string{"100.00", "1,000.00", "10,000.00", "1,00,000.00", "10,00,000.00", "1,00,00,000.00", "10,00,00,000.00", "1,00,00,00,000.00"}
	for i, r := range rupees {
		if got := FormatMoney(Money(r * 100)); got != want[i] {
			t.Errorf("FormatMoney(%d rupees) = %q, want %q", r, got, want[i])
		}
	}
}

func TestFormat_BriefExamples(t *testing.T) { // NUM-051
	cases := []struct {
		m          Money
		plain, inr string
	}{
		{1411280, "14,112.80", "₹14,112.80"},
		{1411200000, "1,41,12,000.00", "₹1,41,12,000.00"},
	}
	for _, c := range cases {
		if got := FormatMoney(c.m); got != c.plain {
			t.Errorf("FormatMoney(%d) = %q, want %q", c.m, got, c.plain)
		}
		if got := FormatINR(c.m); got != c.inr {
			t.Errorf("FormatINR(%d) = %q, want %q", c.m, got, c.inr)
		}
	}
}

func TestFormat_NegativeAndZero(t *testing.T) { // NUM-052
	money := []struct {
		m    Money
		want string
	}{
		{-10000000, "-1,00,000.00"},
		{-1, "-0.01"},
		{-123450, "-1,234.50"},
		{Rate(-40).ToMoney(), "0.00"}, // −0.004 rupee displayed as money: never "-0.00"
	}
	for _, c := range money {
		if got := FormatMoney(c.m); got != c.want {
			t.Errorf("FormatMoney(%d) = %q, want %q", c.m, got, c.want)
		}
	}
	if got := FormatINR(-123450); got != "-₹1,234.50" {
		t.Errorf("FormatINR(-123450) = %q, want %q", got, "-₹1,234.50")
	}
}

func TestFormat_Qty(t *testing.T) { // NUM-053
	qty := []struct {
		q    Qty
		want string
	}{
		{2500, "2.5"},
		{100000000, "1,00,000"},
		{1, "0.001"},
		{12340, "12.34"},
		{1200000, "1,200"}, // reference PO line 1
	}
	for _, c := range qty {
		if got := FormatQty(c.q); got != c.want {
			t.Errorf("FormatQty(%d) = %q, want %q", c.q, got, c.want)
		}
	}
	withUoM := []struct {
		q         Qty
		uom, want string
	}{
		{2500, "kg", "2.5 kg"},
		{1200000, "kg", "1,200 kg"}, // reference PO line 1
	}
	for _, c := range withUoM {
		if got := FormatQtyUoM(c.q, c.uom); got != c.want {
			t.Errorf("FormatQtyUoM(%d, %q) = %q, want %q", c.q, c.uom, got, c.want)
		}
	}
}

func TestFormat_Rate(t *testing.T) { // NUM-054
	cases := []struct {
		r    Rate
		want string
	}{
		{123450, "12.345"},
		{125000, "12.50"},
		{1234567890, "1,23,456.789"},
		{1234, "0.1234"},
	}
	for _, c := range cases {
		if got := FormatRate(c.r); got != c.want {
			t.Errorf("FormatRate(%d) = %q, want %q", c.r, got, c.want)
		}
	}
}

func TestFormat_Percent(t *testing.T) { // NUM-055
	cases := []struct {
		p    Percent
		want string
	}{
		{18000, "18%"},
		{250, "0.25%"},
		{125, "0.125%"},
		{1500, "1.5%"},
		{0, "0%"},
	}
	for _, c := range cases {
		if got := FormatPercent(c.p); got != c.want {
			t.Errorf("FormatPercent(%d) = %q, want %q", c.p, got, c.want)
		}
	}
}
