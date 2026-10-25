package types

import (
	"errors"
	"testing"
)

// wantInputErr fails unless err is a *InputError whose text is exactly msg
// (Task 3: every parse error other than ErrEmpty is an *InputError).
func wantInputErr(t *testing.T, err error, msg string) {
	t.Helper()
	wantMsg(t, err, msg)
	var ie *InputError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %T (%v), want *InputError", err, err)
	}
}

// wantVal fails unless err is nil and got == want.
func wantVal[T comparable](t *testing.T, in string, got T, err error, want T) {
	t.Helper()
	if err != nil || got != want {
		t.Fatalf("parse %q = %v, %v; want %v, nil", in, got, err, want)
	}
}

func TestMoney_ParseFormatRoundTrip(t *testing.T) { // NUM-001
	m, err := ParseMoney("14,112.80")
	wantVal(t, "14,112.80", m, err, Money(1411280))
	if got := FormatMoney(m); got != "14,112.80" {
		t.Fatalf("FormatMoney(%d) = %q, want %q", m, got, "14,112.80")
	}
	if got := FormatINR(m); got != "₹14,112.80" {
		t.Fatalf("FormatINR(%d) = %q, want %q", m, got, "₹14,112.80")
	}
	z, err := ParseMoney("0")
	wantVal(t, "0", z, err, Money(0))
	if got := FormatMoney(z); got != "0.00" {
		t.Fatalf("FormatMoney(0) = %q, want %q", got, "0.00")
	}
}

func TestQty_ParseScale(t *testing.T) { // NUM-006
	for _, c := range []struct {
		in   string
		want Qty
	}{{"2.5", 2500}, {"1,00,000", 100000000}, {"0.001", 1}} {
		q, err := ParseQty(c.in)
		wantVal(t, c.in, q, err, c.want)
	}
	_, err := ParseQty("1.2345")
	wantInputErr(t, err, "Quantity can have at most 3 decimal places.")
}

func TestRate_ParseFormat(t *testing.T) { // NUM-008
	for _, c := range []struct {
		in   string
		want Rate
		disp string
	}{{"12.345", 123450, "12.345"}, {"0.1234", 1234, "0.1234"}, {"12.5", 125000, "12.50"}} {
		r, err := ParseRate(c.in)
		wantVal(t, c.in, r, err, c.want)
		if got := FormatRate(r); got != c.disp {
			t.Fatalf("FormatRate(%d) = %q, want %q", r, got, c.disp)
		}
	}
	_, err := ParseRate("0.12345")
	wantInputErr(t, err, "Rate can have at most 4 decimal places.")
}

func TestPercent_ParseRange(t *testing.T) { // NUM-009
	for _, c := range []struct {
		in   string
		want Percent
	}{{"18", 18000}, {"0.25", 250}, {"0.125", 125}, {"1.5", 1500}} {
		p, err := ParsePercent(c.in)
		wantVal(t, c.in, p, err, c.want)
	}
	_, err := ParsePercent("0.1255")
	wantInputErr(t, err, "Percentage can have at most 3 decimal places.")
	for _, s := range []string{"100.001", "-1"} {
		_, err := ParsePercent(s)
		wantInputErr(t, err, "Percentage must be between 0 and 100.")
	}
}

func TestMoney_RejectExcessDecimals(t *testing.T) { // NUM-010
	_, err := ParseMoney("10.005")
	wantInputErr(t, err, "Amount can have at most 2 decimal places.")
	for _, s := range []string{"10.50", "10.5"} {
		m, err := ParseMoney(s)
		wantVal(t, s, m, err, Money(1050))
	}
}

func TestLimits_NoOverflow(t *testing.T) { // NUM-012
	_, err := Qty(MaxQty).Times(Rate(MaxRate))
	wantMsg(t, err, "Line amount is too large. The most a line can be is ₹10,00,00,00,000.00.")
	_, err = ParseMoney("10,00,00,00,000.01")
	wantInputErr(t, err, "Amount is too large. The maximum is ₹10,00,00,00,000.00.")
	_, err = ParseQty("1,00,00,000.001")
	wantInputErr(t, err, "Quantity is too large. The maximum is 1,00,00,000.")
}

// clamp keeps x if it is in [lo, hi] and otherwise maps it into that range
// (hi-lo+1 must fit in int64).
func clamp(x, lo, hi int64) int64 {
	if x >= lo && x <= hi {
		return x
	}
	n := hi - lo + 1
	return lo + ((x%n)+n)%n
}

// parseResult checks one parser's answer for arbitrary input: a value within
// limits and nil, ErrEmpty, or an *InputError.
func parseResult(t *testing.T, kind, s string, v, lo, hi int64, err error) {
	t.Helper()
	if err == nil {
		if v < lo || v > hi {
			t.Fatalf("%s(%q) = %d, nil; outside [%d, %d]", kind, s, v, lo, hi)
		}
		return
	}
	var ie *InputError
	if !errors.Is(err, ErrEmpty) && !errors.As(err, &ie) {
		t.Fatalf("%s(%q) error = %T (%v), want ErrEmpty or *InputError", kind, s, err, err)
	}
}

func FuzzValues_RoundTripNoPanic(f *testing.F) { // NUM-019
	f.Add(int64(1411280), "14,112.80")
	f.Add(int64(-123450), "-₹1,234.50")
	f.Add(int64(0), "")
	f.Add(int64(1), "1.5E+11")
	f.Fuzz(func(t *testing.T, x int64, s string) {
		m := Money(clamp(x, -int64(MaxMoney), int64(MaxMoney)))
		for _, txt := range []string{FormatMoney(m), FormatINR(m), m.Plain()} {
			got, err := ParseMoney(txt)
			wantVal(t, txt, got, err, m)
		}
		q := Qty(clamp(x, -int64(MaxQty), int64(MaxQty)))
		for _, txt := range []string{FormatQty(q), q.Plain()} {
			got, err := ParseQty(txt)
			wantVal(t, txt, got, err, q)
		}
		r := Rate(clamp(x, -int64(MaxRate), int64(MaxRate)))
		for _, txt := range []string{FormatRate(r), r.Plain()} {
			got, err := ParseRate(txt)
			wantVal(t, txt, got, err, r)
		}
		p := Percent(clamp(x, 0, int64(MaxPercent)))
		for _, txt := range []string{FormatPercent(p), p.Plain()} {
			got, err := ParsePercent(txt)
			wantVal(t, txt, got, err, p)
		}
		fa := Factor(clamp(x, 1, int64(MaxFactor)))
		for _, txt := range []string{FormatFactor(fa), fa.Plain()} {
			got, err := ParseFactor(txt)
			wantVal(t, txt, got, err, fa)
		}

		mv, err := ParseMoney(s)
		parseResult(t, "ParseMoney", s, int64(mv), -int64(MaxMoney), int64(MaxMoney), err)
		qv, err := ParseQty(s)
		parseResult(t, "ParseQty", s, int64(qv), -int64(MaxQty), int64(MaxQty), err)
		rv, err := ParseRate(s)
		parseResult(t, "ParseRate", s, int64(rv), -int64(MaxRate), int64(MaxRate), err)
		pv, err := ParsePercent(s)
		parseResult(t, "ParsePercent", s, int64(pv), 0, int64(MaxPercent), err)
		fv, err := ParseFactor(s)
		parseResult(t, "ParseFactor", s, int64(fv), 1, int64(MaxFactor), err)
	})
}

func TestParse_AcceptedForms(t *testing.T) { // NUM-056
	in := []string{"1,41,12,000.00", "14112000", "1,41,12,000", "14,112,000.00", "₹ 1,234.50", "₹1,234.50", "  1234.5  ", ".5", "5.", "-1,234.50"}
	want := []Money{1411200000, 1411200000, 1411200000, 1411200000, 123450, 123450, 123450, 50, 500, -123450}
	for i, s := range in {
		m, err := ParseMoney(s)
		wantVal(t, s, m, err, want[i])
	}
}

func TestParse_RejectedForms(t *testing.T) { // NUM-057
	for _, s := range []string{"1.234,56", "12,34", "1,2345.00", "1e5", "1.5E+11", "(500)", "Rs. 500", "abc", "१२३", "1 234", "--5"} {
		_, err := ParseMoney(s)
		wantInputErr(t, err, `"`+s+`" is not a valid amount. Write it like 1,234.50 or 1234.50.`)
	}
	if _, err := ParseMoney(""); !errors.Is(err, ErrEmpty) {
		t.Fatalf("ParseMoney(\"\") error = %v, want ErrEmpty", err)
	}
}

func TestParse_DecimalLimitsPerType(t *testing.T) { // NUM-058
	_, err := ParseMoney("1.001")
	wantInputErr(t, err, "Amount can have at most 2 decimal places.")
	_, err = ParseQty("1.0001")
	wantInputErr(t, err, "Quantity can have at most 3 decimal places.")
	_, err = ParseRate("1.00001")
	wantInputErr(t, err, "Rate can have at most 4 decimal places.")
	_, err = ParsePercent("1.0001")
	wantInputErr(t, err, "Percentage can have at most 3 decimal places.")
	m, err := ParseMoney("1.000")
	wantVal(t, "1.000", m, err, Money(100))
}

func TestParse_NBSPAndNegativeZero(t *testing.T) { // Review Focus 2
	m, err := ParseMoney(" 1,234.50 ")
	wantVal(t, " 1,234.50 ", m, err, Money(123450))
	for _, s := range []string{"-0", "-0.00"} {
		z, err := ParseMoney(s)
		wantVal(t, s, z, err, Money(0))
		if got := FormatMoney(z); got != "0.00" {
			t.Fatalf("FormatMoney(ParseMoney(%q)) = %q, want %q", s, got, "0.00")
		}
	}
	_, err = ParseMoney("1 234")
	wantInputErr(t, err, "\"1 234\" is not a valid amount. Write it like 1,234.50 or 1234.50.")
	p, err := ParsePercent("18%")
	wantVal(t, "18%", p, err, Percent(18000))
}
