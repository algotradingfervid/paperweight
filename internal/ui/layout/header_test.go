package layout_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// textNodes counts the text nodes under sel whose trimmed text is want.
func textNodes(sel *goquery.Selection, want string) int {
	n := 0
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode && strings.TrimSpace(node.Data) == want {
			n++
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, node := range sel.Nodes {
		walk(node)
	}
	return n
}

// A-1 (R1): the header names the app once, on every page shape, and the logo
// beside the name is a mark that doesn't draw the name a second time.
func TestHeader_BrandOnce(t *testing.T) {
	s := shell()
	s.AppName = brand.Load().AppName
	for _, o := range []layout.PageOpts{{Title: "Purchase orders"}, {Title: "Sign in", Bare: true}} {
		doc := render(t, s, o)
		if n := textNodes(doc.Find("header"), s.AppName); n != 1 {
			t.Errorf("bare=%v: %d text nodes %q in <header>, want 1", o.Bare, n, s.AppName)
		}
		if alt := doc.Find("header img.brand-logo").AttrOr("alt", "x"); alt != "" {
			t.Errorf("bare=%v: logo alt = %q, want empty (the name is written beside it)", o.Bare, alt)
		}
	}

	rec := httptest.NewRecorder()
	brand.Static().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, brand.AppLogoURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", brand.AppLogoURL, rec.Code)
	}
	logo, _ := io.ReadAll(rec.Body)
	if strings.Contains(string(logo), s.AppName) || strings.Contains(string(logo), "<text") {
		t.Errorf("the header logo draws text (%q): the name would show twice", s.AppName)
	}
}

// §5.2: the desktop search box has a magnifier, the placeholder and the "/" hint.
func TestHeader_SearchLook(t *testing.T) {
	form := render(t, shell(), layout.PageOpts{Title: "PO"}).Find(headerSearchForm)
	if form.Find("svg.icon").Length() != 1 {
		t.Error("search box has no magnifier icon")
	}
	if got := form.Find(`input[name="q"]`).AttrOr("placeholder", ""); got != "Search orders, parties, items…" {
		t.Errorf("placeholder = %q", got)
	}
	kbd := form.Find("kbd")
	if strings.TrimSpace(kbd.Text()) != "/" || kbd.AttrOr("aria-hidden", "") != "true" {
		t.Errorf("key hint = %q aria-hidden=%q, want a hidden \"/\"", kbd.Text(), kbd.AttrOr("aria-hidden", ""))
	}
}

// §5.2: the account chip shows the user's initials and name and keeps its
// menu (sign-out is tested by TestShell_SignOutPosts).
func TestHeader_AccountChip(t *testing.T) {
	cases := map[string]string{"Priya Shah": "PS", "deepak": "D", "Ravi Kumar Kulkarni": "RK", "  ": ""}
	for user, want := range cases {
		s := shell()
		s.User = user
		sum := render(t, s, layout.PageOpts{Title: "PO"}).Find(".user-menu summary")
		av := sum.Find(".avatar-initials")
		if got := av.AttrOr("data-initials", "-"); got != want || strings.TrimSpace(av.Text()) != "" {
			t.Errorf("%q: initials = %q, want %q", user, got, want)
		}
		if av.AttrOr("aria-hidden", "") != "true" {
			t.Errorf("%q: initials are not hidden from screen readers", user)
		}
		if got := strings.TrimSpace(sum.Find(".user-name").Text()); got != strings.TrimSpace(user) {
			t.Errorf("%q: chip name = %q", user, got)
		}
	}
}
