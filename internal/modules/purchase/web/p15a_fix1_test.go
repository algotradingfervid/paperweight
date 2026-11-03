package purchaseweb

import (
	"bytes"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// P15a fix round 1: the Terms form always posts the address, field errors
// sit on their own field, the address hint in full, a disabled stored
// address falls back to the primary billing radio, and the field hints of
// UX §5.4 to §5.6.

// The hint texts, from UX §5.4 to §5.6, one sentence each (P29).
const (
	p15aHintTerms    = "Left blank, the company's standard terms are used; type a dash to print none."
	p15aHintReverse  = "Turn on only if your CA has said reverse charge applies, so GST is shown as payable by us and left out of the total."
	p15aHintRemarks  = "For people in the company only, and never printed."
	p15aHintEarlier  = "Only for orders first raised on paper; printed on the order so the vendor recognises it."
	p15aHintRate     = "Left blank, the last price paid to this vendor on this project is used, or else the item's standard rate."
	p15aHintReceived = "What arrived before this order was entered in the app."
	p15aHintProject  = "Left blank, the site's project is used."
	// p15aMsgWithDate is spec §4's text (messages_order.go msgEarlierPOWithDate).
	p15aMsgWithDate = "Earlier PO no. is required with an earlier PO date."
	// p15aMsgNeeded is spec §4's text (messages_order.go msgEarlierPONeeded).
	p15aMsgNeeded = "Earlier PO no. is required when a line has Already received."
)

// p15aFormValues are the values the page's step form posts, as a browser
// would send them: every named input, select and textarea in the form that
// holds the version, radios and checkboxes only when checked.
func p15aFormValues(t *testing.T, p p15dPage) url.Values {
	t.Helper()
	form := p.doc.Find(`form:has([name="version"])`).First()
	if form.Length() == 0 {
		t.Fatalf("no form posts a version: %s", p.text())
	}
	v := url.Values{}
	form.Find("input[name], select[name], textarea[name]").Each(func(_ int, s *goquery.Selection) {
		name := s.AttrOr("name", "")
		switch goquery.NodeName(s) {
		case "textarea":
			v.Add(name, s.Text())
		case "select":
			v.Add(name, s.Find("option[selected]").AttrOr("value", ""))
		default:
			switch s.AttrOr("type", "text") {
			case "radio", "checkbox":
				if _, on := s.Attr("checked"); on {
					v.Add(name, s.AttrOr("value", ""))
				}
			case "submit", "button":
			default:
				v.Add(name, s.AttrOr("value", ""))
			}
		}
	})
	return v
}

// p15aStoredAddress is PO id's vendor_address_id, "" when NULL.
func p15aStoredAddress(t *testing.T, w *p15dWeb, id int64) string {
	t.Helper()
	var s string
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT COALESCE(CAST(vendor_address_id AS TEXT), '') FROM purchase_order WHERE id = ?`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

// p15aHiddenAddress checks the page has exactly one
// `<input type="hidden" name="vendor_address_id" value="">`.
func p15aHiddenAddress(t *testing.T, what string, p p15dPage) {
	t.Helper()
	in := p.doc.Find(`input[type="hidden"][name="vendor_address_id"]`)
	v, has := in.Attr("value")
	if in.Length() != 1 || !has || v != "" {
		t.Errorf(`%s: %d hidden vendor_address_id inputs (value %q, set %v), want one <input type="hidden" name="vendor_address_id" value="">`, what, in.Length(), v, has)
	}
	if n := p.doc.Find(`[name="vendor_address_id"]`).Length(); n != 1 {
		t.Errorf("%s: %d vendor_address_id controls, want only the hidden one", what, n)
	}
}

// Fix 1 (UX §5.5): with no vendor, and with a vendor whose addresses are all
// disabled, the Terms form still posts vendor_address_id, as a hidden empty
// input, and an error on vendor_address_id shows inline on those pages.
func TestPUX010_TermsAlwaysPostsAddress(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env

	// No vendor.
	none := p15aSave(t, env, env.Priya, p15aHeader(t, w, "", "METRO-SITE", "PRJ-METRO"))
	nonePath := p15dDoc(purchase.DocType, none) + "/edit/terms"
	page := w.get(t, env.Priya, nonePath)
	if page.status != 200 {
		t.Fatalf("GET terms with no vendor = %d", page.status)
	}
	p15aHiddenAddress(t, "no vendor", page)

	// A vendor whose addresses are all disabled after the save.
	bare := p15aSave(t, env, env.Priya, p15aHeader(t, w, "BHARAT", "METRO-SITE", "PRJ-METRO"))
	if p15aStoredAddress(t, w, bare) == "" {
		t.Fatal("BHARAT's draft has no address after its save; the test needs one stored")
	}
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE address SET disabled = 1 WHERE party_id = (SELECT id FROM party WHERE code = 'BHARAT')`); err != nil {
		t.Fatal(err)
	}
	barePath := p15dDoc(purchase.DocType, bare) + "/edit/terms"
	page = w.get(t, env.Priya, barePath)
	if page.status != 200 {
		t.Fatalf("GET terms with no address = %d", page.status)
	}
	p15aHiddenAddress(t, "no address", page)

	// Each page's own form posts vendor_address_id. With no vendor it saves
	// NULL. With no address the post is refused on vendor_address_id (no
	// address, so no supplier state: spec §7) and the redraw shows that error
	// at the field. A posted unknown address is refused at the field on both
	// pages. Each redraw keeps the hidden input.
	stored := p15aStoredAddress(t, w, bare)
	for _, c := range []struct {
		what, path string
		id         int64
		address    string // "": the page's own form
		status     int
		msg        string
	}{
		{"no vendor, own form", nonePath, none, "", 303, ""},
		{"no address, own form", barePath, bare, "", 422, "Set the state on the supplier's address so GST can be worked out."},
		{"no vendor, unknown address", nonePath, none, "999999", 422, "Vendor address not found. Pick one from the list."},
		{"no address, unknown address", barePath, bare, "999999", 422, "Vendor address not found. Pick one from the list."},
	} {
		t.Run(c.what, func(t *testing.T) {
			form := p15aFormValues(t, w.get(t, env.Priya, c.path))
			if got, ok := form["vendor_address_id"]; !ok || len(got) != 1 || got[0] != "" {
				t.Errorf("the page's form posts vendor_address_id %q (present %v), want one empty value", got, ok)
			}
			if c.address != "" {
				form.Set("vendor_address_id", c.address)
			}
			a := w.do(t, env.Priya, "POST", c.path, form)
			if a.status != c.status {
				t.Fatalf("POST = %d, want %d: %s", a.status, c.status, a.text())
			}
			if c.status == 303 {
				if got := p15aStoredAddress(t, w, c.id); got != "" {
					t.Errorf("vendor_address_id after the post = %s, want NULL", got)
				}
				return
			}
			if got := p15dCollapse(a.doc.Find("#f-vendor_address_id-msg").Text()); got != c.msg {
				t.Errorf("inline error at #f-vendor_address_id-msg = %q, want %q: %s", got, c.msg, a.text())
			}
			p15aHiddenAddress(t, c.what+" 422", a)
			if c.id == bare {
				if got := p15aStoredAddress(t, w, bare); got != stored {
					t.Errorf("a refused post changed vendor_address_id to %q, want %q kept", got, stored)
				}
			}
		})
	}
}

// p15aErrorFields are the ids of the page's inline field error regions.
func p15aErrorFields(p p15dPage) []string {
	var out []string
	p.doc.Find(".field__msg--error").Each(func(_ int, s *goquery.Selection) { out = append(out, s.AttrOr("id", "")) })
	return out
}

// Fix 2 (UX §5.5, spec §4): a valid earlier PO date with a blank number is
// refused with `Earlier PO no. is required with an earlier PO date.` at
// Earlier PO no. and at no other field, with the disclosure open. A line's
// Already received with both earlier-PO fields posted blank opens it on the
// error alone. A line error naming a header field isn't drawn at that field.
func TestPUX010_TermsErrorsOnOwnField(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t, purchasetest.WithNoRules()), nil)
	env := w.env
	disclosure := func(p p15dPage) (n int, open bool) {
		d := p.doc.Find("details").FilterFunction(func(_ int, s *goquery.Selection) bool {
			return strings.HasPrefix(p15dCollapse(s.Find("summary").Text()), "More details") // the earlier-PO fields are Field.More (P40)
		})
		_, open = d.Attr("open")
		return d.Length(), open
	}
	check := func(t *testing.T, a p15dPage, want string) {
		t.Helper()
		if a.status != 422 {
			t.Fatalf("POST = %d, want 422: %s", a.status, a.text())
		}
		if n, open := disclosure(a); n != 1 || !open {
			t.Errorf("422: %d More details groups, open %v; want one, open", n, open)
		}
		if got := p15dCollapse(a.doc.Find("#f-earlier_po_no-msg").Text()); got != want {
			t.Errorf("#f-earlier_po_no-msg = %q, want %q", got, want)
		}
		if got := p15aErrorFields(a); len(got) != 1 || got[0] != "f-earlier_po_no-msg" {
			t.Errorf("inline errors at %q, want only f-earlier_po_no-msg", got)
		}
		if n := a.doc.Find(`[aria-invalid="true"]`).Length(); n != 1 || a.doc.Find(`#f-earlier_po_no[aria-invalid="true"]`).Length() != 1 {
			t.Errorf("%d controls marked invalid, want only #f-earlier_po_no", n)
		}
	}

	t.Run("date without number", func(t *testing.T) {
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		terms := p15dDoc(purchase.DocType, po.ID) + "/edit/terms"
		form := p15aFormValues(t, w.get(t, env.Priya, terms))
		form.Set("earlier_po_date", "15-09-2026")
		form.Set("earlier_po_no", "")
		check(t, w.do(t, env.Priya, "POST", terms, form), p15aMsgWithDate)
	})

	t.Run("already received, both blank", func(t *testing.T) {
		line := p15aCement(t, w, "40", "345")
		line["already_received"] = "5"
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(),
			purchasetest.OrderHeader(map[string]string{"earlier_po_no": "P-17"}), purchasetest.OrderLines(line))
		terms := p15dDoc(purchase.DocType, po.ID) + "/edit/terms"
		form := p15aFormValues(t, w.get(t, env.Priya, terms))
		form.Set("earlier_po_no", "")
		form.Set("earlier_po_date", "")
		check(t, w.do(t, env.Priya, "POST", terms, form), p15aMsgNeeded)
	})

	t.Run("line error naming a header field", func(t *testing.T) {
		dt, _ := doctype.Lookup(purchase.DocType)
		body := new(module).termsBody
		c := docscreen.Ctx{Req: httptest.NewRequest("GET", "/", nil), DT: dt, View: &docscreen.DocView{Inputs: map[string]string{}}}
		comp, err := body(c, docscreen.StepState{Errors: []doctype.FieldError{
			{Line: 1, Field: "earlier_po_no", Message: "p15a line error"},
			{Field: "remarks", Message: "p15a remarks error"},
		}})
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if err := comp.Render(t.Context(), &b); err != nil {
			t.Fatal(err)
		}
		d, err := goquery.NewDocumentFromReader(&b)
		if err != nil {
			t.Fatal(err)
		}
		p := p15dPage{doc: d}
		if strings.Contains(p.text(), "p15a line error") {
			t.Errorf("a line 1 error is drawn on the Terms body: %s", p.text())
		}
		if got := p15aErrorFields(p); len(got) != 1 || got[0] != "f-remarks-msg" {
			t.Errorf("inline errors at %q, want only f-remarks-msg", got)
		}
		if got := p15dCollapse(d.Find("#f-remarks-msg").Text()); got != "p15a remarks error" {
			t.Errorf("#f-remarks-msg = %q, want the remarks error", got)
		}
	})
}

// Fix 4 (UX §5.5): each radio's hint is the whole address on one line.
func TestPUX010_TermsAddressHint(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	po := purchasetest.NewOrder(t, w.env, purchasetest.StopAtDraft())
	page := w.get(t, w.env.Priya, p15dDoc(purchase.DocType, po.ID)+"/edit/terms")
	var got []string
	page.doc.Find(`label:has(input[name="vendor_address_id"]) .field__hint`).Each(func(_ int, s *goquery.Selection) {
		got = append(got, strings.TrimSpace(s.Text()))
	})
	want := []string{"Main Road, Mumbai 400001", "Main Road, Thane 400601", "Main Road, Delhi 110001"}
	if len(got) != len(want) {
		t.Fatalf("address hints = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("radio %d hint = %q, want %q", i+1, got[i], want[i])
		}
	}
}

// Fix 5 (UX §5.5): a stored address that is now disabled, while the vendor
// has other enabled addresses, checks the primary billing radio.
func TestPUX010_TermsDisabledStoredAddress(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE address SET is_billing = 1,
		is_primary_billing = CASE city WHEN 'Thane' THEN 1 ELSE 0 END
		WHERE party_id = (SELECT id FROM party WHERE code = 'SHREE')`); err != nil {
		t.Fatal(err)
	}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	terms := p15dDoc(purchase.DocType, po.ID) + "/edit/terms"
	delhi := p15aAddressID(t, w, "SHREE", "Delhi")
	if a := w.do(t, env.Priya, "POST", terms, url.Values{"version": {p15aVersion(t, w, po.ID)}, "vendor_address_id": {delhi}}); a.status != 303 {
		t.Fatalf("POST Delhi = %d: %s", a.status, a.text())
	}
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE address SET disabled = 1 WHERE id = ?`, delhi); err != nil {
		t.Fatal(err)
	}
	if got := p15aStoredAddress(t, w, po.ID); got != delhi {
		t.Fatalf("stored address = %q, want the now-disabled Delhi %s", got, delhi)
	}
	got := p15aRadios(w.get(t, env.Priya, terms))
	mumbai, thane := p15aAddressID(t, w, "SHREE", "Mumbai"), p15aAddressID(t, w, "SHREE", "Thane")
	if len(got) != 2 || got[0].value != mumbai || got[0].checked || got[1].value != thane || !got[1].checked {
		t.Errorf("radios with the stored Delhi disabled = %+v, want Mumbai, then Thane (primary billing) checked", got)
	}
}

// p15aHintOf is the hint text under field name, "" when none.
func p15aHintOf(p p15dPage, name string) string {
	return p15dCollapse(p.doc.Find("#f-" + name + "-hint").Text())
}

// Fix 6 (AD-13 ruling, UX §5.4 to §5.6): the field hints sit under their
// fields: Terms, Reverse charge, Remarks and Earlier PO no. on Terms; Rate
// and Already received on the line page; Project on Details for every user,
// restricted or not (plan P15a Builds overrides UX §5.4's "unrestricted
// only", AD-13).
func TestPUX010_FieldHints(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t, purchasetest.WithNoRules()), nil)
	env := w.env
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderHeader(map[string]string{"earlier_po_no": "P-17"}))
	path := p15dDoc(purchase.DocType, po.ID)

	terms := w.get(t, env.Priya, path+"/edit/terms")
	if terms.status != 200 {
		t.Fatalf("GET terms = %d", terms.status)
	}
	for name, want := range map[string]string{
		"terms": p15aHintTerms, "reverse_charge": p15aHintReverse, "remarks": p15aHintRemarks, "earlier_po_no": p15aHintEarlier,
	} {
		if got := p15aHintOf(terms, name); got != want {
			t.Errorf("Terms hint under %s = %q, want %q", name, got, want)
		}
	}

	line := w.get(t, env.Priya, path+"/lines/1")
	if line.status != 200 {
		t.Fatalf("GET line 1 = %d", line.status)
	}
	for name, want := range map[string]string{"price": p15aHintRate, "already_received": p15aHintReceived} {
		if got := p15aHintOf(line, name); got != want {
			t.Errorf("line page hint under %s = %q, want %q", name, got, want)
		}
	}

	arjun := p15aSave(t, env, env.Arjun, p15aHeader(t, w, "SHREE", "TECH-SITE", "PRJ-TECHPARK"))
	for _, c := range []struct {
		who  string
		page p15dPage
		want string
	}{
		{"Priya (PRJ-METRO only)", w.get(t, env.Priya, path+"/edit/details"), p15aHintProject},
		{"Arjun (unrestricted)", w.get(t, env.Arjun, p15dDoc(purchase.DocType, arjun)+"/edit/details"), p15aHintProject},
	} {
		if c.page.status != 200 {
			t.Fatalf("%s GET details = %d", c.who, c.page.status)
		}
		if c.page.doc.Find(`[name="project_id"]`).Length() == 0 {
			t.Fatalf("%s: Details has no project_id field", c.who)
		}
		if got := p15aHintOf(c.page, "project_id"); got != c.want {
			t.Errorf("%s: Project hint = %q, want %q", c.who, got, c.want)
		}
	}
}

// CD-302, CD-176 (AD-13 ruling): vendor_address_id is derived from the
// vendor. On a draft with no vendor, a Terms post of another party's existing
// address saves it as NULL; with a vendor, another party's address is refused
// at the field and the stored address is kept.
func TestPUX010_TermsAddressOfAnotherParty(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	bharat := p15aAddressID(t, w, "BHARAT", "Bengaluru")

	t.Run("no vendor: cleared", func(t *testing.T) {
		none := p15aSave(t, env, env.Priya, p15aHeader(t, w, "", "METRO-SITE", "PRJ-METRO"))
		a := w.do(t, env.Priya, "POST", p15dDoc(purchase.DocType, none)+"/edit/terms",
			url.Values{"version": {p15aVersion(t, w, none)}, "vendor_address_id": {bharat}})
		if a.status != 303 {
			t.Fatalf("POST BHARAT's address on a no-vendor draft = %d, want 303: %s", a.status, a.text())
		}
		if got := p15aStoredAddress(t, w, none); got != "" {
			t.Errorf("vendor_address_id on a no-vendor draft = %s (BHARAT's), want NULL", got)
		}
	})

	t.Run("vendor: refused", func(t *testing.T) {
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		before := p15aStoredAddress(t, w, po.ID)
		a := w.do(t, env.Priya, "POST", p15dDoc(purchase.DocType, po.ID)+"/edit/terms",
			url.Values{"version": {p15aVersion(t, w, po.ID)}, "vendor_address_id": {bharat}})
		if a.status != 422 {
			t.Fatalf("POST BHARAT's address on a SHREE draft = %d, want 422: %s", a.status, a.text())
		}
		const want = "That address belongs to another party. Pick one of this party's addresses."
		if got := p15dCollapse(a.doc.Find("#f-vendor_address_id-msg").Text()); got != want {
			t.Errorf("inline error at #f-vendor_address_id-msg = %q, want %q", got, want)
		}
		if got := p15aStoredAddress(t, w, po.ID); got != before {
			t.Errorf("a refused post changed vendor_address_id to %q, want %q kept", got, before)
		}
	})
}
