package uitest

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLongValues_Shapes(t *testing.T) {
	for _, c := range []struct {
		name string
		s    string
		n    int
	}{
		{"LongName100", LongName100, 100},
		{"LongCode60", LongCode60, 60},
		{"LongEmail", LongEmail, 253},
		{"LongError", LongError, 400},
		{"LongDevanagari", LongDevanagari, 40},
	} {
		if got := utf8.RuneCountInString(c.s); got != c.n {
			t.Errorf("%s has %d runes, want %d", c.name, got, c.n)
		}
	}
	if !regexp.MustCompile(`^[A-Z0-9]{60}$`).MatchString(LongCode60) {
		t.Errorf("LongCode60 = %q, want 60 of [A-Z0-9]", LongCode60)
	}
	if !strings.Contains(LongError, LongCode60) {
		t.Error("LongError does not contain LongCode60")
	}
	if MaxMoney != "₹99,99,99,999.99" {
		t.Errorf("MaxMoney = %q", MaxMoney)
	}
	lines := Lines(500)
	if len(lines) != 500 {
		t.Fatalf("len(Lines(500)) = %d", len(lines))
	}
	if l := lines[499]; l.Item != LongName100 || l.Qty != "1,00,000 kg" || l.Rate != "₹9,99,999.9999" || l.Amount != MaxMoney {
		t.Errorf("Lines(500)[499] = %+v", l)
	}
}
