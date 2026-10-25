// Package types holds the business value types and their parsers and
// formatters (spec §5, NUM). WP0 declares the signatures; NUM implements them.
package types

import (
	"errors"
	"time"
)

// errNotImplemented stands in for errs.ErrNotImplemented: types imports no
// internal package (spec §4.2), so the stubs carry their own copy.
var errNotImplemented = errors.New("not implemented yet")

type Money int64   // paise
type Qty int64     // thousandths
type Rate int64    // ten-thousandths of a rupee
type Percent int64 // thousandths of a percent: 18% = 18000
type Factor int64  // 1e-9

type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// Timestamp is a UTC instant; NUM adds its fields.
type Timestamp struct{}

type FY struct {
	Start, End   Date
	Label, Short string
} // "2026-27", "26-27"

func FiscalYear(d Date) FY { return FY{} }

type UserID int64 // CD-01

type Page struct{ Number, Size int } // CD-01

type Result[T any] struct { // CD-01
	Rows  []T
	Total int
}

// ParseMoney and the other parsers (CD-33).
func ParseMoney(s string) (Money, error)     { return 0, errNotImplemented }
func ParseQty(s string) (Qty, error)         { return 0, errNotImplemented }
func ParseRate(s string) (Rate, error)       { return 0, errNotImplemented }
func ParsePercent(s string) (Percent, error) { return 0, errNotImplemented }
func ParseDate(s string) (Date, error)       { return Date{}, errNotImplemented }

func FormatMoney(m Money) string            { return "" } // "94,164.00"
func FormatINR(m Money) string              { return "" } // "₹94,164.00"
func FormatQty(q Qty) string                { return "" } // "1,200" (trailing zeros dropped)
func FormatQtyUoM(q Qty, uom string) string { return "" } // "1,200 kg"
func FormatRate(r Rate) string              { return "" } // "62.50" (2–4 decimals, CD-61)
func FormatPercent(p Percent) string        { return "" } // "18%"
func FormatDate(d Date) string              { return "" } // "24-10-2026"
func FormatIST(t time.Time) string          { return "" } // "24-10-2026 10:30", the only timestamp format
