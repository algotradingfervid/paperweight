package docscreen_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

const (
	csrfToken  = "tok-13"
	samplePath = "/doc/docscreentest.Sample"
	doc42      = samplePath + "/42"
	inboxURL   = "/approvals"
)

var ravi = auth.Principal{ID: 3, Name: "Ravi Kulkarni"}

// allRights is every right the document page asks about, plus read.
var allRights = []auth.Right{"read", "write", "submit", "approve", "cancel", "amend", "delete", "print"}

// The real APR service is what production passes as Deps.Approval.
var _ docscreen.Approver = approval.Service(nil)

// approver returns the error the test sets in block; nil allows (CD-193).
type approver struct{ block error }

func (a *approver) CheckApprover(context.Context, *db.Tx, auth.Principal, string, int64) error {
	return a.block
}

// docEnv is the document-page harness: WP0's router with a session manager
// (so web.Flash survives a 303, CD-80), a shell carrying a CSRF token, and
// a cookie jar kept across requests.
type docEnv struct {
	docs    *docfake.Fake
	az      *authfake.Fake
	apr     *approver
	extras  *docscreentest.Extras
	db      *docscreentest.Reader
	d       docscreen.Deps
	as      auth.Principal
	h       http.Handler
	cookies map[string]*http.Cookie
}

// newDocEnv gives Priya (role Buyer) the rights on the sample DocType and
// lets her pass CheckApprover.
func newDocEnv(t *testing.T, rights ...auth.Right) *docEnv {
	t.Helper()
	docscreentest.RegisterSample()
	docscreentest.SetMaster(docscreentest.Supplier, 7, doctype.MasterInfo{Code: "SHREE", Name: "Shree Steel Traders"})
	docscreentest.SetMaster(docscreentest.Item, 3, doctype.MasterInfo{Code: "MS-ANGLE", Name: "MS Angle 50x50x5"})
	e := &docEnv{
		docs:    docfake.New(),
		az:      authfake.New(),
		apr:     &approver{},
		extras:  docscreentest.NewExtras(),
		db:      &docscreentest.Reader{},
		as:      priya,
		cookies: map[string]*http.Cookie{},
	}
	for _, r := range rights {
		e.az.Grant("Buyer", docscreentest.Sample, r, auth.ScopeAll)
	}
	e.az.AddRole(priya.ID, "Buyer")
	e.d = docscreen.Deps{
		DB:        e.db,
		Engine:    e.docs,
		Auth:      e.az,
		Approval:  e.apr,
		Lister:    docscreentest.NewLister(),
		Extras:    e.extras,
		Principal: func(*http.Request) (auth.Principal, bool) { return e.as, true },
		Today:     func() (string, string) { return "24-10-2026", "23-10-2026" },
		InboxURL:  inboxURL,
		PrintURL:  func(name string, id int64) string { return "/print/" + name + "/" + itoa(id) },
	}
	r := web.NewRouter(web.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Sessions: scs.New()})
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(ui.WithShell(r.Context(), ui.Shell{CSRF: csrfToken})))
		})
	})
	docscreen.Routes(e.d)(r)
	e.h = r
	return e
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// sampleInfo is document id in state s: numbered once approved.
func sampleInfo(id int64, s doctype.State) doctype.Info {
	info := doctype.Info{
		DocType: docscreentest.Sample, DocLabel: "Sample Order", ID: id, State: s,
		Display: "Draft " + itoa(id), Party: "Shree Steel Traders",
		CreatedBy: priya.ID, EmployeeID: 5, ProjectIDs: []int64{9},
	}
	if s == doctype.StateApproved || s == doctype.StateClosed || s == doctype.StateCancelled {
		info.Number, info.Display = "SO/26-27/00001", "SO/26-27/00001"
	}
	return info
}

// sampleRecord is the record every page test uses, at version 3.
func sampleRecord() doc.Record {
	return doc.Record{
		Version: 3,
		Values:  map[string]string{"supplier_id": "7", "order_date": "2026-10-24", "amount": "94164.00", "remarks": ""},
		Lines:   []map[string]string{{"id": "1", "item_id": "3", "qty": "1200.000", "uom": "kg", "rate": "62.5000"}},
	}
}

// seed stores document 42 in state s.
func (e *docEnv) seed(s doctype.State) {
	docscreentest.Seed(e.docs, sampleInfo(42, s), sampleRecord())
}

// do sends a request through the router, carrying the session cookie.
func (e *docEnv) do(t *testing.T, method, target string, form url.Values, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	for _, c := range e.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		e.cookies[c.Name] = c
	}
	return rec
}

func (e *docEnv) get(t *testing.T, target string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, http.MethodGet, target, nil, hdr...)
}

// post sends form with the page's csrf token and version 3.
func (e *docEnv) post(t *testing.T, target string, form url.Values, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf_token", csrfToken)
	if !form.Has("version") {
		form.Set("version", "3")
	}
	return e.do(t, http.MethodPost, target, form, hdr...)
}

// norm collapses runs of whitespace.
func norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// bannerTexts lists the message of every banner of kind (success, warning,
// danger) in sel, without the kind's word ("Success:", "Warning:", "Error:").
// The notice <template>s the layout carries for toast.js are not banners.
func bannerTexts(sel *goquery.Selection, kind string) []string {
	var out []string
	sel.Find(".banner--" + kind).Each(func(_ int, b *goquery.Selection) {
		if b.ParentsFiltered("template").Length() > 0 {
			return
		}
		text := norm(b.Find(".banner-body").Text())
		for _, w := range []string{"Success:", "Warning:", "Error:", "Information:"} {
			text = strings.TrimSpace(strings.TrimPrefix(text, w))
		}
		out = append(out, text)
	})
	return out
}

// location is the path of a 303's Location.
func location(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body %.300s", rec.Code, rec.Body)
	}
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u.Path
}

// formOf is the form a control belongs to: its form attribute, else its
// enclosing form.
func formOf(doc *goquery.Document, el *goquery.Selection) *goquery.Selection {
	if id, ok := el.Attr("form"); ok && id != "" {
		return doc.Find(`form[id="` + id + `"]`)
	}
	return el.Closest("form")
}

// buttonsNamed is every button or link whose trimmed text is label.
func buttonsNamed(sel *goquery.Selection, label string) *goquery.Selection {
	return sel.Find("button, a").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return norm(s.Text()) == label
	})
}

func TestDocPage_HiddenRecordIs404(t *testing.T) {
	e := newDocEnv(t, "read")
	e.seed(doctype.StatePending)
	e.az.Hide(docscreentest.Sample, 42)
	rec := e.get(t, doc42)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("hidden record: status = %d, want 404", rec.Code)
	}
	hidden := uitest.Doc(t, rec).Text()
	if !strings.Contains(hidden, doctype.ErrNotFound.Error()) {
		t.Errorf("hidden record: body lacks AUTH's text %q", doctype.ErrNotFound.Error())
	}

	// CD-245 (AUTH-108): a missing id reads the same as a hidden one.
	rec = e.get(t, samplePath+"/99")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing record: status = %d, want 404", rec.Code)
	}
	if missing := uitest.Doc(t, rec).Text(); missing != hidden {
		t.Errorf("missing record page differs from the hidden one:\n%s\n---\n%s", missing, hidden)
	}

	// DOC-031: a save of a missing draft keeps DOC's text.
	rec = e.post(t, samplePath+"/99/edit/details", url.Values{"supplier_id": {"7"}, "order_date": {"24-10-2026"}})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("save of a missing record: status = %d, want 404", rec.Code)
	}
	if text := uitest.Doc(t, rec).Text(); !strings.Contains(text, doc.Gone().Message) {
		t.Errorf("save of a missing record: body lacks DOC's text %q", doc.Gone().Message)
	}
}

func TestDocPage_DraftRedirectsToCheck(t *testing.T) {
	e := newDocEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	if got := location(t, e.get(t, doc42)); got != doc42+"/edit/check" {
		t.Errorf("draft redirects to %q, want %s/edit/check", got, doc42)
	}
}

func TestDocPage_ReadOnlyOutsideDraft(t *testing.T) {
	direct := []string{"submit", "withdraw", "approve", "revise", "amend"}
	for _, s := range []doctype.State{doctype.StatePending, doctype.StateApproved, doctype.StateRejected, doctype.StateClosed, doctype.StateCancelled} {
		t.Run(string(s), func(t *testing.T) {
			e := newDocEnv(t, allRights...)
			e.seed(s)
			rec := e.get(t, doc42)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %.300s", rec.Code, rec.Body)
			}
			doc := uitest.Doc(t, rec)
			page := doc.Find(".app-page")
			if page.Length() == 0 {
				t.Fatal("no .app-page")
			}
			if !strings.Contains(page.Find("h1").Text(), sampleInfo(42, s).Display) {
				t.Errorf("h1 = %q, want the Display", page.Find("h1").Text())
			}
			page.Find("input, select, textarea").Each(func(_ int, el *goquery.Selection) {
				html, _ := goquery.OuterHtml(el)
				form := formOf(doc, el)
				action, _ := form.Attr("action")
				method, _ := form.Attr("method")
				ok := strings.EqualFold(method, "post") && slices.ContainsFunc(direct, func(a string) bool { return action == doc42+"/"+a })
				if !ok {
					t.Errorf("control outside an action form: %s (form action %q)", html, action)
					return
				}
				if typ, _ := el.Attr("type"); goquery.NodeName(el) != "input" || typ != "hidden" {
					t.Errorf("visible control in an action form: %s", html)
				}
			})
		})
	}
}

func TestDocPage_RejectedReasonBanner(t *testing.T) {
	const reason = "Wrong GSTIN on the supplier."
	e := newDocEnv(t, allRights...)
	e.seed(doctype.StateRejected)
	e.extras.SetOrigin(docscreentest.Sample, 42, docscreen.Origin{StateReason: reason})
	rec := e.get(t, doc42)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	main := uitest.Doc(t, rec).Find("#main")
	got := bannerTexts(main, "danger")
	if !slices.Contains(got, reason) {
		t.Fatalf("danger banners = %q, want one holding %q", got, reason)
	}
	first := main.Find(".banner").First()
	if !first.HasClass("banner--danger") || !strings.Contains(first.Text(), reason) {
		t.Error("the reason is not the first banner on the page")
	}
	html, _ := main.Html()
	if dl := strings.Index(html, "<dl"); dl >= 0 && strings.Index(html, reason) > dl {
		t.Error("the reason banner comes after the header facts; want it at the top")
	}
}

func TestDocPage_FlashBanners(t *testing.T) {
	cases := []struct {
		name   string
		before doctype.State
		action string
		reason string
		result doc.Result
		after  doctype.State // state the next GET finds
		to     string        // Location path
		page   string        // the full page GET next (the check page is Task 14's, so a list GET stands in)
		want   string
	}{
		{"submit needing approval", doctype.StateDraft, "submit", "", doc.Result{ID: 42, State: doctype.StatePending, Version: 4}, doctype.StatePending, doc42, doc42,
			"Submitted for approval to Ravi Kulkarni (Purchase Manager)."},
		{"submit straight to approved", doctype.StateDraft, "submit", "", doc.Result{ID: 42, State: doctype.StateApproved, Number: "SO/26-27/00001", Version: 4}, doctype.StateApproved, doc42, doc42,
			"Approved. SO/26-27/00001 has been numbered."},
		{"approve", doctype.StatePending, "approve", "", doc.Result{ID: 42, State: doctype.StateApproved, Number: "SO/26-27/00001", Version: 4}, doctype.StateApproved, doc42, doc42,
			"Approved. SO/26-27/00001 has been numbered."},
		{"reject", doctype.StatePending, "reject", "Wrong GSTIN", doc.Result{ID: 42, State: doctype.StateRejected, Version: 4}, doctype.StateRejected, doc42, doc42,
			"Rejected and sent back."},
		{"withdraw", doctype.StatePending, "withdraw", "", doc.Result{ID: 42, State: doctype.StateDraft, Version: 4}, doctype.StateDraft, doc42 + "/edit/check", samplePath,
			"Withdrawn. You can change the draft and submit it again."},
		{"amend", doctype.StateCancelled, "amend", "", doc.Result{ID: 43, State: doctype.StateDraft, Version: 1}, doctype.StateCancelled, samplePath + "/43/edit/check", samplePath,
			"Amending SO/26-27/00001"},
		{"cancel", doctype.StateApproved, "cancel", "Supplier closed", doc.Result{ID: 42, State: doctype.StateCancelled, Version: 4}, doctype.StateCancelled, doc42, doc42,
			"Cancelled."},
		{"close", doctype.StateApproved, "close", "Short supply accepted", doc.Result{ID: 42, State: doctype.StateClosed, Version: 4}, doctype.StateClosed, doc42, doc42,
			"Closed."},
		{"delete", doctype.StateDraft, "delete", "", doc.Result{ID: 42}, doctype.StateDraft, samplePath, samplePath,
			"Deleted Draft 42."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newDocEnv(t, append(allRights, "create")...)
			e.seed(c.before)
			e.extras.SetApproval(docscreentest.Sample, 42, docscreen.ApprovalView{Needed: true, WaitingOn: "Ravi Kulkarni (Purchase Manager)"})
			e.extras.SetOrigin(docscreentest.Sample, 43, docscreen.Origin{AmendedFrom: "SO/26-27/00001", AmendedFromID: 42})
			e.docs.SetResult(docscreentest.Sample, 42, doctype.Action(c.action), c.result, nil)

			form := url.Values{}
			if c.reason != "" {
				form.Set("reason", c.reason)
			}
			rec := e.post(t, doc42+"/"+c.action, form)
			if got := location(t, rec); got != c.to {
				t.Fatalf("303 to %q, want %q", got, c.to)
			}
			if strings.Contains(rec.Header().Get("Location"), "done=") || strings.Contains(rec.Header().Get("Location"), url.QueryEscape(c.want)) {
				t.Errorf("Location %q carries the banner; it must travel in the flash (CD-80)", rec.Header().Get("Location"))
			}
			if calls := e.docs.Calls(); len(calls) != 1 || calls[0].Action != doctype.Action(c.action) || calls[0].Input.Version != 3 {
				t.Fatalf("engine calls = %+v, want one %s with version 3", calls, c.action)
			}

			info := sampleInfo(42, c.after)
			if c.result.Number != "" {
				info.Number, info.Display = c.result.Number, c.result.Number
			}
			e.docs.SetInfo(info)

			next := c.page
			if c.page == c.to {
				next = rec.Header().Get("Location")
			}
			first := e.get(t, next)
			if first.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, body %.300s", next, first.Code, first.Body)
			}
			doc := uitest.Doc(t, first)
			if got := bannerTexts(doc.Find("#main"), "success"); !slices.Equal(got, []string{c.want}) {
				t.Errorf("success banners on the next GET = %q, want [%q]", got, c.want)
			}
			if c.action == "approve" {
				if link := doc.Find(`#main a[href="` + inboxURL + `"]`); norm(link.Text()) != "Next request" {
					t.Errorf("no \"Next request\" link to %s after approve", inboxURL)
				}
			}

			again := uitest.Doc(t, e.get(t, c.page))
			if got := bannerTexts(again.Find("#main"), "success"); len(got) != 0 {
				t.Errorf("second GET still shows %q", got)
			}
			done := uitest.Doc(t, e.get(t, c.page+"?done="+c.action))
			if got := bannerTexts(done.Find("#main"), "success"); len(got) != 0 {
				t.Errorf("?done= shows a banner %q; banners come only from the flash", got)
			}
		})
	}
}

func TestDocPage_ApproveIsOneTapPost(t *testing.T) {
	e := newDocEnv(t, allRights...)
	e.seed(doctype.StatePending)
	rec := e.get(t, doc42)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	page := uitest.Doc(t, rec)
	approve := buttonsNamed(page.Selection, "Approve")
	if approve.Length() != 1 {
		t.Fatalf("found %d Approve controls, want 1", approve.Length())
	}
	if goquery.NodeName(approve) != "button" {
		t.Fatalf("Approve is a <%s>, want a submit button", goquery.NodeName(approve))
	}
	if typ, ok := approve.Attr("type"); ok && typ != "submit" {
		t.Errorf("Approve type = %q, want submit", typ)
	}
	form := formOf(page, approve)
	if form.Length() != 1 {
		t.Fatal("Approve belongs to no form")
	}
	if m, _ := form.Attr("method"); !strings.EqualFold(m, "post") {
		t.Errorf("form method = %q, want post", m)
	}
	if a, _ := form.Attr("action"); a != doc42+"/approve" {
		t.Errorf("form action = %q, want %s/approve", a, doc42)
	}
	if form.Find(`input[type="hidden"][name="csrf_token"][value="`+csrfToken+`"]`).Length() != 1 {
		t.Error("approve form has no csrf_token")
	}
	if form.Find(`input[type="hidden"][name="version"][value="3"]`).Length() != 1 {
		t.Error("approve form has no version 3")
	}
	if page.Find(".app-page dialog, [hx-confirm], [data-confirm]").Length() != 0 {
		t.Error("the page has a confirm step for approve")
	}

	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionApprove, doc.Result{ID: 42, State: doctype.StateApproved, Number: "SO/26-27/00001", Version: 4}, nil)
	location(t, e.post(t, doc42+"/approve", nil))
	if calls := e.docs.Calls(); len(calls) != 1 || calls[0].Action != doctype.ActionApprove || calls[0].ID != 42 || calls[0].Input.Version != 3 || calls[0].By != priya.ID {
		t.Errorf("one tap: engine calls = %+v, want one approve of 42 at version 3 by Priya", calls)
	}
}

func TestDirectAction_WarningsBanner(t *testing.T) {
	const warn = "Supplier SHREE is on hold."
	e := newDocEnv(t, allRights...)
	e.seed(doctype.StatePending)
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionApprove, doc.Result{ID: 42, State: doctype.StateApproved, Number: "SO/26-27/00001", Version: 4, Warnings: []string{warn}}, nil)
	rec := e.post(t, doc42+"/approve", nil, "HX-Request", "true")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %.300s", rec.Code, rec.Body)
	}
	push, err := url.Parse(rec.Header().Get("HX-Push-Url"))
	if err != nil || push.Path != doc42 {
		t.Errorf("HX-Push-Url = %q, want the document page %s", rec.Header().Get("HX-Push-Url"), doc42)
	}
	if !slices.Contains(rec.Header().Values("Vary"), "HX-Request") {
		t.Errorf("Vary = %q", rec.Header().Values("Vary"))
	}
	doc := uitest.Doc(t, rec)
	if got := bannerTexts(doc.Selection, "warning"); !slices.Equal(got, []string{warn}) {
		t.Errorf("warning banners = %q, want exactly one with %q", got, warn)
	}
	if got := bannerTexts(doc.Selection, "success"); !slices.Equal(got, []string{"Approved. SO/26-27/00001 has been numbered."}) {
		t.Errorf("success banners = %q", got)
	}
	if calls := e.docs.Calls(); len(calls) != 1 || calls[0].Action != doctype.ActionApprove || calls[0].Input.Version != 3 {
		t.Errorf("engine calls = %+v", calls)
	}

	// Rendered directly, so nothing is left in the flash for the next page.
	next := uitest.Doc(t, e.get(t, doc42))
	if got := append(bannerTexts(next.Selection, "warning"), bannerTexts(next.Selection, "success")...); len(got) != 0 {
		t.Errorf("the next full page repeats %q; htmx success must not also flash", got)
	}
}

func TestDirectAction_StalePassesMessageThrough(t *testing.T) {
	const msg = "Someone else changed Draft 42 after you opened it. Reload it and make your changes again."
	staleErr := statusErr{status: http.StatusConflict, msg: msg}
	for _, hx := range []bool{false, true} {
		e := newDocEnv(t, allRights...)
		e.seed(doctype.StatePending)
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionApprove, doc.Result{}, staleErr)
		var hdr []string
		if hx {
			hdr = []string{"HX-Request", "true"}
		}
		rec := e.post(t, doc42+"/approve", nil, hdr...)
		if rec.Code != http.StatusConflict {
			t.Fatalf("htmx=%v: status = %d, want 409", hx, rec.Code)
		}
		doc := uitest.Doc(t, rec)
		if got := bannerTexts(doc.Selection, "danger"); !slices.Equal(got, []string{msg}) {
			t.Errorf("htmx=%v: danger banners = %q, want exactly [%q]", hx, got, msg)
		}
		if !strings.Contains(doc.Find("h1").Text(), "Draft 42") {
			t.Errorf("htmx=%v: 409 is not the document page (h1 %q)", hx, doc.Find("h1").Text())
		}
	}
}

func TestDirectAction_AmendOpensNewDraftCheck(t *testing.T) {
	e := newDocEnv(t, append(allRights, "create")...)
	docscreentest.Seed(e.docs, func() doctype.Info {
		i := sampleInfo(42, doctype.StateCancelled)
		i.Number, i.Display = "PO/26-27/00001", "PO/26-27/00001"
		return i
	}(), sampleRecord())
	e.extras.SetOrigin(docscreentest.Sample, 43, docscreen.Origin{AmendedFrom: "PO/26-27/00001", AmendedFromID: 42})
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionAmend, doc.Result{ID: 43}, nil)
	rec := e.post(t, doc42+"/amend", nil)
	if got := location(t, rec); got != "/doc/docscreentest.Sample/43/edit/check" {
		t.Errorf("amend 303 to %q, want /doc/docscreentest.Sample/43/edit/check", got)
	}
	if calls := e.docs.Calls(); len(calls) != 1 || calls[0].Action != doctype.ActionAmend || calls[0].ID != 42 || calls[0].Input.Version != 3 {
		t.Errorf("engine calls = %+v", calls)
	}
	// The check page is Task 14's; any next full page shows the flash.
	next := uitest.Doc(t, e.get(t, samplePath))
	if got := bannerTexts(next.Find("#main"), "success"); !slices.Equal(got, []string{"Amending PO/26-27/00001"}) {
		t.Errorf("flash = %q, want [\"Amending PO/26-27/00001\"]", got)
	}
}

func TestDirectAction_WarningsFlashedAcross303(t *testing.T) {
	const warn = "Supplier SHREE is on hold."
	e := newDocEnv(t, allRights...)
	e.seed(doctype.StatePending)
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionApprove, doc.Result{ID: 42, State: doctype.StateApproved, Number: "SO/26-27/00001", Version: 4, Warnings: []string{warn}}, nil)
	rec := e.post(t, doc42+"/approve", nil)
	if got := location(t, rec); got != doc42 {
		t.Fatalf("303 to %q, want %s", got, doc42)
	}
	if strings.Contains(rec.Header().Get("Location"), "hold") {
		t.Errorf("Location %q carries the warning", rec.Header().Get("Location"))
	}
	e.docs.SetInfo(sampleInfo(42, doctype.StateApproved))
	doc := uitest.Doc(t, e.get(t, rec.Header().Get("Location")))
	if got := bannerTexts(doc.Find("#alerts"), "warning"); !slices.Equal(got, []string{warn}) {
		t.Errorf("#alerts warnings = %q, want [%q]", got, warn)
	}
	again := uitest.Doc(t, e.get(t, doc42))
	if got := bannerTexts(again.Selection, "warning"); len(got) != 0 {
		t.Errorf("second GET still shows %q", got)
	}
}

func TestDocAction_UnknownNameIs404(t *testing.T) {
	e := newDocEnv(t, allRights...)
	e.seed(doctype.StatePending)
	for _, name := range []string{"frobnicate", "save", "edit", "print", "Approve", "submit.json"} {
		if rec := e.get(t, doc42+"/"+name); rec.Code != http.StatusNotFound {
			t.Errorf("GET /%s = %d, want 404", name, rec.Code)
		}
		if rec := e.post(t, doc42+"/"+name, nil); rec.Code != http.StatusNotFound {
			t.Errorf("POST /%s = %d, want 404", name, rec.Code)
		}
	}
	if calls := e.docs.Calls(); len(calls) != 0 {
		t.Errorf("engine called for unknown actions: %+v", calls)
	}
}
