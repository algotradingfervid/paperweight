package components_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// The info tip is a <details> (tap, keyboard and screen reader, no script and
// no hover) whose summary is the named "i" button; its body follows it so the
// open tip shows under the label row (§5.5 item 4, N4).
func TestInfoTip_DetailsWithNamedSummary(t *testing.T) {
	doc := uitest.Render(t, components.InfoTip("f-code-tip", "Code", "A short name you can type to find this party fast."))
	d := doc.Find("details.infotip")
	if d.Length() != 1 {
		t.Fatalf("want one details.infotip, got %d", d.Length())
	}
	if _, open := d.Attr("open"); open {
		t.Error("the tip starts open")
	}
	s := d.Find("summary")
	if got, _ := s.Attr("aria-label"); got != "Help for Code" {
		t.Errorf("summary aria-label = %q, want Help for Code", got)
	}
	if s.Find("svg").Length() != 1 {
		t.Error("summary has no info icon")
	}
	body := doc.Find("#f-code-tip")
	if got := strings.TrimSpace(body.Text()); got != "A short name you can type to find this party fast." {
		t.Errorf("body = %q", got)
	}
	if !body.Prev().Is("details.infotip") {
		t.Error("the body doesn't follow the details, so CSS can't show it under the label row")
	}
	for _, a := range []string{"x-data", "hx-get", "onclick", "title"} {
		if doc.Find("["+a+"]").Length() != 0 {
			t.Errorf("the tip uses %s; it must work with no script and not on hover", a)
		}
	}
}

// A required choice group says so in its legend, and each of its controls
// carries aria-required (as HTML's required does on a radio; axe allows it).
func TestField_RequiredChoiceAndSelect(t *testing.T) {
	opts := []components.Option{{Value: "a", Label: "A"}, {Value: "b", Label: "B"}}
	radios := uitest.Render(t, components.Radios(components.FieldProps{ID: "f-r", Name: "r", Label: "Delivery", Required: true}, opts))
	if got := radios.Find("legend .visually-hidden").Text(); got != " (required)" {
		t.Errorf("legend hidden text = %q, want \" (required)\"", got)
	}
	if got := strings.TrimSpace(radios.Find(`legend .field__req[aria-hidden="true"]`).Text()); got != "*" {
		t.Errorf("legend asterisk = %q", got)
	}
	if n := radios.Find(`input[type="radio"][aria-required="true"]`).Length(); n != 2 {
		t.Errorf("%d of 2 radios carry aria-required", n)
	}
	sel := uitest.Render(t, components.Select(components.FieldProps{ID: "f-s", Name: "s", Label: "Terms", Required: true}, opts))
	if got, _ := sel.Find("select#f-s").Attr("aria-required"); got != "true" {
		t.Errorf("select aria-required = %q, want true", got)
	}
	tip := uitest.Render(t, components.Checkboxes(components.FieldProps{ID: "f-c", Name: "c", Label: "Documents", Tip: "Sent with the order."}, opts, nil))
	if tip.Find("legend details").Length() != 0 || tip.Find("fieldset details.infotip").Length() != 1 {
		t.Error("a choice group's tip must sit in the fieldset, outside the legend")
	}
	if got, _ := tip.Find("input#f-c").Attr("aria-describedby"); got != "f-c-tip f-c-msg" {
		t.Errorf("checkbox aria-describedby = %q, want f-c-tip f-c-msg", got)
	}
}
