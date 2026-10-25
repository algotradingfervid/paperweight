package types

import (
	"fmt"
	"time"
)

// TermRule is a payment term: Months then Days after the bill date, or
// after the end of the bill's month when FromMonthEnd.
type TermRule struct {
	Days, Months int
	FromMonthEnd bool
}

// DueDate applies r to the bill date.
func DueDate(bill Date, r TermRule) Date {
	base := bill
	if r.FromMonthEnd {
		base = LastDayOfMonth(bill)
	}
	return AddDays(AddMonths(base, r.Months), r.Days)
}

// ValidateDueDate refuses a due date before the bill date.
func ValidateDueDate(bill, due Date) error {
	if due.Compare(bill) < 0 {
		return &InputError{Msg: "Due date can't be before the bill date."}
	}
	return nil
}

// RangePreset returns the dates, both included, of a named date range
// relative to today.
func RangePreset(name string, today Date) (from, to Date, err error) {
	switch name {
	case "This FY":
		fy := FiscalYear(today)
		return fy.Start, fy.End, nil
	case "Last FY":
		fy := FiscalYear(AddMonths(today, -12))
		return fy.Start, fy.End, nil
	case "This month":
		return Date{Year: today.Year, Month: today.Month, Day: 1}, LastDayOfMonth(today), nil
	case "Last 30 days":
		return AddDays(today, -29), today, nil
	}
	return Date{}, Date{}, fmt.Errorf("types: unknown date range preset %q", name)
}

// FiscalQuarter labels d's quarter of the April–March year, "Q2 2025-26".
func FiscalQuarter(d Date) string {
	q := (int(d.Month-time.April)+12)%12/3 + 1
	return fmt.Sprintf("Q%d %s", q, FiscalYear(d).Label)
}
