package purchaseweb

import (
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// p15dHeld is PR-A with row 1's 25 Bag ordered on an approved PO and row 2
// held by Priya's draft, as PUX-022 gives it.
type p15dHeld struct {
	pr, ordered, draft doc.Doc
}

func (w *p15dWeb) heldPRA(t *testing.T) p15dHeld {
	t.Helper()
	pr := w.prA(t)
	return p15dHeld{
		pr:      pr,
		ordered: w.hold(t, purchasetest.StopAtApproved(), pr.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "25"),
		draft:   w.hold(t, purchasetest.StopAtDraft(), pr.Lines[1].ID, "WIRE-18G", "KGS", "7217", "25"),
	}
}

// PUX-022 (PO-052, CD-233, CD-248, CD-280 D6, CD-312; UX §5.16): Priya on
// PR-A reads each row's Requested, Ordered and Pending to order, row 2 ends
// with the draft holding it and its owner, Linked orders lists both POs and
// the bar's primary is Create PO. Deepak, who can't read purchase orders,
// sees his own request's held row as On another order and Linked orders as
// 1 other order, with neither PO named anywhere on the page.
func TestPUX022_RequestPage(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	h := w.heldPRA(t)
	draft := p15dDisplay(h.draft)

	page := w.get(t, env.Priya, p15dDoc(purchase.RequestDocType, h.pr.ID))
	if page.status != 200 {
		t.Fatalf("Priya GET PR-A = %d: %s", page.status, page.text())
	}
	wantNotes := []string{
		"UltraTech or ACC only · Requested 40 BAG · Ordered 25 BAG · Pending to order 15 BAG",
		"Requested 25 KGS · Ordered 0 KGS · Pending to order 0 KGS · On " + draft + " (Priya Shah)",
	}
	if got := page.notes(); !slices.Equal(got, wantNotes) {
		t.Errorf("line notes = %q, want %q", got, wantNotes)
	}
	if got := page.find(".lines-card .num"); !slices.Equal(got, []string{"40 BAG", "25 KGS"}) {
		t.Errorf("line quantities = %q, want 40 BAG and 25 KGS", got)
	}
	card := p15dCard(page, "Linked orders")
	for _, want := range []string{h.ordered.Number, "Shree Steel Traders", "Approved", "Not received", draft, "Draft"} {
		if !strings.Contains(card, want) {
			t.Errorf("Linked orders card %q lacks %q", card, want)
		}
	}
	if strings.Contains(card, "other order") {
		t.Errorf("Linked orders card %q counts unreadable orders; Priya reads both", card)
	}
	if row := p15dCollapse(page.doc.Find(`a[href="` + p15dDoc(purchase.DocType, h.draft.ID) + `"]`).Text()); strings.Contains(row, "received") {
		t.Errorf("the draft's Linked orders row %q has a receipt status; only approved and closed orders do", row)
	}
	for _, href := range []string{p15dDoc(purchase.DocType, h.ordered.ID), p15dDoc(purchase.DocType, h.draft.ID)} {
		if page.doc.Find(`a[href="`+href+`"]`).Length() != 1 {
			t.Errorf("Linked orders has no link to %s", href)
		}
	}
	if got := page.find(".actionbar a"); !slices.Equal(got, []string{"Create PO"}) {
		t.Errorf("Priya's bar = %q, want the primary Create PO", got)
	}
	if page.doc.Find(`.actionbar a[href="`+p15dOrderPath(h.pr.ID)+`"]`).Length() != 1 {
		t.Errorf("Create PO doesn't link to %s", p15dOrderPath(h.pr.ID))
	}
	if !strings.Contains(page.text(), "Raised by Suresh Patil") {
		t.Errorf("Details lacks Raised by Suresh Patil: %s", page.text())
	}

	// A closed order keeps its receipt status in Linked orders.
	closed := w.hold(t, purchasetest.StopAtApproved(), h.pr.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "5")
	w.act(t, env.Priya, purchase.DocType, closed.ID, doctype.ActionClose, "Balance not needed")
	page = w.get(t, env.Priya, p15dDoc(purchase.RequestDocType, h.pr.ID))
	if row := p15dCollapse(page.doc.Find(`a[href="` + p15dDoc(purchase.DocType, closed.ID) + `"]`).Text()); !strings.Contains(row, closed.Number) || !strings.Contains(row, "Closed") || !strings.Contains(row, "Not received") {
		t.Errorf("the closed order's Linked orders row = %q, want %s, Closed and its receipt status Not received", row, closed.Number)
	}

	// Deepak's own request, held by Priya's draft he can't read.
	deepak := p15dDeepakRequest(t, w)
	hidden := w.hold(t, purchasetest.StopAtDraft(), deepak.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "10")
	page = w.get(t, env.Deepak, p15dDoc(purchase.RequestDocType, deepak.ID))
	if page.status != 200 {
		t.Fatalf("Deepak GET his request = %d: %s", page.status, page.text())
	}
	if got, want := page.notes(), []string{"Requested 40 BAG · Ordered 0 BAG · Pending to order 30 BAG · On another order"}; !slices.Equal(got, want) {
		t.Errorf("Deepak's line notes = %q, want %q", got, want)
	}
	if card := p15dCardParas(page, "Linked orders"); !slices.Equal(card, []string{"1 other order"}) {
		t.Errorf("Deepak's Linked orders card = %q, want only 1 other order", card)
	}
	if text := page.text(); strings.Contains(text, p15dDisplay(hidden)) || strings.Contains(text, "Priya Shah") || strings.Contains(text, "Shree Steel") {
		t.Errorf("Deepak's page names the PO he can't read: %s", text)
	}
	if n := page.doc.Find(".actionbar a").Length(); n != 0 {
		t.Errorf("Deepak (no create on Purchase Order) has %d bar links, want no Create PO", n)
	}
}

// p15dDeepakRequest is Deepak's submitted METRO-SITE / PRJ-METRO request of
// 40 Bag of CEMENT-OPC.
func p15dDeepakRequest(t *testing.T, w *p15dWeb) doc.Doc {
	t.Helper()
	env := w.env
	res, err := env.Engine.Do(t.Context(), env.Deepak, purchase.RequestDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"site_id": w.id(t, "site", "METRO-SITE"), "project_id": w.id(t, "project", "PRJ-METRO"), "request_date": "2026-10-24"},
		Lines:  []map[string]string{w.row(t, "CEMENT-OPC", "BAG", "40", "")},
	})
	if err != nil {
		t.Fatalf("save Deepak's request: %v", err)
	}
	if _, err := env.Engine.Do(t.Context(), env.Deepak, purchase.RequestDocType, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil {
		t.Fatalf("submit Deepak's request: %v", err)
	}
	var d doc.Doc
	d.ID = res.ID
	lines := w.lineIDs(t, res.ID)
	for _, id := range lines {
		d.Lines = append(d.Lines, doc.Line{ID: id})
	}
	return d
}

// lineIDs are request id's line ids in line order.
func (w *p15dWeb) lineIDs(t *testing.T, id int64) []int64 {
	t.Helper()
	rows, err := w.env.DB.SQL().QueryContext(t.Context(), `SELECT id FROM purchase_request_line WHERE parent_id = ? ORDER BY line_no`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var n int64
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

// p15dCard is the collapsed text of the card titled title, "" when absent.
func p15dCard(p p15dPage, title string) string {
	var out string
	p.doc.Find("section.card").Each(func(_ int, s *goquery.Selection) {
		if out == "" && p15dCollapse(s.Find("h2.card-title").First().Text()) == title {
			out = p15dCollapse(s.Text())
		}
	})
	return out
}

// p15dCardParas are the paragraphs directly in the card titled title.
func p15dCardParas(p p15dPage, title string) []string {
	var out []string
	p.doc.Find("section.card").Each(func(_ int, s *goquery.Selection) {
		if p15dCollapse(s.Find("h2.card-title").First().Text()) == title {
			s.Find(".card-body > p").Each(func(_ int, q *goquery.Selection) { out = append(out, p15dCollapse(q.Text())) })
		}
	})
	return out
}

// PUX-022's restricted reader (CD-312, lessons §2): Suresh, restricted to
// PUNE-WH and METRO-SITE on the real auth.Service, reads Priya's METRO-SITE
// draft, so his own request names it, with its owner, on the row and in
// Linked orders; the masters' LinkRead would have refused it. He has no
// create on Purchase Order, so no Create PO.
func TestPUX022_RestrictedReaderSeesHolder(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	h := w.heldPRA(t)
	draft := p15dDisplay(h.draft)
	page := w.get(t, env.Suresh, p15dDoc(purchase.RequestDocType, h.pr.ID))
	if page.status != 200 {
		t.Fatalf("Suresh GET PR-A = %d: %s", page.status, page.text())
	}
	if got := page.notes(); len(got) != 2 || !strings.HasSuffix(got[1], " · On "+draft+" (Priya Shah)") {
		t.Errorf("Suresh's line notes = %q, want row 2 to end On %s (Priya Shah)", got, draft)
	}
	card := p15dCard(page, "Linked orders")
	if !strings.Contains(card, h.ordered.Number) || !strings.Contains(card, draft) || strings.Contains(card, "other order") {
		t.Errorf("Suresh's Linked orders card = %q, want both orders named", card)
	}
	if n := page.doc.Find(".actionbar a").Length(); n != 0 {
		t.Errorf("Suresh (no create on Purchase Order) has %d bar links, want no Create PO", n)
	}
}

// PUX-022's Linked orders card with nothing linked, and an unreadable order
// counted in the plural (CD-280 D6): a request on no order reads Not on any
// order yet.; two orders Deepak can't read read 2 other orders, and a
// cancelled one isn't listed.
func TestPUX022_LinkedOrdersEmptyAndPlural(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	deepak := p15dDeepakRequest(t, w)
	page := w.get(t, env.Deepak, p15dDoc(purchase.RequestDocType, deepak.ID))
	if card := p15dCardParas(page, "Linked orders"); !slices.Equal(card, []string{"Not on any order yet."}) {
		t.Errorf("Linked orders with no order = %q, want Not on any order yet.", card)
	}
	w.hold(t, purchasetest.StopAtDraft(), deepak.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "10")
	w.hold(t, purchasetest.StopAtApproved(), deepak.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "5")
	gone := w.hold(t, purchasetest.StopAtApproved(), deepak.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "1")
	w.act(t, env.Ravi, purchase.DocType, gone.ID, doctype.ActionCancel, "Vendor can't supply")
	page = w.get(t, env.Deepak, p15dDoc(purchase.RequestDocType, deepak.ID))
	if card := p15dCardParas(page, "Linked orders"); !slices.Equal(card, []string{"2 other orders"}) {
		t.Errorf("Linked orders with two unreadable orders = %q, want 2 other orders", card)
	}
	if got, want := page.notes(), []string{"Requested 40 BAG · Ordered 5 BAG · Pending to order 25 BAG · On another order"}; !slices.Equal(got, want) {
		t.Errorf("line notes = %q, want %q", got, want)
	}
}

// PUX-022's primary (G5, UX §5.16; lessons §5 and §11): Create PO shows for a
// user holding only create on Purchase Order (and read on the request), on a
// submitted request while a row has pending to order above 0. A user with
// only read on Purchase Order never gets it; nor does the creator on a
// draft, a fully held request, a closed or a cancelled one.
func TestPUX022_CreatePOPrimary(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	creator := w.probe(t, "creator", auth.RightCreate)
	reader := w.probe(t, "reader")
	primary := func(p auth.Principal, id int64) []string {
		t.Helper()
		page := w.get(t, p, p15dDoc(purchase.RequestDocType, id))
		if page.status != 200 {
			t.Fatalf("%s GET request %d = %d: %s", p.Name, id, page.status, page.text())
		}
		return page.find(`.actionbar a[href="` + p15dOrderPath(id) + `"]`)
	}

	open := w.prA(t)
	if got := primary(creator, open.ID); !slices.Equal(got, []string{"Create PO"}) {
		t.Errorf("creator on a submitted request with pending rows: primary %q, want Create PO", got)
	}
	if got := primary(reader, open.ID); len(got) != 0 {
		t.Errorf("reader (no create): primary %q, want none", got)
	}

	draft := w.prA(t, purchasetest.StopAtDraft())
	if got := primary(creator, draft.ID); len(got) != 0 {
		t.Errorf("creator on a draft: primary %q, want none", got)
	}

	full := w.prA(t)
	w.hold(t, purchasetest.StopAtDraft(), full.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "40")
	if got := primary(creator, full.ID); !slices.Equal(got, []string{"Create PO"}) {
		t.Errorf("creator with row 2 still pending: primary %q, want Create PO", got)
	}
	w.hold(t, purchasetest.StopAtApproved(), full.Lines[1].ID, "WIRE-18G", "KGS", "7217", "25")
	if got := primary(creator, full.ID); len(got) != 0 {
		t.Errorf("creator with nothing pending: primary %q, want none", got)
	}

	closed := w.prA(t)
	w.act(t, env.Suresh, purchase.RequestDocType, closed.ID, doctype.ActionClose, "No longer needed")
	if got := primary(creator, closed.ID); len(got) != 0 {
		t.Errorf("creator on a closed request: primary %q, want none", got)
	}

	cancelled := w.prA(t)
	w.act(t, env.Suresh, purchase.RequestDocType, cancelled.ID, doctype.ActionCancel, "Raised twice")
	if got := primary(creator, cancelled.ID); len(got) != 0 {
		t.Errorf("creator on a cancelled request: primary %q, want none", got)
	}
}

// PUX-022's state guard on every state (G5): only a submitted (approved)
// request may show Create PO; a request is never pending or rejected, so
// those states are checked on the guard itself.
func TestPUX022_CreatePOStateGuard(t *testing.T) {
	for _, c := range []struct {
		state doctype.State
		want  bool
	}{
		{doctype.StateDraft, false},
		{doctype.StatePending, false},
		{doctype.StateRejected, false},
		{doctype.StateApproved, true},
		{doctype.StateClosed, false},
		{doctype.StateCancelled, false},
	} {
		if got := createPOShown(c.state, true, true); got != c.want {
			t.Errorf("createPOShown(%s, create, pending) = %v, want %v", c.state, got, c.want)
		}
	}
	if createPOShown(doctype.StateApproved, false, true) || createPOShown(doctype.StateApproved, true, false) {
		t.Error("createPOShown without create or without pending = true, want false")
	}
}

// PUX-024 (PO-050, CD-253; UX §5.16): the owner notice names the requester
// and who may close. Priya (no close) reads Only Suresh Patil or a Purchase
// Manager can close; Ravi and Meena (approve, all) read You or Suresh Patil;
// Suresh sees no notice; a draft read by someone else says he is still
// writing it; closed and cancelled requests have none. Priya's More actions
// has neither Close nor Cancel.
func TestPUX024_OwnerNotices(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	const (
		cant     = "Raised by Suresh Patil. Only Suresh Patil or a Purchase Manager can close this request."
		can      = "Raised by Suresh Patil. You or Suresh Patil can close this request."
		drafting = "Suresh Patil is still writing this request. Only Suresh Patil can change or submit it. Add a comment below."
	)
	notices := func(p auth.Principal, id int64) []string {
		t.Helper()
		page := w.get(t, p, p15dDoc(purchase.RequestDocType, id))
		if page.status != 200 {
			t.Fatalf("%s GET request %d = %d", p.Name, id, page.status)
		}
		return page.banners("info")
	}
	pr := w.prA(t)
	for _, c := range []struct {
		p    auth.Principal
		want []string
	}{
		{env.Priya, []string{cant}},
		{env.Ravi, []string{can}},
		{env.Meena, []string{can}},
		{env.Suresh, nil},
	} {
		if got := notices(c.p, pr.ID); !slices.Equal(got, c.want) {
			t.Errorf("%s on submitted PR-A: notices %q, want %q", c.p.Name, got, c.want)
		}
	}
	more := w.get(t, env.Priya, p15dDoc(purchase.RequestDocType, pr.ID)).find(".more-actions li")
	if slices.Contains(more, "Close") || slices.Contains(more, "Cancel") {
		t.Errorf("Priya's More actions = %q, want neither Close nor Cancel", more)
	}

	draft := w.prA(t, purchasetest.StopAtDraft())
	if got := notices(env.Ravi, draft.ID); !slices.Equal(got, []string{drafting}) {
		t.Errorf("Ravi on Suresh's draft: notices %q, want %q", got, drafting)
	}

	w.act(t, env.Suresh, purchase.RequestDocType, pr.ID, doctype.ActionClose, "No longer needed")
	cancelled := w.prA(t)
	w.act(t, env.Suresh, purchase.RequestDocType, cancelled.ID, doctype.ActionCancel, "Raised twice")
	for _, id := range []int64{pr.ID, cancelled.ID} {
		for _, p := range []auth.Principal{env.Priya, env.Ravi} {
			if got := notices(p, id); len(got) != 0 {
				t.Errorf("%s on closed or cancelled request %d: notices %q, want none", p.Name, id, got)
			}
		}
	}
}

// PUX-033 (PO-050, CD-253, CD-255; real auth.Service): on Suresh's submitted
// PR-A, Ravi (Purchase Manager) has Close and no Cancel and GET close is 200;
// Priya has no Close and GET close is AUTH's 403; Suresh has both; Meena
// (Director) has Close and no Cancel.
func TestPUX033_RequestCloseAndCancel(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	pr := w.prA(t)
	path := p15dDoc(purchase.RequestDocType, pr.ID)
	for _, c := range []struct {
		p             auth.Principal
		close, cancel bool
		closeStatus   int
	}{
		{env.Ravi, true, false, 200},
		{env.Priya, false, false, 403},
		{env.Suresh, true, true, 200},
		{env.Meena, true, false, 200},
	} {
		more := w.get(t, c.p, path).find(".more-actions li")
		if slices.Contains(more, "Close") != c.close || slices.Contains(more, "Cancel") != c.cancel {
			t.Errorf("%s More actions = %q, want Close %v, Cancel %v", c.p.Name, more, c.close, c.cancel)
		}
		if got := w.get(t, c.p, path+"/close").status; got != c.closeStatus {
			t.Errorf("%s GET close = %d, want %d", c.p.Name, got, c.closeStatus)
		}
	}
}
