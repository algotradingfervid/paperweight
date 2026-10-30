package gstin

import (
	"strings"
	"testing"
)

// fuzzPrefix turns any input into a 14-character prefix over the alphabet:
// bytes already in it are kept, others are folded onto it, short inputs are
// padded with '0'.
func fuzzPrefix(raw string) string {
	var b strings.Builder
	for i := range 14 {
		c := byte('0')
		if i < len(raw) {
			c = raw[i]
		}
		if strings.IndexByte(alphabet, c) < 0 {
			c = alphabet[int(c)%len(alphabet)]
		}
		b.WriteByte(c)
	}
	return b.String()
}

func FuzzGSTIN_CheckDigit(f *testing.F) { // MST-032
	for _, seed := range []string{
		"27AABCS1429B1Z", "24AAACR5055K1Z", "29AABCF8078M1C", "9917SGP29001OS", "0717UNO00157UN", // MST-031
		"07DELA01234E1D", "00000000000000", "ZZZZZZZZZZZZZZ", "", "lower case 123",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		prefix := fuzzPrefix(raw)
		g := prefix + string(CheckDigit(prefix))
		if err := ValidateGSTIN(g); err != nil {
			t.Fatalf("ValidateGSTIN(%s): %v; want nil", g, err)
		}
		for i := range len(g) {
			for j := range len(alphabet) {
				if alphabet[j] == g[i] {
					continue
				}
				changed := g[:i] + string(alphabet[j]) + g[i+1:]
				want := "GSTIN " + changed + " is not valid: the last character doesn't match. Check for typing mistakes."
				if err := ValidateGSTIN(changed); err == nil || err.Error() != want {
					t.Fatalf("ValidateGSTIN(%s), %s with character %d changed = %v; want %q", changed, g, i+1, err, want)
				}
			}
		}
	})
}
