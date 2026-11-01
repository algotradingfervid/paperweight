package grnweb_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// MOD-GRN S3a: the check page, the document page and the cancel reason
// (UX doc §5.5, §5.6, §5.8), driven over HTTP on s1World (real engine,
// real auth.Service). The clock is authtest.T0, 24-10-2026.

// The S3 texts (GRN spec §4, §5, §7; UX doc §5.5, §5.6, §5.8).
const (
	s3NoteAll    = "All lines received as ordered."
	s3NoteUnreg  = "Unregistered vendor: the e-way bill may be ours to generate. Check with your CA."
	s3NoteReject = "Reject the e-way bill on the portal within 72 hours if the goods went back."
	s3Hint       = "Say what was wrong. Stores can then amend the receipt to correct it."
	s3Invoice    = "INV-889"
)

// s3Doc is the document page path of GRN id.
func s3Doc(id int64) string {
	return "/doc/" + goodsreceipt.DocType + "/" + strconv.FormatInt(id, 10)
}

// s3Banners are the texts of the page's banners of kind ("info",
// "warning"), each with runs of white space collapsed and without the
// kind's word.
func (p s1Page) s3Banners(kind string) []string {
	var out []string
	p.doc.Find(".banner--" + kind).Each(func(_ int, s *goquery.Selection) {
		text := s2Norm(s.Text())
		for _, word := range []string{"Warning:", "Information:"} {
			text = strings.TrimSpace(strings.TrimPrefix(text, word))
		}
		if text != "" { // the page's empty banner templates
			out = append(out, text)
		}
	})
	return out
}

// s3Has reports whether any banner of kind contains text.
func (p s1Page) s3Has(kind, text string) bool {
	for _, b := range p.s3Banners(kind) {
		if strings.Contains(b, text) {
			return true
		}
	}
	return false
}

// s3POCard is the page's Purchase order card, or an empty selection.
func (p s1Page) s3POCard() *goquery.Selection {
	return p.doc.Find("section.card").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return s2Norm(s.Find("h2.card-title").First().Text()) == "Purchase order"
	})
}

// s3OK fails unless r answered 200.
func s3OK(t *testing.T, r s1Page, what string) s1Page {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("%s = %d, want 200 (%s)", what, r.status, r.text())
	}
	return r
}

// s3Submit submits draft id as Suresh and fails unless it is approved.
func (w *s1World) s3Submit(id int64) {
	w.t.Helper()
	r := w.post(w.Suresh, s3Doc(id)+"/submit", url.Values{"version": {strconv.Itoa(w.s2Version(id))}})
	if r.status != http.StatusSeeOther {
		w.t.Fatalf("submit = %d %q, want 303", r.status, r.errorSummary())
	}
	if g := w.Load(w.t, goodsreceipt.DocType, id); g.State != doctype.StateApproved {
		w.t.Fatalf("GRN %d is %s after submit, want approved", id, g.State)
	}
}

// s3Role creates a user whose only role, name, holds rights on the GRN at
// scope all and nothing on purchase.Order, through the real AUTH.
func (w *s1World) s3Role(name string, rights ...auth.Right) auth.Principal {
	w.t.Helper()
	ctx := context.Background()
	if err := w.Auth.CreateRole(ctx, w.Admin, name); err != nil {
		w.t.Fatalf("create role %s: %v", name, err)
	}
	m := map[auth.Right]auth.Scope{}
	for _, r := range rights {
		m[r] = auth.ScopeAll
	}
	if err := w.Auth.SetRights(ctx, w.Admin, name, goodsreceipt.DocType, m); err != nil {
		w.t.Fatalf("rights of %s: %v", name, err)
	}
	id, _, err := w.Auth.CreateUser(ctx, w.Admin, auth.NewUser{ //nolint:gosec // G101: a test user's password
		Email: strings.ToLower(strings.ReplaceAll(name, " ", ".")) + "@vardhan.test", Name: name,
		Password: "Receipts need care 42", Roles: []string{name},
	})
	if err != nil {
		w.t.Fatalf("create user %s: %v", name, err)
	}
	p, _, err := w.Auth.LoadPrincipal(ctx, id)
	if err != nil {
		w.t.Fatalf("load user %s: %v", name, err)
	}
	return p
}

// GRN-013 (GRN spec §4, §11; CD-234, CD-238): with 1,200 KGS pending on
// line 1, receiving 1,250 warns on the Received quantities step, the check
// page and, after submit, the document page, and submit goes on. Receiving
// 1,250 with 50 rejected warns on none of the three. A cancelled receipt
// no longer counts, so its page doesn't warn (UX doc §5.6).
func TestGRN_OverDeliveryWarns(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	warn := "Line 1: Accepted 1,250 KGS is more than the 1,200 KGS pending on " + po.Number + "."

	id := w.draft(po)
	w.s2Delivery(id)
	s2WantStep(t, w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "1250"}), "/edit/check", "save 1,250")
	if !strings.Contains(w.s2Items(id).text(), warn) {
		t.Errorf("step 3 doesn't warn %q", warn)
	}
	if r := s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page"); !r.s3Has("warning", warn) {
		t.Errorf("check page warnings %q, want %q", r.s3Banners("warning"), warn)
	}
	w.s3Submit(id)
	if r := s3OK(t, w.get(w.Suresh, s3Doc(id)), "document page"); !r.s3Has("warning", warn) {
		t.Errorf("document page warnings %q, want %q", r.s3Banners("warning"), warn)
	}
	w.Cancel(t, w.Ravi, id, "Counted twice")
	if r := s3OK(t, w.get(w.Suresh, s3Doc(id)), "cancelled document page"); strings.Contains(r.text(), "is more than the") {
		t.Errorf("a cancelled receipt warns: %q", r.s3Banners("warning"))
	}

	po2 := w.s2RefPO()
	id2 := w.draft(po2)
	w.s2Delivery(id2)
	s2WantStep(t, w.s2Save(id2, w.s2Items(id2), s2Set{"received", 1, "1250"}, s2Set{"rejected", 1, "50"},
		s2Set{"reason", 1, "Bent pieces"}), "/edit/check", "save 1,250 with 50 rejected")
	pages := map[string]func() s1Page{
		"step 3":     func() s1Page { return w.s2Items(id2) },
		"check page": func() s1Page { return s3OK(t, w.get(w.Suresh, stepPath(id2, "check")), "check page") },
	}
	for what, get := range pages {
		if r := get(); strings.Contains(r.text(), "is more than the") {
			t.Errorf("%s warns with the excess rejected: %q", what, r.s3Banners("warning"))
		}
	}
	w.s3Submit(id2)
	if r := s3OK(t, w.get(w.Suresh, s3Doc(id2)), "document page"); strings.Contains(r.text(), "is more than the") {
		t.Errorf("document page warns with the excess rejected: %q", r.s3Banners("warning"))
	}
}

// CD-277 (1), CD-294: an approved GRN shows no Close action to Stores, the
// Purchase Manager or the Administrator, and GET and POST /close answer
// 404 with nothing changed.
func TestGRN_PageNoClose(t *testing.T) {
	w := newS1World(t)
	g := w.receiveAll(w.s2RefPO())
	for name, p := range map[string]auth.Principal{"Suresh": w.Suresh, "Ravi": w.Ravi, "Admin": w.Admin} {
		r := s3OK(t, w.get(p, s3Doc(g.ID)), name+": document page")
		r.doc.Find("a, button").Each(func(_ int, s *goquery.Selection) {
			if strings.HasSuffix(s.AttrOr("href", ""), "/close") || strings.HasSuffix(s.AttrOr("formaction", ""), "/close") ||
				strings.HasPrefix(s2Norm(s.Text()), "Close") {
				t.Errorf("%s: a Close action %q (%s)", name, s2Norm(s.Text()), s.AttrOr("href", ""))
			}
		})
		if r := w.get(p, s3Doc(g.ID)+"/close"); r.status != http.StatusNotFound {
			t.Errorf("%s: GET /close = %d, want 404", name, r.status)
		}
		if r := w.post(p, s3Doc(g.ID)+"/close", url.Values{"version": {strconv.Itoa(w.s2Version(g.ID))}, "reason": {"No more goods"}}); r.status != http.StatusNotFound {
			t.Errorf("%s: POST /close = %d, want 404", name, r.status)
		}
	}
	if got := w.Load(t, goodsreceipt.DocType, g.ID).State; got != doctype.StateApproved {
		t.Errorf("state %s after the close attempts, want approved", got)
	}
}

// GRN-044, GRN-048 on the pages (CD-272 Gaps 4 and 13; UX doc §5.5,
// §5.6): the check page's info notes are "All lines received as ordered."
// when every pending line is received at its pending, and the e-way hints;
// the document page shows only an e-way hint, as its last banner, never on
// a cancelled receipt.
func TestGRNWeb_S3Notes(t *testing.T) {
	w := newS1World(t)

	ref := w.s2RefPO()
	asOrdered := w.GRN(t, w.Suresh, ref.ID, []grntest.Line{{POLineID: ref.Lines[0].ID, Received: "1200"}, {POLineID: ref.Lines[1].ID, Received: "50"}}, doctype.StateDraft)
	if got := s3OK(t, w.get(w.Suresh, stepPath(asOrdered.ID, "check")), "check").s3Banners("info"); !strings.Contains(strings.Join(got, "|"), s3NoteAll) {
		t.Errorf("as-ordered check page info %q, want %q", got, s3NoteAll)
	}
	w.s3Submit(asOrdered.ID)
	if r := s3OK(t, w.get(w.Suresh, s3Doc(asOrdered.ID)), "document"); strings.Contains(r.text(), s3NoteAll) || len(r.s3Banners("info")) != 0 {
		t.Errorf("as-ordered document page info %q, want none (the note is the check page's)", r.s3Banners("info"))
	}
	short := w.GRN(t, w.Suresh, ref.ID, []grntest.Line{{POLineID: ref.Lines[0].ID, Received: "1000"}}, doctype.StateDraft)
	if got := s3OK(t, w.get(w.Suresh, stepPath(short.ID, "check")), "check").text(); strings.Contains(got, s3NoteAll) {
		t.Errorf("a short receipt's check page says %q", s3NoteAll)
	}

	local := w.po("LOCAL", "1000") // ₹62,500.00 at ₹62.50 from an unregistered vendor
	unreg := w.GRN(t, w.Suresh, local.ID, []grntest.Line{{POLineID: local.Lines[0].ID, Received: "1000"}}, doctype.StateDraft)
	if got := s3OK(t, w.get(w.Suresh, stepPath(unreg.ID, "check")), "check").s3Banners("info"); !strings.Contains(strings.Join(got, "|"), s3NoteAll+"|"+s3NoteUnreg) {
		t.Errorf("unregistered check page info %q, want %q then %q", got, s3NoteAll, s3NoteUnreg)
	}
	w.s3Submit(unreg.ID)
	r := s3OK(t, w.get(w.Suresh, s3Doc(unreg.ID)), "document")
	if !r.s3Has("info", s3NoteUnreg) || strings.Contains(r.text(), s3NoteAll) {
		t.Errorf("document page info %q, want %q and not %q", r.s3Banners("info"), s3NoteUnreg, s3NoteAll)
	}
	w.Cancel(t, w.Ravi, unreg.ID, "Wrong vendor")
	if r := s3OK(t, w.get(w.Suresh, s3Doc(unreg.ID)), "cancelled"); strings.Contains(r.text(), s3NoteUnreg) {
		t.Errorf("a cancelled receipt shows %q", s3NoteUnreg)
	}

	rej := w.s2RefPO()
	rejected := w.GRN(t, w.Suresh, rej.ID, []grntest.Line{{POLineID: rej.Lines[0].ID, Received: "10", Rejected: "10", Reason: "Wet"}}, doctype.StateDraft)
	if r := w.post(w.Suresh, stepPath(rejected.ID, "delivery"), url.Values{"version": {strconv.Itoa(rejected.Version)},
		"challan_kind": {"Delivery challan"}, "challan_no": {"DC-1042"}, "eway_bill_no": {"331000123456"}}); r.status != http.StatusSeeOther {
		t.Fatalf("save the e-way bill no. = %d %q", r.status, r.errorSummary())
	}
	if got := s3OK(t, w.get(w.Suresh, stepPath(rejected.ID, "check")), "check").s3Banners("info"); !strings.Contains(strings.Join(got, "|"), s3NoteReject) {
		t.Errorf("fully rejected check page info %q, want %q", got, s3NoteReject)
	}
}

// UX doc §5.6 (spec §3.1, §11; CD-312): the document page adds Site and
// Project from the GRN's snapshot (the fixture site's name is its code), and the Purchase order card: an Open
// link to the PO, the number and order date, the receipt status words and
// the lines still pending.
func TestGRNWeb_S3PageCard(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	g := w.s2Approve(po, map[int]string{1: "800", 2: "50"})
	r := s3OK(t, w.get(w.Suresh, s3Doc(g.ID)), "document")
	rows := strings.Join(r.summaryRows(), "|")
	for _, want := range []string{"Site: METRO-SITE", "Project: PRJ-METRO"} {
		if !strings.Contains(rows, want) {
			t.Errorf("summary rows %q, want %q", rows, want)
		}
	}
	card := r.s3POCard()
	if card.Length() != 1 {
		t.Fatalf("%d Purchase order cards, want 1", card.Length())
	}
	link := card.Find(".card-head a")
	if href := link.AttrOr("href", ""); href != "/doc/"+purchase.DocType+"/"+strconv.FormatInt(po.ID, 10) || s2Norm(link.Text()) != "Open Purchase order" {
		t.Errorf("card link %q to %q, want Open Purchase order to the PO", s2Norm(link.Text()), href)
	}
	text := s2Norm(card.Text())
	for _, want := range []string{po.Number + " · 24-10-2026", "Receipt status", "Part received", "Still pending", "1 of 2 lines"} {
		if !strings.Contains(text, want) {
			t.Errorf("card %q, want %q", text, want)
		}
	}

	g2 := w.s2Approve(po, map[int]string{1: "400"})
	text = s2Norm(s3OK(t, w.get(w.Suresh, s3Doc(g2.ID)), "document").s3POCard().Text())
	for _, want := range []string{"Fully received", "Nothing"} {
		if !strings.Contains(text, want) {
			t.Errorf("fully received card %q, want %q", text, want)
		}
	}
}

// CD-233, CD-240, CD-248, CD-312, CD-340 on the real auth.Service: a user
// with read and write on GRNs but no read on the PO sees, on the check
// page and the document page, no PO card, link, state, number or
// PO-derived warning or note (over-delivery, received as ordered); the
// GRN's own e-way hint still shows. Suresh (who reads the PO) sees them on
// the same GRN.
func TestGRNWeb_S3HiddenPO(t *testing.T) {
	w := newS1World(t)
	clerk := w.BillClerk(t)
	po := w.po("LOCAL", "1000")
	w.GRN(t, w.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "100"}}, doctype.StateApproved)
	g := w.GRN(t, w.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "1000"}}, doctype.StateDraft)
	warn := "Line 1: Accepted 1,000 KGS is more than the 900 KGS pending on " + po.Number + "."

	wantHidden := func(r s1Page, what string) {
		t.Helper()
		if strings.Contains(r.text(), po.Number) || strings.Contains(r.text(), "is more than the") {
			t.Errorf("%s: names the hidden PO or its pending: %q", what, r.s3Banners("warning"))
		}
		if r.s3POCard().Length() != 0 || r.doc.Find(`a[href*="/doc/`+purchase.DocType+`/"]`).Length() != 0 {
			t.Errorf("%s: a Purchase order card or link", what)
		}
		for _, s := range []string{"Part received", "Receipt status", "Still pending"} {
			if strings.Contains(r.text(), s) {
				t.Errorf("%s: shows the PO's %q", what, s)
			}
		}
		if !r.s3Has("info", s3NoteUnreg) {
			t.Errorf("%s: info %q, want the GRN's own %q", what, r.s3Banners("info"), s3NoteUnreg)
		}
	}
	if r := s3OK(t, w.get(w.Suresh, stepPath(g.ID, "check")), "Suresh check"); !r.s3Has("warning", warn) {
		t.Errorf("Suresh's check page warnings %q, want %q", r.s3Banners("warning"), warn)
	}
	wantHidden(s3OK(t, w.get(clerk, stepPath(g.ID, "check")), "clerk check"), "clerk check page")

	w.s3Submit(g.ID)
	r := s3OK(t, w.get(w.Suresh, s3Doc(g.ID)), "Suresh document")
	if !r.s3Has("warning", warn) || r.s3POCard().Length() != 1 {
		t.Errorf("Suresh's document page: warnings %q, %d PO cards; want %q and the card", r.s3Banners("warning"), r.s3POCard().Length(), warn)
	}
	wantHidden(s3OK(t, w.get(clerk, s3Doc(g.ID)), "clerk document"), "clerk document page")

	// Received as ordered: the note says so from the PO's pending.
	po2 := w.po("LOCAL", "1000")
	g2 := w.GRN(t, w.Suresh, po2.ID, []grntest.Line{{POLineID: po2.Lines[0].ID, Received: "1000"}}, doctype.StateDraft)
	if r := s3OK(t, w.get(w.Suresh, stepPath(g2.ID, "check")), "Suresh check"); !r.s3Has("info", s3NoteAll) {
		t.Errorf("Suresh's check page info %q, want %q", r.s3Banners("info"), s3NoteAll)
	}
	r = s3OK(t, w.get(clerk, stepPath(g2.ID, "check")), "clerk check")
	if strings.Contains(r.text(), s3NoteAll) || !r.s3Has("info", s3NoteUnreg) {
		t.Errorf("clerk's check page info %q, want %q without %q", r.s3Banners("info"), s3NoteUnreg, s3NoteAll)
	}
}

// GRN spec §5, CD-272 Gap 2, GRN-041 (UX doc §5.8): the cancel page warns,
// as one warning banner, "{PO number} is closed. …" when the PO is closed,
// then "This receipt has bill details ({invoice no.}). …" when bill
// details are stored; with neither, no note; the hint is always there. A
// canceller who can't read the PO gets no closed-PO sentence: it would
// name the PO and its state (CD-233, CD-248, CD-340). The reject page
// gets neither note nor hint.
func TestGRNWeb_S3CancelReason(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	g := w.s2Approve(po, map[int]string{1: "800"})
	closed := po.Number + " is closed. After this cancel, only an amendment of this receipt can receive against it again."
	bill := "This receipt has bill details (" + s3Invoice + "). Correct your accounts too."
	page := func(p auth.Principal, what string) s1Page {
		t.Helper()
		r := s3OK(t, w.get(p, s3Doc(g.ID)+"/cancel"), what)
		if !strings.Contains(r.text(), s3Hint) {
			t.Errorf("%s: no hint %q", what, s3Hint)
		}
		return r
	}

	if r := page(w.Ravi, "neither"); len(r.s3Banners("warning")) != 0 || len(r.s3Banners("info")) != 0 {
		t.Errorf("neither: banners %q %q, want none", r.s3Banners("warning"), r.s3Banners("info"))
	}

	if err := w.Service.SaveBill(t.Context(), w.Suresh, g.ID, 0, goodsreceipt.BillInput{InvoiceNo: s3Invoice, InvoiceDate: "24-10-2026"}); err != nil {
		t.Fatalf("save bill details: %v", err)
	}
	if got := page(w.Ravi, "bill").s3Banners("warning"); len(got) != 1 || got[0] != bill {
		t.Errorf("bill: warnings %q, want [%q]", got, bill)
	}

	w.ClosePO(t, w.Admin, po.ID, "Vendor short-closed")
	if got := page(w.Ravi, "both").s3Banners("warning"); len(got) != 1 || got[0] != closed+" "+bill {
		t.Errorf("both: warnings %q, want [%q]", got, closed+" "+bill)
	}
	canceller := w.s3Role("Receipt Canceller", auth.RightRead, auth.RightCancel)
	r := page(canceller, "hidden PO")
	if got := r.s3Banners("warning"); len(got) != 1 || got[0] != bill || strings.Contains(r.text(), po.Number) {
		t.Errorf("hidden PO: warnings %q, want only [%q] and no %s", got, bill, po.Number)
	}

	po2 := w.s2RefPO()
	g2 := w.s2Approve(po2, map[int]string{1: "800"})
	w.ClosePO(t, w.Admin, po2.ID, "Vendor short-closed")
	want := po2.Number + " is closed. After this cancel, only an amendment of this receipt can receive against it again."
	if got := s3OK(t, w.get(w.Ravi, s3Doc(g2.ID)+"/cancel"), "closed only").s3Banners("warning"); len(got) != 1 || got[0] != want {
		t.Errorf("closed only: warnings %q, want [%q]", got, want)
	}

	// Only the cancel page: a reject page (a GRN rule) has no hint.
	w.AddRule(t, goodsreceipt.DocType, 0, "Administrator")
	po3 := w.s2RefPO()
	g3 := w.GRN(t, w.Suresh, po3.ID, []grntest.Line{{POLineID: po3.Lines[0].ID, Received: "800"}}, doctype.StatePending)
	if r := s3OK(t, w.get(w.Admin, s3Doc(g3.ID)+"/reject"), "reject page"); strings.Contains(r.text(), s3Hint) {
		t.Errorf("the reject page has the cancel hint")
	}
}
