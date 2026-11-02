package components_test

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// No case ID: UI v2 D2 (§5.8, N10, CD-375). A picker search that may add a
// record ends with one more option, "New vendor "{q}"", which opens the
// quick-create sheet; the sheet arrives out of band in the picker's own slot,
// outside the search, so it stays visible when the search hides.

func quickSheet() templ.Component {
	return components.QuickCreate(components.QuickCreateProps{
		ListboxID: "f-vendor_id-list", Title: "New vendor", Icon: "user-plus",
		Line:   "Just enough to raise this order. Add contacts and more addresses later in Parties.",
		Action: "/m/party/quick", Role: "vendor",
		Inputs: []templ.Component{components.TextInput(components.FieldProps{ID: "f-quick_name", Name: "quick_name", Label: "Name", Required: true, Value: "Sai Krupa"})},
	})
}

func TestPickerResultsNew_CreateOption(t *testing.T) {
	rows := []components.PickRow{{ID: 7, Code: "SAI", Name: "Sai Steel"}}
	doc := uitest.Render(t, components.PickerResultsNew("f-vendor_id-list", rows, "sai", components.PickNew{Noun: "vendor", Sheet: quickSheet()}))
	opts := doc.Find(`[role="option"]`)
	if opts.Length() != 2 {
		t.Fatalf("%d options, want the row and the create option", opts.Length())
	}
	last := opts.Last()
	if got := text(last); got != `New vendor "sai"` {
		t.Errorf("create option text = %q, want %q", got, `New vendor "sai"`)
	}
	if got := last.AttrOr("data-quick", ""); got != "f-vendor_id-list-sheet" {
		t.Errorf("data-quick = %q, want the sheet's id f-vendor_id-list-sheet", got)
	}
	if got := last.AttrOr("id", ""); got != "f-vendor_id-list-new" {
		t.Errorf("create option id = %q, want f-vendor_id-list-new", got)
	}
	if _, ok := last.Attr("data-value"); ok {
		t.Error("the create option carries data-value, so choosing it would post a value")
	}
	if last.Find(`svg.icon`).Length() != 1 {
		t.Error("the create option has no plus icon")
	}
	slot := doc.Find(`#f-vendor_id-list-quick`)
	if got := slot.AttrOr("hx-swap-oob", ""); got != "innerHTML" {
		t.Errorf("sheet slot hx-swap-oob = %q, want innerHTML (the slot keeps its hx-vals)", got)
	}
	if slot.Find(`dialog#f-vendor_id-list-sheet`).Length() != 1 {
		t.Error("the sheet isn't in the out-of-band slot")
	}
	if slot.Closest(`[role="listbox"], ul`).Length() != 0 {
		t.Error("the sheet slot sits inside the listbox")
	}
}

// No match with create: §5.8's empty state and the create option; without
// create the search keeps "No matches".
func TestPickerResultsNew_EmptyState(t *testing.T) {
	doc := uitest.Render(t, components.PickerResultsNew("f-vendor_id-list", nil, "Sai Krupa Sand Suppliers", components.PickNew{Noun: "vendor", Sheet: quickSheet()}))
	empty := doc.Find(`.picker__empty`)
	if got := empty.AttrOr("aria-disabled", ""); got != "true" {
		t.Errorf("empty state aria-disabled = %q, want true", got)
	}
	if got := text(empty.Find(`.picker__empty-text`)); got != `No vendor matches "Sai Krupa Sand Suppliers"` {
		t.Errorf("empty heading = %q", got)
	}
	if got := text(empty.Find(`.picker__empty-line`)); got != "Check the spelling, or add them now. You'll come straight back here." {
		t.Errorf("empty line = %q", got)
	}
	if empty.Find(`.picker__empty-icon svg.icon`).Length() != 1 {
		t.Error("the empty state has no search icon")
	}
	if got := text(doc.Find(`[data-quick]`)); got != `New vendor "Sai Krupa Sand Suppliers"` {
		t.Errorf("create option = %q", got)
	}
	if doc.Find(`:contains("No matches for")`).Length() != 0 {
		t.Error("the plain No matches row shows beside the empty state")
	}
	blank := uitest.Render(t, components.PickerResultsNew("f-vendor_id-list", nil, "  ", components.PickNew{Noun: "vendor", Sheet: quickSheet()}))
	if got := text(blank.Find(`[data-quick]`)); got != "New vendor" {
		t.Errorf("create option for a blank search = %q, want New vendor", got)
	}
	if got := text(blank.Find(`.picker__empty-text`)); got != "No vendor matches" {
		t.Errorf("empty heading for a blank search = %q, want No vendor matches", got)
	}
}

func TestQuickCreate_Sheet(t *testing.T) {
	doc := uitest.Render(t, quickSheet())
	dlg := doc.Find(`dialog#f-vendor_id-list-sheet`)
	if !dlg.HasClass("sheet") {
		t.Errorf("dialog class = %q, want the Sheet", dlg.AttrOr("class", ""))
	}
	if got := text(dlg.Find(`h2`)); got != "New vendor" {
		t.Errorf("title = %q, want New vendor", got)
	}
	if dlg.Find(`.quick__tile svg.icon`).Length() != 1 {
		t.Error("no icon tile")
	}
	if got := text(dlg.Find(`.quick__line`)); got != "Just enough to raise this order. Add contacts and more addresses later in Parties." {
		t.Errorf("line = %q", got)
	}
	form := dlg.Find(`form`)
	for attr, want := range map[string]string{
		"id": "f-vendor_id-list-sheet-form", "hx-post": "/m/party/quick", "hx-target": "previous .picker",
		"hx-swap": "outerHTML", "hx-trigger": "submit consume",
	} {
		if got := form.AttrOr(attr, ""); got != want {
			t.Errorf("form %s = %q, want %q", attr, got, want)
		}
	}
	if got := form.Find(`input[type="hidden"][name="role"]`).AttrOr("value", "?"); got != "vendor" {
		t.Errorf("role = %q, want vendor", got)
	}
	if got := form.Find(`input[name="quick_name"]`).AttrOr("value", ""); got != "Sai Krupa" {
		t.Errorf("name input = %q, want the prefilled Sai Krupa", got)
	}
	if form.Find(`.field__hint:contains("are required.")`).Length() != 1 {
		t.Error("no required note")
	}
	cancel := form.Find(`button[data-close-dialog]:contains("Cancel")`)
	if cancel.AttrOr("type", "") != "button" {
		t.Error("no Cancel button that closes the sheet")
	}
	if got := text(form.Find(`button[type="submit"]`)); got != "Add and choose" {
		t.Errorf("submit = %q, want Add and choose", got)
	}
	if doc.Find(`.banner`).Length() != 0 {
		t.Error("a banner shows with no problem")
	}
	p := uitest.Render(t, components.QuickForm(components.QuickCreateProps{ListboxID: "x-list", Action: "/m/item/quick", Problem: "A registered party needs a primary billing address."}))
	if got := text(p.Find(`.banner--danger`)); got != "Error: A registered party needs a primary billing address." {
		t.Errorf("problem banner = %q", got)
	}
}

// The picker's quick slot carries the props a quick-create posts back, and
// PickerFromForm reads them, so the server draws the same picker again.
func TestPicker_QuickSlotRoundTrip(t *testing.T) {
	want := components.PickerProps{
		Field:     components.FieldProps{ID: "f-vendor_id", Label: "Vendor", Hint: "Who you buy from.", Tip: "The supplier.", Placeholder: "e.g. Shree", Required: true},
		SearchURL: "/pick/Party?role=vendor", HiddenName: "vendor_id", ListboxID: "f-vendor_id-list",
		Recent: []components.PickRow{{ID: 3, Code: "SHREE", Name: "Shree Steel", Line: "Pune"}},
	}
	doc := uitest.Render(t, components.Picker(want))
	slot := doc.Find(`.field > .picker + #f-vendor_id-list-quick`)
	if !slot.HasClass("picker__quick") {
		t.Fatalf("no quick slot right after the picker: %q", slot.AttrOr("class", ""))
	}
	if doc.Find(`.picker #f-vendor_id-list-quick`).Length() != 0 {
		t.Error("the quick slot is inside the picker, whose handlers would hear the sheet")
	}
	var vals map[string]any
	if err := json.Unmarshal([]byte(slot.AttrOr("hx-vals", "")), &vals); err != nil {
		t.Fatalf("hx-vals: %v", err)
	}
	form := url.Values{}
	for k, v := range vals {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("hx-vals %s = %v, want a string", k, v)
		}
		form.Set(k, s)
	}
	if got := components.PickerFromForm(form); !reflect.DeepEqual(got, want) {
		t.Errorf("PickerFromForm = %+v\nwant %+v", got, want)
	}
	opt := want
	opt.Field.Required, opt.Field.Clearable, opt.Recent = false, true, nil
	doc = uitest.Render(t, components.Picker(opt))
	vals = nil
	if err := json.Unmarshal([]byte(doc.Find(`.picker__quick`).AttrOr("hx-vals", "")), &vals); err != nil {
		t.Fatal(err)
	}
	form = url.Values{}
	for k, v := range vals {
		form.Set(k, v.(string))
	}
	if got := components.PickerFromForm(form); !reflect.DeepEqual(got, opt) {
		t.Errorf("clearable PickerFromForm = %+v\nwant %+v", got, opt)
	}
}

// PickerBox is the picker without its field, with the new record chosen and
// Change focused, as a quick-create answers.
func TestPickerBox_Chosen(t *testing.T) {
	p := components.PickerProps{
		Field:     components.FieldProps{ID: "f-vendor_id", Label: "Vendor"},
		SearchURL: "/pick/Party?role=vendor", HiddenName: "vendor_id", ListboxID: "f-vendor_id-list",
		Chosen: &components.PickRow{ID: 42, Code: "SAIKRUPA", Name: "Sai Krupa"}, Focus: true,
	}
	doc := uitest.Render(t, components.PickerBox(p))
	if doc.Find(`.field`).Length() != 0 {
		t.Error("PickerBox draws the field around the picker")
	}
	if got := text(doc.Find(`[data-picker-summary]`)); got != "SAIKRUPA Sai Krupa" {
		t.Errorf("summary = %q", got)
	}
	if got := doc.Find(`input[type="hidden"][name="vendor_id"]`).AttrOr("value", ""); got != "42" {
		t.Errorf("hidden value = %q, want 42", got)
	}
	if _, ok := doc.Find(`[data-picker-change]`).Attr("autofocus"); !ok {
		t.Error("Change isn't autofocused")
	}
	answer := uitest.Render(t, components.QuickChosen(p))
	if got := text(answer.Find(`.picker [data-picker-summary]`)); got != "SAIKRUPA Sai Krupa" {
		t.Errorf("QuickChosen summary = %q", got)
	}
	slot := answer.Find(`#f-vendor_id-list-quick`)
	if slot.AttrOr("hx-swap-oob", "") != "innerHTML" || slot.Children().Length() != 0 || slot.Closest(".picker").Length() != 0 {
		t.Error("QuickChosen doesn't empty the quick slot out of band")
	}
	if doc.Find(`.picker__quick`).Length() != 0 {
		t.Error("PickerBox holds a quick slot")
	}
	p.Focus = false
	doc = uitest.Render(t, components.PickerBox(p))
	if _, ok := doc.Find(`[data-picker-change]`).Attr("autofocus"); ok {
		t.Error("Change is autofocused without Focus")
	}
}
