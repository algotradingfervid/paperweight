package types_test

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/types"
)

func TestSQL_ScanAndValueAllKinds(t *testing.T) {
	type scanVal interface {
		sql.Scanner
		driver.Valuer
	}
	cases := []struct {
		kind string
		dst  func() scanVal // a fresh destination holding 7
	}{
		{"Money", func() scanVal { v := types.Money(7); return &v }},
		{"Quantity", func() scanVal { v := types.Qty(7); return &v }},
		{"Rate", func() scanVal { v := types.Rate(7); return &v }},
		{"Percentage", func() scanVal { v := types.Percent(7); return &v }},
		{"Conversion factor", func() scanVal { v := types.Factor(7); return &v }},
	}
	value := func(t *testing.T, d scanVal) driver.Value {
		t.Helper()
		v, err := d.Value()
		if err != nil {
			t.Fatalf("Value: %v", err)
		}
		return v
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			d := c.dst()
			if v := value(t, d); v != int64(7) {
				t.Errorf("Value = %#v, want int64(7)", v)
			}
			if err := d.Scan(int64(12345)); err != nil {
				t.Fatalf("Scan(int64): %v", err)
			}
			if v := value(t, d); v != int64(12345) {
				t.Errorf("after Scan(int64(12345)) Value = %#v", v)
			}

			refused := []struct {
				name, msg string
				src       any
			}{
				{"float", c.kind + " column returned a decimal value; use SUM on integer columns.", float64(1.5)},
				{"null", c.kind + " column is empty; use COALESCE or sql.Null.", nil},
				{"text", "", "12"},
				{"bytes", "", []byte("12")},
			}
			for _, r := range refused {
				d := c.dst()
				err := d.Scan(r.src)
				if err == nil {
					t.Errorf("Scan(%s) err = nil, want refusal", r.name)
					continue
				}
				if r.msg != "" {
					var ie *types.InputError
					if !errors.As(err, &ie) {
						t.Errorf("Scan(%s) err = %v (%T), want *types.InputError", r.name, err, err)
					} else if ie.Msg != r.msg {
						t.Errorf("Scan(%s) Msg = %q, want %q", r.name, ie.Msg, r.msg)
					}
				}
				if v := value(t, d); v != int64(7) {
					t.Errorf("Scan(%s) changed the destination to %#v, want it left at 7", r.name, v)
				}
			}
		})
	}
}
