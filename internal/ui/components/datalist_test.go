package components_test

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

func blankCellColumns() []components.Column {
	return []components.Column{
		{Key: "no", Label: "Number", Priority: components.PTitle, Code: true},
		{Key: "amount", Label: "Amount", Priority: components.PFigure, Num: true},
		{Key: "state", Label: "State", Priority: components.PState},
		{Key: "date", Label: "Date", Priority: components.PMeta, NoWrap: true},
		{Key: "gstin", Label: "GSTIN", Priority: components.PMeta, NoWrap: true},
		{Key: "project", Label: "Project", Priority: components.PMeta},
		{Key: "by", Label: "Raised by", Priority: components.PLow},
	}
}

// cardText is a card's row-3 text as drawn: blank cells (hidden on cards),
// hidden labels and, when narrow, the low-priority values are left out.
func cardText(r3 *goquery.Selection, narrow bool) string {
	c := r3.Clone()
	c.Find(".datalist-blank, dt, .visually-hidden").Remove()
	if narrow {
		c.Find(`[data-priority="low"]`).Remove()
	}
	return text(c)
}

// TestDataList_BlankCellNoDanglingSeparator: a blank card value is not
// drawn, separators go only between drawn values (the state's included),
// and each separator ends the value before it, so no line starts with "·".
func TestDataList_BlankCellNoDanglingSeparator(t *testing.T) {
	cases := []struct {
		name         string
		values       map[string]string
		wide, narrow string
	}{
		{"blank middle", map[string]string{"date": "24-10-2026", "project": "Bengaluru client visit", "by": "Anita Rao"},
			"24-10-2026 · Bengaluru client visit · Anita Rao · Approved", "24-10-2026 · Bengaluru client visit · Approved"},
		{"blank first", map[string]string{"gstin": "29AAFCV2449G1Z5", "project": "Bengaluru client visit", "by": "Anita Rao"},
			"29AAFCV2449G1Z5 · Bengaluru client visit · Anita Rao · Approved", "29AAFCV2449G1Z5 · Bengaluru client visit · Approved"},
		{"blank before low and state", map[string]string{"date": "24-10-2026", "by": "Anita Rao"},
			"24-10-2026 · Anita Rao · Approved", "24-10-2026 · Approved"},
		{"only state", map[string]string{}, "Approved", "Approved"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.values["no"], c.values["amount"] = "EXP/0042", "₹1,200.00"
			r := components.Row{Href: "/doc/exp/42", Party: "Meena Iyer", Values: c.values, State: doctype.StateApproved}
			doc := uitest.Render(t, components.DataList(components.DataListProps{Label: "Expense reports", Columns: blankCellColumns(), Rows: []components.Row{r}}))
			r3 := doc.Find(".datalist-r3")
			if r3.Length() != 1 {
				t.Fatalf("want one .datalist-r3, got %d", r3.Length())
			}
			if got := cardText(r3, false); got != c.wide {
				t.Errorf("card row 3 = %q, want %q", got, c.wide)
			}
			if got := cardText(r3, true); got != c.narrow {
				t.Errorf("card row 3 below 64rem = %q, want %q", got, c.narrow)
			}
			r3.Find("dl").Each(func(_ int, dl *goquery.Selection) {
				dd := dl.Find("dd")
				val := dd.Clone()
				val.Find(".datalist-sep").Remove()
				blank := strings.TrimSpace(val.Text()) == ""
				if blank != dl.HasClass("datalist-blank") {
					t.Errorf("cell %q: blank %v but datalist-blank %v", text(dl.Find("dt")), blank, dl.HasClass("datalist-blank"))
				}
				sep := dd.Find(".datalist-sep")
				if blank && sep.Length() != 0 {
					t.Errorf("blank cell %q holds a separator", text(dl.Find("dt")))
				}
				if sep.Length() > 0 && dd.Children().Last().Get(0) != sep.Get(0) {
					t.Errorf("cell %q: separator is not at the end of its value", text(dl.Find("dt")))
				}
			})
		})
	}
}

// TestDataList_NoWrapColumn: an opted-in column's values keep whole tokens.
func TestDataList_NoWrapColumn(t *testing.T) {
	r := components.Row{Href: "/doc/exp/42", Values: map[string]string{"no": "EXP/0042", "amount": "₹1,200.00", "date": "24-10-2026", "gstin": "29AAFCV2449G1Z5", "project": "Bengaluru client visit"}, State: doctype.StateApproved}
	doc := uitest.Render(t, components.DataList(components.DataListProps{Label: "Expense reports", Columns: blankCellColumns(), Rows: []components.Row{r}}))
	a := doc.Find(".datalist-link")
	for v, want := range map[string]bool{"24-10-2026": true, "29AAFCV2449G1Z5": true, "Bengaluru client visit": false, "EXP/0042": false} {
		if got := leafWith(t, a, v).Closest("dl").HasClass("datalist-nowrap"); got != want {
			t.Errorf("%q: datalist-nowrap = %v, want %v", v, got, want)
		}
	}
}

// noStateColumns is a list with no PState column (a non-submittable
// DocType, docscreen.ListColumns), so a separator can end with only
// low-priority values after it.
func noStateColumns() []components.Column {
	return []components.Column{
		{Key: "no", Label: "Number", Priority: components.PTitle, Code: true},
		{Key: "date", Label: "Date", Priority: components.PMeta, NoWrap: true},
		{Key: "gstin", Label: "GSTIN", Priority: components.PMeta, NoWrap: true},
		{Key: "by", Label: "Raised by", Priority: components.PLow},
	}
}

// TestDataList_NoStateSeparators: with no state badge at the end, a blank
// value after the last drawn value adds no separator, and a separator
// followed only by low-priority values is itself low priority, so it hides
// with them below 64rem.
func TestDataList_NoStateSeparators(t *testing.T) {
	cases := []struct {
		name         string
		values       map[string]string
		wide, narrow string
		dateSepLow   bool
	}{
		{"blank after the last value", map[string]string{"date": "24-10-2026"}, "24-10-2026", "24-10-2026", false},
		{"only low after a value", map[string]string{"date": "24-10-2026", "by": "Anita Rao"}, "24-10-2026 · Anita Rao", "24-10-2026", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.values["no"] = "SUP/0042"
			r := components.Row{Href: "/doc/sup/42", Party: "Meena Iyer", Values: c.values}
			doc := uitest.Render(t, components.DataList(components.DataListProps{Label: "Suppliers", Columns: noStateColumns(), Rows: []components.Row{r}}))
			r3 := doc.Find(".datalist-r3")
			if got := cardText(r3, false); got != c.wide {
				t.Errorf("card row 3 = %q, want %q", got, c.wide)
			}
			if got := cardText(r3, true); got != c.narrow {
				t.Errorf("card row 3 below 64rem = %q, want %q", got, c.narrow)
			}
			sep := leafWith(t, r3, "24-10-2026").Closest("dd").Find(".datalist-sep")
			if sep.Length() == 0 {
				if c.dateSepLow {
					t.Fatal("date has no separator")
				}
				return
			}
			if low, _ := sep.Attr("data-priority"); (low == "low") != c.dateSepLow {
				t.Errorf("date separator data-priority = %q, want low %v", low, c.dateSepLow)
			}
		})
	}
}

// nbsp is U+00A0, written as an escape so no editor turns it into a space.
const nbsp = "\u00a0"

// TestDataList_SeparatorStartsWithNoBreakSpace: the separator's first
// character is a no-break space, so the dot never starts a wrapped line.
func TestDataList_SeparatorStartsWithNoBreakSpace(t *testing.T) {
	r := components.Row{Href: "/doc/exp/42", Party: "Meena Iyer", Values: map[string]string{"no": "EXP/0042", "amount": "₹1,200.00", "date": "24-10-2026"}, State: doctype.StateApproved}
	doc := uitest.Render(t, components.DataList(components.DataListProps{Label: "Expense reports", Columns: blankCellColumns(), Rows: []components.Row{r}}))
	sep := doc.Find(".datalist-r3 .datalist-sep")
	if sep.Length() == 0 {
		t.Fatal("no separator drawn")
	}
	if raw := sep.First().Text(); !strings.HasPrefix(raw, nbsp+"·") {
		t.Errorf("separator text = %q, want it to start with U+00A0 then ·", raw)
	}
}

// TestDataList_NoWrapTracks: the list names the grid tracks of its NoWrap
// columns (title track first, even with no title column), which the CSS
// gives a minimum width.
func TestDataList_NoWrapTracks(t *testing.T) {
	noTitle := []components.Column{
		{Key: "amount", Label: "Amount", Priority: components.PFigure, Num: true},
		{Key: "date", Label: "Date", Priority: components.PMeta, NoWrap: true},
		{Key: "by", Label: "Raised by", Priority: components.PLow, NoWrap: true},
	}
	plain := []components.Column{
		{Key: "no", Label: "Number", Priority: components.PTitle, Code: true},
		{Key: "date", Label: "Date", Priority: components.PMeta},
		{Key: "by", Label: "Raised by", Priority: components.PLow},
	}
	cases := []struct {
		name string
		cols []components.Column
		want string
		has  bool
	}{
		{"meta columns after title, figure and before state", blankCellColumns(), "3 4", true},
		{"no title column", noTitle, "3 4", true},
		{"no nowrap column", plain, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := components.Row{Href: "/doc/exp/42", Title: "EXP/0042", Values: map[string]string{"no": "EXP/0042"}, State: doctype.StateApproved}
			doc := uitest.Render(t, components.DataList(components.DataListProps{Label: "Expense reports", Columns: c.cols, Rows: []components.Row{r}}))
			got, has := doc.Find(".datalist").Attr("data-nowrap")
			if got != c.want || has != c.has {
				t.Errorf("data-nowrap = %q (present %v), want %q (present %v)", got, has, c.want, c.has)
			}
		})
	}
}

// TestDataList_ValidateNoWrapLimit: at most 3 NoWrap columns; 4 of them at
// the minimum width don't fit an 8-track list at 48rem (CD-308 (3)).
func TestDataList_ValidateNoWrapLimit(t *testing.T) {
	cols := func(nowrap int) []components.Column {
		c := []components.Column{
			{Key: "no", Label: "Number", Priority: components.PTitle},
			{Key: "m1", Label: "M1", Priority: components.PMeta},
			{Key: "m2", Label: "M2", Priority: components.PMeta},
			{Key: "m3", Label: "M3", Priority: components.PMeta},
			{Key: "m4", Label: "M4", Priority: components.PMeta},
		}
		for i := 1; i <= nowrap; i++ {
			c[i].NoWrap = true
		}
		return c
	}
	if err := (components.DataListProps{Label: "L", Columns: cols(3)}).Validate(); err != nil {
		t.Errorf("3 NoWrap columns: Validate() = %v, want nil", err)
	}
	if err := (components.DataListProps{Label: "L", Columns: cols(4)}).Validate(); err == nil {
		t.Error("4 NoWrap columns: Validate() = nil, want an error")
	}
}

// UI v2 §5.12: every row ends with a chevron the screen reader skips, in
// lists with a state column and in lists without one; the state badge is
// still the last value of row 3 when there is a state column.
func TestDataList_RowChevron(t *testing.T) {
	for name, cols := range map[string][]components.Column{"state column": blankCellColumns(), "no state column": noStateColumns()} {
		t.Run(name, func(t *testing.T) {
			rows := []components.Row{
				{Href: "/doc/exp/1", Values: map[string]string{"no": "EXP/0001", "date": "24-10-2026"}, State: doctype.StateApproved},
				{Href: "/doc/exp/2", Values: map[string]string{"no": "EXP/0002"}, State: doctype.StateDraft},
			}
			doc := uitest.Render(t, components.DataList(components.DataListProps{Label: "Expense reports", Columns: cols, Rows: rows}))
			links := doc.Find("a.datalist-link")
			if links.Length() != 2 {
				t.Fatalf("%d row links, want 2", links.Length())
			}
			links.Each(func(i int, a *goquery.Selection) {
				last := a.Children().Last()
				if !last.HasClass("datalist-go") || last.AttrOr("aria-hidden", "") != "true" || last.Find("svg").Length() != 1 {
					t.Errorf("row %d: last child %q, want an aria-hidden .datalist-go with an icon", i, last.AttrOr("class", ""))
				}
				badge := a.Find(".datalist-r3 > dl").Last().Find(".badge")
				if wantBadge := name == "state column"; (badge.Length() == 1) != wantBadge {
					t.Errorf("row %d: state badge last in row 3 = %v, want %v", i, badge.Length() == 1, wantBadge)
				}
			})
		})
	}
}

// TestDataList_CardPartyColumnAndTitle (CD-371): a card skips the column
// PartyKey names, as a blank one, and keeps the others; with the PTitle
// value blank, row 1's title is Row.Title.
func TestDataList_CardPartyColumnAndTitle(t *testing.T) {
	cases := []struct {
		name, partyKey, no, title string
		wide, narrow              string
	}{
		{"party column skipped", "project", "", "Draft 42", "24-10-2026 · Anita Rao · Approved", "24-10-2026 · Approved"},
		{"no party column", "", "EXP/0042", "EXP/0042", "24-10-2026 · Meena Iyer · Anita Rao · Approved", "24-10-2026 · Meena Iyer · Approved"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := components.Row{
				Href: "/doc/exp/42", Title: "Draft 42", Party: "Meena Iyer", PartyKey: c.partyKey, State: doctype.StateApproved,
				Values: map[string]string{"no": c.no, "amount": "₹1,200.00", "date": "24-10-2026", "project": "Meena Iyer", "by": "Anita Rao"},
			}
			doc := uitest.Render(t, components.DataList(components.DataListProps{Label: "Expense reports", Columns: blankCellColumns(), Rows: []components.Row{r}}))
			title := doc.Find(".datalist-r1 .datalist-title dd").Clone()
			title.Find("dt").Remove()
			if got := text(title); got != c.title {
				t.Errorf("title = %q, want %q", got, c.title)
			}
			r3 := doc.Find(".datalist-r3")
			if got := cardText(r3, false); got != c.wide {
				t.Errorf("card row 3 = %q, want %q", got, c.wide)
			}
			if got := cardText(r3, true); got != c.narrow {
				t.Errorf("card row 3 below 64rem = %q, want %q", got, c.narrow)
			}
			project := doc.Find(`.datalist-r3 dl:has(dt:contains("Project")) dd`).Clone()
			project.Find(".datalist-sep").Remove()
			if got := text(project); got != "Meena Iyer" {
				t.Errorf("Project value = %q, want it kept for aligned rows", got)
			}
		})
	}
}
