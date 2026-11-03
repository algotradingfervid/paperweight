package docscreen_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// r7bWantHead fails unless the page's one way back is the wizard head's ‹
// (UI v2 §5.6, CD-387 (6)): a 44 px a.wizard-close with the chevron-left
// icon, aria-label "Back to {label}", going to items; the meta line names
// the document's kind; the page's one h1 is the head's, reading title; and
// the old back-link is gone, so focus mode hides the app header below 64rem.
func r7bWantHead(t *testing.T, page *goquery.Document, label, items, kind, title, what string) {
	t.Helper()
	if page.Find(".app-header").Length() != 0 && page.Find("body.focus-mode").Length() == 0 {
		t.Errorf("%s: body is not in focus mode", what)
	}
	back := page.Find("#main .wizard-head a.wizard-close")
	if back.Length() != 1 {
		t.Fatalf("%s: found %d ‹ in the wizard head, want 1", what, back.Length())
	}
	if got := back.AttrOr("href", ""); got != items {
		t.Errorf("%s: ‹ goes to %q, want %q", what, got, items)
	}
	if got := back.AttrOr("aria-label", ""); got != "Back to "+label {
		t.Errorf("%s: ‹ aria-label = %q, want %q", what, got, "Back to "+label)
	}
	if back.Find("svg.icon").Length() != 1 || norm(back.Text()) != "" {
		t.Errorf("%s: ‹ holds %q and %d icons, want only the icon", what, norm(back.Text()), back.Find("svg.icon").Length())
	}
	if _, ok := back.Attr("data-open-dialog"); ok {
		t.Errorf("%s: ‹ opens a dialog, want a plain link (the line form saves nothing until Save line)", what)
	}
	if got := norm(page.Find("#main .wizard-head .wizard-head__meta").Text()); got != kind {
		t.Errorf("%s: meta line = %q, want %q", what, got, kind)
	}
	h1 := page.Find("#main h1")
	if h1.Length() != 1 || h1.Closest(".wizard-head").Length() != 1 {
		t.Fatalf("%s: %d h1, in the wizard head %v; want one, in the head", what, h1.Length(), h1.Closest(".wizard-head").Length() == 1)
	}
	if got := norm(h1.Text()); got != title {
		t.Errorf("%s: h1 = %q, want %q", what, got, title)
	}
	if h1.AttrOr("id", "") != "page-title" || h1.AttrOr("tabindex", "") != "-1" {
		t.Errorf("%s: h1 id %q tabindex %q, want page-title and -1", what, h1.AttrOr("id", ""), h1.AttrOr("tabindex", ""))
	}
	if n := page.Find("a.back-link").Length(); n != 0 {
		t.Errorf("%s: %d back-links beside the ‹, want none", what, n)
	}
}

// CD-387 (6), UI v2 §5.6: every line page, generic or the module's Line, is
// in focus mode with the wizard head's ‹ back to Items and the sticky
// footer holding Save line, on the GET (full page and htmx) and the 422
// redraw; the remove page gets the same head.
func TestR7b_LinePageFocusAndFooter(t *testing.T) {
	items := doc42 + "/edit/items"
	t.Run("generic GET", func(t *testing.T) {
		for _, c := range []struct{ path, title string }{{"/lines/new", "Add an item"}, {"/lines/2", "Line 2"}} {
			for _, hx := range []bool{false, true} {
				e := newLinesEnv(t, threeLines())
				what := "GET " + c.path + " htmx " + m22Bool(hx)
				page := uitest.Doc(t, e.get(t, doc42+c.path, m20Hdr(hx)...))
				r7bWantHead(t, page, "Items", items, "Sample order", c.title, what)
				m22WantBar(t, page, items, what)
			}
		}
	})
	t.Run("generic 422", func(t *testing.T) {
		e := newLinesEnv(t, threeLines())
		rec := e.post(t, doc42+"/lines/2", url.Values{"version": {"6"}, "item_id": {"4"}, "qty": {"abc"}}, "HX-Request", "true", "HX-Target", "main")
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body %.300s", rec.Code, rec.Body)
		}
		page := uitest.Doc(t, rec)
		r7bWantHead(t, page, "Items", items, "Sample order", "Line 2", "422")
		m22WantBar(t, page, items, "422")
	})
	t.Run("remove page", func(t *testing.T) {
		e := newLinesEnv(t, threeLines())
		page := uitest.Doc(t, e.get(t, doc42+"/lines/2/remove"))
		r7bWantHead(t, page, "Items", items, "Sample order", "Remove line 2", "remove")
	})
	t.Run("module Line", func(t *testing.T) {
		e, id := m20Env(t, 2, docscreen.Module{Hide: m01bHide(), LineNoun: m20Noun, Line: (&m03Hooks{}).line()})
		base := m01bURL(id)
		page := m01bOK(t, e.get(t, base+"/lines/1"), "GET module line")
		r7bWantHead(t, page, "Items", base+"/edit/items", "Hooked order", m20Noun.Title+" 1", "module line")
	})
}
