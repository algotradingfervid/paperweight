package authweb_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authscreens "github.com/vardhan/internaltools/internal/kernel/auth/web/screens"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// What the plan's P6 tests leave to the AD's rulings: a plain save says so
// (design-system §4 rule 8), and delete means something else on a master.

const p6Saved = "Rights saved."

// p6SavedBanners counts the banners in main saying Rights saved.
func p6SavedBanners(doc *goquery.Document) int {
	return doc.Find("main .banner").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.Contains(p1Text(s), p6Saved)
	}).Length()
}

func TestRightsSave_PlainSaveSaysSaved(t *testing.T) {
	w, _, admin := p6World(t)
	path := t17RightsPath(t, w, p6Stores, authtest.DTClaim)
	r := admin.Post(path, t13With(url.Values{"read": {"own"}, "print": {"all"}}, t17RoleVersion(t, w, p6Stores)))
	wantRedirect(t, "save with read", r, http.StatusSeeOther, path)

	if got := p6SavedBanners(p6Get(t, admin, path)); got != 1 {
		t.Errorf("first GET: %d banners saying saved, want 1", got)
	}
	if got := p6SavedBanners(p6Get(t, admin, path)); got != 0 {
		t.Errorf("second GET: %d banners saying saved, want 0", got)
	}
}

// The read notice shows instead: the page says one thing.
func TestRightsSave_ReadNoticeReplacesSaved(t *testing.T) {
	w, _, admin := p6World(t)
	path := t17RightsPath(t, w, p6Stores, authtest.DTClaim)
	r := admin.Post(path, t13With(url.Values{"print": {"all"}}, t17RoleVersion(t, w, p6Stores)))
	wantRedirect(t, "save without read", r, http.StatusSeeOther, path)

	doc := p6Get(t, admin, path)
	p6WantNotice(t, "the page after the save", doc, 1)
	if got := p6SavedBanners(doc); got != 0 {
		t.Errorf("%d banners saying saved, want 0", got)
	}
}

// No write leaves a row without read; if one exists anyway the line still reads.
func TestRolePage_RowWithoutReadHasNoScopeWords(t *testing.T) {
	w, _, admin := p6World(t)
	w.SetRights(t, p6Stores, authtest.DTItem, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightPrint: auth.ScopeAll})
	p1Exec(t, w, `DELETE FROM role_rights WHERE role_id = ? AND doctype = ? AND right_name = 'read'`,
		t17RoleID(t, w, p6Stores), authtest.DTItem)

	const want = "1 of 7 rights"
	doc := p6Get(t, admin, p6RolePath(t, w, p6Stores))
	if got := p6Summary(p6Card(t, doc, "Item")); got != want {
		t.Errorf("Item summary = %q, want %q", got, want)
	}
}

func TestRightsPage_DeleteMeaningOnMasters(t *testing.T) {
	w, _, admin := p6World(t)
	for _, tc := range []struct{ dt, want, not string }{
		{authtest.DTItem, "Delete records.", "Delete drafts."},
		{authtest.DTOrder, "Delete drafts.", "Delete records."},
	} {
		_, form := p6Open(t, w, admin, p6Stores, tc.dt)
		text := p1Text(p6Row(t, form, "delete"))
		if !strings.Contains(text, tc.want) || strings.Contains(text, tc.not) {
			t.Errorf("%s: delete row %q, want %q and not %q", tc.dt, text, tc.want, tc.not)
		}
	}
}

// Reset can't be undone, so the page asks first (design-system §4 rule 9).
func TestRightsPage_ResetAsksFirst(t *testing.T) {
	w, _, admin := p6World(t)
	doc, _ := p6Open(t, w, admin, p6Stores, authtest.DTOrder)
	const want = "This replaces every role's rights on Purchase Order with the defaults. It can't be undone."

	box := doc.Find("main details.disclosure")
	if box.Length() != 1 {
		t.Fatalf("disclosures = %d, want 1", box.Length())
	}
	if _, open := box.Attr("open"); open {
		t.Errorf("the reset disclosure starts open")
	}
	if text := p1Text(box.Find(".banner")); !strings.Contains(text, want) {
		t.Errorf("disclosure banner %q lacks %q", text, want)
	}
	if b := box.Find(`form button[type=submit]`); b.Length() != 1 || !b.HasClass("btn-error") {
		t.Errorf("reset buttons in the disclosure = %d, class %q; want one Danger", b.Length(), b.AttrOr("class", ""))
	}
}

func TestRightsReset_SaysWhatHappened(t *testing.T) {
	w, _, admin := p6World(t)
	path := t17RightsPath(t, w, p6Stores, authtest.DTOrder)
	const want = "Purchase Order rights were reset to defaults for every role."
	banners := func(doc *goquery.Document) int {
		return doc.Find("main .banner").FilterFunction(func(_ int, s *goquery.Selection) bool {
			return strings.Contains(p1Text(s), want)
		}).Length()
	}
	doc, _ := p6Open(t, w, admin, p6Stores, authtest.DTOrder)
	reset := p1Form(t, "rights page", doc, "/rights/"+authtest.DTOrder+"/reset")

	wantRedirect(t, "reset", admin.Post("/rights/"+authtest.DTOrder+"/reset", p6Form(reset, nil)), http.StatusSeeOther, path)
	if got := banners(p6Get(t, admin, path)); got != 1 {
		t.Errorf("first GET: %d banners, want 1", got)
	}
	if got := banners(p6Get(t, admin, path)); got != 0 {
		t.Errorf("second GET: %d banners, want 0", got)
	}

	wantRedirect(t, "reset from nowhere", admin.Post("/rights/"+authtest.DTOrder+"/reset", url.Values{}), http.StatusSeeOther, "/roles")
	if got := banners(p6Get(t, admin, "/roles")); got != 1 {
		t.Errorf("/roles: %d banners, want 1", got)
	}
}

// The role id only picks the page to return to, and only a page that opens.
func TestRightsReset_RoleWithNoPageGoesToRoles(t *testing.T) {
	w, _, admin := p6World(t)
	path := "/rights/" + authtest.DTOrder + "/reset"
	for what, id := range map[string]string{
		"no such role":  strconv.Itoa(p1NoRoleID),
		"Administrator": t14ID(t17RoleID(t, w, auth.RoleAdministrator)),
	} {
		wantRedirect(t, what, admin.Post(path, url.Values{"role_id": {id}}), http.StatusSeeOther, "/roles")
	}
}

// Who can… and both rights pages show DocTypes in label order, not registered
// or name order.
func TestDocTypes_InLabelOrder(t *testing.T) {
	w, _, admin := p6World(t)
	var want []string
	for _, dt := range doctype.All() {
		want = append(want, dt.Label)
	}
	slices.Sort(want)
	texts := func(s *goquery.Selection) []string {
		var out []string
		s.Each(func(_ int, e *goquery.Selection) { out = append(out, p1Text(e)) })
		return out
	}

	if got := texts(p6Get(t, admin, p6RolePath(t, w, p6Stores)).Find("main .card-title")); !slices.Equal(got, want) {
		t.Errorf("role page = %q, want %q", got, want)
	}
	doc, _ := p6Open(t, w, admin, p6Stores, authtest.DTOrder)
	if got := texts(doc.Find("main .pane-nav a")); !slices.Equal(got, want) {
		t.Errorf("rights page = %q, want %q", got, want)
	}
	if got := texts(p6Get(t, admin, "/rights/who-can").Find(`main select[name=doctype] option`)); !slices.Equal(got, want) {
		t.Errorf("who can = %q, want %q", got, want)
	}
}

func TestRightsPath_EscapesTheDocType(t *testing.T) {
	const want = "/roles/5/rights/Two%20Words"
	if got := authscreens.RightsPath(5, "Two Words"); got != want {
		t.Errorf("RightsPath = %q, want %q", got, want)
	}
}

// The reset form escapes the name too. Rendered directly: a DocType with a
// space, registered here, would stay in the registry for every other test.
func TestRightsPage_ResetPathEscapesTheDocType(t *testing.T) {
	dt := &doctype.DocType{Name: "Two Words", Label: "Two Words"}
	var b strings.Builder
	page := authscreens.RightsPage{Role: auth.RoleInfo{ID: 5}, DocType: dt, DocTypes: []*doctype.DocType{dt}}
	if err := authscreens.Rights(page).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, path := range []string{"/roles/5/rights/Two%20Words", "/rights/Two%20Words/reset"} {
		if n := doc.Find(`form[action="` + path + `"]`).Length(); n != 1 {
			t.Errorf("forms posting to %s = %d, want 1", path, n)
		}
	}
}

// A stale save keeps the page: the message in a Banner, the picks as posted and
// the posted version, so saving again still stops (CD-94).
func TestRightsSave_StaleKeepsThePage(t *testing.T) {
	w, _, admin := p6World(t)
	opened := t17RoleVersion(t, w, p6Stores)
	item := t17RightsPath(t, w, p6Stores, authtest.DTItem)
	wantRedirect(t, "another save", admin.Post(item, t13With(url.Values{"read": {"all"}}, opened)), http.StatusSeeOther, item)

	path := t17RightsPath(t, w, p6Stores, authtest.DTClaim)
	r := admin.Post(path, t13With(url.Values{"print": {"all"}, "write": {"team"}}, opened))
	wantStatus(t, "stale save", r, http.StatusConflict)
	doc := p1Doc(t, "stale save", r)

	if text := p1Text(doc.Find("main .banner")); !strings.Contains(text, t17xStoresChanged) {
		t.Errorf("banner text %q lacks %q", text, t17xStoresChanged)
	}
	// A browser reload would post the stale version again.
	if link := doc.Find(fmt.Sprintf(`main .banner a[href=%q]`, path)); link.Length() != 1 || p1Text(link) != "Reload" {
		t.Errorf("banner links to %s: %d, text %q; want one Reload", path, link.Length(), p1Text(link))
	}
	form := p1Form(t, "stale page", doc, path)
	if got := p1Hidden(t, "stale page", form, "version"); got != opened {
		t.Errorf("hidden version = %q, want the posted %q", got, opened)
	}
	for rt, want := range map[auth.Right]string{auth.RightPrint: "all", auth.RightWrite: "team", auth.RightRead: "", auth.RightCreate: ""} {
		on := p6Row(t, form, rt).Find(`input[type=radio][checked]`)
		if on.Length() != 1 || on.AttrOr("value", "?") != want {
			t.Errorf("%s: %d checked, value %q; want one %q", rt, on.Length(), on.AttrOr("value", "?"), want)
		}
	}
}

// Administrator's page never opens, a stale save included.
func TestRightsSave_StaleAdministratorRefused(t *testing.T) {
	w, _, admin := p6World(t)
	path := t17RightsPath(t, w, auth.RoleAdministrator, authtest.DTOrder)
	version, err := strconv.ParseInt(t17RoleVersion(t, w, auth.RoleAdministrator), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	r := admin.Post(path, t13With(url.Values{"read": {"all"}}, strconv.FormatInt(version+1, 10)))
	wantStatus(t, "stale Administrator save", r, http.StatusBadRequest)
	wantBody(t, "stale Administrator save", r, p1Fixed)
}

// p6Under64rem proves the 64rem rule; this proves the rule outside any media
// query hides the pane, so a phone doesn't show it.
func TestPaneNav_HiddenOutsideTheMediaRule(t *testing.T) {
	files, err := filepath.Glob("../../../ui/static/app.*.css")
	if err != nil || len(files) != 1 {
		t.Fatalf("built CSS: %v, %v", files, err)
	}
	css, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read CSS: %v", err)
	}
	media := regexp.MustCompile(`@media[^{]*\{(?:[^{}]*\{[^{}]*\})*[^{}]*\}`)
	base := media.ReplaceAll(css, nil)
	if !regexp.MustCompile(`\.pane-nav\s*\{[^{}]*display:\s*none`).Match(base) {
		t.Errorf("no .pane-nav rule with display: none outside a media query")
	}
}
