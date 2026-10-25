package types

import "strings"

// kind says why parseDecimal refused its input.
type kind int

const (
	kindOK       kind = iota
	kindForm          // not a number in an accepted form
	kindDecimals      // more decimals than the scale allows
	kindRange         // more than 18 significant digits, or outside the type's range
)

const (
	blank      = " \t\u00a0" // trimmed from both ends: space, tab, NBSP
	numberForm = " is not a valid number. Write it like 1234.50."
	qtyLarge   = "Quantity is too large. The maximum is 1,00,00,000."
)

// parseDecimal reads s (grammar D-NUM-14) as a value with dp decimals.
// rupee allows a "₹" after an optional sign; percent allows a trailing "%".
// It returns ErrEmpty for blank input, and otherwise a kind.
func parseDecimal(s string, dp int, rupee, percent bool) (int64, kind, error) {
	in := strings.Trim(s, blank)
	if in == "" {
		return 0, kindForm, ErrEmpty
	}
	rest, neg := strings.CutPrefix(in, "-")
	if rupee {
		if r, ok := strings.CutPrefix(rest, "₹"); ok {
			rest = strings.TrimLeft(r, " ")
			if !neg {
				rest, neg = strings.CutPrefix(rest, "-")
			}
		}
	}
	if percent {
		rest = strings.TrimSuffix(rest, "%")
	}
	intPart, frac, hasDot := strings.Cut(rest, ".")
	digits, ok := integerDigits(intPart)
	if !ok || !allDigits(frac) || (intPart == "" && frac == "") || (intPart == "" && !hasDot) {
		return 0, kindForm, nil
	}
	frac = strings.TrimRight(frac, "0")
	if len(frac) > dp {
		return 0, kindDecimals, nil
	}
	digits = strings.TrimLeft(digits, "0")
	if len(digits)+dp > 18 {
		return 0, kindRange, nil
	}
	var v int64
	for _, c := range digits + frac + strings.Repeat("0", dp-len(frac)) {
		v = v*10 + int64(c-'0')
	}
	if neg {
		v = -v
	}
	return v, kindOK, nil
}

// integerDigits checks the integer part (empty, plain digits, Indian or
// international grouping) and returns it without commas.
func integerDigits(s string) (string, bool) {
	if !strings.Contains(s, ",") {
		return s, allDigits(s)
	}
	g := strings.Split(s, ",")
	first, mid, last := g[0], g[1:len(g)-1], g[len(g)-1]
	if !allDigits(strings.Join(g, "")) || first == "" || len(first) > 3 || len(last) != 3 {
		return "", false
	}
	indian, intl := len(first) <= 2, true
	for _, m := range mid {
		indian = indian && len(m) == 2
		intl = intl && len(m) == 3
	}
	return strings.Join(g, ""), indian || intl
}

// allDigits reports whether s holds only ASCII digits (true for "").
func allDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// quoted is the trimmed input in double quotes, for the invalid-form message.
func quoted(s string) string { return `"` + strings.Trim(s, blank) + `"` }

// inputErr turns a refusal into the type's message.
func inputErr(k kind, form, decimals, large string) error {
	switch k {
	case kindForm:
		return &InputError{Msg: form}
	case kindDecimals:
		return &InputError{Msg: decimals}
	default:
		return &InputError{Msg: large}
	}
}

// ParseMoney reads an amount in paise; "₹" and spaces may come before
// or after the sign ("-₹1,234.50", "₹ -1,234.50"). At most 2 decimals; |v| <= MaxMoney.
//
// The grammar (D-NUM-14): surrounding spaces, tabs and NBSPs are trimmed;
// an optional "-"; plain digits or Indian ("1,41,12,000") or international
// ("14,112,000") grouping; an optional "." with digits. Trailing zeros of the
// fraction are ignored. Blank input returns ErrEmpty; every other refusal is
// an *InputError (invalid form, too many decimals, or out of range).
func ParseMoney(s string) (Money, error) {
	const large = "Amount is too large. The maximum is ₹10,00,00,00,000.00."
	v, k, err := parseDecimal(s, 2, true, false)
	if err != nil {
		return 0, err
	}
	if k == kindOK && (v > int64(MaxMoney) || v < -int64(MaxMoney)) {
		k = kindRange
	}
	if k != kindOK {
		return 0, inputErr(k, quoted(s)+` is not a valid amount. Write it like 1,234.50 or 1234.50.`,
			"Amount can have at most 2 decimal places.", large)
	}
	return Money(v), nil
}

// ParseQty reads a quantity in thousandths. At most 3 decimals;
// |v| <= MaxQty.
//
// Grammar and errors as for ParseMoney.
func ParseQty(s string) (Qty, error) {
	v, k, err := parseDecimal(s, 3, false, false)
	if err != nil {
		return 0, err
	}
	if k == kindOK && (v > int64(MaxQty) || v < -int64(MaxQty)) {
		k = kindRange
	}
	if k != kindOK {
		return 0, inputErr(k, quoted(s)+numberForm, "Quantity can have at most 3 decimal places.", qtyLarge)
	}
	return Qty(v), nil
}

// ParseRate reads a rate in ten-thousandths of a rupee; a "₹" is allowed
// as for ParseMoney. At most 4 decimals; |v| <= MaxRate.
//
// Grammar and errors as for ParseMoney.
func ParseRate(s string) (Rate, error) {
	v, k, err := parseDecimal(s, 4, true, false)
	if err != nil {
		return 0, err
	}
	if k == kindOK && (v > int64(MaxRate) || v < -int64(MaxRate)) {
		k = kindRange
	}
	if k != kindOK {
		return 0, inputErr(k, quoted(s)+numberForm, "Rate can have at most 4 decimal places.",
			"Rate is too large. The maximum is ₹10,00,00,000.0000.")
	}
	return Rate(v), nil
}

// ParsePercent reads a percentage in thousandths of a percent; a trailing
// "%" is allowed. At most 3 decimals; 0 <= v <= MaxPercent.
//
// Grammar and errors as for ParseMoney.
func ParsePercent(s string) (Percent, error) {
	const outside = "Percentage must be between 0 and 100."
	v, k, err := parseDecimal(s, 3, false, true)
	if err != nil {
		return 0, err
	}
	if k == kindOK && (v < 0 || v > int64(MaxPercent)) {
		k = kindRange
	}
	if k != kindOK {
		return 0, inputErr(k, quoted(s)+numberForm, "Percentage can have at most 3 decimal places.", outside)
	}
	return Percent(v), nil
}

// ParseFactor reads a conversion factor in 1e-9 units. At most 9 decimals;
// 0 < v <= MaxFactor.
//
// Grammar and errors as for ParseMoney.
func ParseFactor(s string) (Factor, error) {
	v, k, err := parseDecimal(s, 9, false, false)
	if err != nil {
		return 0, err
	}
	negLarge := k == kindRange && strings.HasPrefix(strings.Trim(s, blank), "-")
	if (k == kindOK && v <= 0) || negLarge {
		return 0, &InputError{Msg: "Conversion factor must be more than 0."}
	}
	if k == kindOK && v > int64(MaxFactor) {
		k = kindRange
	}
	if k != kindOK {
		return 0, inputErr(k, quoted(s)+numberForm, "Conversion factor can have at most 9 decimal places.",
			"Conversion factor is too large. The maximum is 10,00,000.")
	}
	return Factor(v), nil
}
