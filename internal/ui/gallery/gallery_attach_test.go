package gallery

import (
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// attachVariants are the mock's sections (UX design 2026-10-31 §6 "Gallery
// mock"), by section id and heading.
var attachVariants = []struct{ id, heading string }{
	{"att-a", "(a) Writer, draft, no files"},
	{"att-b", "(b) Writer, draft, three files"},
	{"att-c", "(c) Writer, approved, three files"},
	{"att-d", "(d) Reader"},
	{"att-e", "(e) Refused"},
	{"att-f", "(f) 20 files, longest names"},
	{"att-g", "(g) Remove confirm"},
}

func TestGallery_AttachmentsRenders(t *testing.T) {
	rec := uitest.Do(t, Handler(), http.MethodGet, "/_gallery/attachments", nil)
	if strings.Contains(rec.Body.String(), "style=") {
		t.Error("attachments: page has a style= attribute")
	}
	d := authPage(t, "attachments")
	if idx := get(t, Handler(), "/_gallery"); idx != nil && idx.Find(`a[href="/_gallery/attachments"]`).Length() == 0 {
		t.Error("index: no link to /_gallery/attachments")
	}
	sec := map[string]*goquery.Selection{}
	for _, v := range attachVariants {
		s := d.Find("section#" + v.id)
		sec[v.id] = s
		if got := strings.TrimSpace(s.Find("h2").First().Text()); got != v.heading {
			t.Errorf("%s: heading %q, want %q", v.id, got, v.heading)
		}
		if v.id != "att-g" && s.Find(`.card-title`).FilterFunction(func(_ int, h *goquery.Selection) bool {
			return strings.TrimSpace(h.Text()) == "Attachments"
		}).Length() != 1 {
			t.Errorf("%s: no Card titled Attachments", v.id)
		}
	}

	upload := func(id string, s *goquery.Selection) {
		t.Helper()
		if strings.TrimSpace(s.Find("legend").Text()) != "Add a file" {
			t.Errorf("%s: no legend Add a file", id)
		}
		if !strings.Contains(s.Find(".field__hint").Text(), attachHint) {
			t.Errorf("%s: hint is not %q", id, attachHint)
		}
		if s.Find(`input[type=file][capture="environment"][accept="image/*"]`).Length() != 1 {
			t.Errorf("%s: no Take photo input", id)
		}
		if s.Find(`input[type=file][accept="`+attachAccept+`"]`).Length() != 1 {
			t.Errorf("%s: no Choose file input with accept %q", id, attachAccept)
		}
	}
	removes := func(s *goquery.Selection) int {
		return s.Find("button").FilterFunction(func(_ int, b *goquery.Selection) bool {
			return strings.TrimSpace(b.Text()) == "Remove"
		}).Length()
	}
	const empty = "No files attached yet."

	if s := sec["att-a"]; !strings.Contains(s.Find(".empty-state").Text(), empty) {
		t.Error("att-a: no empty state")
	}
	upload("att-a", sec["att-a"])

	upload("att-b", sec["att-b"])
	b := sec["att-b"]
	if n := b.Find(".attachments > li").Length(); n != 3 {
		t.Errorf("att-b: %d files, want 3", n)
	}
	if b.Find(`img[alt="receipt.jpg"]`).Length() != 1 || !strings.Contains(b.Text(), "HEIC image, opens as download") || !strings.Contains(b.Find(".attachments__chip").Text(), "quote-shree-steel.pdf") {
		t.Error("att-b: want receipt.jpg thumbnail, a HEIC chip and the quote-shree-steel.pdf chip")
	}
	if n := removes(b); n != 3 {
		t.Errorf("att-b: %d Remove buttons, want 3", n)
	}
	if b.Find(`button[aria-label="Remove receipt.jpg"]`).Length() != 1 {
		t.Error("att-b: no Remove named Remove receipt.jpg")
	}
	for _, id := range []string{"1", "2", "3"} {
		dlg := b.Find("dialog#remove-file-" + id)
		if strings.TrimSpace(dlg.Find("h2").Text()) != "Remove this file?" {
			t.Errorf("att-b: no remove-file-%s dialog titled Remove this file?", id)
		}
	}
	dlg := b.Find("dialog#remove-file-1")
	if !strings.Contains(dlg.Text(), "receipt.jpg will be removed from this document. You can attach it again later.") {
		t.Error("att-b: remove-file-1 has the wrong body")
	}
	if dlg.Find(`form[hx-post] button[type=submit].btn-error[data-busy-label="Removing…"]`).Text() != "Remove file" {
		t.Error("att-b: no danger Remove file submit in an htmx form")
	}
	if strings.TrimSpace(dlg.Find(`button[type=button][data-close-dialog]`).Last().Text()) != "Keep file" {
		t.Error("att-b: no Keep file close button")
	}

	upload("att-c", sec["att-c"])
	if n := sec["att-c"].Find(".attachments > li").Length(); n != 3 || removes(sec["att-c"]) != 0 || sec["att-c"].Find("dialog").Length() != 0 {
		t.Errorf("att-c: %d files, want 3 and no Remove", n)
	}

	dsec := sec["att-d"]
	if dsec.Find(".attachments > li").Length() != 3 || dsec.Find("input[type=file]").Length() != 0 || removes(dsec) != 0 {
		t.Error("att-d: want three files, no file input, no Remove")
	}

	e := sec["att-e"]
	upload("att-e", e)
	if !strings.Contains(e.Find("#att-refused-msg.field__msg--error").Text(), uitest.LongError) {
		t.Error("att-e: message region does not hold uitest.LongError")
	}
	if e.Find(`input[type=file][aria-invalid="true"]`).Length() != 2 {
		t.Error("att-e: want both inputs aria-invalid")
	}

	f := sec["att-f"]
	upload("att-f", f)
	if n := f.Find(".attachments > li").Length(); n != 20 {
		t.Errorf("att-f: %d files, want 20", n)
	}
	if n := f.Find("dialog").Length(); n != 20 {
		t.Errorf("att-f: %d remove dialogs, want 20", n)
	}
	if utf8.RuneCountInString(longFileName) != 120 || !strings.Contains(f.Text(), longFileName) {
		t.Errorf("att-f: want the 120-character name shown (have %d runes)", utf8.RuneCountInString(longFileName))
	}
	if !strings.Contains(f.Text(), "बिल रसीद.jpg") {
		t.Error("att-f: no Devanagari name")
	}

	found := false
	for _, s := range Screens("") {
		if s.Name == "gallery attachments, remove dialog open" && s.URL == "/_gallery/attachments" && s.Setup != nil {
			found = true
		}
	}
	if !found {
		t.Error("Screens has no attachments entry with the remove dialog open")
	}
}
