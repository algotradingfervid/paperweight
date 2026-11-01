package grnweb_test

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	grnweb "github.com/vardhan/internaltools/internal/modules/goodsreceipt/web"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// MOD-GRN S4 (UX doc §5.10 to §5.12; GUX-033, GUX-034; CD-340): the reports
// entry and the Bills to enter and Received items pages with their filter
// pages, on a bare router with the principal in the context (internal/app's
// guard asks read on the GRN first; TestGRNWeb_Guards). The data is
// grntest.ReportWorld on the real auth.Service.

const (
	s4Bills = "/reports/grn/bills"
	s4Items = "/reports/grn/items"
	// s4NoRead is AUTH's read refusal for the Goods Receipt (CD-142).
	s4NoRead = "You don't have permission to read Goods Receipt. Ask your administrator for a role that allows it."
)

// s4Get sends GET path as p to the module routes with the real AUTH.
func s4Get(t *testing.T, env *grntest.Env, p auth.Principal, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	grnweb.Routes(grnweb.Deps{Service: env.Service, Auth: env.Auth})(r)
	req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), p), http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

type s4Page struct {
	t   *testing.T
	doc *goquery.Document
}

// s4Open is GET path as p, which must answer 200.
func s4Open(t *testing.T, env *grntest.Env, p auth.Principal, path string) s4Page {
	t.Helper()
	rec := s4Get(t, env, p, path)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s as %s = %d, want 200: %q", path, p.Name, rec.Code, rec.Body.String())
	}
	doc, err := goquery.NewDocumentFromReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	return s4Page{t, doc}
}

func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

// cards is each list card's visible text.
func (p s4Page) cards() []string {
	var out []string
	p.doc.Find(".datalist-rows > li").Each(func(_ int, s *goquery.Selection) {
		s = s.Clone()
		s.Find(".visually-hidden").Remove()
		out = append(out, squash(s.Text()))
	})
	return out
}

// titles is the cards' GRN numbers, in order.
func (p s4Page) titles() []string {
	var out []string
	p.doc.Find(".datalist-rows > li .datalist-title").Each(func(_ int, s *goquery.Selection) {
		out = append(out, strings.TrimSpace(s.Find("dd").Text()))
	})
	return out
}

// card is the one card whose title is number.
func (p s4Page) card(number string) string {
	p.t.Helper()
	i := slices.Index(p.titles(), number)
	if i < 0 {
		p.t.Fatalf("no card %s in %q", number, p.titles())
	}
	return p.cards()[i]
}

func (p s4Page) text(sel string) string { return squash(p.doc.Find(sel).Text()) }

func (p s4Page) href(sel string) string { return p.doc.Find(sel).AttrOr("href", "") }

// chips is the filter chips' text.
func (p s4Page) chips() []string {
	var out []string
	p.doc.Find(".filterbar-chips a.chip").Each(func(_ int, s *goquery.Selection) {
		out = append(out, squash(strings.TrimSuffix(strings.TrimSpace(s.Text()), "Remove filter")))
	})
	return out
}

func wantOn(t *testing.T, what, got string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if !strings.Contains(got, s) {
			t.Errorf("%s %q: no %q", what, got, s)
		}
	}
}

func wantOff(t *testing.T, what, got string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if s != "" && strings.Contains(got, s) {
			t.Errorf("%s %q: has %q", what, got, s)
		}
	}
}

func s4ID(id int64) string { return strconv.FormatInt(id, 10) }

// UX doc §5.10 (CD-282 Q8): the entry page has the title and a card for
// each report with its Open link and line.
func TestS4_ReportsEntry(t *testing.T) {
	env := grntest.New(t)
	p := s4Open(t, env, env.Suresh, "/reports/grn")
	if got := p.text("h1"); got != "Goods receipt reports" {
		t.Errorf("h1 = %q, want Goods receipt reports", got)
	}
	main := p.text("main")
	wantOn(t, "entry", main, "Bills to enter", "Receipts and their vendor bill details, for keying your accounts.",
		"Received items", "Every received line, with rejects and reasons.")
	var links []string
	p.doc.Find(`main .card-head a`).Each(func(_ int, s *goquery.Selection) {
		s = s.Clone()
		s.Find(".visually-hidden").Remove()
		if strings.TrimSpace(s.Text()) == "Open" {
			links = append(links, s.AttrOr("href", ""))
		}
	})
	if want := []string{s4Bills, s4Items}; !slices.Equal(links, want) {
		t.Errorf("Open links = %q, want %q", links, want)
	}
}

// GUX-033, UX doc §5.11: Priya's Bills to enter lists her approved and
// closed GRNs oldest first (never TECH-SITE's GT, nor a draft, pending,
// rejected or cancelled one), each linking to its GRN, with the snapshot
// texts: accepted value, vendor, date · PO · challan, the bill details or
// "No bill details yet", photos and GSTIN; the count and Export (she holds
// export) to the CSV.
func TestS4_BillsPage(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	p := s4Open(t, env, env.Priya, s4Bills)
	if got := p.text("h1"); got != "Bills to enter" {
		t.Errorf("h1 = %q", got)
	}
	if want := []string{w.G1.Number, w.G2.Number, w.GC.Number, w.G3.Number}; !slices.Equal(p.titles(), want) {
		t.Errorf("rows = %q, want %q (oldest receipt first)", p.titles(), want)
	}
	wantOn(t, "G1", p.card(w.G1.Number), "₹29,175.00", "Shree Steel Traders", "24-10-2026 · "+w.RefPO.Number+" · DC-1042",
		grntest.ReportInvoice+" · 24-10-2026 · ₹34,426.50", "2 photos", "27AABCS1429B1ZU")
	wantOff(t, "G1", p.card(w.G1.Number), "No bill details yet")
	wantOn(t, "G2", p.card(w.G2.Number), "₹1,250.00", grntest.ReportVendor, w.BharatPO.Number, "No bill details yet", "No photos", "29AABCT1332L1ZA")
	wantOn(t, "G3", p.card(w.G3.Number), "01-11-2026 · "+w.RefPO.Number)
	if got := p.href(`.datalist-rows a[href="/doc/` + goodsreceipt.DocType + `/` + s4ID(w.G1.ID) + `"]`); got == "" {
		t.Errorf("G1's card doesn't link to its GRN")
	}
	wantOn(t, "bar", p.text("form.filterbar"), "4 receipts")
	if got := p.href(`form.filterbar a[href*="export.csv"]`); got != s4Bills+"/export.csv" {
		t.Errorf("Export = %q, want %s/export.csv", got, s4Bills)
	}
	if got := p.href(`a[href^="` + s4Bills + `/filter"]`); got != s4Bills+"/filter" {
		t.Errorf("Filter = %q", got)
	}

	// Bill details with no invoice total (it is optional): no amount.
	if err := env.Service.SaveBill(context.Background(), env.Suresh, w.G2.ID, 0,
		goodsreceipt.BillInput{InvoiceNo: "B-77", InvoiceDate: "2026-10-24"}); err != nil {
		t.Fatal(err)
	}
	g2 := s4Open(t, env, env.Priya, s4Bills).card(w.G2.Number)
	wantOn(t, "G2 with bill details", g2, "B-77 · 24-10-2026")
	wantOff(t, "G2 with bill details", g2, "No bill details yet", "24-10-2026 · ₹")
}

// GUX-033: Suresh (Stores: read, no export) gets no Export; his rows are
// his scope's.
func TestS4_NoExportLink(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	for _, path := range []string{s4Bills, s4Items} {
		p := s4Open(t, env, env.Suresh, path)
		if got := p.href(`a[href*="export.csv"]`); got != "" {
			t.Errorf("%s as Suresh has Export %q, want none", path, got)
		}
		if slices.Contains(p.titles(), w.GT.Number) || len(p.titles()) == 0 {
			t.Errorf("%s as Suresh rows = %q, want his scope's (never %s)", path, p.titles(), w.GT.Number)
		}
	}
}

// UX doc §5.11 filters: the chip link "No bill details yet" (inactive: a
// Link adding nobill=1) leaves only receipts without bill details and
// becomes a removable chip; vendor and month are chips named from the rows
// ("October 2026"); Export and Filter carry the filters; the count is
// singular for one.
func TestS4_BillsFilters(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	p := s4Open(t, env, env.Priya, s4Bills)
	if got := p.href(`form.filterbar a[href="` + s4Bills + `?nobill=1"]`); got == "" || !strings.Contains(p.text("form.filterbar"), "No bill details yet") {
		t.Errorf("no chip link No bill details yet to %s?nobill=1", s4Bills)
	}
	// Only nobill=1 turns the filter on, as the CSV reads it.
	p = s4Open(t, env, env.Priya, s4Bills+"?nobill=0")
	if len(p.chips()) != 0 || len(p.titles()) != 4 {
		t.Errorf("nobill=0: chips %q, rows %q, want none and every row", p.chips(), p.titles())
	}
	p = s4Open(t, env, env.Priya, s4Bills+"?nobill=1")
	if want := []string{w.G2.Number, w.GC.Number, w.G3.Number}; !slices.Equal(p.titles(), want) {
		t.Errorf("nobill rows = %q, want %q", p.titles(), want)
	}
	if want := []string{"No bill details yet"}; !slices.Equal(p.chips(), want) {
		t.Errorf("nobill chips = %q, want %q", p.chips(), want)
	}
	if got := p.href(`.filterbar-chips a.chip`); got != s4Bills {
		t.Errorf("nobill chip removes to %q, want %s", got, s4Bills)
	}

	q := "?month=2026-10&nobill=1&vendor=" + s4ID(w.Bharat)
	p = s4Open(t, env, env.Priya, s4Bills+q)
	if want := []string{w.G2.Number}; !slices.Equal(p.titles(), want) {
		t.Errorf("rows%s = %q, want %q", q, p.titles(), want)
	}
	if want := []string{"Vendor: " + grntest.ReportVendor, "Month: October 2026", "No bill details yet"}; !slices.Equal(p.chips(), want) {
		t.Errorf("chips%s = %q, want %q", q, p.chips(), want)
	}
	wantOn(t, "bar", p.text("form.filterbar"), "1 receipt")
	wantOff(t, "bar", p.text("form.filterbar"), "1 receipts")
	if got := p.href(`form.filterbar a[href*="export.csv"]`); got != s4Bills+"/export.csv"+q {
		t.Errorf("Export = %q, want the filters carried: %s/export.csv%s", got, s4Bills, q)
	}
	if got := p.href(`a[href^="` + s4Bills + `/filter"]`); got != s4Bills+"/filter"+q {
		t.Errorf("Filter = %q, want %s/filter%s", got, s4Bills, q)
	}
	p = s4Open(t, env, env.Priya, s4Bills+"?month=2026-11")
	if want := []string{w.G3.Number}; !slices.Equal(p.titles(), want) {
		t.Errorf("November rows = %q, want %q", p.titles(), want)
	}
}

// GUX-034, UX doc §5.12: Priya's Received items lists a row per line,
// newest receipt first, with the line's accepted value, item, the
// quantities with the unit, "Rejected: {reason}" only on a rejected line,
// vendor, date · PO and HSN · rate, all from the snapshots (the masters
// were renamed after). Rejected only leaves G1's rejected line.
func TestS4_ItemsPage(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	p := s4Open(t, env, env.Priya, s4Items)
	if got := p.text("h1"); got != "Received items" {
		t.Errorf("h1 = %q", got)
	}
	titles := p.titles()
	if len(titles) != 5 || titles[0] != w.G3.Number || slices.Contains(titles, w.GT.Number) {
		t.Errorf("rows = %q, want 5 lines, %s (newest) first, never %s", titles, w.G3.Number, w.GT.Number)
	}
	wantOn(t, "bar", p.text("form.filterbar"), "5 lines")
	var rejected string
	for _, c := range p.cards() {
		if strings.Contains(c, "Rejected:") {
			if rejected != "" {
				t.Errorf("two cards have a reject reason: %q and %q", rejected, c)
			}
			rejected = c
		}
	}
	wantOn(t, "G1's rejected line", rejected, w.G1.Number, "₹24,375.00", "MS Angle 50x50x5 · MS-ANGLE-50",
		"Received 400 · Accepted 390 · Rejected 10 KGS", "Rejected: Bent pieces", "Shree Steel Traders",
		"24-10-2026 · "+w.RefPO.Number, "HSN 7216 · ₹62.5")
	wantOff(t, "G1's rejected line", rejected, "renamed", "MS-ANGLE-RENAMED", "KGM")

	p = s4Open(t, env, env.Priya, s4Items+"?rejected=1")
	if want := []string{w.G1.Number}; !slices.Equal(p.titles(), want) {
		t.Errorf("rejected rows = %q, want %q", p.titles(), want)
	}
	wantOn(t, "rejected line", p.cards()[0], "Rejected: Bent pieces")
	if want := []string{"Rejected only"}; !slices.Equal(p.chips(), want) {
		t.Errorf("rejected chips = %q, want %q", p.chips(), want)
	}
	q := "?item=" + s4ID(w.Wire) + "&month=2026-11"
	p = s4Open(t, env, env.Priya, s4Items+q)
	if want := []string{w.G3.Number}; !slices.Equal(p.titles(), want) {
		t.Errorf("rows%s = %q, want %q", q, p.titles(), want)
	}
	wantOn(t, "bar", p.text("form.filterbar"), "1 line")
	if want := []string{"Item: Binding Wire 18G · WIRE-18G", "Month: November 2026"}; !slices.Equal(p.chips(), want) {
		t.Errorf("chips%s = %q, want %q", q, p.chips(), want)
	}
	if got := p.href(`form.filterbar a[href*="export.csv"]`); got != s4Items+"/export.csv"+q {
		t.Errorf("Export = %q, want %s/export.csv%s", got, s4Items, q)
	}
	p = s4Open(t, env, env.Priya, s4Items+"?vendor="+s4ID(w.Bharat))
	if want := []string{w.G2.Number}; !slices.Equal(p.titles(), want) {
		t.Errorf("Bharat's rows = %q, want %q", p.titles(), want)
	}
	wantOn(t, "G2's line", p.cards()[0], "HSN 7216 · ₹62.50")

	// A reason left on a line with nothing rejected, a rejected line with
	// no reason and a line with no HSN, with plain SQL (no public path
	// stores the first or last): no "Rejected:" and no bare "HSN".
	for _, q := range []string{
		`UPDATE goods_receipt_line SET reject_reason = 'Stale reason', snap_hsn_sac = '' WHERE parent_id = ?`,
		`UPDATE goods_receipt_line SET rejected_qty = 1000, reject_reason = '' WHERE parent_id = ?`,
	} {
		id := w.G2.ID
		if strings.Contains(q, "rejected_qty") {
			id = w.GC.ID
		}
		if _, err := env.DB.SQL().ExecContext(context.Background(), q, id); err != nil {
			t.Fatal(err)
		}
	}
	p = s4Open(t, env, env.Priya, s4Items)
	wantOff(t, "G2's line", p.card(w.G2.Number), "Rejected:", "Stale reason", "HSN")
	wantOn(t, "G2's line", p.card(w.G2.Number), "₹62.50")
	wantOn(t, "GC's line", p.card(w.GC.Number), "Rejected 1 KGS")
	wantOff(t, "GC's line", p.card(w.GC.Number), "Rejected:")
	p = s4Open(t, env, env.Priya, s4Items+"?vendor="+s4ID(w.Bharat))
	if got := p.href(`form.filterbar a[href="` + s4Items + `?rejected=1&vendor=` + s4ID(w.Bharat) + `"]`); got == "" {
		t.Errorf("no Rejected only link keeping the vendor")
	}
}

// CD-340 on the real auth.Service: a viewer with GRN read but no PO read
// sees no PO number on either page; one who reads only RefPO (own scope)
// sees RefPO's number and not BharatPO's.
func TestS4_UnreadablePOBlank(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	rr := env.ReceiptReader(t)
	for _, path := range []string{s4Bills, s4Items} {
		p := s4Open(t, env, rr, path)
		if len(p.titles()) == 0 {
			t.Fatalf("%s as Receipt Reader has no rows", path)
		}
		wantOff(t, path+" as Receipt Reader", p.text("main"), w.RefPO.Number, w.BharatPO.Number, w.TechPO.Number)
	}
	own := env.POOwnReader(t, w.RefPO.ID)
	p := s4Open(t, env, own, s4Bills)
	wantOn(t, "G1 as PO Own Reader", p.card(w.G1.Number), w.RefPO.Number)
	wantOff(t, "G2 as PO Own Reader", p.card(w.G2.Number), w.BharatPO.Number)
	p = s4Open(t, env, own, s4Items+"?vendor="+s4ID(w.Bharat))
	wantOff(t, "Bharat's line as PO Own Reader", p.text("main"), w.BharatPO.Number)
	p = s4Open(t, env, own, s4Items+"?rejected=1")
	wantOn(t, "G1's line as PO Own Reader", p.text("main"), w.RefPO.Number)
}

// UX doc §5.11, §5.12 empty states: none at all, the chip alone, and other
// filters (with Clear all).
func TestS4_EmptyStates(t *testing.T) {
	env := grntest.New(t)
	for _, tc := range []struct{ path, want, clear string }{
		{s4Bills, "No approved goods receipts yet.", ""},
		{s4Bills + "?nobill=1", "Every receipt has its bill details.", ""},
		{s4Bills + "?month=2025-01", "No receipts match these filters.", s4Bills},
		{s4Bills + "?month=2025-01&nobill=1", "No receipts match these filters.", s4Bills},
		{s4Items, "No goods have been received yet.", ""},
		{s4Items + "?rejected=1", "Nothing has been rejected.", ""},
		{s4Items + "?item=1", "No lines match these filters.", s4Items},
	} {
		p := s4Open(t, env, env.Admin, tc.path)
		main := p.text("main")
		wantOn(t, tc.path, main, tc.want)
		clear := ""
		p.doc.Find("main a").Each(func(_ int, s *goquery.Selection) {
			if strings.TrimSpace(s.Text()) == "Clear all" && !s.ParentsFiltered("form.filterbar").Is("form") {
				clear = s.AttrOr("href", "")
			}
		})
		if clear != tc.clear {
			t.Errorf("%s: empty state's Clear all = %q, want %q", tc.path, clear, tc.clear)
		}
		if strings.Contains(main, " receipts") && strings.Contains(p.text("form.filterbar"), "0 ") {
			t.Errorf("%s shows a zero count", tc.path)
		}
	}
	// No row names item 1, so its chip reads the raw id (CD-233).
	if got, want := s4Open(t, env, env.Admin, s4Items+"?item=1").chips(), []string{"Item: 1"}; !slices.Equal(got, want) {
		t.Errorf("?item=1 chips = %q, want %q", got, want)
	}
}

// The pages parse filters as the CSV does (shared billsFilter and
// itemsFilter): a bad vendor, item or month answers the same status and
// text on the page, its filter page and the CSV, and that is list's 422
// with "Choose a value for Vendor." / "Item." or ParseDate's text.
func TestS4_BadFiltersAsCSV(t *testing.T) {
	env := grntest.New(t)
	for _, tc := range []struct{ report, query, want string }{
		{s4Bills, "?vendor=abc", "Choose a value for Vendor."},
		{s4Bills, "?month=2026-13", ""},
		{s4Items, "?item=x", "Choose a value for Item."},
		{s4Items, "?vendor=x", "Choose a value for Vendor."},
		{s4Items, "?month=oct", ""},
	} {
		csv := s4Get(t, env, env.Admin, tc.report+"/export.csv"+tc.query)
		if csv.Code != http.StatusUnprocessableEntity {
			t.Errorf("CSV%s = %d, want 422", tc.query, csv.Code)
		}
		want := strings.TrimSpace(html.UnescapeString(csv.Body.String()))
		if tc.want != "" && !strings.Contains(want, tc.want) {
			t.Errorf("CSV%s = %q, want %q", tc.query, want, tc.want)
		}
		for _, path := range []string{tc.report + tc.query, tc.report + "/filter" + tc.query} {
			rec := s4Get(t, env, env.Admin, path)
			if body := html.UnescapeString(rec.Body.String()); rec.Code != csv.Code || !strings.Contains(body, want) {
				t.Errorf("GET %s = %d %q, want the CSV's %d %q", path, rec.Code, body, csv.Code, want)
			}
		}
	}
}

// A user without read on the GRN gets AUTH's 403 on every page (the
// service's Scope), never a row.
func TestS4_NoReadRefused(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	none := env.NoReceiptRights(t)
	for _, path := range []string{s4Bills, s4Items, s4Bills + "/filter", s4Items + "/filter"} {
		rec := s4Get(t, env, none, path)
		body := html.UnescapeString(rec.Body.String())
		if rec.Code != http.StatusForbidden || !strings.Contains(body, s4NoRead) || strings.Contains(body, w.G1.Number) {
			t.Errorf("GET %s with no GRN read = %d %q, want 403 %q", path, rec.Code, body, s4NoRead)
		}
	}
}

// UX doc §5.11, §5.12: the filter pages. Bills: a Vendor Picker searching
// vendors and a Month Select of the months with receipts, newest first,
// each with a help text, the Picker with an example; the chosen vendor is
// named from the rows and nobill is kept. Items adds the Item Picker and
// keeps rejected. A viewer who can't read a master gets no Picker for it
// (its search would refuse); a chosen month with no receipts stays chosen.
func TestS4_FilterPages(t *testing.T) {
	env := grntest.New(t)
	w := env.NewReportWorld(t)
	p := s4Open(t, env, env.Priya, s4Bills+"/filter?nobill=1&vendor="+s4ID(w.Bharat))
	if got := p.text("h1"); got != "Filter bills to enter" {
		t.Errorf("h1 = %q", got)
	}
	if got := p.doc.Find(`form[method="get"]`).AttrOr("action", ""); got != s4Bills {
		t.Errorf("form action = %q, want %s", got, s4Bills)
	}
	if got := p.doc.Find(`input[type="hidden"][name="nobill"]`).AttrOr("value", ""); got != "1" {
		t.Errorf("nobill not kept: %q", got)
	}
	if got := p.doc.Find(`input[type="hidden"][name="vendor"]`).AttrOr("value", ""); got != s4ID(w.Bharat) {
		t.Errorf("vendor hidden = %q, want %s", got, s4ID(w.Bharat))
	}
	wantOn(t, "filter page", p.text("main"), grntest.ReportVendor, "Only receipts from this vendor are listed.", "Only receipts dated in this month are listed.")
	if got := p.doc.Find(`[hx-get^="/pick/Party"], [data-search^="/pick/Party"], [data-url^="/pick/Party"]`).Length(); got == 0 && !strings.Contains(p.html(), "/pick/Party?role=vendor") {
		t.Errorf("no vendor Picker searching /pick/Party?role=vendor")
	}
	if !strings.Contains(p.html(), `placeholder="e.g. Shree Steel Traders"`) {
		t.Errorf("vendor Picker has no example placeholder")
	}
	var months []string
	p.doc.Find(`select[name="month"] option`).Each(func(_ int, s *goquery.Selection) { months = append(months, strings.TrimSpace(s.Text())) })
	if want := []string{"Any month", "November 2026", "October 2026"}; !slices.Equal(months, want) {
		t.Errorf("months = %q, want %q", months, want)
	}
	if p.doc.Find(`input[name="item"]`).Length() != 0 {
		t.Errorf("Bills to enter's filter page has an Item filter")
	}

	p = s4Open(t, env, env.Priya, s4Items+"/filter?rejected=1&month=2025-01")
	if got := p.text("h1"); got != "Filter received items" {
		t.Errorf("h1 = %q", got)
	}
	if got := p.doc.Find(`input[type="hidden"][name="rejected"]`).AttrOr("value", ""); got != "1" {
		t.Errorf("rejected not kept: %q", got)
	}
	wantOn(t, "items filter", p.html(), "/pick/Item", `placeholder="e.g. MS-ANGLE-50"`, "Only lines of this item are listed.")
	if got := p.doc.Find(`select[name="month"] option[selected]`).AttrOr("value", ""); got != "2025-01" {
		t.Errorf("chosen month = %q, want 2025-01 kept", got)
	}

	// Without the toggle no toggle value is posted; with nothing chosen the
	// Pickers have no chosen row and post no vendor or item.
	for _, path := range []string{s4Bills + "/filter", s4Items + "/filter"} {
		p = s4Open(t, env, env.Priya, path)
		if n := p.doc.Find(`input[name="nobill"], input[name="rejected"]`).Length(); n != 0 {
			t.Errorf("%s with no toggle posts a toggle (%d inputs)", path, n)
		}
		p.doc.Find(`input[name="vendor"], input[name="item"]`).Each(func(_ int, in *goquery.Selection) {
			if v := in.AttrOr("value", ""); v != "" {
				t.Errorf("%s with nothing chosen posts %s=%q", path, in.AttrOr("name", ""), v)
			}
		})
		p.doc.Find(`.picker [data-picker-chosen]`).Each(func(_ int, c *goquery.Selection) {
			if _, hidden := c.Attr("hidden"); !hidden || squash(c.Find("[data-picker-summary]").Text()) != "" {
				t.Errorf("%s with nothing chosen shows a chosen row %q", path, squash(c.Text()))
			}
		})
	}
	// Each help text names the report's rows.
	wantOn(t, "bills filter", s4Open(t, env, env.Priya, s4Bills+"/filter").text("main"), "Only receipts from this vendor are listed.")
	items := s4Open(t, env, env.Priya, s4Items+"/filter").text("main")
	wantOn(t, "items filter", items, "Only lines from this vendor are listed.", "Only lines dated in this month are listed.")
	wantOff(t, "items filter", items, "receipts")

	// Each Picker follows its own master's read right (real AUTH users
	// holding GRN read and only that master's read).
	for _, tc := range []struct{ role, master, shown, hidden string }{
		{"Party Reader", party.DocType, "/pick/Party", "/pick/Item"},
		{"Item Reader", item.DocType, "/pick/Item", "/pick/Party"},
	} {
		p = s4Open(t, env, s4MasterReader(t, env, tc.role, tc.master), s4Items+"/filter")
		if h := p.html(); !strings.Contains(h, tc.shown) || strings.Contains(h, tc.hidden) {
			t.Errorf("%s's filter page: want %s and not %s", tc.role, tc.shown, tc.hidden)
		}
	}

	rr := env.ReceiptReader(t) // no read on Party or Item
	p = s4Open(t, env, rr, s4Items+"/filter")
	if strings.Contains(p.html(), "/pick/") {
		t.Errorf("Receipt Reader's filter page has a Picker, whose search would refuse")
	}
	if p.doc.Find(`select[name="month"]`).Length() != 1 {
		t.Errorf("Receipt Reader's filter page has no Month")
	}
}

// s4MasterReader creates a user whose only role holds read on the GRN and
// read on master, through the real AUTH.
func s4MasterReader(t *testing.T, env *grntest.Env, role, master string) auth.Principal {
	t.Helper()
	ctx := context.Background()
	if err := env.Auth.CreateRole(ctx, env.Admin, role); err != nil {
		t.Fatal(err)
	}
	for _, dt := range []string{goodsreceipt.DocType, master} {
		if err := env.Auth.SetRights(ctx, env.Admin, role, dt, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}); err != nil {
			t.Fatal(err)
		}
	}
	slug := strings.ToLower(strings.ReplaceAll(role, " ", ""))
	id, _, err := env.Auth.CreateUser(ctx, env.Admin, auth.NewUser{ //nolint:gosec // G101: a test user's password
		Email: slug + "@vardhan.test", Name: role, Password: "Reading one master 42", Roles: []string{role},
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := env.Auth.LoadPrincipal(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (p s4Page) html() string {
	h, _ := p.doc.Html()
	return html.UnescapeString(h)
}

// A page holds 25 rows (UX doc §5.11): 26 approved GRNs (one line each)
// show 25 on page 1 with Next keeping the filters, and the 26th on page 2
// with Previous; a page past the end says so. The copies are plain SQL, as
// G9's too-many-rows test makes them.
func TestS4_PageSize(t *testing.T) {
	ctx := context.Background()
	env := grntest.New(t)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved())
	grn := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "10"}}, doctype.StateApproved)
	if _, err := env.DB.SQL().ExecContext(ctx, `WITH RECURSIVE k(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM k WHERE i < 25)
		INSERT INTO goods_receipt (number, state, created_at, created_by, updated_at, updated_by, purchase_order_id, vendor_id, site_id, project_id,
			receipt_date, challan_kind, challan_no, accepted_value, snap_vendor_name, snap_vendor_gstin, snap_po_number, snap_po_date)
		SELECT 'GR/COPY/' || printf('%02d', i), state, created_at, created_by, updated_at, updated_by, purchase_order_id, vendor_id, site_id, project_id,
			receipt_date, challan_kind, challan_no, accepted_value, snap_vendor_name, snap_vendor_gstin, snap_po_number, snap_po_date
		FROM k, goods_receipt WHERE goods_receipt.id = ?`, grn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := env.DB.SQL().ExecContext(ctx, `INSERT INTO goods_receipt_line (parent_id, line_no, po_line_id, uom_id, received_qty, rejected_qty, accepted_qty,
			snap_item_code, snap_item_name, snap_hsn_sac, snap_uom_code, snap_rate, snap_is_service)
		SELECT g.id, l.line_no, l.po_line_id, l.uom_id, l.received_qty, l.rejected_qty, l.accepted_qty,
			l.snap_item_code, l.snap_item_name, l.snap_hsn_sac, l.snap_uom_code, l.snap_rate, l.snap_is_service
		FROM goods_receipt g, goods_receipt_line l WHERE g.id <> ? AND l.parent_id = ?`, grn.ID, grn.ID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ base, noun string }{{s4Bills, "receipts"}, {s4Items, "lines"}} {
		q := "?month=2026-10"
		p := s4Open(t, env, env.Admin, tc.base+q)
		if n := len(p.titles()); n != 25 {
			t.Errorf("%s page 1 has %d rows, want 25", tc.base, n)
		}
		wantOn(t, tc.base+" bar", p.text("form.filterbar"), "26 "+tc.noun)
		wantOn(t, tc.base+" pager", p.text("nav.pagination"), "1–25 of 26")
		if got := p.href(`nav.pagination a[rel="next"]`); got != tc.base+q+"&page=2" {
			t.Errorf("%s Next = %q, want %s%s&page=2", tc.base, got, tc.base, q)
		}
		p = s4Open(t, env, env.Admin, tc.base+q+"&page=2")
		if n := len(p.titles()); n != 1 {
			t.Errorf("%s page 2 has %d rows, want 1", tc.base, n)
		}
		if got := p.href(`nav.pagination a[rel="prev"]`); got != tc.base+q+"&page=1" {
			t.Errorf("%s Previous = %q", tc.base, got)
		}
		if p.doc.Find(`nav.pagination a[rel="next"]`).Length() != 0 {
			t.Errorf("%s page 2 has Next", tc.base)
		}
		// Unfiltered: Next and Previous carry no query but the page.
		p = s4Open(t, env, env.Admin, tc.base)
		if got := p.href(`nav.pagination a[rel="next"]`); got != tc.base+"?page=2" {
			t.Errorf("%s unfiltered Next = %q, want %s?page=2", tc.base, got, tc.base)
		}
		p = s4Open(t, env, env.Admin, tc.base+"?page=2")
		if got := p.href(`nav.pagination a[rel="prev"]`); got != tc.base+"?page=1" {
			t.Errorf("%s unfiltered page 2 Previous = %q, want %s?page=1", tc.base, got, tc.base)
		}
		s4PastEnd(t, s4Open(t, env, env.Admin, tc.base+"?page=99"), tc.base+"?page=99", tc.base+"?page=1")
		s4PastEnd(t, s4Open(t, env, env.Admin, tc.base+q+"&page=3"), tc.base+q+"&page=3", tc.base+q+"&page=1")
		for _, bad := range []string{"x", "0", "-3"} {
			p = s4Open(t, env, env.Admin, tc.base+"?page="+bad)
			if n := len(p.titles()); n != 25 || p.doc.Find(`nav.pagination a[rel="prev"]`).Length() != 0 {
				t.Errorf("%s page=%s has %d rows (or Previous), want page 1's 25", tc.base, bad, n)
			}
		}
		// A page whose offset would overflow is past the end, never a panic.
		s4PastEnd(t, s4Open(t, env, env.Admin, tc.base+"?page=368934881474191034"), tc.base+" huge page", tc.base+"?page=1")
	}
}

// s4PastEnd checks a page past the last one: no rows, "No more results."
// once with Go to page 1 (first, exactly), not the report's empty text, and
// no pager repeating it.
func s4PastEnd(t *testing.T, p s4Page, what, first string) {
	t.Helper()
	main := p.text("main")
	if n := len(p.titles()); n != 0 || strings.Count(main, "No more results.") != 1 {
		t.Errorf("%s: %d rows, %q, want none and No more results. once", what, n, main)
	}
	wantOff(t, what, main, "No approved goods receipts yet.", "No goods have been received yet.", "match these filters")
	if got := p.href(`.empty-state a`); got != first || strings.TrimSpace(p.text(".empty-state a")) != "Go to page 1" {
		t.Errorf("%s: empty-state link %q %q, want Go to page 1 to %s", what, p.text(".empty-state a"), got, first)
	}
	if p.doc.Find("nav.pagination").Length() != 0 {
		t.Errorf("%s still draws the pager", what)
	}
}
