package purchaseweb

import (
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

const r13ZeroMoney = "₹0.00"

// R13 item 2: an Order with no lines says so, shows no ₹0.00 anywhere on its
// wizard, and keeps the add button; the first line brings the totals back.
func TestR13_EmptyOrderItems(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	id := r13Empty(t, w, purchase.DocType)
	base := p15dDoc(purchase.DocType, id) + "/edit/"

	items := w.get(t, env.Priya, base+"items")
	if got := items.find(".empty-state-text"); len(got) != 1 || got[0] != "No items yet." {
		t.Errorf("empty state = %q", got)
	}
	if got := items.find("#doc-totals .summary-row"); len(got) != 1 || !strings.HasPrefix(got[0], "Billed to") {
		t.Errorf("rows with no lines = %q, want only Billed to", got)
	}
	if n := items.doc.Find("[data-add-line]").Length(); n != 1 {
		t.Errorf("%d add buttons, want 1", n)
	}
	if got := p15dCollapse(items.doc.Find("#footer-total").Text()); got != "0 items" {
		t.Errorf("footer = %q, want only the count", got)
	}
	for _, step := range []string{"items", "details", "check"} {
		if text := w.get(t, env.Priya, base+step).text(); strings.Contains(text, r13ZeroMoney) {
			t.Errorf("%s step shows %s: %s", step, r13ZeroMoney, text)
		}
	}

	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	full := w.get(t, env.Priya, p15dDoc(purchase.DocType, po.ID)+"/edit/items")
	if len(full.find(".empty-state-text")) != 0 || !strings.Contains(p15dCollapse(full.doc.Find("#doc-totals").Text()), "Taxable value") {
		t.Errorf("one line: empty %q, totals %q", full.find(".empty-state-text"), full.find("#doc-totals"))
	}
}

// R13 item 2: a Request with no lines says so too.
func TestR13_EmptyRequestItems(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	id := r13Empty(t, w, purchase.RequestDocType)
	items := w.get(t, w.env.Suresh, p15dDoc(purchase.RequestDocType, id)+"/edit/items")
	if got := items.find(".empty-state-text"); len(got) != 1 || got[0] != "No items yet." {
		t.Errorf("empty state = %q", got)
	}
}

// r13Totals are the check page's totals list, label and value together.
func r13Totals(p p15dPage) []string { return p.find("main .stack > .summary .summary-row") }

// R13 item 3: Check keeps the totals in one place. Details has none of them;
// the list after the grand total has the taxable value and the GST rows that
// aren't zero (intra-state here, so no IGST). The grand total shows only when
// rounding changed it.
func TestR13_CheckTotals(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	check := w.get(t, env.Priya, p15dDoc(purchase.DocType, po.ID)+"/edit/check")
	details := check.doc.Find(".summary").First().Text()
	for _, label := range []string{"Taxable value", "CGST", "SGST", "IGST", "Grand total", "Rounded total"} {
		if strings.Contains(details, label) {
			t.Errorf("Details repeats %s: %s", label, p15dCollapse(details))
		}
	}
	want := []string{"Taxable value₹6,250.00", "CGST₹562.50", "SGST₹562.50"}
	if got := r13Totals(check); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("totals = %q, want %q", got, want)
	}

	round := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(map[string]string{"qty": "1", "price": "10.1"}))
	got := r13Totals(w.get(t, env.Priya, p15dDoc(purchase.DocType, round.ID)+"/edit/check"))
	if !r13Has(got, "Grand total₹11.92") {
		t.Errorf("rounded order's totals = %q, want the exact grand total", got)
	}
}

// R13 item 3: inter-state, the list has IGST and no CGST or SGST rows.
func TestR13_InterStateTotals(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	res, err := env.Engine.Do(t.Context(), env.Arjun, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"vendor_id": w.id(t, "party", "SHREE"), "site_id": w.id(t, "site", "TECH-SITE"), "project_id": w.id(t, "project", "PRJ-TECHPARK")},
		Lines: []map[string]string{{"item_id": w.id(t, "item", "MS-ANGLE-50"), "uom_id": w.id(t, "uom", "KGS"), "qty": "100", "price": "62.5",
			"tax_code_id": w.id(t, "tax_code", "GST18"), "hsn_sac": "7216"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := r13Totals(w.get(t, env.Arjun, p15dDoc(purchase.DocType, res.ID)+"/edit/check"))
	want := []string{"Taxable value₹6,250.00", "IGST₹1,125.00"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("totals = %q, want %q", got, want)
	}
}

// R13 item 3: the document page's Details repeats neither the header's key
// facts nor the totals; the key facts stay in the header, and the totals have
// their own card, after the lines. Fields Details alone shows stay there.
func TestR13_PageTotalsAndFacts(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	po := purchasetest.NewOrder(t, env)
	page := w.get(t, env.Priya, p15dDoc(purchase.DocType, po.ID))
	details := page.find(".doc-main section:has(.card-head:contains('Details')) .summary dt")
	for _, label := range []string{"Vendor", "Deliver to", "Order date", "Required by", "Taxable value", "CGST", "SGST", "IGST", "Grand total", "Rounded total"} {
		if slices.Contains(details, label) {
			t.Errorf("Details repeats %s: %q", label, details)
		}
	}
	if !slices.Contains(details, "Project") || !slices.Contains(details, "Raised by") {
		t.Errorf("Details lost its own rows: %q", details)
	}
	facts := p15dCollapse(page.doc.Find(".doc-head__facts").Text())
	if !containsAll(facts, "Vendor", "Deliver to", "Order date") {
		t.Errorf("header facts = %q", facts)
	}
	totals := page.find(".doc-main section:has(.card-head:contains('Totals')) .summary-row")
	if want := []string{"Taxable value₹6,250.00", "CGST₹562.50", "SGST₹562.50"}; strings.Join(totals, "|") != strings.Join(want, "|") {
		t.Errorf("totals card = %q, want %q", totals, want)
	}
	if got := page.doc.Find(".doc-head__amount").Text(); got != "₹7,375.00" {
		t.Errorf("header amount = %q", got)
	}
}

// R13 item 3: a field the screen hides is in neither totals list; a viewer
// with every total hidden gets no totals card.
func TestR13_TotalsObeyHide(t *testing.T) {
	hide := func(names ...string) func(map[string]docscreen.Module) {
		return func(mods map[string]docscreen.Module) {
			m := mods[purchase.DocType]
			inner := m.Hide
			m.Hide = func(c docscreen.Ctx, f doctype.Field) bool {
				for _, n := range names {
					if f.Name == n {
						return true
					}
				}
				return inner(c, f)
			}
			mods[purchase.DocType] = m
		}
	}
	env := purchasetest.New(t)
	w := newP15aWebWith(t, env, nil, hide("net_total", "cgst_total"))
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	got := r13Totals(w.get(t, env.Priya, p15dDoc(purchase.DocType, po.ID)+"/edit/check"))
	if want := []string{"SGST₹562.50"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("check totals = %q, want %q", got, want)
	}

	env1 := purchasetest.New(t)
	w1 := newP15aWebWith(t, env1, nil, hide("cgst_total", "sgst_total", "igst_total", "grand_total"))
	po1 := purchasetest.NewOrder(t, env1)
	one := w1.get(t, env1.Priya, p15dDoc(purchase.DocType, po1.ID)).find("section:has(.card-head:contains('Totals')) .summary-row")
	if want := []string{"Taxable value₹6,250.00"}; strings.Join(one, "|") != strings.Join(want, "|") {
		t.Errorf("one total shown: card = %q, want %q", one, want)
	}

	env2 := purchasetest.New(t)
	w2 := newP15aWebWith(t, env2, nil, hide("net_total", "cgst_total", "sgst_total", "igst_total", "grand_total"))
	po2 := purchasetest.NewOrder(t, env2)
	if n := w2.get(t, env2.Priya, p15dDoc(purchase.DocType, po2.ID)).doc.Find("section:has(.card-head:contains('Totals'))").Length(); n != 0 {
		t.Errorf("%d totals cards with every total hidden", n)
	}
}

// R13 item 5: the Order's card has the unit's name and the discount, so
// `{qty} {unit} × {rate}` adds up to the amount; no discount, no words; the
// line notes and the Request's card use the name too, a hidden discount
// isn't shown, and the print's code stays (CD-331).
func TestR13_LineCardUnitAndDiscount(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15aWeb(t, env, nil)
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE uom SET name = 'Metric Ton' WHERE code = 'BAG'`); err != nil {
		t.Fatal(err)
	}
	cement := map[string]string{"item_id": w.id(t, "item", "CEMENT-OPC"), "uom_id": w.id(t, "uom", "BAG"), "qty": "40",
		"price": "350", "discount_pct": "10", "tax_code_id": w.id(t, "tax_code", "GST18"), "hsn_sac": "2523"}
	plain := map[string]string{"item_id": w.id(t, "item", "CEMENT-OPC"), "uom_id": w.id(t, "uom", "BAG"), "qty": "40",
		"price": "350", "tax_code_id": w.id(t, "tax_code", "GST18"), "hsn_sac": "2523"}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(cement, plain))
	items := w.get(t, env.Priya, p15dDoc(purchase.DocType, po.ID)+"/edit/items")
	got := items.find(".lines-card .lines-meta .num")
	want := []string{"40 Metric Ton × ₹350.00 less 10%", "40 Metric Ton × ₹350.00"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("cards = %q, want %q", got, want)
	}
	if amounts := items.find(".lines-card .lines-amount"); len(amounts) != 2 || amounts[0] != "₹12,600.00" {
		t.Errorf("amounts = %q, want ₹12,600.00 first", amounts)
	}

	approved := purchasetest.NewOrder(t, env, purchasetest.OrderLines(plain))
	page := w.get(t, env.Priya, p15dDoc(purchase.DocType, approved.ID))
	if notes := page.notes(); !r13Has(notes, "Pending 40 Metric Ton") {
		t.Errorf("approved notes = %q", notes)
	}

	pr := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft(), purchasetest.RequestLines(map[string]string{
		"item_id": w.id(t, "item", "CEMENT-OPC"), "uom_id": w.id(t, "uom", "BAG"), "qty": "40",
	}))
	if got := w.get(t, env.Suresh, p15dDoc(purchase.RequestDocType, pr.ID)+"/edit/items").find(".lines-card .lines-meta .num"); len(got) != 1 || got[0] != "40 Metric Ton" {
		t.Errorf("request card = %q", got)
	}
}

// R13 item 5: a UoM whose name is blank keeps its code; a discount the screen
// hides leaves the card; so does a hidden rate's.
func TestR13_CardGuards(t *testing.T) {
	if got := unitOf(docscreen.LineView{Display: map[string]string{"uom_id": "BAG"}}); got != "BAG" {
		t.Errorf("no name: %q", got)
	}
	if got := unitOf(docscreen.LineView{Display: map[string]string{"uom_id": "BAG Bag"}}); got != "Bag" {
		t.Errorf("code and name: %q", got)
	}
	for _, name := range []string{"discount_pct", "price"} {
		env := purchasetest.New(t)
		w := newP15aWebWith(t, env, nil, func(mods map[string]docscreen.Module) {
			m := mods[purchase.DocType]
			inner := m.Hide
			m.Hide = func(c docscreen.Ctx, f doctype.Field) bool { return f.Name == name || inner(c, f) }
			mods[purchase.DocType] = m
		})
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(map[string]string{"discount_pct": "10"}))
		card := w.get(t, env.Priya, p15dDoc(purchase.DocType, po.ID)+"/edit/items").find(".lines-card .lines-meta .num")
		if len(card) != 1 || strings.Contains(card[0], "less") {
			t.Errorf("%s hidden: card = %q", name, card)
		}
	}
}

// r13Rejected is Priya's PO rejected by Ravi with reason.
func r13Rejected(t *testing.T, w *p15dWeb, reason string) doc.Doc {
	t.Helper()
	po := purchasetest.NewOrder(t, w.env, purchasetest.StopAtPending(), purchasetest.OrderLines(map[string]string{"qty": "1000"}))
	w.act(t, w.env.Ravi, purchase.DocType, po.ID, doctype.ActionReject, reason)
	return po
}

const r13Reason = "Rate too high for this vendor."

// r13Steps are the Order's wizard steps with their tails.
var r13Steps = []string{"details", "terms", "items", "items?from=requests", "check"}

// R13 item 4: after Revise, the rejection reason stays as a banner on every
// wizard step, a redraw after a refused save included, until the resubmit.
// A draft never rejected has none, and the pending and rejected states have
// no wizard to show it on.
func TestR13_RejectionOnEveryStep(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	banner := "Rejected: " + r13Reason
	po := r13Rejected(t, w, r13Reason)
	base := p15dDoc(purchase.DocType, po.ID)

	for _, step := range r13Steps {
		if p := w.get(t, env.Priya, base+"/edit/"+step); p.status != 303 {
			t.Errorf("rejected %s = %d, want the document page", step, p.status)
		}
	}
	w.act(t, env.Priya, purchase.DocType, po.ID, doctype.ActionRevise, "")
	for _, step := range r13Steps {
		p := w.get(t, env.Priya, base+"/edit/"+step)
		if got := p.find("main .banner--warning"); !r13Has(got, banner) {
			t.Errorf("revised %s: warnings %q lack the reason", step, got)
		}
	}

	var version int
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT version FROM purchase_order WHERE id = ?`, po.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	bad := r13Form(version, w.id(t, "party", "SHREE"), w.id(t, "site", "METRO-SITE"), w.id(t, "project", "PRJ-METRO"))
	bad.Set("order_date", "not a date")
	if p := w.post(t, env.Priya, base+"/edit/details", bad); p.status != 422 || !r13Has(p.find("main .banner--warning"), banner) {
		t.Errorf("422 redraw = %d, warnings %q", p.status, p.find("main .banner--warning"))
	}

	w.act(t, env.Priya, purchase.DocType, po.ID, doctype.ActionSubmit, "")
	for _, step := range r13Steps {
		if p := w.get(t, env.Priya, base+"/edit/"+step); p.status != 303 {
			t.Errorf("pending %s = %d, want the document page", step, p.status)
		}
	}
	w.act(t, env.Priya, purchase.DocType, po.ID, doctype.ActionWithdraw, "")
	for _, step := range r13Steps {
		if text := w.get(t, env.Priya, base+"/edit/"+step).text(); strings.Contains(text, r13Reason) {
			t.Errorf("resubmitted and withdrawn %s still shows the reason", step)
		}
	}

	fresh := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	for _, step := range r13Steps {
		if text := w.get(t, env.Priya, p15dDoc(purchase.DocType, fresh.ID)+"/edit/"+step).text(); strings.Contains(text, "Rejected:") {
			t.Errorf("never rejected %s shows a rejection", step)
		}
	}
}

// R13 item 4: only someone who may read and write the draft reaches a step:
// a user holding read alone goes to the document page, a stranger gets
// nothing, and a new document has no banner.
func TestR13_RejectionViewers(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	po := r13Rejected(t, w, r13Reason)
	w.act(t, env.Priya, purchase.DocType, po.ID, doctype.ActionRevise, "")
	base := p15dDoc(purchase.DocType, po.ID)
	reader := w.probe(t, "reader-r13")
	for _, step := range r13Steps {
		p := w.get(t, reader, base+"/edit/"+step)
		if p.status != 303 || strings.Contains(p.text(), r13Reason) {
			t.Errorf("read only %s = %d: %s", step, p.status, p.text())
		}
	}
	if p := w.get(t, env.Priya, "/doc/"+purchase.DocType+"/new"); p.status != 200 || len(p.find("main .banner--warning")) != 0 {
		t.Errorf("new document = %d, warnings %q", p.status, p.find("main .banner--warning"))
	}
}
