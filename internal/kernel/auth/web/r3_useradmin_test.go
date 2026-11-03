package authweb_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// The user administration screens (UI v2 R3, shell review B1). Every post goes
// through the form the page rendered: r3Post reads its action and hidden
// fields, so deleting a form or its hidden version fails here.

const (
	r3Kiran    = "kiran@vardhan.test"
	r3Temp     = "Temp-Godown-2026"
	r3Employee = "Employee"
	r3Stores   = "Stores"

	r3Typed = "not-echoed-Long-Secret-77"
)

// r3Find is the one form on doc that posts to action.
func r3Find(t *testing.T, doc *goquery.Document, action string) *goquery.Selection {
	t.Helper()
	f := doc.Find(`form[method="post"][action="` + action + `"]`)
	if f.Length() != 1 {
		t.Fatalf("%d forms post to %s, want 1", f.Length(), action)
	}
	return f
}

// r3Post posts doc's form for action as a browser would: its hidden fields,
// its ticked boxes, then typed. A key in typed replaces what the form holds.
func r3Post(t *testing.T, c *webtest.Client, doc *goquery.Document, action string, typed url.Values) webtest.Response {
	t.Helper()
	form := url.Values{}
	r3Find(t, doc, action).Find("input").Each(func(_ int, in *goquery.Selection) {
		name, kind := in.AttrOr("name", ""), in.AttrOr("type", "")
		_, checked := in.Attr("checked")
		if name != "" && name != "csrf_token" && (kind == "hidden" || checked) {
			form.Add(name, in.AttrOr("value", ""))
		}
	})
	for k, vs := range typed {
		form[k] = vs
	}
	return c.Post(action, form)
}

// r3Ticked is the ticked roles of doc's form for action.
func r3Ticked(t *testing.T, doc *goquery.Document, action string) []string {
	t.Helper()
	var out []string
	r3Find(t, doc, action).Find(`input[type=checkbox][name=roles][checked]`).Each(func(_ int, in *goquery.Selection) {
		out = append(out, in.AttrOr("value", ""))
	})
	return out
}

func r3Roles(t *testing.T, w *authtest.World, email string) []string {
	t.Helper()
	got := slices.Clone(w.P(t, email).Roles)
	slices.Sort(got)
	return got
}

// r3Version is the version doc's form for action posts.
func r3Version(t *testing.T, doc *goquery.Document, action string) string {
	t.Helper()
	return r3Find(t, doc, action).Find(`input[type=hidden][name=version]`).AttrOr("value", "")
}

// r3Banner is the page's only main banner of class, with text want.
func r3Banner(t *testing.T, what string, doc *goquery.Document, class, want string) {
	t.Helper()
	b := doc.Find("main .banner." + class)
	if b.Length() != 1 || !strings.Contains(p1Text(b), want) {
		t.Errorf("%s: %d %s banners, text %q; want one with %q", what, b.Length(), class, p1Text(b), want)
	}
}

// r3Summary is the page's error summary with the owner's text (CD-91).
func r3Summary(t *testing.T, what string, doc *goquery.Document, want string) {
	t.Helper()
	if got := p1Text(doc.Find(".error-summary li a")); got != want {
		t.Errorf("%s: summary = %q, want %q", what, got, want)
	}
}

func r3NoBanner(t *testing.T, what string, doc *goquery.Document, class string) {
	t.Helper()
	if n := doc.Find("main .banner." + class).Length(); n != 0 {
		t.Errorf("%s: %d %s banners, want none", what, n, class)
	}
}

// B1: the list leads to the new-user form.
func TestUsers_NewUserLink(t *testing.T) {
	_, _, admin := p6World(t)
	doc := p6Get(t, admin, "/users")
	if a := doc.Find(`main a.btn[href="/users/new"]`); p1Text(a) != "New user" {
		t.Errorf("New user link = %q", p1Text(a))
	}
}

// The form offers every role with Employee ticked (D-AUTH-22), marks what is
// required and starts with the v2 note (§5.5 item 2).
func TestNewUser_Form(t *testing.T) {
	_, _, admin := p6World(t)
	doc := p6Get(t, admin, "/users/new")
	if got := r3Ticked(t, doc, "/users"); !reflect.DeepEqual(got, []string{r3Employee}) {
		t.Errorf("ticked roles = %q, want Employee", got)
	}
	f := r3Find(t, doc, "/users")
	for _, role := range []string{auth.RoleAdministrator, r3Stores, r3Employee} {
		if f.Find(`input[name=roles][value="`+role+`"]`).Length() != 1 {
			t.Errorf("no %s role on offer", role)
		}
	}
	for _, name := range []string{"name", "email", "password"} {
		if f.Find(`input[name=`+name+`][aria-required=true]`).Length() != 1 {
			t.Errorf("%s isn't marked required", name)
		}
	}
	if f.Find(`input[name=mobile][aria-required=true]`).Length() != 0 {
		t.Error("mobile is marked required")
	}
	if !strings.Contains(p1Text(f.Find(".field__hint").First()), "are required.") {
		t.Errorf("form doesn't start with the required note: %q", p1Text(f))
	}
	if got := f.Find(`input[name=password]`).AttrOr("autocomplete", ""); got != "new-password" {
		t.Errorf("password autocomplete = %q", got)
	}
	if a := doc.Find(`main a[href="/users"]`); a.Length() == 0 {
		t.Error("no way back to Users")
	}
}

// A saved user can sign in, holds the roles ticked, and the page says it.
func TestNewUser_SavesFromTheForm(t *testing.T) {
	w, h, admin := p6World(t)
	doc := p6Get(t, admin, "/users/new")
	r := r3Post(t, admin, doc, "/users", url.Values{
		"name": {"Kiran Joshi"}, "email": {r3Kiran}, "mobile": {"+91 98200 12345"}, "password": {r3Typed}, "roles": {r3Stores, r3Employee},
	})
	wantStatus(t, "add", r, http.StatusSeeOther)
	page := p6Get(t, admin, r.Header.Get("Location"))
	r3Banner(t, "after add", page, "banner--success", "Kiran Joshi was added.")
	var name, mobile string
	t13Read(t, w, "user", func(ctx context.Context, q db.DBTX) error {
		return q.QueryRowContext(ctx, `SELECT name, mobile FROM users WHERE email = ?`, r3Kiran).Scan(&name, &mobile)
	})
	if name != "Kiran Joshi" || mobile != "+91 98200 12345" {
		t.Errorf("stored name %q, mobile %q", name, mobile)
	}
	var roles []string
	t13Read(t, w, "roles", func(ctx context.Context, q db.DBTX) error {
		rows, err := q.QueryContext(ctx, `SELECT r.name FROM user_roles ur JOIN roles r ON r.id = ur.role_id JOIN users u ON u.id = ur.user_id WHERE u.email = ? ORDER BY r.name`, r3Kiran)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				return err
			}
			roles = append(roles, n)
		}
		return rows.Err()
	})
	if !reflect.DeepEqual(roles, []string{r3Employee, r3Stores}) {
		t.Errorf("roles = %q", roles)
	}
	kiran := h.Client()
	wantBody(t, "new user signs in", kiran.Login(r3Kiran, r3Typed), "SIGNED IN")
	wantRedirect(t, "first request", kiran.Get("/users"), http.StatusSeeOther, "/account/password")
}

// A refusal redraws the form with the owner's text on its field and everything
// typed, except the password.
func TestNewUser_RefusalKeepsEntries(t *testing.T) {
	cases := []struct {
		name, email, password string
		status                int
		text, field           string
	}{
		{"taken email", t10Priya, r3Typed, http.StatusConflict, auth.EmailTakenError(t10Priya).Msg, "email"},
		{"bad email", "priya@", r3Typed, http.StatusBadRequest, auth.ErrEmailInvalid.Msg, "email"},
		{"short password", r3Kiran, "short-1", http.StatusBadRequest, auth.ErrPasswordTooShort.Msg, "password"},
		{"long password", r3Kiran, strings.Repeat("x7", 129), http.StatusBadRequest, auth.ErrPasswordTooLong.Msg, "password"},
		{"common password", r3Kiran, "password123", http.StatusBadRequest, auth.ErrPasswordCommon.Msg, "password"},
		{"password holds the email", r3Kiran, "kiran-long-enough-1", http.StatusBadRequest, auth.ErrPasswordHasName.Msg, "password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, admin := p6World(t)
			doc := p6Get(t, admin, "/users/new")
			r := r3Post(t, admin, doc, "/users", url.Values{
				"name": {"Kiran Joshi"}, "email": {tc.email}, "mobile": {"98200"}, "password": {tc.password}, "roles": {r3Stores},
			})
			wantStatus(t, tc.name, r, tc.status)
			if strings.Contains(r.Body, tc.password) {
				t.Error("the page echoes the password")
			}
			page := p1Doc(t, tc.name, r)
			if got := p1Text(page.Find(".error-summary li a")); got != tc.text {
				t.Errorf("summary = %q, want %q", got, tc.text)
			}
			if got := page.Find(".error-summary li a").AttrOr("href", ""); got != "#f-"+tc.field {
				t.Errorf("summary link = %q, want #f-%s", got, tc.field)
			}
			if got := p1Text(page.Find("#f-" + tc.field + "-msg")); !strings.Contains(got, tc.text) {
				t.Errorf("field message = %q, want %q", got, tc.text)
			}
			other := map[string]string{"email": "password", "password": "email"}[tc.field]
			if got := p1Text(page.Find("#f-" + other + "-msg")); got != "" {
				t.Errorf("%s also shows %q", other, got)
			}
			f := r3Find(t, page, "/users")
			for name, want := range map[string]string{"name": "Kiran Joshi", "email": tc.email, "mobile": "98200", "password": ""} {
				if got := f.Find(`input[name=`+name+`]`).AttrOr("value", ""); got != want {
					t.Errorf("%s keeps %q, want %q", name, got, want)
				}
			}
			if got := r3Ticked(t, page, "/users"); !reflect.DeepEqual(got, []string{r3Stores}) {
				t.Errorf("ticked roles = %q, want Stores", got)
			}
		})
	}
}

// A blank name stays on the form, with the Name field's own text and the rest
// of the entries kept.
func TestNewUser_BlankNameRefused(t *testing.T) {
	for _, name := range []string{"", "   "} {
		w, _, admin := p6World(t)
		r := r3Post(t, admin, p6Get(t, admin, "/users/new"), "/users", url.Values{"name": {name}, "email": {r3Kiran}, "password": {r3Typed}})
		wantStatus(t, "blank name", r, http.StatusBadRequest)
		page := p1Doc(t, "blank name", r)
		if got := page.Find(".error-summary li a").AttrOr("href", ""); got != "#f-name" {
			t.Errorf("summary link = %q, want #f-name", got)
		}
		if got := p1Text(page.Find("#f-name-msg")); !strings.Contains(got, auth.ErrNameRequired.Msg) {
			t.Errorf("name message = %q", got)
		}
		if got := p1Text(page.Find("#f-email-msg")); got != "" {
			t.Errorf("email also shows %q", got)
		}
		if got := r3Find(t, page, "/users").Find(`input[name=email]`).AttrOr("value", ""); got != r3Kiran {
			t.Errorf("email keeps %q", got)
		}
		if t13UserExists(t, w, r3Kiran) {
			t.Error("a blank name saved")
		}
	}
}

// A name typed with spaces round it says the stored name.
func TestNewUser_ResultTrimsTheName(t *testing.T) {
	_, _, admin := p6World(t)
	r := r3Post(t, admin, p6Get(t, admin, "/users/new"), "/users", url.Values{"name": {"  Kiran Joshi  "}, "email": {r3Kiran}, "password": {r3Typed}})
	wantStatus(t, "add", r, http.StatusSeeOther)
	r3Banner(t, "after add", p6Get(t, admin, r.Header.Get("Location")), "banner--success", "Kiran Joshi was added.")
}

// AUTH-052: adding a user with every role unticked warns on their page.
func TestNewUser_NoRolesWarns(t *testing.T) {
	_, _, admin := p6World(t)
	r := r3Post(t, admin, p6Get(t, admin, "/users/new"), "/users", url.Values{"name": {"Kiran Joshi"}, "email": {r3Kiran}, "password": {r3Typed}, "roles": {}})
	wantStatus(t, "add", r, http.StatusSeeOther)
	page := p6Get(t, admin, r.Header.Get("Location"))
	if got := p1Text(page.Find("#alerts .banner--warning")); !strings.Contains(got, fmt.Sprintf(t13NoRolesWarning, "Kiran Joshi")) {
		t.Errorf("alerts = %q", got)
	}
}

// A role the form never offered keeps its plain answer (AD-1).
func TestNewUser_UnknownRoleIsBadForm(t *testing.T) {
	_, _, admin := p6World(t)
	r := admin.Post("/users", url.Values{"name": {"K"}, "email": {r3Kiran}, "password": {r3Typed}, "roles": {"Nobody"}})
	wantStatus(t, "unknown role", r, http.StatusBadRequest)
	wantBody(t, "unknown role", r, t13BadForm)
	if p1Doc(t, "unknown role", r).Find("form").Length() != 0 {
		t.Error("a caller's bug redrew the form")
	}
}

// So does a role the roles form never offered: the page isn't redrawn around it.
func TestUserRoles_UnknownRoleIsBadForm(t *testing.T) {
	w, _, admin := p6World(t)
	path := t13UserPath(w, t10Priya, "")
	r := r3Post(t, admin, p6Get(t, admin, path), path+"/roles", url.Values{"roles": {"Nobody"}})
	wantStatus(t, "unknown role", r, http.StatusBadRequest)
	wantBody(t, "unknown role", r, t13BadForm)
	if p1Doc(t, "unknown role", r).Find("form").Length() != 0 {
		t.Error("a caller's bug redrew the page")
	}
}

// r3UserRoutes are the administrators' routes this task draws forms on: a
// user holding only Administrator gets past each guard, and a user holding
// every role but Administrator gets the route's own text.
func r3UserRoutes(w *authtest.World) []struct {
	name, method, path, text string
	ok                       int
} {
	id := func(suffix string) string { return t13UserPath(w, t10Deepak, suffix) }
	return []struct {
		name, method, path, text string
		ok                       int
	}{
		{"new form", http.MethodGet, "/users/new", auth.ErrAdminOnlyUsers.Msg, http.StatusOK},
		{"create", http.MethodPost, "/users", auth.ErrAdminOnlyUsers.Msg, http.StatusBadRequest},
		{"reset form", http.MethodGet, id("/password"), auth.ErrNotAdminPassword.Msg, http.StatusOK},
		{"reset", http.MethodPost, id("/password"), auth.ErrNotAdminPassword.Msg, http.StatusBadRequest},
		{"unlock", http.MethodPost, id("/unlock"), auth.ErrAdminOnlyField.Msg, http.StatusSeeOther},
		{"roles", http.MethodPost, id("/roles"), auth.ErrAdminOnlyRoles.Msg, http.StatusBadRequest},
		{"delete", http.MethodPost, id("/delete"), auth.ErrAdminOnlyUsers.Msg, http.StatusBadRequest},
	}
}

// The Administrator role alone opens each route, and the other roles together
// open none, with each route's own text and nothing changed.
func TestUserAdmin_Rights(t *testing.T) {
	w, h, admin := p6World(t)
	if got := w.P(t, t13Admin).Roles; !reflect.DeepEqual(got, []string{auth.RoleAdministrator}) {
		t.Fatalf("fixture admin roles = %q, want Administrator only", got)
	}
	w.AddUser(t, r3Kiran, "Kiran Joshi", "Director", "Purchase Manager", "Purchase User", r3Stores, "Masters Manager", r3Employee)
	kiran := signedIn(t, h, r3Kiran)
	before := t13DBVersion(t, w, t10Deepak)
	do := func(c *webtest.Client, method, path string) webtest.Response {
		if method == http.MethodGet {
			return c.Get(path)
		}
		return c.Post(path, url.Values{})
	}
	for _, tc := range r3UserRoutes(w) {
		r := do(kiran, tc.method, tc.path)
		wantStatus(t, tc.name+" without the role", r, http.StatusForbidden)
		wantBody(t, tc.name+" without the role", r, tc.text)
	}
	if got := t13DBVersion(t, w, t10Deepak); got != before {
		t.Errorf("deepak's version moved from %s to %s", before, got)
	}
	for _, tc := range r3UserRoutes(w) {
		wantStatus(t, tc.name+" with the role", do(admin, tc.method, tc.path), tc.ok)
	}
}

// An Administrator's page has every form; anyone else's own page has none of
// them and names no role.
func TestUserPage_FormsAreForAdministrators(t *testing.T) {
	w, h, admin := p6World(t)
	path := t13UserPath(w, t10Priya, "")
	doc := p6Get(t, admin, path)
	version := t13DBVersion(t, w, t10Priya)
	for _, action := range []string{path + "/roles", path + "/delete"} {
		if got := r3Version(t, doc, action); got != version {
			t.Errorf("%s posts version %q, want %q", action, got, version)
		}
	}
	if got := r3Ticked(t, doc, path+"/roles"); !reflect.DeepEqual(got, []string{"Purchase User", r3Employee}) {
		t.Errorf("ticked = %q", got)
	}
	if got := doc.Find(`main a.btn[href="` + path + `/password"]`); p1Text(got) != "Reset password" {
		t.Errorf("reset link = %q", p1Text(got))
	}
	if doc.Find(`main a[href="/users"]`).Length() == 0 {
		t.Error("no Back to Users")
	}

	priya := signedIn(t, h, t10Priya)
	own := p6Get(t, priya, path)
	if n := own.Find("main form").Length(); n != 0 {
		t.Errorf("own page has %d forms", n)
	}
	if n := own.Find("main dialog, main a.btn").Length(); n != 0 {
		t.Errorf("own page has %d admin controls", n)
	}
	text := p1Text(own.Find("main"))
	for _, hidden := range []string{"Purchase User", "Administrator", "locked", "Delete", "Reset password"} {
		if strings.Contains(text, hidden) {
			t.Errorf("own page shows %q: %q", hidden, text)
		}
	}
	if own.Find(`main a[href="/users"]`).Length() != 0 {
		t.Error("own page links Users, which refuses them")
	}
	wantStatus(t, "someone else's page", priya.Get(t13UserPath(w, t10Deepak, "")), http.StatusForbidden)
}

// A reset would end the viewer's own session, so their own page offers none,
// and the typed URL leads to Change password.
func TestUserPage_OwnPageHasNoReset(t *testing.T) {
	w, _, admin := p6World(t)
	path := t13UserPath(w, t13Admin, "")
	if n := p6Get(t, admin, path).Find(`a[href="` + path + `/password"]`).Length(); n != 0 {
		t.Errorf("%d reset links on the admin's own page", n)
	}
	wantRedirect(t, "own reset URL", admin.Get(path+"/password"), http.StatusSeeOther, "/account/password")
}

// r3Enabled asserts the enabled flag doc's form for path posts.
func r3Enabled(t *testing.T, doc *goquery.Document, path, want string) {
	t.Helper()
	if got := r3Find(t, doc, path).Find(`input[type=hidden][name=enabled]`).AttrOr("value", ""); got != want {
		t.Errorf("form posts enabled=%q, want %q", got, want)
	}
}

// D-AUTH-17: disabling goes through a confirm, ends the user's sessions, keeps
// their name and mobile, audits the change and offers Enable; Enable undoes it.
func TestUserDisable_FromTheForm(t *testing.T) {
	w, h, admin := p6World(t)
	priya := signedIn(t, h, t10Priya)
	path := t13UserPath(w, t10Priya, "")
	const mobile = "+91 98200 12345"
	if err := w.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), `UPDATE users SET mobile = ? WHERE email = ?`, mobile, t10Priya)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	before := t13User(t, w, t10Priya)
	if before.Mobile != mobile || before.Name != "Priya Shah" {
		t.Fatalf("before = %+v", before)
	}
	doc := p6Get(t, admin, path)
	r3Enabled(t, doc, path, "false")
	if got := p1Text(doc.Find("dialog#disable-user h2")); got != "Disable Priya Shah?" {
		t.Errorf("confirm title = %q", got)
	}
	if doc.Find(`dialog#disable-user form[action="`+path+`"]`).Length() != 1 {
		t.Error("the disable form isn't in the confirm")
	}
	opener := doc.Find(`main button[data-open-dialog="disable-user"]`)
	if opener.AttrOr("type", "") != "button" || opener.Closest("form").Length() != 0 {
		t.Error("the Disable button can post without the confirm")
	}

	wantRedirect(t, "disable", r3Post(t, admin, doc, path, nil), http.StatusSeeOther, path)
	after := t13User(t, w, t10Priya)
	if after.Enabled != 0 || after.Name != "Priya Shah" || after.Mobile != mobile {
		t.Errorf("after disable %+v, before %+v", after, before)
	}
	if n := t13Sessions(t, w, t10Priya); n != 0 {
		t.Errorf("%d sessions left", n)
	}
	if got := priya.Get("/"); got.Status == http.StatusOK {
		t.Error("priya's session survived")
	}
	entries := w.Audit.For("User", int64(w.Users[t10Priya]))
	if got := fmt.Sprintf("%+v", entries[len(entries)-1].Changes); !strings.Contains(got, "enabled") {
		t.Errorf("audit changes = %s", got)
	}
	page := p6Get(t, admin, path)
	r3Banner(t, "after disable", page, "banner--success", "Priya Shah is disabled and signed out everywhere.")
	if page.Find("dialog#disable-user").Length() != 0 {
		t.Error("a disabled user still offers Disable")
	}
	r3Enabled(t, page, path, "true")

	wantRedirect(t, "enable", r3Post(t, admin, page, path, nil), http.StatusSeeOther, path)
	if got := t13User(t, w, t10Priya); got.Enabled != 1 || got.Name != "Priya Shah" || got.Mobile != mobile {
		t.Errorf("after enable %+v", got)
	}
	page = p6Get(t, admin, path)
	r3Banner(t, "after enable", page, "banner--success", "Priya Shah can sign in again.")
	r3Enabled(t, page, path, "false")
	if got := p1Text(p6Get(t, admin, "/users").Find(`main a[href="` + path + `"]`)); strings.Contains(got, "(disabled)") {
		t.Errorf("enabled row = %q", got)
	}
}

// The list marks a disabled user, as their records do (AUTH-056).
func TestUsers_DisabledMarker(t *testing.T) {
	w, _, admin := p6World(t)
	doc := p6Get(t, admin, "/users")
	if got := p1Text(doc.Find(`main a[href="` + t13UserPath(w, "old@vardhan.test", "") + `"]`)); !strings.Contains(got, "Former Staff (disabled)") {
		t.Errorf("disabled row = %q", got)
	}
	if got := p1Text(doc.Find(`main a[href="` + t13UserPath(w, t10Priya, "") + `"]`)); !strings.Contains(got, "Priya Shah") || strings.Contains(got, "(disabled)") {
		t.Errorf("enabled row = %q", got)
	}
}

// The results never say "can sign in again" while something still stops it:
// Unlock on a disabled user, Enable on a locked one.
func TestUserResults_SayWhatStillBlocks(t *testing.T) {
	w, _, admin := p6World(t)
	path := t13UserPath(w, t10Priya, "")
	r3Lock(t, w, t10Priya)
	doc := p6Get(t, admin, path)
	wantRedirect(t, "disable", r3Post(t, admin, doc, path, nil), http.StatusSeeOther, path)
	admin.Get(path) // shows the disable's flash

	doc = p6Get(t, admin, path)
	wantRedirect(t, "enable while locked", r3Post(t, admin, doc, path, nil), http.StatusSeeOther, path)
	page := p6Get(t, admin, path)
	r3Banner(t, "enable while locked", page, "banner--success", "Priya Shah is enabled, but still locked after too many failed sign-ins. Unlock the account to let them sign in.")

	doc = p6Get(t, admin, path)
	wantRedirect(t, "disable again", r3Post(t, admin, doc, path, nil), http.StatusSeeOther, path)
	admin.Get(path)
	doc = p6Get(t, admin, path)
	wantRedirect(t, "unlock while disabled", r3Post(t, admin, doc, path+"/unlock", nil), http.StatusSeeOther, path)
	r3Banner(t, "unlock while disabled", p6Get(t, admin, path), "banner--success", "Priya Shah is unlocked, but still disabled. Enable the account to let them sign in.")
}

func TestUserDisable_StaleIsTheConflictBanner(t *testing.T) {
	w, _, admin := p6World(t)
	path := t13UserPath(w, t10Priya, "")
	doc := p6Get(t, admin, path)
	posted := r3Version(t, doc, path)
	wantStatus(t, "another save", r3Post(t, admin, doc, path+"/roles", url.Values{"roles": {r3Stores}}), http.StatusSeeOther)
	admin.Get(path) // shows the save's flash

	r3Stale(t, "stale disable", r3Post(t, admin, doc, path, nil), "Priya Shah", path, path, posted)
	if got := t13User(t, w, t10Priya); got.Enabled != 1 {
		t.Error("a stale disable disabled")
	}
}

// AUTH-053: your own page offers no Disable, and a posted one is refused on
// the page. AUTH-057: anyone else gets the owner's text and changes nothing.
func TestUserDisable_Refusals(t *testing.T) {
	w, h, admin := p6World(t)
	own := t13UserPath(w, t13Admin, "")
	doc := p6Get(t, admin, own)
	if doc.Find("dialog#disable-user").Length() != 0 || doc.Find(`input[name=enabled]`).Length() != 0 {
		t.Error("the admin's own page offers Disable")
	}
	r := admin.Post(own, t13With(url.Values{"enabled": {"false"}}, t13DBVersion(t, w, t13Admin)))
	wantStatus(t, "own disable", r, http.StatusBadRequest)
	r3Summary(t, "own disable", p1Doc(t, "own disable", r), auth.ErrCannotDisableSelf.Msg)

	priya := signedIn(t, h, t10Priya)
	other := t13UserPath(w, t10Deepak, "")
	version := t13DBVersion(t, w, t10Deepak)
	r = priya.Post(other, t13With(url.Values{"enabled": {"false"}}, version))
	wantStatus(t, "non-admin disable", r, http.StatusForbidden)
	wantBody(t, "non-admin disable", r, auth.ErrAdminOnlyField.Msg)
	r = priya.Post(t13UserPath(w, t10Priya, ""), t13With(url.Values{"enabled": {"false"}}, t13DBVersion(t, w, t10Priya)))
	wantStatus(t, "non-admin on themselves", r, http.StatusForbidden)
	if got := t13User(t, w, t10Deepak); got.Enabled != 1 || t13DBVersion(t, w, t10Deepak) != version {
		t.Errorf("deepak changed: %+v", got)
	}
}

// A blank name on the save route is refused on the page, the stored name kept.
func TestUserSave_BlankNameRefused(t *testing.T) {
	w, _, admin := p6World(t)
	path := t13UserPath(w, t10Priya, "")
	r := admin.Post(path, t13With(url.Values{"name": {"   "}}, t13DBVersion(t, w, t10Priya)))
	wantStatus(t, "blank name", r, http.StatusBadRequest)
	r3Summary(t, "blank name", p1Doc(t, "blank name", r), auth.ErrNameRequired.Msg)
	if got := t13User(t, w, t10Priya); got.Name != "Priya Shah" {
		t.Errorf("name = %q", got.Name)
	}
}

// Roles save from the form, and the page says so.
func TestUserRoles_Saves(t *testing.T) {
	w, _, admin := p6World(t)
	path := t13UserPath(w, t10Priya, "")
	doc := p6Get(t, admin, path)
	r := r3Post(t, admin, doc, path+"/roles", url.Values{"roles": {r3Stores}})
	wantRedirect(t, "save", r, http.StatusSeeOther, path)
	if got := r3Roles(t, w, t10Priya); !reflect.DeepEqual(got, []string{r3Stores}) {
		t.Errorf("roles = %q", got)
	}
	page := p6Get(t, admin, path)
	r3Banner(t, "after save", page, "banner--success", "Roles for Priya Shah saved.")
	if got := r3Ticked(t, page, path+"/roles"); !reflect.DeepEqual(got, []string{r3Stores}) {
		t.Errorf("ticked = %q", got)
	}
}

// AUTH-052: unticking everything saves, with the warning on the next page.
func TestUserRoles_NoneSavesWithWarning(t *testing.T) {
	w, _, admin := p6World(t)
	path := t13UserPath(w, t10Priya, "")
	doc := p6Get(t, admin, path)
	form := url.Values{"version": {r3Version(t, doc, path+"/roles")}}
	wantRedirect(t, "save none", admin.Post(path+"/roles", form), http.StatusSeeOther, path)
	if got := r3Roles(t, w, t10Priya); len(got) != 0 {
		t.Errorf("roles = %q, want none", got)
	}
	page := p6Get(t, admin, path)
	if got := p1Text(page.Find("#alerts .banner--warning")); !strings.Contains(got, fmt.Sprintf(t13NoRolesWarning, "Priya Shah")) {
		t.Errorf("alerts = %q", got)
	}
}

// AUTH-053: removing your own Administrator role is refused on the page, the
// summary with the owner's text, the ticks as posted.
func TestUserRoles_OwnAdminRefusedOnThePage(t *testing.T) {
	w, _, admin := p6World(t)
	path := t13UserPath(w, t13Admin, "")
	doc := p6Get(t, admin, path)
	r := r3Post(t, admin, doc, path+"/roles", url.Values{"roles": {r3Stores}})
	wantStatus(t, "own role", r, http.StatusBadRequest)
	page := p1Doc(t, "own role", r)
	r3Summary(t, "own role", page, auth.ErrCannotDemoteSelf.Msg)
	r3NoBanner(t, "own role", page, "banner--warning")
	if got := r3Ticked(t, page, path+"/roles"); !reflect.DeepEqual(got, []string{r3Stores}) {
		t.Errorf("ticked = %q, want the posted Stores", got)
	}
	if got := r3Roles(t, w, t13Admin); !reflect.DeepEqual(got, []string{auth.RoleAdministrator}) {
		t.Errorf("roles = %q", got)
	}
	if got := page.Find("title").Text(); !strings.HasPrefix(got, "Error: ") {
		t.Errorf("title = %q, want the Error: prefix", got)
	}
}

// r3Stale asserts a stale write's redraw: ConflictBanner with the owner's
// text, no danger, Reload to the user, the posted version kept (CD-94, CD-381).
func r3Stale(t *testing.T, what string, r webtest.Response, name, path, action, posted string) *goquery.Document {
	t.Helper()
	wantStatus(t, what, r, http.StatusConflict)
	page := p1Doc(t, what, r)
	r3Banner(t, what, page, "banner--warning", t13Changed(name))
	if n := page.Find(".error-summary").Length(); n != 0 {
		t.Errorf("%s: %d error summaries on a stale version", what, n)
	}
	a := page.Find("main .banner--warning a.btn")
	if got := p1Text(a); got != "Reload "+name || a.AttrOr("href", "") != path {
		t.Errorf("%s: Reload = %q -> %q", what, got, a.AttrOr("href", ""))
	}
	if got := r3Version(t, page, action); got != posted {
		t.Errorf("%s: form posts version %q, want the page's %q", what, got, posted)
	}
	return page
}

func TestUserRoles_StaleIsTheConflictBanner(t *testing.T) {
	w, _, admin := p6World(t)
	path := t13UserPath(w, t10Priya, "")
	doc := p6Get(t, admin, path)
	posted := r3Version(t, doc, path+"/roles")
	first := r3Post(t, admin, doc, path+"/roles", url.Values{"roles": {r3Employee}})
	wantStatus(t, "first save", first, http.StatusSeeOther)
	admin.Get(path) // shows the save's flash

	r := r3Post(t, admin, doc, path+"/roles", url.Values{"roles": {r3Stores}})
	page := r3Stale(t, "stale roles", r, "Priya Shah", path, path+"/roles", posted)
	if got := r3Ticked(t, page, path+"/roles"); !reflect.DeepEqual(got, []string{r3Stores}) {
		t.Errorf("ticked = %q, want the posted Stores", got)
	}
	if got := r3Roles(t, w, t10Priya); !reflect.DeepEqual(got, []string{r3Employee}) {
		t.Errorf("roles = %q, want the first save's", got)
	}
}

func TestUserDelete_StaleIsTheConflictBanner(t *testing.T) {
	w, _, admin := p6World(t)
	w.AddUser(t, r3Kiran, "Kiran Joshi", r3Employee)
	path := t13UserPath(w, r3Kiran, "")
	doc := p6Get(t, admin, path)
	posted := r3Version(t, doc, path+"/delete")
	wantStatus(t, "another save", r3Post(t, admin, doc, path+"/roles", url.Values{"roles": {r3Stores}}), http.StatusSeeOther)
	admin.Get(path) // shows the save's flash

	r := r3Post(t, admin, doc, path+"/delete", nil)
	page := r3Stale(t, "stale delete", r, "Kiran Joshi", path, path+"/delete", posted)
	if got := r3Ticked(t, page, path+"/roles"); !reflect.DeepEqual(got, []string{r3Stores}) {
		t.Errorf("ticked = %q, want the stored Stores", got)
	}
	if !t13UserExists(t, w, r3Kiran) {
		t.Error("a stale delete deleted")
	}
}

// AUTH-055: a user with nothing against them goes, and the list says so. The
// confirm is the only form that posts the delete.
func TestUserDelete_FromTheConfirm(t *testing.T) {
	w, _, admin := p6World(t)
	w.AddUser(t, r3Kiran, "Kiran Joshi", r3Employee)
	path := t13UserPath(w, r3Kiran, "")
	doc := p6Get(t, admin, path)
	dlg := doc.Find("dialog#delete-user")
	if got := p1Text(dlg.Find("h2")); got != "Delete Kiran Joshi?" {
		t.Errorf("confirm title = %q", got)
	}
	if dlg.Find(`form[action="`+path+`/delete"]`).Length() != 1 {
		t.Error("the delete form isn't in the confirm")
	}
	opener := doc.Find(`main button[data-open-dialog="delete-user"]`)
	if opener.AttrOr("type", "") != "button" || opener.Closest("form").Length() != 0 {
		t.Errorf("the Delete button can post without the confirm: %v", opener.Nodes)
	}
	r := r3Post(t, admin, doc, path+"/delete", nil)
	wantRedirect(t, "delete", r, http.StatusSeeOther, "/users")
	if t13UserExists(t, w, r3Kiran) {
		t.Error("user still there")
	}
	r3Banner(t, "after delete", p6Get(t, admin, "/users"), "banner--success", "Kiran Joshi was deleted.")
}

// AUTH-055: a user who has signed in stays, with the owner's text on the page.
func TestUserDelete_ReferencedRefusedOnThePage(t *testing.T) {
	w, h, admin := p6World(t)
	signedIn(t, h, t10Priya)
	path := t13UserPath(w, t10Priya, "")
	doc := p6Get(t, admin, path)
	r := r3Post(t, admin, doc, path+"/delete", nil)
	wantStatus(t, "delete", r, http.StatusConflict)
	page := p1Doc(t, "delete", r)
	r3Summary(t, "delete", page, auth.ReferencedUserError("Priya Shah").Msg)
	r3NoBanner(t, "delete", page, "banner--warning")
	if !t13UserExists(t, w, t10Priya) {
		t.Error("user deleted")
	}
}

// AUTH-054: the only Administrator can't delete themselves (their page offers
// no Delete; a posted one is refused), and the page says why.
func TestUserDelete_LastAdminRefusedOnThePage(t *testing.T) {
	w, _, admin := p6World(t)
	path := t13UserPath(w, t13Admin, "")
	r := admin.Post(path+"/delete", t13With(url.Values{}, t13DBVersion(t, w, t13Admin)))
	wantStatus(t, "delete", r, http.StatusConflict)
	page := p1Doc(t, "delete", r)
	r3Summary(t, "delete", page, auth.ErrLastAdmin.Msg)
	r3NoBanner(t, "delete", page, "banner--warning")
	if !t13UserExists(t, w, t13Admin) {
		t.Error("the last admin is gone")
	}
}

// A write to a user deleted meanwhile answers not found, on every form.
func TestUserWrites_DeletedUserIsNotFound(t *testing.T) {
	w, _, admin := p6World(t)
	w.AddUser(t, r3Kiran, "Kiran Joshi", r3Employee)
	path := t13UserPath(w, r3Kiran, "")
	doc := p6Get(t, admin, path)
	wantStatus(t, "delete", r3Post(t, admin, doc, path+"/delete", nil), http.StatusSeeOther)
	for _, action := range []string{"/roles", "/delete"} {
		r := r3Post(t, admin, doc, path+action, nil)
		wantStatus(t, action, r, http.StatusNotFound)
		wantBody(t, action, r, auth.ErrNotFound.Error())
		if p1Doc(t, action, r).Find("main").Length() != 0 {
			t.Errorf("%s drew a page for a user who is gone", action)
		}
	}
	r := r3Post(t, admin, doc, path, nil) // Disable: version and enabled
	wantStatus(t, "disable", r, http.StatusNotFound)
	wantBody(t, "disable", r, auth.ErrNotFound.Error())
	if p1Doc(t, "disable", r).Find("main").Length() != 0 {
		t.Error("disable drew a page for a user who is gone")
	}
	for _, action := range []string{"/password", "/unlock"} {
		wantStatus(t, action, admin.Post(path+action, url.Values{"password": {r3Temp}}), http.StatusNotFound)
	}
	wantStatus(t, "reset form", admin.Get(path+"/password"), http.StatusNotFound)
	wantStatus(t, "bad id", admin.Get("/users/abc/password"), http.StatusNotFound)
}

// AUTH-033: the reset form names who it hits, and a good password takes
// effect with the result on the user's page.
func TestResetPassword_FromTheForm(t *testing.T) {
	w, h, admin := p6World(t)
	suresh := signedIn(t, h, t13Suresh)
	path := t13UserPath(w, t13Suresh, "/password")
	doc := p6Get(t, admin, path)
	main := p1Text(doc.Find("main"))
	for _, want := range []string{"Suresh", t13Suresh, "signs them out everywhere", "must choose a new password", "are required."} {
		if !strings.Contains(main, want) {
			t.Errorf("reset page lacks %q: %q", want, main)
		}
	}
	if doc.Find(`main form .card input[name=password][aria-required=true]`).Length() != 1 {
		t.Error("the password field isn't a required field in a card")
	}
	if got := doc.Find(`input[name=password]`).AttrOr("autocomplete", ""); got != "new-password" {
		t.Errorf("autocomplete = %q", got)
	}
	r := r3Post(t, admin, doc, path, url.Values{"password": {r3Temp}})
	wantRedirect(t, "reset", r, http.StatusSeeOther, t13UserPath(w, t13Suresh, ""))
	page := p6Get(t, admin, r.Header.Get("Location"))
	if b := page.Find("main .banner--success"); !strings.Contains(p1Text(b), "Password reset for Suresh Patil. They choose a new one when they next sign in.") {
		t.Errorf("result = %q", p1Text(b))
	}
	if got := suresh.Get("/"); got.Status == http.StatusOK {
		t.Error("suresh's session survived the reset")
	}
	wantBody(t, "sign in with the temporary password", h.Client().Login(t13Suresh, r3Temp), "SIGNED IN")
}

// A policy refusal stays on the form, on the field, with the password gone;
// each policy text is the owner's.
func TestResetPassword_PolicyRefusedOnTheForm(t *testing.T) {
	for _, tc := range []struct{ password, text string }{
		{"short-1", auth.ErrPasswordTooShort.Msg},
		{strings.Repeat("x7", 129), auth.ErrPasswordTooLong.Msg},
		{"password123", auth.ErrPasswordCommon.Msg},
		{"suresh-long-enough-1", auth.ErrPasswordHasName.Msg},
	} {
		t.Run(tc.text, func(t *testing.T) {
			w, _, admin := p6World(t)
			path := t13UserPath(w, t13Suresh, "/password")
			r := r3Post(t, admin, p6Get(t, admin, path), path, url.Values{"password": {tc.password}})
			wantStatus(t, "reset", r, http.StatusBadRequest)
			if strings.Contains(r.Body, tc.password) {
				t.Error("the page echoes the password")
			}
			page := p1Doc(t, "reset", r)
			if got := p1Text(page.Find(".error-summary li a")); got != tc.text {
				t.Errorf("summary = %q", got)
			}
			if got := page.Find(".error-summary li a").AttrOr("href", ""); got != "#f-password" {
				t.Errorf("summary link = %q, want #f-password", got)
			}
			if got := p1Text(page.Find("#f-password-msg")); !strings.Contains(got, tc.text) {
				t.Errorf("field message = %q", got)
			}
			if got := page.Find(`input[name=password]`).AttrOr("value", ""); got != "" {
				t.Errorf("field keeps %q", got)
			}
		})
	}
}

// r3Lock fails email's sign-in until the account locks.
func r3Lock(t *testing.T, w *authtest.World, email string) {
	t.Helper()
	w.SetPassword(t, email, t10Password)
	for range auth.AccountLimit {
		if _, err := w.Auth.SignIn(context.Background(), auth.SignInInput{Email: email, Password: t11Wrong, IP: "198.51.100.4", UserAgent: "r3"}); err == nil {
			t.Fatal("a wrong password signed in")
		}
	}
}

// AUTH-016: a locked account shows Unlock, and after it the page and sign-in
// agree. One failure short of the limit shows nothing to unlock.
func TestUnlock_FromTheForm(t *testing.T) {
	w, h, admin := p6World(t)
	path := t13UserPath(w, t10Priya, "")
	w.SetPassword(t, t10Priya, t10Password)
	for range auth.AccountLimit - 1 {
		if _, err := w.Auth.SignIn(context.Background(), auth.SignInInput{Email: t10Priya, Password: t11Wrong, IP: "198.51.100.4", UserAgent: "r3"}); err == nil {
			t.Fatal("a wrong password signed in")
		}
	}
	doc := p6Get(t, admin, path)
	if n := doc.Find(`form[action="` + path + `/unlock"]`).Length(); n != 0 {
		t.Errorf("%d unlock forms on an unlocked account", n)
	}
	if got := p1Text(doc.Find("main")); !strings.Contains(got, "This account isn't locked.") {
		t.Errorf("page = %q", got)
	}

	r3Lock(t, w, t10Priya)
	doc = p6Get(t, admin, path)
	if got := p1Text(doc.Find("main")); !strings.Contains(got, "This account is locked after too many failed sign-ins.") {
		t.Errorf("page = %q", got)
	}
	r := r3Post(t, admin, doc, path+"/unlock", nil)
	wantRedirect(t, "unlock", r, http.StatusSeeOther, path)
	page := p6Get(t, admin, path)
	r3Banner(t, "after unlock", page, "banner--success", "Priya Shah can sign in again.")
	if page.Find(`form[action="`+path+`/unlock"]`).Length() != 0 {
		t.Error("still offers Unlock")
	}
	wantBody(t, "sign in", h.Client().Login(t10Priya, t10Password), "SIGNED IN")
}

// No password typed on these forms reaches a log line, a table, the audit
// trail or a page, on success or refusal.
func TestUserAdmin_PasswordsStayOut(t *testing.T) {
	logs := t16CaptureLogs(t)
	w, _, admin := p6World(t)
	const created, refused, reset = "Created-Secret-7741", "1234", "Reset-Secret-Ledger-5"
	bodies := []webtest.Response{
		r3Post(t, admin, p6Get(t, admin, "/users/new"), "/users", url.Values{"name": {"K"}, "email": {r3Kiran}, "password": {created}}),
		r3Post(t, admin, p6Get(t, admin, "/users/new"), "/users", url.Values{"name": {"K"}, "email": {r3Kiran}, "password": {created}}),
		r3Post(t, admin, p6Get(t, admin, "/users/new"), "/users", url.Values{"name": {"K"}, "email": {"new@vardhan.test"}, "password": {refused + refused}}),
	}
	path := t13UserPath(w, t13Suresh, "/password")
	bodies = append(bodies,
		r3Post(t, admin, p6Get(t, admin, path), path, url.Values{"password": {refused + refused}}),
		r3Post(t, admin, p6Get(t, admin, path), path, url.Values{"password": {reset}}))
	wantStatus(t, "created", bodies[0], http.StatusSeeOther)
	wantStatus(t, "taken", bodies[1], http.StatusConflict)
	wantStatus(t, "reset", bodies[4], http.StatusSeeOther)
	for i, r := range bodies {
		for _, pw := range []string{created, reset, refused + refused} {
			if strings.Contains(r.Body, pw) {
				t.Errorf("response %d holds a password", i)
			}
		}
	}
	for _, pw := range []string{created, reset, refused + refused} {
		if strings.Contains(logs.String(), pw) {
			t.Errorf("logs hold %q", pw)
		}
		if got := t16Holding(t, w, t16TextColumns(t, w), pw); len(got) != 0 {
			t.Errorf("columns %q hold %q", got, pw)
		}
		if got := fmt.Sprintf("%+v", w.Audit.Entries()); strings.Contains(got, pw) {
			t.Errorf("audit holds %q", pw)
		}
	}
}

// The admin's own page offers no Delete, as it offers no Disable or Reset.
func TestUserPage_OwnPageHasNoDelete(t *testing.T) {
	w, _, admin := p6World(t)
	path := t13UserPath(w, t13Admin, "")
	doc := p6Get(t, admin, path)
	if n := doc.Find(`dialog#delete-user, form[action="` + path + `/delete"], button[data-open-dialog="delete-user"]`).Length(); n != 0 {
		t.Errorf("%d delete controls on the admin's own page", n)
	}
	if n := p6Get(t, admin, t13UserPath(w, t10Priya, "")).Find(`dialog#delete-user form[action$="/delete"]`).Length(); n != 1 {
		t.Errorf("someone else's page has %d delete forms, want 1", n)
	}
}

// A non-admin posting a field only an administrator may change gets the plain
// 403, not their page redrawn round it.
func TestUserSave_ForbiddenIsPlain(t *testing.T) {
	w, h, _ := p6World(t)
	priya := signedIn(t, h, t10Priya)
	r := priya.Post(t13UserPath(w, t10Priya, ""), t13With(url.Values{"enabled": {"false"}}, t13DBVersion(t, w, t10Priya)))
	wantStatus(t, "own enabled", r, http.StatusForbidden)
	wantBody(t, "own enabled", r, auth.ErrAdminOnlyField.Msg)
	if p1Doc(t, "own enabled", r).Find("main").Length() != 0 {
		t.Error("a 403 redrew the page")
	}
}

// r3Break renames table so the next read of it fails, as a broken database
// would, and returns the undo. The writes these tests make don't touch it, so
// only the read after the save fails (lessons §10: fail the service, not the
// request context).
func r3Break(t *testing.T, w *authtest.World, table string) func() {
	t.Helper()
	exec := func(q string) {
		t.Helper()
		if err := w.DB.Write(context.Background(), func(tx *db.Tx) error {
			_, err := tx.SQL().ExecContext(context.Background(), q)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	exec(`ALTER TABLE ` + table + ` RENAME TO r3_broken`)
	return func() { exec(`ALTER TABLE r3_broken RENAME TO ` + table) }
}

// After a save that worked, a failed read of the user's name still redirects,
// with the result text minus the name; the save stands.
func TestUserWrites_RereadFailureStillRedirects(t *testing.T) {
	cases := []struct {
		name, table, action, want string
		form                      func(t *testing.T, w *authtest.World, doc *goquery.Document, path string) url.Values
		check                     func(t *testing.T, w *authtest.World)
	}{
		{"roles", "user_restrictions", "/roles", "Roles saved.",
			func(t *testing.T, _ *authtest.World, doc *goquery.Document, path string) url.Values {
				return url.Values{"version": {r3Version(t, doc, path+"/roles")}, "roles": {r3Stores}}
			},
			func(t *testing.T, w *authtest.World) {
				if got := r3Roles(t, w, t10Priya); !reflect.DeepEqual(got, []string{r3Stores}) {
					t.Errorf("roles = %q", got)
				}
			}},
		{"disable", "login_attempts", "", "The user is disabled and signed out everywhere.",
			func(t *testing.T, _ *authtest.World, doc *goquery.Document, path string) url.Values {
				return url.Values{"version": {r3Version(t, doc, path)}, "enabled": {"false"}}
			},
			func(t *testing.T, w *authtest.World) {
				if t13User(t, w, t10Priya).Enabled != 0 {
					t.Error("not disabled")
				}
			}},
		{"enable", "login_attempts", "", "The user is enabled.",
			func(t *testing.T, w *authtest.World, _ *goquery.Document, _ string) url.Values {
				if err := w.DB.Write(context.Background(), func(tx *db.Tx) error {
					_, err := tx.SQL().ExecContext(context.Background(), `UPDATE users SET enabled = 0, version = version + 1 WHERE email = ?`, t10Priya)
					return err
				}); err != nil {
					t.Fatal(err)
				}
				return url.Values{"version": {t13DBVersion(t, w, t10Priya)}, "enabled": {"true"}}
			},
			func(t *testing.T, w *authtest.World) {
				if t13User(t, w, t10Priya).Enabled != 1 {
					t.Error("not enabled")
				}
			}},
		{"reset", "user_restrictions", "/password", "Password reset. They choose a new one when they next sign in.",
			func(*testing.T, *authtest.World, *goquery.Document, string) url.Values {
				return url.Values{"password": {r3Temp}}
			},
			func(t *testing.T, w *authtest.World) {
				var must int
				t13Read(t, w, "must change", func(ctx context.Context, q db.DBTX) error {
					return q.QueryRowContext(ctx, `SELECT must_change_password FROM users WHERE email = ?`, t10Priya).Scan(&must)
				})
				if must != 1 {
					t.Error("password not reset")
				}
			}},
		{"unlock", "user_restrictions", "/unlock", "The account is unlocked.",
			func(*testing.T, *authtest.World, *goquery.Document, string) url.Values { return url.Values{} },
			func(t *testing.T, w *authtest.World) {
				if n := t11Int(t, w, `SELECT count(*) FROM login_attempts WHERE key = ?`, auth.AccountKey(t10Priya)); n != 0 {
					t.Error("still locked")
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _, admin := p6World(t)
			r3Lock(t, w, t10Priya)
			path := t13UserPath(w, t10Priya, "")
			doc := p6Get(t, admin, path)
			form := tc.form(t, w, doc, path)
			undo := r3Break(t, w, tc.table)
			r := admin.Post(path+tc.action, form)
			undo()
			wantRedirect(t, tc.name, r, http.StatusSeeOther, path)
			tc.check(t, w)
			r3Banner(t, tc.name, p6Get(t, admin, path), "banner--success", tc.want)
		})
	}
}
