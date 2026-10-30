package authweb_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
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
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Copy rights from another role: design-system §8.6, wp/polish plan P1,
// CD-200. The form is the confirm step (design-system §4 rule 9), so it has
// no dialog.
const (
	p1Route  = "/roles/{id}/copy-rights"
	p1Source = "Copy Source"
	p1Target = "Copy Target"

	p1Title    = "Copy rights to Copy Target"
	p1Warning  = "This replaces every right Copy Target has with the rights of the role you pick."
	p1Button   = "Copy rights"
	p1Choose   = "Choose a role to copy from."
	p1Changed  = "Someone else changed Copy Target after you opened it. Reload it and make your changes again."
	p1BadForm  = "The form could not be read. Reload the page and try again."
	p1Fixed    = "The Administrator role can't be changed."
	p1NoSource = "Role not found. Pick one from the list."
	p1Same     = "Pick a different role to copy from."
	p1FromAdm  = "Administrator has every right that applies and can't be copied."
	p1CSRF     = "Your form has expired. Reload the page and try again."
	p1NoRoleID = 999999
)

var (
	p1SourceOrder = map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightPrint: auth.ScopeAll}
	p1TargetOrder = map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn, auth.RightCreate: auth.ScopeOwn}
	p1TargetGRN   = map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}
)

// p1Exec runs one statement in a write transaction.
func p1Exec(t *testing.T, w *authtest.World, query string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, query, args...)
		return err
	}); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// p1Setup gives the target a row on Order (differs from the source's) and on
// Receipt (the source has none), unmarked, and signs the admin in.
func p1Setup(t *testing.T) (*authtest.World, *webtest.Harness, *webtest.Client) {
	t.Helper()
	w := authtest.NewWorld(t)
	w.SetRights(t, p1Source, authtest.DTOrder, p1SourceOrder)
	w.SetRights(t, p1Target, authtest.DTOrder, p1TargetOrder)
	w.SetRights(t, p1Target, authtest.DTReceipt, p1TargetGRN)
	p1Exec(t, w, `UPDATE role_doctypes SET customised = 0 WHERE role_id = ?`, t17RoleID(t, w, p1Target))
	h := webtest.New(t, w)
	return w, h, signedIn(t, h, t13Admin)
}

func p1Path(t *testing.T, w *authtest.World, role string) string {
	t.Helper()
	return fmt.Sprintf("/roles/%d/copy-rights", t17RoleID(t, w, role))
}

func p1IDText(t *testing.T, w *authtest.World, role string) string {
	t.Helper()
	return t14ID(t17RoleID(t, w, role))
}

// p1Post is the form the page posts: version and source_id.
func p1Post(version, source string) url.Values {
	return url.Values{"version": {version}, "source_id": {source}}
}

// p1Bump saves the target behind the page's back, so the version the page
// opened at goes stale.
func p1Bump(t *testing.T, w *authtest.World) {
	t.Helper()
	id, v := t17RoleID(t, w, p1Target), t14Int(t, w, `SELECT version FROM roles WHERE name = ?`, p1Target)
	if err := w.Auth.SetRightsID(context.Background(), w.P(t, t13Admin), id, authtest.DTItem,
		map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}, v); err != nil {
		t.Fatalf("SetRightsID: %v", err)
	}
}

func p1Doc(t *testing.T, what string, r webtest.Response) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(r.Body))
	if err != nil {
		t.Fatalf("%s: parse page: %v", what, err)
	}
	return doc
}

// p1Text is s's text with white space collapsed.
func p1Text(s *goquery.Selection) string { return strings.Join(strings.Fields(s.Text()), " ") }

// p1Form is the one form that posts to path.
func p1Form(t *testing.T, what string, doc *goquery.Document, path string) *goquery.Selection {
	t.Helper()
	f := doc.Find(fmt.Sprintf(`form[action=%q]`, path))
	if f.Length() != 1 {
		t.Fatalf("%s: %d forms posting to %s, want 1; page: %s", what, f.Length(), path, p1Text(doc.Selection))
	}
	return f
}

func p1Hidden(t *testing.T, what string, form *goquery.Selection, name string) string {
	t.Helper()
	in := form.Find(fmt.Sprintf(`input[type=hidden][name=%q]`, name))
	if in.Length() != 1 {
		t.Fatalf("%s: %d hidden %s inputs, want 1", what, in.Length(), name)
	}
	return in.AttrOr("value", "")
}

// p1Offered is the sorted source_id values of a form, radios or select. A
// select's empty "pick one" entry isn't an option.
func p1Offered(form *goquery.Selection) []string {
	var out []string
	add := func(_ int, s *goquery.Selection) {
		if v := s.AttrOr("value", ""); v != "" {
			out = append(out, v)
		}
	}
	form.Find(`input[type=radio][name=source_id]`).Each(add)
	form.Find(`select[name=source_id] option`).Each(add)
	slices.Sort(out)
	return out
}

// p1WantOffered checks the page offers every role but Administrator and the
// target, each by name.
func p1WantOffered(t *testing.T, what string, w *authtest.World, form *goquery.Selection) {
	t.Helper()
	roles, _, err := w.Auth.RightsMatrix(context.Background(), w.P(t, t13Admin))
	if err != nil {
		t.Fatalf("RightsMatrix: %v", err)
	}
	var want []string
	text := p1Text(form)
	for _, r := range roles {
		if r.IsAdmin || r.Name == p1Target {
			continue
		}
		want = append(want, t14ID(r.ID))
		if !strings.Contains(text, r.Name) {
			t.Errorf("%s: form text %q lacks role %q", what, text, r.Name)
		}
	}
	slices.Sort(want)
	if got := p1Offered(form); !slices.Equal(got, want) {
		t.Errorf("%s: source_id options = %q, want %q", what, got, want)
	}
	if strings.Contains(text, auth.RoleAdministrator) {
		t.Errorf("%s: form text %q offers Administrator", what, text)
	}
}

// p1Keeper records the target's rights and the audit count; the returned
// check fails when a refused request changed either.
func p1Keeper(t *testing.T, w *authtest.World) func(what string) {
	t.Helper()
	order, grn := t17Rights(t, w, p1Target, authtest.DTOrder), t17Rights(t, w, p1Target, authtest.DTReceipt)
	n := len(w.Audit.Entries())
	return func(what string) {
		t.Helper()
		if got := t17Rights(t, w, p1Target, authtest.DTOrder); !reflect.DeepEqual(got, order) {
			t.Errorf("%s: target Order rights = %v, want unchanged %v", what, got, order)
		}
		if got := t17Rights(t, w, p1Target, authtest.DTReceipt); !reflect.DeepEqual(got, grn) {
			t.Errorf("%s: target Receipt rights = %v, want unchanged %v", what, got, grn)
		}
		if got := len(w.Audit.Entries()); got != n {
			t.Errorf("%s: audit entries = %d, want %d", what, got, n)
		}
	}
}

func p1Want4xx(t *testing.T, what string, r webtest.Response, msg string) {
	t.Helper()
	if r.Status < 400 || r.Status > 499 {
		t.Fatalf("%s: status = %d, want 4xx (body %q)", what, r.Status, r.Body)
	}
	wantBody(t, what, r, msg)
}

func TestCopyRightsPage_IsTheConfirmStep(t *testing.T) {
	w, _, admin := p1Setup(t)
	path := p1Path(t, w, p1Target)

	r := admin.Get(path)
	wantStatus(t, "GET copy rights", r, http.StatusOK)
	doc := p1Doc(t, "GET copy rights", r)
	if got := p1Text(doc.Find("h1")); got != p1Title {
		t.Errorf("h1 = %q, want %q", got, p1Title)
	}
	if text := p1Text(doc.Find("main")); !strings.Contains(text, p1Warning) {
		t.Errorf("page text %q lacks the warning %q", text, p1Warning)
	}
	form := p1Form(t, "GET copy rights", doc, path)
	if m := form.AttrOr("method", ""); m != "post" {
		t.Errorf("form method = %q, want post", m)
	}
	if got, want := p1Hidden(t, "GET copy rights", form, "version"), t17RoleVersion(t, w, p1Target); got != want {
		t.Errorf("hidden version = %q, want the target's %q", got, want)
	}
	p1Hidden(t, "GET copy rights", form, "csrf_token")
	buttons := form.Find(`button[type=submit]`)
	if buttons.Length() != 1 || p1Text(buttons) != p1Button || !buttons.HasClass("btn-primary") {
		t.Errorf("submit buttons: %d, text %q, class %q; want one primary %q",
			buttons.Length(), p1Text(buttons), buttons.AttrOr("class", ""), p1Button)
	}
	if n := doc.Find("main dialog").Length(); n != 0 {
		t.Errorf("page has %d dialogs, want none: the page is the confirm step", n)
	}
}

func TestCopyRightsPage_OffersOtherRolesNotAdministrator(t *testing.T) {
	w, _, admin := p1Setup(t)
	path := p1Path(t, w, p1Target)

	r := admin.Get(path)
	wantStatus(t, "GET copy rights", r, http.StatusOK)
	doc := p1Doc(t, "GET copy rights", r)
	p1WantOffered(t, "GET copy rights", w, p1Form(t, "GET copy rights", doc, path))
}

func TestCopyRightsPage_SelectPastSixRoles(t *testing.T) {
	w, _, admin := p1Setup(t) // eight on offer: six seeded, the source, and one added here
	w.SetRights(t, "Copy Extra", authtest.DTItem, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll})
	path := p1Path(t, w, p1Target)

	r := admin.Get(path)
	wantStatus(t, "GET copy rights", r, http.StatusOK)
	form := p1Form(t, "GET copy rights", p1Doc(t, "GET copy rights", r), path)
	if n := form.Find(`select[name=source_id]`).Length(); n != 1 {
		t.Errorf("select[name=source_id] = %d, want 1 past 6 roles", n)
	}
	if n := form.Find(`input[type=radio][name=source_id]`).Length(); n != 0 {
		t.Errorf("radios = %d, want 0 past 6 roles", n)
	}
	p1WantOffered(t, "GET copy rights", w, form)
}

func TestCopyRightsPage_RadiosForFewRoles(t *testing.T) {
	w, _, admin := p1Setup(t)
	gone := `(SELECT id FROM roles WHERE name IN ('Director', 'Purchase Manager', 'Purchase User', 'Masters Manager'))`
	p1Exec(t, w, `DELETE FROM user_roles WHERE role_id IN `+gone)
	p1Exec(t, w, `DELETE FROM roles WHERE id IN `+gone)
	path := p1Path(t, w, p1Target)

	r := admin.Get(path)
	wantStatus(t, "GET copy rights", r, http.StatusOK)
	form := p1Form(t, "GET copy rights", p1Doc(t, "GET copy rights", r), path)
	if n := form.Find(`input[type=radio][name=source_id]`).Length(); n != 3 {
		t.Errorf("radios = %d, want 3 (Stores, Employee, Copy Source)", n)
	}
	if n := form.Find(`select[name=source_id]`).Length(); n != 0 {
		t.Errorf("select[name=source_id] = %d, want 0 for few roles", n)
	}
	p1WantOffered(t, "GET copy rights", w, form)
}

func TestCopyRightsPage_AdminOnly(t *testing.T) {
	w, h, _ := p1Setup(t)
	path := p1Path(t, w, p1Target)
	keep := p1Keeper(t, w)
	priya := signedIn(t, h, t10Priya)

	t14Want403(t, "priya GET copy rights", priya.Get(path), t13AdminOnlyRoles, p1Warning)
	r := priya.Post(path, p1Post(t17RoleVersion(t, w, p1Target), p1IDText(t, w, p1Source)))
	t14Want403(t, "priya POST copy rights", r, t13AdminOnlyRoles)
	keep("priya POST copy rights")
}

func TestCopyRightsPage_AdministratorTargetRefused(t *testing.T) {
	w, _, admin := p1Setup(t)
	path := p1Path(t, w, auth.RoleAdministrator)
	keep := p1Keeper(t, w)

	r := admin.Get(path)
	wantStatus(t, "GET copy rights to Administrator", r, http.StatusBadRequest)
	wantBody(t, "GET copy rights to Administrator", r, p1Fixed)
	if strings.Contains(r.Body, "Copy rights to") {
		t.Errorf("GET copy rights to Administrator: body %q shows the copy page", r.Body)
	}

	r = admin.Post(path, p1Post(t17RoleVersion(t, w, auth.RoleAdministrator), p1IDText(t, w, p1Source)))
	wantStatus(t, "POST copy rights to Administrator", r, http.StatusBadRequest)
	wantBody(t, "POST copy rights to Administrator", r, p1Fixed)
	keep("POST copy rights to Administrator")
}

func TestCopyRightsPage_MissingRoleNotFound(t *testing.T) {
	w, _, admin := p1Setup(t)
	for _, id := range []string{strconv.Itoa(p1NoRoleID), "abc"} {
		path := "/roles/" + id + "/copy-rights"
		t14Want404(t, "GET "+path, admin.Get(path))
		t14Want404(t, "POST "+path, admin.Post(path, p1Post("1", p1IDText(t, w, p1Source))))
	}
}

// The whole round trip: what the page rendered is what is posted.
func TestCopyRightsPost_ReplacesClearsAndRedirects(t *testing.T) {
	w, _, admin := p1Setup(t)
	path := p1Path(t, w, p1Target)
	r := admin.Get(path)
	wantStatus(t, "GET copy rights", r, http.StatusOK)
	form := p1Form(t, "GET copy rights", p1Doc(t, "GET copy rights", r), path)
	source := p1IDText(t, w, p1Source)
	if !slices.Contains(p1Offered(form), source) {
		t.Fatalf("source_id options %q lack the source %s", p1Offered(form), source)
	}
	n0 := len(w.Audit.Entries())

	r = admin.Post(path, p1Post(p1Hidden(t, "GET copy rights", form, "version"), source))
	wantRedirect(t, "POST copy rights", r, http.StatusSeeOther, "/roles")

	if got := t17Rights(t, w, p1Target, authtest.DTOrder); !reflect.DeepEqual(got, p1SourceOrder) {
		t.Errorf("target Order rights = %v, want the source's %v", got, p1SourceOrder)
	}
	if got := t17Rights(t, w, p1Target, authtest.DTReceipt); len(got) != 0 {
		t.Errorf("target Receipt rights = %v, want cleared", got)
	}
	for _, dt := range []string{authtest.DTOrder, authtest.DTReceipt} {
		if c := t17Customised(t, w, p1Target, dt); c != 1 {
			t.Errorf("role_doctypes(target, %s).customised = %d, want 1", dt, c)
		}
	}
	es := w.Audit.Entries()[n0:]
	if len(es) != 1 || es[0].DocType != "Role" || es[0].DocID != t17RoleID(t, w, p1Target) || es[0].Action != "rights" {
		t.Errorf("audit entries = %+v, want one Role rights entry on the target", es)
	}
	wantStatus(t, "GET /roles after the copy", admin.Get("/roles"), http.StatusOK)
}

func TestCopyRightsPost_StaleVersionConflict(t *testing.T) {
	w, _, admin := p1Setup(t)
	path := p1Path(t, w, p1Target)
	opened := t17RoleVersion(t, w, p1Target)
	p1Bump(t, w)
	keep := p1Keeper(t, w)

	r := admin.Post(path, p1Post(opened, p1IDText(t, w, p1Source)))
	wantStatus(t, "POST at the version the page opened", r, http.StatusConflict)
	wantBody(t, "POST at the version the page opened", r, p1Changed)
	keep("POST at the version the page opened")
}

func TestCopyRightsPost_VersionRequired(t *testing.T) {
	w, _, admin := p1Setup(t)
	path := p1Path(t, w, p1Target)
	keep := p1Keeper(t, w)
	source := p1IDText(t, w, p1Source)

	for name, form := range map[string]url.Values{
		"missing":     {"source_id": {source}},
		"non-numeric": p1Post("abc", source),
	} {
		r := admin.Post(path, form)
		wantStatus(t, "POST with version "+name, r, http.StatusBadRequest)
		wantBody(t, "POST with version "+name, r, p1BadForm)
	}
	keep("POST without a usable version")
}

func TestCopyRightsPost_CSRFRequired(t *testing.T) {
	w, _, admin := p1Setup(t)
	path := p1Path(t, w, p1Target)
	keep := p1Keeper(t, w)

	r := admin.NoCSRF().Post(path, p1Post(t17RoleVersion(t, w, p1Target), p1IDText(t, w, p1Source)))
	wantStatus(t, "POST without csrf_token", r, http.StatusForbidden)
	wantBody(t, "POST without csrf_token", r, p1CSRF)
	keep("POST without csrf_token")
}

func TestCopyRightsPost_SourceRefusals(t *testing.T) {
	w, _, admin := p1Setup(t)
	path := p1Path(t, w, p1Target)
	version := t17RoleVersion(t, w, p1Target)
	keep := p1Keeper(t, w)

	for _, tc := range []struct{ name, source, msg string }{
		{"source missing", strconv.Itoa(p1NoRoleID), p1NoSource},
		{"source is the target", p1IDText(t, w, p1Target), p1Same},
		{"source is Administrator", p1IDText(t, w, auth.RoleAdministrator), p1FromAdm},
	} {
		p1Want4xx(t, tc.name, admin.Post(path, p1Post(version, tc.source)), tc.msg)
	}
	keep("refused sources")
}

// None picked or a source_id that isn't a number stays on the page with a
// field error. The page keeps the version it was opened at (CD-94): reloading
// it here would let a stale page post again without a conflict.
func TestCopyRightsPost_NoSourcePickedKeepsThePage(t *testing.T) {
	w, _, admin := p1Setup(t)
	path := p1Path(t, w, p1Target)
	opened := t17RoleVersion(t, w, p1Target)
	p1Bump(t, w)
	keep := p1Keeper(t, w)

	for name, form := range map[string]url.Values{
		"none picked": {"version": {opened}},
		"empty":       p1Post(opened, ""),
		"non-numeric": p1Post(opened, "abc"),
	} {
		r := admin.Post(path, form)
		wantStatus(t, "POST source "+name, r, http.StatusUnprocessableEntity)
		doc := p1Doc(t, "POST source "+name, r)
		if text := p1Text(doc.Find("main")); !strings.Contains(text, p1Choose) {
			t.Errorf("POST source %s: page text %q lacks %q", name, text, p1Choose)
		}
		if strings.Contains(r.Body, p1BadForm) {
			t.Errorf("POST source %s: body %q is the bare bad-form answer", name, r.Body)
		}
		if got := p1Text(doc.Find("h1")); got != p1Title {
			t.Errorf("POST source %s: h1 = %q, want %q", name, got, p1Title)
		}
		f := p1Form(t, "POST source "+name, doc, path)
		p1WantOffered(t, "POST source "+name, w, f)
		if got := p1Hidden(t, "POST source "+name, f, "version"); got != opened {
			t.Errorf("POST source %s: hidden version = %q, want the posted %q", name, got, opened)
		}
	}
	keep("no source picked")
}

func TestRolesPage_CopyRightsLinkPerRole(t *testing.T) {
	w, _, admin := p1Setup(t)
	r := admin.Get("/roles")
	wantStatus(t, "GET /roles", r, http.StatusOK)
	doc := p1Doc(t, "GET /roles", r)

	roles, _, err := w.Auth.RightsMatrix(context.Background(), w.P(t, t13Admin))
	if err != nil {
		t.Fatalf("RightsMatrix: %v", err)
	}
	for _, role := range roles {
		link := doc.Find(fmt.Sprintf(`a[href="/roles/%d/copy-rights"]`, role.ID))
		if role.IsAdmin {
			if link.Length() != 0 {
				t.Errorf("/roles has a copy link for %s", role.Name)
			}
			continue
		}
		if link.Length() != 1 || p1Text(link) != p1Button {
			t.Errorf("/roles link for %s: %d links, text %q; want one %q", role.Name, link.Length(), p1Text(link), p1Button)
		}
	}
}

func TestCopyRightsRoutes_DeclaredAdminOnly(t *testing.T) {
	_, h, _ := p1Setup(t)
	if got := authweb.CheckRoutes(h.Router); len(got) != 0 {
		t.Fatalf("CheckRoutes = %q, want none", got)
	}
	found := map[string]bool{}
	if err := chi.Walk(h.Router, func(method, route string, hd http.Handler, _ ...func(http.Handler) http.Handler) error {
		if route != p1Route {
			return nil
		}
		g, ok := hd.(*authweb.Guarded)
		if !ok || g.Req.Kind != authweb.KindAdmin || g.Req.Msg != t13AdminOnlyRoles {
			t.Errorf("%s %s: handler %T is not Admin(%q)", method, route, hd, t13AdminOnlyRoles)
		}
		found[method] = true
		return nil
	}); err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if !found[http.MethodGet] || !found[http.MethodPost] {
		t.Errorf("routes at %s = %v, want GET and POST", p1Route, found)
	}
}
