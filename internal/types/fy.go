package types

import (
	"fmt"
	"time"
)

// FiscalYear returns the April–March financial year that contains d.
func FiscalYear(d Date) FY {
	y := d.Year
	if d.Month < time.April {
		y--
	}
	return FY{
		Start: Date{Year: y, Month: time.April, Day: 1},
		End:   Date{Year: y + 1, Month: time.March, Day: 31},
		Label: fmt.Sprintf("%d-%02d", y, (y+1)%100),
		Short: fmt.Sprintf("%02d-%02d", y%100, (y+1)%100),
	}
}

// Days is the number of days in the year, Start and End included.
func (fy FY) Days() int { return DaysBetween(fy.Start, fy.End) + 1 }

// Contains reports whether d falls in the year.
func (fy FY) Contains(d Date) bool { return d.Compare(fy.Start) >= 0 && d.Compare(fy.End) <= 0 }
