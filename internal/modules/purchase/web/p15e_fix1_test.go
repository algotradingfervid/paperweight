package purchaseweb

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// p15eFix1Unreadable is DOC's unreadable-value text for Expected on (P18).
const p15eFix1Unreadable = "Expected on isn't a valid value. Check it and try again."

// dangerBanners is the text of each danger banner on p, less the toast's
// <template> copies every page carries.
func dangerBanners(p p15ePage) []string {
	var out []string
	p.doc.Find(".banner--danger").Each(func(_ int, s *goquery.Selection) {
		if s.ParentsFiltered("template").Length() == 0 {
			out = append(out, squash(s.Text()))
		}
	})
	return out
}

// wantOneDanger fails unless p has exactly one danger banner and it holds
// msg (P15e fix round 1: the state refusal is not shown twice).
func wantOneDanger(t *testing.T, what string, p p15ePage, msg string) {
	t.Helper()
	got := dangerBanners(p)
	if len(got) != 1 || !strings.Contains(got[0], msg) {
		t.Errorf("%s: danger banners = %q, want exactly one holding %q", what, got, msg)
	}
}

// MOD-PO P15e fix round 1 (UX §5.12, §5.13, §5.20): a POST refused for the
// PO's state redraws the page with that refusal in exactly one danger banner,
// not once as the redraw's problem and again as the page's refusal: Send and
// Expected on to a draft (422) and Hand over to a pending PO (409, DOC's
// pending refusal).
func TestPurchaseWeb_StateRefusalOneBanner(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())

	t.Run("send to a draft", func(t *testing.T) {
		got := w.post(t, env.Priya, orderPath(draft.ID, "/send"), url.Values{"version": {"0"}, "sent_on": {"24-10-2026"}, "sent_via": {"email"}})
		if got.status != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422; page %q", got.status, got.text)
		}
		wantOneDanger(t, "send to a draft", got, "Only an approved order can be marked as sent.")
	})
	t.Run("expected on for a draft", func(t *testing.T) {
		got := w.post(t, env.Priya, orderPath(draft.ID, "/expected"), url.Values{"version": {"0"}, "expected_on": {"05-11-2026"}})
		if got.status != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want 422; page %q", got.status, got.text)
		}
		wantOneDanger(t, "expected on for a draft", got, "Only an approved order can have an expected date.")
	})
	t.Run("hand over a pending PO", func(t *testing.T) {
		pending := purchasetest.NewOrder(t, env, purchasetest.StopAtPending())
		got := w.post(t, env.Priya, orderPath(pending.ID, "/handover"), url.Values{
			"version": {strconv.Itoa(pending.Version)}, "to": {strconv.FormatInt(int64(env.Arjun.ID), 10)},
		})
		if got.status != http.StatusConflict {
			t.Errorf("status = %d, want DOC's 409 refusal; page %q", got.status, got.text)
		}
		wantOneDanger(t, "hand over a pending PO", got, "Withdraw this order before handing it over.")
	})
}

// MOD-PO P15e fix round 1 (CD-94): a stale Send or Expected on POST redraws
// with the version the page posted, never the row's current one.
func TestPurchaseWeb_StaleKeepsPostedVersion(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	po := purchasetest.NewOrder(t, env)

	send := w.post(t, env.Priya, orderPath(po.ID, "/send"), url.Values{"version": {"4"}, "sent_on": {"24-10-2026"}, "sent_via": {"email"}})
	if send.status != http.StatusConflict {
		t.Errorf("stale send = %d, want 409; page %q", send.status, send.text)
	}
	if v, _ := send.attr(`input[name=version]`, "value"); v != "4" {
		t.Errorf("stale send: version = %q, want the posted 4", v)
	}
	exp := w.post(t, env.Priya, orderPath(po.ID, "/expected"), url.Values{"version": {"3"}, "expected_on": {"05-11-2026"}})
	if exp.status != http.StatusConflict {
		t.Errorf("stale expected on = %d, want 409; page %q", exp.status, exp.text)
	}
	if v, _ := exp.attr(`input[name=version]`, "value"); v != "3" {
		t.Errorf("stale expected on: version = %q, want the posted 3", v)
	}
}

// MOD-PO P15e fix round 1 (UX §5.19): the filter page's vendor Picker
// searches exactly /pick/Party?role=vendor, and with vendor=<id> chosen it
// shows the vendor's name, not the id.
func TestPurchaseWeb_PendingFilterVendor(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	purchasetest.NewOrder(t, env)
	vendorID := strconv.FormatInt(p15eMasterID(t, env, "party", "SHREE"), 10)

	page := w.get(t, env.Admin, pendingPath+"/filter?vendor="+vendorID)
	if page.status != http.StatusOK {
		t.Fatalf("GET = %d %q", page.status, page.text)
	}
	picker := page.doc.Find(`.picker:has(input[type=hidden][name=vendor])`)
	if picker.Length() != 1 {
		t.Fatalf("vendor pickers = %d, want 1", picker.Length())
	}
	if got := picker.Find(`input[role=combobox]`).AttrOr("hx-get", ""); got != "/pick/"+party.DocType+"?role=vendor" {
		t.Errorf("vendor Picker searches %q, want %q", got, "/pick/"+party.DocType+"?role=vendor")
	}
	if got := squash(picker.Find(`[data-picker-summary]`).Text()); got != "Shree Steel Traders" {
		t.Errorf("chosen vendor = %q, want the name Shree Steel Traders (not the id %s)", got, vendorID)
	}
}

// MOD-PO P15e fix round 1 (UX §5.19): a filtered report with more than 25
// rows keeps the filter in its Next link.
func TestPurchaseWeb_PendingFilteredNext(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	for range purchase.PendingPageSize + 1 {
		purchasetest.NewOrder(t, env)
	}
	vendorID := strconv.FormatInt(p15eMasterID(t, env, "party", "SHREE"), 10)
	page := w.get(t, env.Priya, pendingPath+"?vendor="+vendorID)
	if n := page.doc.Find(".datalist-rows > li").Length(); n != purchase.PendingPageSize {
		t.Fatalf("page 1 = %d rows, want %d", n, purchase.PendingPageSize)
	}
	if next, _ := page.attr(".pagination a[rel=next]", "href"); next != pendingPath+"?vendor="+vendorID+"&page=2" {
		t.Errorf("Next = %q, want %q", next, pendingPath+"?vendor="+vendorID+"&page=2")
	}
}

// MOD-PO P15e fix round 1 (UX §5.20): a PO with no required-by date has no
// "Required by" in the Expected on page's summary.
func TestPurchaseWeb_ExpectedNoRequiredBy(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	po := purchasetest.NewOrder(t, env)
	page := w.get(t, env.Priya, orderPath(po.ID, "/expected"))
	if page.status != http.StatusOK {
		t.Fatalf("GET = %d %q", page.status, page.text)
	}
	if !strings.Contains(page.text, p15eSummary) || strings.Contains(page.text, "Required by") {
		t.Errorf("page = %q, want the summary %q and no Required by", page.text, p15eSummary)
	}
}

// MOD-PO P15e fix round 1 (UX §5.2): the request queues have no Total
// column and no rupee figure; an order queue has the column.
func TestPurchaseWeb_RequestQueuesNoTotal(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	purchasetest.NewRequest(t, env)
	purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())

	heads := func(p p15ePage) []string { return p.texts(".datalist-head span") }
	if got := heads(w.get(t, env.Priya, "/modules/purchase/q/drafts")); !slices.Contains(got, "Total") {
		t.Errorf("drafts columns = %q, want Total", got)
	}
	for _, c := range []struct {
		who  auth.Principal
		name string
	}{{env.Suresh, "my-requests"}, {env.Priya, "requests-to-order"}} {
		page := w.get(t, c.who, "/modules/purchase/q/"+c.name)
		if n := page.doc.Find(".datalist-rows > li").Length(); page.status != http.StatusOK || n != 1 {
			t.Fatalf("%s = %d with %d rows, want 200 with 1", c.name, page.status, n)
		}
		if got := heads(page); slices.Contains(got, "Total") {
			t.Errorf("%s columns = %q, want no Total", c.name, got)
		}
		if strings.Contains(page.text, "₹") {
			t.Errorf("%s has a rupee figure: %q", c.name, page.text)
		}
	}
}

// MOD-PO P15e fix round 1, AD-13 ruling pinned: with no card count but some
// recent comments the home shows the comments. UI v2 §5.12 (UIV2-E3): the
// empty text is now Waiting for you's own state, so it shows beside them
// (comments are not work waiting).
func TestPurchaseWeb_HomeCommentsOnly(t *testing.T) {
	v := homeView{comments: []modkit.Comment{{
		DocType: purchase.DocType, RecordID: 7, Body: "Use Bharat Tools for the bolts", CreatedByName: "Ravi Kulkarni",
		CreatedAt: time.Date(2026, time.October, 24, 5, 0, 0, 0, time.UTC), Display: "Draft 7",
	}}}
	var buf bytes.Buffer
	if err := homePage(v).Render(t.Context(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	d, _ := goquery.NewDocumentFromReader(&buf)
	text := squash(d.Text())
	if !strings.Contains(text, "Recent comments") || !strings.Contains(text, "Use Bharat Tools for the bolts") {
		t.Errorf("home = %q, want the recent comment", text)
	}
	if !strings.Contains(text, layout.HomeCaughtUp) {
		t.Errorf("home with only comments lacks %q: %q", layout.HomeCaughtUp, text)
	}
}

// MOD-PO P15e fix round 1 (UX §5.2): ?page=0 on a queue reads page 1.
func TestPurchaseWeb_QueuePageZero(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	for range purchase.QueuePageSize + 1 {
		purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	}
	page := w.get(t, env.Priya, "/modules/purchase/q/drafts?page=0")
	if n := page.doc.Find(".datalist-rows > li").Length(); page.status != http.StatusOK || n != purchase.QueuePageSize || !strings.Contains(page.text, "1–25 of 26") {
		t.Errorf("page=0 = %d with %d rows, text %q; want page 1: 25 rows and `1–25 of 26`", page.status, n, page.text)
	}
	if next, _ := page.attr(".pagination a[rel=next]", "href"); next != "/modules/purchase/q/drafts?page=2" {
		t.Errorf("page=0 Next = %q, want page 2", next)
	}
}

// MOD-PO P15e fix round 1 (UX §5.13): a 422 redraw whose refusal has no
// field shows it in a danger banner without Reload; only the stale 409 has
// Reload.
func TestPurchaseWeb_ReloadOnlyOnStale(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env)
	h := &handlers{d: Deps{Service: env.Service}}
	const problem = "Something else went wrong."
	path := orderPath(po.ID, "/send")

	redraw := func(status int) p15ePage {
		form := url.Values{"version": {"0"}, "sent_on": {"24-10-2026"}, "sent_via": {"email"}}
		req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), env.Priya), http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rc := chi.NewRouteContext()
		rc.URLParams.Add("id", strconv.FormatInt(po.ID, 10))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
		rec := httptest.NewRecorder()
		h.sendRedraw(rec, req, status, []doctype.FieldError{{Message: problem}})
		d, err := goquery.NewDocumentFromReader(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return p15ePage{status: rec.Code, doc: d, text: squash(d.Text())}
	}

	got := redraw(http.StatusUnprocessableEntity)
	wantOneDanger(t, "422", got, problem)
	if n := got.doc.Find(`.banner--danger a`).Length(); n != 0 {
		t.Errorf("422 redraw has %d links in its danger banner, want no Reload", n)
	}
	got = redraw(http.StatusConflict)
	if d := dangerBanners(got); len(d) != 0 || !hasBanner(got, "warning", problem) {
		t.Errorf("409: danger banners %q, banners %q; want only the warning holding %q", d, banners(got), problem)
	}
	if href, _ := got.attr(".banner--warning a", "href"); href != path || !slices.Contains(got.texts(".banner--warning a"), "Reload "+po.Number) {
		t.Errorf("409 Reload = %q %q, want Reload %s -> %q", got.texts(".banner--warning a"), href, po.Number, path)
	}
}

// MOD-PO P15e fix round 1, CD-259 as ruled for Expected on: the date is
// parsed inside SetExpected after the load, right, version and state checks,
// so a bad date never hides a 403, a 409 or the state refusal.
func TestPurchaseWeb_ExpectedDateAfterChecks(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	po := purchasetest.NewOrder(t, env)
	path := orderPath(po.ID, "/expected")
	bad := url.Values{"version": {"3"}, "expected_on": {"31-02-2026"}}

	t.Run("stale and a bad date: 409", func(t *testing.T) {
		got := w.post(t, env.Priya, path, bad)
		if got.status != http.StatusConflict || !hasBanner(got, "warning", strings.Replace(p15eStaleText, "%s", po.Number, 1)) {
			t.Errorf("stale + 31-02-2026 = %d, banners %q; want 409 with the stale text", got.status, banners(got))
		}
		if msg, _ := fieldError(got, "expected_on"); msg != "" || strings.Contains(got.text, p15eFix1Unreadable) {
			t.Errorf("stale + 31-02-2026 shows the date message: %q", got.text)
		}
	})
	t.Run("no right and a bad date: 403", func(t *testing.T) {
		if got := w.post(t, env.Suresh, path, url.Values{"version": {"0"}, "expected_on": {"31-02-2026"}}); got.status != http.StatusForbidden {
			t.Errorf("Suresh + 31-02-2026 = %d %q, want 403", got.status, got.text)
		}
	})
	t.Run("closed and a bad date: the state refusal", func(t *testing.T) {
		closed := purchasetest.NewOrder(t, env)
		if _, err := env.Engine.Do(t.Context(), env.Priya, purchase.DocType, closed.ID, doc.ActionClose,
			doc.ActionInput{Version: closed.Version, Reason: "Balance not needed"}); err != nil {
			t.Fatalf("setup: close: %v", err)
		}
		got := w.post(t, env.Priya, orderPath(closed.ID, "/expected"), url.Values{"version": {"0"}, "expected_on": {"31-02-2026"}})
		if got.status != http.StatusUnprocessableEntity {
			t.Errorf("closed + 31-02-2026 = %d, want 422", got.status)
		}
		wantOneDanger(t, "closed + 31-02-2026", got, "Only an approved order can have an expected date.")
		if strings.Contains(got.text, p15eFix1Unreadable) {
			t.Errorf("closed + 31-02-2026 shows the date message: %q", got.text)
		}
	})
	t.Run("blank and bad dates stay distinct", func(t *testing.T) {
		wantFieldError(t, "blank", w.post(t, env.Priya, path, url.Values{"version": {"0"}, "expected_on": {"  "}}), "expected_on", "Enter the expected date.")
		wantFieldError(t, "bad", w.post(t, env.Priya, path, url.Values{"version": {"0"}, "expected_on": {"31-02-2026"}}), "expected_on", p15eFix1Unreadable)
	})
}
