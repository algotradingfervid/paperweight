package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDiscount_NetRate(t *testing.T) { // NUM-015
	cases := []struct {
		price    Rate
		discount Percent
		want     Rate
	}{
		{1000000, 12500, 875000}, // ₹100.0000 less 12.5% = ₹87.5000
		{999900, 33333, 666603},  // ₹99.9900 less 33.333% = ₹66.6603 (66.66033… rounded)
		{1000000, 100000, 0},     // 100% discount
		{1000000, 0, 1000000},    // no discount
	}
	for _, c := range cases {
		got, err := NetRate(c.price, c.discount)
		if err != nil || got != c.want {
			t.Errorf("NetRate(%d, %d) = %d, %v; want %d, nil", c.price, c.discount, got, err, c.want)
		}
	}
	for _, bad := range []Percent{100001, -1} {
		_, err := NetRate(1000000, bad)
		wantMsg(t, err, "Percentage must be between 0 and 100.")
	}
}

func TestMoney_JSONAsString(t *testing.T) { // NUM-018
	b, err := json.Marshal(Money(123450))
	if err != nil || string(b) != `"1234.50"` {
		t.Errorf("json.Marshal(Money(123450)) = %s, %v; want \"1234.50\", nil", b, err)
	}

	var m Money
	if err := json.Unmarshal([]byte(`"1,234.50"`), &m); err != nil || m != 123450 {
		t.Errorf(`json.Unmarshal("1,234.50") = %d, %v; want 123450, nil`, m, err)
	}

	// A string goes through ParseMoney, so ParseMoney's rejection comes back.
	var bad Money
	_, perr := ParseMoney("12.345")
	if perr == nil {
		t.Fatal(`ParseMoney("12.345") = nil error, want the too-many-decimals error`)
	}
	wantMsg(t, json.Unmarshal([]byte(`"12.345"`), &bad), perr.Error())

	var n Money
	wantMsg(t, json.Unmarshal([]byte(`1234.5`), &n), `Amount must be sent as text, e.g. "1234.50".`)
}

func TestDate_DueDateRules(t *testing.T) { // NUM-084
	bill := fyDate(2026, time.January, 15)
	cases := []struct {
		name string
		rule TermRule
		want Date
	}{
		{"30 days after bill date", TermRule{Days: 30}, fyDate(2026, time.February, 14)},
		{"15 days after end of bill month", TermRule{Days: 15, FromMonthEnd: true}, fyDate(2026, time.February, 15)},
		{"1 month after end of bill month", TermRule{Months: 1, FromMonthEnd: true}, fyDate(2026, time.February, 28)},
	}
	for _, c := range cases {
		if got := DueDate(bill, c.rule); got != c.want {
			t.Errorf("%s: DueDate(%v, %+v) = %v, want %v", c.name, bill, c.rule, got, c.want)
		}
	}

	wantMsg(t, ValidateDueDate(bill, fyDate(2026, time.January, 14)), "Due date can't be before the bill date.")
	for _, due := range []Date{bill, fyDate(2026, time.February, 14)} {
		if err := ValidateDueDate(bill, due); err != nil {
			t.Errorf("ValidateDueDate(%v, %v) = %v, want nil", bill, due, err)
		}
	}
}

func TestDate_RangePresets(t *testing.T) { // NUM-085
	today := fyDate(2026, time.October, 24)
	cases := []struct {
		name     string
		from, to Date
	}{
		{"This FY", fyDate(2026, time.April, 1), fyDate(2027, time.March, 31)},
		{"Last FY", fyDate(2025, time.April, 1), fyDate(2026, time.March, 31)},
		{"This month", fyDate(2026, time.October, 1), fyDate(2026, time.October, 31)},
		{"Last 30 days", fyDate(2026, time.September, 25), fyDate(2026, time.October, 24)},
	}
	for _, c := range cases {
		from, to, err := RangePreset(c.name, today)
		if err != nil || from != c.from || to != c.to {
			t.Errorf("RangePreset(%q, %v) = %v, %v, %v; want %v, %v, nil", c.name, today, from, to, err, c.from, c.to)
		}
	}
	if _, _, err := RangePreset("Next decade", today); err == nil {
		t.Error(`RangePreset("Next decade") = nil error, want an error for an unknown preset`)
	}
}

func TestFiscalYear_Quarter(t *testing.T) { // NUM-086
	cases := []struct {
		d    Date
		want string
	}{
		{fyDate(2025, time.July, 15), "Q2 2025-26"},
		{fyDate(2026, time.February, 10), "Q4 2025-26"},
		{fyDate(2025, time.December, 31), "Q3 2025-26"},
		{fyDate(2026, time.April, 1), "Q1 2026-27"},
	}
	for _, c := range cases {
		if got := FiscalQuarter(c.d); got != c.want {
			t.Errorf("FiscalQuarter(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
