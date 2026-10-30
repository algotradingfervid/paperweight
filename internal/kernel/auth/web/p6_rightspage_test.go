package authweb_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// The DocType rights page and its save (plan "P6 detail" items 3 to 6 and 8).

// p6Meanings is the plan's one-line table, exact text.
var p6Meanings = map[auth.Right]string{
	auth.RightRead:    "See records.",
	auth.RightCreate:  "Add new records.",
	auth.RightWrite:   "Change records.",
	auth.RightSubmit:  "Submit for approval or posting.",
	auth.RightApprove: "Approve or reject requests.",
	auth.RightCancel:  "Cancel submitted records.",
	auth.RightAmend:   "Amend cancelled records.",
	auth.RightDelete:  "Delete drafts.",
	auth.RightPrint:   "Print and save as PDF.",
	auth.RightExport:  "Export to CSV.",
	auth.RightImport:  "Import from CSV.",
}

// p6Applicable is dt's rights in AllRights order.
func p6Applicable(dt *doctype.DocType) []auth.Right {
	var out []auth.Right
	for _, rt := range auth.AllRights {
		if auth.Applies(dt, rt) {
			out = append(out, rt)
		}
	}
	return out
}

func p6Lookup(t *testing.T, name string) *doctype.DocType {
	t.Helper()
	dt, ok := doctype.Lookup(name)
	if !ok {
		t.Fatalf("DocType %s is not registered", name)
	}
	return dt
}

// p6Row is the one fieldset holding rt's radios.
func p6Row(t *testing.T, form *goquery.Selection, rt auth.Right) *goquery.Selection {
	t.Helper()
	rows := form.Find("fieldset").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return s.Find(fmt.Sprintf(`input[type=radio][name=%q]`, string(rt))).Length() > 0
	})
	if rows.Length() != 1 {
		t.Fatalf("rows for %s = %d, want 1; form: %s", rt, rows.Length(), p1Text(form))
	}
	return rows
}

// p6Open opens role's rights page on dt and returns it with its save form.
func p6Open(t *testing.T, w *authtest.World, c *webtest.Client, role, dt string) (*goquery.Document, *goquery.Selection) {
	t.Helper()
	path := t17RightsPath(t, w, role, dt)
	doc := p6Get(t, c, path)
	return doc, p1Form(t, "rights page", doc, path)
}

// p6WantNotice checks the page shows the notice n times, in a Banner in main.
func p6WantNotice(t *testing.T, what string, doc *goquery.Document, n int) {
	t.Helper()
	if got := strings.Count(p1Text(doc.Selection), p6ReadAdded); got != n {
		t.Errorf("%s: notice shown %d times, want %d", what, got, n)
	}
	banners := doc.Find("main .banner").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.Contains(p1Text(s), p6ReadAdded)
	})
	if banners.Length() != n {
		t.Errorf("%s: banners with the notice = %d, want %d", what, banners.Length(), n)
	}
}

func TestRightsPage_OneRowPerApplicableRight(t *testing.T) {
	w, _, admin := p6World(t)
	for _, dt := range doctype.All() {
		doc, form := p6Open(t, w, admin, p6Stores, dt.Name)
		p6NoDuplicateIDs(t, dt.Name, doc)
		if got := p1Text(doc.Find("h1")); !strings.Contains(got, dt.Label) {
			t.Errorf("%s: h1 = %q, want the DocType's label", dt.Name, got)
		}
		if text := p1Text(doc.Find("main")); !strings.Contains(text, p6Stores) {
			t.Errorf("%s: page text %q doesn't name the role", dt.Name, text)
		}
		var want, got []string
		for _, rt := range p6Applicable(dt) {
			want = append(want, auth.RightLabel(rt))
		}
		form.Find("fieldset legend").Each(func(_ int, s *goquery.Selection) { got = append(got, p1Text(s)) })
		if !slices.Equal(got, want) {
			t.Errorf("%s: rows = %q, want %q", dt.Name, got, want)
		}
		for _, rt := range auth.AllRights {
			n := form.Find(fmt.Sprintf(`input[name=%q]`, string(rt))).Length()
			if auth.Applies(dt, rt) && n == 0 {
				t.Errorf("%s: no field for %s", dt.Name, rt)
			}
			if !auth.Applies(dt, rt) && n != 0 {
				t.Errorf("%s: %d fields for %s, which doesn't apply", dt.Name, n, rt)
			}
		}
	}
}

func TestRightsPage_MeaningOfEveryRight(t *testing.T) {
	w, _, admin := p6World(t)
	_, form := p6Open(t, w, admin, p6Stores, p6Full)
	if n := form.Find("fieldset").Length(); n != 11 {
		t.Fatalf("rows = %d, want 11", n)
	}
	for _, rt := range auth.AllRights {
		if got, want := p1Text(p6Row(t, form, rt)), p6Meanings[rt]; !strings.Contains(got, want) {
			t.Errorf("%s row text %q lacks %q", rt, got, want)
		}
	}
}

// None / Own / Team / All as a Segmented with the label above it, the layout
// that fits at 320 px; Team only where auth.ScopeOptions offers it.
func TestRightsPage_SegmentedLabelAbove(t *testing.T) {
	w, _, admin := p6World(t)
	for _, dt := range doctype.All() {
		_, form := p6Open(t, w, admin, p6Stores, dt.Name)
		labels, values := []string{"None"}, []string{""}
		for _, sc := range auth.ScopeOptions(dt) {
			labels = append(labels, strings.ToUpper(string(sc)[:1])+string(sc)[1:])
			values = append(values, string(sc))
		}
		for _, rt := range p6Applicable(dt) {
			row := p6Row(t, form, rt)
			if !row.Children().First().Is("legend") {
				t.Errorf("%s %s: the row doesn't start with its label", dt.Name, rt)
			}
			seg := row.Find(".segmented")
			if seg.Length() != 1 {
				t.Fatalf("%s %s: %d segmented controls, want 1", dt.Name, rt, seg.Length())
			}
			var gotLabels, gotValues []string
			seg.Find("label").Each(func(_ int, s *goquery.Selection) { gotLabels = append(gotLabels, p1Text(s)) })
			seg.Find(`input[type=radio]`).Each(func(_ int, s *goquery.Selection) {
				gotValues = append(gotValues, s.AttrOr("value", "?"))
				if name := s.AttrOr("name", ""); name != string(rt) {
					t.Errorf("%s %s: radio name %q", dt.Name, rt, name)
				}
			})
			if !slices.Equal(gotLabels, labels) || !slices.Equal(gotValues, values) {
				t.Errorf("%s %s: options %q values %q, want %q %q", dt.Name, rt, gotLabels, gotValues, labels, values)
			}
		}
	}
}

func TestRightsPage_ShowsCurrentRights(t *testing.T) {
	w, _, admin := p6World(t)
	_, form := p6Open(t, w, admin, "Purchase User", authtest.DTOrder)
	dt := p6Lookup(t, authtest.DTOrder)
	held := t17Defaults(dt, "Purchase User")
	for _, rt := range p6Applicable(dt) {
		on := p6Row(t, form, rt).Find(`input[type=radio][checked]`)
		if on.Length() != 1 || on.AttrOr("value", "?") != string(held[rt]) {
			t.Errorf("%s: %d checked, value %q; want one %q", rt, on.Length(), on.AttrOr("value", "?"), held[rt])
		}
	}
}

func TestRightsPage_NotShownNote(t *testing.T) {
	w, _, admin := p6World(t)
	for _, tc := range []struct{ dt, want string }{
		{authtest.DTOrder, "Not shown because they don't apply to Purchase Order: Import."},
		{authtest.DTReceipt, "Not shown because they don't apply to GRN: Approve, Import."},
		{authtest.DTItem, "Not shown because they don't apply to Item: Submit, Cancel, Amend, Approve."},
	} {
		doc, _ := p6Open(t, w, admin, p6Stores, tc.dt)
		if text := p1Text(doc.Find("main")); !strings.Contains(text, tc.want) {
			t.Errorf("%s: page text %q lacks %q", tc.dt, text, tc.want)
		}
	}
	doc, _ := p6Open(t, w, admin, p6Stores, p6Full)
	if text := p1Text(doc.Find("main")); strings.Contains(text, "Not shown") {
		t.Errorf("all 11 apply: page text %q has the note", text)
	}
}

func TestRightsPage_SaveForm(t *testing.T) {
	w, _, admin := p6World(t)
	doc, form := p6Open(t, w, admin, p6Stores, authtest.DTOrder)
	if m := form.AttrOr("method", ""); m != "post" {
		t.Errorf("form method = %q, want post", m)
	}
	p1Hidden(t, "save form", form, "csrf_token")
	if got, want := p1Hidden(t, "save form", form, "version"), t17RoleVersion(t, w, p6Stores); got != want {
		t.Errorf("hidden version = %q, want %q", got, want)
	}
	b := form.Find(`button[type=submit]`)
	if b.Length() != 1 || p1Text(b) != p6SaveButton || !b.HasClass("btn-primary") {
		t.Errorf("save buttons: %d, text %q, class %q; want one primary %q", b.Length(), p1Text(b), b.AttrOr("class", ""), p6SaveButton)
	}
	if n := doc.Find("main .btn-primary").Length(); n != 1 {
		t.Errorf("primary buttons in main = %d, want 1 (design-system §4 rule 1)", n)
	}
}

func TestRightsPage_NotFound(t *testing.T) {
	w, _, admin := p6World(t)
	stores := p6RolePath(t, w, p6Stores)
	for _, path := range []string{
		stores + "/rights/no.Such",
		fmt.Sprintf("/roles/%d/rights/%s", p1NoRoleID, authtest.DTOrder),
		"/roles/abc/rights/" + authtest.DTOrder,
	} {
		t14Want404(t, "GET "+path, admin.Get(path))
	}
	t14Want404(t, "POST to an unknown DocType", admin.Post(stores+"/rights/no.Such",
		t13With(url.Values{"read": {"all"}}, t17RoleVersion(t, w, p6Stores))))
}

func TestRightsPage_AdministratorRefused(t *testing.T) {
	w, _, admin := p6World(t)
	path := t17RightsPath(t, w, auth.RoleAdministrator, authtest.DTOrder)

	r := admin.Get(path)
	wantStatus(t, "GET Administrator's rights page", r, http.StatusBadRequest)
	wantBody(t, "GET Administrator's rights page", r, p1Fixed)
	if strings.Contains(r.Body, p6SaveButton) {
		t.Errorf("GET Administrator's rights page: body %q has the form", r.Body)
	}

	for what, form := range map[string]url.Values{
		"with read":    {"read": {"all"}, "print": {"all"}},
		"without read": {"print": {"all"}},
	} {
		r = admin.Post(path, t13With(form, t17RoleVersion(t, w, auth.RoleAdministrator)))
		wantStatus(t, "POST Administrator "+what, r, http.StatusBadRequest)
		wantBody(t, "POST Administrator "+what, r, p1Fixed)
		if got := t17Rights(t, w, auth.RoleAdministrator, authtest.DTOrder); len(got) != 0 {
			t.Errorf("POST Administrator %s: rights = %v, want none", what, got)
		}
	}
	p6WantNotice(t, "/roles after the refused save", p6Get(t, admin, "/roles"), 0)
}

func TestRightsPage_ResetButtonAndForm(t *testing.T) {
	w, _, admin := p6World(t)
	for _, tc := range []struct{ dt, want string }{
		{authtest.DTOrder, "Reset Purchase Order to defaults for every role"},
		{authtest.DTClaim, "Reset Expense Claim to defaults for every role"},
	} {
		doc, form := p6Open(t, w, admin, p6Stores, tc.dt)
		reset := p1Form(t, tc.dt, doc, "/rights/"+tc.dt+"/reset")
		if m := reset.AttrOr("method", ""); m != "post" {
			t.Errorf("%s: reset method = %q, want post", tc.dt, m)
		}
		if b := reset.Find(`button[type=submit]`); b.Length() != 1 || p1Text(b) != tc.want {
			t.Errorf("%s: reset buttons %d, text %q; want one %q", tc.dt, b.Length(), p1Text(b), tc.want)
		}
		if strings.Contains(p1Text(form), tc.want) {
			t.Errorf("%s: the reset button is inside the save form", tc.dt)
		}
	}
}

// p6ResetField is the name of the reset form's hidden role id.
func p6ResetField(t *testing.T, reset *goquery.Selection) string {
	t.Helper()
	var names []string
	reset.Find(`input[type=hidden]`).Each(func(_ int, s *goquery.Selection) {
		if n := s.AttrOr("name", ""); n != "csrf_token" {
			names = append(names, n)
		}
	})
	if len(names) != 1 {
		t.Fatalf("reset form hidden fields = %q, want one role id", names)
	}
	return names[0]
}

func TestRightsReset_ReturnsToThePage(t *testing.T) {
	w, _, admin := p6World(t)
	path := t17RightsPath(t, w, p6Stores, authtest.DTOrder)
	w.Grant(t, p6Stores, authtest.DTOrder, auth.RightPrint, auth.ScopeAll)
	doc, _ := p6Open(t, w, admin, p6Stores, authtest.DTOrder)
	reset := p1Form(t, "rights page", doc, "/rights/"+authtest.DTOrder+"/reset")
	field := p6ResetField(t, reset)
	if got, want := p1Hidden(t, "reset form", reset, field), t14ID(t17RoleID(t, w, p6Stores)); got != want {
		t.Errorf("hidden %s = %q, want the role's id %q", field, got, want)
	}
	n0 := len(w.Audit.Entries())

	r := admin.Post("/rights/"+authtest.DTOrder+"/reset", p6Form(reset, nil))
	wantRedirect(t, "reset from the rights page", r, http.StatusSeeOther, path)

	if got, want := t17Rights(t, w, p6Stores, authtest.DTOrder), t17Defaults(p6Lookup(t, authtest.DTOrder), p6Stores); !reflect.DeepEqual(got, want) {
		t.Errorf("Stores on PO after reset = %v, want %v", got, want)
	}
	es := w.Audit.Entries()[n0:]
	if len(es) != 1 || es[0].DocType != "Role" || es[0].DocID != t17RoleID(t, w, p6Stores) || es[0].Action != "reset" {
		t.Errorf("audit entries = %+v, want one Role reset entry on Stores", es)
	}
}

func TestRightsReset_ElseToRoles(t *testing.T) {
	w, _, admin := p6World(t)
	path := "/rights/" + authtest.DTOrder + "/reset"
	doc, _ := p6Open(t, w, admin, p6Stores, authtest.DTOrder)
	field := p6ResetField(t, p1Form(t, "rights page", doc, path))

	wantRedirect(t, "reset with no role id", admin.Post(path, url.Values{}), http.StatusSeeOther, "/roles")
	for _, bad := range []string{"", "abc", "//evil.example"} {
		wantRedirect(t, fmt.Sprintf("reset with role id %q", bad), admin.Post(path, url.Values{field: {bad}}), http.StatusSeeOther, "/roles")
	}
	t14Want404(t, "reset of an unknown DocType", admin.Post("/rights/no.Such/reset", url.Values{}))
}

// p6Under64rem reports whether one of classes is styled by a 64rem media
// query, which is how a pane appears only from there. It reads the built CSS,
// so run make generate first.
func p6Under64rem(t *testing.T, classes string) bool {
	t.Helper()
	files, err := filepath.Glob("../../../ui/static/app.*.css")
	if err != nil || len(files) != 1 {
		t.Fatalf("built CSS: %v, %v", files, err)
	}
	css, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read CSS: %v", err)
	}
	for _, c := range strings.Fields(classes) {
		sel := regexp.QuoteMeta("." + strings.ReplaceAll(c, ":", `\:`))
		if regexp.MustCompile(`64rem[^{]*\{(?:[^{}]*\{[^{}]*\})*?[^{}]*` + sel + `[^-\w:\\]`).Match(css) {
			return true
		}
	}
	return false
}

// From 64rem: the role's DocType list beside the form, on the sidebar layout
// primitive. On a phone only the form, with a Back link.
func TestRightsPage_TwoPanes(t *testing.T) {
	w, _, admin := p6World(t)
	path := t17RightsPath(t, w, p6Stores, authtest.DTOrder)
	doc, _ := p6Open(t, w, admin, p6Stores, authtest.DTOrder)

	layout := doc.Find("main .sidebar-layout")
	if layout.Length() != 1 {
		t.Fatalf(".sidebar-layout = %d, want 1", layout.Length())
	}
	if layout.Closest(".form-width").Length() != 0 {
		t.Errorf("the two panes sit in the 40rem form column")
	}
	panes := layout.Children()
	if panes.Length() != 2 {
		t.Fatalf("panes = %d, want 2", panes.Length())
	}
	list, right := panes.First(), panes.Last()
	if right.Find(fmt.Sprintf(`form[action=%q]`, path)).Length() != 1 {
		t.Errorf("the second pane has no save form")
	}
	if list.Find("form").Length() != 0 {
		t.Errorf("the list pane has a form")
	}
	for _, dt := range doctype.All() {
		href := t17RightsPath(t, w, p6Stores, dt.Name)
		link := list.Find(fmt.Sprintf(`a[href=%q]`, href))
		if link.Length() != 1 || !strings.Contains(p1Text(link), dt.Label) {
			t.Errorf("list pane: %d links to %s, text %q; want one %q", link.Length(), href, p1Text(link), dt.Label)
		}
	}
	if cur := list.Find(`[aria-current="page"]`); cur.Length() != 1 || cur.AttrOr("href", "") != path {
		t.Errorf("current entry: %d, href %q; want one %q", cur.Length(), cur.AttrOr("href", ""), path)
	}
	if !p6Under64rem(t, list.AttrOr("class", "")+" "+layout.AttrOr("class", "")) {
		t.Errorf("list pane class %q: neither it nor the layout has a class under a 64rem media query, so phones would show the list",
			list.AttrOr("class", ""))
	}

	back := doc.Find(fmt.Sprintf(`main a.back-link[href=%q]`, p6RolePath(t, w, p6Stores)))
	if back.Length() != 1 {
		t.Errorf("back links to the role page = %d, want 1", back.Length())
	}
}

// Saving any right grants read: at the widest scope saved, with the notice.
func TestRightsSave_AddsReadAtTheWidestScope(t *testing.T) {
	w, _, admin := p6World(t)
	path := t17RightsPath(t, w, p6Stores, authtest.DTClaim)
	for _, tc := range []struct {
		name string
		form url.Values
		read auth.Scope
	}{
		{"own", url.Values{"print": {"own"}}, auth.ScopeOwn},
		{"team", url.Values{"print": {"own"}, "write": {"team"}}, auth.ScopeTeam},
		{"all", url.Values{"print": {"own"}, "write": {"team"}, "export": {"all"}}, auth.ScopeAll},
		{"all on an early right", url.Values{"create": {"all"}, "print": {"own"}}, auth.ScopeAll},
		{"read posted as None", url.Values{"read": {""}, "print": {"own"}, "write": {"team"}}, auth.ScopeTeam},
	} {
		w.SetRights(t, p6Stores, authtest.DTClaim, map[auth.Right]auth.Scope{})
		want := map[auth.Right]auth.Scope{auth.RightRead: tc.read}
		for k, vs := range tc.form {
			if vs[0] != "" {
				want[auth.Right(k)] = auth.Scope(vs[0])
			}
		}
		r := admin.Post(path, t13With(tc.form, t17RoleVersion(t, w, p6Stores)))
		wantRedirect(t, tc.name, r, http.StatusSeeOther, path)
		if got := t17Rights(t, w, p6Stores, authtest.DTClaim); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: rights = %v, want %v", tc.name, got, want)
		}
		p6WantNotice(t, tc.name+": the page after the save", p6Get(t, admin, path), 1)
	}
}

// The browser's own post: read left on None, one right picked on the page.
func TestRightsSave_FromThePage(t *testing.T) {
	w, _, admin := p6World(t)
	path := t17RightsPath(t, w, p6Stores, authtest.DTItem)
	_, form := p6Open(t, w, admin, p6Stores, authtest.DTItem)

	r := admin.Post(path, p6Form(form, url.Values{"print": {"all"}}))
	wantRedirect(t, "save from the page", r, http.StatusSeeOther, path)
	want := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightPrint: auth.ScopeAll}
	if got := t17Rights(t, w, p6Stores, authtest.DTItem); !reflect.DeepEqual(got, want) {
		t.Errorf("rights = %v, want %v", got, want)
	}
	p6WantNotice(t, "the page after the save", p6Get(t, admin, path), 1)
}

func TestRightsSave_NoticeShownOnce(t *testing.T) {
	w, _, admin := p6World(t)
	path := t17RightsPath(t, w, p6Stores, authtest.DTClaim)
	r := admin.Post(path, t13With(url.Values{"print": {"all"}}, t17RoleVersion(t, w, p6Stores)))
	wantRedirect(t, "save without read", r, http.StatusSeeOther, path)

	p6WantNotice(t, "first GET", p6Get(t, admin, path), 1)
	p6WantNotice(t, "second GET", p6Get(t, admin, path), 0)
}

// Read posted keeps its own scope, however narrow, and there is no notice.
func TestRightsSave_ReadPostedNoNotice(t *testing.T) {
	w, _, admin := p6World(t)
	path := t17RightsPath(t, w, p6Stores, authtest.DTClaim)
	r := admin.Post(path, t13With(url.Values{"read": {"own"}, "print": {"all"}}, t17RoleVersion(t, w, p6Stores)))
	wantRedirect(t, "save with read", r, http.StatusSeeOther, path)

	want := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn, auth.RightPrint: auth.ScopeAll}
	if got := t17Rights(t, w, p6Stores, authtest.DTClaim); !reflect.DeepEqual(got, want) {
		t.Errorf("rights = %v, want %v", got, want)
	}
	p6WantNotice(t, "the page after the save", p6Get(t, admin, path), 0)
}

// Nothing set adds nothing: None on every right clears the row.
func TestRightsSave_NothingSetClears(t *testing.T) {
	w, _, admin := p6World(t)
	path := t17RightsPath(t, w, p6Stores, authtest.DTOrder)
	_, form := p6Open(t, w, admin, p6Stores, authtest.DTOrder)

	r := admin.Post(path, p6Form(form, url.Values{"read": {""}}))
	wantRedirect(t, "save with nothing set", r, http.StatusSeeOther, path)
	if got := t17Rights(t, w, p6Stores, authtest.DTOrder); len(got) != 0 {
		t.Errorf("rights = %v, want none", got)
	}
	p6WantNotice(t, "the page after the save", p6Get(t, admin, path), 0)
}

// The page posted back as it opened changes nothing: no write, no audit, no
// notice.
func TestRightsSave_UnchangedPageWritesNothing(t *testing.T) {
	w, _, admin := p6World(t)
	path := t17RightsPath(t, w, "Purchase User", authtest.DTOrder)
	_, form := p6Open(t, w, admin, "Purchase User", authtest.DTOrder)
	before, version := t17Rights(t, w, "Purchase User", authtest.DTOrder), t17RoleVersion(t, w, "Purchase User")
	n0 := len(w.Audit.Entries())

	r := admin.Post(path, p6Form(form, nil))
	wantRedirect(t, "unchanged save", r, http.StatusSeeOther, path)
	if got := t17Rights(t, w, "Purchase User", authtest.DTOrder); !reflect.DeepEqual(got, before) {
		t.Errorf("rights = %v, want unchanged %v", got, before)
	}
	if got := t17RoleVersion(t, w, "Purchase User"); got != version {
		t.Errorf("version = %s, want unchanged %s", got, version)
	}
	if got := len(w.Audit.Entries()); got != n0 {
		t.Errorf("audit entries = %d, want %d", got, n0)
	}
	p6WantNotice(t, "the page after the save", p6Get(t, admin, path), 0)
}

// AUTH-065 stays at the service: only the web handler adds read.
func TestRightsSave_ServiceStillRefusesARowWithoutRead(t *testing.T) {
	w, _, _ := p6World(t)
	before := t17Rights(t, w, p6Stores, authtest.DTOrder)
	n0 := len(w.Audit.Entries())
	version := t14Int(t, w, `SELECT version FROM roles WHERE name = ?`, p6Stores)

	err := w.Auth.SetRightsID(context.Background(), w.P(t, t13Admin), t17RoleID(t, w, p6Stores), authtest.DTOrder,
		map[auth.Right]auth.Scope{auth.RightPrint: auth.ScopeAll}, version)
	var problem *auth.Problem
	if !errors.As(err, &problem) || problem.Code != http.StatusBadRequest || problem.Msg != p6ReadNeeded {
		t.Fatalf("SetRightsID without read = %v, want 400 %q", err, p6ReadNeeded)
	}
	if got := t17Rights(t, w, p6Stores, authtest.DTOrder); !reflect.DeepEqual(got, before) {
		t.Errorf("rights = %v, want unchanged %v", got, before)
	}
	if got := len(w.Audit.Entries()); got != n0 {
		t.Errorf("audit entries = %d, want %d", got, n0)
	}
}

// p6Refused posts form as Stores on dt and checks the answer changed nothing
// and left no notice for the next page.
func p6Refused(t *testing.T, w *authtest.World, c *webtest.Client, what, dt string, form url.Values, check func(webtest.Response)) {
	t.Helper()
	path := t17RightsPath(t, w, p6Stores, dt)
	before, n0 := t17Rights(t, w, p6Stores, dt), len(w.Audit.Entries())
	check(c.Post(path, form))
	if got := t17Rights(t, w, p6Stores, dt); !reflect.DeepEqual(got, before) {
		t.Errorf("%s: rights = %v, want unchanged %v", what, got, before)
	}
	if got := len(w.Audit.Entries()); got != n0 {
		t.Errorf("%s: audit entries = %d, want %d", what, got, n0)
	}
	p6WantNotice(t, what+": the next page", p6Get(t, c, path), 0)
}

func TestRightsSave_StaleVersionConflict(t *testing.T) {
	w, _, admin := p6World(t)
	opened := t17RoleVersion(t, w, p6Stores)
	item := t17RightsPath(t, w, p6Stores, authtest.DTItem)
	wantRedirect(t, "another save", admin.Post(item, t13With(url.Values{"read": {"all"}}, opened)), http.StatusSeeOther, item)

	p6Refused(t, w, admin, "stale, read to add", authtest.DTClaim, t13With(url.Values{"print": {"all"}}, opened), func(r webtest.Response) {
		wantStatus(t, "stale save", r, http.StatusConflict)
		wantBody(t, "stale save", r, t17xStoresChanged)
	})
}

func TestRightsSave_Refusals(t *testing.T) {
	w, _, admin := p6World(t)
	for _, tc := range []struct {
		name, dt string
		form     url.Values
		msg      string
	}{
		{"team where not offered", authtest.DTOrder, url.Values{"print": {"team"}}, "Team does not apply to Purchase Order. Choose Own or All."},
		{"right that doesn't apply", authtest.DTItem, url.Values{"submit": {"all"}}, "Submit does not apply to Item. Clear it."},
		{"right that doesn't apply, read posted", authtest.DTItem, url.Values{"read": {"all"}, "submit": {"all"}}, "Submit does not apply to Item. Clear it."},
		{"scope that isn't one", authtest.DTClaim, url.Values{"print": {"everyone"}}, "Scope for print must be own, team or all."},
	} {
		form := t13With(tc.form, t17RoleVersion(t, w, p6Stores))
		p6Refused(t, w, admin, tc.name, tc.dt, form, func(r webtest.Response) {
			wantStatus(t, tc.name, r, http.StatusBadRequest)
			wantBody(t, tc.name, r, tc.msg)
		})
	}
}

// The right is unknown whether or not read was posted, and read is not added.
func TestRightsSave_UnknownRightWithoutRead(t *testing.T) {
	w, _, admin := p6World(t)
	form := t13With(url.Values{"fly": {"all"}}, t17RoleVersion(t, w, p6Stores))
	p6Refused(t, w, admin, "unknown right", authtest.DTClaim, form, func(r webtest.Response) {
		t17WantFormError(t, "unknown right", r, "CHECK", "constraint", "sqlite", "SQL")
	})
}

// One entry per save, with the read that was added in it (AUTH-068).
func TestRightsSave_Audited(t *testing.T) {
	w, _, admin := p6World(t)
	path := t17RightsPath(t, w, p6Stores, authtest.DTClaim)
	n0 := len(w.Audit.Entries())

	r := admin.Post(path, t13With(url.Values{"print": {"all"}}, t17RoleVersion(t, w, p6Stores)))
	wantRedirect(t, "save without read", r, http.StatusSeeOther, path)
	es := w.Audit.Entries()[n0:]
	if len(es) != 1 {
		t.Fatalf("audit entries = %d (%+v), want 1", len(es), es)
	}
	want := audit.Entry{
		DocType: "Role", DocID: t17RoleID(t, w, p6Stores), Action: "rights", By: w.P(t, t13Admin).ID, At: es[0].At,
		Changes: []audit.Change{
			{Field: authtest.DTClaim + ".read", Before: "none", After: "all", Op: "set"},
			{Field: authtest.DTClaim + ".print", Before: "none", After: "all", Op: "set"},
		},
	}
	if !reflect.DeepEqual(es[0], want) {
		t.Errorf("audit entry =\n %+v\nwant\n %+v", es[0], want)
	}
}
