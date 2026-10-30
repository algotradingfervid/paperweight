package docscreen_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// Header search (design-system §6, CD-200 part 4). These are the screen's
// tests over authfake; search_real_test.go runs it on the real auth.Service
// and list.Service (AUTH-106).

// The DocTypes the search screen is tested on (CD-65 prefix). docscreentest's
// Sample has no SearchFields.
const (
	srchOrder  = "docscreentest.SearchOrder"
	srchParty  = "docscreentest.SearchParty"
	srchSecret = "docscreentest.SearchSecret"
	srchPlain  = "docscreentest.SearchPlain" // readable, no SearchFields

	srchPrompt = "Type a name, code or number to search."
	srchMaxLen = 100
)

var srchRegister sync.Once

// srchMaster is the SearchParty master lookup; the screen never calls it.
type srchMaster struct{}

func (srchMaster) Lookup(context.Context, *db.Tx, int64) (doctype.MasterInfo, bool, error) {
	return doctype.MasterInfo{}, false, nil
}

func registerSearchTypes() {
	srchRegister.Do(func() {
		fields := []doctype.Field{
			{Name: "number", Label: "Number", Type: doctype.Text},
			{Name: "party_name", Label: "Party", Type: doctype.Text},
		}
		search := []string{"number", "party_name"}
		doctype.Register(doctype.DocType{
			Name: srchOrder, Label: "Search Order", PluralLabel: "Search Orders",
			Table: "docscreentest_search_order", Fields: fields, SearchFields: search,
		})
		doctype.Register(doctype.DocType{
			Name: srchSecret, Label: "Search Secret", PluralLabel: "Search Secrets",
			Table: "docscreentest_search_secret", Fields: fields, SearchFields: search,
		})
		doctype.Register(doctype.DocType{
			Name: srchPlain, Label: "Plain Order", PluralLabel: "Plain Orders",
			Table: "docscreentest_search_plain", Fields: fields,
		})
		doctype.Register(doctype.DocType{
			Name: srchParty, Label: "Search Party", PluralLabel: "Search Parties",
			Table: "docscreentest_search_party", Master: srchMaster{},
			Fields: []doctype.Field{
				{Name: "code", Label: "Code", Type: doctype.Text},
				{Name: "name", Label: "Name", Type: doctype.Text},
				{Name: "disabled", Label: "Disabled", Type: doctype.Check},
			},
			SearchFields: []string{"code", "name"},
		})
	})
}

// srchLister is a docscreen.Lister over fixed rows that refuses what the
// real path refuses: it asks AUTH for the read right and scope, and hands the
// query to list.Build, so a page size or page the list can't serve is an
// error here too. It serves the query's page size, and Total counts every
// row, as the real count does.
type srchLister struct {
	az auth.Service

	mu      sync.Mutex
	rows    map[string][]docscreen.ListRow
	queries map[string][]docscreen.ListQuery
	fail    error
}

func (l *srchLister) set(name string, rows ...docscreen.ListRow) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.rows[name] = rows
}

func (l *srchLister) queried(name string) []docscreen.ListQuery {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.queries[name])
}

func (l *srchLister) calls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, q := range l.queries {
		n += len(q)
	}
	return n
}

func (l *srchLister) List(ctx context.Context, p auth.Principal, dt *doctype.DocType, q docscreen.ListQuery) (types.Result[docscreen.ListRow], error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var none types.Result[docscreen.ListRow]
	l.queries[dt.Name] = append(l.queries[dt.Name], q)
	if l.fail != nil {
		return none, l.fail
	}
	if err := l.az.Can(ctx, p, dt.Name, "read", nil); err != nil {
		return none, err
	}
	scope, err := l.az.Scope(ctx, p, dt.Name, "read")
	if err != nil {
		return none, err
	}
	if _, _, err := list.Build(dt, list.Query{Search: q.Search, Page: max(q.Page, 1), PageSize: q.Size}, scope); err != nil {
		return none, err
	}
	all := l.rows[dt.Name]
	size := q.Size
	if size == 0 {
		size = list.PageSizes[0]
	}
	return types.Result[docscreen.ListRow]{Total: len(all), Rows: all[:min(size, len(all))]}, nil
}

type srchEnv struct {
	lister *srchLister
	d      docscreen.Deps
}

// newSearchEnv gives Priya, through the Buyer role, read on the named
// DocTypes and nothing else.
func newSearchEnv(t *testing.T, readable ...string) *srchEnv {
	t.Helper()
	registerSearchTypes()
	az := authfake.New()
	for _, name := range readable {
		az.Grant("Buyer", name, "read", auth.ScopeAll)
	}
	az.AddRole(priya.ID, "Buyer")
	l := &srchLister{az: az, rows: map[string][]docscreen.ListRow{}, queries: map[string][]docscreen.ListQuery{}}
	return &srchEnv{lister: l, d: docscreen.Deps{
		DB:        &docscreentest.Reader{},
		Engine:    docfake.New(),
		Auth:      az,
		Approval:  &approver{},
		Lister:    l,
		Extras:    docscreentest.NewExtras(),
		Principal: func(*http.Request) (auth.Principal, bool) { return priya, true },
	}}
}

func (e *srchEnv) get(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	docscreen.Routes(e.d)(r)
	return uitest.Do(t, r, http.MethodGet, target, nil)
}

// search gets /search for the text q and fails t unless the answer is 200.
func (e *srchEnv) search(t *testing.T, q string) (*httptest.ResponseRecorder, *goquery.Document) {
	t.Helper()
	rec := e.get(t, "/search?q="+url.QueryEscape(q))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /search?q=%q: status = %d, want 200; body %.300s", q, rec.Code, rec.Body)
	}
	return rec, uitest.Doc(t, rec)
}

func srchRow(name string, id int64, display, party string) docscreen.ListRow {
	return docscreen.ListRow{
		Info:   doctype.Info{DocType: name, ID: id, State: doctype.StateApproved, Number: display, Display: display, Party: party},
		Values: map[string]string{},
	}
}

// srchRows is n rows of name with ids 1 to n.
func srchRows(name string, n int) []docscreen.ListRow {
	rows := make([]docscreen.ListRow, n)
	for i := range rows {
		id := int64(i + 1)
		rows[i] = srchRow(name, id, fmt.Sprintf("SO/26-27/%05d", id), "")
	}
	return rows
}

type srchHit struct{ title, party, href string }

// srchGroup is one DocType's part of the results page: an h2 (the plural
// label and the count), the DataList after it and its See all link.
type srchGroup struct {
	heading, listLabel string
	hits               []srchHit
	seeAllText, seeAll string
}

func squeeze(s string) string { return strings.Join(strings.Fields(s), " ") }

// srchGroups reads the results page in document order.
func srchGroups(t *testing.T, doc *goquery.Document) []srchGroup {
	t.Helper()
	var out []srchGroup
	doc.Find("main h2, main ul.datalist-rows, main a").Each(func(_ int, s *goquery.Selection) {
		if goquery.NodeName(s) == "h2" {
			out = append(out, srchGroup{heading: squeeze(s.Text())})
			return
		}
		seeAll := strings.HasPrefix(squeeze(s.Text()), "See all")
		isRows := goquery.NodeName(s) == "ul"
		isHit := s.HasClass("datalist-link")
		if !seeAll && !isRows && !isHit {
			return
		}
		if len(out) == 0 {
			t.Fatalf("a result comes before any group heading: %q", squeeze(s.Text()))
		}
		g := &out[len(out)-1]
		switch {
		case isRows:
			g.listLabel = s.AttrOr("aria-label", "")
		case isHit:
			g.hits = append(g.hits, srchHit{
				title: squeeze(s.Find(".datalist-title").Text()),
				party: squeeze(s.Find(".datalist-party").Text()),
				href:  s.AttrOr("href", ""),
			})
		default:
			g.seeAllText, g.seeAll = squeeze(s.Text()), s.AttrOr("href", "")
		}
	})
	return out
}

// srchGroupOf returns the group whose heading names plural, or nil.
func srchGroupOf(groups []srchGroup, plural string) *srchGroup {
	for i := range groups {
		if strings.Contains(groups[i].heading, plural) {
			return &groups[i]
		}
	}
	return nil
}

// wantGroup fails t unless the page has a group under plural whose heading
// carries the count n as a word.
func wantGroup(t *testing.T, doc *goquery.Document, plural string, n int) srchGroup {
	t.Helper()
	groups := srchGroups(t, doc)
	g := srchGroupOf(groups, plural)
	if g == nil {
		t.Fatalf("no group headed %q in %+v", plural, groups)
	}
	words := strings.Fields(strings.ReplaceAll(g.heading, plural, " "))
	if !slices.ContainsFunc(words, func(w string) bool { return strings.Trim(w, "()") == strconv.Itoa(n) }) {
		t.Errorf("heading %q has no count %d", g.heading, n)
	}
	return *g
}

func wantNoGroup(t *testing.T, doc *goquery.Document, plural string) {
	t.Helper()
	if g := srchGroupOf(srchGroups(t, doc), plural); g != nil {
		t.Errorf("group %q shown: %+v", plural, *g)
	}
	if text := doc.Find("main").Text(); strings.Contains(text, plural) {
		t.Errorf("page text names %q: %q", plural, squeeze(text))
	}
}

func wantNothingFound(t *testing.T, doc *goquery.Document, q string) {
	t.Helper()
	want := `Nothing found for "` + q + `".`
	if text := doc.Find("main").Text(); !strings.Contains(text, want) {
		t.Errorf("page lacks %q: %q", want, squeeze(text))
	}
}

func wantSearchPage(t *testing.T, doc *goquery.Document) {
	t.Helper()
	if got := squeeze(doc.Find("main h1").Text()); got != "Search" {
		t.Errorf("h1 = %q, want Search", got)
	}
	if got := doc.Find("title").Text(); !strings.HasPrefix(got, "Search · ") {
		t.Errorf("title = %q, want it to start with %q", got, "Search · ")
	}
}

// wantSeeAll fails t unless g's See all link names n, has path and carries
// exactly q as its only parameter.
func wantSeeAll(t *testing.T, g srchGroup, path string, n int, q string) {
	t.Helper()
	wantText := "See all " + strconv.Itoa(n)
	if g.seeAllText != wantText && !strings.HasPrefix(g.seeAllText, wantText+" ") {
		t.Errorf("See all text = %q, want %q", g.seeAllText, wantText)
	}
	u, err := url.Parse(g.seeAll)
	if err != nil {
		t.Fatalf("See all href %q: %v", g.seeAll, err)
	}
	if u.Path != path {
		t.Errorf("See all path = %q, want %q (href %q)", u.Path, path, g.seeAll)
	}
	if got := u.Query(); len(got) != 1 || len(got["q"]) != 1 || got.Get("q") != q {
		t.Errorf("See all query = %v, want only q=%q (href %q)", got, q, g.seeAll)
	}
}

func TestSearch_GroupsReadableDocTypesUnderPluralLabel(t *testing.T) {
	e := newSearchEnv(t, srchOrder, srchParty)
	e.lister.set(srchOrder,
		srchRow(srchOrder, 42, "PO/26-27/00042", "Shree Steel Traders"),
		srchRow(srchOrder, 7, "Draft 7", "Steel Bharat"),
	)
	e.lister.set(srchParty, srchRow(srchParty, 3, "SHREE", ""))

	_, doc := e.search(t, "steel")
	wantSearchPage(t, doc)

	if groups := srchGroups(t, doc); len(groups) != 2 {
		t.Fatalf("got %d groups, want 2: %+v", len(groups), groups)
	}
	orders := wantGroup(t, doc, "Search Orders", 2)
	if orders.listLabel != "Search Orders" {
		t.Errorf("DataList label = %q, want the plural label", orders.listLabel)
	}
	wantHits := []srchHit{
		{"PO/26-27/00042", "Shree Steel Traders", "/doc/" + srchOrder + "/42"},
		{"Draft 7", "Steel Bharat", "/doc/" + srchOrder + "/7"},
	}
	if !reflect.DeepEqual(orders.hits, wantHits) {
		t.Errorf("order rows = %+v, want %+v", orders.hits, wantHits)
	}
	parties := wantGroup(t, doc, "Search Parties", 1)
	if want := []srchHit{{"SHREE", "", "/doc/" + srchParty + "/3"}}; !reflect.DeepEqual(parties.hits, want) {
		t.Errorf("party rows = %+v, want %+v", parties.hits, want)
	}

	// The master gets the same plain query: no default filter, so a disabled
	// master stays findable.
	for _, name := range []string{srchOrder, srchParty} {
		qs := e.lister.queried(name)
		if len(qs) != 1 {
			t.Fatalf("%s queried %d times, want once", name, len(qs))
		}
		if q := qs[0]; q.Search != "steel" || q.Page != 1 || len(q.Filters) != 0 || q.Sort != "" {
			t.Errorf("%s query = %+v, want Search steel, Page 1, no filter, no sort", name, q)
		}
	}
}

func TestSearch_UnreadableDocTypeHasNoHeadingNoCountNoQuery(t *testing.T) {
	e := newSearchEnv(t, srchOrder)
	e.lister.set(srchOrder, srchRow(srchOrder, 1, "PO/26-27/00001", ""))
	e.lister.set(srchSecret, srchRows(srchSecret, 9)...)

	_, doc := e.search(t, "26-27")
	wantGroup(t, doc, "Search Orders", 1)
	wantNoGroup(t, doc, "Search Secrets")
	if n := len(e.lister.queried(srchSecret)); n != 0 {
		t.Errorf("the unreadable DocType was queried %d times", n)
	}
}

func TestSearch_NoReadableDocTypeLooksLikeNoMatch(t *testing.T) {
	e := newSearchEnv(t)
	e.lister.set(srchSecret, srchRows(srchSecret, 9)...)

	_, doc := e.search(t, "26-27")
	wantSearchPage(t, doc)
	wantNothingFound(t, doc, "26-27")
	wantNoGroup(t, doc, "Search Secrets")
	if n := e.lister.calls(); n != 0 {
		t.Errorf("Lister called %d times with no read right", n)
	}
}

// A DocType with no SearchFields has no search condition, so the list would
// return every row.
func TestSearch_DocTypeWithoutSearchFieldsIsSkipped(t *testing.T) {
	e := newSearchEnv(t, srchOrder, srchPlain)
	e.lister.set(srchOrder, srchRow(srchOrder, 1, "PO/26-27/00001", ""))
	e.lister.set(srchPlain, srchRows(srchPlain, 3)...)

	_, doc := e.search(t, "26-27")
	wantGroup(t, doc, "Search Orders", 1)
	wantNoGroup(t, doc, "Plain Orders")
	if n := len(e.lister.queried(srchPlain)); n != 0 {
		t.Errorf("the DocType with no SearchFields was queried %d times", n)
	}
}

func TestSearch_ReadableDocTypeWithNoMatchHasNoHeading(t *testing.T) {
	e := newSearchEnv(t, srchOrder, srchParty)
	e.lister.set(srchOrder, srchRow(srchOrder, 1, "PO/26-27/00001", ""))

	_, doc := e.search(t, "26-27")
	wantGroup(t, doc, "Search Orders", 1)
	wantNoGroup(t, doc, "Search Parties")
	if text := doc.Find("main").Text(); strings.Contains(text, "Nothing found") {
		t.Errorf("page says nothing was found beside a result: %q", squeeze(text))
	}
}

func TestSearch_CapsAtFiveRowsAndLinksSeeAll(t *testing.T) {
	for _, c := range []struct {
		total, shown int
		seeAll       bool
	}{
		{total: 4, shown: 4},
		{total: 5, shown: 5},
		{total: 6, shown: 5, seeAll: true},
		{total: 30, shown: 5, seeAll: true},
	} {
		t.Run(strconv.Itoa(c.total), func(t *testing.T) {
			e := newSearchEnv(t, srchOrder)
			e.lister.set(srchOrder, srchRows(srchOrder, c.total)...)

			_, doc := e.search(t, "steel")
			g := wantGroup(t, doc, "Search Orders", c.total)
			if len(g.hits) != c.shown {
				t.Fatalf("%d rows shown, want %d", len(g.hits), c.shown)
			}
			for i, h := range g.hits {
				if want := fmt.Sprintf("SO/26-27/%05d", i+1); h.title != want {
					t.Errorf("row %d = %q, want %q: the first rows, in the list's order", i, h.title, want)
				}
			}
			if !c.seeAll {
				if g.seeAll != "" {
					t.Errorf("See all shown for %d rows: %q %q", c.total, g.seeAllText, g.seeAll)
				}
				return
			}
			wantSeeAll(t, g, "/doc/"+srchOrder, c.total, "steel")
		})
	}
}

func TestSearch_BlankQueryPromptsAndRunsNoQuery(t *testing.T) {
	for _, target := range []string{"/search", "/search?q=", "/search?q=%20%20%20", "/search?q=%09%0A"} {
		t.Run(target, func(t *testing.T) {
			e := newSearchEnv(t, srchOrder)
			e.lister.set(srchOrder, srchRows(srchOrder, 3)...)

			rec := e.get(t, target)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			doc := uitest.Doc(t, rec)
			wantSearchPage(t, doc)
			text := doc.Find("main").Text()
			if !strings.Contains(text, srchPrompt) {
				t.Errorf("page lacks %q: %q", srchPrompt, squeeze(text))
			}
			if strings.Contains(text, "Nothing found") {
				t.Errorf("blank query says nothing was found: %q", squeeze(text))
			}
			if n := e.lister.calls(); n != 0 {
				t.Errorf("Lister called %d times for a blank query", n)
			}
			if groups := srchGroups(t, doc); len(groups) != 0 {
				t.Errorf("groups on a blank query: %+v", groups)
			}
		})
	}
}

func TestSearch_NoMatchSaysNothingFound(t *testing.T) {
	e := newSearchEnv(t, srchOrder, srchParty)

	_, doc := e.search(t, "zzz")
	wantSearchPage(t, doc)
	wantNothingFound(t, doc, "zzz")
	if text := doc.Find("main").Text(); strings.Contains(text, srchPrompt) {
		t.Errorf("no-match page shows the blank prompt: %q", squeeze(text))
	}
	if groups := srchGroups(t, doc); len(groups) != 0 {
		t.Errorf("groups on no match: %+v", groups)
	}
	if n := e.lister.calls(); n != 2 {
		t.Errorf("Lister called %d times, want once per searchable readable DocType", n)
	}
}

func TestSearch_QueryIsEscaped(t *testing.T) {
	const script = `<script>alert(1)</script>`
	for _, q := range []string{script, `a&b=c`, `100% "steel"`, `x+y #1/2`, `Bhārat ₹`} {
		t.Run(q, func(t *testing.T) {
			e := newSearchEnv(t, srchOrder)
			rec, doc := e.search(t, q)
			wantNothingFound(t, doc, q)
			if strings.Contains(rec.Body.String(), script) {
				t.Error("the query reaches the page unescaped")
			}

			e.lister.set(srchOrder, srchRows(srchOrder, 6)...)
			rec, doc = e.search(t, q)
			if strings.Contains(rec.Body.String(), script) {
				t.Error("the query reaches the results page unescaped")
			}
			wantSeeAll(t, wantGroup(t, doc, "Search Orders", 6), "/doc/"+srchOrder, 6, q)
			qs := e.lister.queried(srchOrder)
			if got := qs[len(qs)-1].Search; got != q {
				t.Errorf("Lister searched %q, want %q", got, q)
			}
		})
	}
}

func TestSearch_LongQueryIsTruncatedServerSide(t *testing.T) {
	for _, c := range []struct{ name, q, want string }{
		{"over the limit", strings.Repeat("a", srchMaxLen+50), strings.Repeat("a", srchMaxLen)},
		{"at the limit", strings.Repeat("a", srchMaxLen), strings.Repeat("a", srchMaxLen)},
		{"multi-byte characters stay whole", strings.Repeat("₹", srchMaxLen+1), strings.Repeat("₹", srchMaxLen)},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newSearchEnv(t, srchOrder)
			_, doc := e.search(t, c.q)
			qs := e.lister.queried(srchOrder)
			if len(qs) != 1 {
				t.Fatalf("queried %d times, want once", len(qs))
			}
			got := qs[0].Search
			if !utf8.ValidString(got) {
				t.Errorf("searched text is not valid UTF-8: %q", got)
			}
			if got != c.want {
				t.Errorf("searched %d characters (%q), want %d", utf8.RuneCountInString(got), got, utf8.RuneCountInString(c.want))
			}
			wantNothingFound(t, doc, c.want)

			e.lister.set(srchOrder, srchRows(srchOrder, 6)...)
			_, doc = e.search(t, c.q)
			wantSeeAll(t, wantGroup(t, doc, "Search Orders", 6), "/doc/"+srchOrder, 6, c.want)
		})
	}
}

func TestSearch_UsesRecordURLAndListURLWhenSet(t *testing.T) {
	e := newSearchEnv(t, srchOrder, srchParty)
	e.d.RecordURL = func(name string, id int64) string {
		return "/m/" + name + "/" + strconv.FormatInt(id, 10) + "/view"
	}
	e.d.ListURL = func(name string) string { return "/m/" + name + "/all" }
	e.lister.set(srchOrder, srchRow(srchOrder, 42, "PO/26-27/00042", ""))
	e.lister.set(srchParty, srchRows(srchParty, 6)...)

	_, doc := e.search(t, "a b")
	orders := wantGroup(t, doc, "Search Orders", 1)
	if len(orders.hits) != 1 || orders.hits[0].href != "/m/"+srchOrder+"/42/view" {
		t.Errorf("order rows = %+v, want the RecordURL of (%s, 42)", orders.hits, srchOrder)
	}
	parties := wantGroup(t, doc, "Search Parties", 6)
	if len(parties.hits) != 5 || parties.hits[4].href != "/m/"+srchParty+"/5/view" {
		t.Errorf("party rows = %+v, want RecordURL hrefs", parties.hits)
	}
	wantSeeAll(t, parties, "/m/"+srchParty+"/all", 6, "a b")
}

func TestSearch_ListerFailureIsAnErrorNotNothingFound(t *testing.T) {
	e := newSearchEnv(t, srchOrder)
	e.lister.fail = errors.New("disk on fire")

	rec := e.get(t, "/search?q=steel")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "Nothing found") {
		t.Error("a failed query is reported as no match")
	}
}

func TestSearch_RouteIsRegisteredForGetOnly(t *testing.T) {
	e := newSearchEnv(t)
	r := chi.NewRouter()
	docscreen.Routes(e.d)(r)
	var methods []string
	if err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if route == "/search" {
			methods = append(methods, method)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(methods, []string{http.MethodGet}) {
		t.Errorf("/search methods = %q, want only GET", methods)
	}
}

func TestSearch_HasAScreenEntryForTheSweep(t *testing.T) {
	docscreentest.RegisterSample()
	ids := docscreen.SampleIDs{Draft: 42, Pending: 43, Approved: 44, Rejected: 45, Cancelled: 46, Closed: 47, Lines500: 48}
	found := false
	for _, s := range docscreen.Screens(docscreentest.Sample, ids) {
		if s.Route != "/search" {
			continue
		}
		found = true
		if s.URL != "/search" && !strings.HasPrefix(s.URL, "/search?") {
			t.Errorf("screen %q: URL = %q, want /search", s.Name, s.URL)
		}
		if s.As != "priya@vardhan.test" {
			t.Errorf("screen %q: As = %q, want the sweep's signed-in user", s.Name, s.As)
		}
	}
	if !found {
		t.Error("docscreen.Screens has no entry for route /search")
	}
}
