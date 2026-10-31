package purchaseweb

import (
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// p15aQtyRates are the line cards' quantity and rate lines.
func p15aQtyRates(p p15dPage) []string {
	var out []string
	for _, s := range p.find(".lines-card .num") {
		if strings.Contains(s, " × ") {
			out = append(out, s)
		}
	}
	return out
}

// p15aTechSource is Arjun's approved SHREE PO on TECH-SITE / PRJ-TECHPARK of
// 40 Bag CEMENT-OPC at ₹345.00, ordered 12-10-2026, which Priya (PRJ-METRO
// only) can't read. Under ₹50,000, its submit approves it.
func p15aTechSource(t *testing.T, w *p15dWeb) (id int64, number string) {
	t.Helper()
	env := w.env
	h := p15aHeader(t, w, "SHREE", "TECH-SITE", "PRJ-TECHPARK")
	h["order_date"] = "2026-10-12"
	id = p15aSave(t, env, env.Arjun, h, p15aCement(t, w, "40", "345"))
	w.act(t, env.Arjun, purchase.DocType, id, doctype.ActionSubmit, "")
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT number FROM purchase_order WHERE id = ? AND state = 'approved'`, id).Scan(&number); err != nil {
		t.Fatalf("Arjun's PRJ-TECHPARK PO isn't approved: %v", err)
	}
	return id, number
}

// PUX-011 (PO-065, CD-258 G8 and Q6 (a), spec §24.2; UX §5.6): after an
// approved SHREE PO on PRJ-METRO buys CEMENT-OPC at ₹345.00 on 12-10-2026,
// a CEMENT-OPC line saved on Priya's new SHREE draft with a blank rate shows
// `40 BAG × ₹345.00` (the unit, G8), its tax and `Last bought at ₹345.00
// from Shree Steel Traders on 12-10-2026 ({PO})` on the Items step and the
// check page. When a newer price is on a PO Priya can't read, she still reads
// the newest one she can, while Ravi, who reads both, sees the newer one.
func TestPUX011_LastPriceCard(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	src := p15aSource(t, w)
	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p15aCement(t, w, "40", "")))
	path := p15dDoc(purchase.DocType, draft.ID)
	for _, step := range []string{"/edit/items", "/edit/check"} {
		page := w.get(t, env.Priya, path+step)
		if page.status != 200 {
			t.Fatalf("Priya GET %s = %d: %s", step, page.status, page.text())
		}
		if got := p15aQtyRates(page); !slices.Equal(got, []string{"40 BAG × ₹345.00"}) {
			t.Errorf("%s card quantity = %q, want 40 BAG × ₹345.00", step, got)
		}
		if got := page.find(".lines-card .num + span"); !slices.Equal(got, []string{"GST 18%"}) {
			t.Errorf("%s card tax = %q, want GST 18%%", step, got)
		}
		if got := page.notes(); !slices.Equal(got, []string{p15aNote(src)}) {
			t.Errorf("%s card notes = %q, want [%q]", step, got, p15aNote(src))
		}
	}

	// A newer price on a PO Priya can't read: no note for her; Ravi sees it
	// on the draft's read-only page (G1).
	_, tech := p15aTechSource(t, w)
	newer := strings.Replace(p15aNote(src), src.Number, tech, 1)
	if got := w.get(t, env.Priya, path+"/edit/items").notes(); !slices.Equal(got, []string{p15aNote(src)}) {
		t.Errorf("Priya's notes with a newer unreadable PO = %q, want the newest she can read only", got)
	}
	ravi := w.get(t, env.Ravi, path)
	if ravi.status != 200 {
		t.Fatalf("Ravi GET the draft = %d", ravi.status)
	}
	if got := ravi.notes(); !slices.Equal(got, []string{newer}) {
		t.Errorf("Ravi's notes = %q, want [%q]", got, newer)
	}
}

// PO-065 (spec §24.2, AUTH-110): a PO the viewer can't read is never named.
// With the only price on Arjun's PRJ-TECHPARK PO, Priya's card has no note;
// a line with no approved or closed price anywhere has none either.
func TestPUX011_NoNoteWhenUnreadable(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	p15aTechSource(t, w)
	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(),
		purchasetest.OrderLines(p15aCement(t, w, "40", "350"), map[string]string{}))
	page := w.get(t, env.Priya, p15dDoc(purchase.DocType, draft.ID)+"/edit/items")
	if page.status != 200 {
		t.Fatalf("Priya GET items = %d: %s", page.status, page.text())
	}
	if got := page.notes(); len(got) != 0 {
		t.Errorf("Priya's card notes = %q, want none", got)
	}
	if got := p15aQtyRates(page); !slices.Equal(got, []string{"40 BAG × ₹350.00", "100 KGS × ₹62.50"}) {
		t.Errorf("card quantities = %q, want each with its unit", got)
	}
}

// Plan P15a (the last-price note on draft, pending and rejected): a PO in
// each state with a CEMENT-OPC line, after an older approved one at
// ₹345.00, read by Ravi on its document page. Draft, pending and rejected
// show the note; approved, closed and cancelled show none (their notes are
// P15b's pending figures). Approved and closed cards read Received with the
// unit (G8).
func TestPUX011_LastPriceNoteStates(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	src := p15aSource(t, w)
	big := purchasetest.OrderLines(p15aCement(t, w, "200", "")) // ₹69,000: needs approval
	small := purchasetest.OrderLines(p15aCement(t, w, "40", ""))
	for _, c := range []struct {
		state doctype.State
		make  func() int64
		note  bool
	}{
		{doctype.StateDraft, func() int64 { return purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), small).ID }, true},
		{doctype.StatePending, func() int64 { return purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), big).ID }, true},
		{doctype.StateRejected, func() int64 {
			id := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), big).ID
			w.act(t, env.Ravi, purchase.DocType, id, doctype.ActionReject, "Rate too high.")
			return id
		}, true},
		{doctype.StateApproved, func() int64 { return purchasetest.NewOrder(t, env, small).ID }, false},
		{doctype.StateClosed, func() int64 {
			id := purchasetest.NewOrder(t, env, small).ID
			w.act(t, env.Admin, purchase.DocType, id, doctype.ActionClose, "Not needed.")
			return id
		}, false},
		{doctype.StateCancelled, func() int64 {
			id := purchasetest.NewOrder(t, env, small).ID
			w.act(t, env.Admin, purchase.DocType, id, doctype.ActionCancel, "Raised twice.")
			return id
		}, false},
	} {
		t.Run(string(c.state), func(t *testing.T) {
			id := c.make()
			var state string
			if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT state FROM purchase_order WHERE id = ?`, id).Scan(&state); err != nil || state != string(c.state) {
				t.Fatalf("PO %d is %q (%v), want %s", id, state, err, c.state)
			}
			page := w.get(t, env.Ravi, p15dDoc(purchase.DocType, id))
			if page.status != 200 {
				t.Fatalf("Ravi GET = %d: %s", page.status, page.text())
			}
			var want []string
			if c.note {
				want = []string{p15aNote(src)}
			}
			if c.state == doctype.StateApproved || c.state == doctype.StateClosed {
				want = []string{"Received 0 of 40 BAG"}
			}
			if got := page.notes(); !slices.Equal(got, want) {
				t.Errorf("%s card notes = %q, want %q", c.state, got, want)
			}
		})
	}
}

// Plan P15a (LastPricesFor on error): when the last prices can't be read the
// cards show without a note, the page still answers 200 and the error is
// logged. LastPricesFor runs once per request for all the lines.
func TestPUX011_LastPriceError(t *testing.T) {
	env := purchasetest.New(t)
	bad := &p15aScope{Auth: env.Auth, err: errP15a}
	w := newP15aWeb(t, env, p15aService(env, bad))
	p15aSource(t, w)
	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(),
		purchasetest.OrderLines(p15aCement(t, w, "40", "345"), p15aCement(t, w, "10", "345")))
	logs := p15aLogs(t)
	bad.calls.Store(0)
	page := w.get(t, env.Priya, p15dDoc(purchase.DocType, draft.ID)+"/edit/items")
	if page.status != 200 {
		t.Fatalf("GET items with LastPricesFor failing = %d, want 200: %s", page.status, page.text())
	}
	if got := page.notes(); len(got) != 0 {
		t.Errorf("notes with LastPricesFor failing = %q, want none", got)
	}
	if got := p15aQtyRates(page); !slices.Equal(got, []string{"40 BAG × ₹345.00", "10 BAG × ₹345.00"}) {
		t.Errorf("cards with LastPricesFor failing = %q, want both cards", got)
	}
	if !strings.Contains(logs(), errP15a.Error()) {
		t.Errorf("the LastPricesFor error isn't logged: %q", logs())
	}
	if n := bad.calls.Load(); n != 1 {
		t.Errorf("one Items page asked LastPricesFor %d times, want once for both lines", n)
	}

	good := &p15aScope{Auth: env.Auth}
	w = newP15aWeb(t, env, p15aService(env, good))
	if got := w.get(t, env.Priya, p15dDoc(purchase.DocType, draft.ID)+"/edit/items").notes(); len(got) != 2 {
		t.Errorf("notes with LastPricesFor working = %q, want one per line", got)
	}
	if n := good.calls.Load(); n != 1 {
		t.Errorf("one Items page asked LastPricesFor %d times, want once", n)
	}
}

// CD-313, CD-315 (M0-40): from 64rem an Order's lines are the display grid
// with five cells per line: Item (name, code under it), Qty (qty kind), UoM,
// Rate (rate kind) and Tax; docscreen adds #, Amount and the note row,
// which is the last-price note.
func TestPO_OrderLineGrid(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	src := p15aSource(t, w)
	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p15aCement(t, w, "40", "")))
	page := w.get(t, env.Priya, p15dDoc(purchase.DocType, draft.ID)+"/edit/items")
	ed := page.doc.Find(".linelist.linelist--grid .line-editor")
	if ed.Length() != 1 {
		t.Fatalf("Items step has %d display grids, want one", ed.Length())
	}
	var head []string
	ed.Find(".linegrid-head").Children().Each(func(_ int, s *goquery.Selection) { head = append(head, p15dCollapse(s.Text())) })
	if want := []string{"#", "Item", "Qty", "UoM", "Rate", "Tax", "Amount"}; len(head) < len(want) || !slices.Equal(head[:len(want)], want) {
		t.Errorf("grid head = %q, want it to start %q", head, want)
	}
	rows := ed.Find(".linegrid .linegrid-row")
	if rows.Length() != 1 {
		t.Fatalf("grid rows = %d, want 1", rows.Length())
	}
	text := p15dCollapse(rows.Text())
	if !p15aContains(text, "OPC Cement 53 Grade", "CEMENT-OPC", "40", "BAG", "₹345.00", "GST 18%", "₹13,800.00") {
		t.Errorf("grid row = %q, want item, code, qty, unit, rate, tax and amount in order", text)
	}
	for kind, want := range map[string]string{"qty": "40", "rate": "₹345.00"} {
		if k := rows.Find(`[data-kind="` + kind + `"]`); k.Length() != 1 || p15dCollapse(k.Text()) != want {
			t.Errorf("%d data-kind=%q cells reading %q, want one reading %q", k.Length(), kind, p15dCollapse(k.Text()), want)
		}
	}
	if !strings.Contains(p15dCollapse(ed.Text()), p15aNote(src)) {
		t.Errorf("the grid has no note row %q: %q", p15aNote(src), p15dCollapse(ed.Text()))
	}

	l := docscreen.LineView{
		Card:    components.LineCard{Item: "OPC Cement 53 Grade", Code: "CEMENT-OPC"},
		Display: map[string]string{"qty": "40", "uom_id": "BAG Bag", "price": "₹345.00", "tax_code_id": "GST18 GST 18%"},
	}
	dt, _ := doctype.Lookup(purchase.DocType)
	got := Modules(Deps{})[purchase.DocType].LineGrid(docscreen.Ctx{DT: dt}, l)
	want := []components.GridCell{
		{Label: "Item", Text: "OPC Cement 53 Grade", Sub: "CEMENT-OPC"},
		{Label: "Qty", Text: "40", Kind: "qty"},
		{Label: "UoM", Text: "BAG"},
		{Label: "Rate", Text: "₹345.00", Kind: "rate"},
		{Label: "Tax", Text: "GST 18%"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("LineGrid = %+v, want %+v", got, want)
	}
}

// p15aHiding is newP15aWeb with purchase's Order Module wrapped so its Hide
// also hides field, as a screen's own Hide would (the redraw sweep's probe).
func p15aHiding(t *testing.T, env *purchasetest.Env, field string) *p15dWeb {
	t.Helper()
	w := newP15aWeb(t, env, nil)
	mods := Modules(Deps{Service: env.Service, Approvals: env.Approvals, Comments: env.Comments})
	m := mods[purchase.DocType]
	own := m.Hide
	m.Hide = func(c docscreen.Ctx, f doctype.Field) bool { return f.Name == field || own(c, f) }
	mods[purchase.DocType] = m
	r := chi.NewRouter()
	docscreen.Routes(docscreen.Deps{DB: env.DB, Engine: env.Engine, Auth: env.Auth, Approval: env.Approvals, Extras: docscreentest.NewExtras(), Modules: mods})(r)
	w.h = r
	return w
}

// CD-319, hooks spec §4.3 (a hidden field is never rendered): the Order's
// cards and display grid leave out each line field the screen's Hide hides,
// and the last-price note, which names the item, unit, rate and vendor, is
// left out when any of them is hidden. Each row hides one field on a draft
// read by Priya (Items step), or on an approved PO read by Ravi for
// Received.
func TestPO_OrderLinesHonourHide(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15aWeb(t, env, nil)
	src := p15aSource(t, w)
	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p15aCement(t, w, "40", "")))
	done := purchasetest.NewOrder(t, env, purchasetest.OrderLines(p15aCement(t, w, "40", "")))
	full := []string{"40 BAG × ₹345.00", "GST 18%"}
	for _, c := range []struct {
		field    string
		approved bool
		gone     []string // texts the lines must not show
		kept     []string // texts they still show
		note     bool
		qtyRate  string // the card's quantity and rate line, exactly
		received string // the card's Received line, exactly ("" none)
	}{
		{"item_id", false, []string{"OPC Cement 53 Grade", "CEMENT-OPC"}, []string{"BAG", "₹345.00", "GST 18%"}, false, "40 BAG × ₹345.00", ""},
		{"qty", false, []string{"40 BAG"}, []string{"₹345.00", "GST 18%"}, true, "₹345.00", ""},
		{"uom_id", false, []string{"BAG"}, []string{"40 × ₹345.00", "GST 18%"}, false, "40 × ₹345.00", ""},
		{"price", false, []string{"₹345.00"}, []string{"40 BAG", "GST 18%"}, false, "40 BAG", ""},
		{"tax_code_id", false, []string{"GST 18%"}, []string{"40 BAG × ₹345.00"}, true, "40 BAG × ₹345.00", ""},
		{"vendor_id", false, []string{"Shree Steel Traders"}, full, false, "40 BAG × ₹345.00", ""},
		{"received_qty", true, []string{"Received"}, full, false, "40 BAG × ₹345.00", ""},
		{"description", true, nil, append(slices.Clone(full), "Received 0 of 40 BAG"), false, "40 BAG × ₹345.00", "Received 0 of 40 BAG"},
		// On an approved PO: Received names no hidden quantity or unit.
		{"qty", true, []string{"40 BAG"}, []string{"₹345.00", "GST 18%"}, false, "₹345.00", "Received 0 BAG"},
		{"uom_id", true, []string{"BAG"}, []string{"40 × ₹345.00", "GST 18%"}, false, "40 × ₹345.00", "Received 0 of 40"},
		{"price", true, []string{"₹345.00"}, []string{"40 BAG", "GST 18%"}, false, "40 BAG", "Received 0 of 40 BAG"},
	} {
		name := c.field
		if c.approved {
			name += " approved"
		}
		t.Run(name, func(t *testing.T) {
			w := p15aHiding(t, env, c.field)
			p, path := env.Priya, p15dDoc(purchase.DocType, draft.ID)+"/edit/items"
			if c.approved {
				p, path = env.Ravi, p15dDoc(purchase.DocType, done.ID)
			}
			page := w.get(t, p, path)
			if page.status != 200 {
				t.Fatalf("GET %s = %d: %s", path, page.status, page.text())
			}
			lines := p15dCollapse(page.doc.Find(".linelist").Text())
			if c.field == "item_id" {
				// docscreen leaves the item out of l.Card (M0-47); the grid is ours.
				lines = p15dCollapse(page.doc.Find(".line-editor").Text())
			}
			for _, s := range c.gone {
				if strings.Contains(lines, s) {
					t.Errorf("with %s hidden the lines show %q: %q", c.field, s, lines)
				}
			}
			for _, s := range c.kept {
				if !strings.Contains(lines, s) {
					t.Errorf("with %s hidden the lines lack %q: %q", c.field, s, lines)
				}
			}
			if got := strings.Contains(lines, "Last bought at"); got != c.note {
				t.Errorf("with %s hidden the last-price note shows %v, want %v (%q)", c.field, got, c.note, p15aNote(src))
			}
			if n := page.doc.Find(".linegrid-row .linegrid-cell").Length(); n != 5 {
				t.Errorf("with %s hidden the grid row has %d cells, want 5", c.field, n)
			}
			// The card's own lines, uncollapsed, so a double space shows.
			card := page.doc.Find(".lines-card")
			if card.Length() != 1 {
				t.Fatalf("with %s hidden: %d line cards, want 1", c.field, card.Length())
			}
			if got := strings.TrimSpace(card.Find(".lines-meta .num").Text()); got != c.qtyRate {
				t.Errorf("with %s hidden the card's QtyRate = %q, want %q", c.field, got, c.qtyRate)
			}
			var received []string
			card.Find("p.lines-meta").Each(func(_ int, s *goquery.Selection) {
				if txt := strings.TrimSpace(s.Text()); strings.HasPrefix(txt, "Received") {
					received = append(received, txt)
				}
			})
			var want []string
			if c.received != "" {
				want = []string{c.received}
			}
			if !slices.Equal(received, want) {
				t.Errorf("with %s hidden the card's Received = %q, want %q", c.field, received, want)
			}
		})
	}
}
