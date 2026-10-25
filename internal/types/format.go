package types

import "strconv"

// formatScaled shows x, stored with scale decimals, with minDP to maxDP
// decimals: rounded half-up to maxDP, trailing zeros dropped down to minDP.
// group applies Indian grouping (last 3 digits, then groups of 2). A value
// that shows as zero never carries a "-".
func formatScaled(x int64, scale, minDP, maxDP int, group bool) string {
	x = Rescale(x, scale, maxDP)
	u, neg := abs(x)
	digits := strconv.FormatUint(u, 10)
	for len(digits) <= maxDP {
		digits = "0" + digits
	}
	intPart, frac := digits[:len(digits)-maxDP], digits[len(digits)-maxDP:]
	for len(frac) > minDP && frac[len(frac)-1] == '0' {
		frac = frac[:len(frac)-1]
	}
	if group {
		intPart = groupIndian(intPart)
	}
	s := intPart
	if frac != "" {
		s += "." + frac
	}
	if neg {
		s = "-" + s
	}
	return s
}

// groupIndian inserts commas into a digit string: "14112000" → "1,41,12,000".
func groupIndian(d string) string {
	if len(d) <= 3 {
		return d
	}
	head, tail := d[:len(d)-3], d[len(d)-3:]
	out := ""
	for len(head) > 2 {
		out = "," + head[len(head)-2:] + out
		head = head[:len(head)-2]
	}
	return head + out + "," + tail
}

func FormatMoney(m Money) string { return formatScaled(int64(m), 2, 2, 2, true) } // "94,164.00"

// FormatINR is FormatMoney with "₹" after any sign: "-₹1,234.50".
func FormatINR(m Money) string {
	s := FormatMoney(m)
	if s[0] == '-' {
		return "-₹" + s[1:]
	}
	return "₹" + s
}

func FormatQty(q Qty) string                { return formatScaled(int64(q), 3, 0, 3, true) }       // "1,200" (trailing zeros dropped)
func FormatQtyUoM(q Qty, uom string) string { return FormatQty(q) + " " + uom }                    // "1,200 kg"
func FormatRate(r Rate) string              { return formatScaled(int64(r), 4, 2, 4, true) }       // "62.50" (2–4 decimals, CD-61)
func FormatPercent(p Percent) string        { return formatScaled(int64(p), 3, 0, 3, true) + "%" } // "18%"
func FormatFactor(f Factor) string          { return formatScaled(int64(f), 9, 0, 9, true) }       // up to 9 decimals
func FormatCount(n int64) string            { return formatScaled(n, 0, 0, 0, true) }              // "6,200"

// Plain text for export and JSON: no grouping, no symbol.
func (m Money) Plain() string   { return formatScaled(int64(m), 2, 2, 2, false) } // "14112000.00"
func (q Qty) Plain() string     { return formatScaled(int64(q), 3, 0, 3, false) } // "2.5"
func (r Rate) Plain() string    { return formatScaled(int64(r), 4, 2, 4, false) } // "12.50"
func (p Percent) Plain() string { return formatScaled(int64(p), 3, 0, 3, false) } // "18"
func (f Factor) Plain() string  { return formatScaled(int64(f), 9, 0, 9, false) }
