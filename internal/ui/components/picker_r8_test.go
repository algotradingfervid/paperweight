package components_test

import (
	"strconv"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UI v2 §5.8 and the review of 3 Nov (PO, GRN M6, shell L9): rows and the
// chosen value carry the tile and the line; an empty search names the master
// and says nothing about matching when nothing was typed.

func TestPickRow_Monogram(t *testing.T) {
	for _, c := range []struct {
		name string
		row  components.PickRow
		want string
	}{
		{"two words", components.PickRow{Name: "Shree Steel Traders"}, "SS"},
		{"one word", components.PickRow{Name: "Bharat"}, "B"},
		{"punctuation", components.PickRow{Name: "(old) m/s pipes"}, "OM"},
		{"digit", components.PickRow{Name: "3M India"}, "3I"},
		{"Devanagari", components.PickRow{Name: "श्री स्टील"}, "शस"},
		{"a raw id shows nothing of it", components.PickRow{Code: "42"}, ""},
	} {
		if got := c.row.Monogram(); got != c.want {
			t.Errorf("%s: Monogram = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPickerRows_TileAndLine(t *testing.T) {
	rows := []components.PickRow{
		{ID: 1, Code: "SHREE", Name: "Shree Steel", Line: "27AABCS1429B1ZU · Mumbai"},
		{ID: 2, Code: "NEW", Name: "New Traders"},
	}
	doc := uitest.Render(t, components.PickerResults("v", rows, "", components.Noun{One: "vendor", Many: "vendors"}))
	opts := doc.Find(`[role="option"]`)
	first, second := opts.First(), opts.Last()
	if got := text(first.Find(".picker__tile")); got != "SS" {
		t.Errorf("tile = %q, want SS", got)
	}
	if first.Find(".picker__tile").AttrOr("aria-hidden", "") != "true" {
		t.Error("the tile is read aloud, repeating the name")
	}
	if got := text(first.Find(".picker__name")); got != "Shree Steel" {
		t.Errorf("name = %q", got)
	}
	if got := text(first.Find(".picker__sub .picker__code")); got != "SHREE" {
		t.Errorf("code = %q", got)
	}
	if got := text(first.Find(".picker__sub .picker__line")); got != "27AABCS1429B1ZU · Mumbai" {
		t.Errorf("line = %q", got)
	}
	if n := second.Find(".picker__line").Length(); n != 0 {
		t.Errorf("a row with no line draws %d lines", n)
	}
}

func TestPicker_ChosenBox(t *testing.T) {
	p := pickerProps()
	p.Chosen = &components.PickRow{ID: 9, Code: "SHREE", Name: "Shree Steel", Line: "No GSTIN · Pune"}
	doc := uitest.Render(t, components.Picker(p))
	box := doc.Find("[data-picker-chosen]")
	if got := text(box.Find("[data-picker-tile]")); got != "SS" {
		t.Errorf("tile = %q, want SS", got)
	}
	if got := text(box.Find("[data-picker-summary]")); got != "SHREE Shree Steel" {
		t.Errorf("summary = %q", got)
	}
	if got := text(box.Find("[data-picker-line]")); got != "No GSTIN · Pune" {
		t.Errorf("line = %q", got)
	}
}

func TestPicker_PlaceholderKeepsTheCallers(t *testing.T) {
	p := pickerProps()
	p.Field.Placeholder = "e.g. 7214"
	doc := uitest.Render(t, components.Picker(p))
	if got := doc.Find(`input[role="combobox"]`).AttrOr("placeholder", ""); got != "e.g. 7214" {
		t.Errorf("placeholder = %q, want the caller's", got)
	}
}

func TestPickerResults_EmptyNamesTheMaster(t *testing.T) {
	for _, c := range []struct{ q, want string }{
		{"", "No payment term to choose from yet."},
		{"  ", "No payment term to choose from yet."},
		{"net", `No payment term matches "net"`},
	} {
		doc := uitest.Render(t, components.PickerResults("t", nil, c.q, components.Noun{One: "payment term", Many: "payment terms"}))
		if got := text(doc.Find(`[role="option"]`)); got != c.want {
			t.Errorf("q=%q: empty text = %q, want %q", c.q, got, c.want)
		}
	}
}

func TestPickerResults_CountLine(t *testing.T) {
	rows := func(n int) []components.PickRow {
		out := make([]components.PickRow, n)
		for i := range out {
			out[i] = components.PickRow{ID: int64(i + 1), Code: "C" + strconv.Itoa(i), Name: "Name"}
		}
		return out
	}
	for _, c := range []struct {
		n          int
		q          string
		noun       components.Noun
		want       string
		wantShown  int
		wantHidden bool
	}{
		{n: 3, q: "", noun: components.Noun{One: "vendor", Many: "vendors"}, wantShown: 3, wantHidden: true},
		{n: 1, q: "shree", noun: components.Noun{One: "order", Many: "orders"}, want: `1 order matches "shree".`, wantShown: 1},
		{n: 3, q: " shree ", noun: components.Noun{One: "vendor", Many: "vendors"}, want: `3 vendors match "shree".`, wantShown: 3},
		{n: 2, q: "a", noun: components.Noun{One: "party", Many: "parties"}, want: `2 parties match "a".`, wantShown: 2},
		{n: 2, q: "a", noun: components.Noun{One: "branch", Many: "branches"}, want: `2 branches match "a".`, wantShown: 2},
		{n: 20, q: "a", noun: components.Noun{One: "item", Many: "items"}, want: `20 items match "a".`, wantShown: 20},
		{n: 21, q: "a", noun: components.Noun{One: "item", Many: "items"}, want: `More than 20 items match "a". Keep typing to narrow.`, wantShown: 20},
	} {
		doc := uitest.Render(t, components.PickerResults("t", rows(c.n), c.q, c.noun))
		line := doc.Find("p#t-count")
		if line.Length() != 1 || line.AttrOr("hx-swap-oob", "") != "innerHTML" || line.Closest(`[role="listbox"], li, ul`).Length() != 0 {
			t.Fatalf("%d rows, q=%q: want one count paragraph going out of band, outside the list", c.n, c.q)
		}
		if got := text(line); got != c.want {
			t.Errorf("%d rows, q=%q: count = %q, want %q", c.n, c.q, got, c.want)
		}
		_ = c.wantHidden
		if got := doc.Find(`[role="option"]`).Length(); got != c.wantShown {
			t.Errorf("%d rows: %d options, want %d", c.n, got, c.wantShown)
		}
	}
}

// The count line sits above the rows under PickerResultsNew too.
func TestPickerResultsNew_CountLine(t *testing.T) {
	doc := uitest.Render(t, components.PickerResultsNew("t", []components.PickRow{{ID: 1, Code: "A", Name: "A"}}, "a", components.PickNew{Noun: components.Noun{One: "vendor", Many: "vendors"}, Sheet: templ.NopComponent}))
	if got := text(doc.Find("p#t-count[hx-swap-oob]")); got != `1 vendor matches "a".` {
		t.Errorf("count = %q", got)
	}
}

// A refused quick form focuses what was refused, with an id of its own: the
// page under the sheet may hold an ErrorSummary too.
func TestQuickForm_FocusAfterRefusal(t *testing.T) {
	base := components.QuickCreateProps{ListboxID: "v", Title: "New vendor", Action: "/q"}

	refused := base
	refused.Errors = []components.FieldErr{{ID: "f-quick_city", Message: "City is required."}}
	doc := uitest.Render(t, components.QuickForm(refused))
	sum := doc.Find(".error-summary")
	if _, ok := sum.Attr("autofocus"); !ok || sum.AttrOr("tabindex", "") != "-1" {
		t.Error("the summary doesn't take focus")
	}
	if got := sum.AttrOr("aria-labelledby", ""); got != "v-sheet-form-errors" || sum.Find("h2#"+got).Length() != 1 {
		t.Errorf("summary title id = %q, want the sheet's own", got)
	}
	if got := sum.Find(`a[href="#f-quick_city"]`).Text(); got != "City is required." {
		t.Errorf("summary link = %q", got)
	}

	problem := base
	problem.Problem = "Name clash."
	doc = uitest.Render(t, components.QuickForm(problem))
	box := doc.Find(".quick__problem")
	if _, ok := box.Attr("autofocus"); !ok || box.AttrOr("tabindex", "") != "-1" || box.Find(".banner").Length() != 1 {
		t.Error("the problem banner doesn't take focus")
	}

	if doc := uitest.Render(t, components.QuickForm(base)); doc.Find(".error-summary, .quick__problem, [autofocus]").Length() != 0 {
		t.Error("a sheet with nothing refused has a summary or takes focus")
	}
}

func TestQuickHidden(t *testing.T) {
	in := components.TextInput(components.FieldProps{ID: "f-q", Name: "q", Label: "Q"})
	doc := uitest.Render(t, components.QuickHidden(true, false, in))
	if doc.Find("div[hidden] input").Length() != 1 || doc.Find(".quick__given").Length() != 0 {
		t.Error("hidden: want the input inside a hidden wrapper, no given class")
	}
	doc = uitest.Render(t, components.QuickHidden(false, true, in))
	if doc.Find("div[hidden]").Length() != 0 || doc.Find(".quick__given input").Length() != 1 {
		t.Error("given: want the input visible in .quick__given")
	}
}

// The count's live region is beside the listbox, never in it; an empty
// search answer empties it.
func TestPicker_CountLiveRegion(t *testing.T) {
	doc := uitest.Render(t, components.Picker(pickerProps()))
	list := doc.Find(`[role="listbox"]`)
	region := doc.Find("#" + list.AttrOr("id", "") + "-count")
	if region.AttrOr("role", "") != "status" || region.AttrOr("aria-live", "") != "polite" || region.Closest(`[role="listbox"]`).Length() != 0 {
		t.Error("the count region isn't a polite live region outside the listbox")
	}
	for name, c := range map[string]templ.Component{
		"no rows":          components.PickerResults("t", nil, "zz", components.Noun{One: "item", Many: "items"}),
		"no rows, may add": components.PickerResultsNew("t", nil, "zz", components.PickNew{Noun: components.Noun{One: "item", Many: "items"}, Sheet: templ.NopComponent}),
	} {
		empty := uitest.Render(t, c).Find("p#t-count[hx-swap-oob]")
		if empty.Length() != 1 || text(empty) != "" {
			t.Errorf("%s: want the count region emptied", name)
		}
	}
}
