package components_test

import (
	"os"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-53 (CD-326 (2), (5), (6), (8)): the receipt thumbnail on a line card
// composes the compact Attachments, a Date input echoes its value's weekday
// on every render, and a DataList row can take the warning tone.

// m53Compact is a compact image receipt.
func m53Compact() components.Attachment {
	a := *m21Photo()
	a.Compact = true
	return a
}

// CD-326 (6): a compact item is the thumbnail and one "name · size" line,
// with no uploader line; a compact file chip keeps its name in the chip and
// shows the size. Items without Compact keep the uploader line.
func TestAttachments_Compact(t *testing.T) {
	full := *m21Photo()
	pdf := *m21PDF()
	pdf.Compact = true
	doc := uitest.Render(t, components.Attachments([]components.Attachment{m53Compact(), full, pdf}))
	items := doc.Find("ul.attachments > li")
	if items.Length() != 3 {
		t.Fatalf("%d items, want 3", items.Length())
	}

	img := items.Eq(0)
	if !img.HasClass("attachments-compact") {
		t.Errorf("compact item classes = %q, want attachments-compact", img.AttrOr("class", ""))
	}
	if img.Find("a.attachments__thumb img").Length() != 1 {
		t.Error("compact image item has no thumbnail")
	}
	lines := img.Find("p")
	if lines.Length() != 1 {
		t.Fatalf("compact image item has %d text lines, want 1: %q", lines.Length(), m04Norm(img.Text()))
	}
	if got, want := m04Norm(lines.Text()), "IMG_2041.jpg · 2.3 MB"; got != want {
		t.Errorf("compact line = %q, want %q", got, want)
	}
	if strings.Contains(img.Text(), "Deepak Nair") || strings.Contains(img.Text(), "10:30") {
		t.Errorf("compact item shows who uploaded it: %q", m04Norm(img.Text()))
	}

	if items.Eq(1).HasClass("attachments-compact") {
		t.Error("an item without Compact has the compact class")
	}
	if !strings.Contains(m04Norm(items.Eq(1).Text()), "Deepak Nair · 24-10-2026 10:30") {
		t.Errorf("an item without Compact lost its uploader line: %q", m04Norm(items.Eq(1).Text()))
	}

	chip := items.Eq(2)
	if got := m04Norm(chip.Find("p").Text()); got != "312 KB" {
		t.Errorf("compact file line = %q, want the size only (the chip names it)", got)
	}
	if strings.Contains(chip.Text(), "Deepak Nair") {
		t.Error("compact file item shows who uploaded it")
	}

	css, err := os.ReadFile("../css/components/upload.css")
	if err != nil {
		t.Fatal(err)
	}
	if !m21SizeRE.MatchString(m21CSSBlock(string(css), "attachments-compact")) {
		t.Error("upload.css gives the compact thumbnail no 4rem size")
	}
}

// CD-326 (2), (6): a card's receipt is the compact Attachments (one markup,
// no uploader line), with the card's alt; the card lays an image receipt out
// left of the type and date.
func TestLineCard_ReceiptCompact(t *testing.T) {
	cards := m21Cards()
	cards[1].Receipt = m21Photo()
	doc := uitest.Render(t, components.LineList(m21Props(cards, true)))
	card := m21Card(t, doc, 2)
	li := card.Find(".lines-receipt ul.attachments > li.attachments-compact")
	if li.Length() != 1 {
		h, _ := card.Html()
		t.Fatalf("the card's receipt is not one compact Attachments item: %s", h)
	}
	if got := li.Find("img").AttrOr("alt", ""); got != m21ReceiptAlt+"2" {
		t.Errorf("thumbnail alt = %q, want %q", got, m21ReceiptAlt+"2")
	}
	if strings.Contains(card.Text(), "Deepak Nair") {
		t.Error("the card shows who uploaded the receipt")
	}
	css, err := os.ReadFile(m21LinesCSS)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(css), ".lines-card:has(.lines-receipt .attachments__thumb)") {
		t.Error("lines.css has no layout for a card with a receipt thumbnail")
	}
}

// CD-326 (5): a Date input shows its value's weekday under it on render,
// nothing for a value that isn't a date, and the error instead of it.
func TestDateInput_WeekdayEcho(t *testing.T) {
	for _, c := range []struct{ value, errText, want string }{
		{"24-10-2026", "", "Saturday, 24 October 2026"},
		{"01-11-2026", "", "Sunday, 1 November 2026"},
		{"31-02-2026", "", ""},
		{"", "", ""},
		{"24-10-2026", "Date can't be in the future.", "Date can't be in the future."},
	} {
		p := components.DateProps{FieldProps: components.FieldProps{ID: "f-d", Name: "d", Label: "Date", Value: c.value, Error: c.errText}, Today: "24-10-2026", Yesterday: "23-10-2026"}
		doc := uitest.Render(t, components.DateInput(p))
		if got := m04Norm(doc.Find("#f-d-msg").Text()); got != c.want {
			t.Errorf("value %q error %q: message = %q, want %q", c.value, c.errText, got, c.want)
		}
	}
}

// CD-326 (8), CD-330 (4): a row with Tone warning is tinted with the
// warning token; other rows aren't.
func TestDataList_RowTone(t *testing.T) {
	cols := []components.Column{{Key: "no", Label: "Number", Priority: components.PTitle}}
	rows := []components.Row{
		{Href: "/a", Values: map[string]string{"no": "PO/1"}, Tone: components.ToneWarning},
		{Href: "/b", Values: map[string]string{"no": "PO/2"}},
	}
	doc := uitest.Render(t, components.DataList(components.DataListProps{Label: "Rows", Columns: cols, Rows: rows}))
	links := doc.Find("a.datalist-link")
	if links.Length() != 2 {
		t.Fatalf("%d row links, want 2", links.Length())
	}
	if !links.Eq(0).HasClass("datalist-link--warning") {
		t.Errorf("warning row classes = %q, want datalist-link--warning", links.Eq(0).AttrOr("class", ""))
	}
	if links.Eq(1).HasClass("datalist-link--warning") {
		t.Error("a row without Tone is tinted")
	}
	css, err := os.ReadFile("../css/components/datalist.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m21CSSBlock(string(css), "datalist-link--warning"), "--color-warning") {
		t.Error("datalist.css doesn't tint datalist-link--warning with a warning token")
	}
}
