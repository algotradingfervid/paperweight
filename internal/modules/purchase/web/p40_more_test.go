package purchaseweb

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// P40 · CD-359 (7), UI v2 §5.5 item 3: the purchase fields most people skip
// (Vendor quote ref, Earlier PO no. and date on the order's Terms, Note on
// the request's line page) sit in their card's closed More details group,
// which opens when a field inside has a value or an error. Already received
// isn't one: every saved line stores its Default 0, which would open the
// group on every saved line's page.

// p40More is the page's More details group, which must be the only one, and
// whether it is open.
func p40More(t *testing.T, p p15dPage) (*goquery.Selection, bool) {
	t.Helper()
	if p.status != 200 && p.status != 422 {
		t.Fatalf("status %d: %s", p.status, p.text())
	}
	d := p.doc.Find("form details").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.HasPrefix(p15dCollapse(s.Find("summary").Text()), "More details")
	})
	if d.Length() != 1 {
		t.Fatalf("%d More details groups in the form, want 1: %s", d.Length(), p.text())
	}
	_, open := d.Attr("open")
	return d, open
}

// p40Want checks that field's input is inside the group and the group is
// open as wanted.
func p40Want(t *testing.T, p p15dPage, field string, open bool) {
	t.Helper()
	d, got := p40More(t, p)
	if d.Find(`[name="`+field+`"]`).Length() != 1 {
		t.Errorf("%s is not inside More details", field)
	}
	if got != open {
		t.Errorf("More details open = %v, want %v", got, open)
	}
}

// Vendor quote ref, Earlier PO no. and Earlier PO date on the order's
// Terms: closed on a draft with all three blank, open with a stored value,
// open on a POST with an error at the field. An earlier PO date can't be
// stored without its number (spec §4), so its stored row has both; Earlier
// PO no.'s error row posts it blank with a line's Already received set, so
// only the error opens the group.
func TestPO_TermsMoreDetails(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t, purchasetest.WithNoRules()), nil)
	env := w.env
	termsOf := func(id int64) string { return p15dDoc(purchase.DocType, id) + "/edit/terms" }
	long := func(n int) string { return strings.Repeat("Q", n) }
	for _, c := range []struct {
		field  string
		stored map[string]string
		post   map[string]string // posted over the GET form; refused
	}{
		{"vendor_quote_ref", map[string]string{"vendor_quote_ref": "QT-2026-118"}, map[string]string{"vendor_quote_ref": long(141)}},
		{"earlier_po_no", map[string]string{"earlier_po_no": "P-17"}, map[string]string{"earlier_po_no": ""}},
		{"earlier_po_date", map[string]string{"earlier_po_no": "P-17", "earlier_po_date": "15-09-2026"}, map[string]string{"earlier_po_date": "31-02-2026"}},
	} {
		t.Run(c.field, func(t *testing.T) {
			fresh := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
			page := w.get(t, env.Priya, termsOf(fresh.ID))
			p40Want(t, page, c.field, false)
			if d, _ := p40More(t, page); p15dCollapse(d.Find("summary").Text()) != "More details · vendor quote ref, earlier PO no., earlier PO date" {
				t.Errorf("summary = %q, want the three fields named", p15dCollapse(d.Find("summary").Text()))
			}

			kept := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderHeader(c.stored))
			p40Want(t, w.get(t, env.Priya, termsOf(kept.ID)), c.field, true)

			var po int64
			if c.field == "earlier_po_no" {
				line := p15aCement(t, w, "40", "345")
				line["already_received"] = "5"
				po = purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(),
					purchasetest.OrderHeader(map[string]string{"earlier_po_no": "P-17"}), purchasetest.OrderLines(line)).ID
			} else {
				po = purchasetest.NewOrder(t, env, purchasetest.StopAtDraft()).ID
			}
			form := p15aFormValues(t, w.get(t, env.Priya, termsOf(po)))
			for k, v := range c.post {
				form.Set(k, v)
			}
			a := w.do(t, env.Priya, "POST", termsOf(po), form)
			if a.status != 422 {
				t.Fatalf("POST = %d, want 422: %s", a.status, a.text())
			}
			if a.doc.Find("#f-"+c.field+"-msg").Length() != 1 {
				t.Fatalf("no error at %s: %s", c.field, a.text())
			}
			p40Want(t, a, c.field, true)
		})
	}
}

// Note on the request's line page: closed on a new line, open on a line
// with a note, open on a POST with one too long.
func TestPR_LineNoteMoreDetails(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	fresh := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())
	path := p15dDoc(purchase.RequestDocType, fresh.ID) + "/lines/new"
	p40Want(t, w.get(t, env.Suresh, path), "note", false)

	kept := w.prA(t, purchasetest.StopAtDraft())
	p40Want(t, w.get(t, env.Suresh, p15dDoc(purchase.RequestDocType, kept.ID)+"/lines/1"), "note", true)

	form := p15aFormValues(t, w.get(t, env.Suresh, path))
	for k, v := range w.row(t, "CEMENT-OPC", "BAG", "40", strings.Repeat("n", 201)) {
		form.Set(k, v)
	}
	a := w.do(t, env.Suresh, "POST", path, form)
	if a.status != 422 || a.doc.Find("#f-note-msg").Length() != 1 {
		t.Fatalf("POST a note of 201 characters = %d, want 422 with its error: %s", a.status, a.text())
	}
	p40Want(t, a, "note", true)
}
