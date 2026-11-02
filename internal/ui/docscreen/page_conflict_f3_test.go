package docscreen_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

const f3Stale = "Someone else changed Draft 42 after you opened it. Reload it and make your changes again."

// f3Conflict is DOC's stale refusal: Classify reads its kind, so a test double
// with only a 409 status isn't one.
var f3Conflict = &doc.Error{Kind: doc.KindStale, Message: f3Stale}

// f3Hdrs are the full-page post and the htmx one.
var f3Hdrs = map[string][]string{"full": nil, "htmx": {"HX-Request", "true"}}

// f3Banner fails unless page's one banner is the ConflictBanner, exactly.
func f3Banner(t *testing.T, what string, page *goquery.Document, msg, label, href string) {
	t.Helper()
	want := msg + f2Keep + "Reload " + label // the three blocks' text, run together
	if got := bannerTexts(page.Selection, "warning"); !slices.Equal(got, []string{want}) {
		t.Errorf("%s: warning banners = %q, want [%q]", what, got, want)
	}
	if got := bannerTexts(page.Selection, "danger"); len(got) != 0 {
		t.Errorf("%s: danger banners = %q, want none", what, got)
	}
	reload := buttonsNamed(page.Find(".banner--warning"), "Reload "+label)
	if h := reload.AttrOr("href", ""); reload.Length() != 1 || h != href {
		t.Errorf("%s: Reload = %d buttons, href %q, want one to %s", what, reload.Length(), h, href)
	}
}

// f3Refusal fails unless page's one banner is the danger msg, with no
// ConflictBanner and no Reload.
func f3Refusal(t *testing.T, what string, page *goquery.Document, msg string) {
	t.Helper()
	if got := bannerTexts(page.Selection, "danger"); !slices.Equal(got, []string{msg}) {
		t.Errorf("%s: danger banners = %q, want [%q]", what, got, msg)
	}
	if got := bannerTexts(page.Selection, "warning"); len(got) != 0 {
		t.Errorf("%s: warning banners = %q, want none", what, got)
	}
	if got := norm(page.Find("#main, main, #grid-alerts").Text()); strings.Contains(got, "Reload") || strings.Contains(got, f2Keep) {
		t.Errorf("%s: a refusal offers Reload or the keep-your-entries line: %q", what, got)
	}
}

// F3 (CD-362): a document page 409 is the ConflictBanner holding focus, and
// Reload goes to the document, which loads.
func TestDocPage_ConflictBanner(t *testing.T) {
	for name, hdr := range f3Hdrs {
		t.Run(name, func(t *testing.T) {
			e := newDocEnv(t, allRights...)
			e.seed(doctype.StatePending)
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionApprove, doc.Result{}, f3Conflict)
			rec := e.post(t, doc42+"/approve", nil, hdr...)
			m03Status(t, rec.Code, http.StatusConflict, "POST approve", rec.Body.String())
			page := uitest.Doc(t, rec)
			f3Banner(t, "approve", page, f3Stale, "Draft 42", doc42)
			f2Focus(t, "approve", page, ".banner--warning")
			m01bOK(t, e.get(t, doc42), "GET the Reload target")
		})
	}
}

// F3 (CD-362): a reason or delete page 409 is the ConflictBanner holding
// focus, Reload goes to that page, and the typed reason and the posted
// version stay until then (CD-94).
func TestReasonPage_ConflictBanner(t *testing.T) {
	pages := []struct {
		action string
		state  doctype.State
		typed  string
		label  string // the document's display in that state
	}{
		{"reject", doctype.StatePending, "Wrong GSTIN", "Draft 42"},
		{"cancel", doctype.StateApproved, "Vendor changed the terms", "SO/26-27/00001"},
		{"close", doctype.StateApproved, "Balance not needed", "SO/26-27/00001"},
		{"delete", doctype.StateDraft, "", "Draft 42"},
	}
	for _, p := range pages {
		for name, hdr := range f3Hdrs {
			t.Run(p.action+" "+name, func(t *testing.T) {
				e := newDocEnv(t, allRights...)
				e.seed(p.state)
				e.docs.SetResult(docscreentest.Sample, 42, doctype.Action(p.action), doc.Result{}, f3Conflict)
				u := doc42 + "/" + p.action
				rec := e.post(t, u, url.Values{"reason": {p.typed}, "version": {"2"}}, hdr...)
				m03Status(t, rec.Code, http.StatusConflict, "POST "+p.action, rec.Body.String())
				page := uitest.Doc(t, rec)
				f3Banner(t, p.action, page, f3Stale, p.label, u)
				f2Focus(t, p.action, page, ".banner--warning")
				if p.typed != "" {
					if got := page.Find(`textarea[name="reason"]`).Text(); strings.TrimSpace(got) != p.typed {
						t.Errorf("reason = %q, want the typed %q kept", got, p.typed)
					}
				}
				if got := page.Find(`input[name="version"]`).AttrOr("value", ""); got != "2" {
					t.Errorf("version = %q, want the posted 2 (CD-94)", got)
				}
				m01bOK(t, e.get(t, u), "GET the Reload target")
			})
		}
	}
}

// With an error summary on the page, the summary holds the one autofocus and
// the ConflictBanner still shows, as in the wizard frame.
func TestConflictBanner_SummaryKeepsFocus(t *testing.T) {
	conflict := &doc.Error{Kind: doc.KindStale, Message: f3Stale, Fields: []doctype.FieldError{{Message: "Line 1 is not allowed."}}}
	for what, c := range map[string]struct {
		state doctype.State
		act   string
		href  string
	}{
		"document page": {doctype.StatePending, "approve", doc42},
		"reason page":   {doctype.StatePending, "reject", doc42 + "/reject"},
	} {
		t.Run(what, func(t *testing.T) {
			e := newDocEnv(t, allRights...)
			e.seed(c.state)
			e.docs.SetResult(docscreentest.Sample, 42, doctype.Action(c.act), doc.Result{}, conflict)
			page := uitest.Doc(t, e.post(t, doc42+"/"+c.act, url.Values{"reason": {"Wrong GSTIN"}}))
			f3Banner(t, what, page, f3Stale, "Draft 42", c.href)
			f := page.Find("[autofocus]")
			if f.Length() != 1 || !f.Is(".error-summary") {
				t.Errorf("%d autofocus (%.200s), want only the error summary", f.Length(), f.AttrOr("class", ""))
			}
		})
	}
}

// Only a stale version is the ConflictBanner: another refusal stays danger
// with no Reload and holds focus, on the document page and the reason pages.
func TestRefusal_StaysDanger(t *testing.T) {
	const msg = "You can't do this now."
	for _, status := range []int{http.StatusForbidden, http.StatusUnprocessableEntity} {
		refusal := statusErr{status: status, msg: msg}
		t.Run("document page", func(t *testing.T) {
			e := newDocEnv(t, allRights...)
			e.seed(doctype.StatePending)
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionApprove, doc.Result{}, refusal)
			rec := e.post(t, doc42+"/approve", nil)
			m03Status(t, rec.Code, status, "POST approve", rec.Body.String())
			page := uitest.Doc(t, rec)
			f3Refusal(t, "approve", page, msg)
			f2Focus(t, "approve", page, ".banner--danger")
		})
		t.Run("reason page", func(t *testing.T) {
			e := newDocEnv(t, allRights...)
			e.seed(doctype.StatePending)
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionReject, doc.Result{}, refusal)
			rec := e.post(t, doc42+"/reject", url.Values{"reason": {"Wrong GSTIN"}})
			m03Status(t, rec.Code, status, "POST reject", rec.Body.String())
			page := uitest.Doc(t, rec)
			f3Refusal(t, "reject", page, msg)
			f2Focus(t, "reject", page, ".banner--danger")
		})
		t.Run("delete page", func(t *testing.T) {
			e := newDocEnv(t, allRights...)
			e.seed(doctype.StateDraft)
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionDelete, doc.Result{}, refusal)
			rec := e.post(t, doc42+"/delete", url.Values{})
			m03Status(t, rec.Code, status, "POST delete", rec.Body.String())
			page := uitest.Doc(t, rec)
			f3Refusal(t, "delete", page, msg)
			f2Focus(t, "delete", page, ".banner--danger")
		})
	}
}

// F3: only a stale version is the ConflictBanner. A refused or blocked 409
// can't be fixed by reloading, so it stays danger on both pages.
func TestRefused409_StaysDanger(t *testing.T) {
	const msg = "Can't do this now."
	for name, err := range map[string]error{
		"refused":                  doc.Refused(msg),
		"blocked":                  &doc.Error{Kind: doc.KindBlocked, Message: msg},
		"a bare 409 from a module": statusErr{status: http.StatusConflict, msg: msg},
	} {
		t.Run(name, func(t *testing.T) {
			e := newDocEnv(t, allRights...)
			e.seed(doctype.StatePending)
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionApprove, doc.Result{}, err)
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionReject, doc.Result{}, err)
			rec := e.post(t, doc42+"/approve", nil)
			m03Status(t, rec.Code, http.StatusConflict, "POST approve", rec.Body.String())
			f3Refusal(t, "approve", uitest.Doc(t, rec), msg)
			rec = e.post(t, doc42+"/reject", url.Values{"reason": {"Wrong GSTIN"}})
			m03Status(t, rec.Code, http.StatusConflict, "POST reject", rec.Body.String())
			f3Refusal(t, "reject", uitest.Doc(t, rec), msg)
		})
	}
}

// The real engine's blocked cancel keeps its danger banner and blocker link.
func TestReasonPage_BlockedKeepsItsLink(t *testing.T) {
	alert, _ := cancelBlocked(t, nil)
	if !alert.Is(".banner--danger") || alert.Find("a").Length() != 1 {
		t.Errorf("blocked cancel: class %q with %d links, want the danger banner with the blocker link", alert.AttrOr("class", ""), alert.Find("a").Length())
	}
	if strings.Contains(alert.Text(), "Reload") {
		t.Errorf("blocked cancel offers Reload: %q", norm(alert.Text()))
	}
}

// CD-381: on a draft, a refused 409 that isn't a state refusal (a fake here,
// since a state refusal needs a real document past draft: see
// TestRefused409_PastDraft) is a danger banner holding focus.
func TestRefused409_WizardAndGrid(t *testing.T) {
	const msg = "Can't do this now."
	refused := doc.Refused(msg)
	valid := url.Values{"supplier_id": {"7"}, "order_date": {"24-10-2026"}, "priority": {"Normal"}, "remarks": {"typed"}, "version": {"3"}}
	t.Run("new", func(t *testing.T) {
		e := newFormEnv(t, append(allRights, "create")...)
		e.docs.SetResult(docscreentest.Sample, 0, doctype.ActionSave, doc.Result{}, refused)
		rec := e.post(t, samplePath+"/new", valid)
		m03Status(t, rec.Code, http.StatusConflict, "POST new", rec.Body.String())
		page := uitest.Doc(t, rec)
		f3Refusal(t, "new", page, msg)
		f2Focus(t, "new", page, ".banner--danger")
	})
	t.Run("step with a field error", func(t *testing.T) {
		e := newFormEnv(t, allRights...)
		e.seed(doctype.StateDraft)
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{}, &doc.Error{Kind: doc.KindRefused, Message: msg, Fields: []doctype.FieldError{{Field: "remarks", Message: "Remarks are wrong."}}})
		rec := e.post(t, doc42+"/edit/details", valid)
		m03Status(t, rec.Code, http.StatusConflict, "POST details", rec.Body.String())
		page := uitest.Doc(t, rec)
		f3Refusal(t, "step with a field error", page, msg)
		if f := page.Find("[autofocus]"); f.Length() != 1 || !f.Is(".error-summary") {
			t.Errorf("%d autofocus, want only the error summary", f.Length())
		}
	})
	t.Run("grid", func(t *testing.T) {
		e := newLinesEnv(t, manyLines(2))
		page := uitest.Doc(t, e.get(t, doc42+"/edit/items"))
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{}, refused)
		rec := gridPost(t, e, page, 2, "5", map[string]string{"grid-2-qty": "25"})
		m03Status(t, rec.Code, http.StatusConflict, "grid POST", rec.Body.String())
		got := uitest.Doc(t, rec)
		f3Refusal(t, "grid", got, msg)
		f2Focus(t, "grid", got, ".banner--danger")
	})
	// Only a stale version's message outranks the row's cells: this refusal's
	// field error is at its cell, so no banner repeats it.
	t.Run("grid with a field error", func(t *testing.T) {
		e := newLinesEnv(t, manyLines(2))
		page := uitest.Doc(t, e.get(t, doc42+"/edit/items"))
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{}, &doc.Error{Kind: doc.KindRefused, Message: msg, Fields: []doctype.FieldError{{Field: "qty", Line: 2, Message: "Quantity is wrong."}}})
		rec := gridPost(t, e, page, 2, "5", map[string]string{"grid-2-qty": "25"})
		m03Status(t, rec.Code, http.StatusConflict, "grid POST", rec.Body.String())
		got := uitest.Doc(t, rec)
		if n := len(bannerTexts(got.Selection, "danger")) + len(bannerTexts(got.Selection, "warning")); n != 0 {
			t.Errorf("%d banners, want none: the error is at its cell", n)
		}
		if !strings.Contains(rec.Body.String(), "Quantity is wrong.") {
			t.Errorf("the cell's error is gone: %.400s", rec.Body)
		}
	})
}

// F3: the document moved past draft between the page's load and the post (a
// real engine, so the record really is submitted). Every step, line, remove and
// move POST is DOC's 409 in a danger banner holding focus, plain and htmx, with
// the typed values and the posted version kept and nothing written; the GET
// still goes to the document page.
func TestRefused409_PastDraft(t *testing.T) {
	type env struct {
		name string
		make func(t *testing.T) (*hookedEnv, int64)
	}
	envs := []env{
		{"generic", func(t *testing.T) (*hookedEnv, int64) { return m01bHooked(t, docscreen.Module{}) }},
		{"module Line", func(t *testing.T) (*hookedEnv, int64) {
			e, id, _ := m03Env(t, nil)
			return e, id
		}},
	}
	for _, en := range envs {
		for mode, hdr := range f3Hdrs {
			for _, c := range []struct {
				path, focus string
				typed       bool
			}{
				{"/edit/terms", "#wizard-danger", false},
				{"/lines/1", "#line-alert", true},
				{"/lines/new", "#line-alert", true},
				{"/lines/1/remove", "#line-alert", false},
				{"/lines/1/move?dir=down", "#wizard-danger", false},
				{"/lines/2/move?dir=down", "#wizard-danger", false}, // the last line: a move that changes nothing
			} {
				t.Run(en.name+" "+mode+" "+c.path, func(t *testing.T) {
					e, id := en.make(t)
					base := m01bURL(id)
					version := m01bVersion(t, e, id)
					form := url.Values{"version": {version}}
					if c.typed {
						form = e.m01bLine1(version)
						form.Set("qty", "9")
					}
					m01bSubmit(t, e, id)
					before := m01bRecord(t, e, id)
					msg := m56Refusal(t, e, id, before.Version)
					rec := e.post(t, base+c.path, form, hdr...)
					m03Status(t, rec.Code, http.StatusConflict, "POST "+c.path, rec.Body.String())
					page := uitest.Doc(t, rec)
					f3Refusal(t, c.path, page, msg)
					if got := page.Find(c.focus); got.Length() != 1 || !got.Is("[autofocus]") || got.Find(".banner--danger").Length() != 1 {
						t.Errorf("%s: no %s holding focus around the danger banner", c.path, c.focus)
					}
					if f := page.Find("[autofocus]"); f.Length() != 1 {
						t.Errorf("%s: %d autofocus, want the banner's only", c.path, f.Length())
					}
					if got := page.Find("title").Text(); mode == "full" && !strings.HasPrefix(got, "Error: ") {
						t.Errorf("%s: title = %q, want the page marked as an error", c.path, got)
					}
					if c.typed {
						if got := page.Find(`[name="qty"]`).AttrOr("value", ""); got != "9" {
							t.Errorf("qty = %q, want the typed 9 kept", got)
						}
					}
					if c.typed || strings.HasSuffix(c.path, "/remove") {
						if got := page.Find(`input[name="version"]`).AttrOr("value", ""); got != version {
							t.Errorf("version = %q, want the posted %s (CD-94)", got, version)
						}
					}
					m03Unchanged(t, e, id, before, "a refused POST")
					if strings.HasPrefix(c.path, "/lines/") && !strings.Contains(c.path, "move") {
						if got := e.get(t, base+strings.TrimSuffix(c.path, "/remove")); got.Code != http.StatusSeeOther || got.Header().Get("Location") != base {
							t.Errorf("GET %s = %d to %q, want the redirect to the document", c.path, got.Code, got.Header().Get("Location"))
						}
					}
				})
			}
		}
	}
}

// F3: the grid path on a real document submitted between load and post: an
// htmx save of a row on fixture.Order, whose lines are a grid, is DOC's 409 in
// the grid's danger banner holding focus, with the typed value and no Reload.
func TestRefused409_PastDraftGrid(t *testing.T) {
	h := fixturetest.New(t)
	order := h.ApprovedOrder(t, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1200.000", Rate: "62.5000"})
	e := scopeMount(t, h, h.Engine, h.Auth, h.Admin)
	version := strconv.Itoa(order.Version)
	_, refusal := h.Engine.Do(t.Context(), h.Admin, fixture.Order, order.ID, doc.ActionSave, doc.ActionInput{Version: order.Version, Only: []string{}})
	if refusal == nil {
		t.Fatal("the engine saved an approved order, want its refusal")
	}
	rec := e.post(t, "/doc/"+fixture.Order+"/"+itoa(order.ID)+"/lines/1", url.Values{
		"version": {version}, "item_id": {itoa(h.M.Item["MS-ANGLE-50"])}, "qty": {"7.000"}, "rate": {"62.5000"}, "uom": {"kg"},
	}, "HX-Request", "true")
	m03Status(t, rec.Code, http.StatusConflict, "grid POST", rec.Body.String())
	page := uitest.Doc(t, rec)
	f3Refusal(t, "grid", page, refusal.Error())
	if got := page.Find("#grid-alert"); got.Length() != 1 || !got.Is("[autofocus]") || got.Find(".banner--danger").Length() != 1 {
		t.Errorf("no #grid-alert holding focus around the danger banner: %.400s", rec.Body)
	}
	if got := page.Find("#grid-1-qty").AttrOr("value", ""); got != "7.000" {
		t.Errorf("grid-1-qty = %q, want the typed 7.000 kept", got)
	}
}

// F3: only a 409 redraws past draft. Any other refusal of a line POST there
// goes where a GET does.
func TestRefused_Non409PastDraftRedirects(t *testing.T) {
	for _, path := range []string{"/lines/1/move?dir=down", "/lines/1/remove"} {
		e := newLinesEnv(t, manyLines(2))
		docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateApproved), manyLines(2))
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{}, statusErr{status: http.StatusForbidden, msg: "No."})
		rec := e.post(t, doc42+path, url.Values{"version": {"5"}})
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != doc42 {
			t.Errorf("POST %s = %d to %q, want 303 to the document", path, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// goneEngine refuses as told, and from then on the document can't be read: it
// was deleted between the refusal and the redraw.
type goneEngine struct {
	*docfake.Fake
	gone *bool
}

func (g goneEngine) Do(ctx context.Context, p auth.Principal, name string, id int64, a doc.Action, in doc.ActionInput) (doc.Result, error) {
	res, err := g.Fake.Do(ctx, p, name, id, a, in)
	*g.gone = err != nil
	return res, err
}

func (g goneEngine) Load(ctx context.Context, tx *db.Tx, name string, id int64) (doc.Record, error) {
	if *g.gone {
		return doc.Record{}, doctype.ErrNotFound
	}
	return g.Fake.Load(ctx, tx, name, id)
}

// A document gone by the time a 409 redraws is the 404, not a page of nothing.
func TestRefused409_DocumentGone(t *testing.T) {
	for _, path := range []string{"/lines/1/move?dir=down", "/lines/1/remove"} {
		e := newLinesEnv(t, manyLines(2))
		gone := false
		e.d.Engine = goneEngine{e.docs, &gone}
		e.remount()
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{}, doc.Refused("No."))
		rec := e.post(t, doc42+path, url.Values{"version": {"5"}})
		m03Status(t, rec.Code, http.StatusNotFound, "POST "+path, rec.Body.String())
	}
}

// An error that isn't a refusal stays the plain internal error.
func TestReorder_InternalError(t *testing.T) {
	e := newLinesEnv(t, manyLines(2))
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{}, errors.New("disk gone"))
	rec := e.post(t, doc42+"/lines/1/move?dir=down", url.Values{"version": {"5"}})
	m03Status(t, rec.Code, http.StatusInternalServerError, "POST move", rec.Body.String())
}

// CD-381: a refused move with a message only, of any status, is a danger
// banner; it never was a stale page.
func TestMove_Refused422StaysDanger(t *testing.T) {
	const msg = "Can't move this line."
	e := newLinesEnv(t, manyLines(2))
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{}, statusErr{status: http.StatusUnprocessableEntity, msg: msg})
	rec := e.post(t, doc42+"/lines/1/move?dir=down", url.Values{"version": {"5"}})
	m03Status(t, rec.Code, http.StatusUnprocessableEntity, "POST move", rec.Body.String())
	f3Refusal(t, "move 422", uitest.Doc(t, rec), msg)
}

// doc.IsStale is the one test every path uses, through a wrap too; a plain
// 409 from anything else isn't stale.
// The grid's stale banner holds focus too.
func TestGrid_StaleBannerHoldsFocus(t *testing.T) {
	e := newLinesEnv(t, manyLines(2))
	page := uitest.Doc(t, e.get(t, doc42+"/edit/items"))
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{}, f3Conflict)
	rec := gridPost(t, e, page, 2, "5", map[string]string{"grid-2-qty": "25"})
	m03Status(t, rec.Code, http.StatusConflict, "grid POST", rec.Body.String())
	f2Focus(t, "grid", uitest.Doc(t, rec), ".banner--warning")
}

func TestClassify_Stale(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{f3Conflict, true},
		{fmt.Errorf("saving: %w", f3Conflict), true},
		{doc.Refused("no"), false},
		{&doc.Error{Kind: doc.KindBlocked, Message: "no"}, false},
		{statusErr{status: http.StatusConflict, msg: "no"}, false},
	} {
		if pr, ok := docscreen.Classify(c.err); !ok || pr.Stale != c.want {
			t.Errorf("Classify(%v) = %+v, %v; want Stale %v", c.err, pr, ok, c.want)
		}
	}
}
