package purchaseweb

import (
	"slices"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// R13 item 1 (CD-387 (2)): Project, UoM, Unit and Tax code show no star on
// the wizard's pages; the fields that don't resolve keep theirs.
func TestR13_Stars(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	order := w.get(t, env.Priya, "/doc/"+purchase.DocType+"/new")
	p36Check(t, "order details", order.doc,
		p36Want{sel: r13Pick("project_id"), hint: p36Hint(t, purchase.DocType, "project_id")},
		p36Want{sel: r13Pick("vendor_id"), required: true, hint: p36Hint(t, purchase.DocType, "vendor_id")})
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	line := w.get(t, env.Priya, p15dDoc(purchase.DocType, po.ID)+"/lines/new")
	p36Check(t, "order line", line.doc,
		p36Want{sel: r13Pick("uom_id"), hint: p36Hint(t, purchase.DocType, "uom_id")},
		p36Want{sel: r13Pick("tax_code_id"), hint: p36Hint(t, purchase.DocType, "tax_code_id")},
		p36Want{sel: r13Pick("item_id"), required: true, hint: p36Hint(t, purchase.DocType, "item_id")})
	request := w.get(t, env.Suresh, "/doc/"+purchase.RequestDocType+"/new")
	p36Check(t, "request details", request.doc,
		p36Want{sel: r13Pick("project_id"), hint: p36Hint(t, purchase.RequestDocType, "project_id")})
	pr := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())
	prLine := w.get(t, env.Suresh, p15dDoc(purchase.RequestDocType, pr.ID)+"/lines/new")
	p36Check(t, "request line", prLine.doc,
		p36Want{sel: r13Pick("uom_id"), hint: p36Hint(t, purchase.RequestDocType, "uom_id")})
}

// r13Pick selects the link field name's combobox.
func r13Pick(name string) string {
	return `.field:has([name="` + name + `"]) input[role="combobox"]`
}

// R13 item 1: what a blank field resolved to shows after the save: the
// project in its control and on Details, the unit and tax on the line card,
// in the line page's controls.
func TestR13_ResolvedValuesShow(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	res, err := env.Engine.Do(t.Context(), env.Arjun, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"vendor_id": w.id(t, "party", "SHREE"), "site_id": w.id(t, "site", "METRO-SITE")},
		Lines:  []map[string]string{{"item_id": w.id(t, "item", "MS-ANGLE-50"), "qty": "10", "price": "62.5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	base := p15dDoc(purchase.DocType, res.ID)
	details := w.get(t, env.Arjun, base+"/edit/details")
	if got := details.doc.Find(`[name="project_id"]`).AttrOr("value", ""); got != w.id(t, "project", "PRJ-METRO") {
		t.Errorf("project control value = %q", got)
	}
	items := w.get(t, env.Arjun, base+"/edit/items")
	card := p15dCollapse(items.doc.Find(".lines-card").First().Text())
	if !containsAll(card, "10 ", "× ₹62.50", "18%") {
		t.Errorf("line card %q lacks the resolved unit or tax", card)
	}
	line := w.get(t, env.Arjun, base+"/lines/1")
	if got := line.doc.Find(`[name="uom_id"]`).AttrOr("value", ""); got != w.id(t, "uom", "KGS") {
		t.Errorf("uom control value = %q", got)
	}
	if got := line.doc.Find(`[name="tax_code_id"]`).AttrOr("value", ""); got == "" {
		t.Errorf("tax code control is blank")
	}
}

// R13 item 1: the star-less Project keeps what DOC does with it: a user
// restricted on projects still gets its named refusal on a draft (CD-178,
// PO-012), and the redraw keeps every typed value in its field; a stored draft
// and a new document's prefill fill the step, and a parse error names its
// field.
func TestR13_DetailsBody(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	vendor, wh := w.id(t, "party", "SHREE"), w.id(t, "site", "PUNE-WH")
	a := w.post(t, env.Priya, "/doc/"+purchase.DocType+"/new", r13Form(0, vendor, wh, ""))
	if a.status != 422 || !r13Has(a.find(".field__msg--error"), "Project is required.") {
		t.Errorf("restricted, blank project = %d: %s", a.status, a.text())
	}
	if got := a.doc.Find(`[name="site_id"]`).AttrOr("value", ""); got != wh {
		t.Errorf("redraw lost the site: %q", got)
	}
	if got := w.get(t, env.Priya, "/doc/"+purchase.DocType+"/new").doc.Find(`[name="project_id"]`).AttrOr("value", ""); got != w.id(t, "project", "PRJ-METRO") {
		t.Errorf("Priya's only project isn't prefilled: %q", got)
	}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	details := w.get(t, env.Priya, p15dDoc(purchase.DocType, po.ID)+"/edit/details")
	if got := details.doc.Find(`[name="site_id"]`).AttrOr("value", ""); got != w.id(t, "site", "METRO-SITE") {
		t.Errorf("stored site = %q", got)
	}
	bad := r13Form(0, vendor, wh, "")
	bad.Set("order_date", "not a date")
	b := w.post(t, env.Arjun, "/doc/"+purchase.DocType+"/new", bad)
	if b.status != 422 || len(b.find(`#f-order_date-msg`)) != 1 || len(b.find(".field__msg--error")) != 1 {
		t.Errorf("bad date = %d, messages %q: want the date's alone", b.status, b.find(".field__msg--error"))
	}
	// Each step field once, none it doesn't own, read-only ones as text.
	if n := details.doc.Find(`[name="vendor_id"]`).Length(); n != 1 {
		t.Errorf("%d vendor inputs", n)
	}
	if n := details.doc.Find(`input[name="branch_id"]`).Length(); n != 0 {
		t.Errorf("Billed to has an input")
	}
	if !containsAll(details.text(), "Billed to") {
		t.Errorf("Billed to isn't shown: %s", details.text())
	}
}

// R13 item 1: a field a module hides has no input on the step.
func TestR13_DetailsBodyHide(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15aWebWith(t, env, nil, func(mods map[string]docscreen.Module) {
		m := mods[purchase.DocType]
		inner := m.Hide
		m.Hide = func(c docscreen.Ctx, f doctype.Field) bool { return f.Name == "required_by" || inner(c, f) }
		mods[purchase.DocType] = m
	})
	if n := w.get(t, env.Priya, "/doc/"+purchase.DocType+"/new").doc.Find(`[name="required_by"]`).Length(); n != 0 {
		t.Errorf("%d inputs for a hidden field", n)
	}
}

// R13 item 1, R7b: Check says what Submit will refuse: a tax code that
// didn't resolve is a CheckExtra problem, so the Items section is Needs
// attention and lists it (not a top warning), and a Project that can't
// resolve is still Needs attention. A resolved order's Items is Complete.
func TestR13_CheckNamesMissing(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE item SET default_tax_code_id = NULL WHERE code = 'MS-ANGLE-50'`); err != nil {
		t.Fatal(err)
	}
	res, err := env.Engine.Do(t.Context(), env.Arjun, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"vendor_id": w.id(t, "party", "SHREE"), "site_id": w.id(t, "site", "PUNE-WH")},
		Lines:  []map[string]string{{"item_id": w.id(t, "item", "MS-ANGLE-50"), "qty": "10", "price": "62.5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	check := w.get(t, env.Arjun, p15dDoc(purchase.DocType, res.ID)+"/edit/check")
	if got := check.find("main .banner--warning"); r13Has(got, "Tax code") {
		t.Errorf("check warnings = %q, want the tax code in its section only", got)
	}
	items := r7bSection(check.doc, "Items")
	if got := p15dCollapse(items.Find(".check-status").Text()); got != "Needs attention" {
		t.Errorf("Items = %q, want Needs attention", got)
	}
	if got := items.Find("ul > li").Map(func(_ int, s *goquery.Selection) string { return p15dCollapse(s.Text()) }); !slices.Equal(got, []string{"Line 1: Tax code is required."}) {
		t.Errorf("Items lists %q, want the tax code", got)
	}
	if !containsAll(check.text(), "Project is required.") {
		t.Errorf("check lost the project problem: %s", check.text())
	}
	ok := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	resolved := w.get(t, env.Priya, p15dDoc(purchase.DocType, ok.ID)+"/edit/check")
	if got := resolved.find("main .banner--warning"); r13Has(got, "is required.") {
		t.Errorf("resolved order warnings = %q", got)
	}
	if got := p15dCollapse(r7bSection(resolved.doc, "Items").Find(".check-status").Text()); got != "Complete" {
		t.Errorf("resolved order's Items = %q, want Complete", got)
	}
}

// r7bSection is the Check page's section titled title.
func r7bSection(page *goquery.Document, title string) *goquery.Selection {
	return page.Find("section.card").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return p15dCollapse(s.Find(".card-title").First().Text()) == title
	})
}

// R13 item 7: the switch's hint says turn on, not tick.
func TestR13_ReverseChargeHint(t *testing.T) {
	const want = "Turn on only if your CA has said reverse charge applies, so GST is shown as payable by us and left out of the total."
	if got := p36Hint(t, purchase.DocType, "reverse_charge"); got != want {
		t.Errorf("hint = %q", got)
	}
}

// R13 item 6: every step of the Request's wizard has its own intro.
func TestR13_RequestStepIntros(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	pr := purchasetest.NewRequest(t, w.env, purchasetest.StopAtDraft())
	base := p15dDoc(purchase.RequestDocType, pr.ID) + "/edit/"
	want := map[string]string{
		"details": "Where the items are needed and what they're for.",
		"items":   "Add what you need, with the quantity for each line.",
		"check":   "Check the request, then submit it.",
	}
	for slug, intro := range want {
		if text := w.get(t, w.env.Suresh, base+slug).text(); !containsAll(text, intro) {
			t.Errorf("%s step lacks %q: %s", slug, intro, text)
		}
	}
}
