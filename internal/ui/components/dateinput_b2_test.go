package components_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// CD-336 (1), A-15: DateInput has a "Choose date" button inside the control
// box beside the typed text, and a hidden date input for the platform picker
// that posts nothing, isn't tabbable and carries the field's bounds as ISO dates.
func TestDateInput_CalendarButton(t *testing.T) {
	p := components.DateProps{
		FieldProps: components.FieldProps{ID: "f-d", Name: "d", Label: "Bill date", Value: "27-03-2026"},
		Today:      "24-10-2026", Yesterday: "23-10-2026",
		Min: "01-04-2025", Max: "24-10-2026",
	}
	doc := uitest.Render(t, components.DateInput(p))
	box := doc.Find(".field__date")
	if box.Length() != 1 || box.Find("input#f-d[type=text]").Length() != 1 {
		t.Fatalf("no .field__date box holding the text input")
	}
	if got, _ := box.Find("input#f-d").Attr("placeholder"); got != "dd-mm-yyyy" {
		t.Errorf("placeholder = %q", got)
	}
	btn := box.Find(`button[type=button][data-date-pick]`)
	if btn.Length() != 1 {
		t.Fatalf("%d calendar buttons in the box, want 1", btn.Length())
	}
	if got, _ := btn.Attr("aria-label"); got != "Choose date" {
		t.Errorf("button aria-label = %q", got)
	}
	if got, _ := btn.Attr("aria-controls"); got != "f-d" {
		t.Errorf("button aria-controls = %q", got)
	}
	if btn.Find("svg").Length() != 1 {
		t.Error("button has no calendar icon")
	}
	pick := box.Find(`input[type=date][data-date-picker="f-d"]`)
	if pick.Length() != 1 {
		t.Fatalf("%d hidden date inputs, want 1", pick.Length())
	}
	if _, named := pick.Attr("name"); named {
		t.Error("hidden date input has a name: it would be posted")
	}
	for attr, want := range map[string]string{"tabindex": "-1", "aria-hidden": "true", "class": "field__picker", "min": "2025-04-01", "max": "2026-10-24"} {
		if got, _ := pick.Attr(attr); got != want {
			t.Errorf("hidden date input %s = %q, want %q", attr, got, want)
		}
	}
	for _, c := range []string{"24-10-2026", "23-10-2026"} {
		if !doc.Find(`button[data-set-date="` + c + `"]`).HasClass("field__chip") {
			t.Errorf("%s button isn't a chip", c)
		}
	}

	p.Min, p.Max = "", ""
	pick = uitest.Render(t, components.DateInput(p)).Find("input[type=date]")
	for _, attr := range []string{"min", "max"} {
		if v, ok := pick.Attr(attr); ok {
			t.Errorf("no bounds: hidden date input has %s=%q", attr, v)
		}
	}
}
