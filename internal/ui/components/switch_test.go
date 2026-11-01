package components_test

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// posted is what a browser sends for name from doc: each successful control
// in document order (a checkbox only when checked), as FormData builds it.
func posted(doc *goquery.Document, name string) []string {
	var out []string
	doc.Find(`input[name="` + name + `"]`).Each(func(_ int, in *goquery.Selection) {
		typ, _ := in.Attr("type")
		if _, checked := in.Attr("checked"); (typ == "checkbox" || typ == "radio") && !checked {
			return
		}
		v, _ := in.Attr("value")
		out = append(out, v)
	})
	return out
}

// masterValues and docValues are what a master form and DOC read (CD-322).
var (
	masterValues = components.SwitchValues{Yes: "1", No: "0"}
	docValues    = components.SwitchValues{Yes: "true", No: "false"}
)

// A switch row is one real checkbox with role="switch" inside the row's
// label, so the whole row is the target and it posts as a Check does: Yes
// when on, nothing when off (UI v2 §5.5 item 7, N6). The hint is in the
// label, so only the message region describes the switch (CD-353 (3)).
func TestSwitch_RowAndPostedValue(t *testing.T) {
	for _, c := range []struct {
		value string
		vals  components.SwitchValues
		on    bool
		post  []string
	}{
		{"1", masterValues, true, []string{"1"}},
		{"true", masterValues, true, []string{"1"}},
		{"0", masterValues, false, nil},
		{"", masterValues, false, nil},
		{"true", docValues, true, []string{"true"}},
		{"false", docValues, false, nil},
	} {
		doc := uitest.Render(t, components.Switch(components.FieldProps{ID: "f-stock", Name: "is_stock", Label: "Stock item", Hint: "Counted in stock.", Value: c.value}, false, c.vals))
		in := doc.Find(`label.switch-row input[type="checkbox"][role="switch"]#f-stock`)
		if in.Length() != 1 {
			t.Fatalf("value %q: want one checkbox with role=switch inside label.switch-row", c.value)
		}
		if _, on := in.Attr("checked"); on != c.on {
			t.Errorf("value %q: checked = %v, want %v", c.value, on, c.on)
		}
		if got := posted(doc, "is_stock"); strings.Join(got, ",") != strings.Join(c.post, ",") {
			t.Errorf("value %q: posts %q, want %q", c.value, got, c.post)
		}
		if got := strings.TrimSpace(doc.Find("label.switch-row .switch-row__label").Text()); got != "Stock item" {
			t.Errorf("row label = %q", got)
		}
		if got, _ := in.Attr("aria-describedby"); got != "f-stock-msg" {
			t.Errorf("aria-describedby = %q, want f-stock-msg (the hint is read with the label)", got)
		}
		if doc.Find("#f-stock-hint").Length() != 1 || doc.Find("#f-stock-msg").Length() != 1 {
			t.Error("hint or message region missing")
		}
	}
}

// An inverted switch shows "on" when the stored value is false and posts the
// stored meaning: on posts No first (the value the form reads), off posts
// Yes (UI v2 §5.5 item 7, Field.Invert; CD-353 (4)).
func TestSwitch_InvertBothWays(t *testing.T) {
	for _, c := range []struct {
		value string
		vals  components.SwitchValues
		on    bool
		first string
	}{
		{"0", masterValues, true, "0"},
		{"", masterValues, true, "0"},
		{"1", masterValues, false, "1"},
		{"true", masterValues, false, "1"},
		{"false", docValues, true, "false"},
		{"true", docValues, false, "true"},
	} {
		doc := uitest.Render(t, components.Switch(components.FieldProps{ID: "f-disabled", Name: "disabled", Label: "Active", Value: c.value}, true, c.vals))
		in := doc.Find(`input[type="checkbox"][role="switch"]#f-disabled`)
		if _, on := in.Attr("checked"); on != c.on {
			t.Errorf("stored %q: checked = %v, want %v", c.value, on, c.on)
		}
		got := posted(doc, "disabled")
		if len(got) == 0 || got[0] != c.first {
			t.Errorf("stored %q: posts %q, want %q first", c.value, got, c.first)
		}
	}
}

func TestSwitch_ErrorAndRequired(t *testing.T) {
	doc := uitest.Render(t, components.Switch(components.FieldProps{ID: "f-s", Name: "s", Label: "Stock item", Error: "Pick one.", Required: true}, false, masterValues))
	in := doc.Find("input#f-s")
	if got, _ := in.Attr("aria-invalid"); got != "true" {
		t.Errorf("aria-invalid = %q", got)
	}
	if got := strings.TrimSpace(doc.Find("#f-s-msg").Text()); got != "Pick one." {
		t.Errorf("message = %q", got)
	}
	if got := doc.Find(".switch-row__label .visually-hidden").Text(); got != " (required)" {
		t.Errorf("required text = %q", got)
	}
}

// Segmented v2: each option is a radio laid over its label, with a check
// icon the chosen one shows (UI v2 §5.5 item 8).
func TestSegmented_CheckIcon(t *testing.T) {
	opts := []components.Option{{Value: "a", Label: "Alpha"}, {Value: "b", Label: "Beta"}}
	doc := uitest.Render(t, components.Segmented(components.FieldProps{ID: "f-s", Name: "s", Label: "Pick", Value: "b"}, opts))
	if n := doc.Find(".segmented > label > input[type=radio].input-overlay").Length(); n != 2 {
		t.Fatalf("want 2 overlaid radios, got %d", n)
	}
	if n := doc.Find(".segmented > label .segmented__check svg").Length(); n != 2 {
		t.Errorf("want a check icon per option, got %d", n)
	}
	if doc.Find(".segmented--tiles").Length() != 0 {
		t.Error("Segmented renders as tiles")
	}
}

// Tiles are the segmented look in a two-column grid, for longer words (N7).
func TestTiles_Grid(t *testing.T) {
	opts := []components.Option{{Value: "fuel", Label: "Fuel"}, {Value: "travel", Label: "Local travel"}, {Value: "food", Label: "Food"}}
	doc := uitest.Render(t, components.Tiles(components.FieldProps{ID: "f-t", Name: "t", Label: "Type", Value: "travel"}, opts))
	radios := doc.Find("fieldset .segmented.segmented--tiles > label > input[type=radio][name=t]")
	if radios.Length() != 3 {
		t.Fatalf("want 3 radios in tiles, got %d", radios.Length())
	}
	if id, _ := radios.First().Attr("id"); id != "f-t" {
		t.Errorf("first id = %q, want f-t", id)
	}
	if _, ok := doc.Find("input[value=travel]").Attr("checked"); !ok {
		t.Error("travel not checked")
	}
}
