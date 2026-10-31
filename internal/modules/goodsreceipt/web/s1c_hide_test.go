package grnweb_test

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN S1c: CD-319 on S1's own step bodies and saves. A field the
// Module's Hide hides (here another Hide wrapped round it, as the tooling
// redraw sweep's probe does) is neither drawn nor posted by step 1 or 2;
// the tick, which step 2 draws and posts whatever Hide says, stays.
func TestGRNWeb_StepsHonourHide(t *testing.T) {
	w := newS1World(t)
	order := w.po("SHREE", "10")
	id := w.draft(order)
	delivery := stepPath(id, "delivery")
	version := func() string { return strconv.Itoa(w.Load(t, goodsreceipt.DocType, id).Version) }
	stored := url.Values{"challan_kind": {"Tax invoice"}, "challan_no": {"SST/2627/0457"}, "challan_date": {"23-10-2026"}, "vehicle_no": {"MH12AB1234"},
		"transporter": {"Sai Logistics"}, "eway_bill_no": {"331009876543"}, "remarks": {"Two bundles wet"}}
	f := url.Values{"version": {version()}, "rest_not_coming": {"1"}}
	for k, v := range stored {
		f[k] = v
	}
	if r := w.post(w.Suresh, delivery, f); r.status != http.StatusSeeOther {
		t.Fatalf("save step 2 = %d %q", r.status, r.errorSummary())
	}
	header := func() []any {
		h := w.Load(t, goodsreceipt.DocType, id).Header
		return []any{h["receipt_date"].(types.Date).ISO(), h["challan_kind"], h["challan_no"], h["challan_date"].(types.Date).ISO(), h["vehicle_no"], h["transporter"], h["eway_bill_no"], h["remarks"]}
	}
	before := header()

	names := []string{"receipt_date", "challan_kind", "challan_no", "challan_date", "vehicle_no", "transporter", "eway_bill_no", "remarks"}
	w.hidden = map[string]bool{}
	for _, n := range names {
		w.hidden[n] = true
	}
	for _, path := range []string{s1New, stepPath(id, "details"), delivery} {
		r := w.get(w.Suresh, path)
		for _, n := range names {
			if r.doc.Find(`[name="`+n+`"]`).Length() != 0 {
				t.Errorf("GET %s draws the hidden %s", path, n)
			}
		}
	}
	// Step 1, choosing the order (a new draft) and with it fixed (a draft
	// with lines): no date input of any name, nor its label.
	for _, path := range []string{s1New, stepPath(id, "details")} {
		r := w.get(w.Suresh, path)
		if n := r.doc.Find(`main [data-set-date]`).Length(); n != 0 {
			t.Errorf("GET %s draws a date input (%d Today/Yesterday buttons) with receipt_date hidden", path, n)
		}
		if strings.Contains(r.doc.Find("main").Text(), "Receipt date") {
			t.Errorf("GET %s shows the Receipt date label with receipt_date hidden", path)
		}
	}
	if w.get(w.Suresh, delivery).doc.Find(`input[name="rest_not_coming"]`).Length() != 1 {
		t.Errorf("step 2 dropped the tick: it is drawn whatever Hide says")
	}

	crafted := url.Values{"version": {version()}, "challan_kind": {"Delivery challan"}, "challan_no": {"DC-1"}, "challan_date": {"x"},
		"vehicle_no": {"X"}, "transporter": {"X"}, "eway_bill_no": {"1"}, "remarks": {"X"}}
	if r := w.post(w.Suresh, delivery, crafted); r.status != http.StatusSeeOther {
		t.Errorf("step 2 with crafted hidden values = %d %q, want 303: hidden fields aren't read", r.status, r.errorSummary())
	}
	if got := header(); !slices.Equal(got, before) {
		t.Errorf("step 2 wrote hidden fields: %v, want %v kept", got, before)
	}
	if h := w.Load(t, goodsreceipt.DocType, id).Header; h["rest_not_coming"] != false {
		t.Errorf("rest_not_coming %v after an unticked post, want false: the tick is always posted", h["rest_not_coming"])
	}
	for _, date := range []string{"x", "22-10-2026"} {
		if r := w.post(w.Suresh, stepPath(id, "details"), url.Values{"version": {version()}, "receipt_date": {date}, "purchase_order_id": {strconv.FormatInt(order.ID, 10)}}); r.status != http.StatusSeeOther {
			t.Errorf("step 1 of a draft with lines, receipt_date %s = %d %q, want 303: a hidden date isn't read", date, r.status, r.errorSummary())
		}
	}
	if got := header(); !slices.Equal(got, before) {
		t.Errorf("step 1 wrote the hidden receipt date: %v, want %v kept", got, before)
	}

	po := strconv.FormatInt(w.po("SHREE", "5").ID, 10)
	for _, date := range []string{"x", "22-10-2026"} {
		made := w.created(w.post(w.Suresh, s1New, url.Values{"purchase_order_id": {po}, "receipt_date": {date}}))
		if got := w.Load(t, goodsreceipt.DocType, made).Header["receipt_date"].(types.Date).ISO(); got != "2026-10-24" {
			t.Errorf("new draft with the hidden receipt_date %s stores %s, want DraftFromPO's today 2026-10-24", date, got)
		}
	}
}
