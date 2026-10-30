package app_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

const sureshID = 5

// receivedOrder is an approved order by Priya with an approved receipt for
// 400.000 of line 1, made by Suresh. It returns the order's and receipt's ids.
func receivedOrder(t *testing.T, w *world, m fixture.Masters) (order, receipt int64) {
	t.Helper()
	priya := w.signedIn(priyaEmail)
	order = priya.newOrder(m, "PRJ-METRO", "1000.00")
	priya.addLine(m, order)
	priya.act(order, "submit", nil)
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
	return order, res.ID
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
	if got := r.pageText(); !strings.Contains(got, "Received 400 of 1,200 kg") {
		t.Errorf("the order page has no received line: %s", got)
	}
}

// TestApp_DraftPageShowsNoReceived: nothing can have been received on a draft.
func TestApp_DraftPageShowsNoReceived(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	priya := w.signedIn(priyaEmail)
	id := priya.newOrder(m, "PRJ-METRO", "1000.00")
	priya.addLine(m, id)
	if got := priya.get(orderURL(id) + "/edit/check").pageText(); strings.Contains(got, "Received") {
		t.Errorf("the draft's check page shows a received line: %s", got)
	}
}

// TestApp_CancelBlockedLinksTheBlocker: DOC-092's "DOC-052 message with a link
// to the GRN".
func TestApp_CancelBlockedLinksTheBlocker(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	order, receipt := receivedOrder(t, w, m)
	ravi := w.signedIn(raviEmail)
	r := ravi.post(orderURL(order)+"/cancel", url.Values{"version": {w.version(order)}, "reason": {"Vendor changed the terms"}})
	if r.status != http.StatusConflict {
		t.Fatalf("POST cancel = %d (%s), want 409", r.status, r.pageText())
	}
	href := "/doc/" + fixture.Receipt + "/" + strconv.FormatInt(receipt, 10)
	link := r.doc.Find(`[role="alert"] a[href="` + href + `"]`)
	if link.Length() != 1 || strings.TrimSpace(link.Text()) != "GR/26-27/00001" {
		t.Errorf("alert %q has no link to %s", r.alert(), href)
	}
}
