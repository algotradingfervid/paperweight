package grnweb_test

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// MOD-GRN S1e (CD-341 (2), CD-350; UI v2 B1): GRN's custom steps mark a
// field as docscreen's generic FieldInput does, from the doctype's own
// Required flag: a Required field's control has aria-required="true" and
// its label or legend the asterisk and the screen-reader words; any other
// field neither. Step 2 declares no Required field, but marks the fields
// goodsreceipt.SubmitRequired names for the draft: challan_no and
// challan_kind when a line is goods (head ruling on S1e).

// s1eMark reports what of B1's required marking the controls sel and their
// field carry: every control's aria-required, the label's asterisk and the
// hidden " (required)" words.
func s1eMark(sel *goquery.Selection) (aria, star, words bool) {
	aria = sel.Length() > 0
	sel.Each(func(_ int, s *goquery.Selection) { aria = aria && s.AttrOr("aria-required", "") == "true" })
	label := sel.First().Closest(".field").Find("label.field__label, legend.field__label").First()
	req := label.Find(`span.field__req[aria-hidden="true"]`)
	star = req.Length() == 1 && strings.TrimSpace(req.Text()) == "*"
	words = strings.TrimSpace(label.Find("span.visually-hidden").Text()) == "(required)"
	return aria, star, words
}

func s1eWant(t *testing.T, what string, sel *goquery.Selection, required bool) {
	t.Helper()
	if sel.Length() == 0 {
		t.Fatalf("%s: no control", what)
	}
	aria, star, words := s1eMark(sel)
	if aria != required || star != required || words != required {
		t.Errorf("%s: aria-required %v, asterisk %v, hidden words %v; want all %v", what, aria, star, words, required)
	}
}

func TestGRNWeb_RequiredMarkedAsDeclared(t *testing.T) {
	w := newS1World(t)
	po := w.po("SHREE", "10")

	r := w.get(w.Suresh, s1New)
	s1eWant(t, "step 1 Purchase order", r.doc.Find(`input[type="radio"][name="purchase_order_id"]`), true)
	s1eWant(t, "step 1 Receipt date", r.doc.Find("#f-receipt_date"), true)

	id := w.draft(po)
	r = w.get(w.Suresh, stepPath(id, "delivery"))
	s1eWant(t, "step 2 challan_no", r.doc.Find("#f-challan_no"), true)
	s1eWant(t, "step 2 challan_kind", r.doc.Find(`input[type="radio"][name="challan_kind"]`), true)
	for _, f := range []string{"challan_date", "vehicle_no", "transporter", "eway_bill_no", "remarks"} {
		s1eWant(t, "step 2 "+f, r.doc.Find("#f-"+f), false)
	}

	r = w.s2Items(id)
	s1eWant(t, "step 3 Received", r.s2Input("received", 1), true)
	s1eWant(t, "step 3 Rejected", r.s2Input("rejected", 1), false)
	s1eWant(t, "step 3 Reason", r.s2Input("reason", 1), false)
}

// Without a goods line submit doesn't require the challan, so step 2 doesn't
// mark it: a draft with no lines (against a fully received PO) shows the
// fields unmarked, and a service-only draft hides them.
func TestGRNWeb_ChallanMarkedOnlyWithGoods(t *testing.T) {
	w := newS1World(t)
	full := w.po("BHARAT", "5")
	w.receiveAll(full)
	r := w.get(w.Suresh, stepPath(w.draft(full), "delivery"))
	s1eWant(t, "no lines challan_no", r.doc.Find("#f-challan_no"), false)
	s1eWant(t, "no lines challan_kind", r.doc.Find(`input[type="radio"][name="challan_kind"]`), false)

	r = w.get(w.Suresh, stepPath(w.draft(w.servicePO()), "delivery"))
	if n := r.doc.Find(`[name="challan_no"], [name="challan_kind"], .field__req`).Length(); n != 0 {
		t.Errorf("service-only step 2: %d challan controls or asterisks, want none", n)
	}
}
