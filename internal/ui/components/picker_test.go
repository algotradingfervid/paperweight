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
	if got, _ := doc.Find(`input[type="hidden"][name="item_id"]`).Attr("value"); got != "7" {
		t.Errorf("hidden value = %q, want 7", got)
	}
	if _, hidden := doc.Find("[data-picker-search]").Attr("hidden"); !hidden {
		t.Error("the search must be hidden until Change")
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
