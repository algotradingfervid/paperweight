package docscreen_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-21 (CD-275 UX-E3, UX-E7; HOOK-037, HOOK-038), on the real engine and
// the real AUTH service through the Hooked env (CD-125): a Module LineCard's
// receipt shows on the Items step, the check page and the document page;
// PageExtra.Files replaces the read-only Attachments card of a
// non-Attachable DocType in the side panel, with no Remove.

// Test values.
const (
	m21PhotoURL    = "/files/71"
	m21ReceiptAlt  = "Receipt for line "
	m21NotYet      = "No receipt yet"
	m21FilesTitle  = "Other files"
	m21Attachments = "Attachments"
	m21ReceiptName = "IMG_2041.jpg"
	m21OtherName   = "approval-mail.pdf"
)

// m21Photo is the receipt a module's LineCard puts on line 1.
func m21Photo() *components.Attachment {
	return &components.Attachment{ID: 71, Name: m21ReceiptName, Size: "2.3 MB", UploadedBy: "Deepak Nair", UploadedAt: "24-10-2026 10:30", URL: m21PhotoURL, Kind: "image"}
}

// m21Receipt and m21Other are the document's two stored files, as Extras
// gives them (no RemoveURL).
func m21Receipt() components.Attachment { return *m21Photo() }

func m21Other() components.Attachment {
	return components.Attachment{ID: 92, Name: m21OtherName, Size: "120 KB", UploadedBy: "Priya Shah", UploadedAt: "25-10-2026 09:10", URL: "/files/92", Kind: "file"}
}

// m21ReceiptCard is a Module.LineCard: line 1 has the photo, line 2
// neither a receipt nor a reason ("No receipt yet" with the warning).
func m21ReceiptCard(_ docscreen.Ctx, l docscreen.LineView) components.LineCard {
	c := l.Card
	switch l.No {
	case 1:
		c.Receipt = m21Photo()
	case 2:
		c.ReceiptNote, c.ReceiptWarn = m21NotYet, true
	}
	return c
}

// m21WantReceipts checks sel's two line cards: card 1 shows the photo as a
// lazy thumbnail linking to the file, with alt "Receipt for line 1", and
// card 2 reads "No receipt yet" with no thumbnail.
func m21WantReceipts(t *testing.T, sel *goquery.Selection, what string) {
	t.Helper()
	cards := sel.Find(".lines-card")
	if cards.Length() != 2 {
		t.Fatalf("%s: %d line cards, want 2", what, cards.Length())
	}
	img := cards.Eq(0).Find(`a[href="` + m21PhotoURL + `"] img`)
	if img.Length() != 1 {
		t.Errorf("%s: card 1 has %d thumbnails linking to %s, want 1; card %q", what, img.Length(), m21PhotoURL, norm(cards.Eq(0).Text()))
	} else {
		if got := img.AttrOr("alt", ""); got != m21ReceiptAlt+"1" {
			t.Errorf("%s: card 1 thumbnail alt = %q, want %q", what, got, m21ReceiptAlt+"1")
		}
		if got := img.AttrOr("loading", ""); got != "lazy" {
			t.Errorf("%s: card 1 thumbnail loading = %q, want lazy", what, got)
		}
		if got := img.AttrOr("src", ""); got != m21PhotoURL {
			t.Errorf("%s: card 1 thumbnail src = %q, want %q", what, got, m21PhotoURL)
		}
	}
	if !strings.Contains(norm(cards.Eq(1).Text()), m21NotYet) {
		t.Errorf("%s: card 2 = %q, want it to read %q", what, norm(cards.Eq(1).Text()), m21NotYet)
	}
	if strings.Contains(norm(cards.Eq(0).Text()), m21NotYet) {
		t.Errorf("%s: card 1, which has a receipt, reads %q", what, m21NotYet)
	}
	if cards.Eq(1).Find("img").Length() != 0 {
		t.Errorf("%s: card 2, which has no receipt, shows an img", what)
	}
}

// HOOK-037: a Module LineCard setting Receipt (and ReceiptNote with
// ReceiptWarn) shows it on the Items step's cards, the check page and,
// once submitted, the document page.
func TestModule_LineCardReceipt(t *testing.T) {
	e, id := m01bHooked(t, docscreen.Module{LineCard: m21ReceiptCard})
	base := m01bURL(id)
	for _, path := range []string{"/edit/items", "/edit/check"} {
		page := m01bOK(t, e.get(t, base+path), "GET "+path)
		main := page.Find("main#main")
		m21WantReceipts(t, main, "GET "+path)
	}

	ver := m01bRecord(t, e, id).Version
	sub, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, id, doc.ActionSubmit, doc.ActionInput{Version: ver})
	if err != nil || sub.State != doctype.StateApproved {
		t.Fatalf("submitting Hooked %d: state %s, err %v; want approved", id, sub.State, err)
	}
	page := m01bOK(t, e.get(t, base), "GET document page")
	m21WantReceipts(t, page.Find(".doc-main"), "document page")
}

// m21FilesModule's Page returns files as PageExtra.Files.
func m21FilesModule(files *docscreen.FilesCard) docscreen.Module {
	return docscreen.Module{Page: func(docscreen.Ctx) (docscreen.PageExtra, error) {
		return docscreen.PageExtra{Files: files}, nil
	}}
}

// m21Removable is m21Other with the RemoveURL a module might leave on it;
// the page must still offer no Remove (CD-275 UX-E7).
func m21Removable() components.Attachment {
	a := m21Other()
	a.RemoveURL = "/files/92/remove"
	return a
}

// m21ReadOnly is TestDocPage_ReadOnlyOutsideDraft's check without its
// attachments exclusion: every control on the page is a hidden input of a
// form posting one of the document's own actions.
func m21ReadOnly(t *testing.T, page *goquery.Document, base, what string) {
	t.Helper()
	app := page.Find(".app-page")
	if app.Length() == 0 {
		t.Fatalf("%s: no .app-page", what)
	}
	app.Find("input, select, textarea").Each(func(_ int, el *goquery.Selection) {
		html, _ := goquery.OuterHtml(el)
		form := formOf(page, el)
		action, method := form.AttrOr("action", ""), form.AttrOr("method", "")
		if !strings.EqualFold(method, "post") || !strings.HasPrefix(action, base+"/") {
			t.Errorf("%s: control outside an action form: %s (form action %q)", what, html, action)
			return
		}
		if typ, _ := el.Attr("type"); goquery.NodeName(el) != "input" || typ != "hidden" {
			t.Errorf("%s: visible control in an action form: %s", what, html)
		}
	})
	if n := removeControls(app); n != 0 {
		t.Errorf("%s: %d Remove controls, want none (read-only files)", what, n)
	}
}

// m21SideCard is the one card titled title on the page, which must sit in
// the side panel after the state badge; it fails otherwise.
func m21SideCard(t *testing.T, page *goquery.Document, title, what string) *goquery.Selection {
	t.Helper()
	main := page.Find("main#main")
	all := checkSection(main, title)
	if all.Length() != 1 {
		t.Fatalf("%s: %d cards titled %q, want 1", what, all.Length(), title)
	}
	side := main.Find(".doc-side")
	if side.Length() != 1 || !side.Contains(all.Get(0)) {
		t.Errorf("%s: the %q card is not in the side panel (.doc-side)", what, title)
	}
	if st := main.Find(".doc-head__top .badge"); st.Length() != 1 || !inOrder(page, st, all) {
		t.Errorf("%s: the %q card is not after the state badge", what, title)
	}
	return all
}

// HOOK-038: on a non-Attachable DocType, Page returning Files nil keeps
// today's read-only Attachments card; a FilesCard with Items renders in its
// place (Title, or "Attachments" when empty) listing only its items,
// without Remove even when an item carries a RemoveURL; a FilesCard with no
// Items removes the card. An Attachable DocType's card is unchanged by Files.
func TestModule_PageFiles(t *testing.T) {
	e := newHookedEnv(t)
	id := m04ApprovedHooked(t, e)
	base := m04URL(docscreentest.Hooked, id)
	if dt, _ := doctype.Lookup(docscreentest.Hooked); dt.Attachable {
		t.Fatalf("%s sets Attachable; this test needs a DocType without it", docscreentest.Hooked)
	}
	e.extras.SetAttachments(docscreentest.Hooked, id, []components.Attachment{m21Receipt(), m21Other()})
	get := func(t *testing.T, m docscreen.Module, what string) *goquery.Document {
		t.Helper()
		m04Mount(e, docscreentest.Hooked, m)
		return m04Page(t, e.get(t, base), http.StatusOK, what)
	}

	t.Run("nil keeps the card", func(t *testing.T) {
		page := get(t, m21FilesModule(nil), "Files nil")
		card := m21SideCard(t, page, m21Attachments, "Files nil")
		text := norm(card.Find(".attachments").Text())
		if !strings.Contains(text, m21ReceiptName) || !strings.Contains(text, m21OtherName) {
			t.Errorf("Files nil: the card lists %q, want both stored files", text)
		}
		m21ReadOnly(t, page, base, "Files nil")
	})

	for _, c := range []struct {
		name, title, want string
	}{
		{"titled", m21FilesTitle, m21FilesTitle},
		{"untitled", "", m21Attachments},
	} {
		t.Run(c.name, func(t *testing.T) {
			page := get(t, m21FilesModule(&docscreen.FilesCard{Title: c.title, Items: []components.Attachment{m21Removable()}}), c.name)
			card := m21SideCard(t, page, c.want, c.name)
			list := card.Find(".attachments")
			if list.Length() != 1 {
				t.Fatalf("%s: the card has %d attachment lists, want components.Attachments' one", c.name, list.Length())
			}
			if text := norm(list.Text()); !strings.Contains(text, m21OtherName) || strings.Contains(text, m21ReceiptName) {
				t.Errorf("%s: the card lists %q, want only the module's %s", c.name, text, m21OtherName)
			}
			if c.want != m21Attachments && checkSection(page.Find("main#main"), m21Attachments).Length() != 0 {
				t.Errorf("%s: today's Attachments card is still shown beside the module's", c.name)
			}
			if strings.Contains(norm(page.Find("main#main").Text()), m21ReceiptName) {
				t.Errorf("%s: the page still lists %s, which the module left out", c.name, m21ReceiptName)
			}
			m21ReadOnly(t, page, base, c.name)
		})
	}

	for _, c := range []struct {
		name  string
		items []components.Attachment
	}{
		{"nil items", nil},
		{"empty items", []components.Attachment{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			page := get(t, m21FilesModule(&docscreen.FilesCard{Title: m21FilesTitle, Items: c.items}), c.name)
			main := page.Find("main#main")
			for _, title := range []string{m21Attachments, m21FilesTitle} {
				if n := checkSection(main, title).Length(); n != 0 {
					t.Errorf("%s: %d cards titled %q, want none", c.name, n, title)
				}
			}
			if n := main.Find(".attachments").Length(); n != 0 {
				t.Errorf("%s: %d attachment lists on the page, want none", c.name, n)
			}
			m21ReadOnly(t, page, base, c.name)
		})
	}

	t.Run("module items keep their RemoveURL", func(t *testing.T) {
		files := &docscreen.FilesCard{Title: m21FilesTitle, Items: []components.Attachment{m21Removable()}}
		page := get(t, m21FilesModule(files), "module items")
		if n := removeControls(page.Find(".app-page")); n != 0 {
			t.Errorf("module items: %d Remove controls, want none", n)
		}
		if got, want := files.Items[0].RemoveURL, m21Removable().RemoveURL; got != want {
			t.Errorf("after the render, the module's own item has RemoveURL %q, want %q: the page cleared it in the module's slice", got, want)
		}
	})

	// A cancelled Attachable document has no Attachments card (attachOf is
	// nil, CD-243); the module's Files must not add one.
	t.Run("attachable cancelled unchanged", func(t *testing.T) {
		de := newDocEnv(t, allRights...)
		de.seed(doctype.StateCancelled)
		de.extras.SetAttachments(docscreentest.Sample, 42, []components.Attachment{receiptJPG()})
		render := func(m *docscreen.Module) string {
			de.d.Modules = nil
			if m != nil {
				de.d.Modules = map[string]docscreen.Module{docscreentest.Sample: *m}
			}
			de.remount()
			rec := de.get(t, doc42)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %.300s", rec.Code, rec.Body)
			}
			h, err := uitest.Doc(t, rec).Find("main#main").Html()
			if err != nil {
				t.Fatal(err)
			}
			return h
		}
		want := render(nil)
		m := m21FilesModule(&docscreen.FilesCard{Title: m21FilesTitle, Items: []components.Attachment{m21Other()}})
		if got := render(&m); got != want {
			t.Errorf("Files changed a cancelled Attachable DocType's page:\ngot  %.2000s\nwant %.2000s", got, want)
		}
	})

	t.Run("attachable unchanged", func(t *testing.T) {
		de := newDocEnv(t, allRights...)
		de.seed(doctype.StateApproved)
		de.extras.SetAttachments(docscreentest.Sample, 42, []components.Attachment{receiptJPG()})
		render := func(m *docscreen.Module) string {
			de.d.Modules = nil
			if m != nil {
				de.d.Modules = map[string]docscreen.Module{docscreentest.Sample: *m}
			}
			de.remount()
			rec := de.get(t, doc42)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %.300s", rec.Code, rec.Body)
			}
			h, err := uitest.Doc(t, rec).Find("main#main").Html()
			if err != nil {
				t.Fatal(err)
			}
			return h
		}
		want := render(nil)
		if !strings.Contains(want, "receipt.jpg") {
			t.Fatal("the Attachable page doesn't list receipt.jpg; the guard would prove nothing")
		}
		for _, f := range []*docscreen.FilesCard{
			{Title: m21FilesTitle, Items: []components.Attachment{m21Removable()}},
			{Title: m21FilesTitle},
		} {
			m := m21FilesModule(f)
			if got := render(&m); got != want {
				t.Errorf("Files %+v changed an Attachable DocType's page:\ngot  %.2000s\nwant %.2000s", *f, got, want)
			}
		}
	})
}
