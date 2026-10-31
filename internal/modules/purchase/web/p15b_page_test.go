package purchaseweb

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// p15bQty is n whole units as a Qty.
func p15bQty(n int64) types.Qty { return types.Qty(n * 1000) }

// PUX-015 (UX §5.9 line notes, CD-258 Q6, CD-330 (2)): an approved PO's line
// shows the Received line with the unit (CD-258 G8, CD-331) and Pending
// {qty} {unit}, after the line's own description when it isn't the item's
// name; after close the note reads Pending 0 {unit} (closed). A cutover line
// reads Already received … before this app · Pending ….
func TestPUX015_PendingNotes(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	po := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"earlier_po_no": "P-17", "earlier_po_date": "01-10-2026"}),
		purchasetest.OrderLines(
			w.cement(t, "40", nil),
			w.cement(t, "40", map[string]string{"description": "Grey, 50 kg bags"}),
			w.cement(t, "40", map[string]string{"already_received": "10"}),
		))
	purchasetest.SetReceived(t, env, po.Lines[0].ID, p15bQty(25))
	purchasetest.SetReceived(t, env, po.Lines[2].ID, p15bQty(25))
	want := []string{
		"Received 25 of 40 BAG", "Pending 15 BAG",
		"Received 0 of 40 BAG", "Grey, 50 kg bags · Pending 40 BAG",
		"Received 25 of 40 BAG", "Already received 10 BAG before this app · Pending 5 BAG",
	}
	if got := w.page(t, env.Priya, po.ID).notes(); !slices.Equal(got, want) {
		t.Errorf("approved line notes = %q, want %q", got, want)
	}
	w.act(t, env.Priya, purchase.DocType, po.ID, doctype.ActionClose, "Vendor stopped supplying")
	want = []string{
		"Received 25 of 40 BAG", "Pending 0 BAG (closed)",
		"Received 0 of 40 BAG", "Grey, 50 kg bags · Pending 0 BAG (closed)",
		"Received 25 of 40 BAG", "Pending 0 BAG (closed)",
	}
	if got := w.page(t, env.Priya, po.ID).notes(); !slices.Equal(got, want) {
		t.Errorf("closed line notes = %q, want %q", got, want)
	}
}

// PUX-015's state guard (UX §5.9 by state): line notes are only on approved
// and closed POs; draft, pending, rejected and cancelled have none.
func TestPUX015_NotesOnlyApprovedAndClosed(t *testing.T) {
	w := newP15bWeb(t, false)
	for _, s := range p15bStates {
		t.Run(string(s), func(t *testing.T) {
			po := w.order(t, s)
			got := w.page(t, w.env.Ravi, po.ID).notes()
			hasPending := slices.ContainsFunc(got, func(n string) bool { return strings.Contains(n, "Pending") })
			if want := s == doctype.StateApproved || s == doctype.StateClosed; hasPending != want {
				t.Errorf("%s notes = %q, want a Pending note %v", s, got, want)
			}
		})
	}
}

// PO-085 and PUX-037 (GRN spec §4, CD-277): a line whose already received
// plus received exceeds its qty shows Over by {qty} {uom} after its pending
// note, with pending 0 and the receipt status fully received; at the qty
// there is no Over by. Closed keeps the Over by note.
func TestPO_OverByLineNote(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	po := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"earlier_po_no": "P-17", "earlier_po_date": "01-10-2026"}),
		purchasetest.OrderLines(
			w.cement(t, "40", map[string]string{"already_received": "45"}),
			w.cement(t, "40", nil),
			w.cement(t, "40", nil),
		))
	purchasetest.SetReceived(t, env, po.Lines[1].ID, p15bQty(45))
	purchasetest.SetReceived(t, env, po.Lines[2].ID, p15bQty(40))
	want := []string{
		"Received 0 of 40 BAG", "Already received 45 BAG before this app · Pending 0 BAG · Over by 5 BAG",
		"Received 45 of 40 BAG", "Pending 0 BAG · Over by 5 BAG",
		"Received 40 of 40 BAG", "Pending 0 BAG",
	}
	if got := w.page(t, env.Priya, po.ID).notes(); !slices.Equal(got, want) {
		t.Errorf("line notes = %q, want %q", got, want)
	}
	w.act(t, env.Priya, purchase.DocType, po.ID, doctype.ActionClose, "Done")
	if got := w.page(t, env.Priya, po.ID).notes(); !slices.Contains(got, "Pending 0 BAG (closed) · Over by 5 BAG") {
		t.Errorf("closed line notes = %q, want Pending 0 BAG (closed) · Over by 5 BAG", got)
	}
}

// PUX-016 (spec §6.4, CD-258 Q12; UX §5.11): the close page warns with the
// lines still pending, per-unit totals joined with "and", in the singular
// for one line, and the close hint; with nothing pending there is no note,
// and the cancel page has neither.
func TestPUX016_CloseWarning(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	const tail = " pending. Closing means no more goods can be received against this order."
	const hint = "Say why nothing more is coming. If you still need the rest, raise a new order after closing."
	one := purchasetest.NewOrder(t, env, purchasetest.OrderLines(w.cement(t, "40", nil), w.cement(t, "10", nil)))
	purchasetest.SetReceived(t, env, one.Lines[0].ID, p15bQty(25))
	purchasetest.SetReceived(t, env, one.Lines[1].ID, p15bQty(10))
	if note, h := w.poCloseNote(t, env.Priya, one.ID); note != "1 line still has 15 BAG"+tail || h != hint {
		t.Errorf("one line: note %q hint %q, want %q and %q", note, h, "1 line still has 15 BAG"+tail, hint)
	}
	many := purchasetest.NewOrder(t, env, purchasetest.OrderLines(
		w.cement(t, "40", nil), w.line(t, "WIRE-18G", "KGS", "7217", "120", nil), w.cement(t, "5", nil)))
	purchasetest.SetReceived(t, env, many.Lines[0].ID, p15bQty(30))
	if note, _ := w.poCloseNote(t, env.Priya, many.ID); note != "3 lines still have 15 BAG and 120 KGS"+tail {
		t.Errorf("three lines: note %q, want %q", note, "3 lines still have 15 BAG and 120 KGS"+tail)
	}
	done := purchasetest.NewOrder(t, env, purchasetest.OrderLines(w.cement(t, "40", nil)))
	purchasetest.SetReceived(t, env, done.Lines[0].ID, p15bQty(40))
	if note, h := w.poCloseNote(t, env.Priya, done.ID); note != "" || h != hint {
		t.Errorf("nothing pending: note %q hint %q, want none and %q", note, h, hint)
	}
	page := w.get(t, env.Ravi, p15dDoc(purchase.DocType, one.ID)+"/cancel")
	if got := page.banners("info"); len(got) != 0 || strings.Contains(page.text(), hint) {
		t.Errorf("cancel page has the close note or hint: %q", page.text())
	}
}

// PUX-016's units: three units, the last joined with "and" (CD-258 Q12).
func TestPUX016_CloseWarningThreeUnits(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	po := purchasetest.NewOrder(t, env, purchasetest.OrderLines(
		w.cement(t, "15", nil), w.line(t, "WIRE-18G", "KGS", "7217", "120", nil), w.line(t, "BOLT-M12", "NOS", "7318", "4", nil)))
	want := "3 lines still have 15 BAG, 120 KGS and 4 NOS pending. Closing means no more goods can be received against this order."
	if note, _ := w.poCloseNote(t, env.Priya, po.ID); note != want {
		t.Errorf("note %q, want %q", note, want)
	}
}

// poCloseNote is the info banner above the close reason of PO id as p, "" with
// none, and the field's hint.
func (w *p15dWeb) poCloseNote(t *testing.T, p auth.Principal, id int64) (note, hint string) {
	t.Helper()
	page := w.get(t, p, p15dDoc(purchase.DocType, id)+"/close")
	if page.status != 200 {
		t.Fatalf("%s GET close of %d = %d: %s", p.Name, id, page.status, page.text())
	}
	if got := page.banners("info"); len(got) > 0 {
		note = got[0]
	}
	return note, p15dCollapse(page.doc.Find(".field__hint").First().Text())
}

// PUX-017 and PO-063's page part (spec §23, G5; UX §5.9): an approved unsent
// PO's bar primary is Send to vendor for the owner and for a user with write
// or approve, and its Sent card reads Not sent yet with no link; a reader
// has neither. After Mark as sent the card reads the sentence and the note,
// with Change for those who may mark it, and nobody has the primary.
func TestPUX017_SendToVendor(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	po := w.order(t, doctype.StateApproved)
	send := orderPage(po.ID, "/send")
	writer := w.user(t, "writer", []auth.Right{auth.RightWrite})
	approver := w.user(t, "approver", []auth.Right{auth.RightApprove})
	reader := w.user(t, "reader", nil)
	for _, c := range []struct {
		p    auth.Principal
		send bool
	}{{env.Priya, true}, {env.Ravi, true}, {writer, true}, {approver, true}, {env.Arjun, false}, {reader, false}} {
		page := w.page(t, c.p, po.ID)
		label, href := page.primary()
		if got := label == "Send to vendor" && href == send; got != c.send {
			t.Errorf("%s: primary %q %q, want Send to vendor %v", c.p.Name, label, href, c.send)
		}
		if got := p15dCardParas(page, "Sent to vendor"); !slices.Equal(got, []string{"Not sent yet"}) {
			t.Errorf("%s: Sent card %q, want Not sent yet", c.p.Name, got)
		}
		if l, _ := page.cardLink("Sent to vendor"); l != "" {
			t.Errorf("%s: unsent card has link %q", c.p.Name, l)
		}
	}
	w.markSent(t, env.Priya, po.ID, "24-10-2026", "whatsapp", "Sent the PDF to Ramesh at Shree.")
	for _, c := range []struct {
		p      auth.Principal
		change bool
	}{{env.Priya, true}, {writer, true}, {approver, true}, {env.Arjun, false}, {reader, false}} {
		page := w.page(t, c.p, po.ID)
		if got, want := p15dCardParas(page, "Sent to vendor"), []string{"Sent to vendor on 24-10-2026 by WhatsApp (Priya Shah)", "Sent the PDF to Ramesh at Shree."}; !slices.Equal(got, want) {
			t.Errorf("%s: Sent card %q, want %q", c.p.Name, got, want)
		}
		l, href := page.cardLink("Sent to vendor")
		if got := l == "Change" && href == send; got != c.change {
			t.Errorf("%s: Sent card link %q %q, want Change %v", c.p.Name, l, href, c.change)
		}
		if label, _ := page.primary(); label != "" {
			t.Errorf("%s: sent PO has primary %q", c.p.Name, label)
		}
	}
	// Closed keeps the record, with no link.
	w.act(t, env.Priya, purchase.DocType, po.ID, doctype.ActionClose, "Done")
	page := w.page(t, env.Priya, po.ID)
	if got := p15dCardParas(page, "Sent to vendor"); len(got) == 0 || got[0] != "Sent to vendor on 24-10-2026 by WhatsApp (Priya Shah)" {
		t.Errorf("closed Sent card %q, want the record", got)
	}
	if l, _ := page.cardLink("Sent to vendor"); l != "" {
		t.Errorf("closed Sent card has link %q", l)
	}
}

// The Sent card's how words in the sentence (UX §5.9): email, WhatsApp,
// courier, hand, other means.
func TestPUX017_SentWords(t *testing.T) {
	w := newP15bWeb(t, false)
	for via, word := range map[string]string{"email": "email", "whatsapp": "WhatsApp", "courier": "courier", "by_hand": "hand", "other": "other means"} {
		po := w.order(t, doctype.StateApproved)
		w.markSent(t, w.env.Priya, po.ID, "24-10-2026", via, "")
		want := []string{"Sent to vendor on 24-10-2026 by " + word + " (Priya Shah)"}
		if got := p15dCardParas(w.page(t, w.env.Priya, po.ID), "Sent to vendor"); !slices.Equal(got, want) {
			t.Errorf("%s: Sent card %q, want %q", via, got, want)
		}
	}
}

// PUX-017's state guards (spec §23, UX §5.9 by state): Send to vendor is the
// primary only on an approved PO; the Sent card shows on approved and closed,
// and on cancelled only with a record.
func TestPUX017_SendStates(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	for _, s := range p15bStates {
		t.Run(string(s), func(t *testing.T) {
			po := w.order(t, s)
			page := w.page(t, env.Ravi, po.ID)
			if label, _ := page.primary(); (label == "Send to vendor") != (s == doctype.StateApproved) {
				t.Errorf("%s: primary %q", s, label)
			}
			if want := s == doctype.StateApproved || s == doctype.StateClosed; page.hasCard("Sent to vendor") != want {
				t.Errorf("%s: Sent card shown %v, want %v (cards %q)", s, !want, want, page.cards())
			}
		})
	}
	po := w.order(t, doctype.StateApproved)
	w.markSent(t, env.Priya, po.ID, "24-10-2026", "email", "")
	w.act(t, env.Ravi, purchase.DocType, po.ID, doctype.ActionCancel, "Vendor withdrew")
	page := w.page(t, env.Ravi, po.ID)
	if got := p15dCardParas(page, "Sent to vendor"); !slices.Equal(got, []string{"Sent to vendor on 24-10-2026 by email (Priya Shah)"}) {
		t.Errorf("cancelled Sent card %q, want the record", got)
	}
	if l, _ := page.cardLink("Sent to vendor"); l != "" {
		t.Errorf("cancelled Sent card has link %q", l)
	}
}

// TestPO_ExpectedCard (UX §5.9, §5.20; spec §27.1, P18): an approved PO's
// Expected delivery card reads No date from the vendor yet. with Set date for
// users with amend or approve, and after Expected on is set, the date, the
// note and Set by {name} on {date}, with Change. Readers get no link.
func TestPO_ExpectedCard(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	po := w.order(t, doctype.StateApproved)
	expected := orderPage(po.ID, "/expected")
	amender := w.user(t, "amender", []auth.Right{auth.RightAmend})
	approver := w.user(t, "eapprover", []auth.Right{auth.RightApprove})
	reader := w.user(t, "ereader", []auth.Right{auth.RightWrite, auth.RightCreate})
	cases := []struct {
		p    auth.Principal
		link bool
	}{{env.Priya, true}, {env.Ravi, true}, {amender, true}, {approver, true}, {env.Suresh, false}, {reader, false}}
	for _, c := range cases {
		page := w.page(t, c.p, po.ID)
		if got := p15dCardParas(page, "Expected delivery"); !slices.Equal(got, []string{"No date from the vendor yet."}) {
			t.Errorf("%s: Expected card %q, want No date from the vendor yet.", c.p.Name, got)
		}
		l, href := page.cardLink("Expected delivery")
		if got := l == "Set date" && href == expected; got != c.link {
			t.Errorf("%s: Expected link %q %q, want Set date %v", c.p.Name, l, href, c.link)
		}
	}
	w.setExpected(t, env.Priya, po.ID, "05-11-2026", "Vendor moved it; truck on Monday.")
	for _, c := range cases {
		page := w.page(t, c.p, po.ID)
		want := []string{"Expected on 05-11-2026", "Vendor moved it; truck on Monday.", "Set by Priya Shah on 24-10-2026"}
		if got := p15dCardParas(page, "Expected delivery"); !slices.Equal(got, want) {
			t.Errorf("%s: Expected card %q, want %q", c.p.Name, got, want)
		}
		l, href := page.cardLink("Expected delivery")
		if got := l == "Change" && href == expected; got != c.link {
			t.Errorf("%s: Expected link %q %q, want Change %v", c.p.Name, l, href, c.link)
		}
	}
}

// TestPO_ExpectedCard's IST date: a record set at 18:31 UTC on 30-10 was set
// on 31-10-2026 in IST (lessons: a clock just after IST midnight). With no
// note the card has no note line.
func TestPO_ExpectedCardISTMidnight(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	po := w.order(t, doctype.StateApproved)
	env.SetNow(t, p15bAt(18, 31).AddDate(0, 0, -1))
	w.setExpected(t, env.Priya, po.ID, "05-11-2026", "")
	want := []string{"Expected on 05-11-2026", "Set by Priya Shah on 31-10-2026"}
	if got := p15dCardParas(w.page(t, env.Priya, po.ID), "Expected delivery"); !slices.Equal(got, want) {
		t.Errorf("Expected card %q, want %q", got, want)
	}
}

// TestPO_ExpectedCard's states (UX §5.9): no card on draft, pending or
// rejected, nor on closed or cancelled without a record; closed and
// cancelled with a record show it with no link.
func TestPO_ExpectedCardStates(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	for _, s := range p15bStates {
		t.Run(string(s), func(t *testing.T) {
			po := w.order(t, s)
			if got, want := w.page(t, env.Ravi, po.ID).hasCard("Expected delivery"), s == doctype.StateApproved; got != want {
				t.Errorf("%s without a record: card %v, want %v", s, got, want)
			}
		})
	}
	for _, s := range []doctype.State{doctype.StateClosed, doctype.StateCancelled} {
		po := w.order(t, doctype.StateApproved)
		w.setExpected(t, env.Priya, po.ID, "05-11-2026", "")
		if s == doctype.StateClosed {
			w.act(t, env.Priya, purchase.DocType, po.ID, doctype.ActionClose, "Done")
		} else {
			w.act(t, env.Ravi, purchase.DocType, po.ID, doctype.ActionCancel, "Not needed")
		}
		page := w.page(t, env.Ravi, po.ID)
		if got := p15dCardParas(page, "Expected delivery"); len(got) == 0 || got[0] != "Expected on 05-11-2026" {
			t.Errorf("%s with a record: card %q, want the record", s, got)
		}
		if l, _ := page.cardLink("Expected delivery"); l != "" {
			t.Errorf("%s: Expected card has link %q", s, l)
		}
	}
}

// PO-060, PUX-005 and PUX-006's page half (spec §21.1, G3, G4; UX §5.9):
// Priya's draft shows other readers the owner notice; only a user with
// approve gets its Hand over link and Hand over under More actions. The
// Comments card with its box is there.
func TestPO_OwnerBanner(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	approver := w.user(t, "happrover", []auth.Right{auth.RightApprove})
	const notice = "Owned by Priya Shah. Only Priya Shah can change this draft. Add a comment below, or ask Priya Shah to hand it over."
	for _, s := range []doctype.State{doctype.StateDraft, doctype.StateRejected} {
		po := w.order(t, s)
		handover := orderPage(po.ID, "/handover")
		for _, c := range []struct {
			p  auth.Principal
			ho bool
		}{{env.Arjun, false}, {env.Suresh, false}, {env.Ravi, true}, {approver, true}} {
			page := w.page(t, c.p, po.ID)
			if got := page.banners("info"); !slices.Contains(got, notice) {
				t.Errorf("%s %s: info banners %q, want the owner notice", s, c.p.Name, got)
			}
			link := page.doc.Find(`main .banner--info a[href="` + handover + `"]`).Length()
			if (link == 1) != c.ho {
				t.Errorf("%s %s: notice Hand over link %d, want %v", s, c.p.Name, link, c.ho)
			}
			if got := slices.Contains(page.more(), "Hand over"); got != c.ho {
				t.Errorf("%s %s: More %q, Hand over want %v", s, c.p.Name, page.more(), c.ho)
			}
			if page.doc.Find(`#comments textarea`).Length() != 1 {
				t.Errorf("%s %s: no comment box", s, c.p.Name)
			}
		}
	}
}

// PUX-006's rejected half (UX §5.9 by state): on her rejected PO the owner
// has no notice, the Owner card with Hand over, and Hand over under More.
func TestPO_OwnerCardRejected(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	po := w.order(t, doctype.StateRejected)
	page := w.page(t, env.Priya, po.ID)
	if got := page.banners("info"); slices.ContainsFunc(got, func(b string) bool { return strings.HasPrefix(b, "Owned by") }) {
		t.Errorf("owner sees the owner notice: %q", got)
	}
	if got := p15dCardParas(page, "Owner"); !slices.Equal(got, []string{"You own this order. Only you can change or submit it."}) {
		t.Errorf("Owner card %q", got)
	}
	if page.doc.Find(`section.card a[href="`+orderPage(po.ID, "/handover")+`"]`).Length() != 1 {
		t.Errorf("Owner card has no Hand over to the hand-over page")
	}
	if !slices.Contains(page.more(), "Hand over") {
		t.Errorf("owner's More %q lacks Hand over", page.more())
	}
	if p15dCard(w.page(t, env.Ravi, po.ID), "Owner") != "" {
		t.Errorf("Ravi sees the Owner card")
	}
}

// PO-060's state guard: the owner notice, the Owner card and Hand over are
// only on draft and rejected POs.
func TestPO_OwnerBannerStates(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	for _, s := range p15bStates {
		t.Run(string(s), func(t *testing.T) {
			po := w.order(t, s)
			want := s == doctype.StateDraft || s == doctype.StateRejected
			ravi := w.page(t, env.Ravi, po.ID)
			notice := slices.ContainsFunc(ravi.banners("info"), func(b string) bool { return strings.HasPrefix(b, "Owned by") })
			if notice != want || slices.Contains(ravi.more(), "Hand over") != want {
				t.Errorf("%s: Ravi notice %v, More %q; want both %v", s, notice, ravi.more(), want)
			}
			if s == doctype.StateDraft {
				return // the owner gets the check page
			}
			priya := w.page(t, env.Priya, po.ID)
			if got := p15dCard(priya, "Owner") != ""; got != (s == doctype.StateRejected) {
				t.Errorf("%s: Owner card %v", s, got)
			}
			if slices.Contains(priya.more(), "Hand over") != (s == doctype.StateRejected) {
				t.Errorf("%s: Priya's More %q", s, priya.more())
			}
		})
	}
}

// PUX-028's page part (spec §24.1, G4): Copy is under More actions, a POST to
// the copy route, in every state for users with create; Ravi and a user
// without create have none.
func TestPUX028_CopyMore(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	creator := w.user(t, "creator", []auth.Right{auth.RightCreate})
	reader := w.user(t, "creader", []auth.Right{auth.RightApprove})
	for _, s := range p15bStates {
		t.Run(string(s), func(t *testing.T) {
			po := w.order(t, s)
			copyPath := orderPage(po.ID, "/copy")
			for _, c := range []struct {
				p    auth.Principal
				copy bool
			}{{env.Arjun, true}, {creator, true}, {env.Ravi, false}, {reader, false}} {
				page := w.page(t, c.p, po.ID)
				form := page.doc.Find(`.more-actions-items form[action="` + copyPath + `"]`).Length()
				if got := slices.Contains(page.more(), "Copy"); got != c.copy || (form == 1) != c.copy {
					t.Errorf("%s %s: More %q (copy forms %d), want Copy %v", s, c.p.Name, page.more(), form, c.copy)
				}
			}
		})
	}
}

// PUX-038 (CD-283; UX §5.9): with goods due on an approved, sent PO, Receive
// goods is the bar's primary for Suresh (GRN create), linking to GRN's new
// page with the PO; a user without GRN create has none. Fully received, it
// is under More actions with no primary; closed, it is gone.
func TestPUX038_ReceiveGoods(t *testing.T) {
	w := newP15bWeb(t, true)
	env := w.env
	po := w.order(t, doctype.StateApproved, purchasetest.OrderLines(w.cement(t, "40", nil)))
	w.markSent(t, env.Priya, po.ID, "24-10-2026", "email", "")
	purchasetest.SetReceived(t, env, po.Lines[0].ID, p15bQty(25))
	href := p15bReceiveHref(po.ID)
	receiver := w.user(t, "receiver", nil, auth.RightCreate)
	for _, p := range []auth.Principal{env.Suresh, receiver} {
		page := w.page(t, p, po.ID)
		if label, h := page.primary(); label != "Receive goods" || h != href {
			t.Errorf("%s: primary %q %q, want Receive goods %s", p.Name, label, h, href)
		}
		if slices.Contains(page.more(), "Receive goods") {
			t.Errorf("%s: Receive goods also under More", p.Name)
		}
	}
	for _, p := range []auth.Principal{env.Arjun, env.Priya} {
		page := w.page(t, p, po.ID)
		if label, _ := page.primary(); label != "" || slices.Contains(page.more(), "Receive goods") {
			t.Errorf("%s (no GRN create): primary %q, More %q", p.Name, label, page.more())
		}
	}
	purchasetest.SetReceived(t, env, po.Lines[0].ID, p15bQty(40))
	page := w.page(t, env.Suresh, po.ID)
	if label, _ := page.primary(); label != "" {
		t.Errorf("fully received: primary %q, want none", label)
	}
	if got := page.more(); len(got) == 0 || got[0] != "Receive goods" || page.doc.Find(`.more-actions-items a[href="`+href+`"]`).Length() != 1 {
		t.Errorf("fully received: More %q, want Receive goods first, linking %s", got, href)
	}
	w.act(t, env.Priya, purchase.DocType, po.ID, doctype.ActionClose, "Done")
	page = w.page(t, env.Suresh, po.ID)
	if label, _ := page.primary(); label != "" || slices.Contains(page.more(), "Receive goods") {
		t.Errorf("closed: primary %q, More %q; want no Receive goods", label, page.more())
	}
}

// PUX-038's unsent half: a user who may send and receive has Send to vendor
// as the primary and Receive goods first under More; Priya, who may not
// receive, has only Send to vendor.
func TestPUX038_ReceiveWithSend(t *testing.T) {
	w := newP15bWeb(t, true)
	env := w.env
	po := w.order(t, doctype.StateApproved)
	both := w.user(t, "both", []auth.Right{auth.RightWrite, auth.RightCreate}, auth.RightCreate)
	page := w.page(t, both, po.ID)
	if label, _ := page.primary(); label != "Send to vendor" {
		t.Errorf("primary %q, want Send to vendor", label)
	}
	if got := page.more(); len(got) < 2 || got[0] != "Receive goods" || got[1] != "Copy" {
		t.Errorf("More %q, want Receive goods then Copy first", got)
	}
	page = w.page(t, env.Priya, po.ID)
	if label, _ := page.primary(); label != "Send to vendor" || slices.Contains(page.more(), "Receive goods") {
		t.Errorf("Priya: primary %q, More %q", label, page.more())
	}
}

// PUX-038's guards: Receive goods only on an approved PO, and nowhere with a
// nil ReceiveLink.
func TestPUX038_ReceiveStates(t *testing.T) {
	w := newP15bWeb(t, true)
	for _, s := range p15bStates {
		t.Run(string(s), func(t *testing.T) {
			po := w.order(t, s)
			page := w.page(t, w.env.Suresh, po.ID)
			label, _ := page.primary()
			got := label == "Receive goods" || slices.Contains(page.more(), "Receive goods")
			if got != (s == doctype.StateApproved) {
				t.Errorf("%s: Receive goods %v (primary %q, More %q)", s, got, label, page.more())
			}
		})
	}
	n := newP15bWeb(t, false)
	po := n.order(t, doctype.StateApproved)
	page := n.page(t, n.env.Suresh, po.ID)
	if label, _ := page.primary(); label != "" || strings.Contains(page.text(), "Receive goods") {
		t.Errorf("nil hook: primary %q, page %q", label, page.text())
	}
}

// Goods receipts card (UX §5.9; spec §11, CD-248, CD-280 D6, CD-312): each
// GRN the viewer may read is a row with its number, date (Hooks.Date) and
// state, linking to it; the others are one trailing count with no state; a
// PO with none reads No goods receipts yet. Suresh is restricted to PUNE-WH
// and METRO-SITE on the real auth.Service, so TECH-SITE receipts are hidden.
func TestPO_GoodsReceiptsCard(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	po := w.order(t, doctype.StateApproved)
	if got := p15dCardParas(w.page(t, env.Suresh, po.ID), "Goods receipts"); !slices.Equal(got, []string{"No goods receipts yet."}) {
		t.Errorf("no GRN: card %q, want No goods receipts yet.", got)
	}
	seen := w.grn(t, po.ID, "METRO-SITE", "29-10-2026")
	hidden := w.grn(t, po.ID, "TECH-SITE", "30-10-2026")
	page := w.page(t, env.Suresh, po.ID)
	card := p15dCard(page, "Goods receipts")
	for _, want := range []string{seen.Number, "29-10-2026", "Approved", "1 other goods receipt"} {
		if !strings.Contains(card, want) {
			t.Errorf("Suresh's card %q lacks %q", card, want)
		}
	}
	if strings.Contains(card, "other goods receipts") || strings.Contains(page.text(), hidden.Number) || strings.Contains(card, "30-10-2026") {
		t.Errorf("Suresh's card %q names or counts the hidden GRN wrongly", card)
	}
	if page.doc.Find(`a[href="`+p15dDoc(p15bGRN, seen.ID)+`"]`).Length() != 1 {
		t.Errorf("no link to %s", seen.Number)
	}
	if got := p15dCardParas(page, "Goods receipts"); !slices.Equal(got, []string{"1 other goods receipt"}) {
		t.Errorf("Suresh's card paragraphs %q, want only 1 other goods receipt", got)
	}
	w.grn(t, po.ID, "TECH-SITE", "30-10-2026")
	if got := p15dCardParas(w.page(t, env.Suresh, po.ID), "Goods receipts"); !slices.Equal(got, []string{"2 other goods receipts"}) {
		t.Errorf("two hidden: %q, want 2 other goods receipts", got)
	}
	// Priya reads them all.
	card = p15dCard(w.page(t, env.Priya, po.ID), "Goods receipts")
	if !strings.Contains(card, seen.Number) || !strings.Contains(card, hidden.Number) || strings.Contains(card, "other goods") {
		t.Errorf("Priya's card %q, want every GRN named", card)
	}
}

// Goods receipts card states (UX §5.9 by state): approved and closed only.
func TestPO_GoodsReceiptsCardStates(t *testing.T) {
	w := newP15bWeb(t, false)
	for _, s := range p15bStates {
		t.Run(string(s), func(t *testing.T) {
			po := w.order(t, s)
			if got, want := w.page(t, w.env.Ravi, po.ID).hasCard("Goods receipts"), s == doctype.StateApproved || s == doctype.StateClosed; got != want {
				t.Errorf("%s: card %v, want %v", s, got, want)
			}
		})
	}
}

// PO-005's document page part and UX §5.9 pairs: Raised by names the owner;
// Place of supply is the §7.1 text; Reverse charge is the print's note only
// when ticked.
func TestPO_PlaceReasonShown(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	intra := w.order(t, doctype.StateApproved)
	page := w.page(t, env.Ravi, intra.ID)
	if v, _ := page.pair("Raised by"); v != "Priya Shah" {
		t.Errorf("Raised by = %q", v)
	}
	if v, _ := page.pair("Place of supply"); v != "Intra-state: the supplier and the delivery site are both in Maharashtra." {
		t.Errorf("intra Place of supply = %q", v)
	}
	if _, ok := page.pair("Reverse charge"); ok {
		t.Errorf("Reverse charge pair without reverse charge")
	}
	inter := w.order(t, doctype.StateDraft, purchasetest.OrderHeader(map[string]string{"vendor_id": w.id(t, "party", "BHARAT"), "reverse_charge": "true"}))
	page = w.page(t, env.Ravi, inter.ID)
	if v, _ := page.pair("Place of supply"); v != "Inter-state: the supplier is in Karnataka and the delivery site is in Maharashtra." {
		t.Errorf("inter Place of supply = %q", v)
	}
	if v, _ := page.pair("Reverse charge"); !strings.HasPrefix(v, "Reverse charge applies. GST of ₹") {
		t.Errorf("Reverse charge = %q", v)
	}
}

// UX §5.9's Copied from pair (CD-248 family, CD-312): the source's display,
// shown only when the viewer may read the source. Priya, restricted to
// PRJ-METRO on the real auth.Service, can't read Arjun's PRJ-TECHPARK
// source; Arjun can.
func TestPO_CopiedFromPair(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	src := w.techparkOrder(t)
	cp := w.order(t, doctype.StateApproved, purchasetest.OrderHeader(map[string]string{
		"copied_from_id": strconv.FormatInt(src.ID, 10), "copied_from_no": src.Number,
	}))
	// Until P15a's Hide drops the copied_from_no field, Details also has its
	// generic "Copied from" row; the pair is one row more for a reader of the
	// source.
	arjun, priya := w.page(t, env.Arjun, cp.ID).copiedFrom(), w.page(t, env.Priya, cp.ID).copiedFrom()
	if len(arjun) != len(priya)+1 || !slices.Contains(arjun, src.Number) {
		t.Errorf("Copied from rows: Arjun %q, Priya %q; want Arjun's to add the pair %s", arjun, priya, src.Number)
	}
	plain := w.order(t, doctype.StateApproved)
	if got := w.page(t, env.Arjun, plain.ID).copiedFrom(); len(got) != 0 {
		t.Errorf("a PO that isn't a copy has Copied from rows %q", got)
	}
}

// copiedFrom are the values of the Details rows labelled Copied from.
func (p p15dPage) copiedFrom() []string {
	var out []string
	p.doc.Find(".summary-row dt").Each(func(_ int, s *goquery.Selection) {
		if p15dCollapse(s.Text()) == "Copied from" {
			out = append(out, p15dCollapse(s.Next().Text()))
		}
	})
	return out
}

// techparkOrder is Arjun's approved PO on TECH-SITE / PRJ-TECHPARK, which
// Priya (restricted to PRJ-METRO) can't read.
func (w *p15dWeb) techparkOrder(t *testing.T) doc.Doc {
	t.Helper()
	ctx, env := t.Context(), w.env
	res, err := env.Engine.Do(ctx, env.Arjun, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"vendor_id": w.id(t, "party", "SHREE"), "site_id": w.id(t, "site", "TECH-SITE"),
			"project_id": w.id(t, "project", "PRJ-TECHPARK"), "order_date": types.Today(env.Clock).ISO(),
		},
		Lines: []map[string]string{w.line(t, "MS-ANGLE-50", "KGS", "7216", "10", nil)},
	})
	if err != nil {
		t.Fatalf("save TECH-SITE PO: %v", err)
	}
	w.act(t, env.Arjun, purchase.DocType, res.ID, doctype.ActionSubmit, "")
	var out doc.Doc
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		out, err = doc.LoadDoc(ctx, tx, purchase.DocType, res.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// PO-005's blank case: a draft saved without a vendor has no snapshot, so
// there is no Place of supply pair yet (the print shows none either).
func TestPO_PlaceReasonBlankDraft(t *testing.T) {
	w := newP15bWeb(t, false)
	po := w.order(t, doctype.StateDraft, purchasetest.OrderHeader(map[string]string{"vendor_id": ""}))
	page := w.page(t, w.env.Ravi, po.ID)
	if v, ok := page.pair("Place of supply"); ok {
		t.Errorf("Place of supply = %q on a draft with no vendor", v)
	}
	if v, _ := page.pair("Raised by"); v != "Priya Shah" {
		t.Errorf("Raised by = %q", v)
	}
}
