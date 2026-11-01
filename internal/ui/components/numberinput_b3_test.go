package components_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// CD-336 (2), UI v2 §5.7: every number input carries its kind's scale for
// live grouping (money 2, quantity 3 or 0 for a whole unit, rate 4, percent
// 3, factor 9); money and rate show "₹" inside the box at the start, hidden
// from assistive tech; quantity, percent and factor show no "₹".
func TestNumberInput_ScaleAndRupee(t *testing.T) {
	p := components.FieldProps{ID: "f-n", Name: "n", Label: "N", Value: "1"}
	for _, c := range []struct {
		name  string
		in    templ.Component
		scale string
		rupee bool
	}{
		{"MoneyInput", components.MoneyInput(p), "2", true},
		{"RateInput", components.RateInput(p), "4", true},
		{"QtyInput", components.QtyInput(p, false, "kg"), "3", false},
		{"QtyInput whole", components.QtyInput(p, true, "Bag"), "0", false},
		{"LargeQtyInput", components.LargeQtyInput(p, false, "kg"), "3", false},
		{"LargeQtyInput whole", components.LargeQtyInput(p, true, "kg"), "0", false},
		{"PercentInput", components.PercentInput(p), "3", false},
		{"NumberInput money", components.NumberInput(p, components.Money, false), "2", true},
		{"NumberInput qty", components.NumberInput(p, components.Qty, false), "3", false},
		{"NumberInput rate", components.NumberInput(p, components.Rate, false), "4", true},
		{"NumberInput factor", components.NumberInput(p, components.Factor, false), "9", false},
	} {
		doc := uitest.Render(t, c.in)
		in := doc.Find("input#f-n")
		if got, ok := in.Attr("data-scale"); !ok || got != c.scale {
			t.Errorf("%s: data-scale = %q (set %v), want %q", c.name, got, ok, c.scale)
		}
		if _, signed := in.Attr("data-signed"); signed {
			t.Errorf("%s: data-signed set without Signed", c.name)
		}
		pre := doc.Find(".field__control > .field__pre")
		if !c.rupee {
			if pre.Length() != 0 || strings.Contains(doc.Text(), "₹") {
				t.Errorf("%s: has a rupee prefix", c.name)
			}
			continue
		}
		if pre.Length() != 1 || strings.TrimSpace(pre.Text()) != "₹" {
			t.Fatalf("%s: prefix = %d %q, want one ₹", c.name, pre.Length(), pre.Text())
		}
		if got, _ := pre.Attr("aria-hidden"); got != "true" {
			t.Errorf("%s: prefix aria-hidden = %q", c.name, got)
		}
		// The input stays the control's first child, so daisyUI's join keeps
		// its start corners round.
		if !doc.Find(".field__control > :first-child").Is("input#f-n") {
			t.Errorf("%s: the input isn't the control's first child", c.name)
		}
	}
}

// CD-336 (2): Signed marks a number field that may be negative; live grouping
// keeps a typed minus only there. It keeps the caller's other attributes.
func TestSigned(t *testing.T) {
	p := components.FieldProps{ID: "f-n", Name: "n", Label: "N", Attrs: templ.Attributes{"data-x": "1"}}
	in := uitest.Render(t, components.MoneyInput(components.Signed(p))).Find("input#f-n")
	if _, ok := in.Attr("data-signed"); !ok {
		t.Error("Signed field lacks data-signed")
	}
	if got, _ := in.Attr("data-x"); got != "1" {
		t.Errorf("Signed dropped the caller's attribute: data-x = %q", got)
	}
	if _, ok := p.Attrs["data-signed"]; ok {
		t.Error("Signed changed the caller's Attrs map")
	}
}

// CD-336 (2), CD-48: the server stays the parser. A value grouped as the
// user typed it, with commas, checks and comes back in NUM's format.
func TestValidateHandler_GroupedValues(t *testing.T) {
	h := components.ValidateHandler(components.NUMParsers())
	for _, c := range []struct{ kind, in, want string }{
		{"money", "1,50,000.5", "1,50,000.50"},
		{"money", "-1,50,000.5", "-1,50,000.50"},
		{"qty", "1,00,000.125", "1,00,000.125"},
		{"rate", "9,99,999.9999", "9,99,999.9999"},
	} {
		rec := uitest.Do(t, h, http.MethodPost, "/ui/validate/"+c.kind, url.Values{"_id": {"f-n"}, "_name": {"n"}, "n": {c.in}})
		if ev := trigger(t, rec.Header()); ev.Field.Value != c.want || ev.Field.Invalid {
			t.Errorf("%s %q: trigger = %+v, want value %q", c.kind, c.in, ev.Field, c.want)
		}
	}
}
