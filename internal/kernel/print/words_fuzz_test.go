package print_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/types"
)

// FILE-081
func FuzzInWordsINR_RoundTrip(f *testing.F) {
	for _, seed := range []int64{0, 1, 99, 100, 1411280, 1234567890, 99999999999999} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, x int64) {
		v := x % 100_000_000_000_000 // 1e14; the remainder of MinInt64 fits, its negation does too
		if v < 0 {
			v = -v
		}
		m := types.Money(v)

		s, err := print.InWordsINR(m)
		if err != nil {
			t.Fatalf("InWordsINR(%d) error = %v, want nil", m, err)
		}
		if !strings.HasPrefix(s, "Rupees ") && !strings.HasPrefix(s, "Paise ") {
			t.Errorf("InWordsINR(%d) = %q, want prefix \"Rupees \" or \"Paise \"", m, s)
		}
		if !strings.HasSuffix(s, " Only") {
			t.Errorf("InWordsINR(%d) = %q, want suffix \" Only\"", m, s)
		}
		if strings.Contains(s, "  ") {
			t.Errorf("InWordsINR(%d) = %q contains a double space", m, s)
		}
		if strings.Contains(s, "-") {
			t.Errorf("InWordsINR(%d) = %q contains a hyphen", m, s)
		}
		for _, w := range strings.Split(s, " ") {
			if w == "and" { // the one joining word, " and Paise "
				continue
			}
			if w == "" || !unicode.IsUpper([]rune(w)[0]) {
				t.Errorf("InWordsINR(%d) = %q has word %q not starting upper-case", m, s, w)
			}
		}
		if n := strings.Count(s, " and Paise "); n > 1 {
			t.Errorf("InWordsINR(%d) = %q contains \" and Paise \" %d times, want at most 1", m, s, n)
		}
		checkShape(t, s)

		back, err := parseWordsINR(s)
		if err != nil {
			t.Fatalf("parseWordsINR(%q) error = %v (from %d)", s, err, m)
		}
		if back != m {
			t.Errorf("parseWordsINR(InWordsINR(%d)) = %d, words %q", m, back, s)
		}
	})
}

var wordsSmall = map[string]int64{
	"One": 1, "Two": 2, "Three": 3, "Four": 4, "Five": 5, "Six": 6, "Seven": 7, "Eight": 8, "Nine": 9,
	"Ten": 10, "Eleven": 11, "Twelve": 12, "Thirteen": 13, "Fourteen": 14, "Fifteen": 15,
	"Sixteen": 16, "Seventeen": 17, "Eighteen": 18, "Nineteen": 19,
	"Twenty": 20, "Thirty": 30, "Forty": 40, "Fifty": 50, "Sixty": 60, "Seventy": 70, "Eighty": 80, "Ninety": 90,
}

// parseWordsINR is the test-only inverse of InWordsINR (FILE-081).
func parseWordsINR(s string) (types.Money, error) {
	body, ok := strings.CutSuffix(s, " Only")
	if !ok {
		return 0, fmt.Errorf("no \" Only\" suffix")
	}
	if rest, ok := strings.CutPrefix(body, "Paise "); ok {
		p, err := parseNumberWords(rest)
		if err != nil {
			return 0, err
		}
		if p < 1 || p > 99 {
			return 0, fmt.Errorf("paise-only amount %d out of 1..99", p)
		}
		return types.Money(p), nil
	}
	rest, ok := strings.CutPrefix(body, "Rupees ")
	if !ok {
		return 0, fmt.Errorf("no \"Rupees \" or \"Paise \" prefix")
	}
	rupeePart, paisePart, hasPaise := strings.Cut(rest, " and Paise ")
	var rupees int64
	if rupeePart == "Zero" {
		if hasPaise {
			return 0, fmt.Errorf("\"Rupees Zero\" with a paise clause")
		}
	} else {
		r, err := parseNumberWords(rupeePart)
		if err != nil {
			return 0, err
		}
		rupees = r
	}
	var paise int64
	if hasPaise {
		p, err := parseNumberWords(paisePart)
		if err != nil {
			return 0, err
		}
		if p < 1 || p > 99 {
			return 0, fmt.Errorf("paise clause %d out of 1..99", p)
		}
		paise = p
	}
	return types.Money(rupees*100 + paise), nil
}

// parseNumberWords reads number words with multipliers Hundred, Thousand,
// Lakh and Crore. Thousand and Lakh close the current group; Crore multiplies
// everything accumulated before it.
func parseNumberWords(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty number")
	}
	var total, group, cur int64
	for _, w := range strings.Split(s, " ") {
		if v, ok := wordsSmall[w]; ok {
			cur += v
			continue
		}
		switch w {
		case "Hundred":
			cur *= 100
		case "Thousand":
			group += cur * 1_000
			cur = 0
		case "Lakh":
			group += cur * 100_000
			cur = 0
		case "Crore":
			total = (total + group + cur) * 10_000_000
			group, cur = 0, 0
		default:
			return 0, fmt.Errorf("unknown word %q", w)
		}
	}
	return total + group + cur, nil
}
