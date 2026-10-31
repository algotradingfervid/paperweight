package app_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// TOOLING T2b (CD-233, CD-240, CD-248): derived lists, cards, warnings,
// history, inbox, reports, exports, pickers and search name nothing about a
// record the viewer can't read, not even its state or amount. The sweep signs
// in as a viewer holding only viewerRole (viewerRights), restricted to
// project A and to its own expense records, on the real router and the real
// auth.Service, and GETs every GET route the router has. No body may carry a
// hidden record's number, free text, amount, rate or quantity (Indian, plain
// and western grouping) or URL. Every route answers the status sweepStatus
// says, and the controls, which the viewer may read, appear on every route
// that answers 200 unless noControl says why not, so an empty page can't pass.
// checkBuyer then opens the purchase queues, home and Create PO as a buyer
// with the same read scope, which also holds the rights those queues need.
func TestTooling_VisibilitySweep(t *testing.T) {
	s := seedSweep(t)
	s.checkReadable(t)
	s.checkVendorReadable(t)
	v := s.w.signedIn(viewerEmail)

	pages := s.sweepPages(t)
	var hidden []hiddenMarks
	for _, r := range s.records {
		if r.hidden {
			hidden = append(hidden, hiddenMarks{r, r.markers(), r.urlPattern()})
		}
	}
	if len(hidden) == 0 {
		t.Fatal("no hidden records were seeded: the sweep would prove nothing")
	}
	bodies := map[string]string{}
	got := map[string]map[bool]map[int]bool{} // route → hidden page → statuses seen
	pinned := s.urlStatuses()
	mods := s.w.app.DocDeps().Modules
	probed := 0 // line pages of other modules, which still answer as sweepStatuses says
	for _, p := range pages {
		r := v.get(p.url)
		want := sweepStatus(p.route, p.hidden)
		if dt, ok := linePage(p.route, p.url); ok {
			// A NoLinePages module has no line routes (CD-256 (7), CD-329):
			// each of its line pages must answer 404, which is the check that
			// this exception still holds.
			if mods[dt].NoLinePages {
				want = []int{http.StatusNotFound}
			} else {
				probed++
			}
		}
		bodies[p.url] = r.body
		if want, ok := pinned[p.url]; ok && r.status != want {
			t.Errorf("GET %s (route %s) = %d, want %d: update urlStatuses if the change is right", p.url, p.route, r.status, want)
		}
		if got[p.route] == nil {
			got[p.route] = map[bool]map[int]bool{}
		}
		if got[p.route][p.hidden] == nil {
			got[p.route][p.hidden] = map[int]bool{}
		}
		got[p.route][p.hidden][r.status] = true
		if !slices.Contains(want, r.status) {
			t.Errorf("GET %s (route %s, hidden id %v) = %d, want one of %v: update sweepStatuses if the change is right", p.url, p.route, p.hidden, r.status, want)
		}
		for prefix, name := range hiddenMasters {
			if strings.HasPrefix(p.url, prefix) && strings.Contains(r.body, name) {
				t.Errorf("GET %s names %s, a master the viewer can't read", p.url, name)
			}
		}
		for _, m := range []string{"Rajkot Castings", hiddenVendor} {
			if !vendorPage(p.url) && strings.Contains(r.body, m) {
				t.Errorf("GET %s (route %s) names the hidden orders' vendor: %q", p.url, p.route, m)
			}
		}
		if i := strings.Index(r.body, hiddenToken); i >= 0 {
			t.Errorf("GET %s (route %s) shows a hidden record's text: %q", p.url, p.route, r.body[max(0, i-60):min(len(r.body), i+60)])
		}
		for _, h := range hidden {
			if m := leak(r.body, h.markers, h.url); m != "" {
				t.Errorf("GET %s (route %s) names hidden %s %d: %q", p.url, p.route, h.r.label, h.r.id, m)
			}
		}
	}
	if probed == 0 {
		t.Errorf("no line page of a module without NoLinePages was swept: the NoLinePages exception covers every module")
	}
	// The table is exact: a status it allows that no page answered is stale.
	for route, byHidden := range got {
		for hid, seen := range byHidden {
			for _, want := range sweepStatus(route, hid) {
				if !seen[want] {
					t.Errorf("route %s (hidden id %v) never answered %d: update sweepStatuses", route, hid, want)
				}
			}
		}
	}
	for route := range sweepStatuses {
		if got[route] == nil {
			t.Errorf("sweepStatuses lists %s, which the router no longer has", route)
		}
	}
	for u := range pinned {
		if _, ok := bodies[u]; !ok {
			t.Errorf("urlStatuses pins %s, which the sweep no longer GETs", u)
		}
	}

	routeOf := map[string]string{}
	for _, p := range pages {
		routeOf[p.url] = p.route
	}
	expected := map[string]bool{}
	for _, c := range s.controlExpectations() {
		body, ok := bodies[c.url]
		if !ok {
			t.Errorf("control page %s was not swept", c.url)
			continue
		}
		expected[routeOf[c.url]] = true
		if !strings.Contains(body, c.want) {
			t.Errorf("GET %s doesn't show the control's %q: the page shows no data, so the sweep proves nothing there", c.url, c.want)
		}
	}
	for route, byHidden := range got {
		ok200 := byHidden[false][http.StatusOK]
		_, exempt := noControlWhy(route)
		if ok200 && !expected[route] && !exempt {
			t.Errorf("route %s answers 200 but no control expectation covers it: add one in controlExpectations, or a reason in noControl", route)
		}
	}
	for route, why := range noControl {
		switch {
		case got[route] == nil || !got[route][false][http.StatusOK]:
			t.Errorf("route %s never answers 200, so its noControl reason (%s) is stale", route, why)
		case expected[route]:
			t.Errorf("route %s has a control expectation and a noControl reason (%s): drop the reason", route, why)
		}
	}
	s.checkInboxCount(t, v)
	s.checkWideInbox(t, hidden)
	s.checkBuyer(t, hidden)
	t.Logf("swept %d GETs over %d routes as the viewer", len(pages), len(got))
}

// sweepStatuses is, for each route that doesn't answer only 200 to the viewer
// on its control and sample URLs (Control) or only 404 on a hidden record's
// URL (Hidden), the exact set it answers. nil means the default.
var sweepStatuses = func() map[string]struct{ Control, Hidden []int } {
	const ok, see, forbidden, missing, stub = http.StatusOK, http.StatusSeeOther, http.StatusForbidden, http.StatusNotFound, http.StatusNotImplemented
	type st = struct{ Control, Hidden []int }
	out := map[string]st{
		"/": {Control: []int{see}}, // to the inbox
		// A document page: 303 is a writer's draft going to its check step,
		// 404 a TestOnly DocType's missing sample; a hidden record is 404.
		"/doc/{doctype}/new":                                   {Control: []int{ok, forbidden}}, // 200: goods receipts, the only create the viewer holds
		"/doc/{doctype}/{id}":                                  {Control: []int{ok, see, missing}},
		"/doc/{doctype}/{id}/edit/{step}":                      {Control: []int{ok, see}},
		"/doc/{doctype}/{id}/lines/new":                        {Control: []int{ok, see}},
		"/doc/{doctype}/{id}/lines/{no}":                       {Control: []int{ok, see}},
		"/doc/{doctype}/{id}/lines/{no}/remove":                {Control: []int{ok, see}},
		"/doc/{doctype}/{id}/{action}":                         {Control: []int{ok, forbidden}}, // 200: reject, which the viewer may
		"/print/{doctype}/{id}":                                {Control: []int{ok, missing}},   // 404: no print view
		"/print/{doctype}/{id}/page.css":                       {Control: []int{ok, missing}},
		"/files/*":                                             {Control: []int{missing}},
		"/m/party/{id:[1-9][0-9]*}/contacts/{row:[1-9][0-9]*}": {Control: []int{missing}},     // no contact rows
		"/m/payment-terms/{id:[1-9][0-9]*}":                    {Control: []int{missing}},     // no rows
		"/m/project/{id:[1-9][0-9]*}":                          {Control: []int{ok, missing}}, // 404: project B
		"/m/import/check":                                      {Control: []int{see}},         // back to Choose
		"/m/import/errors.csv":                                 {Control: []int{see}},
		"/m/import/import":                                     {Control: []int{see}},
		"/m/import/upload":                                     {Control: []int{see}},
		"/setup":                                               {Control: []int{missing}}, // installed
		"/static/*":                                            {Control: []int{missing}},
		"/static/brand/*":                                      {Control: []int{missing}},
		"/static/ui/*":                                         {Control: []int{missing}},
		"/users/{id}":                                          {Control: []int{ok, forbidden}}, // 200: the viewer's own
		"/modules/purchase/q/{queue}":                          {Control: []int{ok, missing}},   // 404: an unknown queue
		"/modules/purchase/requests/{id}/order":                {Control: []int{forbidden}},     // the viewer can't create an order
	}
	// Administrator-only pages and the create pages the viewer holds no
	// right for.
	for _, r := range []string{
		"/admin/approval-rules", "/admin/approval-rules/new", "/admin/approval-rules/{id}", "/admin/approvals/overdue",
		"/admin/approvals/stuck", "/rights/who-can", "/roles", "/roles/{id}", "/roles/{id}/copy-rights",
		"/roles/{id}/rights/{doctype}", "/settings/business", "/settings/numbering", "/settings/numbering/preview",
		"/settings/numbering/{doctype}", "/setup/company", "/users", "/users/{id}/access", "/m/import/template",
		"/m/employee/new/{step}", "/m/party/new/{step}", "/m/party/{id:[1-9][0-9]*}/addresses/new",
		"/m/party/{id:[1-9][0-9]*}/contacts/new",
		"/m/branch/new", "/m/employee/new", "/m/expense-type/new", "/m/item/new", "/m/party/new",
		"/m/payment-terms/new", "/m/project/new", "/m/site/new", "/m/tax-code/new", "/m/uom/new",
	} {
		out[r] = st{Control: []int{forbidden}}
	}
	// Stubs answer 501 before reading the id (expense E1).
	for _, r := range []string{"/modules/expense/advances/{id}/paid", "/modules/expense/advances/{id}/unpaid"} {
		out[r] = st{Control: []int{stub}, Hidden: []int{stub}}
	}
	for _, r := range []string{"/reports/grn/bills", "/reports/grn/bills/export.csv", "/reports/grn/items", "/reports/grn/items/export.csv"} {
		out[r] = st{Control: []int{stub}}
	}
	return out
}()

// urlStatuses pins, URL by URL, what the viewer gets on routes whose
// sweepStatuses entry allows more than one status: each queue (404 is a
// queue whose card's right the viewer lacks: create or amend on orders,
// create on requests; only no-such-queue is unknown), and Create PO on the
// control and hidden request (403: no create on orders, after read; 404:
// read is checked first).
func (s *sweepSeed) urlStatuses() map[string]int {
	const ok, forbidden, missing = http.StatusOK, http.StatusForbidden, http.StatusNotFound
	q := "/modules/purchase/q/"
	out := map[string]int{
		q + purchase.QueueDrafts: missing, q + purchase.QueueWaiting: missing, q + purchase.QueueToSend: ok,
		q + purchase.QueueToClose: missing, q + purchase.QueueAwaitingDelivery: ok, q + purchase.QueueRequestsToOrder: missing,
		q + purchase.QueueMyRequests: missing, q + "no-such-queue": missing,
	}
	for _, c := range s.candidates("/modules/purchase/requests/{id}/order", "id") {
		want := forbidden
		if c.hidden {
			want = missing
		}
		out["/modules/purchase/requests/"+c.value+"/order"] = want
	}
	return out
}

// linePage reports whether route is a document's line page and, if so, the
// DocType its url names.
func linePage(route, url string) (string, bool) {
	if !strings.HasPrefix(route, "/doc/{doctype}/{id}/lines/") {
		return "", false
	}
	return strings.Split(url, "/")[2], true
}

// sweepStatus is what route may answer the viewer.
func sweepStatus(route string, hidden bool) []int {
	st := sweepStatuses[route]
	if hidden {
		if st.Hidden != nil {
			return st.Hidden
		}
		return []int{http.StatusNotFound}
	}
	if st.Control != nil {
		return st.Control
	}
	return []int{http.StatusOK}
}

// noControl is why a route answering 200 has no control expectation. Master
// pages and pickers (noControlPrefixes) show master rows the viewer reads,
// except projects, which controlExpectations and hiddenMasters check.
var noControl = map[string]string{
	"/account/password":                "the viewer's own password form",
	"/account/sessions":                "the viewer's own sessions",
	"/signin":                          "the sign-in page",
	"/healthz":                         "the health answer",
	"/print/assets/print.css":          "a fixed print asset",
	"/print/assets/brand.css":          "a fixed print asset",
	"/print/assets/print.js":           "a fixed print asset",
	"/print/{doctype}/{id}/page.css":   "the print page's size rules, no data",
	"/approvals/count":                 "checked by checkInboxCount",
	"/users/{id}":                      "the viewer's own user page",
	"/doc/{doctype}/new":               "a blank new-document form",
	"/doc/{doctype}/{id}/lines/new":    "a blank line form",
	"/reports/expense/open/filter":     "a filter form: no rows",
	"/reports/expense/lines/filter":    "a filter form: no rows",
	"/reports/expense/advances/filter": "a filter form: no rows",
	"/reports/purchase/pending/filter": "a filter form: no rows",
}

var noControlPrefixes = []string{"/m/", "/pick/"}

func noControlWhy(route string) (string, bool) {
	if why, ok := noControl[route]; ok {
		return why, true
	}
	for _, p := range noControlPrefixes {
		if strings.HasPrefix(route, p) {
			return "a master page or picker", true
		}
	}
	return "", false
}

// hiddenMasters: the project pages and picker name no project the viewer
// can't read (project B).
var hiddenMasters = map[string]string{"/m/project": projectB, "/pick/Project": projectB}

// leak is the first of a hidden record's markers or URLs in body, "" for
// none. Its free texts and the hidden orders' vendor are found once per page.
func leak(body string, markers []string, url *regexp.Regexp) string {
	for _, m := range markers {
		if strings.Contains(body, m) {
			return m
		}
	}
	return url.FindString(body)
}

// vendorPage is a page of the Party master, which the viewer may read: the
// hidden orders' vendor shows there as itself, not as part of an order.
// checkVendorReadable checks that this still holds.
func vendorPage(u string) bool {
	return strings.HasPrefix(u, "/m/party") || strings.HasPrefix(u, "/pick/Party")
}

// checkVendorReadable is the check behind vendorPage's exception: the viewer
// may read the hidden orders' vendor.
func (s *sweepSeed) checkVendorReadable(t *testing.T) {
	t.Helper()
	id := s.id("party", hiddenVendor)
	if err := s.w.app.Auth().Can(context.Background(), s.viewer, "Party", auth.RightRead, &auth.RecordRef{DocType: "Party", ID: id}); err != nil {
		t.Errorf("viewer read on party %s = %v, want nil: vendorPage's exception no longer holds", hiddenVendor, err)
	}
}

// checkInboxCount: the header count is the viewer's readable pending
// requests only, the ones /approvals lists.
// It also checks that a hidden request waits under a rule whose approver
// slot (viewerRole) the viewer holds, so the inbox has one to leave out.
func (s *sweepSeed) checkInboxCount(t *testing.T, v *browser) {
	t.Helper()
	want, hiddenPending := 0, false
	for _, r := range s.records {
		switch {
		case r.hidden && r.doc.State == doctype.StatePending:
			hiddenPending = true
		case !r.hidden && r.eligible:
			want++
		}
	}
	if want == 0 || !hiddenPending {
		t.Errorf("control requests the viewer may approve: %d; a hidden pending request: %v; want both, or the inbox checks prove nothing", want, hiddenPending)
	}
	got := v.get("/approvals/count").pageText()
	if got != strconv.Itoa(want) {
		t.Errorf("/approvals/count = %q, want %d: the control requests waiting for the viewer, and no hidden one", got, want)
	}
}

// hiddenMarks are a hidden record's markers and URL pattern, built once.
type hiddenMarks struct {
	r       *sweepRecord
	markers []string
	url     *regexp.Regexp
}

// checkWideInbox: the wide approver (approve:all, read:own on claims) sees
// in its inbox and count only the requests it can read, never the hidden
// claim pending under the rule whose slot it holds.
func (s *sweepSeed) checkWideInbox(t *testing.T, hidden []hiddenMarks) {
	t.Helper()
	want, hiddenClaim := 0, false
	for _, r := range s.records {
		switch {
		case r.hidden && r.dt == expense.ClaimDocType && r.doc.State == doctype.StatePending:
			hiddenClaim = true
		case !r.hidden && r.wideOK:
			want++
		}
	}
	if !hiddenClaim {
		t.Error("no hidden claim is pending: the wide approver's inbox check proves nothing")
	}
	b := s.w.signedIn(wideEmail)
	inbox := b.get("/approvals")
	for _, h := range hidden {
		if m := leak(inbox.body, h.markers, h.url); m != "" {
			t.Errorf("wide approver's /approvals names hidden %s %d: %q", h.r.label, h.r.id, m)
		}
	}
	if got := b.get("/approvals/count").pageText(); got != strconv.Itoa(want) {
		t.Errorf("wide approver's /approvals/count = %q, want %d: only the requests it can read", got, want)
	}
}

// sweepPage is one GET the sweep makes; hidden is a URL naming a hidden
// record's id.
type sweepPage struct {
	route, url string
	hidden     bool
}

// sweepPages builds the URLs of every GET route the real router has. A route
// with no builder fails the test, so a new page is swept or listed here.
func (s *sweepSeed) sweepPages(t *testing.T) []sweepPage {
	t.Helper()
	var routes []string
	_ = chi.Walk(s.w.routes(), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == http.MethodGet {
			routes = append(routes, route)
		}
		return nil
	})
	sort.Strings(routes)
	docPages := s.docPages()
	var out []sweepPage
	seen := map[string]bool{}
	covered := map[string]bool{}
	add := func(p sweepPage) {
		if !seen[p.url] {
			seen[p.url] = true
			covered[p.route] = true
			out = append(out, p)
		}
	}
	for _, route := range routes {
		if ps, ok := docPages[route]; ok {
			for _, p := range ps {
				add(p)
			}
			continue
		}
		ps, err := s.fill(route)
		if err != "" {
			t.Errorf("GET %s: %s: give it a URL builder in sweepPages, so the visibility sweep covers it", route, err)
			continue
		}
		for _, p := range ps {
			add(p)
		}
	}
	for _, route := range routes {
		if !covered[route] {
			t.Errorf("GET %s has no swept URL", route)
		}
	}
	return out
}

// sampleIDs are docscreen.Screens' ids for dt: the control in each state, or,
// for a state with no control, the first control there is.
func (s *sweepSeed) sampleIDs(dt string) (docscreen.SampleIDs, bool) {
	by := map[seedState]int64{}
	var any int64
	for _, r := range s.records {
		if r.dt == dt && !r.hidden {
			if by[r.state] == 0 {
				by[r.state] = r.id
			}
			if any == 0 {
				any = r.id
			}
		}
	}
	if any == 0 {
		return docscreen.SampleIDs{}, false
	}
	pick := func(st seedState) int64 {
		if by[st] != 0 {
			return by[st]
		}
		return any
	}
	return docscreen.SampleIDs{
		Draft: pick(stDraft), Pending: pick(stPending), Approved: pick(stApproved), Rejected: pick(stRejected),
		Cancelled: pick(stCancelled), Closed: pick(stClosed), Lines500: pick(stDraft), CloseAs: viewerEmail,
	}, true
}

// docPages are the document routes' URLs: docscreen.Screens on the controls
// (each step, line, reason page and state page), every control's page and
// print page, and only the page and print page of each hidden record; a
// DocType with no control gets Screens on a missing id. Plus each list's
// searches and filters, exports and the search page.
func (s *sweepSeed) docPages() map[string][]sweepPage {
	out := map[string][]sweepPage{}
	add := func(route, u string, hidden bool) {
		out[route] = append(out[route], sweepPage{route, u, hidden})
	}
	for _, dt := range doctype.All() {
		if !dt.IsDocument() {
			continue
		}
		base := "/doc/" + dt.Name
		ids, ok := s.sampleIDs(dt.Name)
		if ok {
			for _, sc := range docscreen.Screens(dt.Name, ids) {
				add(sc.Route, sc.URL, false)
			}
		} else {
			add("/doc/{doctype}", base, false)
			add("/doc/{doctype}/{id}", base+"/999999", false)
		}
		for _, r := range s.records {
			if r.dt != dt.Name {
				continue
			}
			add("/doc/{doctype}/{id}", base+"/"+idText(r.id), r.hidden)
			add("/print/{doctype}/{id}", "/print/"+dt.Name+"/"+idText(r.id), r.hidden)
		}
		if ok {
			add("/print/{doctype}/{id}/page.css", "/print/"+dt.Name+"/"+idText(ids.Approved)+"/page.css", false)
		}
		if !ok {
			add("/print/{doctype}/{id}", "/print/"+dt.Name+"/999999", false)
			add("/print/{doctype}/{id}/page.css", "/print/"+dt.Name+"/999999/page.css", false)
		}
		add("/doc/{doctype}", base+"?q="+hiddenQuery, false)
		add("/doc/{doctype}", base+"?q="+controlQuery, false)
		add("/doc/{doctype}/export.csv", base+"/export.csv?q="+hiddenQuery, false)
		add("/doc/{doctype}/export.csv", base+"/export.csv", false)
		if f, ok := dt.Field("state"); ok && f.Filterable {
			// Screens' filtered list covers approved and pending.
			for _, st := range []doctype.State{doctype.StateDraft, doctype.StateRejected, doctype.StateClosed} {
				q := docscreen.ListQuery{Filters: []docscreen.Filter{{Field: "state", Op: "=", Values: []string{string(st)}}}}.Encode()
				add("/doc/{doctype}", base+"?"+q, false)
			}
		}
	}
	for _, r := range s.records {
		if r.doc.Number != "" && (r.hidden || s.searchable(r.dt)) {
			add("/search", "/search?q="+url.QueryEscape(r.doc.Number[1:]), false) // not the whole number: the page echoes q
		}
	}
	for _, q := range []string{hiddenQuery, controlQuery} {
		add("/search", "/search?q="+q, false)
	}
	return out
}

// fill gives the URLs of a route that isn't a document route, or why it
// can't.
func (s *sweepSeed) fill(route string) ([]sweepPage, string) {
	queries := map[string][]string{
		"/settings/numbering/preview":     {"?tpl=PO%2F%7B%23%23%23%23%23%7D"},
		"/m/import/template":              {"?doctype=Party"},
		"/reports/expense/lines":          {"", "?month=2026-10"},
		"/reports/expense/lines/filter":   {"", "?month=2026-10"},
		"/modules/goodsreceipt/po-search": {"", "?all=1", "?all=1&q=" + hiddenQuery},
	}
	plain := func(us ...string) []sweepPage {
		var out []sweepPage
		for _, u := range us {
			out = append(out, sweepPage{route: route, url: u})
		}
		return out
	}
	switch {
	case strings.HasPrefix(route, "/pick/"):
		var us []string
		for _, q := range []string{"", hiddenQuery, "PRJ"} {
			us = append(us, (&url.URL{Path: route}).EscapedPath()+"?q="+q)
		}
		return plain(us...), ""
	case !strings.ContainsAny(route, "{*"):
		qs, ok := queries[route]
		if !ok {
			qs = []string{""}
		}
		var us []string
		for _, q := range qs {
			us = append(us, route+q)
		}
		return plain(us...), ""
	case strings.HasSuffix(route, "/*"):
		return plain(strings.TrimSuffix(route, "*") + "no-such-file"), ""
	case strings.Contains(route, "{row"):
		return s.partyRows(route), ""
	}
	out := []sweepPage{{route: route, url: route}}
	for _, m := range routeParam.FindAllStringSubmatch(route, -1) {
		cands := s.candidates(route, m[1])
		if len(cands) == 0 {
			return nil, "no values for {" + m[1] + "}"
		}
		var next []sweepPage
		for _, p := range out {
			for _, c := range cands {
				next = append(next, sweepPage{route, strings.Replace(p.url, m[0], url.PathEscape(c.value), 1), p.hidden || c.hidden})
			}
		}
		out = next
	}
	return out, ""
}

// candidate is one value of a route parameter; hidden names a hidden record.
type candidate struct {
	value  string
	hidden bool
}

// candidates are the values the sweep tries for one route parameter.
func (s *sweepSeed) candidates(route, param string) []candidate {
	plain := func(vs ...string) []candidate {
		var out []candidate
		for _, v := range vs {
			out = append(out, candidate{value: v})
		}
		return out
	}
	// ids is one control and one hidden record of dt.
	ids := func(dt string) []candidate {
		var out []candidate
		seen := map[bool]bool{}
		for _, r := range s.records {
			if r.dt == dt && !seen[r.hidden] {
				seen[r.hidden] = true
				out = append(out, candidate{idText(r.id), r.hidden})
			}
		}
		return out
	}
	switch {
	case strings.HasPrefix(route, "/m/"):
		slug := strings.Split(route, "/")[2]
		switch param {
		case "id":
			return plain(s.masterIDs(slug)...)
		case "step":
			return plain("details")
		case "section":
			return plain(masterSections[slug]...)
		}
	case strings.HasPrefix(route, "/modules/purchase/orders/"):
		// Plus the approved control, whose send, hand-over and expected pages
		// draw their forms (controlExpectations).
		return append(ids(purchase.DocType), candidate{value: idText(s.find(purchase.DocType, false, stApproved).id)})
	case strings.HasPrefix(route, "/modules/purchase/requests/"):
		return ids(purchase.RequestDocType)
	case strings.HasPrefix(route, "/modules/expense/advances/"):
		return ids(expense.AdvanceDocType)
	case route == "/modules/purchase/q/{queue}":
		return plain(purchase.QueueDrafts, purchase.QueueWaiting, purchase.QueueToSend, purchase.QueueToClose,
			purchase.QueueAwaitingDelivery, purchase.QueueRequestsToOrder, purchase.QueueMyRequests, "no-such-queue")
	case route == "/files/{id}":
		var out []candidate
		for _, r := range s.records {
			if r.file != 0 {
				out = append(out, candidate{idText(r.file), r.hidden})
			}
		}
		return out
	case strings.HasPrefix(route, "/users/"):
		return plain(idText(int64(adminID)), idText(int64(deepakID)))
	case strings.HasPrefix(route, "/roles/"), strings.HasPrefix(route, "/admin/approval-rules/"), strings.HasPrefix(route, "/settings/numbering/"):
		if param == "doctype" {
			return plain(purchase.DocType, expense.ClaimDocType)
		}
		return plain("1")
	}
	return nil
}

// masterSections are each master's edit sections (MST screens).
var masterSections = map[string][]string{"employee": {"details", "reporting"}, "party": {"details", "gst", "terms"}}

// searchable reports whether dt is in /search (SearchFields).
func (s *sweepSeed) searchable(name string) bool {
	dt, ok := doctype.Lookup(name)
	return ok && len(dt.SearchFields) > 0
}

// partyRows are a party's address or contact row URLs, each row under its
// own party.
func (s *sweepSeed) partyRows(route string) []sweepPage {
	table := "contact"
	if strings.Contains(route, "/addresses/") {
		table = "address"
	}
	var out []sweepPage
	for _, pair := range s.query(`SELECT party_id || ' ' || id FROM "` + table + `" WHERE party_id IS NOT NULL ORDER BY id`) {
		party, row, _ := strings.Cut(pair, " ")
		u := strings.Replace(route, "{id:[1-9][0-9]*}", party, 1)
		out = append(out, sweepPage{route: route, url: strings.Replace(u, "{row:[1-9][0-9]*}", row, 1)})
	}
	if len(out) == 0 { // no rows: the missing row's page is still swept
		u := strings.Replace(route, "{id:[1-9][0-9]*}", "1", 1)
		out = append(out, sweepPage{route: route, url: strings.Replace(u, "{row:[1-9][0-9]*}", "1", 1)})
	}
	return out
}

// masterIDs are the first and last row ids of the master whose list path is
// /m/slug; every row of project and party.
func (s *sweepSeed) masterIDs(slug string) []string {
	for _, dt := range doctype.All() {
		if dt.IsMaster() && strings.ReplaceAll(strings.ToLower(dt.Name), " ", "-") == slug {
			ids := s.query(`SELECT CAST(id AS TEXT) FROM "` + dt.Table + `" ORDER BY id`)
			if len(ids) == 0 {
				return []string{"1"} // an empty master: its missing-record page is still swept
			}
			if slug == "project" || slug == "party" {
				return ids // project B is hidden; the hidden vendor is a party
			}
			return []string{ids[0], ids[len(ids)-1]}
		}
	}
	return nil
}

// query is one text column of q's rows. q names registered tables only.
func (s *sweepSeed) query(q string) []string {
	s.w.t.Helper()
	var out []string
	ctx := context.Background()
	err := s.w.app.DB().Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, q)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	if err != nil {
		s.w.t.Fatalf("%s: %v", q, err)
	}
	return out
}

// controlReports are the derived pages that list a DocType's approved
// records (the pending report: approved orders not fully received).
var controlReports = map[string][]string{
	purchase.DocType:       {"/reports/purchase/pending"},
	expense.ClaimDocType:   {"/reports/expense/open", "/reports/expense/lines"},
	expense.AdvanceDocType: {"/reports/expense/advances"},
}

// controlExpectation is a page that must show a control record.
type controlExpectation struct{ url, want string }

// controlExpectations: each control's list, export, page and print page show
// it, the expense and pending reports and the purchase queues show the
// approved controls, the approved control order's send, hand-over and
// expected pages show its total, and the inbox and My requests show the
// pending ones. The buyer's own records are checkBuyer's.
func (s *sweepSeed) controlExpectations() []controlExpectation {
	var out []controlExpectation
	add := func(u, want string) { out = append(out, controlExpectation{u, want}) }
	for _, r := range s.records {
		if r.hidden || r.buyer {
			continue
		}
		if r.dt == purchase.DocType && r.state == stApproved {
			for _, p := range []string{"send", "handover", "expected"} {
				add("/modules/purchase/orders/"+idText(r.id)+"/"+p, types.FormatMoney(r.amount))
			}
			add("/modules/purchase/q/"+purchase.QueueToSend, r.doc.Number)
			add("/modules/purchase/q/"+purchase.QueueAwaitingDelivery, r.doc.Number)
			// GRN step 1's PO search (S1b, CD-323 (2)): a readable approved
			// order is found by its number, widened to fully received ones.
			add("/modules/goodsreceipt/po-search?all=1", r.doc.Number)
		}
		page := "/doc/" + r.dt + "/" + idText(r.id)
		dt, _ := doctype.Lookup(r.dt)
		_, listsNumber := dt.Field("number")
		if r.state == stDraft {
			add(page+"/edit/check", controlToken) // a writer's draft page redirects to the check step
			add(page+"/lines/1", controlToken)
			add(page+"/lines/1/remove", "Remove line 1")
		} else {
			add(page, controlToken)
		}
		if r.state == stPending && r.dt == purchase.DocType {
			add(page+"/reject", types.FormatMoney(r.amount))
		}
		if r.dt == purchase.DocType {
			// CD-318: the order export works; every readable order is a row,
			// draft and pending ones too, with its vendor and remarks.
			add("/doc/"+r.dt+"/export.csv", text(false, "po-"+string(r.state)))
			add("/doc/"+r.dt+"/export.csv", controlVendor)
		}
		if r.file != 0 {
			add("/files/"+idText(r.file), "%PDF")
		}
		if r.doc.Number != "" {
			if listsNumber {
				add("/doc/"+r.dt, r.doc.Number)
				add("/doc/"+r.dt+"/export.csv", r.doc.Number)
			} else {
				add("/doc/"+r.dt+"/export.csv", controlToken)
			}
			add(page, r.doc.Number)
			if s.searchable(r.dt) {
				// CD-317: number and a free-text field are SearchFields.
				add("/search?q="+url.QueryEscape(r.doc.Number[1:]), r.doc.Number)
				add("/search?q="+controlQuery, dt.PluralLabel+" (")
				add("/doc/"+r.dt+"?q="+controlQuery, r.doc.Number)
			}
			if _, ok := print.Default.Lookup(r.dt); ok {
				add("/print/"+r.dt+"/"+idText(r.id), r.doc.Number)
			}
			for _, rep := range controlReports[r.dt] {
				if r.state != stApproved {
					continue
				}
				add(rep, r.doc.Number)
				if !strings.HasSuffix(rep, ".csv") {
					add(rep+"/export.csv", r.doc.Number)
				}
			}
		}
		switch {
		case r.eligible:
			add("/approvals", types.FormatMoney(r.amount))
		case r.state == stMine:
			add("/approvals/mine", types.FormatMoney(r.amount))
		}
	}
	add("/m/project", projectA)
	add("/pick/Project?q=PRJ", projectA)
	// The home's To send card counts the one approved control, not the
	// hidden approved order too.
	add("/modules/purchase", "1 approved order not sent yet")
	return out
}

// checkBuyer: the buyer (create on orders and requests, amend on orders,
// the viewer's read scope and project-A restriction) opens every queue with
// 200, only no-such-queue answering 404, and each queue shows its control by
// title: the buyer's own draft and pending order, the approved control
// order (to send, to close through its rest row, awaiting delivery), the
// submitted control request and the buyer's own request. The home's cards
// count the controls only. Create PO on the submitted control request shows
// its line; on the hidden one it is 404, because read is checked first. No
// page names a hidden record, though the buyer owns a hidden draft, pending
// order and request on project B and the hidden approved order has a rest
// row too.
func (s *sweepSeed) checkBuyer(t *testing.T, hidden []hiddenMarks) {
	t.Helper()
	own := func(dt string, st seedState) *sweepRecord {
		for _, r := range s.records {
			if r.buyer && !r.hidden && r.dt == dt && r.state == st {
				return r
			}
		}
		t.Fatalf("no buyer's control %s %s seeded", dt, st)
		return nil
	}
	po := s.find(purchase.DocType, false, stApproved)
	pr := s.find(purchase.RequestDocType, false, stApproved)
	hiddenPR := s.find(purchase.RequestDocType, true, stApproved)
	const ok, missing = http.StatusOK, http.StatusNotFound
	q := "/modules/purchase/q/"
	pages := []struct {
		url    string
		status int
		want   []string
	}{
		{q + purchase.QueueDrafts, ok, []string{s.display(own(purchase.DocType, stDraft))}},
		{q + purchase.QueueWaiting, ok, []string{s.display(own(purchase.DocType, stPending))}},
		{q + purchase.QueueToSend, ok, []string{po.doc.Number}},
		{q + purchase.QueueToClose, ok, []string{po.doc.Number}},
		{q + purchase.QueueAwaitingDelivery, ok, []string{po.doc.Number}},
		{q + purchase.QueueRequestsToOrder, ok, []string{pr.doc.Number}},
		{q + purchase.QueueMyRequests, ok, []string{s.display(own(purchase.RequestDocType, stDraft))}},
		{q + "no-such-queue", missing, nil},
		// Each card's text runs together: its title, its figure, its line.
		{"/modules/purchase", ok, []string{
			"My drafts" + "1" + "1 order", "To send" + "1" + "1 approved order not sent yet",
			"To close" + "1" + "1 order the vendor won't complete", "Requests to order" + "1" + "1 request with items to order",
			"Waiting for approval" + "1" + "1 order", "My purchase requests" + "1" + "1 open",
			"Awaiting delivery" + "1" + "1 order not fully received",
		}},
		{"/modules/purchase/requests/" + idText(pr.id) + "/order", ok, []string{text(false, "pr-line")}},
		{"/modules/purchase/requests/" + idText(hiddenPR.id) + "/order", missing, nil},
	}
	b := s.w.signedIn(buyerEmail)
	for _, p := range pages {
		r := b.get(p.url)
		if r.status != p.status {
			t.Errorf("buyer GET %s = %d, want %d", p.url, r.status, p.status)
		}
		for _, w := range p.want {
			if !shows(r.pageText(), w) {
				t.Errorf("buyer GET %s doesn't show the control's %q: the page shows no data, so the check proves nothing there", p.url, w)
			}
		}
		for _, m := range []string{"Rajkot Castings", hiddenVendor, hiddenToken} {
			if strings.Contains(r.body, m) {
				t.Errorf("buyer GET %s names a hidden record's vendor or text: %q", p.url, m)
			}
		}
		for _, h := range hidden {
			if m := leak(r.body, h.markers, h.url); m != "" {
				t.Errorf("buyer GET %s names hidden %s %d: %q", p.url, h.r.label, h.r.id, m)
			}
		}
	}
}

// shows reports whether text holds want not followed by a digit, so "Draft
// 4" doesn't match "Draft 41".
func shows(text, want string) bool {
	return regexp.MustCompile(regexp.QuoteMeta(want) + `([^0-9]|$)`).MatchString(text)
}

// display is r's title as DOC gives it: its number, or "Draft {id}".
func (s *sweepSeed) display(r *sweepRecord) string {
	s.w.t.Helper()
	var out string
	ctx := context.Background()
	err := s.w.app.DB().Read(ctx, func(tx *db.Tx) error {
		info, err := s.w.app.Engine().Info(ctx, tx, r.dt, r.id)
		out = info.Display
		return err
	})
	if err != nil {
		s.w.t.Fatalf("info of %s %d: %v", r.dt, r.id, err)
	}
	return out
}
