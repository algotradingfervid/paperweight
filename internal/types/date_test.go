package types_test

import (
	"errors"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

func date(y int, m time.Month, d int) types.Date { return types.Date{Year: y, Month: m, Day: d} }

// fixedClock is a clock that always reports at.
type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

// wantDateErr fails the test unless err is non-nil, is not ErrEmpty, and its text is exactly msg.
func wantDateErr(t *testing.T, in string, err error, msg string) {
	t.Helper()
	if err == nil || errors.Is(err, types.ErrEmpty) || err.Error() != msg {
		t.Errorf("ParseDate(%q) err = %v, want %q", in, err, msg)
	}
}

func TestDate_BusinessDateFromUTC(t *testing.T) { // NUM-073
	cases := []struct {
		utc    time.Time
		want   types.Date
		wantFY string
	}{
		{time.Date(2025, 3, 31, 18, 29, 59, 0, time.UTC), date(2025, time.March, 31), "2024-25"}, // 23:59:59 IST
		{time.Date(2025, 3, 31, 18, 30, 0, 0, time.UTC), date(2025, time.April, 1), "2025-26"},   // 00:00 IST
	}
	for _, c := range cases {
		got := types.BusinessDate(c.utc)
		if got != c.want {
			t.Errorf("BusinessDate(%s) = %v, want %v", c.utc.Format(time.RFC3339), got, c.want)
		}
		if fy := types.FiscalYear(got).Label; fy != c.wantFY {
			t.Errorf("FiscalYear(BusinessDate(%s)).Label = %q, want %q", c.utc.Format(time.RFC3339), fy, c.wantFY)
		}
	}
}

func TestDate_TodayInIST(t *testing.T) { // NUM-074
	cases := []struct {
		name   string
		clock  interface{ Now() time.Time }
		want   types.Date
		wantFY string
	}{
		{"2025-03-31T20:00:00Z", fixedClock{time.Date(2025, 3, 31, 20, 0, 0, 0, time.UTC)}, date(2025, time.April, 1), "2025-26"},
		{"fixture clock", testkit.Clock(), date(2026, time.October, 24), "2026-27"},
	}
	for _, c := range cases {
		got := types.Today(c.clock)
		if got != c.want {
			t.Errorf("%s: Today() = %v, want %v", c.name, got, c.want)
		}
		if fy := types.FiscalYear(got).Label; fy != c.wantFY {
			t.Errorf("%s: FiscalYear(Today()).Label = %q, want %q", c.name, fy, c.wantFY)
		}
	}
}

func TestDate_Format(t *testing.T) { // NUM-077
	if got := types.FormatDate(date(2025, time.April, 1)); got != "01-04-2025" {
		t.Errorf("FormatDate(2025-04-01) = %q, want %q", got, "01-04-2025")
	}
}

func TestDate_ParseAccepted(t *testing.T) { // NUM-078
	cases := []struct {
		in   string
		want types.Date
	}{
		{"01-04-2025", date(2025, time.April, 1)},
		{"1-4-2025", date(2025, time.April, 1)},
		{"01/04/2025", date(2025, time.April, 1)},
		{"01.04.2025", date(2025, time.April, 1)},
		{"2025-04-01", date(2025, time.April, 1)},
		{"01-Apr-2025", date(2025, time.April, 1)},
		{"01-apr-2025", date(2025, time.April, 1)},
		{"04/05/2025", date(2025, time.May, 4)}, // always day-first
	}
	for _, c := range cases {
		got, err := types.ParseDate(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseDate(%q) = %v, %v; want %v, nil", c.in, got, err, c.want)
		}
	}
}

func TestDate_ParseRejected(t *testing.T) { // NUM-079
	for _, in := range []string{"31-02-2025", "29-02-2025", "29-02-2100", "00-01-2025", "2025-13-01", "01-04-25", "45748", "04/31/2025"} {
		_, err := types.ParseDate(in)
		wantDateErr(t, in, err, in+" is not a valid date. Use dd-mm-yyyy, for example 31-03-2025.")
	}
	accepted := []struct {
		in   string
		want types.Date
	}{
		{"29-02-2024", date(2024, time.February, 29)},
		{"29-02-2000", date(2000, time.February, 29)},
	}
	for _, c := range accepted {
		got, err := types.ParseDate(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseDate(%q) = %v, %v; want %v, nil", c.in, got, err, c.want)
		}
	}
}

func TestDate_YearRange(t *testing.T) { // NUM-080
	for _, in := range []string{"01-04-0025", "01-04-2125"} {
		_, err := types.ParseDate(in)
		wantDateErr(t, in, err, "Year must be between 1900 and 2099.")
	}
}

func TestDate_AddMonthsClamp(t *testing.T) { // NUM-081
	cases := []struct {
		d    types.Date
		n    int
		want types.Date
	}{
		{date(2024, time.January, 31), 1, date(2024, time.February, 29)},
		{date(2025, time.January, 31), 1, date(2025, time.February, 28)},
		{date(2025, time.November, 30), 3, date(2026, time.February, 28)},
		{date(2025, time.March, 31), 1, date(2025, time.April, 30)},
		{date(2025, time.August, 31), -6, date(2025, time.February, 28)},
		{date(2025, time.June, 15), 12, date(2026, time.June, 15)},
	}
	for _, c := range cases {
		if got := types.AddMonths(c.d, c.n); got != c.want {
			t.Errorf("AddMonths(%v, %d) = %v, want %v", c.d, c.n, got, c.want)
		}
	}
}

func TestDate_AddDays(t *testing.T) { // NUM-082
	cases := []struct {
		d    types.Date
		n    int
		want types.Date
	}{
		{date(2024, time.February, 25), 5, date(2024, time.March, 1)},
		{date(2025, time.February, 25), 5, date(2025, time.March, 2)},
		{date(2025, time.March, 30), 30, date(2025, time.April, 29)},
		{date(2025, time.April, 1), -1, date(2025, time.March, 31)},
	}
	for _, c := range cases {
		if got := types.AddDays(c.d, c.n); got != c.want {
			t.Errorf("AddDays(%v, %d) = %v, want %v", c.d, c.n, got, c.want)
		}
	}
	if got := types.DaysBetween(date(2025, time.April, 1), date(2026, time.March, 31)); got != 364 {
		t.Errorf("DaysBetween(2025-04-01, 2026-03-31) = %d, want 364", got)
	}
}

func TestDate_MonthEnd(t *testing.T) { // NUM-083
	cases := []struct {
		d, want types.Date
	}{
		{date(2024, time.February, 10), date(2024, time.February, 29)},
		{date(2025, time.February, 10), date(2025, time.February, 28)},
		{date(2020, time.December, 24), date(2020, time.December, 31)},
	}
	for _, c := range cases {
		if got := types.LastDayOfMonth(c.d); got != c.want {
			t.Errorf("LastDayOfMonth(%v) = %v, want %v", c.d, got, c.want)
		}
	}
	if !types.IsMonthEnd(date(2020, time.December, 31)) {
		t.Error("IsMonthEnd(2020-12-31) = false, want true")
	}
	if types.IsMonthEnd(date(2020, time.December, 24)) {
		t.Error("IsMonthEnd(2020-12-24) = true, want false")
	}
}
