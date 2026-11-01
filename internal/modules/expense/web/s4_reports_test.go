package expenseweb_test

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	expenseweb "github.com/vardhan/internaltools/internal/modules/expense/web"
)

// AUTH's 403 for read (fixture text; CD-142).
const (
	s4NoReadClaims   = "You don't have permission to read Expense Claim. Ask your administrator for a role that allows it."
	s4NoReadAdvances = "You don't have permission to read Employee Advance. Ask your administrator for a role that allows it."
)

// s4Get serves path through the module's routes as p.
func s4Get(t *testing.T, e *expensetest.Env, p auth.Principal, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	expenseweb.Routes(expenseweb.Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files})(r)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), p))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// s4Page is a report page served to email: it must be 200.
type s4Page struct {
	t   *testing.T
	doc *goquery.Document
}

func s4Open(t *testing.T, e *expensetest.Env, email, path string) s4Page {
	t.Helper()
	rec := s4Get(t, e, e.P(t, email), path)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s as %s: status = %d, want 200; body %q", path, email, rec.Code, rec.Body.String())
	}
	doc, err := goquery.NewDocumentFromReader(rec.Body)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return s4Page{t: t, doc: doc}
}

// cards is each list card's visible text, spaces collapsed.
func (p s4Page) cards() []string {
	var out []string
	p.doc.Find(".datalist-rows > li").Each(func(_ int, s *goquery.Selection) {
		s = s.Clone()
		s.Find(".visually-hidden").Remove()
		out = append(out, strings.Join(strings.Fields(s.Text()), " "))
	})
	return out
}

// card is the one card holding title, failing when there is none.
func (p s4Page) card(title string) string {
	p.t.Helper()
	for _, c := range p.cards() {
		if strings.Contains(c, title) {
			return c
		}
	}
	p.t.Fatalf("no card %q in %q", title, p.cards())
	return ""
}

// wantTitles checks the cards are exactly titles, in order.
func (p s4Page) wantTitles(titles ...string) {
	p.t.Helper()
	var got []string
	p.doc.Find(".datalist-rows > li .datalist-title").Each(func(_ int, s *goquery.Selection) {
		got = append(got, strings.TrimSpace(s.Find("dd").Text()))
	})
	if !slices.Equal(got, titles) {
		p.t.Errorf("rows = %q, want %q", got, titles)
	}
}

// wantText checks every s is on the card.
func wantOnCard(t *testing.T, card string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if !strings.Contains(card, s) {
			t.Errorf("card %q: no %q", card, s)
		}
	}
}

// exportHref is the Export link's href, "" when there is none.
func (p s4Page) exportHref() string {
	href, _ := p.doc.Find(`form.filterbar a[href*="export.csv"]`).Attr("href")
	return href
}

// chips is the filter chips' text.
func (p s4Page) chips() []string {
	var out []string
	p.doc.Find(".filterbar-chips a.chip").Each(func(_ int, s *goquery.Selection) {
		out = append(out, strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(s.Text()), "Remove filter")), " "))
	})
	return out
}

// wantNoCount checks the bar shows no result count.
func (p s4Page) wantNoCount() {
	p.t.Helper()
	if got := strings.TrimSpace(p.doc.Find(".filterbar-count").Text()); got != "" {
		p.t.Errorf("count = %q, want none", got)
	}
}

// empty is the empty state's heading, "" when there is none.
func (p s4Page) empty() string {
	return strings.TrimSpace(p.doc.Find(".empty-state .empty-state-text").Text())
}

// emptyLine is the empty state's line under the heading (CD-366).
func (p s4Page) emptyLine() string {
	return strings.TrimSpace(p.doc.Find(".empty-state .empty-state-line").Text())
}

// s4Refused checks rec is status with text in its body.
func s4Refused(t *testing.T, rec *httptest.ResponseRecorder, status int, text string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("status = %d, want %d; body %q", rec.Code, status, rec.Body.String())
	}
	if body := html.UnescapeString(rec.Body.String()); !strings.Contains(body, text) {
		t.Errorf("body = %q, want %q", body, text)
	}
}

// noRoles is email with no roles: it may read nothing.
func noRoles(t *testing.T, e *expensetest.Env, email string) auth.Principal {
	p := e.P(t, email)
	p.Roles = nil
	return p
}

func s4ID(n int64) string { return strconv.FormatInt(n, 10) }

// UX §5.13: the open claims page lists the approved, not closed claims with
// their snapshot labels, narrows to an employee (chip, Export carrying the
// filter), keeps Priya to her team with no Export (no export right), and
// answers a bad filter 422 and a user without read AUTH's 403.
func TestS4_OpenClaimsPage(t *testing.T) {
	e := expensetest.New(t)
	e.E8OpenScenario(t)
	kavya := s4ID(e.E8Employee(t, "EMP-006"))

	p := s4Open(t, e, "meena@vardhan.test", "/reports/expense/open")
	p.wantTitles("EX/26-27/00001", "EX/26-27/00002") // oldest approved first
	wantOnCard(t, p.card("EX/26-27/00001"), "₹1,080.55", "Deepak Nair", "Pune Metro site visit",
		"Approved 27-10-2026 by Priya Shah", "Total ₹2,080.55 · Less advance ₹1,000.00", "1 line without a receipt")
	wantOnCard(t, p.card("EX/26-27/00002"), "Kavya Rao", "Approved 28-10-2026 by Meena Iyer", "2 lines without a receipt")
	// The receipt count shows on phone cards (PMeta); the totals are low.
	for text, low := range map[string]bool{"1 line without a receipt": false, "Total ₹2,080.55": true} {
		cell := p.doc.Find(`.datalist-rows dl:contains("` + text + `")`)
		if _, isLow := cell.Attr("data-priority"); cell.Length() != 1 || isLow != low {
			t.Errorf("%q cell: %d found, low priority %v; want 1, %v", text, cell.Length(), isLow, low)
		}
	}
	if got := p.exportHref(); got != "/reports/expense/open/export.csv" {
		t.Errorf("Export = %q, want the plain CSV", got)
	}
	if !strings.Contains(p.doc.Find(".filterbar-count").Text(), "2 claims") {
		t.Errorf("count = %q, want 2 claims", p.doc.Find(".filterbar-count").Text())
	}

	p = s4Open(t, e, "meena@vardhan.test", "/reports/expense/open?employee="+kavya)
	p.wantTitles("EX/26-27/00002")
	if !slices.Equal(p.chips(), []string{"Employee: Kavya Rao"}) {
		t.Errorf("chips = %q, want Employee: Kavya Rao", p.chips())
	}
	if got := p.exportHref(); got != "/reports/expense/open/export.csv?employee="+kavya {
		t.Errorf("Export = %q, want the CSV with the employee filter", got)
	}
	rows := e8CSV(t, e8Get(t, e, "meena@vardhan.test", p.exportHref()), e8OpenHeader)
	if len(rows) != 1 || !strings.HasPrefix(rows[0], "EX/26-27/00002,") {
		t.Errorf("filtered CSV = %q, want EX/26-27/00002 only", rows)
	}

	p = s4Open(t, e, "priya@vardhan.test", "/reports/expense/open")
	p.wantTitles("EX/26-27/00001")
	if got := p.exportHref(); got != "" {
		t.Errorf("Priya (no export) sees Export %q", got)
	}

	s4Refused(t, s4Get(t, e, e.P(t, "meena@vardhan.test"), "/reports/expense/open?employee=x"), http.StatusUnprocessableEntity, "Choose a value for Employee.")
	s4Refused(t, e8Get(t, e, "meena@vardhan.test", "/reports/expense/open/export.csv?employee=x"), http.StatusUnprocessableEntity, "Choose a value for Employee.")
	s4Refused(t, s4Get(t, e, noRoles(t, e, "deepak@vardhan.test"), "/reports/expense/open"), http.StatusForbidden, s4NoReadClaims)
}

func TestS4_OpenClaimsEmpty(t *testing.T) {
	e := expensetest.New(t)
	p := s4Open(t, e, "meena@vardhan.test", "/reports/expense/open")
	if got := p.empty(); got != "No claims to close" {
		t.Errorf("empty state = %q", got)
	}
	p.wantNoCount()
}

// UX §5.14: the expense lines page, one card per line with its snapshot
// labels (company bill: GST, supplier, invoice and Billed to), each filter
// with its chip, the Export link carrying every filter, both empty states,
// Priya's team only, 422 and 403.
func TestS4_LinesPage(t *testing.T) {
	e := expensetest.New(t)
	e.E8LinesScenario(t)
	food, metro, kavya := s4ID(e.E8Type(t, "FOOD")), s4ID(e.E8Project(t, "PRJ-METRO")), s4ID(e.E8Employee(t, "EMP-006"))

	p := s4Open(t, e, "meena@vardhan.test", "/reports/expense/lines")
	p.wantTitles("EX/26-27/00002 · line 1", "EX/26-27/00001 · line 2", "EX/26-27/00001 · line 1") // newest line date first
	if n := p.doc.Find(".datalist-rows .datalist-title.code").Length(); n != 3 {
		t.Errorf("%d titles in the code font, want 3", n)
	}
	wantOnCard(t, p.card("EX/26-27/00001 · line 1"), "₹4,720.00", "Lodging · 30-09-2026", "Deepak Nair", "PRJ-METRO · METRO-SITE",
		"GST ₹720.00 · 27AABCS1429B1ZU · INV/778 · Billed to 29AAFCV2449G1Z3", "Receipt: Yes", "Approved")
	lunch := p.card("EX/26-27/00001 · line 2")
	wantOnCard(t, lunch, "₹830.55", "Food · 22-10-2026", "PRJ-TECHPARK · TECH-SITE", "Receipt: No")
	if strings.Contains(lunch, "GST") || strings.Contains(lunch, "Billed to") {
		t.Errorf("lunch (no company bill) card %q shows GST details", lunch)
	}
	wantOnCard(t, p.card("EX/26-27/00002 · line 1"), "Kavya Rao", "Fuel · 23-10-2026", "Closed")
	if got := p.exportHref(); got != "/reports/expense/lines/export.csv" {
		t.Errorf("Export = %q, want the plain CSV", got)
	}

	for _, tc := range []struct {
		name, query, chip string
		want              []string
	}{
		{"month", "month=2026-09", "Month: September 2026", []string{"EX/26-27/00001 · line 1"}},
		{"type", "type=" + food, "Type: Food", []string{"EX/26-27/00001 · line 2"}},
		{"project", "project=" + metro, "Project: PRJ-METRO", []string{"EX/26-27/00001 · line 1"}},
		{"employee", "employee=" + kavya, "Employee: Kavya Rao", []string{"EX/26-27/00002 · line 1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := s4Open(t, e, "meena@vardhan.test", "/reports/expense/lines?"+tc.query)
			p.wantTitles(tc.want...)
			if !slices.Equal(p.chips(), []string{tc.chip}) {
				t.Errorf("chips = %q, want %q", p.chips(), tc.chip)
			}
			if got := p.exportHref(); got != "/reports/expense/lines/export.csv?"+tc.query {
				t.Errorf("Export = %q, want the CSV with %s", got, tc.query)
			}
		})
	}

	all := "employee=" + kavya + "&month=2026-10&project=" + metro + "&type=" + food
	p = s4Open(t, e, "meena@vardhan.test", "/reports/expense/lines?"+all)
	if got := p.exportHref(); got != "/reports/expense/lines/export.csv?"+all {
		t.Errorf("Export = %q, want every filter", got)
	}
	// No row matches, so every chip is named from its record (AD review).
	if want := []string{"Month: October 2026", "Type: Food", "Project: PRJ-METRO", "Employee: Kavya Rao"}; !slices.Equal(p.chips(), want) {
		t.Errorf("chips = %q, want %q", p.chips(), want)
	}
	if got := p.empty(); got != "No lines match these filters" {
		t.Errorf("filtered empty state = %q", got)
	}
	if href, _ := p.doc.Find(".empty-state a").Attr("href"); href != "/reports/expense/lines" {
		t.Errorf("empty state action = %q, want Clear all to the plain report", href)
	}
	// Removing one chip keeps the others.
	href, _ := p.doc.Find(`.filterbar-chips a.chip:contains("Month")`).Attr("href")
	if q, _ := url.ParseQuery(strings.TrimPrefix(href, "/reports/expense/lines?")); q.Has("month") || q.Get("type") != food || q.Get("employee") != kavya {
		t.Errorf("Month chip removes to %q, want the other filters kept", href)
	}

	// Priya, restricted to PRJ-METRO, can't read PRJ-TECHPARK: its chip uses
	// the visible rows' snapshot, and with no row the raw id (CD-233).
	e.S4Restrict(t, "priya@vardhan.test", "PRJ-METRO")
	techpark := s4ID(e.E8Project(t, "PRJ-TECHPARK"))
	p = s4Open(t, e, "priya@vardhan.test", "/reports/expense/lines?project="+techpark)
	if !slices.Equal(p.chips(), []string{"Project: PRJ-TECHPARK"}) {
		t.Errorf("Priya's chips = %q, want the row's snapshot code", p.chips())
	}
	p = s4Open(t, e, "priya@vardhan.test", "/reports/expense/lines?month=2026-09&project="+techpark)
	if !slices.Equal(p.chips(), []string{"Month: September 2026", "Project: " + techpark}) {
		t.Errorf("Priya's chips with no row = %q, want the raw id", p.chips())
	}

	p = s4Open(t, e, "priya@vardhan.test", "/reports/expense/lines")
	p.wantTitles("EX/26-27/00001 · line 2", "EX/26-27/00001 · line 1")

	s4Refused(t, s4Get(t, e, e.P(t, "meena@vardhan.test"), "/reports/expense/lines?month=2026-13"), http.StatusUnprocessableEntity, "")
	s4Refused(t, s4Get(t, e, e.P(t, "meena@vardhan.test"), "/reports/expense/lines?type=food"), http.StatusUnprocessableEntity, "Choose a value for Type.")
	s4Refused(t, s4Get(t, e, noRoles(t, e, "deepak@vardhan.test"), "/reports/expense/lines"), http.StatusForbidden, s4NoReadClaims)
}

func TestS4_LinesEmpty(t *testing.T) {
	e := expensetest.New(t)
	p := s4Open(t, e, "meena@vardhan.test", "/reports/expense/lines")
	if got := p.empty(); got != "No expense lines yet" {
		t.Errorf("empty state = %q", got)
	}
	p.wantNoCount()
}

// UX §5.15: the open advances page, oldest first, `Paid on` only on the paid
// row, the employee filter, Priya's team only, 422 and 403.
func TestS4_AdvancesPage(t *testing.T) {
	e := expensetest.New(t)
	e.E8OpenScenario(t)
	kavya := s4ID(e.E8Employee(t, "EMP-006"))

	p := s4Open(t, e, "meena@vardhan.test", "/reports/expense/advances")
	p.wantTitles("EA/26-27/00002", "EA/26-27/00001")
	wantOnCard(t, p.card("EA/26-27/00001"), "₹4,000.00", "Deepak Nair", "24-10-2026 · Pune Metro site visit",
		"Paid on 25-10-2026", "Amount ₹5,000.00 · Used ₹1,000.00", "10 days")
	unpaid := p.card("EA/26-27/00002")
	wantOnCard(t, unpaid, "₹3,000.50", "Kavya Rao", "20-10-2026 · Bengaluru client visit", "14 days")
	if strings.Contains(unpaid, "Paid on") {
		t.Errorf("unpaid card %q shows Paid on", unpaid)
	}
	if got := p.exportHref(); got != "/reports/expense/advances/export.csv" {
		t.Errorf("Export = %q, want the plain CSV", got)
	}

	p = s4Open(t, e, "meena@vardhan.test", "/reports/expense/advances?employee="+kavya)
	p.wantTitles("EA/26-27/00002")
	if !slices.Equal(p.chips(), []string{"Employee: Kavya Rao"}) {
		t.Errorf("chips = %q, want Employee: Kavya Rao", p.chips())
	}
	if got := p.exportHref(); got != "/reports/expense/advances/export.csv?employee="+kavya {
		t.Errorf("Export = %q, want the CSV with the employee filter", got)
	}

	p = s4Open(t, e, "priya@vardhan.test", "/reports/expense/advances")
	p.wantTitles("EA/26-27/00001")

	s4Refused(t, s4Get(t, e, e.P(t, "meena@vardhan.test"), "/reports/expense/advances?employee=x"), http.StatusUnprocessableEntity, "Choose a value for Employee.")
	s4Refused(t, s4Get(t, e, noRoles(t, e, "deepak@vardhan.test"), "/reports/expense/advances"), http.StatusForbidden, s4NoReadAdvances)
}

func TestS4_AdvancesEmpty(t *testing.T) {
	e := expensetest.New(t)
	p := s4Open(t, e, "meena@vardhan.test", "/reports/expense/advances")
	if got := p.empty(); got != "No open advances" {
		t.Errorf("empty state = %q", got)
	}
	p.wantNoCount()
}

// CD-366: each report's plain empty state has a heading and a line with no
// link; with a filter that matches nothing it has its own heading, the shared
// line and a Clear filters link back to the plain report.
func TestS4_ReportEmptyStates(t *testing.T) {
	e := expensetest.New(t)
	meena := s4ID(e.E8Employee(t, "EMP-002")) // no claims or advances in the base fixture
	for _, tc := range []struct{ path, heading, line, noMatch string }{
		{"/reports/expense/open", "No claims to close", "Approved claims show up here until they are closed.", "No claims match these filters"},
		{"/reports/expense/lines", "No expense lines yet", "Lines of approved claims show up here.", "No lines match these filters"},
		{"/reports/expense/advances", "No open advances", "Approved advances show up here until they are closed.", "No advances match these filters"},
	} {
		p := s4Open(t, e, "meena@vardhan.test", tc.path)
		if got := p.empty(); got != tc.heading {
			t.Errorf("%s: empty heading = %q, want %q", tc.path, got, tc.heading)
		}
		if got := p.emptyLine(); got != tc.line {
			t.Errorf("%s: empty line = %q, want %q", tc.path, got, tc.line)
		}
		if n := p.doc.Find(".empty-state a").Length(); n != 0 {
			t.Errorf("%s: plain empty state has %d links, want none", tc.path, n)
		}

		p = s4Open(t, e, "meena@vardhan.test", tc.path+"?employee="+meena)
		if got := p.empty(); got != tc.noMatch {
			t.Errorf("%s filtered: empty heading = %q, want %q", tc.path, got, tc.noMatch)
		}
		if got := p.emptyLine(); got != "Try fewer filters, or clear them." {
			t.Errorf("%s filtered: empty line = %q, want the shared line", tc.path, got)
		}
		link := p.doc.Find(".empty-state a")
		if href, _ := link.Attr("href"); href != tc.path || strings.TrimSpace(link.Text()) != "Clear filters" {
			t.Errorf("%s filtered: link = %q %q, want Clear filters to %s", tc.path, strings.TrimSpace(link.Text()), href, tc.path)
		}
	}
}

// UX §5.14: the Filter link opens each report's filter page, a GET form back
// to the report with the current values chosen: Month and Type selects and
// Project and Employee pickers for the lines, Employee for the others.
func TestS4_FilterPages(t *testing.T) {
	e := expensetest.New(t)
	e.E8LinesScenario(t) // the clock is 2026-10-24
	fuel, kavya := s4ID(e.E8Type(t, "FUEL")), s4ID(e.E8Employee(t, "EMP-006"))

	q := "employee=" + kavya + "&month=2026-10&type=" + fuel // Kavya's fuel line
	p := s4Open(t, e, "meena@vardhan.test", "/reports/expense/lines?"+q)
	p.wantTitles("EX/26-27/00002 · line 1")
	if href, _ := p.doc.Find(`form.filterbar a[href*="/filter"]`).Attr("href"); href != "/reports/expense/lines/filter?"+q {
		t.Errorf("Filter link = %q, want the filter page with the filters", href)
	}

	p = s4Open(t, e, "meena@vardhan.test", "/reports/expense/lines/filter?"+q)
	form := p.doc.Find(`main form[method="get"][action="/reports/expense/lines"]`)
	if form.Length() != 1 {
		t.Fatalf("no GET form to /reports/expense/lines")
	}
	months := form.Find(`select[name="month"] option`)
	if first, _ := months.Eq(1).Attr("value"); first != "2026-10" || months.Eq(1).Text() != "October 2026" || months.Length() != 25 {
		t.Errorf("month options: %d, first %q %q; want Any month then 24 from October 2026", months.Length(), first, months.Eq(1).Text())
	}
	if v, _ := form.Find(`select[name="month"] option[selected]`).Attr("value"); v != "2026-10" {
		t.Errorf("chosen month = %q, want 2026-10", v)
	}
	if got := form.Find(`select[name="type"] option[selected]`).Text(); got != "Fuel" {
		t.Errorf("chosen type = %q, want Fuel", got)
	}
	if v, _ := form.Find(`input[type="hidden"][name="employee"]`).Attr("value"); v != kavya {
		t.Errorf("employee picker value = %q, want %s", v, kavya)
	}
	if got := strings.TrimSpace(form.Find(`.picker`).Has(`input[name="employee"]`).Find(`[data-picker-summary]`).Text()); got != "Kavya Rao" {
		t.Errorf("employee picker shows %q, want Kavya Rao", got)
	}
	if form.Find(`input[type="hidden"][name="project"]`).Length() != 1 {
		t.Errorf("no project picker")
	}
	if href, _ := p.doc.Find(`a.back-link`).Attr("href"); href != "/reports/expense/lines?"+q {
		t.Errorf("Back = %q, want the report with its filters", href)
	}

	// With no matching row the employee picker is still named, from the master.
	p = s4Open(t, e, "meena@vardhan.test", "/reports/expense/lines/filter?month=2026-09&employee="+kavya)
	if got := strings.TrimSpace(p.doc.Find(`.picker`).Has(`input[name="employee"]`).Find(`[data-picker-summary]`).Text()); got != "Kavya Rao" {
		t.Errorf("employee picker (no row) shows %q, want Kavya Rao", got)
	}

	for _, r := range []string{"open", "advances"} {
		p := s4Open(t, e, "meena@vardhan.test", "/reports/expense/"+r+"/filter")
		form := p.doc.Find(`main form[method="get"][action="/reports/expense/` + r + `"]`)
		if form.Find(`input[type="hidden"][name="employee"]`).Length() != 1 || form.Find(`select`).Length() != 0 {
			t.Errorf("%s filter page: want the employee picker only", r)
		}
	}
	s4Refused(t, s4Get(t, e, e.P(t, "meena@vardhan.test"), "/reports/expense/lines/filter?month=x"), http.StatusUnprocessableEntity, "")

	// Priya without read on a master gets no filter for it (/pick would 403).
	for _, dt := range []string{expensetype.DocType, project.DocType, employee.DocType} {
		e.S4RevokeRead(t, "priya@vardhan.test", dt)
	}
	p = s4Open(t, e, "priya@vardhan.test", "/reports/expense/lines/filter")
	if p.doc.Find(`select[name="month"]`).Length() != 1 || p.doc.Find(`select[name="type"], input[name="project"], input[name="employee"]`).Length() != 0 {
		t.Errorf("Priya with no master read: want the month filter only")
	}
	p = s4Open(t, e, "priya@vardhan.test", "/reports/expense/open/filter")
	if p.doc.Find(`input[name="employee"]`).Length() != 0 {
		t.Errorf("Priya with no Employee read sees the employee picker")
	}
}

// S4b: the code and date cells keep whole tokens (datalist-nowrap, CD-307),
// at most 3 per list: record numbers, GSTINs and project and site codes
// first, then dates; the others wrap as before.
func TestS4b_ReportNoWrapCells(t *testing.T) {
	e := expensetest.New(t)
	e.E8OpenScenario(t)
	e.E8LinesScenario(t)
	for _, tc := range []struct {
		path        string
		nowrap, not []string
	}{
		{"/reports/expense/open", []string{"Claim"}, []string{"Approved", "Employee", "Purpose", "Without a receipt", "Total and less advance"}},
		{"/reports/expense/lines", []string{"Claim and line", "Project and site"}, []string{"Type and date", "Employee", "Receipt", "GST details"}},
		{"/reports/expense/advances", []string{"Advance", "Paid on"}, []string{"Date and purpose", "Employee", "Amount and used", "Age"}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			p := s4Open(t, e, "meena@vardhan.test", tc.path)
			check := func(label string, want bool) {
				cells := p.doc.Find(".datalist-rows dl").FilterFunction(func(_ int, s *goquery.Selection) bool {
					return strings.TrimSpace(s.Find("dt").Text()) == label
				})
				if cells.Length() == 0 {
					t.Errorf("%q: no cells", label)
				}
				cells.Each(func(_ int, s *goquery.Selection) {
					if got := s.HasClass("datalist-nowrap"); got != want {
						t.Errorf("%q cell: datalist-nowrap = %v, want %v", label, got, want)
					}
				})
			}
			for _, l := range tc.nowrap {
				check(l, true)
			}
			for _, l := range tc.not {
				check(l, false)
			}
		})
	}
}
