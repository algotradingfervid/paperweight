package components_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

func pickerProps() components.PickerProps {
	return components.PickerProps{
		Field:      components.FieldProps{ID: "f-item_id", Name: "item_id", Label: "Item"},
		SearchURL:  "/pick/item",
		HiddenName: "item_id",
		ListboxID:  "item-lb",
	}
}

func TestPicker_ComboboxARIA(t *testing.T) {
	doc := uitest.Render(t, components.Picker(pickerProps()))
	in := doc.Find(`input[role="combobox"]`)
	if in.Length() != 1 {
		t.Fatalf("want one combobox input, got %d", in.Length())
	}
	want := map[string]string{
		"aria-expanded":     "false",
		"aria-controls":     "item-lb",
		"aria-autocomplete": "list",
		"hx-get":            "/pick/item",
		"hx-trigger":        "input changed delay:250ms, focus once",
		"hx-target":         "#item-lb",
		"name":              "q",
		"id":                "f-item_id",
	}
	for k, v := range want {
		if got, _ := in.Attr(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if doc.Find(`ul[role="listbox"]#item-lb`).Length() != 1 {
		t.Error("no ul[role=listbox] with the ListboxID")
	}
	if doc.Find(`input[type="hidden"][name="item_id"]`).Length() != 1 {
		t.Error("no hidden input named item_id")
	}
	if doc.Find(`[x-data="combobox"]`).Length() != 1 {
		t.Error(`no x-data="combobox"`)
	}
	if doc.Find("select").Length() != 0 {
		t.Error("combobox must not render a select")
	}
}

func TestPicker_SelectFallbackUnder50(t *testing.T) {
	p := pickerProps()
	for i := range 49 {
		p.Options = append(p.Options, components.Option{Value: strconv.Itoa(i), Label: "Unit " + strconv.Itoa(i)})
	}
	doc := uitest.Render(t, components.Picker(p))
	if doc.Find("select option").Length() != 49 {
		t.Errorf("49 options: want a select with 49 options, got %d", doc.Find("select option").Length())
	}
	if doc.Find(`[role="combobox"]`).Length() != 0 {
		t.Error("49 options: combobox rendered")
	}

	p.Options = append(p.Options, components.Option{Value: "49", Label: "Unit 49"})
	if doc := uitest.Render(t, components.Picker(p)); doc.Find("select").Length() != 0 || doc.Find(`[role="combobox"]`).Length() != 1 {
		t.Error("50 options: want the combobox, not a select")
	}

	p.Options = nil
	if doc := uitest.Render(t, components.Picker(p)); doc.Find("select").Length() != 0 || doc.Find(`[role="combobox"]`).Length() != 1 {
		t.Error("0 options: want the combobox, not a select")
	}
}

func TestPicker_ChosenSummaryWithChange(t *testing.T) {
	p := pickerProps()
	p.Chosen = &components.PickRow{ID: 7, Code: "CEMENT-OPC", Name: "OPC Cement 53 Grade", Line: "bag"}
	doc := uitest.Render(t, components.Picker(p))
	if got := strings.TrimSpace(doc.Find(".picker__summary").Text()); got != "CEMENT-OPC OPC Cement 53 Grade" {
		t.Errorf("summary = %q", got)
	}
	found := false
	doc.Find("button").Each(func(_ int, b *goquery.Selection) {
		if strings.TrimSpace(b.Text()) == "Change" {
			found = true
		}
	})
	if !found {
		t.Error(`no "Change" button`)
	}
	if doc.Find(`button.btn--secondary[type="button"][data-picker-change]`).Length() != 1 {
		t.Error("Change is not a Secondary Button")
	}
	if doc.Find(`button.btn--ghost[type="button"][data-picker-close]`).Length() != 1 {
		t.Error("Close is not a Ghost Button")
	}
	if got, _ := doc.Find(`input[type="hidden"][name="item_id"]`).Attr("value"); got != "7" {
		t.Errorf("hidden value = %q, want 7", got)
	}
	if _, hidden := doc.Find("[data-picker-search]").Attr("hidden"); !hidden {
		t.Error("the search must be hidden until Change")
	}
}

func TestPicker_ClearOptionalChosen(t *testing.T) {
	p := pickerProps()
	p.Field.Optional = true
	p.Chosen = &components.PickRow{ID: 7, Code: "CEMENT-OPC", Name: "OPC Cement 53 Grade"}
	doc := uitest.Render(t, components.Picker(p))
	clear := doc.Find(`[data-picker-chosen]:not([hidden]) button[type="button"][data-picker-clear]`)
	if clear.Length() != 1 {
		t.Fatalf("want one visible Clear button, got %d", clear.Length())
	}
	if got := strings.TrimSpace(clear.Text()); got != "Clear" {
		t.Errorf("text = %q, want Clear", got)
	}
	if got, _ := clear.Attr("aria-label"); got != "Clear Item" {
		t.Errorf("aria-label = %q, want Clear Item", got)
	}
	if !clear.HasClass("tap") {
		t.Error("Clear has no tap class, so no 44 × 44 target")
	}
	// Clear moves focus to the group, so it needs a name and must be focusable.
	root := doc.Find(`.picker[role="group"][tabindex="-1"]`)
	if got, _ := root.Attr("aria-label"); got != "Item" {
		t.Errorf("picker group aria-label = %q, want Item", got)
	}
}

func TestPicker_NoClearOnRequired(t *testing.T) {
	p := pickerProps()
	if doc := uitest.Render(t, components.Picker(p)); doc.Find("[data-picker-clear]").Length() != 0 {
		t.Error("required, none chosen: Clear rendered")
	}
	p.Chosen = &components.PickRow{ID: 7, Code: "CEMENT-OPC", Name: "OPC Cement 53 Grade"}
	if doc := uitest.Render(t, components.Picker(p)); doc.Find("[data-picker-clear]").Length() != 0 {
		t.Error("required, chosen: Clear rendered")
	}
}

// With nothing chosen Clear stays inside the hidden summary, which combobox.js
// shows once a row is chosen in the browser.
func TestPicker_NoVisibleClearWithoutChoice(t *testing.T) {
	p := pickerProps()
	p.Field.Optional = true
	doc := uitest.Render(t, components.Picker(p))
	if doc.Find("[data-picker-chosen]:not([hidden]) [data-picker-clear]").Length() != 0 {
		t.Error("none chosen: Clear is visible")
	}
	if doc.Find("[data-picker-chosen][hidden] [data-picker-clear]").Length() != 1 {
		t.Error("none chosen: no Clear waiting in the hidden summary, so a row chosen in the browser can't be cleared")
	}
}

// wantLabelledInput checks that the label names the search input and that the
// input's description holds the error.
func wantLabelledInput(t *testing.T, doc *goquery.Document) {
	t.Helper()
	in := doc.Find(`input[role="combobox"]`)
	id, _ := in.Attr("id")
	if forID, _ := doc.Find("label").Attr("for"); id == "" || forID != id {
		t.Errorf("label for = %q, input id = %q", forID, id)
	}
	described := false
	for ref := range strings.FieldsSeq(in.AttrOr("aria-describedby", "")) {
		if strings.Contains(doc.Find("#"+ref).Text(), "Item ITEM-1 is disabled.") {
			described = true
		}
	}
	if !described {
		t.Errorf("aria-describedby = %q does not reach the message", in.AttrOr("aria-describedby", ""))
	}
}

// ErrorSummary links to #Field.ID. With a row chosen the search input is
// hidden, so the link must land on something visible that can take focus.
func TestPicker_ErrorLinkTargetWhileChosen(t *testing.T) {
	p := pickerProps()
	p.Field.Error = "Item ITEM-1 is disabled."
	p.Chosen = &components.PickRow{ID: 7, Code: "ITEM-1", Name: "Item one"}
	doc := uitest.Render(t, components.Picker(p))

	target := doc.Find("#f-item_id")
	if target.Length() != 1 {
		t.Fatalf("%d elements have the field's id, want 1", target.Length())
	}
	if target.Closest("[hidden]").Length() != 0 {
		t.Error("the link target is hidden")
	}
	if !target.Is(`[tabindex], a[href], button, select, textarea, input:not([type="hidden"])`) {
		t.Errorf("the link target can't take focus: %s", goquery.NodeName(target))
	}
	wantLabelledInput(t, doc)
	// Landing on the target says what is wrong, as landing on an input does.
	if got := doc.Find("#" + target.AttrOr("aria-describedby", "none")).Text(); !strings.Contains(got, "Item ITEM-1 is disabled.") {
		t.Errorf("the link target's aria-describedby = %q does not reach the message", target.AttrOr("aria-describedby", ""))
	}
	if got, _ := target.Attr("aria-invalid"); got != "true" {
		t.Errorf("aria-invalid = %q, want true", got)
	}

	p.Field.Error = ""
	if _, invalid := uitest.Render(t, components.Picker(p)).Find("#f-item_id").Attr("aria-invalid"); invalid {
		t.Error("no error: the picker is marked invalid")
	}
}

func TestPicker_ErrorLinkTargetWithoutChoice(t *testing.T) {
	p := pickerProps()
	p.Field.Error = "Item ITEM-1 is disabled."
	doc := uitest.Render(t, components.Picker(p))

	if doc.Find(`input[role="combobox"]#f-item_id`).Length() != 1 {
		t.Error("nothing chosen: the field's id is not on the search input")
	}
	if doc.Find("#f-item_id").Closest("[hidden]").Length() != 0 {
		t.Error("nothing chosen: the link target is hidden")
	}
	wantLabelledInput(t, doc)
}

func TestPicker_SelectKeepsTheFieldID(t *testing.T) {
	p := pickerProps()
	p.Options = []components.Option{{Value: "7", Label: "Item one"}}
	p.Chosen = &components.PickRow{ID: 7, Code: "ITEM-1", Name: "Item one"}
	doc := uitest.Render(t, components.Picker(p))
	if doc.Find("select#f-item_id").Length() != 1 {
		t.Error("the Select does not carry the field's id")
	}
	if forID, _ := doc.Find("label").Attr("for"); forID != "f-item_id" {
		t.Errorf("label for = %q", forID)
	}
}

func TestPickerResults_Max20AndEmpty(t *testing.T) {
	var rows []components.PickRow
	for i := range 25 {
		rows = append(rows, components.PickRow{ID: int64(i + 1), Code: "IT-" + strconv.Itoa(i), Name: "Item " + strconv.Itoa(i), Line: "kg"})
	}
	doc := uitest.Render(t, components.PickerResults("item-lb", rows, "it"))
	opts := doc.Find(`li[role="option"]`)
	if opts.Length() != 20 {
		t.Fatalf("want 20 options, got %d", opts.Length())
	}
	first := opts.First()
	if id, _ := first.Attr("id"); id == "" {
		t.Error("option has no id")
	}
	if v, _ := first.Attr("data-value"); v != "1" {
		t.Errorf("data-value = %q, want 1", v)
	}
	for _, s := range []string{"IT-0", "Item 0", "kg"} {
		if !strings.Contains(first.Text(), s) {
			t.Errorf("first option %q lacks %q", first.Text(), s)
		}
	}

	empty := uitest.Render(t, components.PickerResults("item-lb", nil, "steel"))
	li := empty.Find(`li[role="option"]`)
	if li.Length() != 1 {
		t.Fatalf("zero rows: want one option, got %d", li.Length())
	}
	if got, _ := li.Attr("aria-disabled"); got != "true" {
		t.Errorf("aria-disabled = %q", got)
	}
	if got := strings.TrimSpace(li.Text()); got != "No matches for “steel”." {
		t.Errorf("empty text = %q", got)
	}
}
