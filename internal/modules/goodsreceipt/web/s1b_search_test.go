package grnweb_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	grnweb "github.com/vardhan/internaltools/internal/modules/goodsreceipt/web"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MOD-GRN S1b (CD-323 (2); UX doc §5.2, GUX-002's 50+ row): the PO search
// combobox step 1 shows at 50 or more orders, and its search route.

const s1bSearch = "/modules/goodsreceipt/po-search"

// s1bRow is one search result: value, code, name and line.
type s1bRow struct{ value, code, name, line string }

// search serves GET s1bSearch+query as p through grnweb.Routes and returns
// the rows.
func (w *s1World) search(p auth.Principal, query string) ([]s1bRow, string) {
	w.t.Helper()
	r := chi.NewRouter()
	grnweb.Routes(grnweb.Deps{Service: w.Service})(r)
	req := httptest.NewRequestWithContext(auth.WithPrincipal(w.t.Context(), p), http.MethodGet, s1bSearch+query, nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		w.t.Fatalf("GET %s%s = %d %q, want 200", s1bSearch, query, rec.Code, rec.Body.String())
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
	if err != nil {
		w.t.Fatal(err)
	}
	var out []s1bRow
	doc.Find(`li[role="option"][data-value]`).Each(func(i int, s *goquery.Selection) {
		if id := s.AttrOr("id", ""); id != "f-purchase_order_id-list-"+strconv.Itoa(i) {
			w.t.Errorf("row %d id = %q, want f-purchase_order_id-list-%d (step 1's listbox)", i, id, i)
		}
		out = append(out, s1bRow{s.AttrOr("data-value", ""), s.Find(".picker__code").Text(), s.Find(".picker__name").Text(), s.Find(".picker__line").Text()})
	})
	return out, strings.Join(strings.Fields(doc.Text()), " ")
}

// GUX-002 (UX doc §5.2): the search matches q against the PO number, the
// vendor code, the vendor name and the GSTIN, case-insensitively; each row
// is Code = PO number, Name = vendor name, Line = "{site} · {p} of {n} lines
// pending". A blank q lists every order with pending.
func TestGRNWeb_POSearchFields(t *testing.T) {
	w := newS1World(t)
	shree := w.po("SHREE", "10")
	bharat := w.po("BHARAT", "5", "6")
	rajkot := w.po("RAJKOT", "7")
	// The fixture codes are all inside their names, so one PO's snapshot
	// code is made unique to prove the code is searched on its own.
	if _, err := w.DB.SQL().ExecContext(t.Context(), `UPDATE purchase_order SET snap_vendor_code = 'VX-77' WHERE id = ?`, rajkot.ID); err != nil {
		t.Fatal(err)
	}
	row := func(po string, id int64, name, line string) s1bRow {
		return s1bRow{strconv.FormatInt(id, 10), po, name, line}
	}
	shreeRow := row(shree.Number, shree.ID, "Shree Steel Traders", "METRO-SITE · 1 of 1 line pending")
	bharatRow := row(bharat.Number, bharat.ID, "Bharat Tools", "METRO-SITE · 2 of 2 lines pending")
	rajkotRow := row(rajkot.Number, rajkot.ID, "Rajkot Castings", "METRO-SITE · 1 of 1 line pending")
	for _, c := range []struct {
		field, q string
		want     []s1bRow
	}{
		{"PO number", strings.ToLower(shree.Number), []s1bRow{shreeRow}},
		{"vendor code", "vx-77", []s1bRow{rajkotRow}},
		{"vendor name", "TOOLS", []s1bRow{bharatRow}},
		{"GSTIN", "24aaacr5055k", []s1bRow{rajkotRow}},
		{"blank", "", []s1bRow{bharatRow, rajkotRow, shreeRow}},
	} {
		if got, _ := w.search(w.Suresh, "?q="+c.q); !slices.Equal(got, c.want) {
			t.Errorf("%s q=%q: rows =\n%+v\nwant\n%+v", c.field, c.q, got, c.want)
		}
	}
	if got, text := w.search(w.Suresh, "?q=nothing"); len(got) != 0 || !strings.Contains(text, "No matches") {
		t.Errorf("q=nothing: rows %+v, text %q, want No matches", got, text)
	}
}

// GUX-002, CD-233, hooks spec §5 rule 4: the search lists only orders the
// user may read (Auth.Scope). Suresh never sees the TECH-SITE PO, by its
// number or its vendor, while admin finds it with the same q.
func TestGRNWeb_POSearchScope(t *testing.T) {
	w := newS1World(t)
	mine := w.po("SHREE", "10")
	tech := w.techPO()
	got, text := w.search(w.Suresh, "?q=shree")
	if len(got) != 1 || got[0].code != mine.Number {
		t.Errorf("Suresh q=shree: rows %+v, want only %s", got, mine.Number)
	}
	if strings.Contains(text, tech.Number) || strings.Contains(text, "TECH-SITE") {
		t.Errorf("Suresh's search names the TECH-SITE PO %s: %q", tech.Number, text)
	}
	if got, _ := w.search(w.Suresh, "?q="+tech.Number); len(got) != 0 {
		t.Errorf("Suresh q=%s: rows %+v, want none", tech.Number, got)
	}
	if got, _ := w.search(w.Admin, "?q=shree"); len(got) != 2 {
		t.Errorf("admin q=shree: rows %+v, want both POs", got)
	}
}

// GUX-003 with GUX-002: the search lists fully received orders only with
// all=1, as step 1's widened list does, their line reading "Fully received".
func TestGRNWeb_POSearchWidened(t *testing.T) {
	w := newS1World(t)
	full := w.po("BHARAT", "5")
	w.receiveAll(full)
	if got, _ := w.search(w.Suresh, "?q=bharat"); len(got) != 0 {
		t.Errorf("default search: rows %+v, want no fully received PO", got)
	}
	want := []s1bRow{{strconv.FormatInt(full.ID, 10), full.Number, "Bharat Tools", "METRO-SITE · Fully received"}}
	if got, _ := w.search(w.Suresh, "?all=1&q=bharat"); !slices.Equal(got, want) {
		t.Errorf("all=1 search: rows %+v, want %+v", got, want)
	}
}

// GUX-002 (UX doc §5.2): 49 orders are a Select; the 50th turns step 1 into
// the combobox: no options rendered, the search input fetching s1bSearch,
// label Purchase order, hint Search by PO number or vendor., and the hidden
// purchase_order_id. ?all=1 carries into the search URL, and a ?po=
// preselect shows as the chosen row.
func TestGRNWeb_ChoosePOCombobox(t *testing.T) {
	w := newS1World(t)
	vendors := []string{"SHREE", "BHARAT", "RAJKOT"}
	var first int64
	for i := range 49 {
		po := w.po(vendors[i%3], "10")
		if i == 0 {
			first = po.ID
		}
	}
	r := w.get(w.Suresh, s1New)
	if n := r.doc.Find(`select[name="purchase_order_id"] option`).Length(); n != 50 || r.doc.Find(`[role="combobox"]`).Length() != 0 {
		t.Fatalf("49 POs: %d select options and %d comboboxes, want 50 options (blank first) and none", n, r.doc.Find(`[role="combobox"]`).Length())
	}

	w.po("SHREE", "10")
	r = w.get(w.Suresh, s1New)
	if n := r.doc.Find("select").Length() + r.doc.Find("option").Length() + len(r.radios("purchase_order_id")); n != 0 {
		t.Errorf("50 POs: %d selects, options or radios, want none", n)
	}
	input := r.doc.Find(`input[role="combobox"]`)
	if got := input.AttrOr("hx-get", ""); input.Length() != 1 || got != s1bSearch {
		t.Fatalf("50 POs: %d comboboxes, hx-get %q, want one fetching %s", input.Length(), got, s1bSearch)
	}
	if got := input.AttrOr("aria-controls", ""); got != "f-purchase_order_id-list" {
		t.Errorf("combobox listbox = %q, want f-purchase_order_id-list (the one the search fills)", got)
	}
	id := input.AttrOr("id", "")
	if got := strings.Join(strings.Fields(r.doc.Find(`label[for="`+id+`"]`).Text()), " "); got != "Purchase order * (required)" {
		t.Errorf("label = %q, want Purchase order * (required) (CD-341 (2))", got)
	}
	if got := r.doc.Find("#f-purchase_order_id-hint").Text(); got != "Search by PO number or vendor." {
		t.Errorf("hint = %q, want Search by PO number or vendor.", got)
	}
	hidden := r.doc.Find(`input[type="hidden"][name="purchase_order_id"]`)
	if hidden.Length() != 1 || hidden.AttrOr("value", "x") != "" {
		t.Errorf("hidden purchase_order_id: %d, value %q, want one, blank", hidden.Length(), hidden.AttrOr("value", "x"))
	}
	if !strings.Contains(r.text(), "Show fully received orders") {
		t.Errorf("the combobox step lost Show fully received orders")
	}

	r = w.get(w.Suresh, s1New+"?all=1")
	if got := r.doc.Find(`input[role="combobox"]`).AttrOr("hx-get", ""); got != s1bSearch+"?all=1" {
		t.Errorf("widened hx-get = %q, want %s?all=1", got, s1bSearch)
	}
	if !strings.Contains(r.text(), "Show only orders with items pending") {
		t.Errorf("the widened combobox step lost Show only orders with items pending")
	}

	r = w.get(w.Suresh, s1New+"?po="+strconv.FormatInt(first, 10))
	if got := r.doc.Find(`input[type="hidden"][name="purchase_order_id"]`).AttrOr("value", ""); got != strconv.FormatInt(first, 10) {
		t.Errorf("?po= hidden value = %q, want %d", got, first)
	}
	if got := strings.TrimSpace(r.doc.Find("[data-picker-summary]").Text()); got != "PO/26-27/00001 Shree Steel Traders" {
		t.Errorf("?po= chosen summary = %q, want PO/26-27/00001 Shree Steel Traders", got)
	}
}

// A failed read is the 500 page, never a 200 saying "No matches": the
// service reads a closed database while the request stays live.
func TestGRNWeb_POSearchReadFails(t *testing.T) {
	w := newS1World(t)
	w.po("SHREE", "10")
	closed, err := db.Open(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	deps := w.Deps
	deps.DB = closed
	r := chi.NewRouter()
	grnweb.Routes(grnweb.Deps{Service: goodsreceipt.New(deps)})(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(auth.WithPrincipal(t.Context(), w.Suresh), http.MethodGet, s1bSearch+"?q=shree", nil))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "No matches") {
		t.Errorf("search on a failed read = %d %q, want the 500 page", rec.Code, rec.Body.String())
	}
}
