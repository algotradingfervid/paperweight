package grnweb_test

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN S1, step 1 Order (UX doc §5.2; GRN spec §8, §11).
// Every case runs as a non-admin user on the real auth.Service (CD-213).

// GUX-001 (GRN-001): Suresh, restricted to his sites, with exactly one
// approved PO with pending sees one radio, already checked, with the PO,
// vendor, site and pending lines, the restricted legend hint, the step
// labels and today's receipt date. A PO of one line says "1 of 1 line".
func TestGRNWeb_ChoosePOOneVisible(t *testing.T) {
	w := newS1World(t)
	po := w.po("SHREE", "1200", "50")
	r := w.get(w.Suresh, s1New)
	if r.status != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", s1New, r.status)
	}
	want := []s1Radio{{strconv.FormatInt(po.ID, 10), "PO/26-27/00001 · Shree Steel Traders METRO-SITE 2 of 2 lines pending · ordered 24-10-2026", true}}
	if got := r.radios("purchase_order_id"); !slices.Equal(got, want) {
		t.Errorf("radios = %+v, want %+v", got, want)
	}
	if got := r.doc.Find("#f-purchase_order_id-hint").Text(); got != "Orders with items still to receive at your sites." {
		t.Errorf("legend hint = %q, want the restricted text", got)
	}
	if got := r.doc.Find(`input[name="receipt_date"]`).AttrOr("value", ""); got != s1Today {
		t.Errorf("receipt date = %q, want %s", got, s1Today)
	}
	if got := strings.TrimSpace(r.doc.Find("h2#step-title").Text()); got != "Order" {
		t.Errorf("h1 = %q, want Order", got)
	}
	if got := r.doc.Find("nav.stepper .stepper__label").Text(); got != "OrderDeliveryQuantitiesCheck" {
		t.Errorf("stepper labels = %q", got)
	}
	for _, name := range []string{"site_id", "project_id"} {
		if r.doc.Find(`[name="`+name+`"]`).Length() != 0 {
			t.Errorf("step 1 renders %s, which Hide hides", name)
		}
	}

	w.po("BHARAT", "5")
	got := w.get(w.Suresh, s1New).radios("purchase_order_id")
	if len(got) != 2 || got[0].text != "PO/26-27/00002 · Bharat Tools METRO-SITE 1 of 1 line pending · ordered 24-10-2026" || got[0].checked || got[1].checked {
		t.Errorf("two POs: radios = %+v, want Bharat's first with 1 of 1 line, neither checked", got)
	}
}

// GUX-001's legend hint for a Stores user with no site restriction.
func TestGRNWeb_ChoosePOHintUnrestricted(t *testing.T) {
	w := newS1World(t)
	w.po("SHREE", "10")
	id, _, err := w.Auth.CreateUser(t.Context(), w.Admin, auth.NewUser{ //nolint:gosec // G101: a test user's password
		Email: "gate3@vardhan.test", Name: "Gate Three", Password: "Receive the trucks 43", Roles: []string{"Stores"},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := w.Auth.LoadPrincipal(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got := w.get(p, s1New).doc.Find("#f-purchase_order_id-hint").Text(); got != "Orders with items still to receive." {
		t.Errorf("legend hint = %q, want the unrestricted text", got)
	}
}

// GUX-002 (GRN-001): up to 6 POs are radios; 7 are a select named
// purchase_order_id, a blank first option (no guessed default), then each
// PO in vendor-name then PO-number order, labelled with PO, vendor, site and
// pending lines. Vendors cycle, so that order differs from id order.
func TestGRNWeb_ChoosePOSelect(t *testing.T) {
	w := newS1World(t)
	vendors := []string{"SHREE", "BHARAT", "RAJKOT"}
	for i := range 6 {
		w.po(vendors[i%3], "10")
	}
	r := w.get(w.Suresh, s1New)
	if got := r.radios("purchase_order_id"); len(got) != 6 || r.doc.Find("select").Length() != 0 {
		t.Fatalf("6 POs: %d radios and %d selects, want 6 radios", len(got), r.doc.Find("select").Length())
	}

	w.po("SHREE", "10")
	r = w.get(w.Suresh, s1New)
	if n := len(r.radios("purchase_order_id")); n != 0 {
		t.Errorf("7 POs: %d radios, want none", n)
	}
	var got []string
	r.doc.Find(`select[name="purchase_order_id"] option`).Each(func(_ int, s *goquery.Selection) {
		got = append(got, s.Text())
	})
	want := []string{""}
	for _, v := range []struct{ name, nos string }{
		{"Bharat Tools", "2 5"}, {"Rajkot Castings", "3 6"}, {"Shree Steel Traders", "1 4 7"},
	} {
		for _, n := range strings.Fields(v.nos) {
			want = append(want, "PO/26-27/0000"+n+" · "+v.name+" · METRO-SITE · 1 of 1 line pending")
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("options =\n%q\nwant\n%q", got, want)
	}
}

// GUX-003 (GRN-001, GRN-014): a fully received PO and a closed PO aren't
// listed; Show fully received orders (?all=1, keeping ?po=) adds the fully
// received one with its hint, never the closed one, with the widened hint,
// the link back, and ?all carried in the form.
func TestGRNWeb_ChoosePOWidened(t *testing.T) {
	w := newS1World(t)
	pending := w.po("SHREE", "10")
	full := w.po("BHARAT", "5")
	w.receiveAll(full)
	closed := w.po("RAJKOT", "7")
	w.ClosePO(t, w.Priya, closed.ID, "Vendor stopped supplying")

	r := w.get(w.Suresh, s1New)
	if got := r.radios("purchase_order_id"); len(got) != 1 || got[0].value != strconv.FormatInt(pending.ID, 10) {
		t.Errorf("default list = %+v, want only the PO with pending", got)
	}
	if link := r.doc.Find(`a:contains("Show fully received orders")`); link.AttrOr("href", "") != s1New+"?all=1" {
		t.Errorf("widen link href = %q, want %s?all=1", link.AttrOr("href", ""), s1New)
	}
	if r.doc.Find(`input[name="all"]`).Length() != 0 {
		t.Errorf("the default list carries all")
	}

	r = w.get(w.Suresh, s1New+"?all=1")
	want := []s1Radio{
		{strconv.FormatInt(full.ID, 10), full.Number + " · Bharat Tools METRO-SITE Fully received · ordered 24-10-2026", false},
		{strconv.FormatInt(pending.ID, 10), pending.Number + " · Shree Steel Traders METRO-SITE 1 of 1 line pending · ordered 24-10-2026", false},
	}
	if got := r.radios("purchase_order_id"); !slices.Equal(got, want) {
		t.Errorf("widened list = %+v, want %+v", got, want)
	}
	if got := r.doc.Find("#f-purchase_order_id-hint").Text(); got != "Approved orders at your sites, including fully received ones." {
		t.Errorf("widened hint = %q", got)
	}
	if link := r.doc.Find(`a:contains("Show only orders with items pending")`); link.AttrOr("href", "") != s1New {
		t.Errorf("narrow link href = %q, want %s", link.AttrOr("href", ""), s1New)
	}
	if r.doc.Find(`input[type="hidden"][name="all"][value="1"]`).Length() != 1 {
		t.Errorf("the widened list doesn't carry all=1 in the form")
	}
	if strings.Contains(r.body, closed.Number) {
		t.Errorf("the closed PO %s is listed", closed.Number)
	}

	po := strconv.FormatInt(pending.ID, 10)
	r = w.get(w.Suresh, s1New+"?po="+po+"&all=1")
	if link := r.doc.Find(`a:contains("Show only orders with items pending")`); link.AttrOr("href", "") != s1New+"?po="+po {
		t.Errorf("narrow link with ?po= = %q, want it kept", link.AttrOr("href", ""))
	}
	if link := w.get(w.Suresh, s1New+"?po="+po).doc.Find(`a:contains("Show fully received orders")`); link.AttrOr("href", "") != s1New+"?all=1&po="+po {
		t.Errorf("widen link with ?po= = %q, want it kept", link.AttrOr("href", ""))
	}
}

// GUX-004 (spec §4): with no PO to show, step 1 says so and asks the buyer,
// with Show fully received orders only when one exists; Save and continue
// is a 422 "Choose a purchase order." with nothing written.
func TestGRNWeb_ChoosePOEmpty(t *testing.T) {
	w := newS1World(t)
	r := w.get(w.Suresh, s1New)
	for _, s := range []string{"No approved orders are waiting for delivery.", "Ask the buyer if an order should be here."} {
		if !strings.Contains(r.text(), s) {
			t.Errorf("empty step 1 has no %q", s)
		}
	}
	if r.doc.Find(`a:contains("Show fully received orders")`).Length() != 0 {
		t.Errorf("empty step 1 offers fully received orders with none")
	}
	if r.doc.Find(`a:contains("Show only orders")`).Length() != 0 || w.get(w.Suresh, s1New+"?all=1").doc.Find(`a:contains("Show")`).Length() != 0 {
		t.Errorf("an empty list shows a toggle link")
	}

	before := w.RowCounts(t)
	r = w.post(w.Suresh, s1New, url.Values{"purchase_order_id": {""}, "receipt_date": {s1Today}})
	if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.errorSummary(), s1MsgChoosePO) {
		t.Errorf("blank PO = %d %q, want 422 %q", r.status, r.errorSummary(), s1MsgChoosePO)
	}
	// One PO listed: the redraw of a blank post leaves it unchosen, so the
	// user's blank isn't replaced by the one-visible default (spec §11).
	if after := w.RowCounts(t); after != before {
		t.Errorf("rows %+v after the blank post, want %+v", after, before)
	}
	one := w.po("SHREE", "10")
	r = w.post(w.Suresh, s1New, url.Values{"purchase_order_id": {""}, "receipt_date": {s1Today}})
	if got := r.radios("purchase_order_id"); r.status != http.StatusUnprocessableEntity || len(got) != 1 ||
		got[0].value != strconv.FormatInt(one.ID, 10) || got[0].checked {
		t.Errorf("blank post with one PO = %d radios %+v, want 422 and its radio unchecked", r.status, got)
	}
	before = w.RowCounts(t)
	if r = w.post(w.Suresh, s1New, url.Values{"purchase_order_id": {"abc"}}); r.status != http.StatusUnprocessableEntity || !strings.Contains(r.errorSummary(), s1MsgChoosePO) {
		t.Errorf("PO abc = %d %q, want 422 %q", r.status, r.errorSummary(), s1MsgChoosePO)
	}
	if after := w.RowCounts(t); after != before {
		t.Errorf("rows %+v after the refusals, want %+v", after, before)
	}

	w.receiveAll(one)
	r = w.get(w.Suresh, s1New)
	if link := r.doc.Find(`.empty-state a`); link.Text() != "Show fully received orders" || link.AttrOr("href", "") != s1New+"?all=1" {
		t.Errorf("empty step 1 with a fully received PO: link %q %q, want Show fully received orders", link.Text(), link.AttrOr("href", ""))
	}
}

// GUX-005 (GRN-002, GRN-029; CD-240): Suresh posting a TECH-SITE PO gets a
// 403 with CD-240's text, no draft, and nothing of that PO in the answer.
func TestGRNWeb_ChoosePOForeign(t *testing.T) {
	w := newS1World(t)
	tech := w.techPO()
	before := w.RowCounts(t)
	r := w.post(w.Suresh, s1New, url.Values{"purchase_order_id": {strconv.FormatInt(tech.ID, 10)}, "receipt_date": {s1Today}})
	if r.status != http.StatusForbidden || !strings.Contains(r.text(), s1MsgNotAvailable) {
		t.Errorf("TECH-SITE PO = %d %q, want 403 %q", r.status, r.text(), s1MsgNotAvailable)
	}
	if strings.Contains(r.body, tech.Number) || strings.Contains(r.body, "Shree Steel") {
		t.Errorf("the refusal names the TECH-SITE PO %s or its vendor", tech.Number)
	}
	if after := w.RowCounts(t); after != before {
		t.Errorf("rows %+v after the refusal, want %+v", after, before)
	}
}

// Plan G6 Do 3, spec §11 step 1: Save and continue creates the draft from
// DraftFromPO, one line per PO line with pending (received = pending), with
// the typed receipt date, and goes on to Delivery. A date NUM can't
// read is a 422 with nothing written.
func TestGRNWeb_ChoosePOCreates(t *testing.T) {
	w := newS1World(t)
	po := w.po("SHREE", "1200", "50")
	before := w.RowCounts(t)
	r := w.post(w.Suresh, s1New, url.Values{"purchase_order_id": {strconv.FormatInt(po.ID, 10)}, "receipt_date": {"31-02-2026"}})
	if r.status != http.StatusUnprocessableEntity || r.errorSummary() == "" {
		t.Errorf("receipt date 31-02-2026 = %d %q, want 422 with NUM's text", r.status, r.errorSummary())
	}
	if after := w.RowCounts(t); after != before {
		t.Errorf("rows %+v after the parse error, want %+v", after, before)
	}
	if got := r.doc.Find(`input[name="receipt_date"]`).AttrOr("value", ""); got != "31-02-2026" {
		t.Errorf("redraw receipt date = %q, want the typed 31-02-2026", got)
	}
	if got := r.radios("purchase_order_id"); len(got) != 1 || !got[0].checked {
		t.Errorf("redraw radios = %+v, want the posted PO checked", got)
	}

	r = w.post(w.Suresh, s1New, url.Values{"purchase_order_id": {strconv.FormatInt(po.ID, 10)}, "receipt_date": {"23-10-2026"}})
	if r.status != http.StatusSeeOther || !strings.HasSuffix(r.location, "/edit/delivery") {
		t.Fatalf("create = %d %q, want 303 to Delivery", r.status, r.location)
	}
	g := w.Load(t, goodsreceipt.DocType, w.created(r))
	if got := g.Header["receipt_date"].(types.Date).ISO(); got != "2026-10-23" {
		t.Errorf("receipt_date = %s, want the typed 2026-10-23", got)
	}
	var got []string
	for _, l := range g.Lines {
		got = append(got, l.Values["received_qty"].(types.Qty).Plain())
	}
	if want := []string{"1200", "50"}; !slices.Equal(got, want) {
		t.Errorf("received = %v, want the pending %v", got, want)
	}
}

// CD-320 (DOC-017 on the header; UX doc §3.4): a PO closed after step 1
// loaded is refused on Purchase order with DOC's text and no "Line {n}: "
// form, nothing written. The redraw keeps the posted ?all and date, and
// the closed PO, no longer listed, isn't drawn.
func TestGRNWeb_ChoosePOClosedRace(t *testing.T) {
	w := newS1World(t)
	po := w.po("SHREE", "10")
	w.receiveAll(w.po("BHARAT", "5"))
	if w.get(w.Suresh, s1New).status != http.StatusOK {
		t.Fatal("step 1 didn't load")
	}
	w.ClosePO(t, w.Priya, po.ID, "Vendor stopped supplying")
	before := w.RowCounts(t)
	r := w.post(w.Suresh, s1New, url.Values{"purchase_order_id": {strconv.FormatInt(po.ID, 10)}, "receipt_date": {"22-10-2026"}, "all": {"1"}})
	want := po.Number + " is closed. Remove the line or pick another purchase order."
	if r.status != http.StatusUnprocessableEntity || r.errorSummary() != want {
		t.Errorf("closed PO = %d %q, want 422 %q only", r.status, r.errorSummary(), want)
	}
	if after := w.RowCounts(t); after != before {
		t.Errorf("rows %+v after the refusal, want %+v", after, before)
	}
	if got := r.doc.Find("#f-purchase_order_id-msg").Text(); got != want {
		t.Errorf("message at the radios = %q, want %q", got, want)
	}
	if r.doc.Find(`input[type="hidden"][name="all"]`).Length() != 1 || r.doc.Find(`input[name="receipt_date"]`).AttrOr("value", "") != "22-10-2026" {
		t.Errorf("the redraw lost ?all or the typed date")
	}
	if got := r.radios("purchase_order_id"); slices.ContainsFunc(got, func(o s1Radio) bool { return o.value == strconv.FormatInt(po.ID, 10) }) {
		t.Errorf("the closed PO is still drawn as a choice: %+v", got)
	}
}

// GUX-007 (GRN-004; spec §3.1): once the draft has lines its PO is fixed:
// step 1 shows it as text from the GRN's snapshot with the hint, Delete
// draft for its creator only (Stores deletes own), an editable receipt
// date and no radios. Saving changes the date; a crafted PO change is
// refused by Validate and nothing changes.
func TestGRNWeb_ChoosePOFixed(t *testing.T) {
	w := newS1World(t)
	po := w.po("SHREE", "1200", "50")
	other := w.po("BHARAT", "5")
	id := w.draft(po)
	path := stepPath(id, "details")

	r := w.get(w.Suresh, path)
	if n := len(r.radios("purchase_order_id")); n != 0 || r.doc.Find("select").Length() != 0 {
		t.Errorf("fixed PO: %d radios, want none", n)
	}
	for _, s := range []string{"Purchase order: PO/26-27/00001 · Shree Steel Traders", "Site: METRO-SITE"} {
		if !slices.Contains(r.summaryRows(), s) {
			t.Errorf("fixed PO step has no row %q: %q", s, r.summaryRows())
		}
	}
	if !strings.Contains(r.text(), "Delete this draft to receive against another order.") {
		t.Errorf("fixed PO step has no hint")
	}
	del := "/doc/" + goodsreceipt.DocType + "/" + strconv.FormatInt(id, 10) + "/delete"
	if got := r.doc.Find(`a:contains("Delete draft")`).AttrOr("href", ""); got != del {
		t.Errorf("Delete draft href = %q, want %s", got, del)
	}
	if got := r.doc.Find(`input[name="receipt_date"]`).AttrOr("value", ""); got != s1Today {
		t.Errorf("receipt date = %q, want %s", got, s1Today)
	}
	if w.get(w.SecondStores(t), path).doc.Find(`a:contains("Delete draft")`).Length() != 0 {
		t.Errorf("another Stores user, who deletes only own drafts, is offered Delete draft")
	}

	version := strconv.Itoa(w.Load(t, goodsreceipt.DocType, id).Version)
	r = w.post(w.Suresh, path, url.Values{"version": {version}, "receipt_date": {"23-10-2026"}, "purchase_order_id": {strconv.FormatInt(po.ID, 10)}})
	if r.status != http.StatusSeeOther {
		t.Fatalf("save the date = %d %q", r.status, r.errorSummary())
	}
	g := w.Load(t, goodsreceipt.DocType, id)
	if got := g.Header["receipt_date"].(types.Date).ISO(); got != "2026-10-23" || len(g.Lines) != 2 {
		t.Errorf("after the save: receipt_date %s with %d lines, want 2026-10-23 and the 2 lines kept", got, len(g.Lines))
	}

	r = w.post(w.Suresh, path, url.Values{"version": {strconv.Itoa(g.Version)}, "receipt_date": {"23-10-2026"},
		"purchase_order_id": {strconv.FormatInt(other.ID, 10)}})
	if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.errorSummary(), "Delete this draft to receive against another order.") {
		t.Errorf("crafted PO change = %d %q, want 422 Delete this draft…", r.status, r.errorSummary())
	}
	if after := w.Load(t, goodsreceipt.DocType, id); after.Version != g.Version || after.Header["purchase_order_id"] != po.ID {
		t.Errorf("the refused change wrote: version %d PO %v", after.Version, after.Header["purchase_order_id"])
	}
	if r = w.post(w.Suresh, path, url.Values{"version": {strconv.Itoa(g.Version)}, "receipt_date": {"x"}}); r.status != http.StatusUnprocessableEntity {
		t.Errorf("receipt date x on the fixed step = %d, want 422", r.status)
	}
}

// UX doc §5.2, existing draft without lines (made against a fully received
// PO): the radios again with its PO checked, the list widened to show it;
// choosing a PO with pending replaces the PO and adds its pending lines.
func TestGRNWeb_ChoosePODraftWithoutLines(t *testing.T) {
	w := newS1World(t)
	full := w.po("SHREE", "10")
	w.receiveAll(full)
	next := w.po("BHARAT", "5", "6")
	id := w.draft(full)
	path := stepPath(id, "details")
	if n := len(w.Load(t, goodsreceipt.DocType, id).Lines); n != 0 {
		t.Fatalf("draft from a fully received PO has %d lines", n)
	}

	r := w.get(w.Suresh, path)
	want := []s1Radio{
		{strconv.FormatInt(next.ID, 10), next.Number + " · Bharat Tools METRO-SITE 2 of 2 lines pending · ordered 24-10-2026", false},
		{strconv.FormatInt(full.ID, 10), full.Number + " · Shree Steel Traders METRO-SITE Fully received · ordered 24-10-2026", true},
	}
	if got := r.radios("purchase_order_id"); !slices.Equal(got, want) {
		t.Errorf("radios = %+v, want %+v", got, want)
	}

	version := strconv.Itoa(w.Load(t, goodsreceipt.DocType, id).Version)
	r = w.post(w.Suresh, path, url.Values{"version": {version}, "purchase_order_id": {strconv.FormatInt(next.ID, 10)}, "receipt_date": {s1Today}})
	if r.status != http.StatusSeeOther {
		t.Fatalf("choose another PO = %d %q", r.status, r.errorSummary())
	}
	g := w.Load(t, goodsreceipt.DocType, id)
	if g.Header["purchase_order_id"] != next.ID || len(g.Lines) != 2 {
		t.Errorf("after choosing: PO %v with %d lines, want %d with 2", g.Header["purchase_order_id"], len(g.Lines), next.ID)
	}
}
