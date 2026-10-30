package authweb_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

const (
	t15Meena  = "meena@vardhan.test"
	t15Kavya  = "kavya@vardhan.test"
	t15NotFnd = "This record doesn't exist or you don't have access to it."
)

// t15Lines returns body's trimmed, non-empty lines.
func t15Lines(body string) []string {
	var out []string
	for l := range strings.SplitSeq(body, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// t15Rows returns the record lines of a probe list or picker body (one
// number, code or "Draft {id}" per line; the Count and Total lines dropped),
// sorted.
func t15Rows(body string) []string {
	out := []string{}
	for _, l := range t15Lines(body) {
		if strings.HasPrefix(l, "Count:") || strings.HasPrefix(l, "Total:") {
			continue
		}
		out = append(out, l)
	}
	slices.Sort(out)
	return out
}

// t15CSVRows checks the export header "number" and returns the data rows,
// sorted.
func t15CSVRows(t *testing.T, what string, body string) []string {
	t.Helper()
	lines := t15Lines(body)
	if len(lines) == 0 || lines[0] != "number" {
		t.Fatalf("%s: CSV %q, want header line \"number\"", what, body)
	}
	out := append([]string{}, lines[1:]...)
	slices.Sort(out)
	return out
}

// t15GetRows GETs path, wants 200 and returns its rows.
func t15GetRows(t *testing.T, c *webtest.Client, what, path string) []string {
	t.Helper()
	r := c.Get(path)
	wantStatus(t, what+" GET "+path, r, http.StatusOK)
	return t15Rows(r.Body)
}

func t15WantRows(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: rows = %q, want %q", what, got, want)
	}
}

func t15WantLine(t *testing.T, what, body, line string) {
	t.Helper()
	if !slices.Contains(t15Lines(body), line) {
		t.Fatalf("%s: body %q lacks the line %q", what, body, line)
	}
}

func t15Draft(id int64) string { return "Draft " + t14ID(id) }

// t15Claim adds an Expense Claim; zero project, site and state default to
// PRJ-METRO, METRO-SITE and draft (Priya is restricted to PRJ-METRO).
func t15Claim(t *testing.T, h *webtest.Harness, d webtest.Doc) int64 {
	t.Helper()
	if d.ProjectID == 0 {
		d.ProjectID = h.W.Projects["PRJ-METRO"]
	}
	if d.SiteID == 0 {
		d.SiteID = h.W.Sites["METRO-SITE"]
	}
	if d.State == "" {
		d.State = string(doctype.StateDraft)
	}
	return h.AddDoc(t, authtest.DTClaim, d)
}

// t15GRN adds an approved GRN at site, created by createdBy.
func t15GRN(t *testing.T, h *webtest.Harness, number, site string, createdBy auth.UserID) int64 {
	t.Helper()
	return h.AddDoc(t, authtest.DTReceipt, webtest.Doc{
		Number: number, State: string(doctype.StateApproved), CreatedBy: createdBy, SiteID: h.W.Sites[site],
	})
}

// t15StatusOf is the HTTP status a Can error is sent with.
func t15StatusOf(t *testing.T, what string, err error) int {
	t.Helper()
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, auth.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, auth.ErrForbidden):
		return http.StatusForbidden
	}
	t.Fatalf("%s: unexpected error %v", what, err)
	return 0
}

// t15WantNotFoundErr checks that err is ErrNotFound (404), not ErrForbidden.
func t15WantNotFoundErr(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, auth.ErrNotFound) || errors.Is(err, auth.ErrForbidden) {
		t.Errorf("%s: err = %v, want auth.ErrNotFound (404), not ErrForbidden", what, err)
	}
}

// AUTH-085
func TestOwnScope_ListFiltered(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	t15Claim(t, h, webtest.Doc{Number: "EX/26-27/00001", CreatedBy: w.Users[t10Deepak], EmployeeID: w.Employees["EMP-007"], Amount: 120000})
	t15Claim(t, h, webtest.Doc{Number: "EX/26-27/00002", CreatedBy: w.Users[t14Suresh], EmployeeID: w.Employees["EMP-005"], Amount: 50000})

	deepak := signedIn(t, h, t10Deepak)
	r := deepak.Get("/expense/list")
	wantStatus(t, "deepak GET /expense/list", r, http.StatusOK)
	t15WantRows(t, "deepak /expense/list", t15Rows(r.Body), "EX/26-27/00001")
	t15WantLine(t, "deepak /expense/list", r.Body, "Count: 1")
	t15WantLine(t, "deepak /expense/list", r.Body, "Total: 120000")
}

// AUTH-086
func TestOwnScope_OthersNotFound(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	mine := t15Claim(t, h, webtest.Doc{Number: "EX/26-27/00001", CreatedBy: w.Users[t10Deepak], EmployeeID: w.Employees["EMP-007"], Amount: 120000})
	other := t15Claim(t, h, webtest.Doc{Number: "EX/26-27/00002", CreatedBy: w.Users[t14Suresh], EmployeeID: w.Employees["EMP-005"], Amount: 50000})

	deepak := signedIn(t, h, t10Deepak)
	// Control: Deepak's own claim opens, so the 404s below come from scope.
	wantStatus(t, "deepak GET own claim", deepak.Get("/expense/"+t14ID(mine)), http.StatusOK)
	for _, suffix := range []string{"", "/print", "/pdf"} {
		r := deepak.Get("/expense/" + t14ID(other) + suffix)
		t14Want404(t, "deepak GET EX/26-27/00002"+suffix, r)
		if strings.Contains(r.Body, "EX/26-27/00002") {
			t.Fatalf("deepak GET EX/26-27/00002%s: body leaks its number: %q", suffix, r.Body)
		}
	}
}

// AUTH-087
func TestOwnScope_WriteDeleteOwnOnly(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	po10 := t14PO(t, h, webtest.Doc{CreatedBy: w.Users[t10Priya]}) // Draft 10
	po11 := t14PO(t, h, webtest.Doc{CreatedBy: w.Users[t10Ravi]})  // Draft 11
	rows := func(id int64) int64 { return t14Int(t, w, `SELECT count(*) FROM probe_order WHERE id = ?`, id) }

	priya := signedIn(t, h, t10Priya)
	t14Want403(t, "priya delete Ravi's PO", priya.Post("/po/"+t14ID(po11)+"/delete", url.Values{}),
		"You can only delete Purchase Orders you created.")
	if n := rows(po11); n != 1 {
		t.Fatalf("Ravi's PO rows after priya's delete = %d, want 1", n)
	}

	wantStatus(t, "priya delete own PO", priya.Post("/po/"+t14ID(po10)+"/delete", url.Values{}), http.StatusSeeOther)
	if n := rows(po10); n != 0 {
		t.Fatalf("Priya's PO rows after her delete = %d, want 0", n)
	}

	// Create is not limited by own scope.
	t14CreatedID(t, "priya POST /po", priya.Post("/po", url.Values{
		"project_id": {t14ID(w.Projects["PRJ-METRO"])},
		"site_id":    {t14ID(w.Sites["METRO-SITE"])},
	}))
}

// AUTH-088
func TestOwnScope_LinkedEmployee(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	id := t15Claim(t, h, webtest.Doc{Number: "EX/26-27/00003", CreatedBy: w.Users[t14Admin], EmployeeID: w.Employees["EMP-007"]})

	deepak := signedIn(t, h, t10Deepak)
	if rows := t15GetRows(t, deepak, "deepak", "/expense/list"); !slices.Contains(rows, "EX/26-27/00003") {
		t.Fatalf("deepak /expense/list rows = %q, want EX/26-27/00003 (for EMP-007)", rows)
	}
	r := deepak.Get("/expense/" + t14ID(id))
	wantStatus(t, "deepak GET EX/26-27/00003", r, http.StatusOK)
	if !strings.Contains(r.Body, "EX/26-27/00003") {
		t.Fatalf("deepak GET EX/26-27/00003: body %q lacks its number", r.Body)
	}
}

// AUTH-090
func TestOwnScope_ExportFiltered(t *testing.T) {
	w := authtest.NewWorld(t)
	w.SetRights(t, "Employee", authtest.DTClaim, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeTeam, auth.RightApprove: auth.ScopeTeam,
		auth.RightCreate: auth.ScopeOwn, auth.RightWrite: auth.ScopeOwn,
		auth.RightSubmit: auth.ScopeOwn, auth.RightPrint: auth.ScopeOwn,
		auth.RightExport: auth.ScopeOwn,
	})
	h := t14Probe(t, w)
	t15Claim(t, h, webtest.Doc{Number: "EX/26-27/00001", CreatedBy: w.Users[t10Deepak], EmployeeID: w.Employees["EMP-007"], Amount: 120000})
	t15Claim(t, h, webtest.Doc{Number: "EX/26-27/00002", CreatedBy: w.Users[t14Suresh], EmployeeID: w.Employees["EMP-005"], Amount: 50000})
	t15Claim(t, h, webtest.Doc{Number: "EX/26-27/00003", CreatedBy: w.Users[t14Admin], EmployeeID: w.Employees["EMP-007"]})

	deepak := signedIn(t, h, t10Deepak)
	r := deepak.Get("/expense/export.csv")
	wantStatus(t, "deepak GET /expense/export.csv", r, http.StatusOK)
	export := t15CSVRows(t, "deepak export", r.Body)
	t15WantRows(t, "deepak export", export, "EX/26-27/00001", "EX/26-27/00003")
	list := t15GetRows(t, deepak, "deepak", "/expense/list")
	if !reflect.DeepEqual(export, list) {
		t.Fatalf("deepak export rows %q != list rows %q", export, list)
	}
}

// AUTH-091
func TestRestriction_SiteList(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	grn1 := t15GRN(t, h, "GR/26-27/00001", "PUNE-WH", w.Users[t14Suresh])
	grn2 := t15GRN(t, h, "GR/26-27/00002", "TECH-SITE", w.Users[t14Admin])

	suresh := signedIn(t, h, t14Suresh)
	r := suresh.Get("/grn/list")
	wantStatus(t, "suresh GET /grn/list", r, http.StatusOK)
	t15WantRows(t, "suresh /grn/list", t15Rows(r.Body), "GR/26-27/00001")
	t15WantLine(t, "suresh /grn/list", r.Body, "Count: 1")
	wantStatus(t, "suresh GET GR/26-27/00001", suresh.Get("/grn/"+t14ID(grn1)), http.StatusOK)
	t14Want404(t, "suresh GET GR/26-27/00002", suresh.Get("/grn/"+t14ID(grn2)))
}

// AUTH-092
func TestRestriction_ProjectList(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	t14PO(t, h, webtest.Doc{Number: "PO/26-27/00020", State: string(doctype.StateApproved)})
	po21 := t14PO(t, h, webtest.Doc{
		Number: "PO/26-27/00021", State: string(doctype.StateApproved),
		ProjectID: w.Projects["PRJ-TECHPARK"], SiteID: w.Sites["TECH-SITE"],
	})

	priya := signedIn(t, h, t10Priya)
	t15WantRows(t, "priya /po/list", t15GetRows(t, priya, "priya", "/po/list"), "PO/26-27/00020")
	r := priya.Get("/po/export.csv")
	wantStatus(t, "priya GET /po/export.csv", r, http.StatusOK)
	t15WantRows(t, "priya export", t15CSVRows(t, "priya export", r.Body), "PO/26-27/00020")
	t14Want404(t, "priya GET PO/26-27/00021", priya.Get("/po/"+t14ID(po21)))
}

// AUTH-093
func TestRestriction_StrictEmptyHidden(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	h.AddDoc(t, authtest.DTOrder, webtest.Doc{
		Number: "PO/26-27/00022", State: string(doctype.StateApproved),
		CreatedBy: w.Users[t10Priya], SiteID: w.Sites["METRO-SITE"], // no Project
	})

	priya := signedIn(t, h, t10Priya)
	if rows := t15GetRows(t, priya, "priya", "/po/list"); slices.Contains(rows, "PO/26-27/00022") {
		t.Fatalf("priya (restricted to PRJ-METRO) /po/list rows = %q, must not contain PO/26-27/00022 (no Project)", rows)
	}
	ravi := signedIn(t, h, t10Ravi)
	if rows := t15GetRows(t, ravi, "ravi", "/po/list"); !slices.Contains(rows, "PO/26-27/00022") {
		t.Fatalf("ravi (unrestricted) /po/list rows = %q, want PO/26-27/00022", rows)
	}
}

// AUTH-094
func TestRestriction_ChildLinksChecked(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	po23 := t14PO(t, h, webtest.Doc{
		Number: "PO/26-27/00023", State: string(doctype.StateApproved),
		SiteID: w.Sites["PUNE-WH"], LineSiteIDs: []int64{w.Sites["TECH-SITE"]},
	})

	// Control: the PO exists and opens for an unrestricted user.
	ravi := signedIn(t, h, t10Ravi)
	wantStatus(t, "ravi GET PO/26-27/00023", ravi.Get("/po/"+t14ID(po23)), http.StatusOK)

	suresh := signedIn(t, h, t14Suresh)
	r := suresh.Get("/po/" + t14ID(po23))
	t14Want404(t, "suresh GET PO/26-27/00023 (line at TECH-SITE)", r)
	if strings.Contains(r.Body, "PO/26-27/00023") {
		t.Fatalf("suresh GET PO/26-27/00023: body leaks its number: %q", r.Body)
	}
}

// AUTH-098
func TestRestriction_LinkPickerFiltered(t *testing.T) {
	w := authtest.NewWorld(t)
	w.Grant(t, "Site Reader", authtest.DTSite, auth.RightRead, auth.ScopeAll)
	w.Grant(t, "Site Reader", authtest.DTProject, auth.RightRead, auth.ScopeAll)
	w.AddRole(t, t14Suresh, "Site Reader")
	h := t14Probe(t, w)

	suresh := signedIn(t, h, t14Suresh)
	t15WantRows(t, "suresh /site/picker", t15GetRows(t, suresh, "suresh", "/site/picker?q="), "PUNE-WH", "METRO-SITE")
	t15WantRows(t, "suresh /project/picker", t15GetRows(t, suresh, "suresh", "/project/picker?q="), "PRJ-METRO", "PRJ-TECHPARK")
}

// AUTH-099
func TestRestriction_SaveDisallowedValue(t *testing.T) {
	w := authtest.NewWorld(t)
	w.Restrict(t, t14Suresh, nil, []string{"PUNE-WH", "METRO-SITE"})
	h := t14Probe(t, w)
	receipts := func() int64 { return t14Int(t, w, `SELECT count(*) FROM probe_receipt`) }
	before := receipts()

	suresh := signedIn(t, h, t14Suresh)
	t14Want403(t, "suresh POST /grn at TECH-SITE",
		suresh.Post("/grn", url.Values{"site_id": {t14ID(w.Sites["TECH-SITE"])}}),
		"You can only use Sites METRO-SITE, PUNE-WH. Choose one of your allowed sites.")
	if n := receipts(); n != before {
		t.Fatalf("probe_receipt rows after the rejected POST = %d, want %d", n, before)
	}
}

// AUTH-101
func TestRestriction_LinkedDocumentMustBeVisible(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	po30 := t14PO(t, h, webtest.Doc{
		Number: "PO/26-27/00030", State: string(doctype.StateApproved),
		ProjectID: w.Projects["PRJ-TECHPARK"], SiteID: w.Sites["TECH-SITE"],
	})
	receipts := func() int64 { return t14Int(t, w, `SELECT count(*) FROM probe_receipt`) }

	suresh := signedIn(t, h, t14Suresh)
	t14Want403(t, "suresh POST /grn linking PO/26-27/00030",
		suresh.Post("/grn", url.Values{
			"site_id":           {t14ID(w.Sites["PUNE-WH"])},
			"purchase_order_id": {t14ID(po30)},
		}),
		"You can't use this Purchase Order. It isn't available to you.")
	if n := receipts(); n != 0 {
		t.Fatalf("probe_receipt rows after the rejected POST = %d, want 0", n)
	}

	if rows := t15GetRows(t, suresh, "suresh", "/po/picker?q="); slices.Contains(rows, "PO/26-27/00030") {
		t.Fatalf("suresh /po/picker rows = %q, must not contain PO/26-27/00030", rows)
	}
	// Control: the picker lists it for an unrestricted user.
	ravi := signedIn(t, h, t10Ravi)
	if rows := t15GetRows(t, ravi, "ravi", "/po/picker?q="); !slices.Contains(rows, "PO/26-27/00030") {
		t.Fatalf("ravi /po/picker rows = %q, want PO/26-27/00030", rows)
	}
}

// AUTH-106
func TestScope_AllChannelsConsistent(t *testing.T) {
	w := authtest.NewWorld(t)
	// Stores has no GRN export in the seed; export is granted so channel (f)
	// returns rows rather than a 403 (fixture rule: a test adds what it needs).
	w.Grant(t, "Stores", authtest.DTReceipt, auth.RightExport, auth.ScopeAll)
	h := t14Probe(t, w)
	t15GRN(t, h, "GR/26-27/00001", "PUNE-WH", w.Users[t14Suresh])
	grn2 := t15GRN(t, h, "GR/26-27/00002", "TECH-SITE", w.Users[t14Admin])
	const hidden = "GR/26-27/00002"
	suresh := signedIn(t, h, t14Suresh)

	for _, tc := range []struct {
		name, path string
		status     int
		check      func(t *testing.T, body string)
	}{
		{"list", "/grn/list", http.StatusOK, func(t *testing.T, body string) {
			t15WantRows(t, "list", t15Rows(body), "GR/26-27/00001")
			t15WantLine(t, "list", body, "Count: 1")
		}},
		{"count", "/grn/count", http.StatusOK, func(t *testing.T, body string) {
			t15WantLine(t, "count", body, "Count: 1")
		}},
		{"search", "/search?q=" + url.QueryEscape(hidden), http.StatusOK, func(t *testing.T, body string) {
			t15WantRows(t, "search", t15Rows(body))
		}},
		{"search control", "/search?q=" + url.QueryEscape("GR/26-27/00001"), http.StatusOK, func(t *testing.T, body string) {
			t15WantRows(t, "search control", t15Rows(body), "GR/26-27/00001")
		}},
		{"picker", "/grn/picker?q=", http.StatusOK, func(t *testing.T, body string) {
			t15WantRows(t, "picker", t15Rows(body), "GR/26-27/00001")
		}},
		{"print", "/grn/" + t14ID(grn2) + "/print", http.StatusNotFound, func(t *testing.T, body string) {
			if !strings.Contains(body, t15NotFnd) {
				t.Fatalf("print: body %q, want the not-found text", body)
			}
		}},
		{"export", "/grn/export.csv", http.StatusOK, func(t *testing.T, body string) {
			t15WantRows(t, "export", t15CSVRows(t, "export", body), "GR/26-27/00001")
		}},
		{"api", "/api/grn?site=TECH-SITE", http.StatusOK, func(t *testing.T, body string) {
			if got := strings.TrimSpace(body); got != "[]" {
				t.Fatalf("api: body %q, want []", got)
			}
		}},
		{"api control", "/api/grn?site=PUNE-WH", http.StatusOK, func(t *testing.T, body string) {
			if !strings.Contains(body, "GR/26-27/00001") {
				t.Fatalf("api control: body %q, want GR/26-27/00001", body)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := suresh.Get(tc.path)
			wantStatus(t, "suresh GET "+tc.path, r, tc.status)
			if strings.Contains(r.Body, hidden) {
				t.Fatalf("suresh GET %s: body confirms %s: %q", tc.path, hidden, r.Body)
			}
			tc.check(t, r.Body)
		})
	}

	// Every registered GET route on a GRN record answers 404 for GR/26-27/00002.
	var paths []string
	if err := chi.Walk(h.Router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet || !strings.Contains(route, "{id}") {
			return nil
		}
		if strings.HasPrefix(route, "/grn/") || strings.HasPrefix(route, "/api/grn/") {
			paths = append(paths, strings.ReplaceAll(route, "{id}", t14ID(grn2)))
		}
		return nil
	}); err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	for _, want := range []string{"/grn/{id}", "/api/grn/{id}", "/grn/{id}/print", "/grn/{id}/pdf"} {
		if p := strings.ReplaceAll(want, "{id}", t14ID(grn2)); !slices.Contains(paths, p) {
			t.Fatalf("router GET routes on a GRN = %q, want %s among them", paths, want)
		}
	}
	for _, p := range paths {
		r := suresh.Get(p)
		t14Want404(t, "suresh GET "+p, r)
		if strings.Contains(r.Body, hidden) {
			t.Fatalf("suresh GET %s: body confirms %s: %q", p, hidden, r.Body)
		}
	}
}

// AUTH-108
func TestScope_IDGuessingNoLeak(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	deepakID, emp7 := w.Users[t10Deepak], w.Employees["EMP-007"]
	t15Claim(t, h, webtest.Doc{CreatedBy: w.Users[t14Suresh], EmployeeID: w.Employees["EMP-005"]})
	own1 := t15Claim(t, h, webtest.Doc{CreatedBy: deepakID, EmployeeID: emp7})
	t15Claim(t, h, webtest.Doc{CreatedBy: w.Users[t10Priya], EmployeeID: w.Employees["EMP-004"]})
	own2 := t15Claim(t, h, webtest.Doc{CreatedBy: deepakID, EmployeeID: emp7})
	t15Claim(t, h, webtest.Doc{CreatedBy: w.Users[t10Ravi], EmployeeID: w.Employees["EMP-003"]})
	owned := []int64{own1, own2}

	deepak := signedIn(t, h, t10Deepak)
	var first string
	var firstID int64
	for id := int64(1); id <= 50; id++ {
		r := deepak.Get("/expense/" + t14ID(id))
		if slices.Contains(owned, id) {
			wantStatus(t, "deepak GET own claim "+t14ID(id), r, http.StatusOK)
			continue
		}
		t14Want404(t, "deepak GET /expense/"+t14ID(id), r)
		body := r.Body
		if tok := r.Header.Get("X-Test-Csrf"); tok != "" {
			body = strings.ReplaceAll(body, tok, "{csrf}")
		}
		if firstID == 0 {
			first, firstID = body, id
			continue
		}
		if body != first {
			t.Fatalf("deepak GET /expense/%d: 404 body %q (%d bytes) differs from /expense/%d's %q (%d bytes)",
				id, body, len(body), firstID, first, len(first))
		}
	}
}

// AUTH-109
func TestScope_AttachmentsFollowParent(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	grn2 := t15GRN(t, h, "GR/26-27/00002", "TECH-SITE", w.Users[t14Admin])
	content := []byte("%PDF-1.7 invoice_8812 probe attachment")
	file := "/files/" + t14ID(h.AddAttachment(t, authtest.DTReceipt, grn2, "invoice_8812.pdf", content))

	suresh := signedIn(t, h, t14Suresh)
	r := suresh.Get(file)
	t14Want404(t, "suresh GET invoice_8812.pdf", r)
	if strings.Contains(r.Body, string(content)) {
		t.Fatalf("suresh GET invoice_8812.pdf: body leaks the file: %q", r.Body)
	}

	deepak := signedIn(t, h, t10Deepak)
	t14Want403(t, "deepak GET invoice_8812.pdf", deepak.Get(file),
		"You don't have permission to read GRN. Ask your administrator for a role that allows it.", string(content))

	admin := signedIn(t, h, t14Admin)
	r = admin.Get(file)
	wantStatus(t, "admin GET invoice_8812.pdf", r, http.StatusOK)
	if r.Body != string(content) {
		t.Fatalf("admin GET invoice_8812.pdf: body %q, want exactly %q", r.Body, content)
	}
}

// AUTH-110
func TestScope_FiltersCannotWiden(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	admin := w.Users[t14Admin]
	t15GRN(t, h, "GR/26-27/00001", "PUNE-WH", admin)
	t15GRN(t, h, "GR/26-27/00002", "TECH-SITE", admin)

	suresh := signedIn(t, h, t14Suresh)
	r := suresh.Get("/grn/list?site=TECH-SITE")
	wantStatus(t, "suresh GET /grn/list?site=TECH-SITE", r, http.StatusOK)
	t15WantRows(t, "suresh ?site=TECH-SITE", t15Rows(r.Body))
	t15WantLine(t, "suresh ?site=TECH-SITE", r.Body, "Count: 0")

	for _, q := range []string{"?created_by=" + strconv.FormatInt(int64(admin), 10), "?sort=site"} {
		t15WantRows(t, "suresh /grn/list"+q, t15GetRows(t, suresh, "suresh", "/grn/list"+q), "GR/26-27/00001")
	}
}

// AUTH-112
func TestScope_APIEqualsUI(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	metro, techpark := w.Projects["PRJ-METRO"], w.Projects["PRJ-TECHPARK"]
	pune, metroSite, techSite := w.Sites["PUNE-WH"], w.Sites["METRO-SITE"], w.Sites["TECH-SITE"]
	priyaID, raviID := w.Users[t10Priya], w.Users[t10Ravi]
	var pos []int64
	for _, d := range []webtest.Doc{
		{ProjectID: metro, SiteID: metroSite, CreatedBy: priyaID},
		{ProjectID: metro, SiteID: pune, CreatedBy: raviID},
		{ProjectID: metro, SiteID: techSite, CreatedBy: priyaID},
		{ProjectID: techpark, SiteID: techSite, CreatedBy: raviID},
		{ProjectID: techpark, SiteID: pune, CreatedBy: priyaID},
		{SiteID: metroSite, CreatedBy: raviID}, // no Project
		{ProjectID: metro, SiteID: pune, CreatedBy: priyaID, LineSiteIDs: []int64{techSite}},
		{ProjectID: techpark, SiteID: metroSite, CreatedBy: raviID, LineSiteIDs: []int64{metroSite}},
	} {
		d.State = string(doctype.StateApproved)
		pos = append(pos, h.AddDoc(t, authtest.DTOrder, d))
	}

	ctx := context.Background()
	seen := map[int]bool{}
	for _, email := range []string{t14Admin, t15Meena, t10Ravi, t10Priya, t14Suresh, t15Kavya, t10Deepak} {
		c := signedIn(t, h, email)
		p := w.P(t, email)
		for _, id := range pos {
			ref := h.Ref(t, authtest.DTOrder, id)
			what := email + " PO " + t14ID(id)
			want := t15StatusOf(t, what, w.Auth.Can(ctx, p, authtest.DTOrder, auth.RightRead, &ref))
			ui := c.Get("/po/" + t14ID(id)).Status
			api := c.Get("/api/po/" + t14ID(id)).Status
			if ui != api {
				t.Errorf("%s: /po/{id} status %d != /api/po/{id} status %d", what, ui, api)
			}
			if ui != want {
				t.Errorf("%s: /po/{id} status %d, want %d (Can(read))", what, ui, want)
			}
			seen[ui] = true
		}
	}
	for _, s := range []int{http.StatusOK, http.StatusForbidden, http.StatusNotFound} {
		if !seen[s] {
			t.Errorf("no (user, PO) pair answered %d; the table must cover allow, 403 and 404", s)
		}
	}
}

// AUTH-116
func TestTeamScope_DirectReportVisibleAndApprovable(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	exp40 := t15Claim(t, h, webtest.Doc{
		State: string(doctype.StatePending), CreatedBy: w.Users[t10Deepak], EmployeeID: w.Employees["EMP-007"], Amount: 120000,
	})
	exp41 := t15Claim(t, h, webtest.Doc{
		State: string(doctype.StatePending), CreatedBy: w.Users[t14Suresh], EmployeeID: w.Employees["EMP-005"],
	})

	priya := signedIn(t, h, t10Priya)
	rows := t15GetRows(t, priya, "priya", "/expense/list")
	if !slices.Contains(rows, t15Draft(exp40)) || slices.Contains(rows, t15Draft(exp41)) {
		t.Fatalf("priya /expense/list rows = %q, want %s and not %s", rows, t15Draft(exp40), t15Draft(exp41))
	}
	wantStatus(t, "priya GET Deepak's claim", priya.Get("/expense/"+t14ID(exp40)), http.StatusOK)
	t14Want404(t, "priya GET Suresh's claim", priya.Get("/expense/"+t14ID(exp41)))

	// UsersWithRight(approve): Anil (Administrator), Meena (Director, all),
	// Priya (team) and Deepak (team includes own). APR's self-approval rule,
	// not AUTH, removes Deepak.
	r := priya.Get("/expense/" + t14ID(exp40) + "/approvers")
	wantStatus(t, "priya GET approvers", r, http.StatusOK)
	got := []int64{}
	for _, l := range t15Lines(r.Body) {
		id, err := strconv.ParseInt(l, 10, 64)
		if err != nil {
			t.Fatalf("approvers body %q: line %q is not a user ID", r.Body, l)
		}
		got = append(got, id)
	}
	want := []int64{
		int64(w.Users[t14Admin]), int64(w.Users[t15Meena]), int64(w.Users[t10Priya]), int64(w.Users[t10Deepak]),
	}
	if !reflect.DeepEqual(want, []int64{1, 2, 4, 7}) {
		t.Fatalf("fixture user IDs = %v, want Anil 1, Meena 2, Priya 4, Deepak 7", want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("approvers of Deepak's claim = %v, want %v", got, want)
	}

	// AUTH's share: Can(approve) passes; in the app the approve goes through
	// DOC's visibility check and APR's CheckApprover (CD-52).
	wantStatus(t, "priya approve Deepak's claim", priya.Post("/expense/"+t14ID(exp40)+"/approve", url.Values{}), http.StatusSeeOther)
	if got := t14Str(t, w, `SELECT coalesce(state, '') FROM probe_claim WHERE id = ?`, exp40); got != string(doctype.StateApproved) {
		t.Fatalf("Deepak's claim state after priya's approve = %q, want approved", got)
	}
}

// AUTH-117
func TestTeamScope_IndirectReportExcluded(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	exp40 := t15Claim(t, h, webtest.Doc{
		State: string(doctype.StatePending), CreatedBy: w.Users[t10Deepak], EmployeeID: w.Employees["EMP-007"], Amount: 120000,
	})
	state := func() string {
		return t14Str(t, w, `SELECT coalesce(state, '') FROM probe_claim WHERE id = ?`, exp40)
	}

	// Visibility is checked before anything about approval (CD-52).
	threeNotFound := func(who string, c *webtest.Client) {
		t.Helper()
		if rows := t15GetRows(t, c, who, "/expense/list"); slices.Contains(rows, t15Draft(exp40)) {
			t.Fatalf("%s /expense/list rows = %q, must not contain %s", who, rows, t15Draft(exp40))
		}
		t14Want404(t, who+" GET Deepak's claim", c.Get("/expense/"+t14ID(exp40)))
		t14Want404(t, who+" approve Deepak's claim", c.Post("/expense/"+t14ID(exp40)+"/approve", url.Values{}))
		if got := state(); got != string(doctype.StatePending) {
			t.Fatalf("state after %s's approve = %q, want pending_approval", who, got)
		}
	}

	threeNotFound("ravi", signedIn(t, h, t10Ravi))

	meena := signedIn(t, h, t15Meena)
	w.SetRoles(t, t15Meena, "Employee")
	threeNotFound("meena (Employee only)", meena)

	w.SetRoles(t, t15Meena, "Director")
	wantStatus(t, "meena (Director) GET Deepak's claim", meena.Get("/expense/"+t14ID(exp40)), http.StatusOK)
}

// AUTH-118
func TestTeamScope_LeftEmployeeIgnored(t *testing.T) {
	w := authtest.NewWorld(t)
	w.Dir.SetManager(w.Employees["EMP-008"], w.Employees["EMP-004"])
	h := t14Probe(t, w)
	exp42 := t15Claim(t, h, webtest.Doc{CreatedBy: w.Users[t14Admin], EmployeeID: w.Employees["EMP-008"]})
	exp43 := t15Claim(t, h, webtest.Doc{CreatedBy: w.Users[t14Suresh], EmployeeID: w.Employees["EMP-005"]})

	priya := signedIn(t, h, t10Priya)
	if rows := t15GetRows(t, priya, "priya", "/expense/list"); slices.Contains(rows, t15Draft(exp42)) {
		t.Fatalf("priya /expense/list rows = %q, must not contain %s (EMP-008 has left)", rows, t15Draft(exp42))
	}
	t14Want404(t, "priya GET EMP-008's claim", priya.Get("/expense/"+t14ID(exp42)))

	ravi := signedIn(t, h, t10Ravi)
	listed := func() bool { return slices.Contains(t15GetRows(t, ravi, "ravi", "/expense/list"), t15Draft(exp43)) }
	wantStatus(t, "ravi GET Suresh's claim", ravi.Get("/expense/"+t14ID(exp43)), http.StatusOK)
	if !listed() {
		t.Fatalf("ravi /expense/list lacks %s while Suresh is active", t15Draft(exp43))
	}

	// Team membership is read on every check: no sign-out, no perm_version bump.
	permVersion := func() int64 { return t14Int(t, w, `SELECT perm_version FROM auth_meta WHERE id = 1`) }
	pv := permVersion()
	w.Dir.SetActive(w.Employees["EMP-005"], false)
	if got := permVersion(); got != pv {
		t.Fatalf("perm_version after SetActive(EMP-005, false) = %d, want %d", got, pv)
	}
	t14Want404(t, "ravi GET Suresh's claim after Suresh left", ravi.Get("/expense/"+t14ID(exp43)))
	if listed() {
		t.Fatalf("ravi /expense/list still shows %s after Suresh left", t15Draft(exp43))
	}

	w.Dir.SetActive(w.Employees["EMP-005"], true)
	if got := permVersion(); got != pv {
		t.Fatalf("perm_version after SetActive(EMP-005, true) = %d, want %d", got, pv)
	}
	wantStatus(t, "ravi GET Suresh's claim after Suresh is active again", ravi.Get("/expense/"+t14ID(exp43)), http.StatusOK)
	if !listed() {
		t.Fatalf("ravi /expense/list lacks %s after Suresh is active again", t15Draft(exp43))
	}
}

// Task 5 review: a record that is hidden AND lacks right r is 404, not 403
// (visibility is checked before the right's scope).
func TestCan_HiddenWithoutRightIsNotFound(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	techPO := &auth.RecordRef{
		DocType: authtest.DTOrder, ID: 1, CreatedBy: w.Users[t10Ravi],
		ProjectIDs: []int64{w.Projects["PRJ-TECHPARK"]}, SiteIDs: []int64{w.Sites["TECH-SITE"]},
	}
	priya := w.P(t, t10Priya) // restricted to PRJ-METRO; no approve, cancel or amend on PO
	for _, r := range []auth.Right{auth.RightApprove, auth.RightCancel, auth.RightAmend} {
		t15WantNotFoundErr(t, "priya "+string(r)+" PO on PRJ-TECHPARK", w.Auth.Can(ctx, priya, authtest.DTOrder, r, techPO))
	}
	sureshClaim := &auth.RecordRef{
		DocType: authtest.DTClaim, ID: 2, CreatedBy: w.Users[t14Suresh],
		EmployeeID: w.Employees["EMP-005"], ProjectIDs: []int64{w.Projects["PRJ-METRO"]},
	}
	// Deepak: read own/team (no reports), no export.
	t15WantNotFoundErr(t, "deepak export Suresh's claim",
		w.Auth.Can(ctx, w.P(t, t10Deepak), authtest.DTClaim, auth.RightExport, sureshClaim))
}

// Task 5 review: restrictions are strict; a restricted dimension with no IDs
// on the record hides it.
func TestCan_StrictEmptyDimensionHidden(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	noProject := &auth.RecordRef{
		DocType: authtest.DTOrder, ID: 1, CreatedBy: w.Users[t10Priya], SiteIDs: []int64{w.Sites["METRO-SITE"]},
	}
	noSite := &auth.RecordRef{
		DocType: authtest.DTOrder, ID: 2, CreatedBy: w.Users[t10Priya], ProjectIDs: []int64{w.Projects["PRJ-METRO"]},
	}
	t15WantNotFoundErr(t, "priya (PRJ-METRO) read PO without a Project",
		w.Auth.Can(ctx, w.P(t, t10Priya), authtest.DTOrder, auth.RightRead, noProject))
	t15WantNotFoundErr(t, "suresh (PUNE-WH, METRO-SITE) read PO without a Site",
		w.Auth.Can(ctx, w.P(t, t14Suresh), authtest.DTOrder, auth.RightRead, noSite))
	// Controls: an unrestricted user sees both, and each restricted user sees
	// the record missing only the dimension they aren't restricted on.
	ravi := w.P(t, t10Ravi)
	for _, rec := range []*auth.RecordRef{noProject, noSite} {
		if err := w.Auth.Can(ctx, ravi, authtest.DTOrder, auth.RightRead, rec); err != nil {
			t.Errorf("ravi (unrestricted) read PO %d = %v, want nil", rec.ID, err)
		}
	}
	if err := w.Auth.Can(ctx, w.P(t, t10Priya), authtest.DTOrder, auth.RightRead, noSite); err != nil {
		t.Errorf("priya read PO with a Project and no Site = %v, want nil (no Site restriction)", err)
	}
	if err := w.Auth.Can(ctx, w.P(t, t14Suresh), authtest.DTOrder, auth.RightRead, noProject); err != nil {
		t.Errorf("suresh read PO with a Site and no Project = %v, want nil (no Project restriction)", err)
	}
}

// Task 5 review: create skips the own/team scope check on a visible record.
func TestCan_CreateSkipsOwnScope(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	deepakClaim := &auth.RecordRef{
		DocType: authtest.DTClaim, ID: 1, CreatedBy: w.Users[t10Deepak],
		EmployeeID: w.Employees["EMP-007"], ProjectIDs: []int64{w.Projects["PRJ-METRO"]},
	}
	priya := w.P(t, t10Priya) // Employee: read team, create own, write own
	if err := w.Auth.Can(ctx, priya, authtest.DTClaim, auth.RightRead, deepakClaim); err != nil {
		t.Fatalf("priya read Deepak's claim (team) = %v, want nil", err)
	}
	if err := w.Auth.Can(ctx, priya, authtest.DTClaim, auth.RightCreate, deepakClaim); err != nil {
		t.Errorf("priya create (own) on Deepak's visible claim = %v, want nil: create skips own scope", err)
	}
	// Control: write (own) on the same record is refused with the own-only text.
	err := w.Auth.Can(ctx, priya, authtest.DTClaim, auth.RightWrite, deepakClaim)
	var de *auth.DeniedError
	if !errors.As(err, &de) || !de.OwnOnly || err.Error() != "You can only change Expense Claims you created." {
		t.Errorf("priya write Deepak's claim = %v, want the own-only DeniedError", err)
	}
}
