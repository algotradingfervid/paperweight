package purchaseweb

import (
	"net/http"
	"testing"
)

// P42 · the pickers purchase draws itself carry UI v2 §5.8's placeholders.

// UI v2 §5.8: the pending report's filter pickers and the Create PO from
// request vendor picker carry their master's placeholder.
func TestP42_PickerPlaceholders(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	pr := w.prA(t)

	page := w.get(t, env.Priya, "/reports/purchase/pending/filter")
	if page.status != http.StatusOK {
		t.Fatalf("GET the pending filter = %d, want 200", page.status)
	}
	for id, want := range map[string]string{
		"f-vendor":  "Name, code or GSTIN",
		"f-project": "Name or code",
		"f-site":    "Name or code",
		"f-item":    "Name or code",
	} {
		in := page.doc.Find("input#" + id)
		if got, _ := in.Attr("placeholder"); in.Length() != 1 || got != want {
			t.Errorf("pending filter %s: %d inputs, placeholder %q; want one with %q", id, in.Length(), got, want)
		}
	}

	page = w.get(t, env.Priya, p15dOrderPath(pr.ID))
	if page.status != http.StatusOK {
		t.Fatalf("GET Create PO from request = %d, want 200", page.status)
	}
	in := page.doc.Find(`input[name="q"][id^="f-vendor_id"]`)
	if got, _ := in.Attr("placeholder"); in.Length() != 1 || got != "Name, code or GSTIN" {
		t.Errorf("Create PO vendor picker: %d inputs, placeholder %q; want one with %q", in.Length(), got, "Name, code or GSTIN")
	}
}
