package doc

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/types"
)

// Values holds typed field values by field name: Text and Select string, Int
// int64, Money types.Money, Qty types.Qty, Rate types.Rate, Percent
// types.Percent, Factor types.Factor, Date types.Date, Timestamp time.Time
// (UTC), Check bool, Link int64; nil is empty.
type Values map[string]any

const (
	dateLayout      = "2006-01-02"
	timestampLayout = audit.TimeLayout
)

var errUnreadable = errors.New("doc: unreadable form value")

// scale is the number of decimals of a scaled number type; ok is false for
// every other type.
func scale(t FieldType) (int, bool) {
	switch t {
	case Money:
		return 2, true
	case Qty, Percent:
		return 3, true
	case Rate:
		return 4, true
	case Factor:
		return 9, true
	}
	return 0, false
}

// PlainValue parses a form value (the plan's form value format) into its
// typed value; "" is nil.
func PlainValue(f Field, s string) (any, error) {
	if s == "" {
		return nil, nil
	}
	if sc, ok := scale(f.Type); ok {
		n, err := parseDecimal(s, sc)
		if err != nil {
			return nil, err
		}
		switch f.Type {
		case Money:
			return types.Money(n), nil
		case Qty:
			return types.Qty(n), nil
		case Rate:
			return types.Rate(n), nil
		case Percent:
			return types.Percent(n), nil
		default:
			return types.Factor(n), nil
		}
	}
	switch f.Type {
	case Text:
		return s, nil
	case Select:
		if !slices.Contains(f.Options, s) {
			return nil, errUnreadable
		}
		return s, nil
	case Int, Link:
		n, err := parseDecimal(s, 0)
		if err != nil {
			return nil, err
		}
		return n, nil
	case Check:
		switch s {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, errUnreadable
	case Date:
		t, err := time.Parse(dateLayout, s)
		if err != nil {
			return nil, errUnreadable
		}
		return types.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}, nil
	case Timestamp:
		t, err := time.Parse(timestampLayout, s)
		if err != nil {
			return nil, errUnreadable
		}
		return t.UTC(), nil
	}
	return nil, errUnreadable
}

// parseDecimal reads an optional "-", base-10 digits and at most sc decimals
// after a dot, and returns the value in units of 10^-sc.
func parseDecimal(s string, sc int) (int64, error) {
	digits := strings.TrimPrefix(s, "-")
	whole, frac, hasDot := strings.Cut(digits, ".")
	if whole == "" || !allDigits(whole) || !allDigits(frac) || len(frac) > sc || (hasDot && frac == "") {
		return 0, errUnreadable
	}
	n, err := strconv.ParseInt(whole+frac+strings.Repeat("0", sc-len(frac)), 10, 64)
	if err != nil {
		return 0, errUnreadable
	}
	if len(digits) != len(s) {
		n = -n
	}
	return n, nil
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// FormatPlain is the inverse of PlainValue (CD-43): nil is "", numbers carry
// their scale's full decimals. A value of the wrong Go type for f panics.
func FormatPlain(f Field, v any) string {
	if v == nil {
		return ""
	}
	if sc, ok := scale(f.Type); ok {
		if n, ok := intOf(v); ok {
			return formatDecimal(n, sc)
		}
	}
	switch x := v.(type) {
	case string:
		if f.Type == Text || f.Type == Select {
			return x
		}
	case int64:
		if f.Type == Int || f.Type == Link {
			return strconv.FormatInt(x, 10)
		}
	case bool:
		if f.Type == Check {
			return strconv.FormatBool(x)
		}
	case types.Date:
		if f.Type == Date {
			return fmt.Sprintf("%04d-%02d-%02d", x.Year, int(x.Month), x.Day)
		}
	case time.Time:
		if f.Type == Timestamp {
			return x.UTC().Format(timestampLayout)
		}
	}
	panic(fmt.Sprintf("doc: FormatPlain got %T for %s field %q", v, typeNames[f.Type], f.Name))
}

// typeNames names each FieldType in messages.
var typeNames = map[FieldType]string{
	Text: "Text", Int: "Int", Check: "Check", Select: "Select", Link: "Link", Money: "Money",
	Qty: "Qty", Rate: "Rate", Percent: "Percent", Factor: "Factor", Date: "Date", Timestamp: "Timestamp",
}

// formatDecimal writes n (units of 10^-sc) with exactly sc decimals.
func formatDecimal(n int64, sc int) string {
	s := strconv.FormatInt(n, 10)
	sign := ""
	if n < 0 {
		sign, s = "-", s[1:]
	}
	if sc == 0 {
		return sign + s
	}
	if len(s) <= sc {
		s = strings.Repeat("0", sc-len(s)+1) + s
	}
	return sign + s[:len(s)-sc] + "." + s[len(s)-sc:]
}

// intOf returns the integer of an Int, Link or scaled number value.
func intOf(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case types.Money:
		return int64(x), true
	case types.Qty:
		return int64(x), true
	case types.Rate:
		return int64(x), true
	case types.Percent:
		return int64(x), true
	case types.Factor:
		return int64(x), true
	}
	return 0, false
}
