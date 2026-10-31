package components_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// TestLargeQtyInput (HOOK-047, CD-286 Q4): LargeQtyInput is QtyInput with the
// large control class; the input's id, name, inputmode, hx-post and the unit
// suffix are QtyInput's.
func TestLargeQtyInput(t *testing.T) {
	for _, whole := range []bool{false, true} {
		p := components.FieldProps{ID: "f-lines-1-received", Name: "lines[1].received", Label: "Received now", Value: "9,99,999.9999"}
		large := uitest.Render(t, components.LargeQtyInput(p, whole, "kg"))
		plain := uitest.Render(t, components.QtyInput(p, whole, "kg"))

		if large.Find(".field__control.field__control--lg input.num").Length() != 1 {
			t.Errorf("whole=%v: the input is not inside a .field__control--lg control", whole)
		}
		if plain.Find(".field__control--lg").Length() != 0 {
			t.Errorf("whole=%v: QtyInput has the large class", whole)
		}
		li, pi := large.Find("input"), plain.Find("input")
		if li.Length() != 1 || pi.Length() != 1 {
			t.Fatalf("whole=%v: inputs = %d large, %d plain", whole, li.Length(), pi.Length())
		}
		for _, a := range []string{"id", "name", "inputmode", "hx-post"} {
			lv, lok := li.Attr(a)
			pv, pok := pi.Attr(a)
			if !lok || lv != pv || pok != lok {
				t.Errorf("whole=%v: %s = %q (set %v), QtyInput's = %q (set %v)", whole, a, lv, lok, pv, pok)
			}
		}
		if got, _ := li.Attr("hx-post"); got != "/ui/validate/qty" {
			t.Errorf("whole=%v: hx-post = %q", whole, got)
		}
		ls := strings.TrimSpace(large.Find(".field__suffix").Text())
		if ps := strings.TrimSpace(plain.Find(".field__suffix").Text()); ls != ps || ls != "kg" {
			t.Errorf("whole=%v: suffix = %q, QtyInput's = %q", whole, ls, ps)
		}
	}
}
