package docscreen_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// Header search on the real stack (design-system §6, CD-200 part 4, AUTH-106's
// "global search"): real sessions and AUTH's Self guard, the real
// auth.Service and list.Service, one database. /search is outside /doc/, so
// CheckedInHandler can't guard it (CD-87); WP3 mounts it with Self, as here.

// Copies of AUTH's probe GRN and order with SearchFields. They live on the
// probe tables; no role has a right on them until a test grants it.
const (
	realGRN   = "docscreentest.SearchGRN"
	realOrder = "docscreentest.SearchPO"

	realGRNs   = "GRNs"
	realOrders = "Purchase Orders"

	realPassword   = "Correct-Horse-42"
	realRestricted = "suresh@vardhan.test" // Stores, restricted to PUNE-WH and METRO-SITE
	realOpen       = "search-open@vardhan.test"
)

var realRegister sync.Once

func registerRealSearchTypes() {
	authtest.RegisterDocTypes()
	realRegister.Do(func() {
		for _, c := range []struct{ from, name string }{{authtest.DTReceipt, realGRN}, {authtest.DTOrder, realOrder}} {
			base, ok := doctype.Lookup(c.from)
			if !ok {
				panic("docscreen_test: no DocType " + c.from)
			}
			dt := *base
			dt.Name, dt.SearchFields, dt.DefaultRights = c.name, []string{"number"}, nil
			doctype.Register(dt)
		}
	})
}

// realLister is the docscreen.Lister WP3 writes (CD-78) over the real
// list.Service, which checks read and ANDs the read scope into both the rows
// and the count.
type realLister struct{ svc *list.Service }

func (l realLister) List(ctx context.Context, p auth.Principal, dt *doctype.DocType, q docscreen.ListQuery) (types.Result[docscreen.ListRow], error) {
	res, err := l.svc.List(ctx, p, dt, list.Query{Search: q.Search, Page: max(q.Page, 1), PageSize: q.Size})
	if err != nil {
		return types.Result[docscreen.ListRow]{}, err
	}
	out := types.Result[docscreen.ListRow]{Total: res.Total}
	for _, r := range res.Rows {
		number, _ := r.Values["number"].(string)
		out.Rows = append(out.Rows, docscreen.ListRow{
			Info:   doctype.Info{DocType: dt.Name, ID: r.ID, State: doctype.StateApproved, Number: number, Display: number},
			Values: map[string]string{},
		})
	}
	return out, nil
}

type realEnv struct {
	w      *authtest.World
	h      *webtest.Harness
	lister realLister
}

// newRealSearch mounts the docscreen routes' /search behind AUTH's Self guard.
// Stores reads realGRN at scope all. realOpen holds Stores too, has no
// restriction and also reads realOrder; Suresh does not read realOrder.
func newRealSearch(t *testing.T) *realEnv {
	t.Helper()
	registerRealSearchTypes()
	w := authtest.NewWorld(t)
	w.Grant(t, "Stores", realGRN, "read", auth.ScopeAll)
	w.Grant(t, "Search Buyer", realOrder, "read", auth.ScopeAll)
	w.AddUser(t, realOpen, "Olga Open", "Stores", "Search Buyer")

	l := realLister{&list.Service{DB: w.DB, Auth: w.Auth}}
	screens := chi.NewRouter()
	docscreen.Routes(docscreen.Deps{ // Principal nil: AUTH's context principal
		DB:       w.DB,
		Engine:   docfake.New(),
		Auth:     w.Auth,
		Approval: &approver{},
		Lister:   l,
		Extras:   docscreentest.NewExtras(),
	})(screens)
	h := webtest.New(t, w, webtest.WithRoutes(func(r chi.Router, s *authweb.Server) {
		r.Method(http.MethodGet, "/search", s.Self(screens))
	}))
	return &realEnv{w: w, h: h, lister: l}
}

func (e *realEnv) client(t *testing.T, email string) *webtest.Client {
	t.Helper()
	e.w.SetPassword(t, email, realPassword)
	c := e.h.Client()
	c.Login(email, realPassword)
	return c
}

func (e *realEnv) insert(t *testing.T, table, number, siteCode string) int64 {
	t.Helper()
	site, ok := e.w.Sites[siteCode]
	if !ok {
		t.Fatalf("no site %q", siteCode)
	}
	var id int64
	if err := e.w.DB.Write(t.Context(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(t.Context(),
			`INSERT INTO `+table+` (number, state, site_id) VALUES (?, 'approved', ?) RETURNING id`, number, site).Scan(&id)
	}); err != nil {
		t.Fatalf("insert %s: %v", number, err)
	}
	return id
}

// wantListTotal checks the fixture itself, on the same list path the screen
// is given: email's list for q has total rows.
func (e *realEnv) wantListTotal(t *testing.T, email, name, q string, total int) {
	t.Helper()
	dt, ok := doctype.Lookup(name)
	if !ok {
		t.Fatalf("no DocType %s", name)
	}
	res, err := e.lister.List(t.Context(), e.w.P(t, email), dt, docscreen.ListQuery{Search: q, Page: 1})
	if err != nil {
		t.Fatalf("fixture: %s lists %s: %v", email, name, err)
	}
	if res.Total != total {
		t.Fatalf("fixture: %s lists %d %s rows for %q, want %d", email, res.Total, name, q, total)
	}
}

func realSearch(t *testing.T, c *webtest.Client, q string) *goquery.Document {
	t.Helper()
	r := c.Get("/search?q=" + url.QueryEscape(q))
	if r.Status != http.StatusOK {
		t.Fatalf("GET /search?q=%q: status = %d, want 200; body %.300s", q, r.Status, r.Body)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(r.Body))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// AUTH-106 (c): Suresh, restricted to PUNE-WH and METRO-SITE, can't find
// GR/26-27/00002 at TECH-SITE: search neither returns nor confirms it.
func TestSearch_RealAuth_RestrictedUserCannotFindHiddenRecord(t *testing.T) {
	e := newRealSearch(t)
	const hiddenNo = "GR/26-27/00002"
	seen := e.insert(t, "probe_receipt", "GR/26-27/00001", "PUNE-WH")
	hidden := e.insert(t, "probe_receipt", hiddenNo, "TECH-SITE")
	e.wantListTotal(t, realRestricted, realGRN, "GR/26-27", 1)
	e.wantListTotal(t, realOpen, realGRN, "GR/26-27", 2)
	suresh, open := e.client(t, realRestricted), e.client(t, realOpen)
	hiddenHref := "/doc/" + realGRN + "/" + strconv.FormatInt(hidden, 10)

	doc := realSearch(t, suresh, hiddenNo)
	wantSearchPage(t, doc)
	wantNothingFound(t, doc, hiddenNo)
	wantNoGroup(t, doc, realGRNs)
	if doc.Find(`a[href="`+hiddenHref+`"]`).Length() != 0 {
		t.Error("search links the hidden record")
	}

	doc = realSearch(t, suresh, "GR/26-27")
	g := wantGroup(t, doc, realGRNs, 1)
	want := "/doc/" + realGRN + "/" + strconv.FormatInt(seen, 10)
	if len(g.hits) != 1 || g.hits[0].title != "GR/26-27/00001" || g.hits[0].href != want {
		t.Errorf("Suresh's rows = %+v, want only GR/26-27/00001 at %s", g.hits, want)
	}
	if text := doc.Find("main").Text(); strings.Contains(text, hiddenNo) {
		t.Errorf("page confirms the hidden record: %q", squeeze(text))
	}

	// The restriction is what hides it: an unrestricted user with the same
	// role finds it.
	doc = realSearch(t, open, hiddenNo)
	g = wantGroup(t, doc, realGRNs, 1)
	if len(g.hits) != 1 || g.hits[0].href != hiddenHref {
		t.Errorf("unrestricted rows = %+v, want the record at %s", g.hits, hiddenHref)
	}
}

// The count and See all come from the restricted total, so they don't reveal
// how many records are hidden, and the five-row cap holds on the real list
// path.
func TestSearch_RealAuth_CountAndSeeAllLeakNoHiddenRecords(t *testing.T) {
	e := newRealSearch(t)
	for i := range 6 {
		e.insert(t, "probe_receipt", fmt.Sprintf("GR/26-27/%05d", i+1), "PUNE-WH")
	}
	for i := range 3 {
		e.insert(t, "probe_receipt", fmt.Sprintf("GR/26-27/%05d", i+7), "TECH-SITE")
	}
	e.wantListTotal(t, realRestricted, realGRN, "GR/26-27", 6)
	e.wantListTotal(t, realOpen, realGRN, "GR/26-27", 9)

	for _, c := range []struct {
		email string
		total int
	}{{realRestricted, 6}, {realOpen, 9}} {
		t.Run(c.email, func(t *testing.T) {
			g := wantGroup(t, realSearch(t, e.client(t, c.email), "GR/26-27"), realGRNs, c.total)
			if len(g.hits) != 5 {
				t.Errorf("%d rows shown, want 5", len(g.hits))
			}
			wantSeeAll(t, g, "/doc/"+realGRN, c.total, "GR/26-27")
		})
	}
}

func TestSearch_RealAuth_UnreadableDocTypeIsAbsent(t *testing.T) {
	e := newRealSearch(t)
	e.insert(t, "probe_receipt", "GR/26-27/00001", "PUNE-WH")
	e.insert(t, "probe_order", "GR/26-27/90001", "PUNE-WH")
	e.wantListTotal(t, realOpen, realOrder, "GR/26-27", 1)

	doc := realSearch(t, e.client(t, realRestricted), "GR/26-27")
	wantGroup(t, doc, realGRNs, 1)
	wantNoGroup(t, doc, realOrders)

	doc = realSearch(t, e.client(t, realOpen), "GR/26-27")
	wantGroup(t, doc, realGRNs, 1)
	wantGroup(t, doc, realOrders, 1)
}

// Signed out, /search gets AUTH's normal answer; signed in, Self lets it
// through to the page, and AUTH's route inventory accepts the mount.
func TestSearch_RealAuth_GuardedBySelf(t *testing.T) {
	e := newRealSearch(t)
	if got := authweb.CheckRoutes(e.h.Router); len(got) != 0 {
		t.Errorf("CheckRoutes = %q, want none", got)
	}

	r := e.h.Client().Get("/search?q=steel")
	if r.Status != http.StatusSeeOther {
		t.Fatalf("signed out: status = %d, want 303", r.Status)
	}
	if got, want := r.Header.Get("Location"), "/signin?next=%2Fsearch%3Fq%3Dsteel"; got != want {
		t.Errorf("signed out: Location = %q, want %q", got, want)
	}

	wantSearchPage(t, realSearch(t, e.client(t, realRestricted), "steel"))
}
