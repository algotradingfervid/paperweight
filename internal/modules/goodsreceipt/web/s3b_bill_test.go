package grnweb_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// MOD-GRN S3b: the Bill details card on the document page (UX doc §5.7),
// its no-JS refusals (PRG through the session), the read-side warnings,
// G11's duplicate challan on the step, check and document pages, and the
// e-way Notice guard. Driven over HTTP on s1World (real engine, real
// auth.Service): Suresh writes, Priya only reads GRNs, OwnReceiver reads
// and writes only its own. The clock is authtest.T0, 24-10-2026.

// The S3b texts (UX doc §5.7 copy table; spec §3.3; CD-279 (3), (4)).
const (
	s3bEmpty     = "No bill details yet."
	s3bHint      = "Add the invoice photo under Attachments first: adding a file reloads the page."
	s3bAccepted  = "Accepted value before tax is ₹6,250.00."
	s3bSaved     = "Bill details saved."
	s3bCopiedTax = "Copied from the tax invoice entered at the gate. Check and save."
	s3bStale     = "Someone else changed Bill details after you opened it. Reload it and make your changes again."
	s3bNoReq     = "Vendor invoice no. is required."
	s3bNotAppr   = "Add bill details after the receipt is approved."
	s3bInvoice   = "INV-889"
	s3bReload    = "Reload Bill details"
	s3bKeep      = "Your entries stay on this page until you reload, so you can copy them."
)

// s3bPO is an approved PO of 1,200 KGS at ₹62.50.
func (w *s1World) s3bPO() doc.Doc {
	w.t.Helper()
	return purchasetest.NewOrder(w.t, w.Env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(map[string]string{"qty": "1200", "price": "62.5"}))
}

// s3bGRN is a GRN as p receiving 100 KGS of po's line 1 (₹6,250.00
// accepted), stopped at stop.
func (w *s1World) s3bGRN(p auth.Principal, po doc.Doc, stop doctype.State) doc.Doc {
	w.t.Helper()
	return w.GRN(w.t, p, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "100"}}, stop)
}

// s3bBillPath is GRN id's bill route.
func s3bBillPath(id int64) string {
	return "/modules/goodsreceipt/receipts/" + strconv.FormatInt(id, 10) + "/bill"
}

// s3bForm is what the card's form posts.
func s3bForm(version int, no, date, amount string) url.Values {
	return url.Values{"version": {strconv.Itoa(version)}, "invoice_no": {no}, "invoice_date": {date}, "invoice_amount": {amount}}
}

// s3bSave posts form to GRN id's bill route as p, with htmx or without.
func (w *s1World) s3bSave(p auth.Principal, id int64, form url.Values, htmx bool) s1Page {
	w.t.Helper()
	w.htmx = htmx
	defer func() { w.htmx = false }()
	return w.post(p, s3bBillPath(id), form)
}

// s3bMustSave stores bill details on GRN id as p through the Service.
func (w *s1World) s3bMustSave(p auth.Principal, id int64, version int, no, amount string) {
	w.t.Helper()
	if err := w.Service.SaveBill(w.t.Context(), p, id, version, goodsreceipt.BillInput{InvoiceNo: no, InvoiceDate: "23-10-2026", InvoiceAmount: amount}); err != nil {
		w.t.Fatalf("save bill details on GRN %d as %s: %v", id, p.Name, err)
	}
}

// s3bCard is the page's Bill details card (id "bill"), or an empty selection.
func (p s1Page) s3bCard() *goquery.Selection { return p.doc.Find("#bill") }

// s3bText is the card's text, white space collapsed.
func (p s1Page) s3bText() string { return s2Norm(p.s3bCard().Text()) }

// s3bValue is the value of the card's input name, and whether it has one.
func (p s1Page) s3bValue(name string) (string, bool) {
	s := p.s3bCard().Find(`input[name="` + name + `"]`)
	if s.Length() == 0 {
		return "", false
	}
	return s.AttrOr("value", ""), true
}

// s3bWantForm asserts the card is the form posting to GRN id's bill route
// at version with the three values.
func s3bWantForm(t *testing.T, what string, p s1Page, id int64, version, no, date, amount string) {
	t.Helper()
	if n := p.s3bCard().Find(`form[action="` + s3bBillPath(id) + `"][hx-post="` + s3bBillPath(id) + `"][hx-target="#bill"]`).Length(); n != 1 {
		t.Fatalf("%s: %d forms posting to the bill route with htmx into #bill, want 1 (card %q)", what, n, p.s3bText())
	}
	if n := p.s3bCard().Find(".summary").Length(); n != 0 {
		t.Errorf("%s: %d summary lists beside the form, want none", what, n)
	}
	for name, want := range map[string]string{"version": version, "invoice_no": no, "invoice_date": date, "invoice_amount": amount} {
		if got, ok := p.s3bValue(name); !ok || got != want {
			t.Errorf("%s: input %s = %q (present %v), want %q", what, name, got, ok, want)
		}
	}
}

// s3bWantRead asserts the card is the read view: no inputs, the rows, and
// the Change link only when change.
func s3bWantRead(t *testing.T, what string, p s1Page, change bool, rows ...string) {
	t.Helper()
	card := p.s3bCard()
	if card.Length() != 1 {
		t.Fatalf("%s: %d Bill details cards, want 1", what, card.Length())
	}
	if n := card.Find("input, form").Length(); n != 0 {
		t.Errorf("%s: %d inputs or forms in the read view, want none", what, n)
	}
	var got []string
	card.Find(".summary-row").Each(func(_ int, s *goquery.Selection) {
		got = append(got, s2Norm(s.Find("dt").Text())+": "+s2Norm(s.Find("dd").Text()))
	})
	if strings.Join(got, "|") != strings.Join(rows, "|") {
		t.Errorf("%s: rows %q, want %q", what, got, rows)
	}
	card.Find("p").Each(func(_ int, s *goquery.Selection) {
		if s2Norm(s.Text()) == "" {
			t.Errorf("%s: an empty paragraph in the card", what)
		}
	})
	link := card.Find(".card-head a")
	switch {
	case change && (link.AttrOr("href", "") != "?bill=edit#bill" || s2Norm(link.Text()) != "Change Bill details"):
		t.Errorf("%s: Change link %q to %q, want Change Bill details to ?bill=edit#bill", what, s2Norm(link.Text()), link.AttrOr("href", ""))
	case !change && link.Length() != 0:
		t.Errorf("%s: a Change link %q, want none", what, s2Norm(link.Text()))
	}
}

// s3bBanners are the card's banners of kind, without the kind's word.
func (p s1Page) s3bBanners(kind string) []string {
	var out []string
	p.s3bCard().Find(".banner--" + kind).Each(func(_ int, s *goquery.Selection) {
		text := s2Norm(s.Text())
		for _, word := range []string{"Warning:", "Information:", "Error:"} {
			text = strings.TrimSpace(strings.TrimPrefix(text, word))
		}
		out = append(out, text)
	})
	return out
}

// UX doc §5.7 (spec §3.3; CD-279 (4)) for a writer and a reader on an
// approved GRN: Suresh gets the empty form (No bill details yet., the
// attachments hint, Vendor invoice no. and Invoice date marked required,
// Invoice total with the accepted value as its hint, Save bill details as
// a secondary button) and Priya only the empty text. An htmx save answers
// the read view with the polite status and Last changed by; a total equal
// to the accepted value draws no information line. Later renders are the
// read view, with Change only for Suresh; ?bill=edit opens the form with
// the stored values at the row's version. A total that differs draws the
// information line in the read view, right after the save and on later
// renders, for both viewers.
func TestGRNBill_CardWriterAndReader(t *testing.T) {
	w := newS1World(t)
	g := w.s3bGRN(w.Suresh, w.s3bPO(), doctype.StateApproved)

	r := s3OK(t, w.get(w.Suresh, s3Doc(g.ID)), "Suresh, no bill")
	s3bWantForm(t, "Suresh, no bill", r, g.ID, "0", "", "", "")
	text := r.s3bText()
	for _, want := range []string{"Bill details", s3bEmpty, s3bHint, "Vendor invoice no.", "Invoice date", "Invoice total"} {
		if !strings.Contains(text, want) {
			t.Errorf("empty form %q, want %q", text, want)
		}
	}
	card := r.s3bCard()
	for name, want := range map[string]bool{"invoice_no": true, "invoice_date": true, "invoice_amount": false} {
		id := "f-" + name
		_, aria := card.Find("#" + id).Attr("aria-required")
		star := card.Find(`label[for="`+id+`"] .field__req`).Length() == 1
		if aria != want || star != want {
			t.Errorf("%s: aria-required %v, asterisk %v; want both %v (CD-279 (4))", name, aria, star, want)
		}
	}
	if got := card.Find("#f-invoice_no").AttrOr("maxlength", ""); got != "40" {
		t.Errorf("Vendor invoice no. maxlength %q, want 40 (spec §3.3)", got)
	}
	for chip, want := range map[string]string{"Today": s1Today, "Yesterday": "23-10-2026"} {
		b := card.Find(`button[data-set-date][aria-controls="f-invoice_date"]`).FilterFunction(func(_ int, s *goquery.Selection) bool { return s2Norm(s.Text()) == chip })
		if got := b.AttrOr("data-set-date", ""); b.Length() != 1 || got != want {
			t.Errorf("Invoice date %s chip: %d, date %q; want one at %q", chip, b.Length(), got, want)
		}
	}
	if got := card.Find(`input[type=date][data-date-picker="f-invoice_date"]`).AttrOr("max", ""); got != "2026-10-24" {
		t.Errorf("Invoice date's calendar max = %q, want today (the bill refuses a future date)", got)
	}
	if got := s2Norm(card.Find("#f-invoice_amount-hint").Text()); got != s3bAccepted {
		t.Errorf("Invoice total hint %q, want %q", got, s3bAccepted)
	}
	btn := card.Find(`form button[type="submit"]`)
	if s2Norm(btn.Text()) != "Save bill details" || !btn.HasClass("btn--secondary") {
		t.Errorf("save button %q (class %q), want a secondary Save bill details", s2Norm(btn.Text()), btn.AttrOr("class", ""))
	}
	if card.Find(".card-head a").Length() != 0 || len(r.s3bBanners("info")) != 0 {
		t.Errorf("empty form: a Change link or an information line %q", r.s3bBanners("info"))
	}

	r = s3OK(t, w.get(w.Priya, s3Doc(g.ID)), "Priya, no bill")
	if r.s3bText() != "Bill details "+s3bEmpty || r.s3bCard().Find("input, form").Length() != 0 {
		t.Errorf("Priya, no bill: card %q, want only the empty text and no form", r.s3bText())
	}

	r = w.s3bSave(w.Suresh, g.ID, s3bForm(0, s3bInvoice, "23-10-2026", "6,250.00"), true)
	if r.status != http.StatusOK {
		t.Fatalf("htmx save = %d %q", r.status, r.text())
	}
	rows := []string{"Vendor invoice no.: " + s3bInvoice, "Invoice date: 23-10-2026", "Invoice total: ₹6,250.00"}
	s3bWantRead(t, "after the htmx save", r, true, rows...)
	if got := s2Norm(r.s3bCard().Find(`[role="status"]`).Text()); got != s3bSaved {
		t.Errorf("after the htmx save: status %q, want %q", got, s3bSaved)
	}
	changed := "Last changed by " + w.Suresh.Name + " on 24-10-2026"
	if !strings.Contains(r.s3bText(), changed) || len(r.s3bBanners("info")) != 0 || len(r.s3bBanners("warning")) != 0 {
		t.Errorf("after the htmx save: card %q, want %q and no information line (total = accepted) or warning", r.s3bText(), changed)
	}

	r = s3OK(t, w.get(w.Suresh, s3Doc(g.ID)), "Suresh, saved")
	s3bWantRead(t, "Suresh, saved", r, true, rows...)
	if strings.Contains(r.s3bText(), s3bSaved) || !strings.Contains(r.s3bText(), changed) {
		t.Errorf("Suresh, saved: card %q, want %q without %q", r.s3bText(), changed, s3bSaved)
	}
	s3bWantRead(t, "Priya, saved", s3OK(t, w.get(w.Priya, s3Doc(g.ID)), "Priya, saved"), false, rows...)
	s3bWantRead(t, "Priya, ?bill=edit", s3OK(t, w.get(w.Priya, s3Doc(g.ID)+"?bill=edit"), "Priya edit"), false, rows...)

	r = s3OK(t, w.get(w.Suresh, s3Doc(g.ID)+"?bill=edit"), "Suresh, change")
	s3bWantForm(t, "Suresh, change", r, g.ID, "1", s3bInvoice, "23-10-2026", "6,250.00")
	if strings.Contains(r.s3bText(), s3bEmpty) {
		t.Errorf("Suresh, change: card %q says %q", r.s3bText(), s3bEmpty)
	}

	r = w.s3bSave(w.Suresh, g.ID, s3bForm(1, s3bInvoice, "23-10-2026", "7,375.00"), true)
	if got := r.s3bBanners("info"); r.status != http.StatusOK || len(got) != 1 || got[0] != s3bAccepted {
		t.Errorf("htmx save with a different total = %d, information %q, want 200 and [%q]", r.status, got, s3bAccepted)
	}
	for name, p := range map[string]auth.Principal{"Suresh": w.Suresh, "Priya": w.Priya} {
		r := s3OK(t, w.get(p, s3Doc(g.ID)), name+", different total")
		if got := r.s3bBanners("info"); len(got) != 1 || got[0] != s3bAccepted {
			t.Errorf("%s, different total: information %q, want [%q]", name, got, s3bAccepted)
		}
	}
}

// UX doc §5.7, CD-272 Gap 5 (GRN-046): with a tax invoice at the gate and
// no bill details, Suresh's form offers the challan no. and date under the
// note, at version 0; Priya sees only the empty text, without the offer.
func TestGRNBill_TaxInvoiceOffer(t *testing.T) {
	w := newS1World(t)
	po := w.s3bPO()
	id := w.draft(po)
	if r := w.post(w.Suresh, stepPath(id, "delivery"), url.Values{"version": {strconv.Itoa(w.s2Version(id))},
		"challan_kind": {"Tax invoice"}, "challan_no": {"TI-77"}, "challan_date": {"22-10-2026"}}); r.status != http.StatusSeeOther {
		t.Fatalf("save step 2 = %d %q", r.status, r.errorSummary())
	}
	s2WantStep(t, w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "100"}), "/edit/check", "save 100")
	w.s3Submit(id)

	r := s3OK(t, w.get(w.Suresh, s3Doc(id)), "Suresh")
	s3bWantForm(t, "Suresh", r, id, "0", "TI-77", "22-10-2026", "")
	if got := r.s3bBanners("info"); len(got) != 1 || got[0] != s3bCopiedTax {
		t.Errorf("Suresh: information %q, want [%q]", got, s3bCopiedTax)
	}
	r = s3OK(t, w.get(w.Priya, s3Doc(id)), "Priya")
	if r.s3bText() != "Bill details "+s3bEmpty {
		t.Errorf("Priya: card %q, want only %q (the copy is only an offer)", r.s3bText(), s3bEmpty)
	}
}

// UX doc §5.7 refusals, with htmx and without (the Comments panel's PRG;
// CD-279 (4)): a blank invoice no. answers htmx 422 with the card's error
// summary linking to the field, the message at the field and the typed
// values kept; without JavaScript it redirects (303) to the page at #bill,
// and the next load draws the same from the session, once. A stale
// version answers 409 with DOC's text for Bill details, a Reload link and
// the typed values at the posted version, also as the form over a stored
// row; without JavaScript the same after the redirect. A post on a
// cancelled receipt without bill details is the state text, drawn in the
// card on the next load though such a receipt has no card otherwise.
func TestGRNBill_Refusals(t *testing.T) {
	w := newS1World(t)
	po := w.s3bPO()
	// GRN ids from 1001, so an id in another base or a cut id is a
	// different key and URL from the decimal one.
	if _, err := w.DB.SQL().ExecContext(t.Context(), `INSERT INTO sqlite_sequence (name, seq) VALUES ('goods_receipt', 1000)`); err != nil {
		t.Fatal(err)
	}
	g := w.s3bGRN(w.Suresh, po, doctype.StateApproved)
	other := w.s3bGRN(w.Suresh, po, doctype.StateApproved)
	if g.ID <= 1000 {
		t.Fatalf("GRN id %d, want above 1000", g.ID)
	}

	noStatus := func(what string, r s1Page) {
		t.Helper()
		if n := r.s3bCard().Find(`[role="status"]`).Length(); n != 0 {
			t.Errorf("%s: %d saved statuses on a refusal, want none", what, n)
		}
	}
	wantInvalid := func(what string, r s1Page) {
		t.Helper()
		noStatus(what, r)
		s3bWantForm(t, what, r, g.ID, "0", "", "23-10-2026", "7,375.00")
		sum := r.s3bCard().Find(".error-summary li a")
		if sum.Length() != 1 || sum.AttrOr("href", "") != "#f-invoice_no" || s2Norm(sum.Text()) != s3bNoReq {
			t.Errorf("%s: summary %q to %q, want %q to #f-invoice_no", what, s2Norm(sum.Text()), sum.AttrOr("href", ""), s3bNoReq)
		}
		if got := s2Norm(r.s3bCard().Find("#f-invoice_no-msg").Text()); got != s3bNoReq {
			t.Errorf("%s: message at the field %q, want %q", what, got, s3bNoReq)
		}
		if got := r.s3bBanners("danger"); len(got) != 0 {
			t.Errorf("%s: error banners %q, want none (the summary carries the field errors)", what, got)
		}
	}
	r := w.s3bSave(w.Suresh, g.ID, s3bForm(0, "", "23-10-2026", "7,375.00"), true)
	if r.status != http.StatusUnprocessableEntity {
		t.Errorf("htmx blank no. = %d, want 422", r.status)
	}
	wantInvalid("htmx blank no.", r)

	w.keep = true
	r = w.s3bSave(w.Suresh, g.ID, s3bForm(0, "", "23-10-2026", "7,375.00"), false)
	if r.status != http.StatusSeeOther || r.location != s3Doc(g.ID)+"#bill" {
		t.Fatalf("no-JS blank no. = %d %q, want 303 to the page at #bill", r.status, r.location)
	}
	// The kept post is GRN g's: another GRN's page in the same session
	// draws its own empty form and leaves it for g's.
	r = s3OK(t, w.get(w.Suresh, s3Doc(other.ID)), "another GRN")
	s3bWantForm(t, "another GRN", r, other.ID, "0", "", "", "")
	if r.s3bCard().Find(".error-summary").Length() != 0 || len(r.s3bBanners("danger")) != 0 {
		t.Errorf("another GRN: shows g's refusal: %q", r.s3bText())
	}
	wantInvalid("no-JS blank no., next load", s3OK(t, w.get(w.Suresh, s3Doc(g.ID)), "next load"))
	r = s3OK(t, w.get(w.Suresh, s3Doc(g.ID)), "load after")
	s3bWantForm(t, "load after", r, g.ID, "0", "", "", "")
	if r.s3bCard().Find(".error-summary").Length() != 0 {
		t.Errorf("load after: the error summary is still there")
	}

	w.s3bMustSave(w.Suresh, g.ID, 0, s3bInvoice, "")
	wantStale := func(what string, r s1Page) {
		t.Helper()
		noStatus(what, r)
		s3bWantForm(t, what, r, g.ID, "0", "INV-NEW", "23-10-2026", "")
		// The 409 banner is the ConflictBanner (CD-362, CD-365): a warning
		// alert with DOC's text, the keep-your-entries line and a Reload
		// button naming what it reloads.
		got := r.s3bBanners("warning")
		if len(got) != 1 || !strings.HasPrefix(got[0], s3bStale) {
			t.Errorf("%s: warning banners %q, want [%q …]", what, got, s3bStale)
		}
		if got := r.s3bBanners("danger"); len(got) != 0 {
			t.Errorf("%s: error banners %q, want none", what, got)
		}
		b := r.s3bCard().Find(".banner--warning")
		if b.AttrOr("role", "") != "alert" {
			t.Errorf("%s: warning banner role %q, want alert", what, b.AttrOr("role", ""))
		}
		if got := s2Norm(b.Find(".banner-body > p").Eq(1).Text()); got != s3bKeep {
			t.Errorf("%s: second line %q, want %q", what, got, s3bKeep)
		}
		if a := b.Find("a.btn.btn--secondary"); a.Length() != 1 || s2Norm(a.Text()) != s3bReload || a.AttrOr("href", "") != s3Doc(g.ID)+"#bill" {
			t.Errorf("%s: %d buttons %q to %q, want %s to the page at #bill", what, a.Length(), s2Norm(a.Text()), a.AttrOr("href", ""), s3bReload)
		}
	}
	w.keep = false
	r = w.s3bSave(w.Suresh, g.ID, s3bForm(0, "INV-NEW", "23-10-2026", ""), true)
	if r.status != http.StatusConflict {
		t.Errorf("htmx stale = %d, want 409", r.status)
	}
	wantStale("htmx stale", r)
	w.keep = true
	r = w.s3bSave(w.Suresh, g.ID, s3bForm(0, "INV-NEW", "23-10-2026", ""), false)
	if r.status != http.StatusSeeOther || r.location != s3Doc(g.ID)+"#bill" {
		t.Fatalf("no-JS stale = %d %q, want 303 to the page at #bill", r.status, r.location)
	}
	wantStale("no-JS stale, next load", s3OK(t, w.get(w.Suresh, s3Doc(g.ID)), "next load"))
	r = s3OK(t, w.get(w.Suresh, s3Doc(g.ID)), "load after")
	s3bWantRead(t, "load after the stale one", r, true, "Vendor invoice no.: "+s3bInvoice, "Invoice date: 23-10-2026")
	if got := r.s3bBanners("info"); len(got) != 0 {
		t.Errorf("load after the stale one: information %q, want none without a total", got)
	}

	c := w.s3bGRN(w.Suresh, po, doctype.StateApproved)
	w.Cancel(t, w.Ravi, c.ID, "Counted twice")
	r = w.s3bSave(w.Suresh, c.ID, s3bForm(0, s3bInvoice, "23-10-2026", ""), false)
	if r.status != http.StatusSeeOther {
		t.Fatalf("no-JS on a cancelled receipt = %d, want 303", r.status)
	}
	r = s3OK(t, w.get(w.Suresh, s3Doc(c.ID)), "cancelled, next load")
	if got := r.s3bBanners("danger"); len(got) != 1 || got[0] != s3bNotAppr || r.s3bCard().Find("form").Length() != 0 {
		t.Errorf("cancelled, next load: error banners %q, want [%q] and no form", got, s3bNotAppr)
	}
	if b := r.s3bCard().Find(".banner"); b.Length() != 1 || strings.Contains(b.Text(), s3bKeep) || b.Find("a").Length() != 0 {
		t.Errorf("cancelled, next load: %d banners %q, want only the danger one without the conflict line or a button", b.Length(), s2Norm(b.Text()))
	}
	if r := s3OK(t, w.get(w.Suresh, s3Doc(c.ID)), "cancelled, load after"); r.s3bCard().Length() != 0 {
		t.Errorf("cancelled, load after: a Bill details card %q, want none", r.s3bText())
	}

	// A draft: its writer's page is the check page, which has no card, so
	// nothing is kept to show after approval.
	d := w.s3bGRN(w.Suresh, po, doctype.StateDraft)
	if r := w.s3bSave(w.Suresh, d.ID, s3bForm(0, s3bInvoice, "23-10-2026", ""), false); r.status != http.StatusSeeOther {
		t.Fatalf("no-JS on a draft = %d, want 303", r.status)
	}
	w.s3Submit(d.ID)
	r = s3OK(t, w.get(w.Suresh, s3Doc(d.ID)), "draft, approved after")
	s3bWantForm(t, "draft, approved after", r, d.ID, "0", "", "", "")
	if got := r.s3bBanners("danger"); len(got) != 0 {
		t.Errorf("draft, approved after: error banners %q, want none", got)
	}
}

// UX doc §5.7 by state: a draft has no card (Priya's read-only page); a
// cancelled receipt with bill details shows them read-only, without
// Change, also at ?bill=edit; a cancelled receipt without any has no card.
func TestGRNBill_DraftAndCancelled(t *testing.T) {
	w := newS1World(t)
	po := w.s3bPO()
	d := w.s3bGRN(w.Suresh, po, doctype.StateDraft)
	if r := s3OK(t, w.get(w.Priya, s3Doc(d.ID)), "Priya, draft"); r.s3bCard().Length() != 0 {
		t.Errorf("draft: a Bill details card %q", r.s3bText())
	}

	g := w.s3bGRN(w.Suresh, po, doctype.StateApproved)
	w.s3bMustSave(w.Suresh, g.ID, 0, s3bInvoice, "6,250.00")
	w.Cancel(t, w.Ravi, g.ID, "Counted twice")
	rows := []string{"Vendor invoice no.: " + s3bInvoice, "Invoice date: 23-10-2026", "Invoice total: ₹6,250.00"}
	s3bWantRead(t, "cancelled with a bill", s3OK(t, w.get(w.Suresh, s3Doc(g.ID)), "cancelled"), false, rows...)
	s3bWantRead(t, "cancelled with a bill, ?bill=edit", s3OK(t, w.get(w.Suresh, s3Doc(g.ID)+"?bill=edit"), "cancelled edit"), false, rows...)

	n := w.s3bGRN(w.Suresh, po, doctype.StateApproved)
	w.Cancel(t, w.Ravi, n.ID, "Counted twice")
	if r := s3OK(t, w.get(w.Suresh, s3Doc(n.ID)), "cancelled, no bill"); r.s3bCard().Length() != 0 {
		t.Errorf("cancelled without a bill: a card %q", r.s3bText())
	}
}

// Spec §3.3, CD-279 (3), CD-233, CD-324 on the real auth.Service: the
// duplicate invoice warning is part of the card on every later render.
// OwnReceiver, who reads only its own GRNs, sees "another receipt" on its
// GRN; Suresh (writer) and Priya (reader) see the other GRN named.
func TestGRNBill_Duplicate(t *testing.T) {
	w := newS1World(t)
	own := w.OwnReceiver(t)
	po := w.s3bPO()
	a := w.s3bGRN(w.Suresh, po, doctype.StateApproved)
	b := w.s3bGRN(own, po, doctype.StateApproved)
	w.s3bMustSave(w.Suresh, a.ID, 0, "INV-500", "")
	w.s3bMustSave(own, b.ID, 0, "INV-500", "")

	r := s3OK(t, w.get(own, s3Doc(b.ID)), "OwnReceiver")
	if got := r.s3bBanners("warning"); len(got) != 1 || got[0] != "Invoice INV-500 is also on another receipt." || strings.Contains(r.s3bText(), a.Number) {
		t.Errorf("OwnReceiver: warnings %q, want only the another-receipt form", got)
	}
	named := "Invoice INV-500 is also on " + b.Number + "."
	for name, p := range map[string]auth.Principal{"Suresh": w.Suresh, "Priya": w.Priya} {
		if got := s3OK(t, w.get(p, s3Doc(a.ID)), name).s3bBanners("warning"); len(got) != 1 || got[0] != named {
			t.Errorf("%s: warnings %q, want [%q]", name, got, named)
		}
	}
}

// Spec v0.5 §3.3, CD-279 (11), (12) (GRN-042): the cancelled original's
// bill details are copied to its amendment on approval, and the
// amendment's card shows them with "Copied from {GR number}" and Change;
// the original keeps its own, read-only, without that line.
func TestGRNBill_AmendmentCopied(t *testing.T) {
	w := newS1World(t)
	po := w.s3bPO()
	a := w.s3bGRN(w.Suresh, po, doctype.StateApproved)
	w.s3bMustSave(w.Suresh, a.ID, 0, s3bInvoice, "")
	w.Cancel(t, w.Ravi, a.ID, "Wrong count")
	am := w.Amend(t, w.Suresh, a.ID)
	w.s3Submit(am.ID)

	rows := []string{"Vendor invoice no.: " + s3bInvoice, "Invoice date: 23-10-2026"}
	r := s3OK(t, w.get(w.Suresh, s3Doc(am.ID)), "amendment")
	s3bWantRead(t, "amendment", r, true, rows...)
	if !strings.Contains(r.s3bText(), "Copied from "+a.Number) {
		t.Errorf("amendment: card %q, want %q", r.s3bText(), "Copied from "+a.Number)
	}
	r = s3OK(t, w.get(w.Suresh, s3Doc(a.ID)), "original")
	s3bWantRead(t, "original", r, false, rows...)
	if strings.Contains(r.s3bText(), "Copied from") {
		t.Errorf("original: card %q says Copied from", r.s3bText())
	}

	// The line names the original: not for a viewer who can't read it
	// (CD-233). Suresh is restricted to METRO-SITE; moving the original's
	// site in the database is the only way an amendment's reader loses it.
	site := func(code string) {
		t.Helper()
		if _, err := w.DB.SQL().ExecContext(t.Context(), `UPDATE goods_receipt SET site_id = ? WHERE id = ?`, w.id("site", code), a.ID); err != nil {
			t.Fatal(err)
		}
	}
	site("TECH-SITE")
	if r := s3OK(t, w.get(w.Suresh, s3Doc(am.ID)), "amendment, original hidden"); strings.Contains(r.s3bText(), "Copied from") || strings.Contains(r.s3bText(), a.Number) {
		t.Errorf("amendment, original hidden: card %q names it", r.s3bText())
	}
	site("METRO-SITE")

	// After a change on the amendment the line goes: it is no longer the copy.
	w.s3bMustSave(w.Suresh, am.ID, 1, "INV-890", "")
	if r := s3OK(t, w.get(w.Suresh, s3Doc(am.ID)), "amendment, changed"); strings.Contains(r.s3bText(), "Copied from") {
		t.Errorf("amendment, changed: card %q still says Copied from", r.s3bText())
	}
}

// CD-337 (6) on the pages (G11): a GRN whose challan no. is on another
// non-cancelled GRN of the vendor warns on the Quantities step,
// the check page and the document page, naming the other GRN for Suresh;
// OwnReceiver, who can't read Suresh's GRNs, gets "another receipt" on its
// own GRN's page (CD-233, CD-324). A cancelled receipt's page doesn't warn.
func TestGRNWeb_DuplicateChallan(t *testing.T) {
	w := newS1World(t)
	po := w.s3bPO()
	a := w.s3bGRN(w.Suresh, po, doctype.StateApproved) // challan DC-1042, as every grntest receipt
	warn := "Challan DC-1042 is also on " + a.Number + "."

	id := w.draft(po)
	w.s2Delivery(id)
	if r := w.s2Items(id); !r.s3Has("warning", warn) {
		t.Errorf("step 3 warnings %q, want %q", r.s3Banners("warning"), warn)
	}
	s2WantStep(t, w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "100"}), "/edit/check", "save 100")
	if r := s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page"); !r.s3Has("warning", warn) {
		t.Errorf("check page warnings %q, want %q", r.s3Banners("warning"), warn)
	}
	w.s3Submit(id)
	if r := s3OK(t, w.get(w.Suresh, s3Doc(id)), "document page"); !r.s3Has("warning", warn) {
		t.Errorf("document page warnings %q, want %q", r.s3Banners("warning"), warn)
	}

	own := w.OwnReceiver(t)
	b := w.s3bGRN(own, po, doctype.StateApproved)
	r := s3OK(t, w.get(own, s3Doc(b.ID)), "OwnReceiver")
	if !r.s3Has("warning", "Challan DC-1042 is also on another receipt.") || strings.Contains(r.text(), a.Number) {
		t.Errorf("OwnReceiver: warnings %q, want the another-receipt form only", r.s3Banners("warning"))
	}

	w.Cancel(t, w.Ravi, id, "Counted twice")
	if r := s3OK(t, w.get(w.Suresh, s3Doc(id)), "cancelled"); strings.Contains(r.text(), "Challan DC-1042 is also on") {
		t.Errorf("a cancelled receipt warns: %q", r.s3Banners("warning"))
	}
	named := "Challan DC-1042 is also on " + naming.Label(b.Number, b.ID) + "."
	if r := s3OK(t, w.get(w.Suresh, s3Doc(a.ID)), "first GRN"); !r.s3Has("warning", named) {
		t.Errorf("first GRN warnings %q, want %q", r.s3Banners("warning"), named)
	}

	// The text shows challan_no's value: not where Hide hides it (CD-319).
	id2 := w.draft(po)
	w.s2Delivery(id2)
	s2WantStep(t, w.s2Save(id2, w.s2Items(id2), s2Set{"received", 1, "100"}), "/edit/check", "save 100")
	w.hidden = map[string]bool{"challan_no": true}
	pages := map[string]s1Page{
		"step 3, challan_no hidden":     w.s2Items(id2),
		"check page, challan_no hidden": s3OK(t, w.get(w.Suresh, stepPath(id2, "check")), "check page"),
		"document, challan_no hidden":   s3OK(t, w.get(w.Suresh, s3Doc(a.ID)), "first GRN"),
	}
	for what, r := range pages {
		if strings.Contains(r.text(), "DC-1042") {
			t.Errorf("%s: shows the hidden challan no.: %q", what, r.s3Banners("warning"))
		}
	}
}

// drawn are the page's banners of kind ("info", "warning") outside the
// layout's <template> elements, whose banners are never shown as they are;
// an empty one counts.
func (p s1Page) drawn(kind string) *goquery.Selection {
	return p.doc.Find(".banner--" + kind).FilterFunction(func(_ int, s *goquery.Selection) bool {
		return s.ParentsFiltered("template").Length() == 0
	})
}

// S3a review minor (UX doc §5.6, CD-263) and G11: the e-way Notice and the
// duplicate challan are drawn only on their conditions. A lone GRN with no
// e-way hint and no other GRN draws no warning banner on step 3 or the
// check page, and no information or warning banner on its document page
// (an empty Notice or warning would be a banner with no message).
func TestGRNWeb_NoEmptyBanners(t *testing.T) {
	w := newS1World(t)
	id := w.draft(w.s3bPO())
	w.s2Delivery(id)
	if d := w.s2Items(id).drawn("warning"); d.Length() != 0 {
		t.Errorf("step 3: %d warning banners drawn (%q), want none", d.Length(), s2Norm(d.Text()))
	}
	s2WantStep(t, w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "100"}), "/edit/check", "save 100")
	if d := s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page").drawn("warning"); d.Length() != 0 {
		t.Errorf("check page: %d warning banners drawn (%q), want none", d.Length(), s2Norm(d.Text()))
	}
	w.s3Submit(id)
	r := s3OK(t, w.get(w.Suresh, s3Doc(id)), "document page")
	for _, kind := range []string{"info", "warning"} {
		if d := r.drawn(kind); d.Length() != 0 {
			t.Errorf("document page: %d %s banners drawn (%q), want none", d.Length(), kind, s2Norm(d.Text()))
		}
	}
}
