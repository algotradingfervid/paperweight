package purchaseweb

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// p36Want is one field as UI v2 B1 shows it (CD-341 (2), CD-350): its
// controls' selector, whether it is required and its one visible hint
// (blank: none).
type p36Want struct {
	sel      string
	required bool
	hint     string
}

// p36Check checks each field on doc: a required field's every control has
// aria-required="true" and its label the asterisk; an optional one has
// neither; the field shows want's hint once, or none. No "(optional)"
// anywhere on the page.
func p36Check(t *testing.T, page string, doc *goquery.Document, wants ...p36Want) {
	t.Helper()
	if html, _ := doc.Html(); strings.Contains(strings.ToLower(html), "(optional)") {
		t.Errorf("%s: the page says (optional)", page)
	}
	for _, w := range wants {
		controls := doc.Find(w.sel)
		if controls.Length() == 0 {
			t.Errorf("%s: no control %s", page, w.sel)
			continue
		}
		controls.Each(func(_ int, s *goquery.Selection) {
			if got := s.AttrOr("aria-required", ""); (got == "true") != w.required || (!w.required && got != "") {
				t.Errorf("%s: %s aria-required = %q, want required %v", page, w.sel, got, w.required)
			}
		})
		field := controls.First().Closest(".field")
		stars := field.Find(".field__label .field__req")
		if want := map[bool]int{true: 1, false: 0}[w.required]; stars.Length() != want || field.Find(".field__req").Length() != want {
			t.Errorf("%s: %s label has %d asterisks, want %d", page, w.sel, stars.Length(), want)
		}
		if w.required && strings.TrimSpace(stars.Text()) != "*" {
			t.Errorf("%s: %s asterisk = %q, want *", page, w.sel, stars.Text())
		}
		var hints []string
		field.ChildrenFiltered(".field__hint").Each(func(_ int, s *goquery.Selection) {
			hints = append(hints, squash(s.Text()))
		})
		switch {
		case w.hint == "" && len(hints) != 0:
			t.Errorf("%s: %s hints = %q, want none", page, w.sel, hints)
		case w.hint != "" && (len(hints) != 1 || hints[0] != w.hint):
			t.Errorf("%s: %s hints = %q, want [%q]", page, w.sel, hints, w.hint)
		}
	}
}

// p36Hint is the declared hint of dt's field name.
func p36Hint(t *testing.T, dt, name string) string {
	t.Helper()
	d, _ := doctype.Lookup(dt)
	f, ok := d.Field(name)
	if !ok || f.Hint == "" {
		t.Fatalf("%s.%s has no hint", dt, name)
	}
	return f.Hint
}

// MOD-PO P36 (UI v2 B1, CD-350): the Terms step's vendor address radios are
// optional, as declared, with the field's hint.
func TestP36_TermsAddress(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	po := purchasetest.NewOrder(t, w.env, purchasetest.StopAtDraft())
	page := w.get(t, w.env.Priya, p15dDoc(purchase.DocType, po.ID)+"/edit/terms")
	if page.status != 200 {
		t.Fatalf("GET terms = %d: %s", page.status, page.text())
	}
	p36Check(t, "terms", page.doc,
		p36Want{sel: `input[type="radio"][name="vendor_address_id"]`, hint: p36Hint(t, purchase.DocType, "vendor_address_id")})
}

// MOD-PO P36: Add from requests' quantity is the line's Quantity, required
// with its declared hint; the tick is a choice, never required.
func TestP36_FromRequests(t *testing.T) {
	w := newP15cWeb(t)
	pr := w.prA(t)
	d := w.draft(t)
	page, _ := w.open(t, w.env.Priya, d.ID, pr.Number)
	p36Check(t, "add from requests", page.doc,
		p36Want{sel: `input.num[name^="` + fromQty + `"]`, required: true, hint: p36Hint(t, purchase.DocType, "qty")},
		p36Want{sel: `input[type="checkbox"][name="` + fromTick + `"]`})
}

// MOD-PO P36: Create PO's vendor picker is required, with the page's hint.
func TestP36_CreatePOVendor(t *testing.T) {
	w := newP15dWeb(t)
	pr := w.prA(t)
	page := w.get(t, w.env.Priya, p15dOrderPath(pr.ID))
	if page.status != 200 {
		t.Fatalf("GET Create PO = %d: %s", page.status, page.text())
	}
	p36Check(t, "create PO", page.doc, p36Want{sel: `input[role="combobox"]`, required: true, hint: vendorHint})
}

// MOD-PO P36: Send needs the date and the way (spec §23); the note is
// optional with its hint.
func TestP36_Send(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	po := purchasetest.NewOrder(t, env)
	page := w.get(t, env.Priya, orderPath(po.ID, "/send"))
	if page.status != 200 {
		t.Fatalf("GET send = %d: %s", page.status, page.text)
	}
	p36Check(t, "send", page.doc,
		p36Want{sel: `input[name="sent_on"]`, required: true, hint: "For example, 27-03-2026"},
		p36Want{sel: `input[type="radio"][name="sent_via"]`, required: true},
		p36Want{sel: `textarea[name="note"]`, hint: sendNoteHint})
}

// MOD-PO P36: Expected on needs the date (SetExpectedPosted refuses a blank
// one); the note is optional.
func TestP36_Expected(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	po := purchasetest.NewOrder(t, env)
	page := w.get(t, env.Priya, orderPath(po.ID, "/expected"))
	if page.status != 200 {
		t.Fatalf("GET expected = %d: %s", page.status, page.text)
	}
	p36Check(t, "expected", page.doc,
		p36Want{sel: `input[name="expected_on"]`, required: true, hint: expectedHint},
		p36Want{sel: `textarea[name="note"]`})
}

// MOD-PO P36: Hand over needs the person; the note is optional with its
// hint.
func TestP36_HandOver(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	page := w.get(t, env.Priya, orderPath(po.ID, "/handover"))
	if page.status != 200 {
		t.Fatalf("GET handover = %d: %s", page.status, page.text)
	}
	p36Check(t, "hand over", page.doc,
		p36Want{sel: `input[type="radio"][name="to"]`, required: true},
		p36Want{sel: `textarea[name="note"]`, hint: noteHintHandOver})
}

// MOD-PO P36: declaredProps carries a field's Required, Hint and Tip as
// docscreen's FieldInput does; addressInput passes them on (here a vendor-less
// draft, whose hint is the page's text).
func TestP36_DeclaredProps(t *testing.T) {
	f := doctype.Field{Name: "vendor_address_id", Label: "Vendor address", Hint: "h", Tip: "Longer help.", Required: true}
	want := components.FieldProps{ID: "f-x", Name: f.Name, Label: f.Label, Hint: "h", Tip: "Longer help.", Required: true}
	if got := declaredProps(f, "f-x"); got.ID != want.ID || got.Name != want.Name || got.Label != want.Label ||
		got.Hint != want.Hint || got.Tip != want.Tip || got.Required != want.Required || got.Clearable {
		t.Errorf("declaredProps = %+v, want %+v", got, want)
	}

	env := purchasetest.New(t)
	dt, _ := doctype.Lookup(purchase.DocType)
	c := p150Ctx(env, env.Priya, httptest.NewRequest("GET", "/", nil), &docscreen.DocView{Inputs: map[string]string{}})
	c.DT = dt
	in, err := addressInput(c, f, "", "", "terms")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := in.Render(t.Context(), &b); err != nil {
		t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Find(".field__label .field__req").Length() != 1 {
		t.Errorf("a required address has no asterisk: %s", b.String())
	}
	if !strings.Contains(b.String(), "Longer help.") {
		t.Errorf("the address has no tip: %s", b.String())
	}
	if got := squash(doc.Find(".field__hint").Text()); got != noVendorText {
		t.Errorf("address hint = %q, want %q", got, noVendorText)
	}
}
