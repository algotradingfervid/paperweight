package modkitweb_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	modkitweb "github.com/vardhan/internaltools/internal/modules/modkit/web"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// PO spec §22's route, POST /modules/comments/{doctype}/{id}, on the real
// auth.Service and document engine. AUTH's guard (signed in, CSRF) is
// internal/app's; TestApp_ModkitRoutesMounted covers it there.

const (
	msgCommentEmpty   = "Write a comment first."
	msgCommentTooLong = "A comment can be at most 1,000 characters."

	namePriya = "Priya Shah"
	nameRavi  = "Ravi Kulkarni"

	// bodyField is the comment box's form field.
	bodyField = "body"
)

func TestMain(m *testing.M) {
	modkit.RegisterComments(fixture.Order)
	os.Exit(m.Run())
}

// webEnv is the fixture harness (its engine and fake AUTH only make the
// documents) with modkit's routes and a Comments reader on the real AUTH.
type webEnv struct {
	*fixturetest.H
	Clock  *clock.Fixed
	C      *modkit.Comments
	Router chi.Router
}

// newWebEnv seeds the default rights: Priya (restricted to PRJ-METRO) and
// Ravi read orders; Deepak has no read on them.
func newWebEnv(t *testing.T) *webEnv {
	t.Helper()
	ctx := t.Context()
	h := fixturetest.New(t)
	info := doc.NewEngine(doc.Deps{DB: h.DB, Clock: h.Clock, Naming: h.Naming, Numbers: fixturetest.Numbers(), Files: h.Files})
	a := auth.New(auth.Deps{
		DB: h.DB, Clock: h.Clock, Directory: testkit.Directory(h.F), Audit: auditfake.New(), Refs: &authtest.Refs{}, Info: info,
	})
	if err := a.SeedDefaults(ctx, auth.SeedOptions{}); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	if err := a.ChangeRestrictions(ctx, h.Admin, h.Priya.ID, auth.DimProject, []int64{h.M.Project["PRJ-METRO"]}, nil, nil); err != nil {
		t.Fatalf("restrict Priya: %v", err)
	}
	ck := testkit.Clock()
	r := chi.NewRouter()
	modkitweb.Routes(modkitweb.Deps{DB: h.DB, Auth: a, Audit: auditfake.New(), Clock: ck, Info: info})(r)
	c := modkit.NewComments(modkit.CommentsDeps{DB: h.DB, Auth: a, Info: info, Clock: ck})
	return &webEnv{H: h, Clock: ck, C: c, Router: r}
}

// order makes a draft order owned by p on project.
func (e *webEnv) order(t *testing.T, p auth.Principal, project string) int64 {
	t.Helper()
	return e.NewOrderWith(t, p, map[string]string{"project_id": strconv.FormatInt(e.M.Project[project], 10)}).ID
}

// commentPath is the comment route for a document.
func commentPath(dt string, id string) string { return "/modules/comments/" + dt + "/" + id }

func orderComments(id int64) string { return commentPath(fixture.Order, strconv.FormatInt(id, 10)) }

// result is one answer with its body parsed.
type result struct {
	status int
	header http.Header
	raw    string
	doc    *goquery.Document
}

func (r result) text() string { return strings.Join(strings.Fields(r.doc.Text()), " ") }

// post sends body as p, through htmx when hx.
func (e *webEnv) post(t *testing.T, p auth.Principal, path, body string, hx bool) result {
	t.Helper()
	form := url.Values{bodyField: {body}}
	req := httptest.NewRequestWithContext(auth.WithPrincipal(t.Context(), p), http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if hx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	e.Router.ServeHTTP(rec, req)
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("POST %s: parse body: %v", path, err)
	}
	return result{status: rec.Code, header: rec.Header(), raw: rec.Body.String(), doc: d}
}

// list reads the document's comments as Ravi, who reads every order.
func (e *webEnv) list(t *testing.T, id int64) []string {
	t.Helper()
	got, err := e.C.ListComments(t.Context(), e.Ravi, fixture.Order, id)
	if err != nil {
		t.Fatalf("ListComments(%d): %v", id, err)
	}
	out := make([]string, len(got))
	for i, c := range got {
		out[i] = c.Body
	}
	return out
}

func wantBodies(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") || len(got) != len(want) {
		t.Errorf("%s = %q, want %q", what, got, want)
	}
}

// pageTag is a tag only a whole page has.
var pageTag = regexp.MustCompile(`(?i)<(html|head|body|main|nav)[\s>]`)

// box is the response's one comment box.
func box(t *testing.T, what string, r result) *goquery.Selection {
	t.Helper()
	ta := r.doc.Find("textarea")
	if ta.Length() != 1 || ta.AttrOr("name", "") != bodyField {
		t.Fatalf("%s: %d textareas (first named %q), want exactly one named %q; body:\n%s", what, ta.Length(), ta.AttrOr("name", ""), bodyField, r.raw)
	}
	return ta
}

// Without htmx the post redirects to the document page, and the comment is saved.
func TestCommentsWeb_PostRedirects(t *testing.T) {
	e := newWebEnv(t)
	id := e.order(t, e.Priya, "PRJ-METRO")
	r := e.post(t, e.Priya, orderComments(id), "  Checked the rate with the vendor.  ", false)
	want := "/doc/" + fixture.Order + "/" + strconv.FormatInt(id, 10) + "#comments" // UX design §5.10
	if r.status != http.StatusSeeOther || r.header.Get("Location") != want {
		t.Errorf("POST = %d Location %q, want 303 to %q; body %q", r.status, r.header.Get("Location"), want, r.raw)
	}
	wantBodies(t, "comments after the post", e.list(t, id), []string{"Checked the rate with the vendor."})
}

// With htmx the answer is only the comment list, the new comment last, and a
// cleared box: never the whole page.
func TestCommentsWeb_HTMXFragment(t *testing.T) {
	e := newWebEnv(t)
	id := e.order(t, e.Priya, "PRJ-METRO")
	t0 := e.Clock.Now()
	if _, err := e.C.AddComment(t.Context(), e.Priya, fixture.Order, id, "Checked the rate with the vendor."); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	e.Clock.Advance(26 * time.Hour)
	r := e.post(t, e.Ravi, orderComments(id), "Approved on the phone.", true)
	if r.status != http.StatusOK {
		t.Fatalf("htmx POST = %d, want 200; body %q", r.status, r.raw)
	}
	if m := pageTag.FindString(r.raw); m != "" {
		t.Errorf("htmx answer contains %s, want only the comment list and the box:\n%s", m, r.raw)
	}
	if ta := box(t, "htmx answer", r); strings.TrimSpace(ta.Text()) != "" {
		t.Errorf("box after the post holds %q, want it cleared", ta.Text())
	}
	txt := r.text()
	first, second := strings.Index(txt, "Checked the rate with the vendor."), strings.Index(txt, "Approved on the phone.")
	if first < 0 || second < 0 || first > second {
		t.Errorf("htmx answer %q: want both comments, oldest first", txt)
	}
	for _, s := range []string{namePriya, nameRavi, types.FormatIST(t0), types.FormatIST(t0.Add(26 * time.Hour))} {
		if !strings.Contains(txt, s) {
			t.Errorf("htmx answer %q lacks %q (each comment's name and time)", txt, s)
		}
	}
	wantBodies(t, "comments after the post", e.list(t, id), []string{"Checked the rate with the vendor.", "Approved on the phone."})
}

// A refused body is a 422 that keeps what was typed and shows the message on
// the box; nothing is saved.
func TestCommentsWeb_InvalidKeepsBody(t *testing.T) {
	e := newWebEnv(t)
	id := e.order(t, e.Priya, "PRJ-METRO")
	long := strings.TrimSpace(strings.Repeat("Rate is too high. ", 60)) // 1,079 characters
	for _, c := range []struct{ what, body, keep, msg string }{
		{"too long", long, long, msgCommentTooLong},
		{"empty", "   ", "", msgCommentEmpty},
	} {
		r := e.post(t, e.Priya, orderComments(id), c.body, true)
		if r.status != http.StatusUnprocessableEntity {
			t.Errorf("%s: htmx POST = %d, want 422; body %q", c.what, r.status, r.raw)
			continue
		}
		ta := box(t, c.what, r)
		if strings.TrimSpace(ta.Text()) != c.keep {
			t.Errorf("%s: box holds %d characters %q, want the typed %d", c.what, len([]rune(ta.Text())), ta.Text(), len([]rune(c.keep)))
		}
		if ta.AttrOr("aria-invalid", "") != "true" {
			t.Errorf("%s: box is not marked invalid", c.what)
		}
		msgID := ta.AttrOr("id", "") + "-msg"
		if got := strings.TrimSpace(r.doc.Find(`[id="` + msgID + `"]`).Text()); got != c.msg {
			t.Errorf("%s: box message (#%s) = %q, want %q", c.what, msgID, got, c.msg)
		}
	}
	wantBodies(t, "comments after refused posts", e.list(t, id), nil)
}

// A record the user can't read answers exactly like a missing one: AUTH-108's
// 404 with AUTH's text (CD-245); so do an unregistered DocType and a bad id.
func TestCommentsWeb_HiddenIsNotFound(t *testing.T) {
	e := newWebEnv(t)
	metro := e.order(t, e.Priya, "PRJ-METRO")
	techpark := e.order(t, e.Ravi, "PRJ-TECHPARK")
	claim := e.NewClaim(t, e.Priya, "EMP-004", fixturetest.CL{Type: "Travel", Amount: "1500.00"}).ID

	missing := e.post(t, e.Priya, orderComments(999_999), "Is this still needed?", false)
	if missing.status != http.StatusNotFound || !strings.Contains(missing.text(), auth.MsgNotFound) {
		t.Fatalf("missing order: %d %q, want 404 %q", missing.status, missing.text(), auth.MsgNotFound)
	}
	for _, c := range []struct {
		what string
		p    auth.Principal
		path string
	}{
		{"hidden by restriction (Priya on PRJ-TECHPARK)", e.Priya, orderComments(techpark)},
		{"no read on orders (Deepak)", e.Deepak, orderComments(metro)},
		{"DocType not registered for comments", e.Priya, commentPath(fixture.Claim, strconv.FormatInt(claim, 10))},
		{"id not a number", e.Priya, commentPath(fixture.Order, "abc")},
	} {
		r := e.post(t, c.p, c.path, "Is this still needed?", false)
		if r.status != http.StatusNotFound || r.raw != missing.raw {
			t.Errorf("%s: %d %q, want the missing order's 404 %q", c.what, r.status, r.raw, missing.raw)
		}
	}
	for _, id := range []int64{metro, techpark} {
		wantBodies(t, "comments after refused posts", e.list(t, id), nil)
	}
}

// CommentsPanel: a card with the comments in the order given (ListComments'
// oldest first), each with name and time, and a box posting to the route with
// the CSRF token and a Post comment button.
func TestCommentsPanel_Render(t *testing.T) {
	t0 := testkit.Clock().Now()
	comments := []modkit.Comment{
		{ID: 1, DocType: fixture.Order, RecordID: 42, Body: "Checked the rate with the vendor.", CreatedBy: 4, CreatedByName: namePriya, CreatedAt: t0},
		{ID: 2, DocType: fixture.Order, RecordID: 42, Body: "Approved on the phone.", CreatedBy: 3, CreatedByName: nameRavi, CreatedAt: t0.Add(26 * time.Hour)},
	}
	var buf bytes.Buffer
	if err := modkitweb.CommentsPanel(fixture.Order, 42, comments, "csrf-m014").Render(t.Context(), &buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	r := result{status: http.StatusOK, raw: buf.String(), doc: d}
	txt := r.text()
	first, second := strings.Index(txt, comments[0].Body), strings.Index(txt, comments[1].Body)
	if first < 0 || second < 0 || first > second {
		t.Errorf("panel %q: want both comments, oldest first", txt)
	}
	for _, s := range []string{namePriya, nameRavi, types.FormatIST(t0), types.FormatIST(t0.Add(26 * time.Hour))} {
		if !strings.Contains(txt, s) {
			t.Errorf("panel %q lacks %q", txt, s)
		}
	}
	form := d.Find(`form[method="post"]`)
	if form.Length() != 1 || form.AttrOr("action", "") != commentPath(fixture.Order, "42") {
		t.Errorf("panel has %d POST forms (action %q), want one posting to %q", form.Length(), form.AttrOr("action", ""), commentPath(fixture.Order, "42"))
	}
	if got := form.Find(`input[name="csrf_token"]`).AttrOr("value", ""); got != "csrf-m014" {
		t.Errorf("form's csrf_token = %q, want %q", got, "csrf-m014")
	}
	if ta := box(t, "panel", r); strings.TrimSpace(ta.Text()) != "" || form.Find("textarea").Length() != 1 {
		t.Errorf("panel's box: want one empty textarea inside the form")
	}
	if btn := form.Find(`button[type="submit"]`); btn.Length() != 1 || strings.TrimSpace(btn.Text()) != "Post comment" {
		t.Errorf("panel's submit buttons = %d (%q), want one %q", btn.Length(), strings.TrimSpace(btn.Text()), "Post comment")
	}
	if strings.Contains(r.raw, "style=") || strings.Contains(r.raw, "<script") {
		t.Errorf("panel has an inline style or script (CSP)")
	}
}
