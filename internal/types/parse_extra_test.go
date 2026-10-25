package types

import "testing"

// A negative factor is refused as not positive, however many digits it has.
func TestParseFactor_NegativeTooManyDigits(t *testing.T) {
	for _, s := range []string{"-99999999999", "-5"} {
		_, err := ParseFactor(s)
		wantInputErr(t, err, "Conversion factor must be more than 0.")
	}
}
