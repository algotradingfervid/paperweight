package mstweb

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// No case ID: UI v2 B1b, A-9 on master forms. Each of input's own controls
// (the ones docscreen.FieldInput doesn't draw) takes Required and Tip from the
// master field: the asterisk, the hidden " (required)", aria-required and the
// info "i"; an optional field has none of them and no "(optional)".
func TestInput_RequiredAndTipFromField(t *testing.T) {
	type rec = map[string]string
	h := &handlers[rec, rec]{Screen: Screen[rec, rec]{
		Lookups:      map[string]Lookup{"look": {}},
		Codes:        map[string]components.CodeKind{"code": components.GSTIN},
		Numbers:      map[string]components.NumberKind{"num": components.Money},
		OptionLabels: map[string]map[string]string{"opt": {"a": "A"}},
		Selects:      map[string][]components.Option{"sel": {{Value: "a", Label: "A"}}},
	}}
	for _, name := range []string{"look", "code", "num", "opt", "sel"} {
		for _, required := range []bool{true, false} {
			fd := doctype.Field{Name: name, Label: "Label " + name, Type: doctype.Text, Options: []string{"a"}, Required: required}
			if required {
				fd.Tip = "Tip for " + name
			}
			var b strings.Builder
			if err := h.input(fd, name, form{}).Render(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(b.String()))
			if err != nil {
				t.Fatal(err)
			}
			label := doc.Find(".field__label").Text()
			if strings.Contains(label, "(optional)") {
				t.Errorf("%s: label %q has (optional)", name, label)
			}
			if got := strings.Contains(label, "(required)"); got != required {
				t.Errorf("%s required=%v: label %q has (required) = %v", name, required, label, got)
			}
			if got := doc.Find(`[aria-required="true"]`).Length() > 0; got != required {
				t.Errorf("%s required=%v: aria-required = %v", name, required, got)
			}
			if got := strings.Contains(b.String(), "Tip for "+name); got != required {
				t.Errorf("%s: tip shown = %v; want %v", name, got, required)
			}
		}
	}
}
