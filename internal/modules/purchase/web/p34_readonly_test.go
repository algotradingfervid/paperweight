package purchaseweb

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// p34OrderSystem are purchase.Order's header System fields a user never
// fills in (P34): the totals and receipt_status.
var p34OrderSystem = []string{"net_total", "cgst_total", "sgst_total", "igst_total", "grand_total", "rounded_total", "receipt_status"}

// p34NoInputs fails for each of names that has an input on page pg.
func p34NoInputs(t *testing.T, where string, pg p15dPage, names ...string) {
	t.Helper()
	if pg.status != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", where, pg.status, pg.text())
	}
	for _, n := range names {
		if pg.doc.Find(`[name="`+n+`"]`).Length() != 0 {
			t.Errorf("%s has an input for %s, want none (System, ReadOnly)", where, n)
		}
	}
}

// MOD-PO P34: the PO Details step, new and draft, has no input for the
// totals or receipt_status, and the draft's step doesn't show Receipt.
func TestP34_OrderDetailsNoSystemInputs(t *testing.T) {
	w := newP15dWeb(t)
	p34NoInputs(t, "new PO", w.get(t, w.env.Priya, "/doc/"+purchase.DocType+"/new"), p34OrderSystem...)
	po := w.order(t, doctype.StateDraft)
	step := w.get(t, w.env.Priya, p15dDoc(purchase.DocType, po.ID)+"/edit/details")
	p34NoInputs(t, "draft PO Details", step, p34OrderSystem...)
	if v, ok := step.pair("Receipt"); ok {
		t.Errorf("draft PO Details shows Receipt %q, want it hidden while the order is edited", v)
	}
}

// MOD-PO P34: the Request Details step, new and draft, has no order_status
// input, and the draft's step doesn't show Ordered.
func TestP34_RequestDetailsNoOrderStatus(t *testing.T) {
	w := newP15dWeb(t)
	p34NoInputs(t, "new request", w.get(t, w.env.Suresh, "/doc/"+purchase.RequestDocType+"/new"), "order_status")
	pr := w.prA(t, purchasetest.StopAtDraft())
	step := w.get(t, w.env.Suresh, p15dDoc(purchase.RequestDocType, pr.ID)+"/edit/details")
	p34NoInputs(t, "draft request Details", step, "order_status")
	if v, ok := step.pair("Ordered"); ok {
		t.Errorf("draft request Details shows Ordered %q, want it hidden while the request is edited", v)
	}
}

// MOD-PO P34: the PO line page has no Received input and the request line
// page no Ordered input.
func TestP34_LinePagesNoSystemInputs(t *testing.T) {
	w := newP15dWeb(t)
	po := w.order(t, doctype.StateDraft)
	p34NoInputs(t, "PO line page", w.get(t, w.env.Priya, p15dDoc(purchase.DocType, po.ID)+"/lines/1"), "received_qty")
	pr := w.prA(t, purchasetest.StopAtDraft())
	p34NoInputs(t, "request line page", w.get(t, w.env.Suresh, p15dDoc(purchase.RequestDocType, pr.ID)+"/lines/1"), "ordered_qty")
}

// MOD-PO P34: the approved PO's document page still shows Receipt and the
// totals as values.
func TestP34_ApprovedOrderShowsValues(t *testing.T) {
	w := newP15dWeb(t)
	po := w.order(t, doctype.StateApproved)
	pg := w.page(t, w.env.Priya, po.ID)
	if v, ok := pg.pair("Receipt"); !ok || v != "Not received" {
		t.Errorf("approved PO Receipt = %q, %v; want Not received", v, ok)
	}
	for _, label := range []string{"Taxable value", "Grand total", "Rounded total"} {
		if v, ok := pg.pair(label); !ok || !strings.HasPrefix(v, "₹") {
			t.Errorf("approved PO %s = %q, %v; want a rupee value", label, v, ok)
		}
	}
}

// MOD-PO P34 (CD-327 (1), CD-334): on the Request list order_status is not
// hidden (Listing true, nil View), so its filter is kept; on a new record
// (nil View) and a draft it is hidden, and on a submitted request it shows.
func TestP34_RequestListFiltersOrderStatus(t *testing.T) {
	var m module
	f := doctype.Field{Name: "order_status"}
	if m.requestHide(docscreen.Ctx{Listing: true}, f) {
		t.Error("requestHide(Listing, order_status) = true, want shown on the list")
	}
	if !m.requestHide(docscreen.Ctx{}, f) {
		t.Error("requestHide(nil View, order_status) = false, want hidden on a new record")
	}
	draft := &docscreen.DocView{}
	draft.Info.State = doctype.StateDraft
	if !m.requestHide(docscreen.Ctx{View: draft}, f) {
		t.Error("requestHide(draft, order_status) = false, want hidden while edited")
	}
	approved := &docscreen.DocView{}
	approved.Info.State = doctype.StateApproved
	if m.requestHide(docscreen.Ctx{View: approved}, f) {
		t.Error("requestHide(approved, order_status) = true, want shown on the document page")
	}

	dt, _ := doctype.Lookup(purchase.RequestDocType)
	hide := docscreen.Deps{Modules: Modules(Deps{})}.ListHidden(httptest.NewRequest(http.MethodGet, "/doc/"+purchase.RequestDocType, nil), dt)
	q := docscreen.ListedQuery(dt, docscreen.ParseListQuery(url.Values{"f": {"order_status:=:ordered"}}), hide)
	if len(q.Filters) != 1 || q.Filters[0].Field != "order_status" {
		t.Errorf("Request list filters = %+v, want the order_status filter kept", q.Filters)
	}
}

// MOD-PO P34: orderHide hides receipt_status on a new record and a draft,
// and shows it on the list and on a submitted order.
func TestP34_OrderHideReceiptStatus(t *testing.T) {
	var m module
	f := doctype.Field{Name: "receipt_status"}
	if m.orderHide(docscreen.Ctx{Listing: true}, f) {
		t.Error("orderHide(Listing, receipt_status) = true, want shown on the list")
	}
	if !m.orderHide(docscreen.Ctx{}, f) {
		t.Error("orderHide(nil View, receipt_status) = false, want hidden on a new record")
	}
	pending := &docscreen.DocView{}
	pending.Info.State = doctype.StatePending
	if m.orderHide(docscreen.Ctx{View: pending}, f) {
		t.Error("orderHide(pending, receipt_status) = true, want shown on the document page")
	}
}
