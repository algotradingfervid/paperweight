package print

import (
	"errors"

	"github.com/vardhan/internaltools/internal/types"
)

// MaxWordsAmount is the largest amount InWordsINR writes: 10^14 − 1 paise.
const MaxWordsAmount types.Money = 99_999_999_999_999

var (
	ErrNegativeAmount = errors.New("Amount in words needs an amount of 0 or more.")
	ErrAmountTooLarge = errors.New("Amount is too large to write in words.")
)

var (
	ones = [...]string{
		"Zero", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine",
		"Ten", "Eleven", "Twelve", "Thirteen", "Fourteen", "Fifteen", "Sixteen", "Seventeen", "Eighteen", "Nineteen",
	}
	tens = [...]string{"", "", "Twenty", "Thirty", "Forty", "Fifty", "Sixty", "Seventy", "Eighty", "Ninety"}
)

// InWordsINR writes an amount in paise as Indian-scale words, for example
// 1411280 → "Rupees Fourteen Thousand One Hundred Twelve and Paise Eighty Only".
func InWordsINR(m types.Money) (string, error) {
	if m < 0 {
		return "", ErrNegativeAmount
	}
	if m > MaxWordsAmount {
		return "", ErrAmountTooLarge
	}
	rupees, paise := int64(m)/100, int64(m)%100
	switch {
	case rupees == 0 && paise == 0:
		return "Rupees Zero Only", nil
	case rupees == 0:
		return "Paise " + words(paise) + " Only", nil
	case paise == 0:
		return "Rupees " + words(rupees) + " Only", nil
	}
	return "Rupees " + words(rupees) + " and Paise " + words(paise) + " Only", nil
}

// words writes n > 0 with the crore, lakh, thousand and hundred scale.
func words(n int64) string {
	for _, s := range []struct {
		size int64
		name string
	}{{10_000_000, "Crore"}, {100_000, "Lakh"}, {1_000, "Thousand"}, {100, "Hundred"}} {
		if n >= s.size {
			out := words(n/s.size) + " " + s.name
			if rest := n % s.size; rest > 0 {
				out += " " + words(rest)
			}
			return out
		}
	}
	if n < 20 {
		return ones[n]
	}
	if n%10 == 0 {
		return tens[n/10]
	}
	return tens[n/10] + " " + ones[n%10]
}
