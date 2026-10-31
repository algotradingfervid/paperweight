package purchaseweb

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

const pendingPath = "/reports/purchase/pending"

// rowOf is the DataList row text of PO number, "" when not listed.
func rowOf(p p15ePage, number string) string {
	for _, r := range p.texts(".datalist-rows > li") {
		if strings.Contains(r, number) {
			return r
		}
	}
	return ""
}

// MOD-PO P15e, PUX-026 and PUX-035's report part (UX §5.19; spec §13,
// §27.1; plan P9): each overdue row reads `Due {date} · Overdue`, a row with
// Expected on reads `Due {date} (expected)` without it, `Overdue only` keeps
// only the overdue rows (with its chip and Clear all), the count line, the
// row's figures and link, and `Export` only for a user with export. The
// clock is just after IST midnight (18:31 UTC on 26-10 is 27-10 IST), so a
// line due 26-10 is overdue only by the IST date.
func TestPurchaseWeb_PendingReport(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)

	empty := w.get(t, env.Priya, pendingPath)
	if empty.status != http.StatusOK || !strings.Contains(empty.text, "Nothing is pending on approved orders.") {
		t.Errorf("empty report = %d %q", empty.status, empty.text)
	}

	due := func(on string) string {
		return purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"required_by": on})).Number
	}
	late1, late2, late3 := due("26-10-2026"), due("25-10-2026"), due("26-10-2026")
	later := due("28-10-2026")
	expected := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"required_by": "25-10-2026"}))
	on, _ := types.ParseDate("05-11-2026")
	if err := env.Service.SetExpected(t.Context(), env.Priya, expected.ID, 0, on, ""); err != nil {
		t.Fatalf("setup: SetExpected: %v", err)
	}
	env.SetNow(t, time.Date(2026, time.October, 26, 18, 31, 0, 0, time.UTC))

	page := w.get(t, env.Priya, pendingPath)
	if page.status != http.StatusOK {
		t.Fatalf("GET = %d %q", page.status, page.text)
	}
	if h1 := page.texts("h1"); !slices.Equal(h1, []string{"Pending items"}) {
		t.Errorf("h1 = %q", h1)
	}
	if !strings.Contains(page.text, "5 pending lines") {
		t.Errorf("no `5 pending lines` in %q", page.text)
	}
	for _, n := range []string{late1, late2, late3} {
		if r := rowOf(page, n); !strings.Contains(r, "· Overdue") {
			t.Errorf("row %s = %q, want Overdue", n, r)
		}
	}
	if r := rowOf(page, late2); !strings.Contains(r, "Due 25-10-2026 · Overdue") {
		t.Errorf("row %s = %q, want `Due 25-10-2026 · Overdue`", late2, r)
	}
	if r := rowOf(page, later); !strings.Contains(r, "Due 28-10-2026") || strings.Contains(r, "Overdue") {
		t.Errorf("row %s = %q, want due 28-10-2026 and not overdue", later, r)
	}
	if r := rowOf(page, expected.Number); !strings.Contains(r, "Due 05-11-2026 (expected)") || strings.Contains(r, "Overdue") {
		t.Errorf("row %s = %q, want `Due 05-11-2026 (expected)` and not overdue", expected.Number, r)
	}

	rows, err := env.Service.PendingReport(t.Context(), env.Priya, purchase.PendingFilter{}, 1)
	if err != nil || len(rows.Rows) == 0 {
		t.Fatalf("PendingReport: %v", err)
	}
	r0 := rows.Rows[0]
	row := rowOf(page, r0.PONumber)
	for _, s := range []string{
		types.FormatINR(r0.PendingValue), r0.ItemName + " · " + r0.ItemCode,
		"Pending " + types.FormatQtyUoM(r0.Pending, r0.UoM) + " of " + types.FormatQty(r0.Ordered) + " · Received " + types.FormatQty(r0.Received),
		r0.Vendor, r0.Project + " · " + r0.Site,
	} {
		if !strings.Contains(row, s) {
			t.Errorf("row %s = %q, want %q in it", r0.PONumber, row, s)
		}
	}
	if href := page.doc.Find(".datalist-rows > li a").First().AttrOr("href", ""); href != "/doc/purchase.Order/"+strconv.FormatInt(r0.POID, 10) {
		t.Errorf("first row links to %q, want its PO", href)
	}
	if href, _ := page.attr(`a:contains("Overdue only")`, "href"); href != pendingPath+"?overdue=1" {
		t.Errorf("Overdue only = %q", href)
	}

	overdue := w.get(t, env.Priya, pendingPath+"?overdue=1")
	if n := overdue.doc.Find(".datalist-rows > li").Length(); n != 3 || rowOf(overdue, later) != "" || !strings.Contains(overdue.text, "3 pending lines") {
		t.Errorf("overdue only = %d rows %q, want the 3 overdue ones", n, overdue.texts(".datalist-rows > li"))
	}
	if !strings.Contains(overdue.text, "Overdue only") || !strings.Contains(overdue.text, "Clear all") {
		t.Errorf("overdue only has no chip or Clear all: %q", overdue.text)
	}

	none := w.get(t, env.Priya, pendingPath+"?item=999999")
	if !strings.Contains(none.text, "No pending lines match these filters.") {
		t.Errorf("filtered empty = %q", none.text)
	}
	if bad := w.get(t, env.Priya, pendingPath+"?vendor=abc"); bad.status != http.StatusBadRequest {
		t.Errorf("vendor=abc = %d, want 400", bad.status)
	}
	vendorID := strconv.FormatInt(p15eMasterID(t, env, "party", "SHREE"), 10)
	if v := w.get(t, env.Priya, pendingPath+"?vendor="+vendorID); !strings.Contains(v.text, "Vendor: "+r0.Vendor) {
		t.Errorf("vendor filter has no chip `Vendor: %s`: %q", r0.Vendor, v.text)
	}

	t.Run("export only with export", func(t *testing.T) {
		exporter := p15eUser(t, env, "Order Exporter", map[string][]auth.Right{purchase.DocType: {auth.RightRead, auth.RightExport}})
		got := w.get(t, exporter, pendingPath+"?overdue=1")
		if href, ok := got.attr(`a:contains("Export")`, "href"); !ok || href != pendingPath+"/export.csv?overdue=1" {
			t.Errorf("exporter's Export = %q %v, want the CSV with the filters", href, ok)
		}
		if _, ok := w.get(t, env.Suresh, pendingPath).attr(`a:contains("Export")`, "href"); ok {
			t.Errorf("Suresh (read, no export) has Export")
		}
	})

	t.Run("pages of 25", func(t *testing.T) {
		for range purchase.PendingPageSize {
			purchasetest.NewOrder(t, env)
		}
		got := w.get(t, env.Priya, pendingPath)
		if n := got.doc.Find(".datalist-rows > li").Length(); n != purchase.PendingPageSize || !strings.Contains(got.text, "1–25 of 30") {
			t.Errorf("page 1 = %d rows %q, want 25 and `1–25 of 30`", n, got.text)
		}
		if next, _ := got.attr(".pagination a[rel=next]", "href"); next != pendingPath+"?page=2" {
			t.Errorf("Next = %q", next)
		}
		if n := w.get(t, env.Priya, pendingPath+"?page=2").doc.Find(".datalist-rows > li").Length(); n != 5 {
			t.Errorf("page 2 = %d rows, want 5", n)
		}
	})
}

// MOD-PO P15e (UX §5.19): the filter page is a GET form back to the report
// with a Picker for each of vendor, project, site and item the user may
// read, the active filters chosen and overdue kept; a user who reads no
// master gets no Pickers.
func TestPurchaseWeb_PendingFilter(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	vendorID := p15eMasterID(t, env, "party", "SHREE")
	page := w.get(t, env.Admin, pendingPath+"/filter?overdue=1&vendor="+strconv.FormatInt(vendorID, 10))
	if page.status != http.StatusOK {
		t.Fatalf("GET = %d %q", page.status, page.text)
	}
	if h1 := page.texts("h1"); !slices.Equal(h1, []string{"Filter pending items"}) {
		t.Errorf("h1 = %q", h1)
	}
	form := page.doc.Find(`form[method=get][action="` + pendingPath + `"]`)
	var names []string
	form.Find(`input[type=hidden]`).Each(func(_ int, s *goquery.Selection) {
		names = append(names, s.AttrOr("name", "")+"="+s.AttrOr("value", ""))
	})
	for _, want := range []string{"overdue=1", "vendor=" + strconv.FormatInt(vendorID, 10), "project=", "site=", "item="} {
		if !slices.Contains(names, want) {
			t.Errorf("hidden inputs = %q, want %q", names, want)
		}
	}
	for _, dt := range []string{party.DocType, project.DocType, site.DocType, item.DocType} {
		if form.Find(`[data-search-url^="/pick/`+dt+`"], [hx-get^="/pick/`+dt+`"]`).Length() == 0 && !strings.Contains(mustHTML(form), "/pick/"+dt) {
			t.Errorf("no Picker searching /pick/%s", dt)
		}
	}
	reader := p15eUser(t, env, "Order Only", map[string][]auth.Right{purchase.DocType: {auth.RightRead}})
	if got := w.get(t, reader, pendingPath+"/filter"); got.status != http.StatusOK || strings.Contains(mustHTML(got.doc.Selection), "/pick/") {
		t.Errorf("reader with no master read = %d, has a Picker", got.status)
	}
}

func mustHTML(s *goquery.Selection) string {
	h, _ := goquery.OuterHtml(s)
	return h
}

// p15eMasterID is the id of the master row with code in table.
func p15eMasterID(t *testing.T, env *purchasetest.Env, table, code string) int64 {
	t.Helper()
	var id int64
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&id); err != nil { //nolint:gosec // G202: fixed table names
		t.Fatalf("id of %s %s: %v", table, code, err)
	}
	return id
}

// MOD-PO P15e (UX §5.19): the report and its filter page refuse a user with
// no read on Purchase Order with AUTH's 403 (the service's scope), and an id
// that isn't a number with AUTH's bad form.
func TestPurchaseWeb_PendingRefusals(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	requests := p15eUser(t, env, "Request Only", map[string][]auth.Right{purchase.RequestDocType: {auth.RightRead}})
	for _, path := range []string{pendingPath, pendingPath + "/filter"} {
		if got := w.get(t, requests, path); got.status != http.StatusForbidden {
			t.Errorf("GET %s with no read on Order = %d, want 403", path, got.status)
		}
		if got := w.get(t, env.Priya, path+"?site=x"); got.status != http.StatusBadRequest || !strings.Contains(got.text, auth.ErrBadForm.Msg) {
			t.Errorf("GET %s?site=x = %d %q, want 400 with %q", path, got.status, got.text, auth.ErrBadForm.Msg)
		}
	}
}
