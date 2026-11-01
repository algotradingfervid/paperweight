package components_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UI v2 §5.8 closed: a search icon at the start, a chevron at the end and a
// "Type to search" placeholder, so a master link no longer looks like free
// text.
func TestPicker_V2SearchLook(t *testing.T) {
	doc := uitest.Render(t, components.Picker(pickerProps()))
	in := doc.Find(`input[role="combobox"]`)
	if got, _ := in.Attr("placeholder"); got != "Type to search" {
		t.Errorf("placeholder = %q, want Type to search", got)
	}
	box := in.Parent()
	if !box.HasClass("picker__box") {
		t.Fatalf("the input sits in %q, want .picker__box", box.AttrOr("class", ""))
	}
	kids := box.Children()
	first, last := kids.First(), kids.Last()
	if !first.HasClass("picker__icon") || first.Find("svg.icon").Length() != 1 || first.AttrOr("aria-hidden", "") != "true" {
		t.Errorf("first part of the box = %s, want the hidden search icon", goquery.NodeName(first)+"."+first.AttrOr("class", ""))
	}
	if !last.HasClass("picker__chevron") || last.Find("svg.icon").Length() != 1 || last.AttrOr("aria-hidden", "") != "true" {
		t.Errorf("last part of the box = %s, want the hidden chevron", goquery.NodeName(last)+"."+last.AttrOr("class", ""))
	}
	if !strings.Contains(mustHTML(t, first), "m21 21") {
		t.Error("the start icon is not Lucide search")
	}
	if !strings.Contains(mustHTML(t, last), "m6 9 6 6 6-6") {
		t.Error("the end icon is not Lucide chevron-down")
	}
}

func mustHTML(t *testing.T, s *goquery.Selection) string {
	t.Helper()
	h, err := s.Html()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func recentRows(n int) []components.PickRow {
	var out []components.PickRow
	for i := range n {
		out = append(out, components.PickRow{ID: int64(100 + i), Code: "SHREE-" + strconv.Itoa(i), Name: "Shree Traders " + strconv.Itoa(i)})
	}
	return out
}

// Recent picks wait in a template that combobox.js puts at the top of the
// listbox after each search: a "Recent" group of at most 5 options, newest
// first, with ids that never clash with the search's.
func TestPicker_RecentGroup(t *testing.T) {
	p := pickerProps()
	p.Recent = recentRows(6)
	doc := uitest.Render(t, components.Picker(p))
	tpl := doc.Find(`.picker template[data-picker-recent]`)
	if tpl.Length() != 1 {
		t.Fatalf("want one recent template, got %d", tpl.Length())
	}
	if tpl.Closest(`[role="listbox"]`).Length() != 0 {
		t.Error("the template sits inside the listbox, which every search replaces")
	}
	inner, err := goquery.NewDocumentFromReader(strings.NewReader(mustHTML(t, tpl)))
	if err != nil {
		t.Fatal(err)
	}
	g := inner.Find(`li[role="group"].picker__group`)
	if g.Length() != 1 {
		t.Fatalf("want one group in the template, got %d", g.Length())
	}
	hID := g.AttrOr("aria-labelledby", "")
	if hID != "item-lb-recent-h" {
		t.Errorf("aria-labelledby = %q, want item-lb-recent-h", hID)
	}
	h := inner.Find("#" + hID)
	if got := strings.TrimSpace(h.Text()); got != "Recent" || h.Find("svg.icon").Length() != 1 {
		t.Errorf("heading = %q with %d icons, want Recent with the history icon", got, h.Find("svg.icon").Length())
	}
	opts := g.Find(`li[role="option"]`)
	if opts.Length() != 5 {
		t.Fatalf("want 5 recent options of 6, got %d", opts.Length())
	}
	opts.Each(func(i int, o *goquery.Selection) {
		r := p.Recent[i]
		want := map[string]string{
			"id": "item-lb-recent-" + strconv.Itoa(i), "data-value": strconv.FormatInt(r.ID, 10),
			"data-label": r.Display(), "aria-selected": "false",
		}
		for k, v := range want {
			if got := o.AttrOr(k, ""); got != v {
				t.Errorf("option %d %s = %q, want %q", i, k, got, v)
			}
		}
		if got := o.Find(".picker__name").Text(); got != r.Name {
			t.Errorf("option %d name = %q, want %q", i, got, r.Name)
		}
	})

	p.Recent = nil
	if n := uitest.Render(t, components.Picker(p)).Find("template").Length(); n != 0 {
		t.Errorf("no recent picks: %d templates rendered", n)
	}
}

// Results come in one labelled group: "All" for the empty search, "Matches"
// for typed text. Option ids keep {listbox}-{n}, which the search pages and
// tests address.
func TestPickerResults_Sections(t *testing.T) {
	rows := []components.PickRow{{ID: 1, Code: "SHREE", Name: "Shree Steel Traders"}, {ID: 2, Code: "SAI", Name: "Sai Krupa"}}
	for _, c := range []struct{ q, heading string }{{"", "All"}, {"  ", "All"}, {"s", "Matches"}} {
		doc := uitest.Render(t, components.PickerResults("item-lb", rows, c.q))
		g := doc.Find(`li[role="group"].picker__group`)
		if g.Length() != 1 {
			t.Fatalf("q %q: want one group, got %d", c.q, g.Length())
		}
		if got := strings.TrimSpace(doc.Find("#" + g.AttrOr("aria-labelledby", "none")).Text()); got != c.heading {
			t.Errorf("q %q: heading = %q, want %q", c.q, got, c.heading)
		}
		g.Find(`li[role="option"]`).Each(func(i int, o *goquery.Selection) {
			if got := o.AttrOr("id", ""); got != "item-lb-"+strconv.Itoa(i) {
				t.Errorf("q %q: option %d id = %q", c.q, i, got)
			}
		})
	}
	doc := uitest.Render(t, components.PickerResults("item-lb", nil, "zz"))
	if doc.Find(".picker__group").Length() != 0 || doc.Find(`li[role="option"][aria-disabled="true"]`).Length() != 1 {
		t.Error("no rows: want only the disabled No matches option, no group")
	}
}

// The typed text is marked in each row's name, code and line, ignoring case; the
// visible text is unchanged and HTML in a name stays text.
func TestPickerResults_HighlightsMatch(t *testing.T) {
	rows := []components.PickRow{
		{ID: 1, Code: "SHREE-01", Name: "Shree Steel Traders"},
		{ID: 2, Code: "AGS", Name: "Agarwal <b>shree</b> Sons"},
		{ID: 3, Code: "SAI", Name: "Sai Krupa", Line: "SAI · Shree Nagar, Pune"},
		{ID: 4, Code: "ŚRĪ", Name: "Śrī Ganesh"},
	}
	doc := uitest.Render(t, components.PickerResults("item-lb", rows, " shree "))
	// The line is marked too: a GSTIN or city match shows where it matched.
	if got := doc.Find(`li[role="option"]`).Eq(2).Find(".picker__line mark").Text(); got != "Shree" {
		t.Errorf("line marks %q, want Shree", got)
	}
	opts := doc.Find(`li[role="option"]`)
	cases := []struct{ name, nameMark, code, codeMark string }{
		{"Shree Steel Traders", "Shree", "SHREE-01", "SHREE"},
		{"Agarwal <b>shree</b> Sons", "shree", "AGS", ""},
		{"Sai Krupa", "", "SAI", ""},
		{"Śrī Ganesh", "", "ŚRĪ", ""},
	}
	for i, c := range cases {
		o := opts.Eq(i)
		name, code := o.Find(".picker__name"), o.Find(".picker__code")
		if name.Text() != c.name || code.Text() != c.code {
			t.Errorf("row %d text = %q / %q, want %q / %q", i, code.Text(), name.Text(), c.code, c.name)
		}
		for _, part := range []struct {
			s    *goquery.Selection
			want string
		}{{name, c.nameMark}, {code, c.codeMark}} {
			m := part.s.Find("mark")
			if got := m.Text(); got != part.want || m.Length() > 1 {
				t.Errorf("row %d marks %q (%d), want %q", i, got, m.Length(), part.want)
			}
		}
	}
	if opts.Eq(1).Find(".picker__name b").Length() != 0 {
		t.Error("HTML in a name was not escaped")
	}

	doc = uitest.Render(t, components.PickerResults("item-lb", rows, "śrī"))
	if got := doc.Find(`li[role="option"]`).Eq(3).Find(".picker__name mark").Text(); got != "Śrī" {
		t.Errorf("non-ASCII match marks %q, want Śrī", got)
	}
	// The cut is made on the original text, so a letter whose lower case is
	// longer (İ) never shifts it.
	ist := []components.PickRow{{ID: 5, Code: "TR-9", Name: "İstanbul Pipes"}}
	if got := uitest.Render(t, components.PickerResults("item-lb", ist, "st")).Find(".picker__name mark").Text(); got != "st" {
		t.Errorf("İstanbul marks %q, want st", got)
	}
	if n := uitest.Render(t, components.PickerResults("item-lb", rows, "")).Find("mark").Length(); n != 0 {
		t.Errorf("empty search: %d marks", n)
	}
}
