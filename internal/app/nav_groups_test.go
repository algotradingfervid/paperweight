package app_test

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/icons"
)

// navGroups is the sidebar on b's /approvals: each heading with its links,
// in order. Links under no heading are under "".
func navGroups(b *browser) (heads []string, links map[string][]string) {
	links = map[string][]string{}
	side := b.get("/approvals").doc.Find("nav.app-sidebar")
	side.Find("h2").Each(func(_ int, h *goquery.Selection) {
		name := strings.TrimSpace(h.Text())
		heads = append(heads, name)
		side.Find(`ul[aria-labelledby="` + h.AttrOr("id", "") + `"]`).Find("a").Each(func(_ int, a *goquery.Selection) {
			links[name] = append(links[name], a.AttrOr("href", ""))
		})
	})
	return heads, links
}

// testOnlyHref reports whether href is a TestOnly DocType's list, which has
// no place in the real sidebar.
func testOnlyHref(href string) bool {
	for _, dt := range doctype.All() {
		if dt.TestOnly && href == app.ListURL(dt.Name) {
			return true
		}
	}
	return false
}

// DL §4.7, UI v2 N18: every DocType the sidebar can list (a document, or a
// master with screens) has an icon from the registry.
func TestNav_EveryDocTypeHasIcon(t *testing.T) {
	w := newWorld(t)
	known := icons.Names()
	checked := map[bool]int{}
	for _, dt := range doctype.All() {
		if dt.TestOnly || (dt.IsMaster() && !w.app.HasScreen(app.ListURL(dt.Name))) {
			continue
		}
		checked[dt.IsMaster()]++
		icon, ok := app.NavIcon(dt.Name)
		if !ok {
			t.Errorf("DocType %q has no nav icon: add it to docNav in routes.go", dt.Name)
			continue
		}
		if !slices.Contains(known, icon) {
			t.Errorf("DocType %q: icon %q isn't in icons.txt", dt.Name, icon)
		}
	}
	// The skip above must not hide the real ones: 11 masters with screens and
	// 5 documents today.
	if checked[true] < 11 || checked[false] < 5 {
		t.Errorf("checked %d masters and %d documents, want at least 11 and 5", checked[true], checked[false])
	}
}

// A-2 (R2), UI v2 §5.2: the Administrator's sidebar has the six groups in
// order with each item in its group, the rarely used masters under More
// masters, and no icon twice.
func TestNav_AdminGroups(t *testing.T) {
	w := newWorld(t)
	w.canonical()
	admin := w.signedIn(adminEmail)
	heads, links := navGroups(admin)
	heads = slices.DeleteFunc(heads, func(h string) bool { return h == "Documents" })
	if want := []string{"Work", "Purchase", "Goods receipt", "Expenses", "Masters", "Admin"}; !reflect.DeepEqual(heads, want) {
		t.Fatalf("headings = %q, want %q (plus Documents for test-only DocTypes)", heads, want)
	}
	for _, href := range links["Documents"] {
		if !testOnlyHref(href) {
			t.Errorf("%s is under Documents, which only test-only DocTypes fall back to", href)
		}
	}
	want := map[string][]string{
		"Work":          {"/approvals", "/approvals/mine"},
		"Purchase":      {"/modules/purchase", "/doc/purchase.Order", "/doc/purchase.Request"},
		"Goods receipt": {"/doc/goodsreceipt.Receipt"},
		"Expenses":      {"/doc/expense.Claim", "/doc/expense.Advance"},
		"Masters": {"/m/party", "/m/item", "/m/project", "/m/site", "/m/employee",
			"/m/branch", "/m/company", "/m/expense-type", "/m/payment-terms", "/m/tax-code", "/m/uom"},
		"Admin": {"/users", "/roles", "/admin/approval-rules", "/settings/numbering", "/settings/business"},
	}
	for _, g := range heads {
		if !reflect.DeepEqual(links[g], want[g]) {
			t.Errorf("group %q = %q, want %q", g, links[g], want[g])
		}
	}

	doc := admin.get("/approvals").doc
	more := doc.Find("nav.app-sidebar details")
	if got := strings.TrimSpace(more.Find("summary").Text()); got != "More masters" {
		t.Errorf("disclosure = %q, want More masters", got)
	}
	if got := hrefs(more); !reflect.DeepEqual(got, want["Masters"][5:]) {
		t.Errorf("More masters = %q, want %q", got, want["Masters"][5:])
	}

	seen := map[string]string{}
	doc.Find("nav.app-sidebar a").Each(func(_ int, a *goquery.Selection) {
		href := a.AttrOr("href", "")
		if testOnlyHref(href) {
			return
		}
		svg, _ := goquery.OuterHtml(a.Find("svg").First())
		if prev, ok := seen[svg]; ok || svg == "" {
			t.Errorf("%s shares its icon with %q (or has none)", href, prev)
		}
		seen[svg] = href
	})
}

// Rights still decide the items, and a group with no item has no heading: a
// user with no role sees Work alone; one who may read only purchase orders
// sees Work and Purchase, with no requests.
func TestNav_GroupsFollowRights(t *testing.T) {
	w := newWorld(t)
	admin := w.seedUsers()
	ctx := context.Background()
	a := w.app.Auth()
	if err := a.CreateRole(ctx, admin, "Order Reader"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetRights(ctx, admin, "Order Reader", purchase.DocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}); err != nil {
		t.Fatal(err)
	}
	for _, u := range []auth.NewUser{
		{Email: "orders@vardhan.test", Name: "Olga Reader", Password: testPassword, Roles: []string{"Order Reader"}},
		{Email: nobodyEmail, Name: "Nina Bose", Password: testPassword, Roles: []string{}},
	} {
		if _, _, err := a.CreateUser(ctx, admin, u); err != nil {
			t.Fatal(err)
		}
	}

	heads, _ := navGroups(w.signedIn(nobodyEmail))
	if !reflect.DeepEqual(heads, []string{"Work"}) {
		t.Errorf("no-role user's headings = %q, want [Work]", heads)
	}
	heads, links := navGroups(w.signedIn("orders@vardhan.test"))
	if !reflect.DeepEqual(heads, []string{"Work", "Purchase"}) {
		t.Errorf("order reader's headings = %q, want [Work Purchase]", heads)
	}
	if want := []string{"/modules/purchase", "/doc/purchase.Order"}; !reflect.DeepEqual(links["Purchase"], want) {
		t.Errorf("order reader's Purchase = %q, want %q", links["Purchase"], want)
	}
}

// hrefs lists the hrefs of the links in sel.
func hrefs(sel *goquery.Selection) []string {
	var out []string
	sel.Find("a").Each(func(_ int, a *goquery.Selection) { out = append(out, a.AttrOr("href", "")) })
	return out
}
