package docscreen_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// Recent picks (UI v2 §5.8, A-17) on the real auth.Service over one
// database: a header Link's picker offers the values the user chose on their
// own last 20 documents of the DocType, read through Auth.Scope, keeping only
// masters the user can still read and that are enabled (CD-233 family).

const (
	rpSuresh  = "suresh@vardhan.test" // Stores, restricted to PUNE-WH and METRO-SITE
	rpPriya   = "priya@vardhan.test"
	rpBuyer   = "rp-buyer@vardhan.test"
	rpCreator = "rp-creator@vardhan.test"
)

type rpEnv struct {
	w  *authtest.World
	d  docscreen.Deps
	as string
}

func newRecentEnv(t *testing.T) *rpEnv {
	t.Helper()
	authtest.RegisterDocTypes()
	w := authtest.NewWorld(t)
	w.Grant(t, "Stores", authtest.DTOrder, auth.RightCreate, auth.ScopeAll) // Stores already reads every order
	w.Grant(t, "Stores", authtest.DTProject, auth.RightRead, auth.ScopeAll)
	e := &rpEnv{w: w, as: rpSuresh}
	e.d = docscreen.Deps{
		DB:        w.DB,
		Engine:    docfake.New(),
		Auth:      w.Auth,
		Approval:  &approver{},
		Lister:    docscreentest.NewLister(),
		Extras:    docscreentest.NewExtras(),
		Principal: func(*http.Request) (auth.Principal, bool) { return w.P(t, e.as), true },
		Today:     func() (string, string) { return "24-10-2026", "23-10-2026" },
		LinkRead: func(ctx context.Context, tx *db.Tx, p auth.Principal, target *doctype.DocType, id int64) error {
			return w.Auth.CheckLinkTx(ctx, tx, p, target.Name, id)
		},
	}
	return e
}

// order inserts an order by email with site and project codes ("" = none),
// created at "mm-dd hh" in 2026, and returns its id.
func (e *rpEnv) order(t *testing.T, email, site, project, at string) int64 {
	t.Helper()
	ids := map[string]any{"": nil}
	for code, id := range e.w.Sites {
		ids[code] = id
	}
	for code, id := range e.w.Projects {
		ids[code] = id
	}
	siteID, ok1 := ids[site]
	projectID, ok2 := ids[project]
	if !ok1 || !ok2 {
		t.Fatalf("unknown site %q or project %q", site, project)
	}
	u := e.w.P(t, email).ID
	var id int64
	if err := e.w.DB.Write(t.Context(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(t.Context(),
			`INSERT INTO probe_order (state, created_by, created_at, site_id, project_id) VALUES ('draft', ?, ?, ?, ?) RETURNING id`,
			int64(u), "2026-"+at[:5]+"T"+at[6:]+":00:00Z", siteID, projectID).Scan(&id)
	}); err != nil {
		t.Fatalf("insert order: %v", err)
	}
	return id
}

// site adds a probe site with code; restrictions name sites by code.
func (e *rpEnv) site(t *testing.T, code string) {
	t.Helper()
	var id int64
	if err := e.w.DB.Write(t.Context(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(t.Context(), `INSERT INTO probe_site (code, name) VALUES (?, ?) RETURNING id`, code, code).Scan(&id)
	}); err != nil {
		t.Fatalf("insert site: %v", err)
	}
	e.w.Sites[code] = id
}

// newPage is the new order's first step as e.as.
func (e *rpEnv) newPage(t *testing.T) *goquery.Document {
	t.Helper()
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(ui.WithShell(r.Context(), ui.Shell{CSRF: csrfToken})))
		})
	})
	docscreen.Routes(e.d)(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/doc/"+authtest.DTOrder+"/new", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET new order as %s: status %d: %.300s", e.as, rec.Code, rec.Body.String())
	}
	doc, err := goquery.NewDocumentFromReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// recent is the labels of the recent picks waiting in field's picker, in
// order; nil when the picker has none.
func recent(t *testing.T, doc *goquery.Document, field string) []string {
	t.Helper()
	picker := doc.Find(`.picker:has(input[type="hidden"][name="` + field + `"])`)
	if picker.Length() != 1 {
		t.Fatalf("%d pickers for %s", picker.Length(), field)
	}
	tpl := picker.Find("template[data-picker-recent]")
	if tpl.Length() == 0 {
		return nil
	}
	h, err := tpl.Html()
	if err != nil {
		t.Fatal(err)
	}
	inner, err := goquery.NewDocumentFromReader(strings.NewReader(h))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	inner.Find(`[role="option"]`).Each(func(_ int, o *goquery.Selection) { out = append(out, o.AttrOr("data-label", "")) })
	return out
}

func wantRecent(t *testing.T, doc *goquery.Document, field string, want ...string) {
	t.Helper()
	if got := recent(t, doc, field); !slices.Equal(got, want) {
		t.Errorf("%s recent picks = %q, want %q", field, got, want)
	}
}

// A site-restricted user's earlier order at a site they no longer have names
// neither that site nor anything else on it; the order at a site they still
// have shows its site and project; another user's order shows nothing.
func TestRecentPicks_RestrictedUser(t *testing.T) {
	e := newRecentEnv(t)
	e.w.Grant(t, "Stores", authtest.DTSite, auth.RightRead, auth.ScopeAll)
	e.order(t, rpSuresh, "PUNE-WH", "PRJ-METRO", "10-20 10")      // still allowed
	e.order(t, rpSuresh, "TECH-SITE", "PRJ-TECHPARK", "10-22 10") // newest, at a site Suresh lost
	e.order(t, rpPriya, "METRO-SITE", "PRJ-METRO", "10-23 10")    // not his: readable, but not his pick
	doc := e.newPage(t)
	wantRecent(t, doc, "site_id", "PUNE-WH PUNE-WH")
	wantRecent(t, doc, "project_id", "PRJ-METRO Pune Metro Depot")

	// Positive row on the other side: an unrestricted reader sees the order
	// at TECH-SITE as their own pick.
	e.w.Restrict(t, rpSuresh, nil, nil)
	doc = e.newPage(t)
	wantRecent(t, doc, "site_id", "TECH-SITE TECH-SITE", "PUNE-WH PUNE-WH")
	wantRecent(t, doc, "project_id", "PRJ-TECHPARK Whitefield Tech Park, Bengaluru", "PRJ-METRO Pune Metro Depot")
}

// A master the user can't read, or a disabled one, is never offered, even
// from a document they can read.
func TestRecentPicks_UnreadableAndDisabledMasters(t *testing.T) {
	e := newRecentEnv(t) // Stores reads projects but not sites
	e.order(t, rpSuresh, "PUNE-WH", "PRJ-METRO", "10-20 10")
	doc := e.newPage(t)
	wantRecent(t, doc, "site_id")
	wantRecent(t, doc, "project_id", "PRJ-METRO Pune Metro Depot")

	e.w.SetMasterEnabled(t, authtest.DTProject, "PRJ-METRO", false)
	wantRecent(t, e.newPage(t), "project_id")
}

// The scope is read's: a user holding only read and create (the page
// needs create; Grant adds read) sees their own pick, so a scope asked for
// write, submit or print would hide it.
func TestRecentPicks_ReadRightOnly(t *testing.T) {
	e := newRecentEnv(t)
	e.w.Grant(t, "RP Creator", authtest.DTOrder, auth.RightCreate, auth.ScopeAll)
	e.w.Grant(t, "RP Creator", authtest.DTSite, auth.RightRead, auth.ScopeAll)
	e.w.AddUser(t, rpCreator, "Chitra Creator", "RP Creator")
	e.order(t, rpCreator, "PUNE-WH", "", "10-20 10")
	e.as = rpCreator
	wantRecent(t, e.newPage(t), "site_id", "PUNE-WH PUNE-WH")
}

// Only the last 20 orders by creation time count; ids out of date order and
// one back-dated order make a dropped or changed ORDER BY and a larger LIMIT
// show S-OLD. Values come newest first, each once.
func TestRecentPicks_Last20NewestFirst(t *testing.T) {
	e := newRecentEnv(t)
	e.w.Grant(t, "RP Buyer", authtest.DTOrder, auth.RightRead, auth.ScopeAll)
	e.w.Grant(t, "RP Buyer", authtest.DTOrder, auth.RightCreate, auth.ScopeAll)
	e.w.Grant(t, "RP Buyer", authtest.DTSite, auth.RightRead, auth.ScopeAll)
	e.w.AddUser(t, rpBuyer, "Rita Buyer", "RP Buyer")
	for _, c := range []string{"S-NEW", "S-A", "S-OLD", "S-MID"} {
		e.site(t, c)
	}
	for i := 1; i <= 21; i++ {
		switch i {
		case 11:
			e.order(t, rpBuyer, "S-OLD", "", "09-01 09") // the 11th id, created first of all (1 September)
		case 5:
			e.order(t, rpBuyer, "S-MID", "", "10-05 23") // between S-A orders: listed at its oldest place, S-A would come after it
		case 20:
			e.order(t, rpBuyer, "S-A", "", "10-30 10") // created with S-NEW's order: the later id is newer
		case 21:
			e.order(t, rpBuyer, "S-NEW", "", "10-30 10")
		default:
			e.order(t, rpBuyer, "S-A", "", fmt.Sprintf("10-%02d 10", i))
		}
	}
	e.as = rpBuyer
	wantRecent(t, e.newPage(t), "site_id", "S-NEW S-NEW", "S-A S-A", "S-MID S-MID")
}

// A line Link never gets recent picks, even rendered with a header step's
// Deps (as a module's Top or Body may): only header fields are read.
func TestRecentPicks_HeaderFieldsOnly(t *testing.T) {
	e := newRecentEnv(t)
	e.w.Grant(t, "Stores", authtest.DTSite, auth.RightRead, auth.ScopeAll)
	e.order(t, rpSuresh, "PUNE-WH", "", "10-20 10")
	dt, _ := doctype.Lookup(authtest.DTOrder)
	line, _ := dt.Field("line_site_id")
	e.d.Modules = map[string]docscreen.Module{authtest.DTOrder: {Steps: map[string]docscreen.Step{
		"details": {Top: func(c docscreen.Ctx) (templ.Component, error) {
			return docscreen.FieldInput(line, components.FieldID("line_site_id"), "", "", c.Deps), nil
		}},
	}}}
	doc := e.newPage(t)
	wantRecent(t, doc, "site_id", "PUNE-WH PUNE-WH")
	wantRecent(t, doc, "line_site_id")
}
