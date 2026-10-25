package types

import (
	"database/sql/driver"
	"fmt"
)

// scanInt64 accepts only an int64 from the driver. A float64 (from TOTAL,
// AVG or a REAL column) is refused, never converted (D-NUM-01); nil means
// the column needs COALESCE or sql.Null.
func scanInt64(kind string, src any) (int64, error) {
	switch v := src.(type) {
	case int64:
		return v, nil
	case float64: //nolint:forbidigo // detects the driver's REAL result to refuse it; the value is never used
		return 0, &InputError{Msg: kind + " column returned a decimal value; use SUM on integer columns."}
	case nil:
		return 0, &InputError{Msg: kind + " column is empty; use COALESCE or sql.Null."}
	default:
		return 0, fmt.Errorf("types: %s column returned %T, want int64", kind, src)
	}
}

// Scan implements sql.Scanner.
func (m *Money) Scan(src any) error {
	v, err := scanInt64("Money", src)
	if err != nil {
		return err
	}
	*m = Money(v)
	return nil
}

// Value implements driver.Valuer.
func (m Money) Value() (driver.Value, error) { return int64(m), nil }

// Scan implements sql.Scanner.
func (q *Qty) Scan(src any) error {
	v, err := scanInt64("Quantity", src)
	if err != nil {
		return err
	}
	*q = Qty(v)
	return nil
}

// Value implements driver.Valuer.
func (q Qty) Value() (driver.Value, error) { return int64(q), nil }

// Scan implements sql.Scanner.
func (r *Rate) Scan(src any) error {
	v, err := scanInt64("Rate", src)
	if err != nil {
		return err
	}
	*r = Rate(v)
	return nil
}

// Value implements driver.Valuer.
func (r Rate) Value() (driver.Value, error) { return int64(r), nil }

// Scan implements sql.Scanner.
func (p *Percent) Scan(src any) error {
	v, err := scanInt64("Percentage", src)
	if err != nil {
		return err
	}
	*p = Percent(v)
	return nil
}

// Value implements driver.Valuer.
func (p Percent) Value() (driver.Value, error) { return int64(p), nil }

// Scan implements sql.Scanner.
func (f *Factor) Scan(src any) error {
	v, err := scanInt64("Conversion factor", src)
	if err != nil {
		return err
	}
	*f = Factor(v)
	return nil
}

// Value implements driver.Valuer.
func (f Factor) Value() (driver.Value, error) { return int64(f), nil }
