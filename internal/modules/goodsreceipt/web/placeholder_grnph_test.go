package grnweb_test

import (
	"strings"
	"testing"
)

// MOD-GRN field placeholders (CD-370, UI v2 §5.5 item 5): each text and
// quantity input the GRN's Bodies draw shows its Field's Placeholder, and
// the dates show only dd-mm-yyyy (UX doc §5.3, §5.4).

// TestGRNWeb_DeliveryPlaceholders: step 2's text inputs carry the declared
// examples; Challan date carries no "e.g." example.
func TestGRNWeb_DeliveryPlaceholders(t *testing.T) {
	w := newS1World(t)
	r := w.get(w.Suresh, stepPath(w.draft(w.po("SHREE", "10")), "delivery"))
	for name, want := range map[string]string{
		"challan_no":   "e.g. DC-1042",
		"vehicle_no":   "e.g. MH12AB1234",
		"transporter":  "e.g. Shree Ganesh Roadways",
		"eway_bill_no": "e.g. 331009876543",
		"remarks":      "e.g. Unloaded at gate 2",
	} {
		in := r.doc.Find(`#wizard-form [name="` + name + `"]`)
		if in.Length() != 1 {
			t.Fatalf("%d inputs named %s: %s", in.Length(), name, r.text())
		}
		if got := in.AttrOr("placeholder", ""); got != want {
			t.Errorf("%s placeholder = %q, want %q", name, got, want)
		}
	}
	if got := r.doc.Find(`#wizard-form input[name="challan_date"]`).AttrOr("placeholder", ""); got != "dd-mm-yyyy" {
		t.Errorf("challan_date placeholder = %q, want dd-mm-yyyy", got)
	}
}

// TestGRNWeb_ReceiptDatePlaceholder: step 1's Receipt date shows only
// dd-mm-yyyy.
func TestGRNWeb_ReceiptDatePlaceholder(t *testing.T) {
	w := newS1World(t)
	r := w.get(w.Suresh, stepPath(w.draft(w.po("SHREE", "10")), "details"))
	in := r.doc.Find(`#wizard-form input[name="receipt_date"]`)
	if got := in.AttrOr("placeholder", ""); got != "dd-mm-yyyy" || strings.Contains(got, "e.g.") {
		t.Errorf("receipt_date placeholder = %q, want dd-mm-yyyy (%d inputs)", got, in.Length())
	}
}

// TestGRNWeb_ItemsPlaceholders: each card's Received, Rejected and Reason
// inputs carry the declared examples.
func TestGRNWeb_ItemsPlaceholders(t *testing.T) {
	w := newS1World(t)
	r := w.s2Items(w.draft(w.s2RefPO()))
	for _, n := range []int{1, 2} {
		for key, want := range map[string]string{
			"received": "e.g. 400",
			"rejected": "e.g. 10",
			"reason":   "e.g. 6 bars bent",
		} {
			in := r.s2Input(key, n)
			if in.Length() != 1 {
				t.Fatalf("card %d: %d %s inputs", n, in.Length(), key)
			}
			if got := in.AttrOr("placeholder", ""); got != want {
				t.Errorf("card %d %s placeholder = %q, want %q", n, key, got, want)
			}
		}
	}
}
