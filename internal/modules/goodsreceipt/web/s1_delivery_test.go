package grnweb_test

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN S1, step 2 Delivery details (UX doc §5.3; CD-272 Gap 5, Gap 8;
// CD-282 Q1, Q7) and Module.Hide (UX doc §5.6, §6.3). Suresh on the real
// auth.Service (CD-213).

const s1MsgPhoto = "You can photograph the challan on the last step, before you submit."

// label is the text of the label or legend of field id.
func (p s1Page) label(id string) string {
	l := p.doc.Find(`label[for="` + id + `"]`)
	if l.Length() == 0 {
		l = p.doc.Find(`#` + id).Closest("fieldset").Find("legend")
	}
	return strings.Join(strings.Fields(l.First().Text()), " ")
}

// GUX-008 (GRN-007, GRN-045): on a goods PO step 2 has the kind radios
// with neither checked, no label marked "(optional)" (CD-341 (2)), the optional
// transport fields with their input hints, Remarks, the tick as
// one unticked row, and the photo line; on a PO whose only line is
// SVC-CRANE only Remarks, the tick and the photo line.
func TestGRNWeb_DeliveryStep(t *testing.T) {
	w := newS1World(t)
	r := w.get(w.Suresh, stepPath(w.draft(w.po("SHREE", "10")), "delivery"))
	if got := strings.TrimSpace(r.doc.Find("h1").Text()); got != "Delivery details" {
		t.Errorf("h1 = %q, want Delivery details", got)
	}
	want := []s1Radio{{"Delivery challan", "Delivery challan", false}, {"Tax invoice", "Tax invoice", false}}
	if got := r.radios("challan_kind"); !slices.Equal(got, want) || r.label("f-challan_kind") != "Document with the goods" {
		t.Errorf("kind radios %+v legend %q, want %+v under Document with the goods", got, r.label("f-challan_kind"), want)
	}
	for _, c := range []struct {
		id, label string
		attrs     map[string]string
	}{
		{"f-challan_no", "Challan or invoice no.", map[string]string{"maxlength": "40", "autocapitalize": "characters", "spellcheck": "false"}},
		{"f-challan_date", "Challan date", nil},
		{"f-vehicle_no", "Vehicle no.", map[string]string{"maxlength": "20", "autocapitalize": "characters", "spellcheck": "false"}},
		{"f-transporter", "Transporter", map[string]string{"maxlength": "140"}},
		{"f-eway_bill_no", "E-way bill no.", map[string]string{"maxlength": "20", "inputmode": "numeric"}},
		{"f-remarks", "Remarks", map[string]string{"maxlength": "500"}},
	} {
		if got := r.label(c.id); got != c.label {
			t.Errorf("%s label = %q, want %q", c.id, got, c.label)
		}
		for k, v := range c.attrs {
			if got := r.doc.Find("#"+c.id).AttrOr(k, ""); got != v {
				t.Errorf("%s %s = %q, want %q", c.id, k, got, v)
			}
		}
	}
	for _, s := range []string{"As printed on the document. Type Not recorded if it's lost.", "The date on the challan or invoice.", "Printed on the GRN.", s1MsgPhoto} {
		if !strings.Contains(r.text(), s) {
			t.Errorf("step 2 has no %q", s)
		}
	}
	tick := r.doc.Find(`input[type="checkbox"][name="rest_not_coming"]`)
	if _, on := tick.Attr("checked"); tick.Length() != 1 || on || tick.AttrOr("value", "") != "1" ||
		!strings.Contains(tick.Parent().Text(), "Vendor says the rest won't come") ||
		!strings.Contains(tick.Parent().Text(), "Tick only if the vendor told you nothing more will be sent on this order. The buyer is told when you submit.") {
		t.Errorf("the tick is not one unticked row with its label and hint: %d checked=%v", tick.Length(), on)
	}

	// A draft with no lines yet (made against a fully received PO) isn't
	// service-only: it has the document fields.
	full := w.po("BHARAT", "5")
	w.receiveAll(full)
	if r = w.get(w.Suresh, stepPath(w.draft(full), "delivery")); r.doc.Find(`[name="challan_no"]`).Length() != 1 {
		t.Errorf("step 2 of a draft with no lines has no Challan or invoice no.")
	}

	r = w.get(w.Suresh, stepPath(w.draft(w.servicePO()), "delivery"))
	for _, name := range []string{"challan_kind", "challan_no", "challan_date", "vehicle_no", "transporter", "eway_bill_no"} {
		if r.doc.Find(`[name="`+name+`"]`).Length() != 0 {
			t.Errorf("service-only step 2 shows %s", name)
		}
	}
	if r.doc.Find(`[name="remarks"]`).Length() != 1 || r.doc.Find(`[name="rest_not_coming"]`).Length() != 1 || !strings.Contains(r.text(), s1MsgPhoto) {
		t.Errorf("service-only step 2 lacks remarks, the tick or the photo line")
	}
}

// UX doc §5.3: step 2's Save stores what the step shows, the tick as 1 when
// ticked and 0 when not, and moves on; a service-only receipt saves only
// remarks and the tick. A 422 redraws every typed value from the post, and
// a challan date NUM can't read is refused before anything is written.
func TestGRNWeb_DeliverySave(t *testing.T) {
	w := newS1World(t)
	id := w.draft(w.po("SHREE", "10"))
	path := stepPath(id, "delivery")
	form := func(extra url.Values) url.Values {
		f := url.Values{"version": {strconv.Itoa(w.Load(t, goodsreceipt.DocType, id).Version)},
			"challan_kind": {"Tax invoice"}, "challan_no": {"SST/2627/0457"}, "challan_date": {"23-10-2026"}, "vehicle_no": {"MH12AB1234"},
			"transporter": {"Sai Logistics"}, "eway_bill_no": {"331009876543"}, "remarks": {"Two bundles wet"}, "rest_not_coming": {"1"}}
		for k, v := range extra {
			f[k] = v
		}
		return f
	}
	r := w.post(w.Suresh, path, form(nil))
	if r.status != http.StatusSeeOther || !strings.HasSuffix(r.location, "/edit/items") {
		t.Fatalf("save step 2 = %d %q %q, want 303 to Received quantities", r.status, r.location, r.errorSummary())
	}
	h := w.Load(t, goodsreceipt.DocType, id).Header
	got := []any{h["challan_kind"], h["challan_no"], h["challan_date"].(types.Date).ISO(), h["vehicle_no"], h["transporter"], h["eway_bill_no"], h["remarks"], h["rest_not_coming"]}
	want := []any{"Tax invoice", "SST/2627/0457", "2026-10-23", "MH12AB1234", "Sai Logistics", "331009876543", "Two bundles wet", true}
	if !slices.Equal(got, want) {
		t.Errorf("stored %v, want %v", got, want)
	}
	if r = w.get(w.Suresh, path); !r.doc.Find(`input[name="rest_not_coming"]`).Is("[checked]") || !r.doc.Find(`input[name="challan_kind"][value="Tax invoice"]`).Is("[checked]") {
		t.Errorf("step 2 of the saved draft doesn't show the tick and the kind")
	}

	f := form(nil)
	f.Del("rest_not_coming")
	if r = w.post(w.Suresh, path, f); r.status != http.StatusSeeOther || w.Load(t, goodsreceipt.DocType, id).Header["rest_not_coming"] != false {
		t.Errorf("untick = %d, rest_not_coming %v, want 303 and false", r.status, w.Load(t, goodsreceipt.DocType, id).Header["rest_not_coming"])
	}

	before := w.Load(t, goodsreceipt.DocType, id).Version
	r = w.post(w.Suresh, path, form(url.Values{"challan_date": {"25-10-2026"}, "challan_no": {"DC-77"}}))
	if r.status != http.StatusUnprocessableEntity || r.errorSummary() != "Challan date can't be after the receipt date." {
		t.Errorf("challan date after the receipt = %d %q, want 422", r.status, r.errorSummary())
	}
	if r.doc.Find(`#f-challan_no`).AttrOr("value", "") != "DC-77" || r.doc.Find(`#f-challan_date`).AttrOr("value", "") != "25-10-2026" ||
		!r.doc.Find(`input[name="rest_not_coming"]`).Is("[checked]") || !r.doc.Find(`input[name="challan_kind"][value="Tax invoice"]`).Is("[checked]") ||
		r.doc.Find(`#f-remarks`).Text() != "Two bundles wet" {
		t.Errorf("the 422 redraw lost a typed value")
	}
	if got := r.doc.Find("#f-challan_date-msg").Text(); got != "Challan date can't be after the receipt date." {
		t.Errorf("message by the challan date = %q", got)
	}
	if r = w.post(w.Suresh, path, form(url.Values{"challan_date": {"x"}})); r.status != http.StatusUnprocessableEntity || r.errorSummary() == "" {
		t.Errorf("challan date x = %d %q, want 422 with NUM's text", r.status, r.errorSummary())
	}
	if after := w.Load(t, goodsreceipt.DocType, id).Version; after != before {
		t.Errorf("version %d after the refusals, want %d", after, before)
	}

	svc := w.draft(w.servicePO())
	r = w.post(w.Suresh, stepPath(svc, "delivery"), url.Values{"version": {strconv.Itoa(w.Load(t, goodsreceipt.DocType, svc).Version)},
		"challan_no": {"DC-1"}, "challan_date": {"x"}, "remarks": {"Crane on site"}, "rest_not_coming": {"1"}})
	h = w.Load(t, goodsreceipt.DocType, svc).Header
	if no, _ := h["challan_no"].(string); r.status != http.StatusSeeOther || no != "" || h["remarks"] != "Crane on site" || h["rest_not_coming"] != true {
		t.Errorf("service-only save = %d: challan_no %q remarks %q tick %v, want 303, no challan, remarks and tick saved", r.status, h["challan_no"], h["remarks"], h["rest_not_coming"])
	}
}

// pageRows are the document page's summary rows as "label: value".
func (w *s1World) pageRows(id int64) []string {
	w.t.Helper()
	r := w.get(w.Suresh, "/doc/"+goodsreceipt.DocType+"/"+strconv.FormatInt(id, 10))
	if r.status != http.StatusOK {
		w.t.Fatalf("document page %d = %d", id, r.status)
	}
	return r.summaryRows()
}

// hasRow reports whether rows has one labelled label.
func hasRow(rows []string, label string) bool {
	return slices.ContainsFunc(rows, func(r string) bool { return strings.HasPrefix(r, label+": ") })
}

// UX doc §5.6, §6.3 (Module.Hide): site_id, project_id and po_line_id are
// never shown or posted; on the document page the tick shows only when it
// is on (Yes), and a service-only receipt shows no document fields while a
// goods receipt does.
func TestGRNWeb_Hide(t *testing.T) {
	w := newS1World(t)
	po := w.po("SHREE", "10", "20")
	off := w.receiveAll(po)
	rows := w.pageRows(off.ID)
	for _, label := range []string{"Site", "Project", "Vendor says the rest won't come"} {
		if hasRow(rows, label) {
			t.Errorf("approved page shows %s: %q", label, rows)
		}
	}
	if !hasRow(rows, "Challan or invoice no.") {
		t.Errorf("a goods receipt's page has no Challan or invoice no.: %q", rows)
	}

	on := w.GRN(t, w.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "1"}}, doctype.StateDraft)
	tickOn(t, w, on)
	if rows := w.pageRows(on.ID); !slices.Contains(rows, "Vendor says the rest won't come: Yes") {
		t.Errorf("approved page with the tick: %q, want Vendor says the rest won't come: Yes", rows)
	}

	svc := w.servicePO()
	rows = w.pageRows(w.GRN(t, w.Suresh, svc.ID, []grntest.Line{{POLineID: svc.Lines[0].ID, Received: "8"}}, doctype.StateApproved).ID)
	for _, label := range []string{"Document with the goods", "Challan or invoice no.", "Challan date"} {
		if hasRow(rows, label) {
			t.Errorf("service-only page shows %s: %q", label, rows)
		}
	}

	draft := w.draft(w.po("BHARAT", "5"))
	line := w.get(w.Suresh, "/doc/"+goodsreceipt.DocType+"/"+strconv.FormatInt(draft, 10)+"/lines/1")
	if line.doc.Find(`[name="po_line_id"]`).Length() != 0 {
		t.Errorf("the line page renders po_line_id")
	}
	line.doc.Find("form").Each(func(_ int, s *goquery.Selection) {
		if s.Find(`[name="site_id"], [name="project_id"]`).Length() != 0 {
			t.Errorf("a form posts site_id or project_id")
		}
	})
}

// tickOn saves draft g's tick through step 2 and approves it.
func tickOn(t *testing.T, w *s1World, g doc.Doc) {
	t.Helper()
	r := w.post(w.Suresh, stepPath(g.ID, "delivery"), url.Values{"version": {strconv.Itoa(g.Version)},
		"challan_kind": {"Delivery challan"}, "challan_no": {"DC-1042"}, "rest_not_coming": {"1"}})
	if r.status != http.StatusSeeOther {
		t.Fatalf("tick on = %d %q", r.status, r.errorSummary())
	}
	g = w.Load(t, goodsreceipt.DocType, g.ID)
	if _, err := w.Engine.Do(t.Context(), w.Suresh, goodsreceipt.DocType, g.ID, doctype.ActionSubmit, doc.ActionInput{Version: g.Version}); err != nil {
		t.Fatalf("submit: %v", err)
	}
}
