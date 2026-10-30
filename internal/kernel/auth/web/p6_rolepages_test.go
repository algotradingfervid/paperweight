package authweb_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// Roles list → role page → DocType rights page: design-system §8.6, CD-200
// part 5, wp/polish plan "P6 detail". Never a role × DocType × right grid.
const (
	p6Stores    = "Stores"
	p6Full      = "p6.Full"
	p6FullLabel = "Full Doc"

	p6WhoCan      = "/rights/who-can"
	p6WhoCanLabel = "Who can…"
	p6AdminLine   = "Every right that applies. " + p1Fixed
	p6NoRights    = "No rights"
	p6ReadAdded   = "Read was added, because every right needs it."
	p6SaveButton  = "Save rights"
	p6ReadNeeded  = "Read is needed for any other right on Purchase Order. Tick Read or clear the other rights."
)

// p6Register adds a DocType where all 11 rights apply and Team is offered, as
// in the design's "8 of 11 rights". Call it before authtest.NewWorld.
func p6Register() {
	if _, ok := doctype.Lookup(p6Full); !ok {
		doctype.Register(doctype.DocType{
			Name: p6Full, Label: p6FullLabel, PluralLabel: "Full Docs",
			Submittable: true, Approvable: true, Importable: true, EmployeeField: "employee_id",
		})
	}
}

func p6World(t *testing.T) (*authtest.World, *webtest.Harness, *webtest.Client) {
	t.Helper()
	p6Register()
	w := authtest.NewWorld(t)
	h := webtest.New(t, w)
	return w, h, signedIn(t, h, t13Admin)
}

// p6Get is a page the client must be able to open.
func p6Get(t *testing.T, c *webtest.Client, path string) *goquery.Document {
	t.Helper()
	r := c.Get(path)
	wantStatus(t, "GET "+path, r, http.StatusOK)
	return p1Doc(t, "GET "+path, r)
}

func p6RolePath(t *testing.T, w *authtest.World, role string) string {
	t.Helper()
	return t17xRolePath(t, w, role, "")
}

func p6Roles(t *testing.T, w *authtest.World) []auth.RoleInfo {
	t.Helper()
	roles, _, err := w.Auth.RightsMatrix(context.Background(), w.P(t, t13Admin))
	if err != nil {
		t.Fatalf("RightsMatrix: %v", err)
	}
	return roles
}

// p6Card is the one card on a role page titled label.
func p6Card(t *testing.T, doc *goquery.Document, label string) *goquery.Selection {
	t.Helper()
	cards := doc.Find("main .card").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return p1Text(s.Find(".card-title")) == label
	})
	if cards.Length() != 1 {
		t.Fatalf("cards titled %q = %d, want 1; page: %s", label, cards.Length(), p1Text(doc.Find("main")))
	}
	return cards
}

// p6Summary is a card's text apart from its title and Change link.
func p6Summary(card *goquery.Selection) string {
	return p1Text(card.Find(".card-body").Children().Not(".card-head"))
}

// p6NoDuplicateIDs checks main's ids: each form row of a list needs its own.
func p6NoDuplicateIDs(t *testing.T, what string, doc *goquery.Document) {
	t.Helper()
	seen := map[string]bool{}
	doc.Find("main [id]").Each(func(_ int, s *goquery.Selection) {
		id := s.AttrOr("id", "")
		if seen[id] {
			t.Errorf("%s: id %q is used twice", what, id)
		}
		seen[id] = true
	})
}

// p6Form is what a browser posts from form: its hidden fields and the checked
// radios, overridden by set.
func p6Form(form *goquery.Selection, set url.Values) url.Values {
	out := url.Values{}
	form.Find(`input[type=hidden], input[type=radio][checked]`).Each(func(_ int, s *goquery.Selection) {
		out.Set(s.AttrOr("name", ""), s.AttrOr("value", ""))
	})
	for k, vs := range set {
		out[k] = vs
	}
	return out
}

// The role, DocType and right never meet on one screen: /roles has no rights
// at all, a role page only summarises, a rights page edits one role on one
// DocType.
func TestRightsPages_NeverAGrid(t *testing.T) {
	w, _, admin := p6World(t)
	rights := t17RightsPath(t, w, p6Stores, authtest.DTOrder)
	action := regexp.MustCompile(`^/roles/\d+/rights/`)
	rightForms := func(doc *goquery.Document) []string {
		var out []string
		doc.Find("form").Each(func(_ int, s *goquery.Selection) {
			if a := s.AttrOr("action", ""); action.MatchString(a) {
				out = append(out, a)
			}
		})
		return out
	}

	for _, path := range []string{
		"/roles", p6RolePath(t, w, p6Stores), p6RolePath(t, w, "Director"), p6RolePath(t, w, "Employee"),
	} {
		doc := p6Get(t, admin, path)
		if got := rightForms(doc); len(got) != 0 {
			t.Errorf("%s: rights forms %q, want none", path, got)
		}
		for _, sel := range []string{"table", "select", `input[type=radio]`, `form[action$="/reset"]`, ".segmented"} {
			if n := doc.Find("main " + sel).Length(); n != 0 {
				t.Errorf("%s: %d of %s, want none", path, n, sel)
			}
		}
	}

	doc := p6Get(t, admin, rights)
	if got := rightForms(doc); !slices.Equal(got, []string{rights}) {
		t.Errorf("rights page: rights forms %q, want only %q", got, rights)
	}
	for _, sel := range []string{"table", "select"} {
		if n := doc.Find("main " + sel).Length(); n != 0 {
			t.Errorf("rights page: %d of %s, want none", n, sel)
		}
	}
}

func TestRolesList_ShowsNoRights(t *testing.T) {
	_, _, admin := p6World(t)
	text := p1Text(p6Get(t, admin, "/roles").Find("main"))
	for _, no := range []string{"Purchase Order", "Expense Claim", "Reset", "every right that applies"} {
		if strings.Contains(text, no) {
			t.Errorf("/roles text %q shows %q", text, no)
		}
	}
}

func TestRolesList_EachRoleLinksToItsPage(t *testing.T) {
	w, _, admin := p6World(t)
	doc := p6Get(t, admin, "/roles")
	for _, role := range p6Roles(t, w) {
		link := doc.Find(fmt.Sprintf(`main a[href="/roles/%d"]`, role.ID))
		if link.Length() != 1 || p1Text(link) != role.Name {
			t.Errorf("link to %s's page: %d links, text %q; want one %q", role.Name, link.Length(), p1Text(link), role.Name)
		}
	}
}

// Add, rename and delete as before, now composed from the components.
func TestRolesList_AddRenameDeleteComposed(t *testing.T) {
	w, _, admin := p6World(t)
	doc := p6Get(t, admin, "/roles")
	p6NoDuplicateIDs(t, "/roles", doc)

	add := p1Form(t, "/roles", doc, "/roles")
	p1Hidden(t, "add form", add, "csrf_token")
	if n := add.Find(`.field input[name=name]`).Length(); n != 1 {
		t.Errorf("add form: %d name fields, want 1", n)
	}
	if b := add.Find(`button.btn[type=submit]`); b.Length() != 1 || p1Text(b) != "Add role" {
		t.Errorf("add form: %d buttons, text %q; want one Add role", b.Length(), p1Text(b))
	}

	for _, role := range p6Roles(t, w) {
		rename := fmt.Sprintf("/roles/%d/rename", role.ID)
		del := fmt.Sprintf("/roles/%d/delete", role.ID)
		if role.IsAdmin {
			if n := doc.Find(fmt.Sprintf(`form[action=%q], form[action=%q]`, rename, del)).Length(); n != 0 {
				t.Errorf("Administrator has %d rename or delete forms, want none", n)
			}
			continue
		}
		version := strconv.FormatInt(role.Version, 10)
		f := p1Form(t, role.Name, doc, rename)
		if got := p1Hidden(t, role.Name, f, "version"); got != version {
			t.Errorf("%s rename version = %q, want %q", role.Name, got, version)
		}
		if in := f.Find(`.field input[name=name]`); in.Length() != 1 || in.AttrOr("value", "") != role.Name {
			t.Errorf("%s rename: %d name fields, value %q", role.Name, in.Length(), in.AttrOr("value", ""))
		}
		if b := f.Find(`button.btn[type=submit]`); b.Length() != 1 || p1Text(b) != "Rename" {
			t.Errorf("%s rename: %d buttons, text %q; want one Rename", role.Name, b.Length(), p1Text(b))
		}
		f = p1Form(t, role.Name, doc, del)
		if got := p1Hidden(t, role.Name, f, "version"); got != version {
			t.Errorf("%s delete version = %q, want %q", role.Name, got, version)
		}
		if b := f.Find(`button.btn[type=submit]`); b.Length() != 1 || p1Text(b) != "Delete" {
			t.Errorf("%s delete: %d buttons, text %q; want one Delete", role.Name, b.Length(), p1Text(b))
		}
	}
}

func TestRolesList_WhoCanLink(t *testing.T) {
	_, _, admin := p6World(t)
	link := p6Get(t, admin, "/roles").Find(fmt.Sprintf(`main a[href=%q]`, p6WhoCan))
	if link.Length() != 1 || p1Text(link) != p6WhoCanLabel {
		t.Errorf("Who can links: %d, text %q; want one %q", link.Length(), p1Text(link), p6WhoCanLabel)
	}
}

func TestRolePage_OneCardPerDocTypeWithChange(t *testing.T) {
	w, _, admin := p6World(t)
	doc := p6Get(t, admin, p6RolePath(t, w, p6Stores))

	if got := p1Text(doc.Find("h1")); !strings.Contains(got, p6Stores) {
		t.Errorf("h1 = %q, want the role's name", got)
	}
	if got, want := doc.Find("main .card").Length(), len(doctype.All()); got != want {
		t.Errorf("cards = %d, want one per DocType (%d)", got, want)
	}
	for _, dt := range doctype.All() {
		href := t17RightsPath(t, w, p6Stores, dt.Name)
		card := p6Card(t, doc, dt.Label)
		link := card.Find(fmt.Sprintf(`a[href=%q]`, href))
		if link.Length() != 1 || !strings.HasPrefix(p1Text(link), "Change") {
			t.Errorf("%s card: %d links to %s, text %q; want one Change", dt.Label, link.Length(), href, p1Text(link))
		}
	}
}

// Held rights of the rights that apply; the scope words come from read alone.
func TestRolePage_Summaries(t *testing.T) {
	w, _, admin := p6World(t)
	eight := map[auth.Right]auth.Scope{}
	for _, rt := range []auth.Right{
		auth.RightRead, auth.RightCreate, auth.RightWrite, auth.RightSubmit,
		auth.RightDelete, auth.RightPrint, auth.RightExport, auth.RightImport,
	} {
		eight[rt] = auth.ScopeOwn
	}
	w.SetRights(t, p6Stores, p6Full, eight)
	w.SetRights(t, "Director", p6Full, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn, auth.RightPrint: auth.ScopeAll})

	for _, tc := range []struct{ role, label, want string }{
		{p6Stores, p6FullLabel, "8 of 11 rights · own documents"},
		{"Director", p6FullLabel, "2 of 11 rights · own documents"},
		{p6Stores, "Purchase Order", "1 of 10 rights · all documents"},
		{p6Stores, "GRN", "5 of 9 rights · all documents"},
		{"Employee", "Expense Claim", "6 of 10 rights · team documents"},
		{"Purchase User", "Purchase Order", "7 of 10 rights · all documents"},
		{"Masters Manager", "Item", "6 of 7 rights · all documents"},
		{p6Stores, "Item", p6NoRights},
		{p6Stores, "Expense Claim", p6NoRights},
	} {
		doc := p6Get(t, admin, p6RolePath(t, w, tc.role))
		if got := p6Summary(p6Card(t, doc, tc.label)); got != tc.want {
			t.Errorf("%s on %s: summary = %q, want %q", tc.role, tc.label, got, tc.want)
		}
	}
}

// A row left from when the right applied doesn't count: only rights that
// apply to the DocType do.
func TestRolePage_CountsOnlyRightsThatApply(t *testing.T) {
	w, _, admin := p6World(t)
	w.SetRights(t, p6Stores, authtest.DTItem, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll})
	p1Exec(t, w, `INSERT INTO role_rights (role_id, doctype, right_name, scope) VALUES (?, ?, 'submit', 'all')`,
		t17RoleID(t, w, p6Stores), authtest.DTItem)

	const want = "1 of 7 rights · all documents"
	doc := p6Get(t, admin, p6RolePath(t, w, p6Stores))
	if got := p6Summary(p6Card(t, doc, "Item")); got != want {
		t.Errorf("Item summary = %q, want %q", got, want)
	}
}

func TestRolePage_Administrator(t *testing.T) {
	w, _, admin := p6World(t)
	doc := p6Get(t, admin, p6RolePath(t, w, auth.RoleAdministrator))

	if got := p1Text(doc.Find("h1")); !strings.Contains(got, auth.RoleAdministrator) {
		t.Errorf("h1 = %q, want the role's name", got)
	}
	main := doc.Find("main")
	text := p1Text(main)
	if !strings.Contains(text, p6AdminLine) {
		t.Errorf("page text %q lacks the line %q", text, p6AdminLine)
	}
	if strings.Contains(text, "Change") {
		t.Errorf("page text %q offers Change", text)
	}
	if n := main.Find(`a[href*="/rights/"]`).Length(); n != 0 {
		t.Errorf("links to rights pages = %d, want none", n)
	}
	if n := main.Find(".card").Length(); n != 0 {
		t.Errorf("cards = %d, want none: one line", n)
	}
}

func TestRolePage_UnknownRoleNotFound(t *testing.T) {
	_, _, admin := p6World(t)
	for _, id := range []string{strconv.Itoa(p1NoRoleID), "abc"} {
		t14Want404(t, "GET /roles/"+id, admin.Get("/roles/"+id))
	}
}

func TestRightsPages_AdminOnly(t *testing.T) {
	w, h, _ := p6World(t)
	priya := signedIn(t, h, t10Priya)
	for _, path := range []string{
		p6RolePath(t, w, p6Stores),
		t17RightsPath(t, w, p6Stores, authtest.DTOrder),
		t17RightsPath(t, w, p6Stores, "no.Such"),
	} {
		t14Want403(t, "priya GET "+path, priya.Get(path), t13AdminOnlyRoles, "of 10 rights", p6SaveButton)
	}
}

func TestRightsPages_RoutesDeclaredAdminOnly(t *testing.T) {
	_, h, _ := p6World(t)
	if got := authweb.CheckRoutes(h.Router); len(got) != 0 {
		t.Fatalf("CheckRoutes = %q, want none", got)
	}
	want := []string{
		"GET /roles/{id}", "GET /roles/{id}/rights/{doctype}",
		"POST /roles/{id}/rights/{doctype}", "POST /rights/{doctype}/reset",
	}
	found := map[string]bool{}
	if err := chi.Walk(h.Router, func(method, route string, hd http.Handler, _ ...func(http.Handler) http.Handler) error {
		name := method + " " + route
		if !slices.Contains(want, name) {
			return nil
		}
		g, ok := hd.(*authweb.Guarded)
		if !ok || g.Req.Kind != authweb.KindAdmin || g.Req.Msg != t13AdminOnlyRoles {
			t.Errorf("%s: handler %T is not Admin(%q)", name, hd, t13AdminOnlyRoles)
		}
		found[name] = true
		return nil
	}); err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	for _, name := range want {
		if !found[name] {
			t.Errorf("route %s is missing", name)
		}
	}
}
