package types

import (
	"fmt"
	"time"
)

// TermRule is a payment term: Months then Days after the bill date; with
// FromMonthEnd the Days count from the end of the month Months brings.
type TermRule struct {
	Days, Months int
	FromMonthEnd bool
}

// DueDate applies r to the bill date: AddMonths(bill, Months), moved to
// that month's last day when FromMonthEnd, then AddDays(Days). Taking the
// month end after adding months follows ERPNext
// (erpnext/accounts/party.py: get_last_day(add_months(due_date, credit_months))),
// so 15 Feb + 1 month from month end is 31 Mar, not 28 Mar.
func DueDate(bill Date, r TermRule) Date {
	due := AddMonths(bill, r.Months)
	if r.FromMonthEnd {
		due = LastDayOfMonth(due)
	}
	return AddDays(due, r.Days)
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
