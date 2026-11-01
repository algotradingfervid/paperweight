package docscreen_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// CD-336 (2): live grouping keeps a typed minus only where the field allows
// negatives. A money, quantity or rate field is signed unless it is
// NonNegative or Positive; percent and factor never are (their parsers refuse
// negatives).
func TestFieldInput_SignedNumbers(t *testing.T) {
	for _, c := range []struct {
		name   string
		f      doctype.Field
		signed bool
	}{
		{"money", doctype.Field{Type: doctype.Money}, true},
		{"money non-negative", doctype.Field{Type: doctype.Money, NonNegative: true}, false},
		{"money positive", doctype.Field{Type: doctype.Money, Positive: true}, false},
		{"qty", doctype.Field{Type: doctype.Qty}, true},
		{"qty non-negative", doctype.Field{Type: doctype.Qty, NonNegative: true}, false},
		{"qty positive", doctype.Field{Type: doctype.Qty, Positive: true}, false},
		{"rate", doctype.Field{Type: doctype.Rate}, true},
		{"rate non-negative", doctype.Field{Type: doctype.Rate, NonNegative: true}, false},
		{"rate positive", doctype.Field{Type: doctype.Rate, Positive: true}, false},
		{"percent", doctype.Field{Type: doctype.Percent}, false},
		{"factor", doctype.Field{Type: doctype.Factor}, false},
		{"text", doctype.Field{Type: doctype.Text}, false},
	} {
		c.f.Name, c.f.Label = "n", "N"
		in := uitest.Render(t, docscreen.FieldInput(c.f, "f-n", "", "", docscreen.Deps{})).Find("input#f-n")
		if in.Length() != 1 {
			t.Fatalf("%s: no input#f-n", c.name)
		}
		if _, got := in.Attr("data-signed"); got != c.signed {
			t.Errorf("%s: data-signed set = %v, want %v", c.name, got, c.signed)
		}
	}
}

// CD-336 (2), CD-48: a posted value keeps the commas live grouping put in;
// ToPlain (every docscreen save) parses it.
func TestToPlain_GroupedValues(t *testing.T) {
	for _, c := range []struct {
		typ       doctype.FieldType
		in, plain string
	}{
		{doctype.Money, "1,50,000.5", "150000.50"},
		{doctype.Money, "-1,50,000.50", "-150000.50"},
		{doctype.Qty, "1,00,000.125", "100000.125"},
		{doctype.Rate, "9,99,999.9999", "999999.9999"},
	} {
		got, fe := docscreen.ToPlain(doctype.Field{Name: "n", Label: "N", Type: c.typ}, c.in)
		if fe != nil || got != c.plain {
			t.Errorf("ToPlain(%v, %q) = %q, %v; want %q", c.typ, c.in, got, fe, c.plain)
		}
	}
}
