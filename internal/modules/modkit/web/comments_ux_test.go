package modkitweb_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	modkitweb "github.com/vardhan/internaltools/internal/modules/modkit/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// UX design 5.10 and its N1 note: line breaks kept, focus and a polite
// announcement after a redraw, the hint, and the no-script refusal (PRG).

const (
	msgPosted  = "Comment posted."
	hintOrder  = "Everyone who can open this purchase order sees it. Comments can't be changed later."
	draftRoute = "/draft/{doctype}/{id}"
)

// newUXEnv is newWebEnv with sessions (the router runs inside LoadAndSave and
// serves TakeCommentDraft at draftRoute) and the handler's InfoSource
// wrapped by wrap.
func newUXEnv(t *testing.T, wrap func(doctype.InfoSource) doctype.InfoSource) (*webEnv, *scs.SessionManager) {
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
	ck := testkit.Clock()
	sm := scs.New()
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	modkitweb.Routes(modkitweb.Deps{DB: h.DB, Auth: a, Audit: auditfake.New(), Clock: ck, Info: wrap(info), Sessions: sm})(r)
	r.Get(draftRoute, func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		typed, msg := modkitweb.TakeCommentDraft(r.Context(), sm, chi.URLParam(r, "doctype"), id)
		_, _ = w.Write([]byte(typed + "|" + msg)) //nolint:gosec // G705: a test route the test itself reads as text
	})
	c := modkit.NewComments(modkit.CommentsDeps{DB: h.DB, Auth: a, Info: info, Clock: ck})
	return &webEnv{H: h, Clock: ck, C: c, Router: r}, sm
}

func same(i doctype.InfoSource) doctype.InfoSource { return i }

func parse(t *testing.T, raw string) result {
	t.Helper()
	d, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return result{status: http.StatusOK, raw: raw, doc: d}
}

func panelHTML(t *testing.T, dt string, comments []modkit.Comment, typed, msg string) result {
	t.Helper()
	var buf bytes.Buffer
	if err := modkitweb.CommentsPanelDraft(dt, 42, comments, "csrf-m014", typed, msg).Render(t.Context(), &buf); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return parse(t, buf.String())
}

// announcement is the polite status line.
func announcement(r result) *goquery.Selection { return r.doc.Find(`[role="status"]`) }

// A body keeps its line breaks and wraps anywhere.
func TestCommentsUX_BodyKeepsLineBreaks(t *testing.T) {
	body := "Rate checked.\nVendor: SHREE-CONSTRUCTION-SUPPLIES-AND-HARDWARE-LIMITED-UNIT-2"
	r := panelHTML(t, fixture.Order, []modkit.Comment{{ID: 1, Body: body, CreatedByName: namePriya, CreatedAt: testkit.Clock().Now()}}, "", "")
	var p *goquery.Selection
	r.doc.Find("p").Each(func(_ int, s *goquery.Selection) {
		if s.Text() == body {
			p = s
		}
	})
	if p == nil {
		t.Fatalf("no element holds the body with its line break:\n%s", r.raw)
	}
	for _, cls := range []string{"whitespace-pre-line", "wrap-anywhere"} {
		if !p.HasClass(cls) {
			t.Errorf("body element class %q lacks %q", p.AttrOr("class", ""), cls)
		}
	}
}

// The panel: an empty announcement, no focus taken on page load, the hint
// with the DocType's label in lower case, "No comments yet." when empty.
func TestCommentsUX_PanelHintAndQuietLoad(t *testing.T) {
	r := panelHTML(t, fixture.Order, nil, "", "")
	if _, ok := box(t, "panel", r).Attr("autofocus"); ok {
		t.Errorf("panel's box takes focus on page load")
	}
	if a := announcement(r); a.Length() != 1 || strings.TrimSpace(a.Text()) != "" {
		t.Errorf("panel announcement: %d elements %q, want one empty", a.Length(), a.Text())
	}
	txt := r.text()
	for _, s := range []string{hintOrder, "No comments yet."} {
		if !strings.Contains(txt, s) {
			t.Errorf("panel %q lacks %q", txt, s)
		}
	}
	if txt := panelHTML(t, "nope.Thing", nil, "", "").text(); !strings.Contains(txt, "Everyone who can open this document sees it.") {
		t.Errorf("panel for an unknown DocType %q: want the hint to say document", txt)
	}
}

// CommentsPanelDraft shows a refused post's text and message on the box.
func TestCommentsUX_PanelDraft(t *testing.T) {
	r := panelHTML(t, fixture.Order, nil, "Typed before.", msgCommentEmpty)
	ta := box(t, "draft panel", r)
	if strings.TrimSpace(ta.Text()) != "Typed before." || ta.AttrOr("aria-invalid", "") != "true" {
		t.Errorf("draft box = %q invalid %q, want the typed text, invalid", ta.Text(), ta.AttrOr("aria-invalid", ""))
	}
	if got := strings.TrimSpace(r.doc.Find(`[id="` + ta.AttrOr("id", "") + `-msg"]`).Text()); got != msgCommentEmpty {
		t.Errorf("draft box message = %q, want %q", got, msgCommentEmpty)
	}
}

// After an htmx post the box takes focus; a saved comment is announced out
// of band, a refused one isn't.
func TestCommentsUX_RedrawFocusAndAnnouncement(t *testing.T) {
	e := newWebEnv(t)
	id := e.order(t, e.Priya, "PRJ-METRO")
	for _, c := range []struct {
		what, body string
		status     int
		announce   string
	}{
		{"saved", "Checked the rate.", http.StatusOK, msgPosted},
		{"refused", "  ", http.StatusUnprocessableEntity, ""},
	} {
		r := e.post(t, e.Priya, orderComments(id), c.body, true)
		if r.status != c.status {
			t.Fatalf("%s: htmx POST = %d, want %d; body %q", c.what, r.status, c.status, r.raw)
		}
		if _, ok := box(t, c.what, r).Attr("autofocus"); !ok {
			t.Errorf("%s: redrawn box has no autofocus", c.what)
		}
		a := announcement(r)
		if a.Length() != 1 || strings.TrimSpace(a.Text()) != c.announce || a.AttrOr("hx-swap-oob", "") != "true" {
			t.Errorf("%s: announcement %d elements %q oob %q, want one %q swapped out of band", c.what, a.Length(), a.Text(), a.AttrOr("hx-swap-oob", ""), c.announce)
		}
	}
}

// Without htmx a refused body goes into the session and the post redirects
// to the panel; TakeCommentDraft returns it once.
func TestCommentsUX_NoScriptRefusal(t *testing.T) {
	e, _ := newUXEnv(t, same)
	id := e.order(t, e.Priya, "PRJ-METRO")
	long := strings.Repeat("x", 1001)
	form := url.Values{bodyField: {long}}
	req := httptest.NewRequestWithContext(auth.WithPrincipal(t.Context(), e.Priya), http.MethodPost, orderComments(id), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.Router.ServeHTTP(rec, req)
	want := "/doc/" + fixture.Order + "/" + strconv.FormatInt(id, 10) + "#comments"
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
		t.Fatalf("no-script refused POST = %d Location %q, want 303 to %q", rec.Code, rec.Header().Get("Location"), want)
	}
	cookies := rec.Result().Cookies()
	take := func() string {
		t.Helper()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/draft/"+fixture.Order+"/"+strconv.FormatInt(id, 10), nil)
		for _, ck := range cookies {
			req.AddCookie(ck)
		}
		rec := httptest.NewRecorder()
		e.Router.ServeHTTP(rec, req)
		return rec.Body.String()
	}
	if got := take(); got != long+"|"+msgCommentTooLong {
		t.Errorf("TakeCommentDraft = %q, want the typed text and %q", got, msgCommentTooLong)
	}
	if got := take(); got != "|" {
		t.Errorf("second TakeCommentDraft = %q, want nothing", got)
	}
	wantBodies(t, "comments after the refused post", e.list(t, id), nil)
}

// failSecond answers not found on every Info call after the first, as if
// the record went away between the post's two transactions.
type failSecond struct {
	doctype.InfoSource
	n atomic.Int32
}

func (f *failSecond) Info(ctx context.Context, tx *db.Tx, dt string, id int64) (doctype.Info, error) {
	if f.n.Add(1) > 1 {
		return doctype.Info{}, doctype.ErrNotFound
	}
	return f.InfoSource.Info(ctx, tx, dt, id)
}

// The redraw's read failing after the comment was saved answers AUTH's 404,
// not a redraw; the comment stays saved.
func TestCommentsUX_RedrawReadFails(t *testing.T) {
	e, _ := newUXEnv(t, func(i doctype.InfoSource) doctype.InfoSource { return &failSecond{InfoSource: i} })
	id := e.order(t, e.Priya, "PRJ-METRO")
	r := e.post(t, e.Priya, orderComments(id), "Checked the rate.", true)
	if r.status != http.StatusNotFound || r.doc.Find("textarea").Length() != 0 {
		t.Errorf("htmx POST with the redraw's read failing = %d, want 404 and no redraw; body %q", r.status, r.raw)
	}
	wantBodies(t, "comments after the post", e.list(t, id), []string{"Checked the rate."})
}
