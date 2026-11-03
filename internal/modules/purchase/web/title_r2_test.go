package purchaseweb

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// wantTitle fails unless the page's <title> starts with "Error: " exactly
// when refused (R2, UI v2 §5.14), and otherwise with title.
func wantTitle(t *testing.T, what string, d *goquery.Document, title string, refused bool) {
	t.Helper()
	want := title + " · "
	if refused {
		want = "Error: " + want
	}
	if got := d.Find("title").Text(); !strings.HasPrefix(got, want) {
		t.Errorf("%s: title = %q, want it to start %q", what, got, want)
	}
}

// R2: each PO page's POST redraw carries "Error: " in its title, a 422, a stale
// 409 and a refusal the page itself states alike; its GET page doesn't.
func TestPurchaseWeb_OrderPagesErrorTitle(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	env.SetNow(t, time.Date(2026, time.October, 24, 18, 31, 0, 0, time.UTC))

	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	display := "Draft " + strconv.FormatInt(draft.ID, 10)
	handover := orderPath(draft.ID, "/handover")
	wantTitle(t, "hand over GET", w.get(t, env.Priya, handover).doc, "Hand over "+display, false)
	wantTitle(t, "hand over 422", w.post(t, env.Priya, handover, url.Values{"version": {strconv.Itoa(draft.Version)}}).doc, "Hand over "+display, true)
	wantTitle(t, "hand over 409", w.post(t, env.Priya, handover, url.Values{"version": {strconv.Itoa(draft.Version + 3)}, "to": {strconv.FormatInt(int64(env.Arjun.ID), 10)}}).doc, "Hand over "+display, true)

	pending := purchasetest.NewOrder(t, env, purchasetest.StopAtPending())
	pendingDisplay := "Draft " + strconv.FormatInt(pending.ID, 10)
	got := w.post(t, env.Priya, orderPath(pending.ID, "/handover"), url.Values{"version": {strconv.Itoa(pending.Version)}, "to": {strconv.FormatInt(int64(env.Arjun.ID), 10)}})
	if got.status == http.StatusSeeOther {
		t.Fatalf("hand over a pending PO = 303, want its refusal")
	}
	wantTitle(t, "hand over refused by state", got.doc, "Hand over "+pendingDisplay, true)

	po := purchasetest.NewOrder(t, env)
	send := orderPath(po.ID, "/send")
	wantTitle(t, "send GET", w.get(t, env.Priya, send).doc, "Send "+po.Number, false)
	wantTitle(t, "send 422", w.post(t, env.Priya, send, url.Values{"version": {"0"}, "sent_on": {"25-10-2026"}}).doc, "Send "+po.Number, true)
	expected := orderPath(po.ID, "/expected")
	wantTitle(t, "expected GET", w.get(t, env.Priya, expected).doc, "Expected delivery for "+po.Number, false)
	wantTitle(t, "expected 422", w.post(t, env.Priya, expected, url.Values{"version": {"0"}}).doc, "Expected delivery for "+po.Number, true)

	sent, _ := types.ParseDate("25-10-2026")
	on, _ := types.ParseDate("05-11-2026")
	if err := env.Service.MarkSent(t.Context(), env.Priya, po.ID, 0, purchase.SentInput{SentOn: sent, SentVia: "email"}); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	if err := env.Service.SetExpected(t.Context(), env.Priya, po.ID, 0, on, ""); err != nil {
		t.Fatalf("SetExpected: %v", err)
	}
	wantTitle(t, "undo send 409", w.post(t, env.Priya, send+"/undo", url.Values{"version": {"99"}}).doc, "Send "+po.Number, true)
	wantTitle(t, "clear expected 409", w.post(t, env.Priya, expected+"/clear", url.Values{"version": {"99"}}).doc, "Expected delivery for "+po.Number, true)
}

// R2: Create PO from a request's POST redraws (422, 409) carry "Error: "; its
// GET page doesn't.
func TestPurchaseWeb_FromRequestErrorTitle(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	pr := w.prA(t)
	path := p15dOrderPath(pr.ID)
	wantTitle(t, "GET", w.get(t, env.Priya, path).doc, fromRequestTitle, false)
	v, _ := strconv.Atoi(w.version(t, pr.ID))
	wantTitle(t, "422", w.do(t, env.Priya, http.MethodPost, path, url.Values{"version": {strconv.Itoa(v)}, "vendor_id": {""}}).doc, fromRequestTitle, true)
	wantTitle(t, "409", w.do(t, env.Priya, http.MethodPost, path, url.Values{"version": {strconv.Itoa(v - 1)}, "vendor_id": {w.id(t, "party", "SHREE")}}).doc, fromRequestTitle, true)
}
