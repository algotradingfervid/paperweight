package components_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// leafWith returns the deepest element under s whose text contains want,
// ignoring visually hidden labels.
func leafWith(t *testing.T, s *goquery.Selection, want string) *goquery.Selection {
	t.Helper()
	var found *goquery.Selection
	s.Find("*").Not("dt, .visually-hidden").EachWithBreak(func(_ int, e *goquery.Selection) bool {
		if !strings.Contains(text(e), want) {
			return true
		}
		inner := false
		e.Children().Not("dt, .visually-hidden").Each(func(_ int, c *goquery.Selection) {
			if strings.Contains(text(c), want) {
				inner = true
			}
		})
		if !inner {
			found = e
			return false
		}
		return true
	})
	if found == nil {
		t.Fatalf("no element holds %q", want)
	}
	return found
}

// lca returns the closest element holding both a and b.
func lca(a, b *goquery.Selection) *goquery.Selection {
	for p := a; p.Length() > 0; p = p.Parent() {
		if p.Get(0) == b.Get(0) || p.Contains(b.Get(0)) {
			return p
		}
	}
	return a.Parents().Last()
}

func holds(s, e *goquery.Selection) bool {
	return s.Get(0) == e.Get(0) || s.Contains(e.Get(0))
}

// before reports whether a comes before b in document order.
func before(doc *goquery.Document, a, b *goquery.Selection) bool {
	all := doc.Find("*").Nodes
	ia, ib := -1, -1
	for i, n := range all {
		if n == a.Get(0) {
			ia = i
		}
		if n == b.Get(0) {
			ib = i
		}
	}
	return ia >= 0 && ib >= 0 && ia < ib
}

// plain is the visible text of s without the visually hidden labels.
func plain(s *goquery.Selection) string {
	c := s.Clone()
	c.Find("dt, .visually-hidden").Remove()
	return text(c)
}

func listColumns() []components.Column {
	return []components.Column{
		{Key: "no", Label: "Number", Priority: components.PTitle, Code: true},
		{Key: "amount", Label: "Amount", Priority: components.PFigure, Num: true},
		{Key: "state", Label: "State", Priority: components.PState},
		{Key: "date", Label: "Date", Priority: components.PMeta},
		{Key: "project", Label: "Project", Priority: components.PMeta},
	}
}

func listRow(party string) components.Row {
	return components.Row{
		Href:  "/doc/po/42",
		Party: party,
		Values: map[string]string{
			"no":      "PO/26-27/0042",
			"amount":  "₹94,164.00",
			"date":    "24-10-2026",
			"project": "Hosur plant expansion",
		},
		State: doctype.StateApproved,
	}
}

func TestDataList_CardOrder(t *testing.T) {
	doc := uitest.Render(t, components.DataList(components.DataListProps{
		Label:   "Purchase orders",
		Columns: listColumns(),
		Rows:    []components.Row{listRow("Shree Steel Traders")},
	}))
	dl := doc.Find(`.datalist`)
	if dl.Length() != 1 || dl.AttrOr("data-cols", "") != "5" {
		t.Fatalf(".datalist count %d data-cols %q, want 1 and \"5\"", dl.Length(), dl.AttrOr("data-cols", ""))
	}
	li := dl.Find(`ul.list[role="list"] > li`)
	if li.Length() != 1 {
		t.Fatalf("want one li in ul.list[role=list], got %d", li.Length())
	}
	a := li.Find("a")
	if a.Length() != 1 || a.AttrOr("href", "") != "/doc/po/42" {
		t.Fatalf("card has %d links (href %q), want one to /doc/po/42", a.Length(), a.AttrOr("href", ""))
	}

	title := leafWith(t, a, "PO/26-27/0042")
	figure := leafWith(t, a, "₹94,164.00")
	party := leafWith(t, a, "Shree Steel Traders")
	date := leafWith(t, a, "24-10-2026")
	project := leafWith(t, a, "Hosur plant expansion")
	badge := a.Find("span.badge.badge--approved")
	if badge.Length() != 1 || !strings.Contains(text(badge), "Approved") {
		t.Fatalf("want one approved badge inside the card link, got %d", badge.Length())
	}
	if figure.Closest(".num").Length() == 0 {
		t.Error("figure is not in .num")
	}

	row1 := lca(title, figure)
	for name, e := range map[string]*goquery.Selection{"party": party, "date": date, "project": project, "badge": badge} {
		if holds(row1, e) {
			t.Errorf("row 1 (title + figure) also holds %s", name)
		}
	}
	row3 := lca(lca(date, project), badge)
	for name, e := range map[string]*goquery.Selection{"title": title, "figure": figure, "party": party} {
		if holds(row3, e) {
			t.Errorf("row 3 (meta + badge) also holds %s", name)
		}
	}
	if got := plain(row3); !strings.Contains(got, "24-10-2026 · Hosur plant expansion") {
		t.Errorf("row 3 = %q, want date · project", got)
	}
	if !before(doc, title, party) || !before(doc, party, date) {
		t.Error("card order is not title, party, meta")
	}

	labels := map[string]bool{}
	a.Find("dt.visually-hidden").Each(func(_ int, d *goquery.Selection) { labels[text(d)] = true })
	for _, l := range []string{"Number", "Amount", "State", "Date", "Project"} {
		if !labels[l] {
			t.Errorf("no dt.visually-hidden label %q in the card (got %v)", l, labels)
		}
	}
}

func TestDataList_PartyFallsBackToMeta(t *testing.T) {
	doc := uitest.Render(t, components.DataList(components.DataListProps{
		Label:   "Purchase orders",
		Columns: listColumns(),
		Rows:    []components.Row{listRow("")},
	}))
	a := doc.Find(`ul.list > li a`)
	if a.Length() != 1 {
		t.Fatalf("want one card link, got %d", a.Length())
	}
	if n := strings.Count(plain(a), "24-10-2026"); n != 1 {
		t.Errorf("date shown %d times in the card, want 1", n)
	}
	title := leafWith(t, a, "PO/26-27/0042")
	figure := leafWith(t, a, "₹94,164.00")
	date := leafWith(t, a, "24-10-2026")
	project := leafWith(t, a, "Hosur plant expansion")
	badge := a.Find("span.badge")
	if badge.Length() != 1 {
		t.Fatalf("want one badge, got %d", badge.Length())
	}
	row1 := lca(title, figure)
	if holds(row1, date) {
		t.Error("row 1 holds the date")
	}
	row3 := lca(project, badge)
	if holds(row3, date) || holds(row3, title) {
		t.Errorf("row 3 = %q, want only the remaining meta and the badge", plain(row3))
	}
	if !before(doc, figure, date) || !before(doc, date, project) {
		t.Error("first PMeta is not row 2 (between row 1 and row 3)")
	}
}

func TestDataList_ValidateRules(t *testing.T) {
	col := func(k string, p components.Priority) components.Column {
		return components.Column{Key: k, Label: k, Priority: p}
	}
	titled := []components.Row{{Title: "Draft 42"}}
	untitled := []components.Row{{Title: ""}}
	nine := []components.Column{col("t", components.PTitle)}
	for i := range 8 {
		nine = append(nine, col(fmt.Sprintf("m%d", i), components.PMeta))
	}
	cases := []struct {
		name    string
		p       components.DataListProps
		wantErr bool
	}{
		{"valid", components.DataListProps{Columns: listColumns(), Rows: untitled}, false},
		{"eight columns", components.DataListProps{Columns: nine[:8], Rows: untitled}, false},
		{"two PTitle", components.DataListProps{Columns: []components.Column{col("a", components.PTitle), col("b", components.PTitle)}, Rows: titled}, true},
		{"nine columns", components.DataListProps{Columns: nine, Rows: titled}, true},
		{"no columns", components.DataListProps{Columns: nil, Rows: titled}, true},
		{"two PFigure", components.DataListProps{Columns: []components.Column{col("t", components.PTitle), col("a", components.PFigure), col("b", components.PFigure)}, Rows: titled}, true},
		{"two PState", components.DataListProps{Columns: []components.Column{col("t", components.PTitle), col("a", components.PState), col("b", components.PState)}, Rows: titled}, true},
		{"no PTitle and empty Title", components.DataListProps{Columns: []components.Column{col("a", components.PFigure), col("d", components.PMeta)}, Rows: untitled}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.p.Validate()
			if c.wantErr && err == nil {
				t.Error("Validate() = nil, want an error")
			}
			if !c.wantErr && err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestDataList_LowPriorityMarked(t *testing.T) {
	cols := append(listColumns(),
		components.Column{Key: "by", Label: "Raised by", Priority: components.PLow},
		components.Column{Key: "site", Label: "Site", Priority: components.Priority(6)},
	)
	r := listRow("Shree Steel Traders")
	r.Values["by"] = "Anita Rao"
	r.Values["site"] = "Hosur Road Unit 2"
	doc := uitest.Render(t, components.DataList(components.DataListProps{Label: "Purchase orders", Columns: cols, Rows: []components.Row{r}}))
	a := doc.Find(`ul.list > li a`)
	if a.Length() != 1 {
		t.Fatalf("want one card link, got %d", a.Length())
	}
	for _, v := range []string{"Anita Rao", "Hosur Road Unit 2"} {
		if leafWith(t, a, v).Closest(`[data-priority="low"]`).Length() == 0 {
			t.Errorf("%q is not marked data-priority=low", v)
		}
	}
	for _, v := range []string{"PO/26-27/0042", "₹94,164.00", "24-10-2026", "Hosur plant expansion", "Shree Steel Traders"} {
		if leafWith(t, a, v).Closest(`[data-priority="low"]`).Length() != 0 {
			t.Errorf("%q is marked low priority", v)
		}
	}
}

func TestDataList_HeaderAriaHidden(t *testing.T) {
	doc := uitest.Render(t, components.DataList(components.DataListProps{
		Label: "Purchase orders", Columns: listColumns(), Rows: []components.Row{listRow("Shree Steel Traders")},
	}))
	var header *goquery.Selection
	doc.Find(`.datalist [aria-hidden="true"]`).Each(func(_ int, s *goquery.Selection) {
		if s.Closest("ul.list").Length() == 0 && header == nil {
			header = s
		}
	})
	if header == nil {
		t.Fatal(`no aria-hidden="true" header row outside ul.list`)
	}
	got := text(header)
	for _, l := range []string{"Number", "Amount", "State", "Date", "Project"} {
		if !strings.Contains(got, l) {
			t.Errorf("header %q lacks %q", got, l)
		}
	}
	if !before(doc, header, doc.Find("ul.list")) {
		t.Error("header row is not before the list")
	}
}

func TestDataList_NoTable(t *testing.T) {
	rows := []components.Row{listRow("Shree Steel Traders"), listRow("Lakshmi Hardware")}
	doc := uitest.Render(t, components.DataList(components.DataListProps{Label: "Purchase orders", Columns: listColumns(), Rows: rows}))
	if n := doc.Find(`.datalist ul.list > li`).Length(); n != 2 {
		t.Fatalf("want 2 list items, got %d", n)
	}
	if n := doc.Find("table, thead, tbody, tr, td, th").Length(); n != 0 {
		t.Errorf("found %d table elements", n)
	}
}

func lineCards(n int) []components.LineCard {
	cards := make([]components.LineCard, n)
	for i := range cards {
		no := i + 1
		cards[i] = components.LineCard{
			No: no, Item: fmt.Sprintf("MS Angle 50x50x5 lot %d", no), Code: fmt.Sprintf("MSA-%03d", no),
			QtyRate: "1,200 kg × ₹62.5000", Amount: "₹75,000.00", Tax: "GST 18%",
			ChangeURL:   fmt.Sprintf("/doc/po/7/lines/%d", no),
			RemoveURL:   fmt.Sprintf("/doc/po/7/lines/%d/remove", no),
			MoveUpURL:   fmt.Sprintf("/doc/po/7/lines/%d/up", no),
			MoveDownURL: fmt.Sprintf("/doc/po/7/lines/%d/down", no),
		}
	}
	return cards
}

func TestLineList_ShowAllAndFind(t *testing.T) {
	ctx := ui.WithShell(context.Background(), ui.Shell{CSRF: "tok-9"})
	p := components.LineListProps{
		Cards: lineCards(25), TaxableTotal: "₹18,75,000.00", AddURL: "/doc/po/7/lines/new",
		ShowAllURL: "/doc/po/7/lines?all=1", FindURL: "/doc/po/7/lines", Find: "angle", Total: 64, Editable: true,
	}
	doc := uitest.RenderCtx(t, ctx, components.LineList(p))
	if n := doc.Find("ol.lines > li").Length(); n != 25 {
		t.Errorf("want 25 cards in ol.lines, got %d", n)
	}
	all := doc.Find(`a[href="/doc/po/7/lines?all=1"]`)
	if all.Length() != 1 || text(all) != "Show all 64 lines" {
		t.Errorf("show-all link count %d text %q", all.Length(), text(all))
	}
	f := doc.Find(`form[method="get"][action="/doc/po/7/lines"]`)
	if f.Length() != 1 {
		t.Fatalf("want one GET find form, got %d", f.Length())
	}
	in := f.Find(`input:not([type="hidden"]):not([type="submit"])`)
	if in.Length() != 1 || in.AttrOr("value", "") != "angle" {
		t.Fatalf("find input count %d value %q", in.Length(), in.AttrOr("value", ""))
	}
	label := in.AttrOr("aria-label", "")
	if id, ok := in.Attr("id"); ok && label == "" {
		label = text(doc.Find(fmt.Sprintf(`label[for=%q]`, id)))
	}
	if label != "Find a line" {
		t.Errorf("find input label = %q, want \"Find a line\"", label)
	}

	p.Cards, p.Total = lineCards(3), 3
	doc = uitest.RenderCtx(t, ctx, components.LineList(p))
	if doc.Find(`a[href="/doc/po/7/lines?all=1"]`).Length() != 0 || doc.Find(`form[method="get"]`).Length() != 0 {
		t.Error("show-all or find shown when every line is on the page")
	}
}

func TestLineList_MoveButtonsArePosts(t *testing.T) {
	ctx := ui.WithShell(context.Background(), ui.Shell{CSRF: "tok-9"})
	p := components.LineListProps{Cards: lineCards(3), Total: 3, AddURL: "/doc/po/7/lines/new", TaxableTotal: "₹2,25,000.00", Editable: true}
	doc := uitest.RenderCtx(t, ctx, components.LineList(p))
	for _, c := range p.Cards {
		for url, word := range map[string]string{c.MoveUpURL: "Move up", c.MoveDownURL: "Move down"} {
			f := doc.Find(fmt.Sprintf(`form[method="post"][action=%q]`, url))
			if f.Length() != 1 {
				t.Errorf("line %d: want one POST form to %s, got %d", c.No, url, f.Length())
				continue
			}
			if f.Find(`input[type="hidden"][name="csrf_token"]`).AttrOr("value", "") != "tok-9" {
				t.Errorf("line %d: %s form has no csrf_token", c.No, word)
			}
			if b := f.Find("button"); b.Length() != 1 || !strings.Contains(text(b)+" "+b.AttrOr("aria-label", ""), word) {
				t.Errorf("line %d: %s button text %q", c.No, word, text(b))
			}
			if doc.Find(fmt.Sprintf(`a[href=%q]`, url)).Length() != 0 {
				t.Errorf("line %d: %s is a GET link", c.No, word)
			}
		}
		if ch := doc.Find(fmt.Sprintf(`a[href=%q]`, c.ChangeURL)); !strings.Contains(text(ch), "Change") {
			t.Errorf("line %d: Change link text %q", c.No, text(ch))
		}
		if rm := doc.Find(fmt.Sprintf(`a[href=%q]`, c.RemoveURL)); !strings.Contains(text(rm), "Remove") {
			t.Errorf("line %d: Remove link text %q", c.No, text(rm))
		}
	}

	p.Editable = false
	doc = uitest.RenderCtx(t, ctx, components.LineList(p))
	if n := doc.Find("ol.lines > li").Length(); n != 3 {
		t.Fatalf("read-only list: want 3 cards, got %d", n)
	}
	if n := doc.Find(`form[method="post"]`).Length(); n != 0 {
		t.Errorf("read-only list has %d POST forms", n)
	}
}

// gridRows builds n grid rows whose cells are real inputs with ids
// grid-<no>-<field>. Row 1 has no move up and the last row no move down.
func gridRows(n int) []components.LineGridRow {
	rows := make([]components.LineGridRow, n)
	for i := range rows {
		no := i + 1
		f := func(field, label, value string) components.FieldProps {
			return components.FieldProps{
				ID: fmt.Sprintf("grid-%d-%s", no, field), Name: field, Label: label, Value: value,
			}
		}
		rows[i] = components.LineGridRow{
			No: no,
			Cells: []components.GridCell{
				{Label: "Item", Input: components.TextInput(f("item", "Item", "MSA-050 MS Angle 50x50x5"))},
				{Label: "Qty", Input: components.QtyInput(f("qty", "Qty", "1,200"), false, "kg")},
				{Label: "UoM", Input: components.TextInput(f("uom", "UoM", "kg"))},
				{Label: "Rate", Input: components.RateInput(f("rate", "Rate", "62.50"))},
				{Label: "Tax", Input: components.TextInput(f("tax", "Tax", "GST 18%"))},
			},
			Amount:      "₹75,000.00",
			PostURL:     fmt.Sprintf("/doc/po/7/lines/%d/grid", no),
			RemoveURL:   fmt.Sprintf("/doc/po/7/lines/%d/remove", no),
			MoveUpURL:   fmt.Sprintf("/doc/po/7/lines/%d/up", no),
			MoveDownURL: fmt.Sprintf("/doc/po/7/lines/%d/down", no),
		}
	}
	if n > 0 {
		rows[0].MoveUpURL = ""
		rows[n-1].MoveDownURL = ""
	}
	return rows
}

func TestLineGrid_UniqueIDs(t *testing.T) {
	ctx := ui.WithShell(context.Background(), ui.Shell{CSRF: "tok-9"})
	both := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := components.LineList(components.LineListProps{
			Cards: lineCards(3), Total: 3, AddURL: "/doc/po/7/lines/new", TaxableTotal: "₹2,25,000.00", Editable: true,
		}).Render(ctx, w); err != nil {
			return err
		}
		return components.LineGrid(gridRows(3), "₹2,25,000.00").Render(ctx, w)
	})
	doc := uitest.RenderCtx(t, ctx, both)
	for no := 1; no <= 3; no++ {
		for _, id := range []string{fmt.Sprintf("grid-row-%d", no), fmt.Sprintf("grid-%d-qty", no), fmt.Sprintf("grid-%d-rate", no)} {
			if doc.Find("#"+id).Length() == 0 {
				t.Errorf("no element with id %q", id)
			}
		}
	}
	seen := map[string]int{}
	doc.Find("[id]").Each(func(_ int, s *goquery.Selection) { seen[s.AttrOr("id", "")]++ })
	for id, n := range seen {
		if n > 1 {
			t.Errorf("id %q used %d times", id, n)
		}
	}
}

func TestLineGrid_RowSaveAttributes(t *testing.T) {
	rows := gridRows(3)
	doc := uitest.Render(t, components.LineGrid(rows, "₹2,25,000.00"))
	ed := doc.Find("#line-editor")
	if ed.Length() != 1 {
		t.Fatalf("want one #line-editor wrapper, got %d", ed.Length())
	}
	if doc.Find(`[x-data="linegrid"]`).Length() != 1 {
		t.Error(`no x-data="linegrid"`)
	}
	forms := doc.Find(`form[id^="grid-row-"]`)
	if forms.Length() != len(rows) {
		t.Fatalf("want %d row forms, got %d", len(rows), forms.Length())
	}
	for _, r := range rows {
		f := doc.Find(fmt.Sprintf("form#grid-row-%d", r.No))
		if f.Length() != 1 {
			t.Errorf("no form#grid-row-%d", r.No)
			continue
		}
		if !holds(ed, f) {
			t.Errorf("row %d form is outside #line-editor", r.No)
		}
		want := map[string]string{
			"hx-post":    r.PostURL,
			"hx-trigger": "change",
			"hx-target":  "this",
			"hx-swap":    "outerHTML",
			"hx-include": "#doc-version",
			"hx-sync":    "#line-editor:queue all",
		}
		for attr, v := range want {
			if got := f.AttrOr(attr, ""); got != v {
				t.Errorf("row %d %s = %q, want %q", r.No, attr, got, v)
			}
		}
	}
	if n := doc.Find(`[hx-target="#lines"]`).Length(); n != 0 {
		t.Errorf("%d elements target #lines", n)
	}
	if n := doc.Find(`input[name="version"], #doc-version`).Length(); n != 0 {
		t.Errorf("grid renders %d version inputs", n)
	}
}
