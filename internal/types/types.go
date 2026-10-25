// Package types holds the business value types and their parsers and
// formatters (spec §5, NUM). WP0 declares the signatures; NUM implements them.
package types

import "time"

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

// Timestamp is a UTC instant, truncated to the millisecond (timestamp.go).
type Timestamp struct{ t time.Time }

type FY struct {
	Start, End   Date
	Label, Short string
} // "2026-27", "26-27"

type UserID int64 // CD-01

type Page struct{ Number, Size int } // CD-01

type Result[T any] struct { // CD-01
	Rows  []T
	Total int
}
