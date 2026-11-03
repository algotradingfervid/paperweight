package grnweb_test

import (
	"strings"
	"testing"
)

// GRN review M10: the receipt date's calendar stops at today, and its echo has
// the check icon and the step's meaning. The challan date, which may be any
// day, has neither. TestE2E_NewDocumentDatedToday checks the max on the app's
// clock.
func TestDetails_ReceiptDateBoundsAndEcho(t *testing.T) {
	w := newS1World(t)
	id := w.draft(w.po("SHREE", "10"))
	r := w.get(w.Suresh, stepPath(id, "details"))
	pick := r.doc.Find(`input[type=date][data-date-picker="f-receipt_date"]`)
	if got := pick.AttrOr("max", ""); got != "2026-10-24" {
		t.Errorf("receipt date's calendar max = %q, want 2026-10-24", got)
	}
	if v := r.doc.Find("#f-receipt_date").AttrOr("value", ""); v == "" {
		t.Fatal("no receipt date on the draft")
	}
	msg := r.doc.Find("p#f-receipt_date-msg")
	if got := strings.TrimSpace(msg.Text()); !strings.HasSuffix(got, " · the day the goods arrived") || msg.Find("svg").Length() != 1 {
		t.Errorf("receipt date's echo = %q, %d icons", got, msg.Find("svg").Length())
	}
	if got := r.doc.Find("#f-receipt_date").AttrOr("hx-vals", ""); !strings.Contains(got, `"_note":"the day the goods arrived"`) || !strings.Contains(got, `"_max":"24-10-2026"`) {
		t.Errorf("hx-vals = %s", got)
	}
	d := w.get(w.Suresh, stepPath(id, "delivery"))
	if got, ok := d.doc.Find(`input[type=date][data-date-picker="f-challan_date"]`).Attr("max"); ok {
		t.Errorf("challan date's calendar max = %q, want none", got)
	}
	if got := d.doc.Find("#f-challan_date").AttrOr("hx-vals", ""); strings.Contains(got, "_note") {
		t.Errorf("challan hx-vals = %s", got)
	}
}
