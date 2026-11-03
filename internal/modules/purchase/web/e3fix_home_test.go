package purchaseweb

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// e3Vendor is purchasetest.NewOrder's vendor as DOC's party shows it.
const e3Vendor = "SHREE Shree Steel Traders"

// e3Reject submits a draft PO as Priya and has Ravi reject it with reason.
func e3Reject(t *testing.T, env *purchasetest.Env, reason string) doc.Doc {
	t.Helper()
	d := purchasetest.NewOrder(t, env, purchasetest.StopAtPending())
	if _, err := env.Engine.Do(t.Context(), env.Ravi, purchase.DocType, d.ID, doctype.ActionReject, doc.ActionInput{Version: d.Version, Reason: reason}); err != nil {
		t.Fatalf("setup: reject: %v", err)
	}
	return d
}

// e3RowText is the text of the row in the section headed title that links
// to href, "" when there is none.
func e3RowText(p p15ePage, title, href string) string {
	return squash(p.doc.Find("section.homesect:has(h2:contains('" + title + "')) .datalist-link[href='" + href + "']").Text())
}

// CD-368 (1): Waiting for you lists the viewer's rejected drafts with
// `Needs changes · "{reason}"` and the approved orders they may send with
// `Not sent to the vendor yet`, after the approvals, at most 3 in all;
// each with its exact total. Restricted: Priya (PRJ-METRO) has no row for a
// rejected draft or an approved order moved to PRJ-TECHPARK.
func TestPurchaseWeb_HomeWaitingChangesAndToSend(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	rejected := e3Reject(t, env, "Wrong vendor")
	purchasetest.NewOrder(t, env, purchasetest.StopAtDraft()) // a plain draft isn't waiting

	// Rejected drafts alone: no See all, which counts the approvals.
	page := w.get(t, env.Priya, homePath)
	if got := e3Rows(page, "Waiting for you"); !slices.Equal(got, []string{draftTitle(rejected.ID)}) {
		t.Errorf("Waiting = %q, want only the rejected draft", got)
	}
	if n := page.doc.Find("section.homesect:has(h2:contains('Waiting for you')) .homesect-head a").Length(); n != 0 {
		t.Errorf("no approvals waiting, but %d See all links", n)
	}

	approved := purchasetest.NewOrder(t, env)
	page = w.get(t, env.Priya, homePath)
	if got := e3Rows(page, "Waiting for you"); !slices.Equal(got, []string{draftTitle(rejected.ID), approved.Number}) {
		t.Errorf("Waiting = %q, want the rejected draft then the order to send", got)
	}
	if got := e3RowText(page, "Waiting for you", docHref(purchase.DocType, rejected.ID)); !strings.Contains(got, e3Total) || !strings.Contains(got, e3Vendor) || !strings.Contains(got, `Needs changes · "Wrong vendor"`) {
		t.Errorf("rejected row = %q, want %s, %s and Needs changes · \"Wrong vendor\"", got, e3Total, e3Vendor)
	}
	if got := e3RowText(page, "Waiting for you", docHref(purchase.DocType, approved.ID)); !strings.Contains(got, e3Total) || !strings.Contains(got, e3Vendor) || !strings.Contains(got, "Not sent to the vendor yet") {
		t.Errorf("to-send row = %q, want %s, %s and Not sent to the vendor yet", got, e3Total, e3Vendor)
	}
	if strings.Contains(page.text, layout.HomeCaughtUp) {
		t.Error("work waiting but the home says all caught up")
	}

	// At most 3 rows: rejected drafts come before the orders to send.
	second := e3Reject(t, env, "Use the rate contract")
	purchasetest.NewOrder(t, env)
	page = w.get(t, env.Priya, homePath)
	if got := e3Rows(page, "Waiting for you"); len(got) != 3 || got[2] != approved.Number || !slices.Contains(got[:2], draftTitle(second.ID)) {
		t.Errorf("Waiting = %q, want the two rejected drafts then the first order to send", got)
	}

	// Restricted: neither moved record is shown.
	e3MoveToTechPark(t, env, "purchase_order", rejected.ID)
	e3MoveToTechPark(t, env, "purchase_order", second.ID)
	e3MoveToTechPark(t, env, "purchase_order", approved.ID)
	page = w.get(t, env.Priya, homePath)
	for _, hidden := range []string{draftTitle(rejected.ID), draftTitle(second.ID), approved.Number} {
		if strings.Contains(page.text, hidden) {
			t.Errorf("restricted: the home names the unreadable %s", hidden)
		}
	}
	if got := e3Rows(page, "Waiting for you"); len(got) != 1 {
		t.Errorf("restricted: Waiting = %q, want only the order still on PRJ-METRO", got)
	}
}

// A rejected draft without a reason says only "Needs changes".
func TestPurchaseWeb_HomeChangesNoReason(t *testing.T) {
	hp := homeProps(homeView{changes: []purchase.HomeRow{{DocType: purchase.DocType, ID: 3, Title: "Draft 3", State: doctype.StateRejected}}})
	if got := hp.Waiting.Rows[0].Values["m0"]; got != "Needs changes" {
		t.Errorf("meta = %q, want Needs changes", got)
	}
}

// newE3Web is newP15eWeb with ListHidden standing in for the app's
// registered Hide.
func newE3Web(t *testing.T, env *purchasetest.Env, hidden func(r *http.Request, dt *doctype.DocType) func(string) bool) *p15eWeb {
	t.Helper()
	sm := scs.New()
	r := chi.NewRouter()
	Routes(Deps{
		Service: env.Service, Approvals: env.Approvals, Comments: env.Comments, Sessions: sm,
		CSRF:       func(*http.Request) string { return "p15e-token" },
		ListHidden: hidden,
	})(r)
	h := sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.ServeHTTP(w, req.WithContext(web.WithSessions(req.Context(), sm)))
	}))
	return &p15eWeb{env: env, h: h}
}

// CD-368 (3): Recent and the waiting rows ask the registered Hide, as the
// list does, for each value they show: with the PO's rounded total, vendor
// and state hidden, the rows show no total, vendor or state badge.
func TestPurchaseWeb_HomeRecentHide(t *testing.T) {
	env := p15eRuleEnv(t)
	dt, _ := doctype.Lookup(purchase.DocType)
	party, _ := doc.PartyField(dt)
	var asked []string
	w := newE3Web(t, env, func(r *http.Request, d *doctype.DocType) func(string) bool {
		return func(name string) bool {
			asked = append(asked, d.Name+"."+name)
			return d.Name == purchase.DocType && (name == "rounded_total" || name == party.Name || name == "state")
		}
	})
	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	rejected := e3Reject(t, env, "Wrong vendor")

	page := w.get(t, env.Priya, homePath)
	for _, c := range []struct{ title, href string }{
		{"Recent", docHref(purchase.DocType, draft.ID)},
		{"Waiting for you", docHref(purchase.DocType, rejected.ID)},
	} {
		got := e3RowText(page, c.title, c.href)
		if got == "" {
			t.Errorf("%s: no row for %s", c.title, c.href)
		}
		for _, leak := range []string{"₹", "Shree", "Rejected", "Pending"} {
			if strings.Contains(got, leak) {
				t.Errorf("%s row %q shows %q, which Hide hides", c.title, got, leak)
			}
		}
	}
	if n := page.doc.Find("section.homesect .badge").Length(); n != 0 {
		t.Errorf("state hidden but the home rows show %d state badges", n)
	}
	if !slices.Contains(asked, purchase.DocType+".rounded_total") {
		t.Errorf("Hide was asked %q, never about rounded_total", asked)
	}
}

// CD-368 (3): the approval rows go through the same Hide check: with the
// PO's rounded total and vendor hidden, Ravi's waiting row shows neither.
func TestPurchaseWeb_HomeInboxHide(t *testing.T) {
	env := p15eRuleEnv(t)
	dt, _ := doctype.Lookup(purchase.DocType)
	party, _ := doc.PartyField(dt)
	w := newE3Web(t, env, func(r *http.Request, d *doctype.DocType) func(string) bool {
		return func(name string) bool {
			return d.Name == purchase.DocType && (name == "rounded_total" || name == party.Name)
		}
	})
	pending := purchasetest.NewOrder(t, env, purchasetest.StopAtPending())
	got := e3RowText(w.get(t, env.Ravi, homePath), "Waiting for you", docHref(purchase.DocType, pending.ID))
	if !strings.Contains(got, "Needs your approval") {
		t.Fatalf("no approval row for %d: %q", pending.ID, got)
	}
	if strings.Contains(got, "₹") || strings.Contains(got, "Shree") {
		t.Errorf("approval row %q shows a hidden total or vendor", got)
	}
}

// CD-368 (3): a hidden Request state alone also drops the state column.
func TestPurchaseWeb_HomeRequestStateHidden(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newE3Web(t, env, func(r *http.Request, d *doctype.DocType) func(string) bool {
		return func(name string) bool { return d.Name == purchase.RequestDocType && name == "state" }
	})
	purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	page := w.get(t, env.Priya, homePath)
	if len(e3Rows(page, "Recent")) == 0 {
		t.Fatal("no Recent rows")
	}
	if n := page.doc.Find("section.homesect .badge").Length(); n != 0 {
		t.Errorf("Request state hidden but the home rows show %d state badges", n)
	}
}

// Routes refuses to build without ListHidden, like any missing dependency,
// so the app can't drop its Hide wiring and serve the home unchecked.
func TestPurchaseWeb_RoutesNeedListHidden(t *testing.T) {
	defer func() {
		if got := recover(); got != "purchaseweb.Routes: Deps.ListHidden is nil" {
			t.Errorf("Routes(Deps{}) recovered %v, want the ListHidden panic", got)
		}
	}()
	Routes(Deps{})
}

// CD-368 (3) ruling (R4 review): with the PO state hidden, an approved order
// leaves Waiting for you: "Not sent to the vendor yet" would name its state.
func TestPurchaseWeb_HomeToSendStateHidden(t *testing.T) {
	env := p15eRuleEnv(t)
	hideState := false
	w := newE3Web(t, env, func(r *http.Request, d *doctype.DocType) func(string) bool {
		return func(name string) bool { return hideState && d.Name == purchase.DocType && name == "state" }
	})
	approved := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved())
	href := docHref(purchase.DocType, approved.ID)
	if got := e3RowText(w.get(t, env.Priya, homePath), "Waiting for you", href); !strings.Contains(got, "Not sent to the vendor yet") {
		t.Fatalf("control: to-send row = %q", got)
	}
	hideState = true
	page := w.get(t, env.Priya, homePath)
	if got := e3RowText(page, "Waiting for you", href); got != "" {
		t.Errorf("state hidden: to-send row = %q, want none", got)
	}
	if strings.Contains(page.text, "Not sent to the vendor yet") {
		t.Error("state hidden: the home still says Not sent to the vendor yet")
	}
}
