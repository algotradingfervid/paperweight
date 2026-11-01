package app_test

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// newTiles is the tiles' labels and hrefs in sel.
func newTiles(sel *goquery.Selection) (labels, links []string) {
	sel.Find("a.newtile").Each(func(_ int, a *goquery.Selection) {
		labels = append(labels, strings.TrimSpace(a.Text()))
		links = append(links, a.AttrOr("href", ""))
	})
	return labels, links
}

// A-4, UI v2 §5.2 (N2): /new and the "Create new" sheet on every page list
// exactly the DocTypes the user may create, in nav order.
func TestNewPage_ListsWhatTheUserMayCreate(t *testing.T) {
	w := newWorld(t)
	w.canonical()
	for email, want := range map[string][]string{
		sureshEmail: {"Purchase request", "Goods receipt", "Expense claim", "Advance request"},
		adminEmail:  {"Purchase order", "Purchase request", "Goods receipt", "Expense claim", "Advance request", "Party"},
	} {
		b := w.signedIn(email)
		r := b.get("/new")
		if r.status != http.StatusOK {
			t.Fatalf("%s: GET /new = %d", email, r.status)
		}
		if got := strings.TrimSpace(r.doc.Find("main h1").Text()); got != "Create new" {
			t.Errorf("%s: h1 = %q", email, got)
		}
		labels, _ := newTiles(r.doc.Find("main"))
		if !reflect.DeepEqual(labels, want) {
			t.Errorf("%s: /new = %q, want %q", email, labels, want)
		}
		if !strings.Contains(r.doc.Find("main").Text(), "Only what your role can create is listed.") {
			t.Errorf("%s: /new has no line", email)
		}
		// The sheet on another page lists the same.
		labels, _ = newTiles(b.get("/approvals").doc.Find("dialog#new-sheet"))
		if !reflect.DeepEqual(labels, want) {
			t.Errorf("%s: sheet = %q, want %q", email, labels, want)
		}
	}
	_, links := newTiles(w.signedIn(adminEmail).get("/new").doc.Find("main"))
	want := []string{"/doc/purchase.Order/new", "/doc/purchase.Request/new", "/doc/goodsreceipt.Receipt/new",
		"/doc/expense.Claim/new", "/doc/expense.Advance/new", "/m/party/new"}
	if !reflect.DeepEqual(links, want) {
		t.Errorf("links = %q, want %q", links, want)
	}
}

// The create right decides, not read: a user who may read every PO but create
// only requests and parties sees those two; one with no role sees
// none, and a guest is sent to sign in.
func TestNewPage_CreateRightOnly(t *testing.T) {
	w := newWorld(t)
	admin := w.seedUsers()
	ctx := context.Background()
	a := w.app.Auth()
	if err := a.CreateRole(ctx, admin, "Requester"); err != nil {
		t.Fatal(err)
	}
	for dt, rights := range map[string]map[auth.Right]auth.Scope{
		purchase.DocType:        {auth.RightRead: auth.ScopeAll},
		purchase.RequestDocType: {auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll},
		party.DocType:           {auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll},
	} {
		if err := a.SetRights(ctx, admin, "Requester", dt, rights); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range []auth.NewUser{
		{Email: "req@vardhan.test", Name: "Rekha Rao", Password: testPassword, Roles: []string{"Requester"}},
		{Email: nobodyEmail, Name: "Nina Bose", Password: testPassword, Roles: []string{}},
	} {
		if _, _, err := a.CreateUser(ctx, admin, u); err != nil {
			t.Fatal(err)
		}
	}
	labels, _ := newTiles(w.signedIn("req@vardhan.test").get("/new").doc.Find("main"))
	if want := []string{"Purchase request", "Party"}; !reflect.DeepEqual(labels, want) {
		t.Errorf("requester's /new = %q, want %q", labels, want)
	}
	r := w.signedIn(nobodyEmail).get("/new")
	if labels, _ := newTiles(r.doc.Find("main")); r.status != http.StatusOK || len(labels) != 0 {
		t.Errorf("no-role user's /new = %d %q, want 200 and no tile", r.status, labels)
	}
	if r := w.browser().get("/new"); r.status != http.StatusSeeOther || !strings.HasPrefix(r.header.Get("Location"), "/signin") {
		t.Errorf("guest GET /new = %d %q, want a redirect to sign in", r.status, r.header.Get("Location"))
	}
}

// A-3, N12: the bottom nav is on list and home pages, never on the pages with
// their own action footer: wizard steps, line and reason pages, the document
// page and a master's form.
func TestBottomNav_FocusPages(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	priya := w.signedIn(priyaEmail)
	id := priya.newOrder(m, "PRJ-METRO", "1000.00")
	step := priya.get(orderURL(id)).location()
	sent := priya.newOrder(m, "PRJ-METRO", "1000.00")
	priya.addLine(m, sent)
	priya.act(sent, "submit", nil)
	admin := w.signedIn(adminEmail)
	for _, c := range []struct {
		b    *browser
		path string
		nav  bool
	}{
		{priya, "/approvals", true},
		{priya, "/modules/purchase", true},
		{priya, orderPath, true},
		{priya, "/search", true},
		{priya, "/new", true},
		{admin, "/m/party", true},
		{admin, "/m/party/1", true},
		{priya, orderPath + "/new", false},
		{priya, step, false},
		{priya, orderURL(sent), false},
		{priya, orderURL(id) + "/lines/new", false},
		{priya, orderURL(id) + "/delete", false},
		{admin, "/m/party/new", false},
		{admin, "/m/item/new", false},
	} {
		r := c.b.get(c.path)
		if r.status != http.StatusOK {
			t.Errorf("GET %s = %d", c.path, r.status)
			continue
		}
		if got := r.doc.Find("nav.bottomnav").Length() == 1; got != c.nav {
			t.Errorf("%s: bottom nav shown = %v, want %v", c.path, got, c.nav)
		}
	}
}
