package purchaseweb

import (
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// p15aRadio is one address radio as the Terms step shows it.
type p15aRadio struct {
	value, label string
	checked      bool
}

// radios are the page's vendor_address_id radios: value, the option's text
// (label and hint) and whether it is checked.
func p15aRadios(p p15dPage) []p15aRadio {
	var out []p15aRadio
	p.doc.Find(`input[type="radio"][name="vendor_address_id"]`).Each(func(_ int, s *goquery.Selection) {
		_, checked := s.Attr("checked")
		out = append(out, p15aRadio{value: s.AttrOr("value", ""), label: p15dCollapse(s.Closest("label").Text()), checked: checked})
	})
	return out
}

// p15aVersion is PO id's stored version, as a form value.
func p15aVersion(t *testing.T, w *p15dWeb, id int64) string {
	t.Helper()
	var v int
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT version FROM purchase_order WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatalf("version of PO %d: %v", id, err)
	}
	return strconv.Itoa(v)
}

// p15aAddressID is party's address id in city.
func p15aAddressID(t *testing.T, w *p15dWeb, party, city string) string {
	t.Helper()
	var id int64
	if err := w.env.DB.SQL().QueryRowContext(t.Context(),
		`SELECT a.id FROM address a JOIN party p ON p.id = a.party_id WHERE p.code = ? AND a.city = ?`, party, city).Scan(&id); err != nil {
		t.Fatalf("address of %s in %s: %v", party, city, err)
	}
	return strconv.FormatInt(id, 10)
}

// PUX-010 (UX §5.5, spec §3.1, §15 item 11): with SHREE's three addresses
// and Thane (the second) its primary billing, Terms shows three radios
// `{city} · {GSTIN}` with the address as hint, Thane checked; a posted
// choice is saved by the generic post. With no vendor, `Choose a vendor on
// Details first.` and `Go to Details` replace them; a vendor with no address
// reads `{vendor} has no addresses. Add one in Parties, then come back.`
func TestPUX010_TermsAddressRadios(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	// Thane becomes SHREE's billing and primary billing address (masters only).
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE address SET is_billing = 1,
		is_primary_billing = CASE city WHEN 'Thane' THEN 1 ELSE 0 END
		WHERE party_id = (SELECT id FROM party WHERE code = 'SHREE')`); err != nil {
		t.Fatal(err)
	}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	terms := p15dDoc(purchase.DocType, po.ID) + "/edit/terms"
	page := w.get(t, env.Priya, terms)
	if page.status != 200 {
		t.Fatalf("Priya GET terms = %d: %s", page.status, page.text())
	}
	mumbai, thane, delhi := p15aAddressID(t, w, "SHREE", "Mumbai"), p15aAddressID(t, w, "SHREE", "Thane"), p15aAddressID(t, w, "SHREE", "Delhi")
	want := []p15aRadio{
		{mumbai, "Mumbai · 27AABCS1429B1ZU", false},
		{thane, "Thane · 27AABCS1429B2ZT", true},
		{delhi, "Delhi · 07AABCS1429B1ZW", false},
	}
	got := p15aRadios(page)
	if len(got) != len(want) {
		t.Fatalf("Terms radios = %+v, want %d", got, len(want))
	}
	for i, r := range got {
		if r.value != want[i].value || !strings.HasPrefix(r.label, want[i].label) || r.checked != want[i].checked {
			t.Errorf("radio %d = %+v, want %+v", i+1, r, want[i])
		}
	}
	for i, pin := range []string{"Mumbai 400001", "Thane 400601", "Delhi 110001"} {
		if i < len(got) && !strings.HasSuffix(got[i].label, ", "+pin) {
			t.Errorf("radio %d hint %q, want the address ending %q", i+1, got[i].label, pin)
		}
	}
	if !strings.Contains(page.text(), "Vendor address") || page.doc.Find(`[name="payment_terms_id"]`).Length() == 0 {
		t.Errorf("Terms lacks the Vendor address legend or Payment terms: %s", page.text())
	}

	// The generic post saves the chosen radio.
	a := w.do(t, env.Priya, "POST", terms, url.Values{"version": {p15aVersion(t, w, po.ID)}, "vendor_address_id": {delhi}})
	if a.status != 303 {
		t.Fatalf("POST terms = %d: %s", a.status, a.text())
	}
	var stored string
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT CAST(vendor_address_id AS TEXT) FROM purchase_order WHERE id = ?`, po.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != delhi {
		t.Errorf("vendor_address_id after the post = %s, want Delhi's %s", stored, delhi)
	}
	if got := p15aRadios(w.get(t, env.Priya, terms)); len(got) != 3 || !got[2].checked || got[1].checked {
		t.Errorf("after choosing Delhi the radios are %+v, want Delhi checked", got)
	}

	// No vendor.
	none := p15aSave(t, env, env.Priya, p15aHeader(t, w, "", "METRO-SITE", "PRJ-METRO"))
	page = w.get(t, env.Priya, p15dDoc(purchase.DocType, none)+"/edit/terms")
	if r := p15aRadios(page); len(r) != 0 || !strings.Contains(page.text(), "Choose a vendor on Details first.") {
		t.Errorf("no vendor: radios %+v, text %q; want the Details text instead", r, page.text())
	}
	link := page.doc.Find("main a").FilterFunction(func(_ int, s *goquery.Selection) bool { return p15dCollapse(s.Text()) == "Go to Details" })
	if href := link.AttrOr("href", ""); link.Length() != 1 || href != p15dDoc(purchase.DocType, none)+"/edit/details" {
		t.Errorf("no vendor: %d Go to Details links (href %q), want one to the Details step", link.Length(), href)
	}

	// A vendor with no address left: its only one disabled after the save (a
	// save with none is refused).
	bare := p15aSave(t, env, env.Priya, p15aHeader(t, w, "BHARAT", "METRO-SITE", "PRJ-METRO"))
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE address SET disabled = 1 WHERE party_id = (SELECT id FROM party WHERE code = 'BHARAT')`); err != nil {
		t.Fatal(err)
	}
	page = w.get(t, env.Priya, p15dDoc(purchase.DocType, bare)+"/edit/terms")
	if r := p15aRadios(page); len(r) != 0 || !strings.Contains(page.text(), "Bharat Tools has no addresses. Add one in Parties, then come back.") {
		t.Errorf("no address: radios %+v, text %q; want the no-addresses text", r, page.text())
	}
}

// PUX-010's redraw (UX §5.5): a 422 on Terms keeps the posted radio and
// values, and opens the Earlier paper order disclosure on its field's error;
// a GET with both earlier-PO fields blank has it closed, and open with a
// value.
func TestPUX010_TermsRedraw(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t, purchasetest.WithNoRules()), nil)
	env := w.env
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	terms := p15dDoc(purchase.DocType, po.ID) + "/edit/terms"
	disclosure := func(p p15dPage) (n int, open bool) {
		d := p.doc.Find("details").FilterFunction(func(_ int, s *goquery.Selection) bool {
			return p15dCollapse(s.Find("summary").Text()) == "Earlier paper order"
		})
		_, open = d.Attr("open")
		return d.Length(), open
	}
	page := w.get(t, env.Priya, terms)
	if n, open := disclosure(page); n != 1 || open {
		t.Errorf("GET terms: %d Earlier paper order disclosures, open %v; want one, closed", n, open)
	}
	if page.doc.Find(`details [name="earlier_po_no"]`).Length() != 1 || page.doc.Find(`details [name="earlier_po_date"]`).Length() != 1 {
		t.Error("the earlier-PO fields are not inside the disclosure")
	}

	thane := p15aAddressID(t, w, "SHREE", "Thane")
	a := w.do(t, env.Priya, "POST", terms, url.Values{
		"version": {p15aVersion(t, w, po.ID)}, "vendor_address_id": {thane}, "vendor_quote_ref": {"SST/Q/778"}, "earlier_po_date": {"31-02-2026"},
	})
	if a.status != 422 {
		t.Fatalf("POST a bad earlier PO date = %d, want 422: %s", a.status, a.text())
	}
	if n, open := disclosure(a); n != 1 || !open {
		t.Errorf("422: disclosure %d, open %v; want it open on its error", n, open)
	}
	if r := p15aRadios(a); len(r) != 3 || !r[1].checked {
		t.Errorf("422: radios %+v, want the posted Thane checked", r)
	}
	if v := a.doc.Find(`[name="vendor_quote_ref"]`).AttrOr("value", ""); v != "SST/Q/778" {
		t.Errorf("422: vendor quote ref = %q, want the posted value", v)
	}

	if a := w.do(t, env.Priya, "POST", terms, url.Values{"version": {p15aVersion(t, w, po.ID)}, "earlier_po_no": {"P-17"}}); a.status != 303 {
		t.Fatalf("POST earlier PO no = %d: %s", a.status, a.text())
	}
	if n, open := disclosure(w.get(t, env.Priya, terms)); n != 1 || !open {
		t.Errorf("with Earlier PO no. set: disclosure %d, open %v; want it open", n, open)
	}
}

// CD-258 Q7 (a), UX §5.6, CD-327: the earlier-PO fields show while
// purchase.Order has no approval rule, or when either has a value; Already
// received only when Earlier PO no. is set; the module-set fields never. Each
// row is a world with or without rules and a draft with the values given,
// checked through the Terms step and the line page. A date without a number
// can't be saved (spec §4), so TestPO_OrderHideNilViewAndErrors asks Hide
// with it directly.
func TestPO_OrderHideEarlierFields(t *testing.T) {
	for _, c := range []struct {
		name          string
		rules         bool
		no, date      string
		earlier, recd bool // Earlier PO fields shown, Already received shown
	}{
		{"rules, blank", true, "", "", false, false},
		{"rules, number", true, "P-17", "", true, true},
		{"rules, number and date", true, "P-17", "15-09-2026", true, true},
		{"no rules, blank", false, "", "", true, false},
		{"no rules, number", false, "P-17", "", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			var opts []purchasetest.EnvOpt
			if !c.rules {
				opts = append(opts, purchasetest.WithNoRules())
			}
			env := purchasetest.New(t, opts...)
			w := newP15aWeb(t, env, nil)
			h := map[string]string{}
			if c.no != "" {
				h["earlier_po_no"] = c.no
			}
			if c.date != "" {
				h["earlier_po_date"] = c.date
			}
			po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderHeader(h))
			terms := w.get(t, env.Priya, p15dDoc(purchase.DocType, po.ID)+"/edit/terms")
			for _, f := range []string{"earlier_po_no", "earlier_po_date"} {
				if got := terms.doc.Find(`[name="` + f + `"]`).Length(); (got == 1) != c.earlier {
					t.Errorf("Terms has %d %s inputs, want shown %v", got, f, c.earlier)
				}
			}
			line := w.get(t, env.Priya, p15dDoc(purchase.DocType, po.ID)+"/lines/1")
			if line.status != 200 {
				t.Fatalf("GET line 1 = %d: %s", line.status, line.text())
			}
			if got := line.doc.Find(`[name="already_received"]`).Length(); (got == 1) != c.recd {
				t.Errorf("line page has %d already_received inputs, want shown %v", got, c.recd)
			}
			if n := line.doc.Find(`[name="request_line_id"]`).Length(); n != 0 {
				t.Errorf("line page has %d request_line_id inputs, want none", n)
			}
		})
	}
}

// CD-327 (1), plan P15a: Hide with a nil View (the list and search pages)
// never panics; with rules the earlier-PO fields hide, without them they
// show; Already received and the module-set fields hide; other fields show.
// HasRules is read once per request however many fields are asked, and a
// failed read shows the fields and is logged.
func TestPO_OrderHideNilViewAndErrors(t *testing.T) {
	env := purchasetest.New(t)
	m := Modules(Deps{Approvals: env.Approvals})[purchase.DocType]
	dt, _ := doctype.Lookup(purchase.DocType)
	ctx := func(r *p15aReader) docscreen.Ctx {
		return docscreen.Ctx{Req: httptest.NewRequest("GET", "/doc/"+purchase.DocType, nil), Principal: env.Priya, DT: dt, Deps: docscreen.Deps{DB: r}}
	}
	hiddenOf := func(c docscreen.Ctx) []string {
		var out []string
		for _, f := range dt.Fields {
			if m.Hide(c, f) {
				out = append(out, f.Name)
			}
		}
		return out
	}
	always := []string{"copied_from_no", "copied_from_id", "copy_notes", "already_received", "request_line_id"}
	earlier := []string{"earlier_po_no", "earlier_po_date"}

	r := &p15aReader{db: env.DB}
	got := hiddenOf(ctx(r))
	want := append(slices.Clone(always), earlier...)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("Hide with rules and no View hides %q, want %q", got, want)
	}
	if n := r.reads.Load(); n != 1 {
		t.Errorf("one request read HasRules %d times, want once", n)
	}

	logs := p15aLogs(t)
	bad := &p15aReader{db: env.DB, err: errP15a}
	got = hiddenOf(ctx(bad))
	want = slices.Clone(always)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("Hide with HasRules failing hides %q, want only %q (the fields show)", got, want)
	}
	if !strings.Contains(logs(), errP15a.Error()) || bad.reads.Load() != 1 {
		t.Errorf("a failed HasRules read was read %d times and logged %q; want once, with the error", bad.reads.Load(), logs())
	}

	// With rules, a stored date alone shows both earlier-PO fields but not
	// Already received; a stored number shows all three.
	for _, c := range []struct {
		inputs map[string]string
		want   []string
	}{
		{map[string]string{"earlier_po_date": "2026-09-15"}, always},
		{map[string]string{"earlier_po_no": "P-17"}, []string{"copied_from_no", "copied_from_id", "copy_notes", "request_line_id"}},
	} {
		c0 := ctx(&p15aReader{db: env.DB})
		c0.View = &docscreen.DocView{Inputs: c.inputs}
		got := hiddenOf(c0)
		want := slices.Clone(c.want)
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("Hide with rules and stored %v hides %q, want %q", c.inputs, got, want)
		}
	}

	noRules := purchasetest.New(t, purchasetest.WithNoRules())
	m = Modules(Deps{Approvals: noRules.Approvals})[purchase.DocType]
	got = hiddenOf(ctx(&p15aReader{db: noRules.DB}))
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("Hide with no rules and no View hides %q, want %q", got, want)
	}
}

// Hooks spec §4.3 (CD-256 (1)): the Terms body leaves out any field the
// screen's Hide hides, the address radios and a plain field alike, as the
// generic body does (the redraw sweep's probe wraps Hide this way).
func TestPUX010_TermsHonourHide(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	for _, field := range []string{"vendor_address_id", "vendor_quote_ref"} {
		page := p15aHiding(t, env, field).get(t, env.Priya, p15dDoc(purchase.DocType, po.ID)+"/edit/terms")
		if page.status != 200 {
			t.Fatalf("GET terms hiding %s = %d", field, page.status)
		}
		if n := page.doc.Find(`[name="` + field + `"]`).Length(); n != 0 {
			t.Errorf("Terms hiding %s draws %d of its inputs, want none", field, n)
		}
		if n := page.doc.Find(`[name="payment_terms_id"]`).Length(); n == 0 {
			t.Errorf("Terms hiding %s lost Payment terms", field)
		}
	}
}
