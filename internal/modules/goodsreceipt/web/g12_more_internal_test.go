package grnweb

import (
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// MOD-GRN G12 · the two rules step 2's HTTP tests can't reach: every GRN
// More field is plain text with only a length rule, so an error there
// always comes with a value, and the tick is always shown, so step 2 never
// shows only More inputs.

// renderDelivery is step 2's view of ins.
func renderDelivery(t *testing.T, ins ...docscreen.MoreInput) string {
	t.Helper()
	var b strings.Builder
	if err := deliveryView(ins).Render(t.Context(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// An error alone opens the group, as docscreen's moreBox does.
func TestGRNWeb_MoreFieldsErrorOpens(t *testing.T) {
	plain := moreInput(doctype.Field{Label: "Challan or invoice no."}, components.FieldProps{}, templ.NopComponent)
	vehicle := moreInput(doctype.Field{Label: "Vehicle no.", More: true}, components.FieldProps{}, templ.NopComponent)
	if html := renderDelivery(t, plain, vehicle); !strings.Contains(html, "<details") || strings.Contains(html, " open") {
		t.Fatalf("want a closed group with no value or error, got %s", html)
	}
	transporter := moreInput(doctype.Field{Label: "Transporter", More: true}, components.FieldProps{Error: "Transporter can be at most 140 characters."}, templ.NopComponent)
	if html := renderDelivery(t, plain, vehicle, transporter); !strings.Contains(html, "<details") || !strings.Contains(html, " open") {
		t.Errorf("an error inside left the group closed: %s", html)
	}
}

// CD-363: when every shown input is More, they show without a group.
func TestGRNWeb_DeliveryAllMoreShowsNoGroup(t *testing.T) {
	remarks := moreInput(doctype.Field{Label: "Remarks", More: true}, components.FieldProps{}, templ.Raw(`<textarea name="remarks"></textarea>`))
	html := renderDelivery(t, remarks)
	if !strings.Contains(html, `name="remarks"`) || strings.Contains(html, "<details") {
		t.Errorf("only More inputs: want them shown without a group, got %s", html)
	}
}
