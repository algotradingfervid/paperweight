package app_test

import (
	"context"
	"encoding/csv"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/testkit"
)

// AUTH's WP3 cases on the real router (CD-40, AUTH plan hand-off). AUTH's own
// tests of these ids stay in internal/kernel/auth/web on its probe router as
// the unit level, since that package can't import internal/app; authCases runs
// each one here through app.Handler(), the real engine, APR and MST's
// Directory. Each row names the Go test the case's document maps it to, and
// TestAuthCases_CoverEveryID that nothing fell out of the table.
var authCases = []struct {
	id, goTest string
	run        func(t *testing.T, c *authCase)
}{
	{"AUTH-064", "TestRights_NoRoleNoAccess", caseNoRoleNoAccess},
	{"AUTH-070", "TestRights_AdminCannotBypassLifecycle", caseAdminFollowsLifecycle},
	{"AUTH-071", "TestRights_Read", caseRead},
	{"AUTH-072", "TestRights_Create", caseCreate},
	{"AUTH-073", "TestRights_Write", caseWrite},
	{"AUTH-074", "TestRights_Submit", caseSubmit},
	{"AUTH-075", "TestRights_Approve", caseApprove},
	{"AUTH-076", "TestRights_Cancel", caseCancel},
	{"AUTH-077", "TestRights_Amend", caseAmend},
	{"AUTH-078", "TestRights_Delete", caseDelete},
	{"AUTH-079", "TestRights_Print", casePrint},
	{"AUTH-080", "TestRights_Export", caseExport},
	{"AUTH-081", "TestRights_Import", caseImport},
	{"AUTH-082", "TestRights_LinesFollowParent", caseLinesFollowParent},
	{"AUTH-083", "TestRights_OwnerNotClientSettable", caseOwnerNotClientSettable},
	{"AUTH-085", "TestOwnScope_ListFiltered", caseOwnListFiltered},
	{"AUTH-086", "TestOwnScope_OthersNotFound", caseOwnOthersNotFound},
	{"AUTH-087", "TestOwnScope_WriteDeleteOwnOnly", caseOwnWriteDelete},
	{"AUTH-088", "TestOwnScope_LinkedEmployee", caseOwnLinkedEmployee},
	{"AUTH-089", "TestOwnScope_AllWins", caseOwnAllWins},
	{"AUTH-090", "TestOwnScope_ExportFiltered", caseOwnExportFiltered},
	{"AUTH-091", "TestRestriction_SiteList", caseRestrictionSiteList},
	{"AUTH-092", "TestRestriction_ProjectList", caseRestrictionProjectList},
	{"AUTH-093", "TestRestriction_StrictEmptyHidden", caseRestrictionStrictEmpty},
	{"AUTH-094", "TestRestriction_ChildLinksChecked", caseRestrictionChildLinks},
	{"AUTH-095", "TestRestriction_DimensionsAnd", caseRestrictionDimensionsAnd},
	{"AUTH-096", "TestRestriction_DoesNotGrant", caseRestrictionDoesNotGrant},
	{"AUTH-097", "TestRestriction_AndOwnScope", caseRestrictionAndOwnScope},
	{"AUTH-098", "TestRestriction_LinkPickerFiltered", caseRestrictionPicker},
	{"AUTH-099", "TestRestriction_SaveDisallowedValue", caseRestrictionSaveDisallowed},
	{"AUTH-100", "TestRestriction_SingleValueDefault", caseRestrictionSingleDefault},
	{"AUTH-101", "TestRestriction_LinkedDocumentMustBeVisible", caseRestrictionLinkedDocument},
	{"AUTH-102", "TestRestriction_MasterSelf", caseRestrictionMasterSelf},
	{"AUTH-103", "TestRestriction_AdminOnly", caseRestrictionAdminOnly},
	{"AUTH-104", "TestRestriction_ClearLifts", caseRestrictionClearLifts},
	{"AUTH-105", "TestRestriction_DisabledMasterKept", caseRestrictionDisabledKept},
	{"AUTH-106", "TestScope_AllChannelsConsistent", caseScopeAllChannels},
	{"AUTH-107", "TestRoutes_AllDeclarePermission", caseRoutesDeclarePermission},
	{"AUTH-108", "TestScope_IDGuessingNoLeak", caseScopeIDGuessing},
	{"AUTH-109", "TestScope_AttachmentsFollowParent", caseScopeAttachments},
	{"AUTH-110", "TestScope_FiltersCannotWiden", caseScopeFilters},
	{"AUTH-111", "TestScope_ReportTotals", caseScopeReportTotals},
	{"AUTH-112", "TestScope_APIEqualsUI", caseScopeAPIEqualsUI},
	{"AUTH-115", "TestGuest_OnlyPublicRoutes", caseGuestPublicRoutes},
	{"AUTH-116", "TestTeamScope_DirectReportVisibleAndApprovable", caseTeamDirectReport},
	{"AUTH-117", "TestTeamScope_IndirectReportExcluded", caseTeamIndirectReport},
	{"AUTH-118", "TestTeamScope_LeftEmployeeIgnored", caseTeamLeftEmployee},
}

func TestAuthCases_RealRouter(t *testing.T) {
	seed := newAuthSeed(t)
	for _, c := range authCases {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			c.run(t, seed.open(t))
		})
	}
}

// The ids of the AUTH plan's hand-off: 064, 070, 071-083, 085-112, 115-118.
func TestAuthCases_CoverEveryID(t *testing.T) {
	want := map[string]bool{"AUTH-064": true}
	for _, r := range [][2]int{{70, 83}, {85, 112}, {115, 118}} {
		for n := r[0]; n <= r[1]; n++ {
			want["AUTH-"+strconv.Itoa(1000 + n)[1:]] = true
		}
	}
	for _, c := range authCases {
		if !want[c.id] {
			t.Errorf("%s is not a hand-off id, or is listed twice", c.id)
		}
		delete(want, c.id)
	}
	if len(want) != 0 {
		t.Errorf("no real-router run for %v", want)
	}
}

const (
	kavyaEmail = "kavya@vardhan.test"
	meenaEmail = "meena@vardhan.test"
	clerkEmail = "clerk@vardhan.test"
	viewEmail  = "viewer@vardhan.test"

	roleClerk  = "Purchase Clerk"
	roleViewer = "Purchase Viewer"

	msgNotFound = "This record doesn't exist or you don't have access to it."

	byteOrderMark = "\ufeff"
)

// session is a signed-in browser's cookies and CSRF token.
type session struct {
	cookies []*http.Cookie
	csrf    string
}

// authSeed is a canonical world with every user signed in, kept as a database
// file. Each case opens its own copy, so the password hashes run once, here,
// and no case sees another's rows. On top of canonical it has the fixture's
// Site restriction for Suresh, and the extra users of AUTH-074 and AUTH-079
// with their empty roles.
type authSeed struct {
	file     string
	fx       testkit.Fixtures
	m        fixture.Masters
	sessions map[string]session
}

func newAuthSeed(t *testing.T) *authSeed {
	t.Helper()
	ctx := context.Background()
	w := newWorld(t)
	admin, fx, m := w.canonical()
	s := &authSeed{file: filepath.Join(t.TempDir(), "seed.db"), fx: fx, m: m, sessions: map[string]session{}}
	au := w.app.Auth()
	if err := au.ChangeRestrictions(ctx, admin, sureshID, auth.DimSite, []int64{s.site("PUNE-WH"), s.site("METRO-SITE")}, nil, nil); err != nil {
		t.Fatalf("restrict Suresh: %v", err)
	}
	for _, e := range []struct{ email, name, role string }{
		{clerkEmail, "Purchase Clerk", roleClerk}, {viewEmail, "Purchase Viewer", roleViewer},
	} {
		if err := au.CreateRole(ctx, admin, e.role); err != nil {
			t.Fatalf("CreateRole(%s): %v", e.role, err)
		}
		if _, _, err := au.CreateUser(ctx, admin, auth.NewUser{Email: e.email, Name: e.name, Password: testPassword, Roles: []string{e.role}}); err != nil {
			t.Fatalf("CreateUser(%s): %v", e.email, err)
		}
	}
	u, err := url.Parse(w.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{adminEmail, meenaEmail, raviEmail, priyaEmail, sureshEmail, kavyaEmail, deepakEmail, clerkEmail, viewEmail} {
		b := w.signedIn(email)
		s.sessions[email] = session{cookies: b.c.Jar.Cookies(u), csrf: b.csrf}
	}
	if _, err := w.app.DB().SQL().ExecContext(ctx, `VACUUM INTO ?`, s.file); err != nil {
		t.Fatalf("snapshot the seed: %v", err)
	}
	return s
}

func (s *authSeed) site(code string) int64 {
	for _, x := range s.fx.Sites {
		if x.Code == code {
			return x.ID
		}
	}
	panic("no fixture site " + code)
}

func (s *authSeed) employee(code string) int64 {
	for _, e := range s.fx.Employees {
		if e.Code == code {
			return e.ID
		}
	}
	panic("no fixture employee " + code)
}

// authCase is one case's world, with the seed's users signed in.
type authCase struct {
	*world
	*authSeed
	admin auth.Principal
}

func (s *authSeed) open(t *testing.T) *authCase {
	t.Helper()
	dir := t.TempDir()
	raw, err := os.ReadFile(s.file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.db"), raw, 0o600); err != nil { //nolint:gosec // G703: under t.TempDir
		t.Fatal(err)
	}
	w := newWorld(t, func(c *app.Config) { c.DataDir = dir })
	return &authCase{world: w, authSeed: s, admin: w.principal(adminID)}
}

// as is a browser in email's seeded session.
func (c *authCase) as(email string) *browser {
	c.t.Helper()
	s, ok := c.sessions[email]
	if !ok {
		c.t.Fatalf("no seeded session for %s", email)
	}
	u, err := url.Parse(c.srv.URL)
	if err != nil {
		c.t.Fatal(err)
	}
	b := c.browser()
	b.c.Jar.SetCookies(u, s.cookies)
	b.csrf = s.csrf
	return b
}

// row is a document stored directly, for a case about what an existing
// document does: its state is the case's Given. Zero ids are NULL.
type row struct {
	number, state                string
	by                           auth.UserID
	project, site, employee, amt int64
	lineSites                    []int64
}

func nullable(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func (r row) stateOr() string {
	if r.state == "" {
		return "draft"
	}
	return r.state
}

func (r row) numberOr() any {
	if r.number == "" {
		return nil
	}
	return r.number
}

// insert adds one document row with the convention columns plus cols.
func (w *world) insert(table string, r row, cols string, vals ...any) int64 {
	w.t.Helper()
	at := testkit.Clock().Now().UTC().Format("2006-01-02T15:04:05.000Z")
	args := append([]any{r.numberOr(), r.stateOr(), at, int64(r.by), at, int64(r.by)}, vals...)
	marks := strings.Repeat(", ?", len(vals))
	var id int64
	err := w.app.DB().SQL().QueryRowContext(context.Background(),
		`INSERT INTO `+table+` (number, state, created_at, created_by, updated_at, updated_by, `+cols+`) VALUES (?, ?, ?, ?, ?, ?`+marks+`) RETURNING id`, //nolint:gosec // G202: fixed table and column names
		args...).Scan(&id)
	if err != nil {
		w.t.Fatalf("insert into %s: %v", table, err)
	}
	return id
}

func (c *authCase) order(r row) int64 {
	c.t.Helper()
	if r.by == 0 {
		r.by = priyaID
	}
	return c.insert("fixture_order", r, "supplier_id, project_id, order_date, amount, priority, discount_pct",
		c.m.Party["SHREE"], nullable(r.project), "2026-10-24", r.amt, "normal", 0)
}

func (c *authCase) claim(r row) int64 {
	c.t.Helper()
	return c.insert("fixture_claim", r, "employee_id, claim_date, total", nullable(r.employee), "2026-10-24", r.amt)
}

func (c *authCase) grn(r row) int64 {
	c.t.Helper()
	if r.by == 0 {
		r.by = sureshID
	}
	id := c.insert("apptest_grn", r, "site_id, project_id, receipt_date, ref", nullable(r.site), nullable(r.project), "2026-10-24", r.number)
	for i, site := range r.lineSites {
		if _, err := c.app.DB().SQL().ExecContext(context.Background(),
			`INSERT INTO apptest_grn_line (parent_id, line_no, line_site_id, qty) VALUES (?, ?, ?, 1000)`, id, i+1, site); err != nil {
			c.t.Fatalf("insert GRN line: %v", err)
		}
	}
	return id
}

func deny(verb, label string) string {
	return "You don't have permission to " + verb + " " + label + ". Ask your administrator for a role that allows it."
}

// wantRefused checks r's status, that its page says msg, and that it shows
// none of leaks.
func wantRefused(t *testing.T, what string, r *response, status int, msg string, leaks ...string) {
	t.Helper()
	text := r.pageText()
	if r.status != status {
		t.Fatalf("%s = %d (%q), want %d", what, r.status, text, status)
	}
	if !strings.Contains(text, msg) {
		t.Fatalf("%s: page %q, want %q", what, text, msg)
	}
	for _, l := range leaks {
		if strings.Contains(r.body, l) {
			t.Fatalf("%s: the page leaks %q: %q", what, l, text)
		}
	}
}

func want403(t *testing.T, what string, r *response, msg string, leaks ...string) {
	t.Helper()
	wantRefused(t, what, r, http.StatusForbidden, msg, leaks...)
}

func want404(t *testing.T, what string, r *response, leaks ...string) {
	t.Helper()
	wantRefused(t, what, r, http.StatusNotFound, msgNotFound, leaks...)
}

func wantStatus(t *testing.T, what string, r *response, status int) {
	t.Helper()
	if r.status != status {
		t.Fatalf("%s = %d (%q), want %d", what, r.status, r.pageText(), status)
	}
}

// wantPage checks a 200 whose page has every of want and none of hide.
func wantPage(t *testing.T, what string, r *response, want, hide []string) {
	t.Helper()
	wantStatus(t, what, r, http.StatusOK)
	for _, s := range want {
		if !strings.Contains(r.body, s) {
			t.Errorf("%s: no %q in %q", what, s, r.pageText())
		}
	}
	for _, s := range hide {
		if strings.Contains(r.body, s) {
			t.Errorf("%s: %q must not show: %q", what, s, r.pageText())
		}
	}
}

// wantListed checks a 200 list page of path whose rows are exactly ids. The
// real list's rows are links: the number isn't a column of every DocType.
func wantListed(t *testing.T, what string, r *response, path string, ids ...int64) {
	t.Helper()
	wantStatus(t, what, r, http.StatusOK)
	got := map[string]bool{}
	r.doc.Find("a.datalist-link").Each(func(_ int, a *goquery.Selection) {
		if href, ok := a.Attr("href"); ok {
			got[href] = true
		}
	})
	want := map[string]bool{}
	for _, id := range ids {
		want[path+"/"+idText(id)] = true
	}
	if !maps.Equal(got, want) {
		t.Errorf("%s: rows %v, want %v", what, slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
}

// csvRows reads an export: the header row is dropped.
func csvRows(t *testing.T, what string, r *response) [][]string {
	t.Helper()
	wantStatus(t, what, r, http.StatusOK)
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(r.body, byteOrderMark))).ReadAll()
	if err != nil || len(rows) == 0 {
		t.Fatalf("%s: CSV %q: %v", what, r.body, err)
	}
	return rows[1:]
}

func idText(id int64) string { return strconv.FormatInt(id, 10) }

func withVersion(version string, f url.Values) url.Values {
	f.Set("version", version)
	return f
}

// details is fixture.Order's first step for id, posted with its current
// version.
func (c *authCase) details(id int64, remarks string) url.Values {
	c.t.Helper()
	return withVersion(c.version(id), url.Values{
		"supplier_id": {idText(c.m.Party["SHREE"])}, "project_id": {idText(c.m.Project["PRJ-METRO"])},
		"order_date": {"24-10-2026"}, "amount": {"1000.00"}, "remarks": {remarks},
		"priority": {"normal"}, "discount_pct": {"0"},
	})
}
