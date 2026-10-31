package purchaseweb

import (
	"bytes"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/web"
)

const (
	p15eSummary   = "Shree Steel Traders · ₹7,375.00" // the default PO: 100 kg × ₹62.50 + 18% GST
	p15eStaleText = "Someone else changed %s after you opened it. Reload it and make your changes again."
)

func orderPath(id int64, tail string) string {
	return "/modules/purchase/orders/" + strconv.FormatInt(id, 10) + tail
}

// fieldError is the message shown at field name and whether the error
// summary links to it.
func fieldError(p p15ePage, name string) (msg string, linked bool) {
	id := "f-" + name
	msg = squash(p.doc.Find("#" + id + "-msg.field__msg--error").Text())
	linked = p.doc.Find(`.error-summary a[href="#`+id+`"]`).Length() == 1
	return msg, linked
}

// wantFieldError fails unless msg is at field name and in the error summary.
func wantFieldError(t *testing.T, what string, p p15ePage, name, msg string) {
	t.Helper()
	got, linked := fieldError(p, name)
	if got != msg || !linked {
		t.Errorf("%s: at %s %q (summary link %v), want %q in both; page %q", what, name, got, linked, msg, p.text)
	}
}

// banners is the text of each banner on p.
func banners(p p15ePage) []string { return p.texts(".banner") }

func hasBanner(p p15ePage, kind, msg string) bool {
	return slices.ContainsFunc(p.texts(".banner--"+kind), func(s string) bool { return strings.Contains(s, msg) })
}

func wantFlash(t *testing.T, what string, p p15ePage, location, text string) {
	t.Helper()
	if p.status != http.StatusSeeOther || p.location != location {
		t.Errorf("%s = %d to %q, want 303 to %q; page %q", what, p.status, p.location, location, p.text)
	}
	if want := []web.Notice{{Level: "success", Text: text}}; !slices.Equal(p.flash, want) {
		t.Errorf("%s: flash %+v, want %+v", what, p.flash, want)
	}
}

func poURL(id int64) string { return "/doc/" + purchase.DocType + "/" + strconv.FormatInt(id, 10) }

// MOD-PO P15e, PUX-009 (UX §5.12; spec §21.2; plan P10): the hand-over page
// shows the summary and the eligible people as radios with their roles; each
// refusal redraws the page with the message at the radios (or the note) and
// in the error summary, the choice and note kept; a stale post is 409 with
// DOC's text and Reload; success flashes `{display} is now {name}'s.`; a
// pending PO shows the refusal and no form.
func TestPurchaseWeb_HandOverPage(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	display := "Draft " + strconv.FormatInt(po.ID, 10)
	path := orderPath(po.ID, "/handover")

	page := w.get(t, env.Priya, path)
	if page.status != http.StatusOK {
		t.Fatalf("GET = %d %q", page.status, page.text)
	}
	if h1 := page.texts("h1"); !slices.Equal(h1, []string{"Hand over " + display}) {
		t.Errorf("h1 = %q", h1)
	}
	if !strings.Contains(page.text, p15eSummary+" · Owned by Priya Shah") {
		t.Errorf("no summary %q in %q", p15eSummary+" · Owned by Priya Shah", page.text)
	}
	var choices []string
	page.doc.Find(`input[type=radio][name=to]`).Each(func(_ int, s *goquery.Selection) {
		v, _ := s.Attr("value")
		choices = append(choices, v+" "+squash(s.Parent().Text()))
	})
	wantChoices := []string{
		strconv.FormatInt(int64(env.Admin.ID), 10) + " Anil Kapoor Administrator",
		strconv.FormatInt(int64(env.Arjun.ID), 10) + " Arjun Mehta Employee, Purchase User",
	}
	if !slices.Equal(choices, wantChoices) {
		t.Errorf("choices = %q, want %q", choices, wantChoices)
	}
	if v, _ := page.attr(`input[name=version]`, "value"); v != strconv.Itoa(po.Version) {
		t.Errorf("version = %q, want %d", v, po.Version)
	}
	if !strings.Contains(page.text, "Hand over to") || !strings.Contains(page.text, "Posted as a comment on the order.") {
		t.Errorf("no legend or note hint in %q", page.text)
	}
	if form, _ := page.attr("form[method=post]", "action"); form != path {
		t.Errorf("form action = %q, want %q", form, path)
	}

	post := func(to int64, note string, version int) p15ePage {
		f := url.Values{"version": {strconv.Itoa(version)}, "note": {note}}
		if to != 0 {
			f.Set("to", strconv.FormatInt(to, 10))
		}
		return w.post(t, env.Priya, path, f)
	}

	t.Run("nobody picked", func(t *testing.T) {
		got := post(0, "Over to you", po.Version)
		if got.status != http.StatusUnprocessableEntity {
			t.Errorf("status = %d", got.status)
		}
		wantFieldError(t, "nobody picked", got, "to", "Pick who takes over this order.")
		if v := squash(got.doc.Find("textarea[name=note]").Text()); v != "Over to you" {
			t.Errorf("note = %q, want it kept", v)
		}
	})
	t.Run("can't raise", func(t *testing.T) {
		got := post(int64(env.Deepak.ID), "", po.Version)
		wantFieldError(t, "Deepak", got, "to", "Deepak Nair can't raise purchase orders. Pick someone with the Purchase User role.")
	})
	t.Run("note too long", func(t *testing.T) {
		long := strings.Repeat("n", 1001)
		got := post(int64(env.Arjun.ID), long, po.Version)
		wantFieldError(t, "note too long", got, "note", "A comment can be at most 1,000 characters.")
		if checked, _ := got.attr(`input[name=to][checked]`, "value"); checked != strconv.FormatInt(int64(env.Arjun.ID), 10) {
			t.Errorf("checked = %q, want Arjun kept", checked)
		}
	})
	t.Run("stale", func(t *testing.T) {
		got := post(int64(env.Arjun.ID), "Over to you", po.Version+3)
		if got.status != http.StatusConflict || !hasBanner(got, "danger", strings.Replace(p15eStaleText, "%s", display, 1)) {
			t.Errorf("stale = %d, banners %q", got.status, banners(got))
		}
		if href, _ := got.attr(".banner--danger a", "href"); href != path {
			t.Errorf("Reload = %q, want %q", href, path)
		}
		if checked, _ := got.attr(`input[name=to][checked]`, "value"); checked != strconv.FormatInt(int64(env.Arjun.ID), 10) {
			t.Errorf("checked = %q, want Arjun kept", checked)
		}
		if v, _ := got.attr(`input[name=version]`, "value"); v != strconv.Itoa(po.Version+3) {
			t.Errorf("version = %q, want the posted one kept (CD-94)", v)
		}
	})
	t.Run("success", func(t *testing.T) {
		got := post(int64(env.Arjun.ID), "", po.Version)
		wantFlash(t, "hand over", got, poURL(po.ID), display+" is now Arjun Mehta's.")
	})

	t.Run("pending: refusal, no form", func(t *testing.T) {
		pending := purchasetest.NewOrder(t, env, purchasetest.StopAtPending())
		got := w.get(t, env.Priya, orderPath(pending.ID, "/handover"))
		if got.status != http.StatusOK || !hasBanner(got, "danger", "Withdraw this order before handing it over.") || got.doc.Find("form[method=post]").Length() != 0 {
			t.Errorf("pending = %d, banners %q, forms %d; want the refusal and no form", got.status, banners(got), got.doc.Find("form[method=post]").Length())
		}
	})
	t.Run("403 and 404", func(t *testing.T) {
		if got := w.get(t, env.Suresh, path); got.status != http.StatusForbidden {
			t.Errorf("Suresh = %d, want 403", got.status)
		}
		if got := w.get(t, env.Priya, orderPath(999_999, "/handover")); got.status != http.StatusNotFound {
			t.Errorf("missing = %d, want 404", got.status)
		}
	})
}

// MOD-PO P15e (UX §5.12): with nobody eligible the page says so and has no
// form.
func TestPurchaseWeb_HandOverNobody(t *testing.T) {
	var buf bytes.Buffer
	v := handoverView{id: 7, st: purchase.HandOverState{Display: "Draft 7", Owner: "Priya Shah"}}
	if err := handoverPage(v).Render(t.Context(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	d, _ := goquery.NewDocumentFromReader(&buf)
	if text := squash(d.Text()); !strings.Contains(text, msgNobodyEligible) || d.Find("form").Length() != 0 {
		t.Errorf("page = %q, want %q and no form", text, msgNobodyEligible)
	}
}

// MOD-PO P15e, PUX-018 (UX §5.13; spec §23; plan P11; CD-258 Q1): the Send
// page shows the summary, `Print or save as PDF` to the print view and no
// Share, Sent on prefilled with today (the IST date just after midnight),
// the five ways and `Mark as sent`; each refusal is at its field with the
// typed values kept; stale is 409; a non-approved PO shows the refusal and
// no form; a sent PO shows the banner, the record and `Mark as not sent`;
// both POSTs flash.
func TestPurchaseWeb_SendPage(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	po := purchasetest.NewOrder(t, env)
	path := orderPath(po.ID, "/send")
	env.SetNow(t, time.Date(2026, time.October, 24, 18, 31, 0, 0, time.UTC)) // 25-10 00:01 IST

	page := w.get(t, env.Priya, path)
	if page.status != http.StatusOK {
		t.Fatalf("GET = %d %q", page.status, page.text)
	}
	if h1 := page.texts("h1"); !slices.Equal(h1, []string{"Send " + po.Number}) {
		t.Errorf("h1 = %q", h1)
	}
	for _, s := range []string{p15eSummary, "Get the order to the vendor", "Record how it was sent", "Save the order as a PDF, then send it by email or WhatsApp."} {
		if !strings.Contains(page.text, s) {
			t.Errorf("page has no %q: %q", s, page.text)
		}
	}
	if strings.Contains(page.text, "Share") || strings.Contains(page.text, "Mark as not sent") {
		t.Errorf("page has Share or Mark as not sent: %q", page.text)
	}
	if href, _ := page.attr(`a.btn:contains("Print or save as PDF")`, "href"); href != "/print/purchase.Order/"+strconv.FormatInt(po.ID, 10) {
		t.Errorf("Print or save as PDF = %q", href)
	}
	if v, _ := page.attr(`input[name=sent_on]`, "value"); v != "25-10-2026" {
		t.Errorf("Sent on = %q, want today 25-10-2026 (IST)", v)
	}
	if v, _ := page.attr(`button[data-set-date]`, "data-set-date"); v != "25-10-2026" {
		t.Errorf("Today button = %q, want 25-10-2026", v)
	}
	var ways []string
	page.doc.Find(`input[type=radio][name=sent_via]`).Each(func(_ int, s *goquery.Selection) {
		ways = append(ways, s.AttrOr("value", "")+" "+squash(s.Parent().Text()))
	})
	if want := []string{"email Email", "whatsapp WhatsApp", "courier Courier", "by_hand By hand", "other Other"}; !slices.Equal(ways, want) {
		t.Errorf("ways = %q, want %q", ways, want)
	}
	if v, _ := page.attr(`input[name=version]`, "value"); v != "0" {
		t.Errorf("version = %q, want 0", v)
	}

	send := func(on, via, note string, version int) p15ePage {
		return w.post(t, env.Priya, path, url.Values{"version": {strconv.Itoa(version)}, "sent_on": {on}, "sent_via": {via}, "note": {note}})
	}
	t.Run("refusals", func(t *testing.T) {
		got := send("25-10-2026", "", "Docket 42", 0)
		wantFieldError(t, "no method", got, "sent_via", "Choose how it was sent.")
		if v := squash(got.doc.Find("textarea[name=note]").Text()); v != "Docket 42" {
			t.Errorf("note = %q, want kept", v)
		}
		got = send("26-10-2026", "email", "", 0)
		wantFieldError(t, "future", got, "sent_on", "Sent on can't be in the future.")
		if v, _ := got.attr(`input[name=sent_on]`, "value"); v != "26-10-2026" {
			t.Errorf("Sent on = %q, want the typed 26-10-2026", v)
		}
		if v, _ := got.attr(`input[name=sent_via][checked]`, "value"); v != "email" {
			t.Errorf("checked = %q, want email kept", v)
		}
		wantFieldError(t, "before order", send("23-10-2026", "email", "", 0), "sent_on", "Sent on can't be before the order date.")
		got = send("25-10-2026", "email", "", 4)
		if got.status != http.StatusConflict || !hasBanner(got, "danger", strings.Replace(p15eStaleText, "%s", po.Number, 1)) {
			t.Errorf("stale = %d, banners %q", got.status, banners(got))
		}
	})
	t.Run("mark, then undo", func(t *testing.T) {
		wantFlash(t, "mark", send("25-10-2026", "whatsapp", "To Mr Rao", 0), poURL(po.ID), po.Number+" marked as sent.")
		page := w.get(t, env.Ravi, path)
		if !hasBanner(page, "info", "Sent to vendor on 25-10-2026 by WhatsApp (Priya Shah)") {
			t.Errorf("banners = %q, want the sent banner", banners(page))
		}
		if v, _ := page.attr(`input[name=sent_via][checked]`, "value"); v != "whatsapp" {
			t.Errorf("checked = %q, want the record's whatsapp", v)
		}
		undo := page.doc.Find(`form[action="` + path + `/undo"]`)
		if undo.Length() != 1 || !strings.Contains(undo.Text(), "Mark as not sent") || undo.Find(`input[name=version]`).AttrOr("value", "") != "1" {
			t.Errorf("undo form = %d %q, want one with Mark as not sent at version 1", undo.Length(), squash(undo.Text()))
		}
		got := w.post(t, env.Ravi, path+"/undo", url.Values{"version": {"1"}})
		wantFlash(t, "undo", got, poURL(po.ID), po.Number+" marked as not sent.")
	})
	t.Run("not approved, 403", func(t *testing.T) {
		draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		got := w.get(t, env.Priya, orderPath(draft.ID, "/send"))
		if got.status != http.StatusOK || !hasBanner(got, "danger", "Only an approved order can be marked as sent.") || got.doc.Find("form[method=post]").Length() != 0 {
			t.Errorf("draft = %d, banners %q; want the refusal and no form", got.status, banners(got))
		}
		if got := w.get(t, env.Arjun, path); got.status != http.StatusForbidden {
			t.Errorf("Arjun (not owner, no approve) = %d, want 403", got.status)
		}
	})
}

// MOD-PO P15e, PUX-035's page part (UX §5.20; spec §27.1; plan P18): the
// Expected on page shows the summary with Required by and the hint; each
// refusal is at the date with the typed values kept; stale is 409; Save
// date and Clear date flash; a set record is prefilled with Clear date; a
// non-approved PO shows the refusal and no form; Suresh gets 403.
func TestPurchaseWeb_ExpectedPage(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	po := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"required_by": "28-10-2026"}))
	path := orderPath(po.ID, "/expected")

	page := w.get(t, env.Priya, path)
	if page.status != http.StatusOK {
		t.Fatalf("GET = %d %q", page.status, page.text)
	}
	if h1 := page.texts("h1"); !slices.Equal(h1, []string{"Expected delivery for " + po.Number}) {
		t.Errorf("h1 = %q", h1)
	}
	for _, s := range []string{p15eSummary + " · Required by 28-10-2026", "The date the vendor now gives. It replaces the required-by date in the overdue lists.", "Save date"} {
		if !strings.Contains(page.text, s) {
			t.Errorf("page has no %q: %q", s, page.text)
		}
	}
	if strings.Contains(page.text, "Clear date") {
		t.Errorf("Clear date with no record: %q", page.text)
	}
	if v, _ := page.attr(`input[name=expected_on]`, "value"); v != "" {
		t.Errorf("Expected on = %q, want blank", v)
	}

	set := func(on, note string, version int) p15ePage {
		return w.post(t, env.Priya, path, url.Values{"version": {strconv.Itoa(version)}, "expected_on": {on}, "note": {note}})
	}
	t.Run("refusals", func(t *testing.T) {
		got := set("", "Vendor called", 0)
		wantFieldError(t, "blank", got, "expected_on", "Enter the expected date.")
		if v := squash(got.doc.Find("textarea[name=note]").Text()); v != "Vendor called" {
			t.Errorf("note = %q, want kept", v)
		}
		got = set("31-02-2026", "", 0)
		wantFieldError(t, "invalid", got, "expected_on", "Expected on isn't a valid value. Check it and try again.")
		if v, _ := got.attr(`input[name=expected_on]`, "value"); v != "31-02-2026" {
			t.Errorf("Expected on = %q, want the typed value", v)
		}
		wantFieldError(t, "before order", set("20-10-2026", "", 0), "expected_on", "Expected on can't be before the order date.")
		got = set("05-11-2026", "", 3)
		if got.status != http.StatusConflict || !hasBanner(got, "danger", strings.Replace(p15eStaleText, "%s", po.Number, 1)) {
			t.Errorf("stale = %d, banners %q", got.status, banners(got))
		}
	})
	t.Run("set, then clear", func(t *testing.T) {
		wantFlash(t, "set", set("05-11-2026", "Vendor called", 0), poURL(po.ID), "Expected on for "+po.Number+" saved.")
		page := w.get(t, env.Ravi, path)
		if v, _ := page.attr(`input[name=expected_on]`, "value"); v != "05-11-2026" {
			t.Errorf("Expected on = %q, want the record's 05-11-2026", v)
		}
		clear := page.doc.Find(`form[action="` + path + `/clear"]`)
		if clear.Length() != 1 || !strings.Contains(clear.Text(), "Clear date") || clear.Find(`input[name=version]`).AttrOr("value", "") != "1" {
			t.Errorf("clear form = %d %q, want Clear date at version 1", clear.Length(), squash(clear.Text()))
		}
		wantFlash(t, "clear", w.post(t, env.Ravi, path+"/clear", url.Values{"version": {"1"}}), poURL(po.ID), "Expected on for "+po.Number+" cleared.")
	})
	t.Run("not approved, 403", func(t *testing.T) {
		draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		got := w.get(t, env.Priya, orderPath(draft.ID, "/expected"))
		if got.status != http.StatusOK || !hasBanner(got, "danger", "Only an approved order can have an expected date.") || got.doc.Find("form[method=post]").Length() != 0 {
			t.Errorf("draft = %d, banners %q; want the refusal and no form", got.status, banners(got))
		}
		if got := w.get(t, env.Suresh, path); got.status != http.StatusForbidden {
			t.Errorf("Suresh = %d, want 403", got.status)
		}
	})
}

// MOD-PO P15e: the rejected PO can be handed over too (spec §21.2), so its
// page has the form.
func TestPurchaseWeb_HandOverRejected(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtPending())
	res, err := env.Engine.Do(t.Context(), env.Ravi, purchase.DocType, po.ID, doctype.ActionReject, doc.ActionInput{Version: po.Version, Reason: "Wrong vendor"})
	if err != nil {
		t.Fatalf("setup: reject: %v", err)
	}
	got := w.get(t, env.Ravi, orderPath(res.ID, "/handover"))
	if got.status != http.StatusOK || got.doc.Find("form[method=post] input[name=to]").Length() == 0 {
		t.Errorf("rejected = %d %q, want the form", got.status, got.text)
	}
}
