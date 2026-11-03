package components_test

import (
	"bytes"
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/icons"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-21 (CD-275 UX-E3, HOOK-036): LineCard.Receipt shows the line's receipt
// on its card, a 64 px lazy thumbnail for an image and the attachments
// switch's file chip for PDF and HEIC, with alt "Receipt for line {n}";
// ReceiptNote is a line of text on the card, and ReceiptWarn adds the alert
// icon ("No receipt yet"). A card that sets none of them renders as before.

// Test values.
const (
	m21PhotoURL   = "/files/71"
	m21PDFURL     = "/files/72"
	m21PDFName    = "bill-2410.pdf"
	m21HEICURL    = "/files/73"
	m21HEICText   = "HEIC image, opens as download"
	m21NoReceipt  = "No receipt: Auto rickshaw, no bill <given> & paid"
	m21NotYet     = "No receipt yet"
	m21LinesCSS   = "../css/components/lines.css"
	m21AlertIcon  = "alert-circle"
	m21ReceiptAlt = "Receipt for line "
)

// m21Golden is today's LineList of two zero-receipt cards (editable, then
// read-only), captured before M0-21 at f993da7: a card that sets no
// receipt field renders byte-identically to it.
var m21Golden = map[bool]string{
	true:  `<div class="linelist"><ol class="lines"><li class="lines-card"><div class="lines-top"><span class="lines-no">1</span> <span class="lines-name"><span>Food</span> <span class="code"></span></span> <span class="num lines-amount">₹450.00</span></div><p class="lines-meta"><span class="num">24-10-2026</span> <span></span></p><p class="lines-meta">Lunch at site canteen</p><div class="lines-actions"><a href="/doc/x/1/lines/1" class="btn tap btn-ghost btn--ghost" aria-label="Change line 1"><svg class="icon" aria-hidden="true" focusable="false" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"></path><path d="m15 5 4 4"></path></svg><span>Change</span></a><a href="/doc/x/1/lines/1/remove" class="btn tap btn-ghost ghost-danger btn--danger-quiet" aria-label="Remove line 1"><svg class="icon" aria-hidden="true" focusable="false" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10 11v6"></path><path d="M14 11v6"></path><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"></path><path d="M3 6h18"></path><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path></svg><span>Remove</span></a><div class="lines-moves"><form method="post" action="/doc/x/1/lines/1/move?dir=down"><input type="hidden" name="csrf_token" value=""><input type="hidden" name="version" value="4"><button type="submit" class="btn tap btn-ghost btn--ghost" data-busy-label="Saving…" aria-label="Move down line 1" data-move><svg class="icon" aria-hidden="true" focusable="false" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14"></path><path d="m19 12-7 7-7-7"></path></svg></button></form></div></div></li><li class="lines-card"><div class="lines-top"><span class="lines-no">2</span> <span class="lines-name"><span>Lodging</span> <span class="code">LODGE</span></span> <span class="num lines-amount">₹4,500.00</span></div><p class="lines-meta"><span class="num">22-10-2026</span> <span>GST ₹482.14</span></p><p class="lines-meta">Received 1 of 1</p><div class="lines-actions"><a href="/doc/x/1/lines/2" class="btn tap btn-ghost btn--ghost" aria-label="Change line 2"><svg class="icon" aria-hidden="true" focusable="false" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"></path><path d="m15 5 4 4"></path></svg><span>Change</span></a><a href="/doc/x/1/lines/2/remove" class="btn tap btn-ghost ghost-danger btn--danger-quiet" aria-label="Remove line 2"><svg class="icon" aria-hidden="true" focusable="false" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10 11v6"></path><path d="M14 11v6"></path><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"></path><path d="M3 6h18"></path><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path></svg><span>Remove</span></a><div class="lines-moves"><form method="post" action="/doc/x/1/lines/2/move?dir=up"><input type="hidden" name="csrf_token" value=""><input type="hidden" name="version" value="4"><button type="submit" class="btn tap btn-ghost btn--ghost" data-busy-label="Saving…" aria-label="Move up line 2" data-move><svg class="icon" aria-hidden="true" focusable="false" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12 7-7 7 7"></path><path d="M12 19V5"></path></svg></button></form></div></div></li></ol><a href="/doc/x/1/lines/new" class="btn tap btn-outline btn--secondary" data-add-line><svg class="icon" aria-hidden="true" focusable="false" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"></path><path d="M12 5v14"></path></svg><span>Add an item</span></a></div>`,
	false: `<div class="linelist"><ol class="lines"><li class="lines-card"><div class="lines-top"><span class="lines-no">1</span> <span class="lines-name"><span>Food</span> <span class="code"></span></span> <span class="num lines-amount">₹450.00</span></div><p class="lines-meta"><span class="num">24-10-2026</span> <span></span></p><p class="lines-meta">Lunch at site canteen</p></li><li class="lines-card"><div class="lines-top"><span class="lines-no">2</span> <span class="lines-name"><span>Lodging</span> <span class="code">LODGE</span></span> <span class="num lines-amount">₹4,500.00</span></div><p class="lines-meta"><span class="num">22-10-2026</span> <span>GST ₹482.14</span></p><p class="lines-meta">Received 1 of 1</p></li></ol></div>`,
}

// m21Cards are the golden's two cards, with no receipt field set.
func m21Cards() []components.LineCard {
	return []components.LineCard{
		{No: 1, Item: "Food", QtyRate: "24-10-2026", Amount: "₹450.00", ChangeURL: "/doc/x/1/lines/1", RemoveURL: "/doc/x/1/lines/1/remove", MoveDownURL: "/doc/x/1/lines/1/move?dir=down", Version: "4", Note: "Lunch at site canteen"},
		{No: 2, Item: "Lodging", Code: "LODGE", QtyRate: "22-10-2026", Amount: "₹4,500.00", Tax: "GST ₹482.14", Received: "Received 1 of 1", ChangeURL: "/doc/x/1/lines/2", RemoveURL: "/doc/x/1/lines/2/remove", MoveUpURL: "/doc/x/1/lines/2/move?dir=up", Version: "4"},
	}
}

func m21Props(cards []components.LineCard, editable bool) components.LineListProps {
	return components.LineListProps{Cards: cards, Total: len(cards), Editable: editable, AddURL: "/doc/x/1/lines/new"}
}

// m21Raw is p's LineList HTML as rendered, byte for byte.
func m21Raw(t *testing.T, p components.LineListProps) string {
	t.Helper()
	var b bytes.Buffer
	if err := components.LineList(p).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func m21Photo() *components.Attachment {
	return &components.Attachment{ID: 71, Name: "IMG_2041.jpg", Size: "2.3 MB", UploadedBy: "Deepak Nair", UploadedAt: "24-10-2026 10:30", URL: m21PhotoURL, Kind: "image"}
}

func m21PDF() *components.Attachment {
	return &components.Attachment{ID: 72, Name: m21PDFName, Size: "312 KB", UploadedBy: "Deepak Nair", UploadedAt: "24-10-2026 10:31", URL: m21PDFURL, Kind: "file"}
}

func m21HEIC() *components.Attachment {
	return &components.Attachment{ID: 73, Name: "IMG_2042.heic", Size: "1.9 MB", UploadedBy: "Deepak Nair", UploadedAt: "24-10-2026 10:32", URL: m21HEICURL, Kind: "heic"}
}

// m21Card is card no of the rendered list.
func m21Card(t *testing.T, doc *goquery.Document, no int) *goquery.Selection {
	t.Helper()
	li := doc.Find("li.lines-card")
	if li.Length() < no {
		t.Fatalf("%d cards, want at least %d", li.Length(), no)
	}
	return li.Eq(no - 1)
}

// m21Link is the one link in card to url; it fails unless there is one.
func m21Link(t *testing.T, card *goquery.Selection, url, what string) *goquery.Selection {
	t.Helper()
	a := card.Find(`a[href="` + url + `"]`)
	if a.Length() != 1 {
		t.Fatalf("%s: %d links to %s on the card, want 1; card %s", what, a.Length(), url, m04Norm(card.Text()))
	}
	return a
}

// m21IconSVG is icons.Icon(name)'s inner markup, which tells one icon from
// another (every icon is <svg class="icon">).
func m21IconSVG(t *testing.T, name string) string {
	t.Helper()
	h, err := uitest.Render(t, icons.Icon(name)).Find("svg").Html()
	if err != nil || h == "" {
		t.Fatalf("icon %q renders no svg: %v", name, err)
	}
	return h
}

// m21HasIcon reports whether sel holds icon name's svg.
func m21HasIcon(t *testing.T, sel *goquery.Selection, name string) bool {
	t.Helper()
	want := m21IconSVG(t, name)
	return sel.Find("svg").FilterFunction(func(_ int, s *goquery.Selection) bool {
		h, _ := s.Html()
		return h == want
	}).Length() > 0
}

// m21SizeRE finds a 64 px length (4rem or 64px, not 64rem).
var m21SizeRE = regexp.MustCompile(`(?:^|[^\w.])(?:4rem|64px)\b`)

// m21CSSBlock is the body of the first rule in css whose selector names
// class (".class" or "@utility class"), nested rules included; "" when
// there is none.
func m21CSSBlock(css, class string) string {
	re := regexp.MustCompile(`(?:@utility\s+|\.)` + regexp.QuoteMeta(class) + `(?:[^\w-][^{;}]*)?\{`)
	loc := re.FindStringIndex(css)
	if loc == nil {
		return ""
	}
	depth := 1
	for i := loc[1]; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return css[loc[1]:i]
			}
		}
	}
	return css[loc[1]:]
}

// m21Sized64 reports whether lines.css sizes the thumbnail at 64 px through
// a class on the thumbnail, its link or a wrapper inside the card
// (CD-275: an @utility in css/components/lines.css).
func m21Sized64(t *testing.T, img *goquery.Selection) bool {
	t.Helper()
	css, err := os.ReadFile(m21LinesCSS)
	if err != nil {
		t.Fatalf("reading %s: %v", m21LinesCSS, err)
	}
	var classes []string
	for s := img; s.Length() > 0 && !s.Is("li.lines-card"); s = s.Parent() {
		classes = append(classes, strings.Fields(s.AttrOr("class", ""))...)
	}
	for _, c := range classes {
		if c == "lines-card" {
			continue
		}
		if m21SizeRE.MatchString(m21CSSBlock(string(css), c)) {
			return true
		}
	}
	return false
}

// HOOK-036: a card with Receipt shows the image as a 64 px lazy thumbnail
// linking to the file, or the attachments switch's chip for PDF and HEIC,
// with alt "Receipt for line {n}"; ReceiptNote shows as text, and
// ReceiptWarn adds the alert icon; a zero card renders as before.
func TestLineCard_Receipt(t *testing.T) {
	t.Run("image", func(t *testing.T) {
		cards := m21Cards()
		cards[1].Receipt = m21Photo()
		doc := uitest.Render(t, components.LineList(m21Props(cards, true)))
		card := m21Card(t, doc, 2)
		a := m21Link(t, card, m21PhotoURL, "image receipt")
		img := a.Find("img")
		if img.Length() != 1 {
			t.Fatalf("image receipt: the link to %s holds %d img, want 1", m21PhotoURL, img.Length())
		}
		if got := img.AttrOr("src", ""); got != m21PhotoURL {
			t.Errorf("thumbnail src = %q, want %q", got, m21PhotoURL)
		}
		if got := img.AttrOr("loading", ""); got != "lazy" {
			t.Errorf("thumbnail loading = %q, want lazy", got)
		}
		if got := img.AttrOr("alt", ""); got != m21ReceiptAlt+"2" {
			t.Errorf("thumbnail alt = %q, want %q", got, m21ReceiptAlt+"2")
		}
		if !m21Sized64(t, img) {
			t.Errorf("no class on the thumbnail or its link has a 64 px size (4rem or 64px) in %s (CD-275 UX-E3)", m21LinesCSS)
		}
		if n := m21Card(t, doc, 1).Find("img, a[href^='/files/']").Length(); n != 0 {
			t.Errorf("card 1 has no receipt but shows %d receipt elements", n)
		}
	})

	t.Run("pdf chip", func(t *testing.T) {
		cards := m21Cards()
		cards[0].Receipt = m21PDF()
		doc := uitest.Render(t, components.LineList(m21Props(cards, false)))
		card := m21Card(t, doc, 1)
		a := m21Link(t, card, m21PDFURL, "PDF receipt")
		if a.Find("img").Length() != 0 || card.Find("img").Length() != 0 {
			t.Error("a PDF receipt renders an img, want the file chip")
		}
		if !strings.Contains(m04Norm(a.Text()), m21PDFName) {
			t.Errorf("PDF chip reads %q, want the file name %q (attachments switch)", m04Norm(a.Text()), m21PDFName)
		}
		if !m21HasIcon(t, a, "file-text") {
			t.Error("PDF chip has no file-text icon (attachments switch)")
		}
		if _, ok := a.Attr("download"); ok {
			t.Error("PDF chip is a download link; only HEIC downloads (attachments switch)")
		}
	})

	t.Run("heic chip", func(t *testing.T) {
		cards := m21Cards()
		cards[0].Receipt = m21HEIC()
		doc := uitest.Render(t, components.LineList(m21Props(cards, false)))
		card := m21Card(t, doc, 1)
		a := m21Link(t, card, m21HEICURL, "HEIC receipt")
		if card.Find("img").Length() != 0 {
			t.Error("a HEIC receipt renders an img, which browsers can't show; want the file chip")
		}
		if _, ok := a.Attr("download"); !ok {
			t.Error("HEIC chip has no download attribute (attachments switch)")
		}
		if !strings.Contains(m04Norm(a.Text()), m21HEICText) {
			t.Errorf("HEIC chip reads %q, want %q (attachments switch)", m04Norm(a.Text()), m21HEICText)
		}
		if !m21HasIcon(t, a, "file") {
			t.Error("HEIC chip has no file icon (attachments switch)")
		}
	})

	t.Run("receipt note", func(t *testing.T) {
		cards := m21Cards()
		cards[1].ReceiptNote = m21NoReceipt
		doc := uitest.Render(t, components.LineList(m21Props(cards, true)))
		card := m21Card(t, doc, 2)
		note := m04Shows(card, m21NoReceipt)
		if note.Length() != 1 {
			t.Fatalf("card 2 = %q, want one element reading %q", m04Norm(card.Text()), m21NoReceipt)
		}
		if doc.Find("given").Length() != 0 {
			t.Error("the receipt note's markup characters were rendered as markup")
		}
		if m21HasIcon(t, card, m21AlertIcon) {
			t.Error("a card with ReceiptNote but not ReceiptWarn shows the alert icon")
		}
		if card.Find("img, a[href^='/files/']").Length() != 0 {
			t.Error("a card with no Receipt shows a thumbnail or file link")
		}
		if strings.Contains(m04Norm(m21Card(t, doc, 1).Text()), "No receipt") {
			t.Error("card 1 shows card 2's receipt note")
		}
	})

	t.Run("no receipt yet", func(t *testing.T) {
		cards := m21Cards()
		cards[0].ReceiptNote, cards[0].ReceiptWarn = m21NotYet, true
		doc := uitest.Render(t, components.LineList(m21Props(cards, true)))
		card := m21Card(t, doc, 1)
		note := m04Shows(card, m21NotYet)
		if note.Length() != 1 {
			t.Fatalf("card 1 = %q, want one element reading %q", m04Norm(card.Text()), m21NotYet)
		}
		// The icon sits with the words: in the element showing them, or
		// in the line (its parent) that holds both.
		if !m21HasIcon(t, note, m21AlertIcon) && !m21HasIcon(t, note.Parent(), m21AlertIcon) {
			h, _ := goquery.OuterHtml(note.Parent())
			t.Errorf("%q has no %s icon beside it (never colour alone): %s", m21NotYet, m21AlertIcon, h)
		}
		if m21HasIcon(t, m21Card(t, doc, 2), m21AlertIcon) {
			t.Error("card 2, without ReceiptWarn, shows the alert icon")
		}
	})

	t.Run("zero card unchanged", func(t *testing.T) {
		for _, editable := range []bool{true, false} {
			if got := m21Raw(t, m21Props(m21Cards(), editable)); got != m21Golden[editable] {
				t.Errorf("editable %v: zero-receipt cards changed:\ngot  %s\nwant %s", editable, got, m21Golden[editable])
			}
		}
		// Beside a card with a receipt, a zero card is still today's.
		cards := m21Cards()
		cards[1].Receipt, cards[1].ReceiptNote, cards[1].ReceiptWarn = m21Photo(), m21NotYet, true
		mixed := uitest.Render(t, components.LineList(m21Props(cards, true)))
		golden, err := goquery.NewDocumentFromReader(strings.NewReader(m21Golden[true]))
		if err != nil {
			t.Fatal(err)
		}
		before, _ := golden.Find("li.lines-card").Eq(0).Html()
		after, _ := mixed.Find("li.lines-card").Eq(0).Html()
		if before != after {
			t.Errorf("a zero card beside a receipt card changed:\nbefore %s\nafter  %s", before, after)
		}
	})
}
