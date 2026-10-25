package types

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	dateForm  = " is not a valid date. Use dd-mm-yyyy, for example 31-03-2025."
	yearRange = "Year must be between 1900 and 2099."
)

var monthNames = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March, "apr": time.April,
	"may": time.May, "jun": time.June, "jul": time.July, "aug": time.August,
	"sep": time.September, "oct": time.October, "nov": time.November, "dec": time.December,
}

// ParseDate reads a business date (D-NUM-15): d-m-yyyy, d/m/yyyy, d.m.yyyy,
// d-Mon-yyyy or yyyy-mm-dd, always day first.
func ParseDate(s string) (Date, error) {
	s = strings.Trim(s, blank)
	if s == "" {
		return Date{}, ErrEmpty
	}
	d, ok := splitDate(s)
	if !ok || d.Month < time.January || d.Month > time.December ||
		d.Day < 1 || d.Day > LastDayOfMonth(d).Day {
		return Date{}, &InputError{Msg: s + dateForm}
	}
	if d.Year < 1900 || d.Year > 2099 {
		return Date{}, &InputError{Msg: yearRange}
	}
	return d, nil
}

// splitDate parses the form only; calendar checks are the caller's.
func splitDate(s string) (Date, bool) {
	var sep string
	for _, c := range []string{"-", "/", "."} {
		if strings.Contains(s, c) {
			sep = c
			break
		}
	}
	if sep == "" {
		return Date{}, false
	}
	p := strings.Split(s, sep)
	if len(p) != 3 {
		return Date{}, false
	}
	if sep == "-" && len(p[0]) == 4 { // yyyy-mm-dd
		y, ok1 := digits(p[0], 4, 4)
		m, ok2 := digits(p[1], 2, 2)
		d, ok3 := digits(p[2], 2, 2)
		return Date{Year: y, Month: time.Month(m), Day: d}, ok1 && ok2 && ok3
	}
	d, ok1 := digits(p[0], 1, 2)
	y, ok3 := digits(p[2], 4, 4)
	m, ok2 := digits(p[1], 1, 2)
	if !ok2 && sep == "-" {
		var mon time.Month
		mon, ok2 = monthNames[strings.ToLower(p[1])]
		m = int(mon)
	}
	return Date{Year: y, Month: time.Month(m), Day: d}, ok1 && ok2 && ok3
}

// digits parses s as min to max ASCII digits.
func digits(s string, lo, hi int) (int, bool) {
	if len(s) < lo || len(s) > hi {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// FormatDate is the display form, "01-04-2025" (CD-33).
func FormatDate(d Date) string { return fmt.Sprintf("%02d-%02d-%04d", d.Day, int(d.Month), d.Year) }

// ISO is the stored form, "2025-04-01".
func (d Date) ISO() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day) }

// DateFromISO reads the stored form.
func DateFromISO(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Date{}, fmt.Errorf("types: date %q: %w", s, err)
	}
	return dateOf(t), nil
}

// Compare returns -1, 0 or +1 as d is before, equal to or after o.
func (d Date) Compare(o Date) int { return d.utc().Compare(o.utc()) }

// Scan implements sql.Scanner (TEXT in ISO form).
func (d *Date) Scan(src any) error {
	s, ok := src.(string)
	if !ok {
		return fmt.Errorf("types: Date column returned %T, want string", src)
	}
	v, err := DateFromISO(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// Value implements driver.Valuer.
func (d Date) Value() (driver.Value, error) { return d.ISO(), nil }

func (d Date) utc() time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }

func dateOf(t time.Time) Date { return Date{Year: t.Year(), Month: t.Month(), Day: t.Day()} }

// AddDays returns d moved by n days.
func AddDays(d Date, n int) Date { return dateOf(d.utc().AddDate(0, 0, n)) }

// AddMonths returns d moved by n months, clamped to the last day of the
// target month (31-01 + 1 → 28-02 or 29-02).
func AddMonths(d Date, n int) Date {
	months := d.Year*12 + int(d.Month) - 1 + n
	first := Date{Year: months / 12, Month: time.Month(months%12 + 1), Day: 1}
	first.Day = min(d.Day, LastDayOfMonth(first).Day)
	return first
}

// DaysBetween is the number of days from a to b (negative if b is before a).
func DaysBetween(a, b Date) int { return int(b.utc().Sub(a.utc()) / (24 * time.Hour)) }

// LastDayOfMonth returns the last day of d's month.
func LastDayOfMonth(d Date) Date {
	return dateOf(time.Date(d.Year, d.Month+1, 0, 0, 0, 0, 0, time.UTC))
}

// IsMonthEnd reports whether d is the last day of its month.
func IsMonthEnd(d Date) bool { return d.Day == LastDayOfMonth(d).Day }

// BusinessDate is the calendar date of t in IST.
func BusinessDate(t time.Time) Date { return dateOf(t.In(IST)) }

// Today is the business date now; c is usually a clock.Clock.
func Today(c interface{ Now() time.Time }) Date { return BusinessDate(c.Now()) }
