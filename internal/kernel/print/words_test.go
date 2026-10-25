package print_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/types"
)

// wordsRow is one paise → words row.
type wordsRow struct {
	name string
	in   types.Money
	want string
}

// checkShape asserts the output never uses the western scale, a hyphen or the
// misspelling "Fourty" (FILE-075, FILE-077).
func checkShape(t *testing.T, s string) {
	t.Helper()
	for _, bad := range []string{"Million", "-", "Fourty"} {
		if strings.Contains(s, bad) {
			t.Errorf("InWordsINR output %q contains %q", s, bad)
		}
	}
}

func runWordsRows(t *testing.T, rows []wordsRow) {
	t.Helper()
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			got, err := print.InWordsINR(r.in)
			if err != nil {
				t.Fatalf("InWordsINR(%d) error = %v, want nil", r.in, err)
			}
			if got != r.want {
				t.Errorf("InWordsINR(%d) = %q, want %q", r.in, got, r.want)
			}
			checkShape(t, got)
		})
	}
}

// rupeeRows turns whole-rupee amounts into rows "Rupees <words> Only".
func rupeeRows(rupees []int64, words []string) []wordsRow {
	rows := make([]wordsRow, len(rupees))
	for i, r := range rupees {
		rows[i] = wordsRow{name: words[i], in: types.Money(r * 100), want: "Rupees " + words[i] + " Only"}
	}
	return rows
}

// FILE-072
func TestInWordsINR(t *testing.T) {
	runWordsRows(t, []wordsRow{
		{"brief example", 1411280, "Rupees Fourteen Thousand One Hundred Twelve and Paise Eighty Only"},
		{"max", 99999999999999, "Rupees Ninety Nine Thousand Nine Hundred Ninety Nine Crore Ninety Nine Lakh Ninety Nine Thousand Nine Hundred Ninety Nine and Paise Ninety Nine Only"},
	})
}

// FILE-073
func TestInWordsINR_ZeroAndPaiseOnly(t *testing.T) {
	runWordsRows(t, []wordsRow{
		{"zero", 0, "Rupees Zero Only"},
		{"1 paise", 1, "Paise One Only"},
		{"5 paise", 5, "Paise Five Only"},
		{"10 paise", 10, "Paise Ten Only"},
		{"50 paise", 50, "Paise Fifty Only"},
		{"80 paise", 80, "Paise Eighty Only"},
		{"99 paise", 99, "Paise Ninety Nine Only"},
	})
}

// FILE-074
func TestInWordsINR_WholeRupees(t *testing.T) {
	runWordsRows(t, []wordsRow{
		{"one rupee", 100, "Rupees One Only"},
		{"twenty one rupees", 2100, "Rupees Twenty One Only"},
		{"five thousand rupees", 500000, "Rupees Five Thousand Only"},
	})
}

// FILE-075
func TestInWordsINR_BelowThousand(t *testing.T) {
	runWordsRows(t, rupeeRows(
		[]int64{11, 15, 19, 20, 40, 99, 100, 101, 110, 115, 999},
		[]string{
			"Eleven", "Fifteen", "Nineteen", "Twenty", "Forty",
			"Ninety Nine", "One Hundred", "One Hundred One", "One Hundred Ten", "One Hundred Fifteen", "Nine Hundred Ninety Nine",
		},
	))
}

// FILE-076
func TestInWordsINR_Thousands(t *testing.T) {
	runWordsRows(t, rupeeRows(
		[]int64{1000, 1001, 1010, 10000, 99999},
		[]string{
			"One Thousand", "One Thousand One", "One Thousand Ten", "Ten Thousand",
			"Ninety Nine Thousand Nine Hundred Ninety Nine",
		},
	))
}

// FILE-077
func TestInWordsINR_Lakh(t *testing.T) {
	runWordsRows(t, rupeeRows(
		[]int64{100000, 100001, 110000, 1234567, 9999999},
		[]string{
			"One Lakh", "One Lakh One", "One Lakh Ten Thousand",
			"Twelve Lakh Thirty Four Thousand Five Hundred Sixty Seven",
			"Ninety Nine Lakh Ninety Nine Thousand Nine Hundred Ninety Nine",
		},
	))
}

// FILE-078
func TestInWordsINR_Crore(t *testing.T) {
	runWordsRows(t, []wordsRow{
		{"one crore", 1000000000, "Rupees One Crore Only"},
		{"crore with paise", 1234567890, "Rupees One Crore Twenty Three Lakh Forty Five Thousand Six Hundred Seventy Eight and Paise Ninety Only"},
		{"hundred crore", 100000000000, "Rupees One Hundred Crore Only"},
		{"thousands of crore", 9999999999999, "Rupees Nine Thousand Nine Hundred Ninety Nine Crore Ninety Nine Lakh Ninety Nine Thousand Nine Hundred Ninety Nine and Paise Ninety Nine Only"},
	})
}

// FILE-079
func TestInWordsINR_Errors(t *testing.T) {
	if print.MaxWordsAmount != 99_999_999_999_999 {
		t.Errorf("MaxWordsAmount = %d, want 99999999999999", print.MaxWordsAmount)
	}

	errRows := []struct {
		name    string
		in      types.Money
		wantErr error
		wantMsg string
	}{
		{"negative", -100, print.ErrNegativeAmount, "Amount in words needs an amount of 0 or more."},
		{"too large", 100_000_000_000_000, print.ErrAmountTooLarge, "Amount is too large to write in words."},
	}
	for _, r := range errRows {
		t.Run(r.name, func(t *testing.T) {
			_, err := print.InWordsINR(r.in)
			if !errors.Is(err, r.wantErr) {
				t.Fatalf("InWordsINR(%d) error = %v, want errors.Is %v", r.in, err, r.wantErr)
			}
			if err.Error() != r.wantMsg {
				t.Errorf("InWordsINR(%d) error text = %q, want %q", r.in, err.Error(), r.wantMsg)
			}
		})
	}

	t.Run("largest accepted", func(t *testing.T) {
		got, err := print.InWordsINR(99_999_999_999_999)
		if err != nil {
			t.Fatalf("InWordsINR(99999999999999) error = %v, want nil", err)
		}
		checkShape(t, got)
	})
}

// FILE-080
func TestInWordsINR_NoFloatDrift(t *testing.T) {
	runWordsRows(t, []wordsRow{
		{"28 paise", 28, "Paise Twenty Eight Only"},
		{"29 paise", 29, "Paise Twenty Nine Only"},
	})

	a, err := print.InWordsINR(1411279)
	if err != nil {
		t.Fatalf("InWordsINR(1411279) error = %v, want nil", err)
	}
	b, err := print.InWordsINR(1411280)
	if err != nil {
		t.Fatalf("InWordsINR(1411280) error = %v, want nil", err)
	}
	checkShape(t, a)
	checkShape(t, b)
	if a == b {
		t.Errorf("InWordsINR(1411279) and InWordsINR(1411280) both = %q, want different words", a)
	}
}
