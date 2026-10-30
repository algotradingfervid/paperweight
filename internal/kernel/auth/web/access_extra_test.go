package authweb_test

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/views"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// Task 20: the access page takes the flash once; it is gone on the next GET
// (the page is rendered before the session is saved, CD-160).
func TestAccessEditor_FlashShownOnce(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	admin := signedIn(t, h, t13Admin)
	page := t13UserPath(w, t13Suresh, "/access")
	const flash = "Site and project access for Suresh Patil saved."

	r := admin.Post(t13UserPath(w, t13Suresh, "/restrictions"), t13With(url.Values{
		"dimension": {"site"}, "add": {strconv.FormatInt(w.Sites["TECH-SITE"], 10)},
	}, t13DBVersion(t, w, t13Suresh)))
	wantRedirect(t, "admin adds TECH-SITE", r, http.StatusSeeOther, page)
	if body := t20Page(t, admin, "first GET "+page, page); !strings.Contains(body, flash) {
		t.Fatalf("first GET %s: body lacks the flash %q: %q", page, flash, body)
	}
	if body := t20Page(t, admin, "second GET "+page, page); strings.Contains(body, flash) {
		t.Fatalf("second GET %s: the flash %q is shown again: %q", page, flash, body)
	}
}

// Task 20: an Administrator's view of a user links the access page; a user
// viewing themselves (who can't open it) gets no link.
func TestUserPage_AccessLink(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	path := t13UserPath(w, t13Suresh, "")
	link := `href="` + t13UserPath(w, t13Suresh, "/access") + `"`

	r := signedIn(t, h, t13Admin).Get(path)
	wantStatus(t, "admin GET "+path, r, http.StatusOK)
	if !strings.Contains(r.Body, link) || !strings.Contains(r.Body, "Site and project access") {
		t.Fatalf("admin GET %s: body lacks the \"Site and project access\" link %s: %q", path, link, r.Body)
	}
	r = signedIn(t, h, t13Suresh).Get(path)
	wantStatus(t, "suresh GET "+path, r, http.StatusOK)
	if strings.Contains(r.Body, link) {
		t.Fatalf("suresh GET %s: body links the access page, which only an administrator may open: %q", path, r.Body)
	}
}

// t20Bare serves /bare/... from a second Server, behind the harness's session
// middleware. cfg gets a func that cancels the current request's context.
func t20Bare(t *testing.T, w *authtest.World, cfg func(cancel func()) authweb.Config) *webtest.Harness {
	t.Helper()
	var bare http.Handler
	var cancel context.CancelFunc
	h := webtest.New(t, w, webtest.WithRoutes(func(r chi.Router, _ *authweb.Server) {
		r.Get("/bare/users/{id}/access", func(rw http.ResponseWriter, req *http.Request) {
			ctx, stop := context.WithCancel(context.WithValue(req.Context(), chi.RouteCtxKey, nil))
			defer stop()
			cancel = stop
			req = req.WithContext(ctx)
			req.URL.Path = strings.TrimPrefix(req.URL.Path, "/bare")
			bare.ServeHTTP(rw, req)
		})
	}))
	srv := authweb.New(w.Auth, auth.NewSessionStore(w.DB, w.Clock, auth.DefaultSessionConfig(false)), h.SM,
		views.Default{}, cfg(func() { cancel() }))
	mux := chi.NewRouter()
	srv.Mount(mux)
	bare = mux
	return h
}

// Task 20: a Server whose Config has no PickerURL answers the access page
// with a plain internal error, not a panic.
func TestAccessEditor_NoPickerURL(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t20Bare(t, w, func(func()) authweb.Config { return authweb.Config{} })

	path := "/bare" + t13UserPath(w, t13Suresh, "/access")
	r := signedIn(t, h, t13Admin).Get(path)
	wantStatus(t, "admin GET "+path+" without Config.PickerURL", r, http.StatusInternalServerError)
	if !strings.Contains(r.Body, t10InternalMsg) {
		t.Fatalf("admin GET %s: body lacks the internal error message: %q", path, r.Body)
	}
}

// CD-160: a page that fails to render is a 500 with the plain error page, not
// a 200. PickerURL cancels the request, so the render that follows fails.
func TestAccessEditor_RenderError(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t20Bare(t, w, func(cancel func()) authweb.Config {
		return authweb.Config{PickerURL: func(*doctype.DocType) string {
			cancel()
			return "/picker"
		}}
	})

	path := "/bare" + t13UserPath(w, t13Suresh, "/access")
	r := signedIn(t, h, t13Admin).Get(path)
	wantStatus(t, "admin GET "+path+" with a failing render", r, http.StatusInternalServerError)
	if !strings.Contains(r.Body, t10InternalMsg) {
		t.Fatalf("admin GET %s: body lacks the internal error message: %q", path, r.Body)
	}
}

// Task 20: each section's Add button is named by its add text, as Remove
// is named by its value.
func TestAccessEditor_AddButtonNames(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	page := t13UserPath(w, t13Suresh, "/access")
	post := t13UserPath(w, t13Suresh, "/restrictions")
	body := t20Page(t, signedIn(t, h, t13Admin), "admin GET "+page, page)
	for dim, name := range map[auth.Dimension]string{auth.DimSite: "Add a site", auth.DimProject: "Add a project"} {
		adds := t20RestrictionForms(body, post, dim, "add")
		if len(adds) != 1 {
			t.Fatalf("admin GET %s: %d %s add forms, want 1", page, len(adds), dim)
		}
		var submit map[string]string
		for _, b := range regexp.MustCompile(`(?is)<button\b[^>]*>`).FindAllString(adds[0].Inner, -1) {
			if a := t20Attrs(b); a["type"] == "submit" {
				submit = a
			}
		}
		if got := submit["aria-label"]; got != name {
			t.Errorf("admin GET %s: the %s Add button's aria-label = %q, want %q: %q", page, dim, got, name, adds[0].Inner)
		}
	}
}

// Task 20: the access page of a user who doesn't exist, or of an id that isn't
// a number, is the app's 404 page, not the router's.
func TestAccessEditor_UnknownUser(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	admin := signedIn(t, h, t13Admin)
	for _, path := range []string{"/users/9999/access", "/users/abc/access"} {
		r := admin.Get(path)
		wantStatus(t, "admin GET "+path, r, http.StatusNotFound)
		wantBody(t, "admin GET "+path, r, auth.MsgNotFound)
	}
}

// Task 20: the Back link goes to the user's page and carries their name.
func TestAccessEditor_BackLink(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	page := t13UserPath(w, t13Suresh, "/access")
	body := t20Page(t, signedIn(t, h, t13Admin), "admin GET "+page, page)
	back := t20cDoc(t, body).Find("a.back-link")
	if got, want := back.AttrOr("href", ""), t13UserPath(w, t13Suresh, ""); got != want {
		t.Fatalf("admin GET %s: the Back link's href = %q, want %q: %q", page, got, want, body)
	}
	if got := strings.TrimSpace(back.Text()); got != "Suresh Patil" {
		t.Fatalf("admin GET %s: the Back link's text = %q, want \"Suresh Patil\"", page, got)
	}
}

// t20cDoc parses a page (CD-148).
func t20cDoc(t *testing.T, body string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse page: %v", err)
	}
	return doc
}

// t20cWantError checks the page shows msg once, in the summary and on dim's
// picker, and nothing on other's picker.
func t20cWantError(t *testing.T, what, body string, dim, other auth.Dimension, msg string) {
	t.Helper()
	doc := t20cDoc(t, body)
	links := doc.Find(".error-summary a")
	if links.Length() != 1 || links.Text() != msg {
		t.Fatalf("%s: summary links %q (%d), want one %q: %q", what, links.Text(), links.Length(), msg, body)
	}
	id := "f-add-" + string(dim)
	if got := links.AttrOr("href", ""); got != "#"+id {
		t.Fatalf("%s: the summary links %q, want #%s", what, got, id)
	}
	if got := doc.Find("input#"+id).AttrOr("aria-invalid", ""); got != "true" {
		t.Fatalf("%s: picker %s has aria-invalid = %q, want true: %q", what, id, got, body)
	}
	if got := strings.TrimSpace(doc.Find("#" + id + "-msg").Text()); got != msg {
		t.Fatalf("%s: picker %s shows %q, want %q", what, id, got, msg)
	}
	otherID := "f-add-" + string(other)
	if got := strings.TrimSpace(doc.Find("#" + otherID + "-msg").Text()); got != "" {
		t.Fatalf("%s: picker %s shows %q, want nothing", what, otherID, got)
	}
	if _, invalid := doc.Find("input#" + otherID).Attr("aria-invalid"); invalid {
		t.Fatalf("%s: picker %s is marked invalid: %q", what, otherID, body)
	}
	if got := doc.Find("title").Text(); !strings.HasPrefix(got, "Error: ") {
		t.Fatalf("%s: <title> = %q, want the error prefix", what, got)
	}
}

// CD-194: the page's Add form posted with nothing picked is a field error on
// that section: a 422 page, no write, no audit entry, no flash.
func TestAccessEditor_EmptyAdd(t *testing.T) {
	for _, tc := range []struct {
		dim, other auth.Dimension
		msg        string
	}{
		{auth.DimSite, auth.DimProject, "Choose a site."},
		{auth.DimProject, auth.DimSite, "Choose a project."},
	} {
		t.Run(string(tc.dim), func(t *testing.T) {
			w := authtest.NewWorld(t)
			h := t14Probe(t, w)
			admin := signedIn(t, h, t13Admin)
			page := t13UserPath(w, t13Suresh, "/access")
			post := t13UserPath(w, t13Suresh, "/restrictions")
			what := "admin posts an empty Add for " + string(tc.dim)

			body := t20Page(t, admin, "admin GET "+page, page)
			adds := t20RestrictionForms(body, post, tc.dim, "add")
			if len(adds) != 1 || adds[0].Fields.Get("add") != "" {
				t.Fatalf("admin GET %s: %s add forms = %v, want one with add empty", page, tc.dim, adds)
			}
			before, err := w.Auth.UserRestrictions(context.Background(), w.Users[t13Suresh])
			if err != nil {
				t.Fatal(err)
			}
			version, entries := t13DBVersion(t, w, t13Suresh), len(w.Audit.Entries())

			r := admin.Post(post, adds[0].Fields)
			wantStatus(t, what, r, http.StatusUnprocessableEntity)
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("%s: Content-Type = %q, want the page", what, ct)
			}
			t20cWantError(t, what, r.Body, tc.dim, tc.other, tc.msg)
			if got, _ := t20Removes(t, w, what, r.Body, post); !reflect.DeepEqual(got, []string{"METRO-SITE", "PUNE-WH"}) {
				t.Fatalf("%s: sites with Remove = %q, want [METRO-SITE PUNE-WH]", what, got)
			}

			after, err := w.Auth.UserRestrictions(context.Background(), w.Users[t13Suresh])
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("%s: restrictions = %v, want them unchanged: %v", what, after, before)
			}
			if got := t13DBVersion(t, w, t13Suresh); got != version {
				t.Fatalf("%s: users.version = %s, want it unchanged: %s", what, got, version)
			}
			if n := len(w.Audit.Entries()); n != entries {
				t.Fatalf("%s: %d audit entries, want %d", what, n, entries)
			}
			const flash = "Site and project access for Suresh Patil saved."
			if strings.Contains(r.Body, flash) {
				t.Fatalf("%s: the page shows the flash: %q", what, r.Body)
			}
			if body := t20Page(t, admin, "admin GET "+page+" after", page); strings.Contains(body, flash) {
				t.Fatalf("%s: the next page shows the flash: %q", what, body)
			}
		})
	}
}

// CD-194, CD-94: the re-rendered forms keep the version the user posted, so
// a change made meanwhile still stops the next save with a 409.
func TestAccessEditor_EmptyAddKeepsPostedVersion(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	admin := signedIn(t, h, t13Admin)
	page := t13UserPath(w, t13Suresh, "/access")
	post := t13UserPath(w, t13Suresh, "/restrictions")

	body := t20Page(t, admin, "admin GET "+page, page)
	adds := t20RestrictionForms(body, post, auth.DimSite, "add")
	if len(adds) != 1 {
		t.Fatalf("admin GET %s: %d site add forms, want 1", page, len(adds))
	}
	posted := adds[0].Fields.Get("version")

	// Another admin's save, after this page was opened.
	r := admin.Post(post, t13With(url.Values{
		"dimension": {"site"}, "add": {strconv.FormatInt(w.Sites["TECH-SITE"], 10)},
	}, posted))
	wantRedirect(t, "the other save", r, http.StatusSeeOther, page)
	t20Page(t, admin, "admin GET "+page+" to take the flash", page)
	fresh := t13DBVersion(t, w, t13Suresh)
	if fresh == posted {
		t.Fatalf("the other save left users.version at %s", fresh)
	}

	r = admin.Post(post, adds[0].Fields)
	wantStatus(t, "empty Add with the old version", r, http.StatusUnprocessableEntity)
	forms := t20Forms(r.Body)
	n := 0
	for _, f := range forms {
		if f.Attrs["action"] != post {
			continue
		}
		n++
		if got := f.Fields.Get("version"); got != posted {
			t.Fatalf("a re-rendered form posts version %q, want the posted %q (users.version is %s): %q", got, posted, fresh, f.Inner)
		}
	}
	// One Add form per section and one Remove form per site (three now).
	if n != 5 {
		t.Fatalf("re-rendered page has %d restriction forms, want 5: %q", n, r.Body)
	}

	again := t20RestrictionForms(r.Body, post, auth.DimSite, "add")[0].Fields
	again.Set("add", strconv.FormatInt(w.Sites["METRO-SITE"], 10))
	r = admin.Post(post, again)
	wantStatus(t, "the next save from the re-rendered page", r, http.StatusConflict)
	wantBody(t, "the next save from the re-rendered page", r, "Someone else changed Suresh Patil after you opened it.")
}

// CD-194: the empty-Add page is refused like the page itself.
func TestAccessEditor_EmptyAddRefusals(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	form := func(version string) url.Values {
		return t13With(url.Values{"dimension": {"site"}, "add": {""}}, version)
	}

	post := t13UserPath(w, t13Suresh, "/restrictions")
	r := signedIn(t, h, t13Suresh).Post(post, form(t13DBVersion(t, w, t13Suresh)))
	wantStatus(t, "suresh posts an empty Add", r, http.StatusForbidden)
	wantBody(t, "suresh posts an empty Add", r, t13AdminOnlyAccess)

	const unknown = "/users/9999/restrictions"
	r = signedIn(t, h, t13Admin).Post(unknown, form("1"))
	wantStatus(t, "admin posts an empty Add for an unknown user", r, http.StatusNotFound)
	wantBody(t, "admin posts an empty Add for an unknown user", r, auth.MsgNotFound)
}

// CD-194: only a form with a blank add and nothing to remove, on a real
// dimension, is the field error. Everything else answers as before. The
// page's forms post add or remove, never both, so a blank add with a real
// remove can't come from the page: it stays formIDs' 400 and removes nothing.
func TestAccessEditor_EmptyAddOnlyThatForm(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	admin := signedIn(t, h, t13Admin)
	page := t13UserPath(w, t13Suresh, "/access")
	post := t13UserPath(w, t13Suresh, "/restrictions")
	metro := strconv.FormatInt(w.Sites["METRO-SITE"], 10)
	tech := strconv.FormatInt(w.Sites["TECH-SITE"], 10)
	before, err := w.Auth.UserRestrictions(context.Background(), w.Users[t13Suresh])
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		form   url.Values
		status int
	}{
		{"add not a number", url.Values{"dimension": {"site"}, "add": {"abc"}}, http.StatusBadRequest},
		{"remove not a number", url.Values{"dimension": {"site"}, "add": {""}, "remove": {"abc"}}, http.StatusBadRequest},
		{"unknown dimension", url.Values{"dimension": {"team"}, "add": {""}}, http.StatusBadRequest},
		{"no dimension", url.Values{"add": {""}}, http.StatusBadRequest},
		{"blank add, real remove", url.Values{"dimension": {"site"}, "add": {""}, "remove": {metro}}, http.StatusBadRequest},
		{"one blank add of two", url.Values{"dimension": {"site"}, "add": {"", tech}}, http.StatusBadRequest},
		{"blank add, blank remove", url.Values{"dimension": {"site"}, "add": {""}, "remove": {""}}, http.StatusUnprocessableEntity},
		{"two blank adds", url.Values{"dimension": {"site"}, "add": {"", ""}}, http.StatusUnprocessableEntity},
	} {
		what := "POST " + tc.name
		r := admin.Post(post, t13With(tc.form, t13DBVersion(t, w, t13Suresh)))
		wantStatus(t, what, r, tc.status)
		if tc.status == http.StatusBadRequest {
			wantBody(t, what, r, auth.ErrBadForm.Msg)
		}
	}
	after, err := w.Auth.UserRestrictions(context.Background(), w.Users[t13Suresh])
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("restrictions = %v, want them unchanged: %v", after, before)
	}

	// No add and no remove stays the no-change save it was.
	r := admin.Post(post, t13With(url.Values{"dimension": {"site"}}, t13DBVersion(t, w, t13Suresh)))
	wantRedirect(t, "POST without add or remove", r, http.StatusSeeOther, page)
}
