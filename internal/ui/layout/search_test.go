package layout_test

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/layout"
)

// Header search form (design-system §6, CD-200 part 4). uibrowser checks that
// it shows only from 64rem.

const headerSearchForm = `header form[action="/search"]`

func TestHeaderSearch_SignedInPageHasGetFormToSearch(t *testing.T) {
	doc := render(t, shell(), layout.PageOpts{Title: "Purchase orders"})
	form := doc.Find(headerSearchForm)
	if form.Length() != 1 {
		t.Fatalf("%d header search forms, want 1", form.Length())
	}
	if got := strings.ToLower(form.AttrOr("method", "")); got != "get" {
		t.Errorf("method = %q, want get", got)
	}
	input := form.Find(`input[name="q"]`)
	if input.Length() != 1 {
		t.Fatalf("%d q inputs, want 1", input.Length())
	}
	if got := input.AttrOr("maxlength", ""); got != "100" {
		t.Errorf("maxlength = %q, want 100", got)
	}
	if got := form.Find("input").Length(); got != 1 {
		t.Errorf("%d inputs, want only q: a GET form must not put the CSRF token in the URL", got)
	}
	if id := input.AttrOr("id", ""); id == "filter-q" {
		t.Error("input id clashes with the list page's filter bar input")
	}
}

func TestHeaderSearch_InputIsLabelledSearch(t *testing.T) {
	form := render(t, shell(), layout.PageOpts{Title: "PO"}).Find(headerSearchForm)
	input := form.Find(`input[name="q"]`)
	label := form.Find("label")
	wired := label.Length() == 1 && (label.AttrOr("for", "") == input.AttrOr("id", "-") || label.Find("input").Length() == 1)
	switch {
	case wired && strings.TrimSpace(label.Text()) == "Search":
	case input.AttrOr("aria-label", "") == "Search":
	default:
		t.Errorf("input has no accessible name Search: label %q, aria-label %q", label.Text(), input.AttrOr("aria-label", ""))
	}
}

// The form works without JavaScript: nothing on it needs htmx or Alpine.
func TestHeaderSearch_FormNeedsNoJavaScript(t *testing.T) {
	form := render(t, shell(), layout.PageOpts{Title: "PO"}).Find(headerSearchForm)
	if form.Length() != 1 {
		t.Fatalf("%d header search forms, want 1", form.Length())
	}
	form.Find("*").AddSelection(form).Each(func(_ int, s *goquery.Selection) {
		for _, a := range s.Nodes[0].Attr {
			if strings.HasPrefix(a.Key, "hx-") || strings.HasPrefix(a.Key, "x-") {
				t.Errorf("<%s> has %s=%q", goquery.NodeName(s), a.Key, a.Val)
			}
		}
	})
}

func TestHeaderSearch_AbsentOnBarePages(t *testing.T) {
	if render(t, shell(), layout.PageOpts{Title: "Sign in"}).Find(headerSearchForm).Length() != 1 {
		t.Fatal("control: the same shell without Bare has no header search form")
	}
	doc := render(t, shell(), layout.PageOpts{Title: "Sign in", Bare: true})
	if doc.Find(`form[action="/search"]`).Length() != 0 {
		t.Error("bare page has the header search form")
	}
}

func TestHeaderSearch_AbsentWhenSignedOut(t *testing.T) {
	if render(t, shell(), layout.PageOpts{Title: "Not found"}).Find(headerSearchForm).Length() != 1 {
		t.Fatal("control: the same page signed in has no header search form")
	}
	s := shell()
	s.SignedIn = false
	doc := render(t, s, layout.PageOpts{Title: "Not found"})
	if doc.Find(`form[action="/search"]`).Length() != 0 {
		t.Error("signed-out page has the header search form")
	}
}
