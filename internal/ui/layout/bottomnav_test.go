package layout_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// newShell is shell() with two things to create.
func newShell(path string) ui.Shell {
	s := shell()
	s.Path = path
	s.New = []ui.NavItem{
		{Label: "Purchase order", Href: "/doc/purchase.Order/new", Icon: "shopping-cart"},
		{Label: "Party", Href: "/m/party/new", Icon: "handshake"},
	}
	return s
}

// texts is each element's whitespace-collapsed text.
func texts(sel *goquery.Selection) []string {
	var out []string
	sel.Each(func(_ int, e *goquery.Selection) { out = append(out, strings.Join(strings.Fields(e.Text()), " ")) })
	return out
}

// A-3, UI v2 §5.2 (N1): five fixed links, the Approvals one named with its
// count, New opening the "Create new" sheet, and Me the account sheet.
func TestBottomNav_FiveItems(t *testing.T) {
	nav := render(t, newShell("/doc/purchase.Order"), layout.PageOpts{Title: "Purchase orders"}).Find("nav.bottomnav")
	if nav.Length() != 1 {
		t.Fatalf("%d bottom navs, want 1", nav.Length())
	}
	if got := nav.AttrOr("aria-label", ""); got != "Quick" {
		t.Errorf("aria-label = %q, want Quick", got)
	}
	links := nav.Find("a")
	if got, want := hrefs(nav), []string{"/", "/approvals", "/new", "/search", "/account/sessions"}; !reflect.DeepEqual(got, want) {
		t.Errorf("hrefs = %q, want %q", got, want)
	}
	if got, want := texts(links), []string{"Home", "3 Approvals, 3 waiting", "New", "Search", "Me"}; !reflect.DeepEqual(got, want) {
		t.Errorf("texts = %q, want %q", got, want)
	}
	if got := links.Eq(1).Find(`[aria-hidden="true"]`).Text(); got != "3" {
		t.Errorf("visible count = %q, want 3 (hidden from screen readers)", got)
	}
	if got := links.Eq(1).Find(".visually-hidden").Text(); got != ", 3 waiting" {
		t.Errorf("Approvals' hidden text = %q", got)
	}
	for i, id := range map[int]string{2: "new-sheet", 4: "me-sheet"} {
		a := links.Eq(i)
		if a.AttrOr("data-open-dialog", "") != id || a.AttrOr("aria-haspopup", "") != "dialog" {
			t.Errorf("link %d opens %q (haspopup %q), want %s", i, a.AttrOr("data-open-dialog", ""), a.AttrOr("aria-haspopup", ""), id)
		}
	}
	if links.Find("svg.icon").Length() != 5 {
		t.Errorf("%d icons, want one per item", links.Find("svg.icon").Length())
	}

	// No inbox count: no badge and no hidden text.
	s := newShell("/")
	s.InboxCount = 0
	a := render(t, s, layout.PageOpts{Title: "Home"}).Find("nav.bottomnav a").Eq(1)
	if got := strings.TrimSpace(a.Text()); got != "Approvals" {
		t.Errorf("no count: text = %q, want Approvals", got)
	}
}

// §5.2: the current item carries aria-current="page", and only it.
func TestBottomNav_CurrentItem(t *testing.T) {
	for path, want := range map[string]string{
		"/":                   "Home",
		"/approvals":          "Approvals",
		"/approvals/12":       "Approvals",
		"/new":                "New",
		"/search":             "Search",
		"/account/password":   "Me",
		"/account/sessions":   "Me",
		"/doc/purchase.Order": "",
		"/newer":              "",
		"/searching":          "",
	} {
		cur := render(t, newShell(path), layout.PageOpts{Title: "T"}).Find(`nav.bottomnav a[aria-current="page"]`)
		if got := strings.Join(texts(cur), "|"); !strings.Contains(got, want) || (want == "") != (cur.Length() == 0) || cur.Length() > 1 {
			t.Errorf("%s: current = %q, want %q", path, got, want)
		}
	}
}

// A-3, N12: the bottom nav is on signed-in app pages only; Focus (wizard,
// line and document pages), Bare and guests have none.
func TestBottomNav_HiddenInFocusBareAndForGuests(t *testing.T) {
	guest := newShell("/x")
	guest.SignedIn = false
	for name, c := range map[string]struct {
		s ui.Shell
		o layout.PageOpts
	}{
		"focus": {newShell("/x"), layout.PageOpts{Title: "Step", Focus: true}},
		"bare":  {newShell("/x"), layout.PageOpts{Title: "Sign in", Bare: true}},
		"guest": {guest, layout.PageOpts{Title: "Not found"}},
	} {
		doc := render(t, c.s, c.o)
		if n := doc.Find("nav.bottomnav").Length(); n != 0 {
			t.Errorf("%s: %d bottom navs, want none", name, n)
		}
		if doc.Find("body").HasClass("with-bottomnav") {
			t.Errorf("%s: body keeps room for a bottom nav it doesn't have", name)
		}
	}
	if !render(t, newShell("/x"), layout.PageOpts{Title: "List"}).Find("body").HasClass("with-bottomnav") {
		t.Error("body has no room for the bottom nav")
	}
}

// N2: the "Create new" sheet lists Shell.New as tiles, focuses the first one
// on open, and is on every signed-in page, focus pages included (the header's
// New opens it there).
func TestNewSheet_ListsShellNew(t *testing.T) {
	for _, o := range []layout.PageOpts{{Title: "List"}, {Title: "Step", Focus: true}} {
		dlg := render(t, newShell("/x"), o).Find("dialog#new-sheet")
		if dlg.Length() != 1 {
			t.Fatalf("focus=%v: %d new sheets, want 1", o.Focus, dlg.Length())
		}
		if got := strings.TrimSpace(dlg.Find("h2").Text()); got != "Create new" {
			t.Errorf("heading = %q", got)
		}
		tiles := dlg.Find("a.newtile")
		if got, want := texts(tiles), []string{"Purchase order", "Party"}; !reflect.DeepEqual(got, want) {
			t.Errorf("tiles = %q, want %q", got, want)
		}
		if got, want := hrefs(dlg.Find(".newtiles")), []string{"/doc/purchase.Order/new", "/m/party/new"}; !reflect.DeepEqual(got, want) {
			t.Errorf("tile hrefs = %q, want %q", got, want)
		}
		if _, ok := tiles.First().Attr("autofocus"); !ok {
			t.Error("the first tile isn't focused on open")
		}
		if _, ok := tiles.Last().Attr("autofocus"); ok {
			t.Error("the last tile has autofocus too")
		}
		if got := strings.TrimSpace(dlg.Find("p").Text()); got != "Only what your role can create is listed." {
			t.Errorf("line = %q", got)
		}
	}
	guest := newShell("/x")
	guest.SignedIn = false
	if render(t, guest, layout.PageOpts{Title: "T"}).Find("dialog#new-sheet").Length() != 0 {
		t.Error("a guest's page has the new sheet")
	}
}

// §5.2: the header's "+ New" (desktop) opens the sheet, and goes to /new
// without JavaScript.
func TestHeader_NewButton(t *testing.T) {
	a := render(t, newShell("/x"), layout.PageOpts{Title: "T"}).Find(`header .header-new a`)
	if a.Length() != 1 {
		t.Fatalf("%d header New buttons, want 1", a.Length())
	}
	if a.AttrOr("href", "") != "/new" || a.AttrOr("data-open-dialog", "") != "new-sheet" || a.AttrOr("aria-haspopup", "") != "dialog" {
		t.Errorf("New = href %q opens %q", a.AttrOr("href", ""), a.AttrOr("data-open-dialog", ""))
	}
	if got := strings.TrimSpace(a.Text()); got != "New" {
		t.Errorf("New text = %q", got)
	}
	if render(t, newShell("/x"), layout.PageOpts{Title: "T", Bare: true}).Find("header .header-new a").Length() != 0 {
		t.Error("a bare page has the New button")
	}
}

// §5.2: Me opens the account sheet: the user's name, My requests, My
// sessions, Change password and Sign out (a POST with the CSRF token).
func TestMeSheet_Contents(t *testing.T) {
	dlg := render(t, newShell("/x"), layout.PageOpts{Title: "T"}).Find("dialog#me-sheet")
	if dlg.Length() != 1 {
		t.Fatalf("%d me sheets, want 1", dlg.Length())
	}
	if got := strings.TrimSpace(dlg.Find("h2").Text()); got != "Priya Shah" {
		t.Errorf("heading = %q", got)
	}
	if got, want := hrefs(dlg.Find("ul")), []string{"/approvals/mine", "/account/sessions", "/account/password"}; !reflect.DeepEqual(got, want) {
		t.Errorf("links = %q, want %q", got, want)
	}
	if got, want := texts(dlg.Find("ul a")), []string{"My requests", "My sessions", "Change password"}; !reflect.DeepEqual(got, want) {
		t.Errorf("link texts = %q, want %q", got, want)
	}
	form := dlg.Find(`form[method="post"][action="/signout"]`)
	if form.Find(`input[name="csrf_token"]`).AttrOr("value", "") != `tok"x` || strings.TrimSpace(form.Find("button").Text()) != "Sign out" {
		t.Error("no Sign out form with the CSRF token")
	}
}
