package types

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// storedForm is the fixed-width UTC text a Timestamp is stored as (D-NUM-18).
const storedForm = "2006-01-02T15:04:05.000Z"

// TimestampOf returns t as a UTC instant truncated to the millisecond.
func TimestampOf(t time.Time) Timestamp {
	return Timestamp{t: t.UTC().Truncate(time.Millisecond)}
}

// Time returns the instant in UTC.
func (ts Timestamp) Time() time.Time { return ts.t }

// String is the stored form, "2025-04-01T04:30:00.000Z".
func (ts Timestamp) String() string { return ts.t.Format(storedForm) }

// ParseTimestamp reads the stored form.
func ParseTimestamp(s string) (Timestamp, error) {
	t, err := time.Parse(storedForm, s)
	if err != nil {
		return Timestamp{}, fmt.Errorf("types: timestamp %q: %w", s, err)
	}
	return TimestampOf(t), nil
}

// Scan implements sql.Scanner (TEXT in the stored form).
func (ts *Timestamp) Scan(src any) error {
	s, ok := src.(string)
	if !ok {
		return fmt.Errorf("types: Timestamp column returned %T, want string", src)
	}
	v, err := ParseTimestamp(s)
	if err != nil {
		return err
	}
	*ts = v
	return nil
}

// Value implements driver.Valuer.
func (ts Timestamp) Value() (driver.Value, error) { return ts.String(), nil }

// FormatIST is the only display form for timestamps: "24-10-2026 10:30" (CD-33).
func FormatIST(t time.Time) string { return t.In(IST).Format("02-01-2006 15:04") }

// FormatISTExport is the CSV export form: "2025-04-01 10:00:00".
func FormatISTExport(t time.Time) string { return t.In(IST).Format("2006-01-02 15:04:05") }

// ISTDayStart is the instant of 00:00 IST on d.
func ISTDayStart(d Date) Timestamp {
	return TimestampOf(time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, IST))
}
