package print_test

import (
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/print"
)

func TestPartyAddress(t *testing.T) {
	cases := []struct {
		name, text, gstin string
		want              []string
	}{
		{"no snapshot", "", "29AAFCV2449G1Z5", nil},
		{"gstin line dropped", "12 MG Road\nBengaluru\nGSTIN: 29AAFCV2449G1Z5", "29AAFCV2449G1Z5", []string{"12 MG Road", "Bengaluru"}},
		{"other gstin kept", "12 MG Road\nGSTIN: 29AAFCV2449G1Z5", "27AAFCV2449G1Z5", []string{"12 MG Road", "GSTIN: 29AAFCV2449G1Z5"}},
		{"gstin line not last kept", "GSTIN: 29AAFCV2449G1Z5\nBengaluru", "29AAFCV2449G1Z5", []string{"GSTIN: 29AAFCV2449G1Z5", "Bengaluru"}},
		{"unregistered", "12 MG Road\nBengaluru", "", []string{"12 MG Road", "Bengaluru"}},
	}
	for _, c := range cases {
		if got := print.PartyAddress(c.text, c.gstin); !slices.Equal(got, c.want) || (got == nil) != (c.want == nil) {
			t.Errorf("%s: PartyAddress(%q, %q) = %q, want %q", c.name, c.text, c.gstin, got, c.want)
		}
	}
}
