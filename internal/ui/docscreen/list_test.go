package docscreen_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

var priya = auth.Principal{ID: 2, Name: "Priya Shah"}

type env struct {
	docs   *docfake.Fake
	az     *authfake.Fake
	lister *docscreentest.Lister
	db     *docscreentest.Reader
	h      http.Handler
}

// newEnv grants the Buyer role the given rights on the sample DocType and
// gives Priya that role (CD-56).
func newEnv(t *testing.T, rights ...auth.Right) *env {
	t.Helper()
	docscreentest.RegisterSample()
	e := &env{docs: docfake.New(), az: authfake.New(), lister: docscreentest.NewLister(), db: &docscreentest.Reader{}}
	for _, r := range rights {
		e.az.Grant("Buyer", docscreentest.Sample, r, auth.ScopeAll)
	}
	e.az.AddRole(priya.ID, "Buyer")
	d := docscreen.Deps{
		DB:        e.db,
		Engine:    e.docs,
		Auth:      e.az,
		Approval:  &approver{},
		Lister:    e.lister,
		Extras:    docscreentest.NewExtras(),
		Principal: func(*http.Request) (auth.Principal, bool) { return priya, true },
	}
	r := chi.NewRouter()
	docscreen.Routes(d)(r)
	e.h = r
	return e
}

func (e *env) get(t *testing.T, target string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	return uitest.Do(t, e.h, http.MethodGet, target, nil, hdr...)
}

func twoRows(total int) types.Result[docscreen.ListRow] {
	return types.Result[docscreen.ListRow]{Total: total, Rows: []docscreen.ListRow{
		{
			Info:   doctype.Info{DocType: docscreentest.Sample, ID: 1, State: doctype.StateApproved, Number: "SO/26-27/00001", Display: "SO/26-27/00001", Party: "Shree Steel Traders"},
			Values: map[string]string{"amount": "94164.00", "order_date": "2026-10-24"},
		},
		{
			Info:   doctype.Info{DocType: docscreentest.Sample, ID: 42, State: doctype.StateDraft, Display: "Draft 42", Party: "Bharat Tools"},
			Values: map[string]string{"amount": "1200.50", "order_date": "2026-10-23"},
		},
	}}
}

func TestDocList_RendersCardsAndCount(t *testing.T) {
	e := newEnv(t, "read", "create")
	e.lister.Set(docscreentest.Sample, twoRows(2))
	rec := e.get(t, "/doc/docscreentest.Sample")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if n := e.db.Calls(); n != 1 {
		t.Errorf("Read called %d times per list GET, want 1", n)
	}
	doc := uitest.Doc(t, rec)

	items := doc.Find("ul.datalist-rows > li")
	if items.Length() != 2 {
		t.Fatalf("got %d rows, want 2", items.Length())
	}
	first := items.Eq(0)
	if got := strings.TrimSpace(first.Find(".datalist-title").Text()); got != "SO/26-27/00001" {
		t.Errorf("title = %q, want Info.Display", got)
	}
	if href, _ := first.Find("a").Attr("href"); href != "/doc/docscreentest.Sample/1" {
		t.Errorf("row href = %q", href)
	}
	for _, want := range []string{"₹94,164.00", "24-10-2026", "Approved"} {
		if !strings.Contains(first.Text(), want) {
			t.Errorf("row 1 lacks %q: %q", want, first.Text())
		}
	}
	if got := strings.TrimSpace(items.Eq(1).Find(".datalist-party").Text()); got != "Bharat Tools" {
		t.Errorf("row 2 party = %q, want Info.Party", got)
	}
	if got := strings.TrimSpace(doc.Find(".filterbar-count").Text()); got != "2 sample orders" {
		t.Errorf("count = %q", got)
	}
	if got := doc.Find(`label[for="filter-q"]`).Text(); got != "Search sample orders" {
		t.Errorf("search label = %q", got)
	}
	newLink := doc.Find(`a[href="/doc/docscreentest.Sample/new"]`)
	if strings.TrimSpace(newLink.Text()) != "New sample order" {
		t.Errorf("new link = %q", newLink.Text())
	}
	if doc.Find(`a[href$="export.csv"]`).Length() != 0 {
		t.Error("Export shown without the export right")
	}

	e.az.Grant("Buyer", docscreentest.Sample, "export", auth.ScopeAll)
	doc = uitest.Doc(t, e.get(t, "/doc/docscreentest.Sample"))
	if strings.TrimSpace(doc.Find(`a[href="/doc/docscreentest.Sample/export.csv"]`).Text()) != "Export" {
		t.Error("Export missing with the export right")
	}
}

func TestDocList_StateRoundTripsURL(t *testing.T) {
	e := newEnv(t, "read")
	e.lister.Set(docscreentest.Sample, twoRows(80))
	rec := e.get(t, "/doc/docscreentest.Sample?q=steel&sort=amount.desc&f=state:=:approved&page=2&size=50")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	want := docscreen.ListQuery{
		Search:  "steel",
		Sort:    "amount.desc",
		Filters: []docscreen.Filter{{Field: "state", Op: "=", Values: []string{"approved"}}},
		Page:    2,
		Size:    50,
	}
	if got := e.lister.Last(); !reflect.DeepEqual(got, want) {
		t.Errorf("Lister got %+v, want %+v", got, want)
	}

	doc := uitest.Doc(t, rec)
	form := doc.Find("form.filterbar")
	if v, _ := form.Find(`input[name="q"]`).Attr("value"); v != "steel" {
		t.Errorf("q = %q", v)
	}
	if v, _ := form.Find(`select[name="sort"] option[selected]`).Attr("value"); v != "amount.desc" {
		t.Errorf("selected sort = %q", v)
	}
	if form.Find(`input[type="hidden"][name="f"][value="state:=:approved"]`).Length() != 1 {
		t.Error("form does not keep the active filter as a hidden f")
	}
	if form.Find(`input[type="hidden"][name="size"][value="50"]`).Length() != 1 {
		t.Error("form does not keep the page size as a hidden size")
	}
	if form.Find(`input[type="hidden"][name="page"]`).Length() != 0 {
		t.Error("form keeps page; a new search or sort must start on page 1")
	}
	chip := form.Find("a.chip")
	if chip.Length() != 1 || !strings.Contains(chip.Text(), "State: Approved") {
		t.Fatalf("chips = %d %q", chip.Length(), chip.Text())
	}
	href, _ := chip.Attr("href")
	u, err := url.Parse(href)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/doc/docscreentest.Sample" || u.Query().Has("f") || u.Query().Get("q") != "steel" || u.Query().Get("sort") != "amount.desc" {
		t.Errorf("chip remove URL = %q", href)
	}
	if got := doc.Find(".pagination-range").Text(); got != "51–80 of 80" {
		t.Errorf("range = %q", got)
	}
	if prev, _ := doc.Find(`a[rel="prev"]`).Attr("href"); prev != "/doc/docscreentest.Sample?"+(docscreen.ListQuery{Search: "steel", Sort: "amount.desc", Filters: want.Filters, Page: 1, Size: 50}).Encode() {
		t.Errorf("prev = %q", prev)
	}
}

func TestDocList_HTMXGetsMainOnly(t *testing.T) {
	e := newEnv(t, "read")
	e.lister.Set(docscreentest.Sample, twoRows(2))
	rec := e.get(t, "/doc/docscreentest.Sample", "HX-Request", "true")
	// The page's <title> leads (R2), then #main.
	title, frag, _ := strings.Cut(rec.Body.String(), "</title>")
	if !strings.HasPrefix(title, "<title>Sample Orders · ") || !strings.HasPrefix(frag, "<main") {
		t.Errorf("body starts %.80q, want the list's <title>, then <main", rec.Body.String())
	}
	if !slices.Contains(rec.Header().Values("Vary"), "HX-Request") {
		t.Errorf("Vary = %q", rec.Header().Values("Vary"))
	}
}

func TestDocList_ForbiddenUsesAuthMessage(t *testing.T) {
	e := newEnv(t)
	authErr := e.az.Can(context.Background(), priya, docscreentest.Sample, "read", nil)
	if !errors.Is(authErr, auth.ErrForbidden) {
		t.Fatalf("Can without a grant = %v, want auth.ErrForbidden", authErr)
	}
	rec := e.get(t, "/doc/docscreentest.Sample")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if text := uitest.Doc(t, rec).Text(); !strings.Contains(text, authErr.Error()) {
		t.Errorf("body lacks AUTH's message %q", authErr.Error())
	}
	if e.lister.Calls() != 0 {
		t.Error("Lister called without the read right")
	}
}

func TestListColumns_FromListPriority(t *testing.T) {
	docscreentest.RegisterSample()
	dt, ok := doctype.Lookup(docscreentest.Sample)
	if !ok {
		t.Fatal("sample DocType not registered")
	}
	got := docscreen.ListColumns(dt)
	want := []components.Column{
		{Key: "amount", Label: "Amount", Priority: components.PFigure, Num: true},
		{Key: "state", Label: "State", Priority: components.PState},
		{Key: "order_date", Label: "Order date", Priority: components.PMeta},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListColumns = %+v, want %+v", got, want)
	}

	wide := &doctype.DocType{Name: "docscreentest.Wide"}
	for i := range 9 {
		wide.Fields = append(wide.Fields, doctype.Field{Name: string(rune('a' + i)), ListPriority: 5})
	}
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "docscreentest.Wide") {
			t.Errorf("9 columns: recover() = %v, want a panic naming the DocType", r)
		}
	}()
	docscreen.ListColumns(wide)
}

// CD-366 (UI v2 §5.14): with no search or filter an empty list says "yet"
// with the plain line and no action (New is in the page head and the bottom
// nav), for a creator and a reader alike; when a search or a filter hides
// every row it says nothing matched and offers Clear filters, which keeps
// the sort and size but drops the search and every filter. The message never
// repeats a filter's value.
func TestDocList_EmptyState(t *testing.T) {
	plain := "/doc/docscreentest.Sample?size=50&sort=amount.desc"
	for _, c := range []struct {
		name               string
		rights             []auth.Right
		target, text, line string
		action, href       string // "" action: no link at all
	}{
		{"no search or filter", []auth.Right{"read", "create"}, "/doc/docscreentest.Sample", "No sample orders yet", "Sample orders you add show up here.", "", ""},
		{"no search or filter, read only", []auth.Right{"read"}, "/doc/docscreentest.Sample", "No sample orders yet", "Sample orders you add show up here.", "", ""},
		{"search", []auth.Right{"read", "create"}, "/doc/docscreentest.Sample?q=no-such-thing-zz&sort=amount.desc&size=50", `No sample orders match "no-such-thing-zz"`, "Try fewer words, or clear the filters.", "Clear filters", plain},
		{"filter, no search", []auth.Right{"read", "create"}, "/doc/docscreentest.Sample?f=state:=:draft&sort=amount.desc&size=50", "No sample orders match these filters", "Try fewer filters, or clear them.", "Clear filters", plain},
		{"search and filter", []auth.Right{"read", "create"}, "/doc/docscreentest.Sample?q=zz&f=state:=:draft&sort=amount.desc&size=50", `No sample orders match "zz"`, "Try fewer words, or clear the filters.", "Clear filters", plain},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t, c.rights...)
			rec := e.get(t, c.target)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			empty := uitest.Doc(t, rec).Find(".empty-state")
			if got := strings.TrimSpace(empty.Find("p.empty-state-text").Text()); got != c.text {
				t.Errorf("empty message = %q, want %q", got, c.text)
			}
			if got := strings.TrimSpace(empty.Find("p.empty-state-line").Text()); got != c.line {
				t.Errorf("empty line = %q, want %q", got, c.line)
			}
			if strings.Contains(empty.Text(), "draft") || strings.Contains(empty.Text(), "Draft") {
				t.Errorf("empty state %q repeats the filter value", empty.Text())
			}
			a := empty.Find("a")
			if c.action == "" {
				if a.Length() != 0 {
					t.Errorf("empty state has %d links, want none", a.Length())
				}
				return
			}
			if a.Length() != 1 || a.AttrOr("href", "") != c.href || strings.TrimSpace(a.Text()) != c.action {
				t.Errorf("empty action = %d %q %q, want %q to %q", a.Length(), a.AttrOr("href", ""), strings.TrimSpace(a.Text()), c.action, c.href)
			}
		})
	}
}

// A filter on a field the module hides on the list is dropped before the
// Lister runs (CD-334), so it doesn't make the list "filtered": the empty
// state says "yet" and never shows the hidden value.
func TestDocList_EmptyStateIgnoresHiddenFilter(t *testing.T) {
	registerM54Sorted()
	e := newDocEnv(t, "read")
	e.az.Grant("Buyer", m54Sorted, "read", auth.ScopeAll)
	e.d.Modules = map[string]docscreen.Module{m54Sorted: {Hide: func(c docscreen.Ctx, f doctype.Field) bool { return c.Listing && f.Name == "secret" }}}
	e.remount()
	page := m01bOK(t, e.get(t, "/doc/"+m54Sorted+"?f=secret:=:zq-secret"), "GET with a filter on hidden Secret")
	if got := strings.TrimSpace(page.Find(".empty-state p.empty-state-text").Text()); got != "No sorteds yet" {
		t.Errorf("empty message = %q, want No sorteds yet", got)
	}
	if strings.Contains(page.Find("#main").Text(), "zq-secret") {
		t.Error("the list shows the hidden filter's value")
	}
}

func TestDocList_PastTheEndHasNoEmptyState(t *testing.T) {
	e := newEnv(t, "read", "create")
	e.lister.Set(docscreentest.Sample, types.Result[docscreen.ListRow]{Total: 30})
	doc := uitest.Doc(t, e.get(t, "/doc/docscreentest.Sample?page=5"))
	if doc.Find(".empty-state").Length() != 0 {
		t.Error("empty state shown on a page past the end of 30 results")
	}
	if !strings.Contains(doc.Text(), "No more results.") {
		t.Error("no \"No more results.\" past the end")
	}
}

func TestRoutes_AllRegistered(t *testing.T) {
	e := newEnv(t)
	var got []string
	if err := chi.Walk(e.h.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got = append(got, method+" "+route)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /doc/{doctype}",
		"GET /doc/{doctype}/new",
		"POST /doc/{doctype}/new",
		"GET /doc/{doctype}/{id}",
		"GET /doc/{doctype}/{id}/edit/{step}",
		"POST /doc/{doctype}/{id}/edit/{step}",
		"GET /doc/{doctype}/{id}/lines/new",
		"POST /doc/{doctype}/{id}/lines/new",
		"GET /doc/{doctype}/{id}/lines/{no}",
		"POST /doc/{doctype}/{id}/lines/{no}",
		"GET /doc/{doctype}/{id}/lines/{no}/remove",
		"POST /doc/{doctype}/{id}/lines/{no}/remove",
		"POST /doc/{doctype}/{id}/lines/{no}/move",
		"GET /doc/{doctype}/{id}/{action}",
		"POST /doc/{doctype}/{id}/{action}",
		"GET /search",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("routes:\n got %q\nwant %q", got, want)
	}
}
