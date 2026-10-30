package app_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// receivedOrder is an approved order by Priya with an approved receipt for
// 400.000 of line 1, made by Suresh. It returns the order's and receipt's ids.
func receivedOrder(t *testing.T, w *world, m fixture.Masters) (order, receipt int64) {
	t.Helper()
	priya := w.signedIn(priyaEmail)
	order = priya.newOrder(m, "PRJ-METRO", "1000.00")
	priya.addLine(m, order)
	priya.act(order, "submit", nil)
	return order, receiptFor(t, w, order)
}

// receiptFor is an approved receipt by Suresh for 400.000 of the order's line.
func receiptFor(t *testing.T, w *world, order int64) int64 {
	t.Helper()
	ctx := context.Background()
	suresh := w.principal(sureshID)
	line := w.text(`SELECT CAST(id AS TEXT) FROM fixture_order_line WHERE parent_id = ?`, order)
	res, err := w.app.Engine().Do(ctx, suresh, fixture.Receipt, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"receipt_date": "2026-10-24"},
		Lines:  []map[string]string{{"order_line_id": line, "qty": "400.000"}},
	})
	if err != nil {
		t.Fatalf("save receipt: %v", err)
	}
	if _, err := w.app.Engine().Do(ctx, suresh, fixture.Receipt, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil {
		t.Fatalf("submit receipt: %v", err)
	}
	return res.ID
}

// TestApp_OrderPageShowsReceived: DOC-092's "received 400 of 1,200 kg", from
// the receipt's roll-up, through the real engine and page.
func TestApp_OrderPageShowsReceived(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	order, _ := receivedOrder(t, w, m)
	r := w.signedIn(raviEmail).get(orderURL(order))
	if r.status != http.StatusOK {
		t.Fatalf("GET order = %d", r.status)
	}
	if got := receivedLine(r); got != "Received 400 of 1,200 kg" {
		t.Errorf("received line = %q", got)
	}
}

// receivedLine is the page's "Received n of m" line, or "".
func receivedLine(r *response) string {
	var got string
	r.doc.Find(".lines-meta").Each(func(_ int, m *goquery.Selection) {
		if t := strings.TrimSpace(m.Text()); strings.HasPrefix(t, "Received") {
			got = t
		}
	})
	return got
}

// TestApp_DraftPageShowsNoReceived: nothing can have been received on a draft.
func TestApp_DraftPageShowsNoReceived(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	priya := w.signedIn(priyaEmail)
	id := priya.newOrder(m, "PRJ-METRO", "1000.00")
	priya.addLine(m, id)
	if got := receivedLine(priya.get(orderURL(id) + "/edit/check")); got != "" {
		t.Errorf("the draft's check page shows %q", got)
	}
}

// TestApp_PendingAndCancelledShowNoReceived: only an approved or closed order
// says what was received.
func TestApp_PendingAndCancelledShowNoReceived(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	priya := w.signedIn(priyaEmail)
	id := w.pendingOrder(w.signedIn(adminEmail), priya, m)
	if got := receivedLine(priya.get(orderURL(id))); got != "" {
		t.Errorf("the pending order shows %q", got)
	}
	ravi := w.signedIn(raviEmail)
	ravi.act(id, "approve", nil)
	ravi.act(id, "cancel", url.Values{"reason": {"Not needed"}})
	if got := w.state(id); got != "cancelled" {
		t.Fatalf("state = %q, want cancelled", got)
	}
	if got := receivedLine(ravi.get(orderURL(id))); got != "" {
		t.Errorf("the cancelled order shows %q", got)
	}
}

// TestApp_ClosedPageShowsReceived: a closed order keeps its received line.
func TestApp_ClosedPageShowsReceived(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	order, _ := receivedOrder(t, w, m)
	priya := w.signedIn(priyaEmail)
	priya.act(order, "close", url.Values{"reason": {"All received"}})
	if got := w.state(order); got != "closed" {
		t.Fatalf("state = %q, want closed", got)
	}
	if got := receivedLine(priya.get(orderURL(order))); got != "Received 400 of 1,200 kg" {
		t.Errorf("the closed order shows %q", got)
	}
}

// cancelOrder posts the cancel reason page.
func cancelOrder(b *browser, order int64, version string) *response {
	b.w.t.Helper()
	return b.post(orderURL(order)+"/cancel", url.Values{"version": {version}, "reason": {"Vendor changed the terms"}})
}

// alerts is the text of every alert on the page; the shell's live region is
// the first and is empty.
func alerts(r *response) string { return r.doc.Find(`[role="alert"]`).Text() }

func receiptLink(r *response, receipt int64) *goquery.Selection {
	return r.doc.Find(`a[href="/doc/` + fixture.Receipt + "/" + strconv.FormatInt(receipt, 10) + `"]`)
}

// TestApp_CancelBlockedLinksTheBlocker: DOC-092's "DOC-052 message with a link
// to the GRN", for a user who can read the GRN, and the link opens it.
func TestApp_CancelBlockedLinksTheBlocker(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	order, receipt := receivedOrder(t, w, m)
	admin := w.signedIn(adminEmail)
	r := cancelOrder(admin, order, w.version(order))
	if r.status != http.StatusConflict {
		t.Fatalf("POST cancel = %d (%s), want 409", r.status, r.pageText())
	}
	link := r.doc.Find(`[role="alert"]`).Find("a")
	if link.Length() != 1 || strings.TrimSpace(link.Text()) != "GR/26-27/00001" {
		t.Fatalf("alerts %q have no link to the GRN", alerts(r))
	}
	href, _ := link.Attr("href")
	if got := admin.get(href); got.status != http.StatusOK || !strings.Contains(got.pageText(), "GR/26-27/00001") {
		t.Errorf("GET %s = %d", href, got.status)
	}
	if receiptLink(r, receipt).Length() != 1 {
		t.Errorf("the link goes to %s, not the receipt", href)
	}
}

// TestApp_CancelBlockedNoLinkForReader: Ravi may cancel the order but can't
// read receipts, so he gets the message and no dead link.
func TestApp_CancelBlockedNoLinkForReader(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	order, receipt := receivedOrder(t, w, m)
	r := cancelOrder(w.signedIn(raviEmail), order, w.version(order))
	if r.status != http.StatusConflict || !strings.Contains(alerts(r), "is linked to it") {
		t.Fatalf("POST cancel = %d, alerts %q", r.status, alerts(r))
	}
	if receiptLink(r, receipt).Length() != 0 {
		t.Error("Ravi can't read the GRN but the page links it")
	}
}

// TestApp_CancelForbiddenNoLink: Priya may not cancel; her 403 names no GRN.
func TestApp_CancelForbiddenNoLink(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	order, receipt := receivedOrder(t, w, m)
	r := cancelOrder(w.signedIn(priyaEmail), order, w.version(order))
	if r.status != http.StatusForbidden {
		t.Fatalf("POST cancel = %d (%s), want 403", r.status, r.pageText())
	}
	if receiptLink(r, receipt).Length() != 0 || strings.Contains(alerts(r), "GR/26-27/00001") {
		t.Errorf("the 403 names the GRN: %s", alerts(r))
	}
}

// TestApp_CancelStaleNoLink: a stale page gets the stale message alone, even
// for a user who can read the GRN.
func TestApp_CancelStaleNoLink(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	order, receipt := receivedOrder(t, w, m)
	r := cancelOrder(w.signedIn(adminEmail), order, "1")
	if r.status != http.StatusConflict || !strings.Contains(alerts(r), "Someone else changed") {
		t.Fatalf("POST cancel = %d, alerts %q, want the stale message", r.status, alerts(r))
	}
	if receiptLink(r, receipt).Length() != 0 {
		t.Error("the stale refusal links the GRN")
	}
}

// TestApp_CancelTwoBlockersNoLink: with several blockers the message lists
// them and nothing is linked.
func TestApp_CancelTwoBlockersNoLink(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	order, receipt := receivedOrder(t, w, m)
	second := receiptFor(t, w, order)
	r := cancelOrder(w.signedIn(adminEmail), order, w.version(order))
	if r.status != http.StatusConflict || !strings.Contains(alerts(r), "2 documents are linked to it") {
		t.Fatalf("POST cancel = %d, alerts %q", r.status, alerts(r))
	}
	if receiptLink(r, receipt).Length() != 0 || receiptLink(r, second).Length() != 0 {
		t.Error("the page links one of several blockers")
	}
}

// postedSupplier posts a new order whose amount is refused, so the form comes
// back with the posted supplier id, and returns the form's main area.
func postedSupplier(b *browser, supplier string) (int, string) {
	b.w.t.Helper()
	r := b.post(orderPath+"/new", url.Values{
		"supplier_id": {supplier}, "order_date": {"24-10-2026"}, "amount": {"lots"},
		"priority": {"normal"}, "discount_pct": {"0"},
	})
	html, err := r.doc.Find("main").Html()
	if err != nil {
		b.w.t.Fatal(err)
	}
	return r.status, html
}

// withoutID is page with the posted supplier id, in the picker's summary and
// hidden input, replaced by "ID".
func withoutID(page, id string) string {
	page = strings.Replace(page, `data-picker-summary="">`+id+` <`, `data-picker-summary="">ID <`, 1)
	return strings.Replace(page, `name="supplier_id" value="`+id+`"`, `name="supplier_id" value="ID"`, 1)
}

// TestApp_PostedLinkLabelNeedsRead: CD-233 through the app's own wiring. A
// posted supplier Priya may read shows its name (CD-10 grants buyers read on
// parties); once her role loses that right it shows its raw id, the page
// otherwise equal to an unknown id's.
func TestApp_PostedLinkLabelNeedsRead(t *testing.T) {
	w := newWorld(t)
	admin, _, m := w.canonical()
	priya := w.signedIn(priyaEmail)
	shree := strconv.FormatInt(m.Party["SHREE"], 10)
	const unknown = "999999"

	if _, readable := postedSupplier(priya, shree); !strings.Contains(readable, "SHREE Shree Steel Traders") {
		t.Errorf("a supplier Priya may read shows no name: %.200s", readable[strings.Index(readable, "picker__summary"):])
	}

	err := w.app.Auth().SetRights(context.Background(), admin, "Purchase User", fixture.Party, map[auth.Right]auth.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	hiddenStatus, hidden := postedSupplier(priya, shree)
	unknownStatus, unknownPage := postedSupplier(priya, unknown)
	if hiddenStatus != http.StatusUnprocessableEntity || unknownStatus != hiddenStatus {
		t.Fatalf("POST new = %d and %d, want 422", hiddenStatus, unknownStatus)
	}
	if strings.Contains(hidden, "Shree") {
		t.Errorf("a hidden supplier shows its name: %.200s", hidden[strings.Index(hidden, "picker__summary"):])
	}
	if got, want := withoutID(hidden, shree), withoutID(unknownPage, unknown); got != want {
		t.Errorf("the hidden supplier's page differs from an unknown id's:\n%s\n---\n%s", got, want)
	}
}
