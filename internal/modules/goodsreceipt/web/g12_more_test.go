package grnweb_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
)

// MOD-GRN G12 · CD-359 (7), CD-363, UI v2 §5.5 item 3 (UX doc §5.3): step
// 2's rarely used fields (Vehicle no., Transporter, E-way bill no.,
// Remarks) sit in one MoreDetails after the other inputs, closed when they
// are empty and open when one holds a value or an error. The fields submit
// may require (Document with the goods, Challan or invoice no.), Challan
// date and the tick are never inside.

const g12Summary = "More details · vehicle no., transporter, e-way bill no., remarks"

// g12More is step 2's one More details group, its summary and whether it
// is open.
func g12More(t *testing.T, p s1Page) (d *goquery.Selection, summary string, open bool) {
	t.Helper()
	if p.status != http.StatusOK && p.status != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %s", p.status, p.text())
	}
	d = p.doc.Find("form details").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.HasPrefix(s2Norm(s.ChildrenFiltered("summary").Text()), "More details")
	})
	if d.Length() != 1 {
		t.Fatalf("%d More details groups in step 2, want 1: %s", d.Length(), p.text())
	}
	return d, s2Norm(d.ChildrenFiltered("summary").Text()), s2Open(d)
}

// g12Moved are the fields marked More; g12Kept are step 2's others.
var (
	g12Moved = []string{"vehicle_no", "transporter", "eway_bill_no", "remarks"}
	g12Kept  = []string{"challan_kind", "challan_no", "challan_date", "rest_not_coming"}
)

// A goods draft's step 2: the four fields inside a closed group named for
// them, the others outside it (none of the required ones inside).
func TestGRNWeb_DeliveryMoreDetails(t *testing.T) {
	w := newS1World(t)
	r := w.get(w.Suresh, stepPath(w.draft(w.po("SHREE", "10")), "delivery"))
	d, summary, open := g12More(t, r)
	if summary != g12Summary || open {
		t.Errorf("group %q open %v, want %q closed", summary, open, g12Summary)
	}
	for _, name := range g12Moved {
		if d.Find(`[name="`+name+`"]`).Length() != 1 {
			t.Errorf("%s is not inside More details", name)
		}
	}
	for _, name := range g12Kept {
		if r.doc.Find(`[name="`+name+`"]`).Length() == 0 || d.Find(`[name="`+name+`"]`).Length() != 0 {
			t.Errorf("%s is missing or inside More details", name)
		}
	}
	if n := d.Find(`[aria-required="true"], [required]`).Length(); n != 0 {
		t.Errorf("%d required inputs inside More details", n)
	}
	if r.doc.Find(`[aria-required="true"]`).Length() == 0 {
		t.Errorf("step 2 marks nothing required: the check above saw nothing")
	}
}

// The group opens on a draft where one of its fields holds a value (each
// in turn, the others blank), and on a 422 whose error is inside it, with
// the typed value kept.
func TestGRNWeb_DeliveryMoreDetailsOpen(t *testing.T) {
	w := newS1World(t)
	values := map[string]string{"vehicle_no": "MH12AB1234", "transporter": "Sai Logistics", "eway_bill_no": "331009876543", "remarks": "Two bundles wet"}
	for _, name := range g12Moved {
		t.Run(name, func(t *testing.T) {
			id := w.draft(w.po("SHREE", "10"))
			path := stepPath(id, "delivery")
			form := url.Values{"version": {strconv.Itoa(w.Load(t, goodsreceipt.DocType, id).Version)}, name: {values[name]}}
			if r := w.post(w.Suresh, path, form); r.status != http.StatusSeeOther {
				t.Fatalf("save %s = %d %q", name, r.status, r.errorSummary())
			}
			if _, _, open := g12More(t, w.get(w.Suresh, path)); !open {
				t.Errorf("group closed with %s stored", name)
			}
		})
	}

	id := w.draft(w.po("SHREE", "10"))
	path := stepPath(id, "delivery")
	r := w.post(w.Suresh, path, url.Values{"version": {strconv.Itoa(w.Load(t, goodsreceipt.DocType, id).Version)}, "transporter": {strings.Repeat("T", 141)}})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("transporter of 141 = %d, want 422", r.status)
	}
	d, _, open := g12More(t, r)
	if !open || d.Find("#f-transporter-msg").Length() != 1 {
		t.Errorf("422: group open %v, error inside %d, want open with the error inside", open, d.Find("#f-transporter-msg").Length())
	}
}

// CD-363 on a service-only draft: the document fields are hidden, so the
// group holds only Remarks; the tick is shown and isn't More, so Remarks
// stays in a group rather than showing without one.
func TestGRNWeb_DeliveryMoreDetailsServiceOnly(t *testing.T) {
	w := newS1World(t)
	r := w.get(w.Suresh, stepPath(w.draft(w.servicePO()), "delivery"))
	d, summary, open := g12More(t, r)
	if summary != "More details · remarks" || open {
		t.Errorf("group %q open %v, want More details · remarks closed", summary, open)
	}
	if d.Find(`[name="remarks"]`).Length() != 1 || d.Find("input, textarea, select").Length() != 1 {
		t.Errorf("service-only group doesn't hold Remarks alone: %d inputs", d.Find("input, textarea, select").Length())
	}
	if r.doc.Find(`[name="rest_not_coming"]`).Length() != 1 || d.Find(`[name="rest_not_coming"]`).Length() != 0 {
		t.Errorf("the tick is missing or inside More details")
	}
}
