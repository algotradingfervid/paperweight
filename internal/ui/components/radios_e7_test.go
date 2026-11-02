package components_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// e7Field is the radio group the E7 tests draw.
var e7Field = components.FieldProps{ID: "f-po", Name: "po", Label: "Purchase order"}

// An option without Lines or Bar draws exactly as before E7: the label and
// its hint, nothing else (UI v2 §5.6; every existing caller).
func TestRadios_PlainOptionUnchanged(t *testing.T) {
	doc := uitest.Render(t, components.Radios(e7Field, []components.Option{{Value: "a", Label: "Site", Hint: "At the site."}, {Value: "b", Label: "Store"}}))
	var got []string
	doc.Find(".field__choice-text").Each(func(_ int, s *goquery.Selection) {
		h, _ := goquery.OuterHtml(s)
		got = append(got, h)
	})
	want := []string{
		`<span class="field__choice-text">Site <span class="field__hint">At the site.</span></span>`,
		`<span class="field__choice-text">Store </span>`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("option text =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if n := doc.Find("progress, .choice-line").Length(); n != 0 {
		t.Errorf("plain options draw %d lines or bars, want none", n)
	}
}

// Lines are muted lines under the label, in order; a Bar is a <progress>
// with its value and max, hidden from screen readers, first in the last
// line, whose text says the same. Without a Bar no line has one.
func TestRadios_LinesAndBar(t *testing.T) {
	opts := []components.Option{
		{Value: "1", Label: "PO/26-27/00001 · Shree Steel Traders", Lines: []string{"Metro Depot Site", "1 of 3 lines pending"}, LineIcon: "map-pin", Bar: &components.Meter{Value: 1, Max: 3}},
		{Value: "2", Label: "PO/26-27/00002 · Bharat Tools", Lines: []string{"Pune Warehouse"}},
	}
	doc := uitest.Render(t, components.Radios(e7Field, opts))
	rows := doc.Find("label.field__choice")
	if rows.Length() != 2 {
		t.Fatalf("%d rows, want 2", rows.Length())
	}
	first := rows.Eq(0).Find(".field__choice-text > .field__hint.choice-line")
	var lines []string
	first.Each(func(_ int, s *goquery.Selection) { lines = append(lines, strings.TrimSpace(s.Text())) })
	if strings.Join(lines, "|") != "Metro Depot Site|1 of 3 lines pending" {
		t.Errorf("first row lines = %q", lines)
	}
	if n := first.Eq(0).Find("progress").Length(); n != 0 {
		t.Errorf("the first line has %d bars, want none", n)
	}
	if icon := first.Eq(0).Children(); icon.Length() != 1 || !icon.Is("svg.icon") || icon.Find("circle").Length() != 1 {
		t.Errorf("the first line starts with %d children, want the map-pin icon alone", icon.Length())
	}
	if n := first.Eq(1).Find("svg").Length(); n != 0 {
		t.Errorf("the last line has %d icons, want none", n)
	}
	bar := first.Eq(1).Children().First()
	if goquery.NodeName(bar) != "progress" || bar.AttrOr("value", "") != "1" || bar.AttrOr("max", "") != "3" ||
		bar.AttrOr("aria-hidden", "") != "true" || bar.AttrOr("class", "") != "choice-meter" {
		h, _ := goquery.OuterHtml(bar)
		t.Errorf("last line starts with %s, want <progress class=choice-meter value=1 max=3 aria-hidden=true>", h)
	}
	second := rows.Eq(1)
	if got := strings.TrimSpace(second.Find(".choice-line").Text()); got != "Pune Warehouse" || second.Find("progress").Length() != 0 {
		t.Errorf("second row line %q with %d bars, want Pune Warehouse and none", got, second.Find("progress").Length())
	}
	if n := second.Find("svg").Length(); n != 0 {
		t.Errorf("second row (no LineIcon) has %d icons, want none", n)
	}

	// The bounds: an empty and a full bar draw.
	for _, m := range []components.Meter{{Value: 0, Max: 1}, {Value: 2, Max: 2}} {
		doc := uitest.Render(t, components.Radios(e7Field, []components.Option{{Value: "1", Label: "x", Lines: []string{"y"}, Bar: &m}}))
		if doc.Find("progress").Length() != 1 {
			t.Errorf("bar %+v isn't drawn", m)
		}
	}
}

// A Bar outside 0 ≤ Value ≤ Max with Max above 0, or with no line beside
// it, and a LineIcon with no line or not in the icons set, are caller bugs:
// drawing them panics, in Radios and in Checkboxes.
func TestRadios_BadBarPanics(t *testing.T) {
	for _, c := range []struct {
		name  string
		bar   components.Meter
		lines []string
	}{
		{"max 0", components.Meter{Value: 0, Max: 0}, []string{"y"}},
		{"max below 0", components.Meter{Value: 0, Max: -1}, []string{"y"}},
		{"value below 0", components.Meter{Value: -1, Max: 2}, []string{"y"}},
		{"value above max", components.Meter{Value: 3, Max: 2}, []string{"y"}},
		{"no line", components.Meter{Value: 1, Max: 2}, nil},
		{"icon without a line", components.Meter{}, nil},
		{"unknown icon", components.Meter{}, []string{"y"}},
	} {
		opts := []components.Option{{Value: "1", Label: "x", Lines: c.lines, Bar: &c.bar}}
		switch c.name {
		case "icon without a line":
			opts[0].Bar, opts[0].LineIcon = nil, "map-pin"
		case "unknown icon":
			opts[0].Bar, opts[0].LineIcon = nil, "no-such-icon"
		}
		for kind, render := range map[string]func(){
			"radios":     func() { _ = components.Radios(e7Field, opts).Render(t.Context(), &bytes.Buffer{}) },
			"checkboxes": func() { _ = components.Checkboxes(e7Field, opts, nil).Render(t.Context(), &bytes.Buffer{}) },
		} {
			func() {
				defer func() {
					if recover() == nil {
						t.Errorf("%s, %s: drawing didn't panic", c.name, kind)
					}
				}()
				render()
			}()
		}
	}
}
