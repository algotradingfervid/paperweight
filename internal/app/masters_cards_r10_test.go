package app_test

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// masterForms walks the real router: every master's new page, a master with
// no New being reached through its picker's record instead, and the masters
// the app mounts a picker for.
func masterForms(t *testing.T, w *world) (forms, pickers []string) {
	t.Helper()
	newPage := regexp.MustCompile(`^/m/[a-z-]+/new$`)
	err := chi.Walk(w.routes(), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet {
			return nil
		}
		if newPage.MatchString(route) {
			forms = append(forms, route)
		}
		if name, ok := strings.CutPrefix(route, "/pick/"); ok {
			pickers = append(pickers, name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	return forms, pickers
}

// wantCardedForm checks a form page: inputs only inside section cards, each
// card with its tile, heading and line, and an example in every text-like
// input a person types a value into.
func wantCardedForm(t *testing.T, url string, doc *goquery.Selection) {
	t.Helper()
	form := doc.Find("main form").First()
	cards := form.Find("section.card")
	if cards.Length() == 0 {
		t.Errorf("%s: the form has no section cards", url)
		return
	}
	cards.Each(func(_ int, c *goquery.Selection) {
		if c.Find(".card-icon svg").Length() != 1 || c.Find(".card-title").Length() == 0 || strings.TrimSpace(c.Find(".card-desc").Text()) == "" {
			t.Errorf("%s: card %q lacks its tile, heading or line", url, c.Find(".card-title").Text())
		}
	})
	form.Find("input:not([type=hidden]), select, textarea").Each(func(_ int, in *goquery.Selection) {
		id := in.AttrOr("id", "")
		if in.Closest("section.card").Length() == 0 {
			t.Errorf("%s: #%s is outside the cards", url, id)
		}
		if id == "f-code" || id == "f-name" || in.HasClass("code") {
			if p := in.AttrOr("placeholder", ""); !strings.HasPrefix(p, "e.g. ") {
				t.Errorf("%s: #%s example = %q", url, id, p)
			}
		}
	})
}

// UI v2 R10 (§5.5 items 1 and 5, shell H8): every master's form, read from the
// real router rather than a list here, is section cards with examples.
func TestMasterForms_CardsAndExamples(t *testing.T) {
	w := newWorld(t)
	w.canonical()
	b := w.signedIn(adminEmail)
	forms, pickers := masterForms(t, w)
	if len(forms) < 10 {
		t.Fatalf("%d master new pages: %q; the walk found too few", len(forms), forms)
	}
	for _, route := range forms {
		r := b.get(route)
		if r.status != http.StatusOK {
			t.Errorf("GET %s = %d", route, r.status)
			continue
		}
		wantCardedForm(t, route, r.doc.Selection)
	}
	const companyPage = "/m/company/1"
	r := b.get(companyPage)
	if r.status != http.StatusOK {
		t.Fatalf("GET %s = %d", companyPage, r.status)
	}
	wantCardedForm(t, companyPage, r.doc.Selection)

	// Every registered master with a picker was reached by one of the above:
	// a master can't join the app and miss this test.
	seen := len(forms) + 1 // the company's page
	var masters int
	for _, dt := range doctype.All() {
		if dt.IsMaster() && slices.Contains(pickers, dt.Name) {
			masters++
		}
	}
	if seen != masters {
		t.Errorf("%d master forms read (%q + company) but %d masters have a picker: %q", seen, forms, masters, pickers)
	}
}

// Numbering is DataList links, whose row is the target (the browser test
// measures 44 px), the series in words (shell M12).
func TestNumbering_RowsAreRealLinks(t *testing.T) {
	w := newWorld(t)
	w.canonical()
	r := w.signedIn(adminEmail).get("/settings/numbering")
	if r.status != http.StatusOK {
		t.Fatalf("GET = %d", r.status)
	}
	if n := r.doc.Find(`main a.datalist-link[href^="/settings/numbering/"]`).Length(); n == 0 {
		t.Errorf("no list row links\nbody: %s", r.pageText())
	}
	if text := r.pageText(); strings.Contains(text, "{FY}") || !strings.Contains(text, "financial year") {
		t.Errorf("series isn't in words: %s", text)
	}
}
