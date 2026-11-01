package docscreen_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// b1cFields are one field per control kind that takes a typed value.
var b1cFields = []doctype.Field{
	{Name: "ref", Label: "Reference", Type: doctype.Text, MaxLen: 40},
	{Name: "count", Label: "Count", Type: doctype.Int},
	{Name: "notes", Label: "Notes", Type: doctype.Text, MaxLen: 500},
	{Name: "total", Label: "Total", Type: doctype.Money},
	{Name: "qty", Label: "Quantity", Type: doctype.Qty},
	{Name: "rate", Label: "Rate", Type: doctype.Rate},
	{Name: "tax", Label: "Tax", Type: doctype.Percent},
	{Name: "factor", Label: "Factor", Type: doctype.Factor},
	{Name: "bill_date", Label: "Bill date", Type: doctype.Date},
	{Name: "vendor", Label: "Vendor", Type: doctype.Link, Link: "party.Party"},
}

// b1cPlaceholders renders f empty and returns every placeholder= value in
// the HTML, in order, so a second attribute beside the first shows up (an
// HTML parser keeps only the first). The split relies on templ escaping '"'
// inside attribute values.
func b1cPlaceholders(t *testing.T, f doctype.Field) []string {
	t.Helper()
	var b bytes.Buffer
	if err := docscreen.FieldInput(f, "f-"+f.Name, "", "", docscreen.Deps{}).Render(t.Context(), &b); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, part := range strings.Split(b.String(), ` placeholder="`)[1:] {
		got = append(got, part[:strings.IndexByte(part, '"')])
	}
	return got
}

// Field.Placeholder reaches the input of every control kind but Date, and a Link's
// Picker search box in place of "Type to search" (CD-370, UI v2 §5.5 item 5,
// §5.8).
func TestFieldInput_Placeholder(t *testing.T) {
	for _, f := range b1cFields {
		if f.Type == doctype.Date {
			continue // TestFieldInput_DateIgnoresPlaceholder
		}
		f.Placeholder = "e.g. " + f.Label + " 42"
		doc := uitest.Render(t, docscreen.FieldInput(f, "f-"+f.Name, "", "", docscreen.Deps{}))
		if got, _ := doc.Find("#f-" + f.Name).Attr("placeholder"); got != f.Placeholder {
			t.Errorf("%s: placeholder = %q, want %q", f.Name, got, f.Placeholder)
		}
		if got := b1cPlaceholders(t, f); len(got) != 1 || got[0] != f.Placeholder {
			t.Errorf("%s: placeholder attributes %q, want only %q", f.Name, got, f.Placeholder)
		}
	}
}

// A Date ignores Field.Placeholder and keeps exactly one placeholder,
// dd-mm-yyyy (UI v2 §5.5 item 5).
func TestFieldInput_DateIgnoresPlaceholder(t *testing.T) {
	f := doctype.Field{Name: "bill_date", Label: "Bill date", Type: doctype.Date, Placeholder: "e.g. 27-03-2026"}
	if got := b1cPlaceholders(t, f); len(got) != 1 || got[0] != "dd-mm-yyyy" {
		t.Errorf("placeholder attributes %q, want only dd-mm-yyyy", got)
	}
}

// A chosen Link's search box carries the placeholder too.
func TestFieldInput_PlaceholderChosenLink(t *testing.T) {
	f := doctype.Field{Name: "vendor", Label: "Vendor", Type: doctype.Link, Link: "party.Party", Placeholder: "e.g. Shree Steel"}
	doc := uitest.Render(t, docscreen.FieldInput(f, "f-vendor", "7", "", docscreen.Deps{}))
	if got, _ := doc.Find("input#f-vendor-search").Attr("placeholder"); got != "e.g. Shree Steel" {
		t.Errorf("placeholder = %q, want e.g. Shree Steel", got)
	}
}

// Without a Placeholder each control keeps what it showed before: none on
// the text and number controls (no empty attribute), dd-mm-yyyy on a Date, and
// "Type to search" on a Link.
func TestFieldInput_NoPlaceholder(t *testing.T) {
	want := map[doctype.FieldType][]string{
		doctype.Date: {"dd-mm-yyyy"},
		doctype.Link: {"Type to search"},
	}
	for _, f := range b1cFields {
		got := b1cPlaceholders(t, f)
		if strings.Join(got, "|") != strings.Join(want[f.Type], "|") {
			t.Errorf("%s: placeholder attributes %q, want %q", f.Name, got, want[f.Type])
		}
	}
}

// Check and Select controls (switch, segmented, tiles, select) ignore
// Field.Placeholder: none of them renders a placeholder attribute.
func TestFieldInput_PlaceholderIgnoredOnChoices(t *testing.T) {
	for _, f := range []doctype.Field{
		{Name: "is_stock", Label: "Stock item", Type: doctype.Check},
		selectField("Yes", "No"),
		selectField("Vendor only", "Vendor and customer"),
		selectField("A", "B", "C", "D", "E", "F"),
	} {
		f.Placeholder = "e.g. X"
		if got := b1cPlaceholders(t, f); len(got) != 0 {
			t.Errorf("%s %v: placeholder attributes %q, want none", f.Name, f.OptionLabels, got)
		}
	}
}
