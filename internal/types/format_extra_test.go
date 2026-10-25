package types

import (
	"math"
	"testing"
)

func TestFormat_FactorAndCount(t *testing.T) { // Task 2: FormatFactor, FormatCount
	factors := []struct {
		f    Factor
		want string
	}{
		{1_500_000_000, "1.5"},
		{1, "0.000000001"},
		{0, "0"},
		{-2_000_000_000, "-2"},
		{123456789123456789, "12,34,56,789.123456789"},
	}
	for _, c := range factors {
		if got := FormatFactor(c.f); got != c.want {
			t.Errorf("FormatFactor(%d) = %q, want %q", c.f, got, c.want)
		}
	}
	counts := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{6200, "6,200"},
		{50000, "50,000"},
		{-1234567, "-12,34,567"},
	}
	for _, c := range counts {
		if got := FormatCount(c.n); got != c.want {
			t.Errorf("FormatCount(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestFormat_MinInt64(t *testing.T) { // |MinInt64| must not overflow
	if got, want := FormatMoney(math.MinInt64), "-92,23,37,20,36,85,47,758.08"; got != want {
		t.Errorf("FormatMoney(MinInt64) = %q, want %q", got, want)
	}
}

func TestPlain(t *testing.T) { // Task 2: export/JSON text, no grouping, no symbol
	cases := []struct{ got, want string }{
		{Money(1411200000).Plain(), "14112000.00"},
		{Money(-49).Plain(), "-0.49"},
		{Money(0).Plain(), "0.00"},
		{Qty(2500).Plain(), "2.5"},
		{Qty(100000000).Plain(), "100000"},
		{Rate(123450).Plain(), "12.345"},
		{Rate(125000).Plain(), "12.50"},
		{Rate(1234567890).Plain(), "123456.789"},
		{Percent(18000).Plain(), "18"},
		{Percent(125).Plain(), "0.125"},
		{Factor(123456789123456789).Plain(), "123456789.123456789"},
		{Factor(1_500_000_000).Plain(), "1.5"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: Plain() = %q, want %q", i, c.got, c.want)
		}
	}
}
