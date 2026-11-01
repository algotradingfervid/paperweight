package grnweb

import (
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// MOD-GRN G12 · the two rules step 2's HTTP tests can't reach: every GRN
// More field is plain text with only a length rule, so an error there
// always comes with a value, and the tick is always shown, so step 2 never
// shows only More inputs.

// An error alone opens the group, as docscreen's moreBox does.
func TestGRNWeb_MoreFieldsErrorOpens(t *testing.T) {
	var m moreFields
	m.add(doctype.Field{Label: "Vehicle no."}, "", "", templ.NopComponent)
	if m.open {
		t.Fatalf("open with no value or error")
	}
	m.add(doctype.Field{Label: "Transporter"}, "", "Transporter can be at most 140 characters.", templ.NopComponent)
	if !m.open {
		t.Errorf("an error inside left the group closed")
	}
}

// CD-363: when every shown input is More, they show without a group.
func TestGRNWeb_DeliveryAllMoreShowsNoGroup(t *testing.T) {
	var m moreFields
	m.add(doctype.Field{Label: "Remarks"}, "", "", templ.Raw(`<textarea name="remarks"></textarea>`))
	var b strings.Builder
	if err := deliveryView(deliveryForm{More: m}).Render(t.Context(), &b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `name="remarks"`) || strings.Contains(b.String(), "<details") {
		t.Errorf("only More inputs: want them shown without a group, got %s", b.String())
	}
}
