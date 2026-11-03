package layout_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// groupedShell has items in all six groups of UI v2 §5.2, two "More" masters
// and one item with no group.
func groupedShell(path string) ui.Shell {
	s := shell()
	s.Path = path
	s.Nav = []ui.NavItem{
		{Label: "Gallery", Href: "/_gallery", Icon: "image"},
		{Label: "Home", Href: "/", Icon: "house", Group: "Work"},
		{Label: "Approvals", Href: "/approvals", Icon: "inbox", Group: "Work"},
		{Label: "My requests", Href: "/approvals/mine", Icon: "file-clock", Group: "Work"},
		{Label: "Purchase home", Href: "/modules/purchase", Icon: "layout-dashboard", Group: "Purchase"},
		{Label: "Purchase Orders", Href: "/doc/purchase.Order", Icon: "shopping-cart", Group: "Purchase"},
		{Label: "Goods Receipts", Href: "/doc/goodsreceipt.Receipt", Icon: "truck", Group: "Goods receipt"},
		{Label: "Expense Claims", Href: "/doc/expense.Claim", Icon: "receipt-indian-rupee", Group: "Expenses"},
		{Label: "Parties", Href: "/m/party", Icon: "handshake", Group: "Masters"},
		{Label: "Branches", Href: "/m/branch", Icon: "store", Group: "Masters", More: true},
		{Label: "UoMs", Href: "/m/uom", Icon: "ruler", Group: "Masters", More: true},
		{Label: "Users", Href: "/users", Icon: "users", Group: "Admin"},
	}
	return s
}

// hrefs lists the hrefs of the links in sel.
func hrefs(sel *goquery.Selection) []string {
	var out []string
	sel.Find("a").Each(func(_ int, a *goquery.Selection) { out = append(out, a.AttrOr("href", "")) })
	return out
}

// A-2 (R2): the sidebar renders the groups in the shell's order, each under an
// <h2> that labels its list, and no two items share an icon.
func TestNav_GroupsInOrder(t *testing.T) {
	doc := render(t, groupedShell("/doc/purchase.Order/7"), layout.PageOpts{Title: "PO"})
	side := doc.Find("nav.app-sidebar")
	var heads []string
	side.Find("h2").Each(func(_ int, h *goquery.Selection) { heads = append(heads, strings.TrimSpace(h.Text())) })
	if want := []string{"Work", "Purchase", "Goods receipt", "Expenses", "Masters", "Admin"}; !reflect.DeepEqual(heads, want) {
		t.Fatalf("sidebar headings = %q, want %q", heads, want)
	}
	want := map[string][]string{
		"Work":          {"/", "/approvals", "/approvals/mine"},
		"Purchase":      {"/modules/purchase", "/doc/purchase.Order"},
		"Goods receipt": {"/doc/goodsreceipt.Receipt"},
		"Expenses":      {"/doc/expense.Claim"},
		"Masters":       {"/m/party", "/m/branch", "/m/uom"},
		"Admin":         {"/users"},
	}
	ids := map[string]bool{}
	side.Find("h2").Each(func(_ int, h *goquery.Selection) {
		id := h.AttrOr("id", "")
		if id == "" || ids[id] {
			t.Errorf("heading %q has id %q (empty or repeated)", h.Text(), id)
		}
		ids[id] = true
		list := side.Find(`ul[aria-labelledby="` + id + `"]`)
		if list.Length() != 1 {
			t.Errorf("heading %q labels %d lists, want 1", h.Text(), list.Length())
			return
		}
		name := strings.TrimSpace(h.Text())
		if got := hrefs(list); !reflect.DeepEqual(got, want[name]) {
			t.Errorf("group %q links = %q, want %q", name, got, want[name])
		}
	})
	// The item with no group comes first, under no heading.
	if got := hrefs(side); len(got) == 0 || got[0] != "/_gallery" {
		t.Errorf("sidebar links start %q, want the ungrouped /_gallery first", got)
	}
	if _, ok := side.Find("ul").First().Attr("aria-labelledby"); ok {
		t.Error("the ungrouped list is labelled by a heading that doesn't exist")
	}
	icons := map[string]string{}
	side.Find("a").Each(func(_ int, a *goquery.Selection) {
		svg, _ := goquery.OuterHtml(a.Find("svg").First())
		if svg == "" {
			t.Errorf("%s has no icon", a.AttrOr("href", ""))
			return
		}
		if prev, ok := icons[svg]; ok {
			t.Errorf("%s and %s share an icon", prev, a.AttrOr("href", ""))
		}
		icons[svg] = a.AttrOr("href", "")
	})
	cur := side.Find(`a[aria-current="page"]`)
	if cur.Length() != 1 || cur.AttrOr("href", "") != "/doc/purchase.Order" {
		t.Errorf("current items = %q, want only /doc/purchase.Order", hrefs(cur.Parent()))
	}
}

// Empty groups aren't rendered: a shell with only Work items has one heading.
func TestNav_NoEmptyGroups(t *testing.T) {
	s := groupedShell("/approvals")
	s.Nav = s.Nav[1:3]
	side := render(t, s, layout.PageOpts{Title: "Approvals"}).Find("nav.app-sidebar")
	if n := side.Find("h2").Length(); n != 1 {
		t.Errorf("%d headings, want 1 (Work)", n)
	}
	if side.Find("details").Length() != 0 {
		t.Error("a More disclosure without More items")
	}
}

// §5.2: the More items sit in a "More masters" disclosure after the group's
// other items (the list's last item), open only when the current page is one of them.
func TestNav_MoreMasters(t *testing.T) {
	for path, open := range map[string]bool{"/m/uom/3": true, "/m/party": false, "/approvals": false} {
		side := render(t, groupedShell(path), layout.PageOpts{Title: "x"}).Find("nav.app-sidebar")
		d := side.Find("details")
		if d.Length() != 1 {
			t.Fatalf("%s: %d disclosures, want 1", path, d.Length())
		}
		if got := strings.TrimSpace(d.Find("summary").Text()); got != "More masters" {
			t.Errorf("%s: summary = %q", path, got)
		}
		if got := hrefs(d); !reflect.DeepEqual(got, []string{"/m/branch", "/m/uom"}) {
			t.Errorf("%s: disclosure links = %q", path, got)
		}
		li := d.Parent()
		if !li.Is("li:last-child") || hrefs(li.Prev())[0] != "/m/party" {
			t.Errorf("%s: the disclosure isn't the Masters list's last item, after /m/party", path)
		}
		if _, isOpen := d.Attr("open"); isOpen != open {
			t.Errorf("%s: open = %v, want %v", path, isOpen, open)
		}
	}
}

// The phone drawer has the same groups, with ids that don't repeat the
// sidebar's.
func TestNav_SheetGroups(t *testing.T) {
	doc := render(t, groupedShell("/approvals"), layout.PageOpts{Title: "Approvals"})
	sheet := doc.Find("#nav-sheet nav")
	if n := sheet.Find("h2").Length(); n != 6 {
		t.Errorf("drawer has %d headings, want 6", n)
	}
	if got, want := hrefs(sheet), hrefs(doc.Find("nav.app-sidebar")); !reflect.DeepEqual(got, want) {
		t.Errorf("drawer links = %q, want the sidebar's %q", got, want)
	}
	seen := map[string]bool{}
	doc.Find("[id]").Each(func(_ int, e *goquery.Selection) {
		id := e.AttrOr("id", "")
		if seen[id] {
			t.Errorf("id %q appears twice", id)
		}
		seen[id] = true
	})
}

// §5.2: Approvals shows the inbox count, with words for screen readers.
func TestNav_ApprovalsCount(t *testing.T) {
	s := groupedShell("/m/party")
	a := render(t, s, layout.PageOpts{Title: "x"}).Find(`nav.app-sidebar a[href="/approvals"]`)
	if got := strings.Join(strings.Fields(a.Find(".nav-badge").Text()), " "); got != "3 waiting" {
		t.Errorf("Approvals badge = %q, want \"3 waiting\"", got)
	}
	if got := strings.TrimSpace(a.Find(".nav-badge .visually-hidden").Text()); got != "waiting" {
		t.Errorf("hidden badge words = %q", got)
	}
	if n := render(t, s, layout.PageOpts{Title: "x"}).Find(`nav.app-sidebar .nav-badge`).Length(); n != 1 {
		t.Errorf("%d badges, want only the Approvals one", n)
	}
	s.InboxCount = 0
	if n := render(t, s, layout.PageOpts{Title: "x"}).Find(`nav .nav-badge`).Length(); n != 0 {
		t.Errorf("%d badges for count 0", n)
	}
}

// H4: Home is current only on `/`, though every path starts with it; a page
// with no nav item has no current item.
func TestNav_HomeCurrentOnlyOnItself(t *testing.T) {
	for path, want := range map[string]string{"/": "/", "/approvals": "/approvals", "/search": "", "/doc/purchase.Order/7": "/doc/purchase.Order"} {
		got := render(t, groupedShell(path), layout.PageOpts{Title: "x"}).Find(`nav.app-sidebar a[aria-current="page"]`)
		if want == "" && got.Length() != 0 {
			t.Errorf("%s: current = %q, want none", path, got.AttrOr("href", ""))
		}
		if want != "" && got.AttrOr("href", "") != want {
			t.Errorf("%s: current = %q, want %q", path, got.AttrOr("href", ""), want)
		}
	}
}

// M10: the drawer focuses the current item (DL §5.1), or Close with none, the sidebar's copy never
// autofocuses a link on load, and the drawer has the brand row and the user.
func TestNavSheet_FocusBrandAndUser(t *testing.T) {
	for path, want := range map[string]string{"/approvals": "/approvals", "/m/uom": "/m/uom", "/search": ""} {
		doc := render(t, groupedShell(path), layout.PageOpts{Title: "x"})
		got := doc.Find(`#nav-sheet [data-dialog-focus]`)
		if want == "" && (got.Length() != 1 || got.AttrOr("aria-label", "") != "Close menu") {
			t.Errorf("%s: drawer focus on %d elements, want Close menu", path, got.Length())
		}
		if want != "" && (got.Length() != 1 || got.AttrOr("href", "") != want) {
			t.Errorf("%s: drawer focus on %q (%d), want %q", path, got.AttrOr("href", ""), got.Length(), want)
		}
		if n := doc.Find(`nav.app-sidebar [data-dialog-focus], [autofocus]`).Length(); n != 0 {
			t.Errorf("%s: %d focus marks in the sidebar or autofocus anywhere", path, n)
		}
	}
	doc := render(t, groupedShell("/approvals"), layout.PageOpts{Title: "x"})
	if doc.Find(`#nav-sheet .nav-sheet-head a.brand[href="/"]`).Length() != 1 {
		t.Error("the drawer has no brand row")
	}
	if got := strings.TrimSpace(doc.Find(`#nav-sheet .nav-sheet-user`).Text()); got != shell().User {
		t.Errorf("drawer user = %q, want %q", got, shell().User)
	}
}
