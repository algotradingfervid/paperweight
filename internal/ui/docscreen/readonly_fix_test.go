package docscreen_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// m15Render renders c to a string.
func m15Render(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestM15_StateBadgeLabel: a label replaces the state's word and keeps its
// look; "" renders exactly StateBadge (CD-263 G7).
func TestM15_StateBadgeLabel(t *testing.T) {
	got := m15Render(t, components.StateBadgeLabel(doctype.StateApproved, "Submitted"))
	if !strings.Contains(got, `class="badge badge--approved"`) || !strings.HasSuffix(got, " Submitted</span>") || strings.Contains(got, "Approved") {
		t.Errorf("StateBadgeLabel(approved, Submitted) = %s, want the approved badge reading Submitted", got)
	}
	if got := m15Render(t, components.StateBadgeLabel("unknown", "Odd")); got != `<span class="badge badge--neutral">Odd</span>` {
		t.Errorf("StateBadgeLabel(unknown, Odd) = %s, want the neutral badge reading Odd", got)
	}
	for _, s := range []doctype.State{doctype.StateDraft, doctype.StateApproved, "unknown"} {
		if a, b := m15Render(t, components.StateBadgeLabel(s, "")), m15Render(t, components.StateBadge(s)); a != b {
			t.Errorf("StateBadgeLabel(%s, \"\") = %s, want StateBadge's %s", s, a, b)
		}
	}
}

// m15Submitter is a user whose only rights on fixture.Order are read and
// submit (all): they may submit and withdraw an order but not change it.
func m15Submitter(t *testing.T, e *m15OrderEnv) auth.Principal {
	t.Helper()
	if err := e.a.CreateRole(t.Context(), e.fx.Admin, "M15 Submitter"); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := e.a.SetRights(t.Context(), e.fx.Admin, "M15 Submitter", fixture.Order, map[auth.Right]auth.Scope{"read": auth.ScopeAll, "submit": auth.ScopeAll}); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	return m15User(t, e.a, e.fx.Admin, "m15-submitter@vardhan.test", "M15 Submitter")
}

// m15ReaderAfter fails unless rec is a 200 (or want) document page for a
// user without write, with no step form, and HX-Push-Url push ("" = none
// checked).
func m15ReaderAfter(t *testing.T, rec *httptest.ResponseRecorder, want int, push, what string) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s = %d, want %d; body %.300s", what, rec.Code, want, rec.Body)
	}
	page := uitest.Doc(t, rec)
	if page.Find(".doc-layout").Length() == 0 || page.Find("#wizard, #wizard-form").Length() != 0 {
		t.Errorf("%s: want the read-only document page, not the wizard; main %.300s", what, norm(page.Find("main").Text()))
	}
	if push != "" {
		if got := rec.Header().Get("HX-Push-Url"); got != push {
			t.Errorf("%s: HX-Push-Url = %q, want %q", what, got, push)
		}
	}
}

// Review fix (CD-263 G1): an htmx action that leads a writer to the check
// page (Amend, Withdraw) leads a user without write to the read-only
// document page, and a refused submit shows them the refusal there.
func TestM15_ReaderAfterHTMXActions(t *testing.T) {
	e := m15NewOrderEnv(t)
	u := m15DocURL(fixture.Order, e.id)

	// Amend by Ravi (Purchase Manager: amend all, no write) on a cancelled order.
	e.fx.ForceState(t, fixture.Order, e.id, doc.StateCancelled, "PO-M15")
	e.who = m15Load(t, e.a, e.fx.Ravi.ID)
	v := strconv.Itoa(e.fx.Record(t, fixture.Order, e.id).Version)
	rec := e.post(t, u+"/amend", url.Values{"version": {v}}, "HX-Request", "true")
	amended := e.id + 1
	if got := e.fx.Info(t, fixture.Order, amended).State; got != doctype.StateDraft {
		t.Fatalf("the amendment is %s, want a draft", got)
	}
	m15ReaderAfter(t, rec, http.StatusOK, m15DocURL(fixture.Order, amended), "Ravi's htmx amend")

	// Withdraw by a user with submit but not write.
	e.fx.ForceState(t, fixture.Order, e.id, doc.StatePending, "")
	e.who = m15Submitter(t, e)
	rec = e.post(t, u+"/withdraw", url.Values{"version": {v}}, "HX-Request", "true")
	m15ReaderAfter(t, rec, http.StatusOK, u, "a submitter's htmx withdraw")

	// A submit DOC refuses with field errors: the refusal on the document page.
	bad := e.fx.NewOrderWith(t, m15Load(t, e.a, e.fx.Priya.ID), map[string]string{"supplier_id": ""}, fixturetest.OL{Item: "CEMENT-OPC", Qty: "1.000", Rate: "350.0000"})
	bu := m15DocURL(fixture.Order, bad.ID)
	for _, hdr := range [][]string{nil, {"HX-Request", "true"}} {
		rec = e.post(t, bu+"/submit", url.Values{"version": {strconv.Itoa(bad.Version)}}, hdr...)
		m15ReaderAfter(t, rec, http.StatusUnprocessableEntity, "", "a submitter's refused submit")
		if uitest.Doc(t, rec).Find(".error-summary").Length() == 0 {
			t.Errorf("a submitter's refused submit shows no refusal; main %.300s", norm(uitest.Doc(t, rec).Find("main").Text()))
		}
	}
}

// Review fix (CD-263 G1): a reader's step or line POST whose values can't
// be parsed gets AUTH's write refusal (DOC's 403), not a 422 redraw.
func TestM15_ReaderBadPostIsRefused(t *testing.T) {
	e := m15NewEnv(t)
	u := m15DocURL(docscreentest.Hooked, e.id)
	e.who = e.reader
	v := strconv.Itoa(m02Rec(t, e.hookedEnv, e.id).Version)
	form := url.Values{"item_id": {itoa(e.fx.M.Item["CEMENT-OPC"])}, "qty": {"lots"}, "version": {v}}
	m15Denied(t, e.post(t, u+"/lines/1", form), m15HookedDenied, "reader POST /lines/1 with a bad quantity")

	// Hooked's header has no field that can fail to parse; fixture.Order's
	// order date can.
	o := m15NewOrderEnv(t)
	o.who = m15Load(t, o.a, o.fx.Ravi.ID)
	ou := m15DocURL(fixture.Order, o.id)
	form = url.Values{"order_date": {"not a date"}, "version": {strconv.Itoa(o.fx.Record(t, fixture.Order, o.id).Version)}}
	m15Denied(t, o.post(t, ou+"/edit/details", form), "You don't have permission to change Purchase Order", "Ravi POST /edit/details with a bad date")
}

// m15Denied fails unless rec is a 403 whose main carries AUTH's text.
func m15Denied(t *testing.T, rec *httptest.ResponseRecorder, text, what string) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Errorf("%s = %d, want 403; body %.300s", what, rec.Code, rec.Body)
		return
	}
	if got := norm(uitest.Doc(t, rec).Find("main").Text()); !strings.Contains(got, text) {
		t.Errorf("%s main lacks AUTH's %q; main %.300s", what, text, got)
	}
}

// Review fix: the read-only page after an htmx action answers the module's
// Page error as the document page does (hooks spec §4.4).
func TestM15_ReaderAfterHTMXActionPageError(t *testing.T) {
	e := m15NewEnv(t)
	a := e.d.Auth.(*auth.Auth)
	if err := a.CreateRole(t.Context(), e.fx.Admin, "M15 Hook Submitter"); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := a.SetRights(t.Context(), e.fx.Admin, "M15 Hook Submitter", docscreentest.Hooked, map[auth.Right]auth.Scope{"read": auth.ScopeAll, "submit": auth.ScopeAll}); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	e.who = m15User(t, a, e.fx.Admin, "m15-hook-submitter@vardhan.test", "M15 Hook Submitter")
	e.fx.ForceState(t, docscreentest.Hooked, e.id, doc.StatePending, "")
	m02Mount(e.hookedEnv, docscreen.Module{Page: func(docscreen.Ctx) (docscreen.PageExtra, error) {
		return docscreen.PageExtra{}, errors.New("m15 page hook failed")
	}})
	v := strconv.Itoa(m02Rec(t, e.hookedEnv, e.id).Version)
	rec := e.post(t, m15DocURL(docscreentest.Hooked, e.id)+"/withdraw", url.Values{"version": {v}}, "HX-Request", "true")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("htmx withdraw with a failing Page hook = %d, want 500; body %.300s", rec.Code, rec.Body)
	}
}
