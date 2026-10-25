package authweb_test

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/web"
)

// t18SiteReader gives each email read on the Site and Project masters
// through the ad hoc role "Site Reader" (plan "Fixture mapping").
func t18SiteReader(t *testing.T, w *authtest.World, emails ...string) {
	t.Helper()
	w.Grant(t, "Site Reader", authtest.DTSite, auth.RightRead, auth.ScopeAll)
	w.Grant(t, "Site Reader", authtest.DTProject, auth.RightRead, auth.ScopeAll)
	for _, e := range emails {
		w.AddRole(t, e, "Site Reader")
	}
}

var (
	t18InputRe    = regexp.MustCompile(`<input\b[^>]*\bname="site_id"[^>]*>`)
	t18ValueRe    = regexp.MustCompile(`\bvalue="([^"]*)"`)
	t18RequiredRe = regexp.MustCompile(`\brequired\b`)
)

// t18SiteInput returns the site_id input tag of a /grn/new page and its
// value ("" when the tag has no value attribute).
func t18SiteInput(t *testing.T, what, body string) (tag, value string) {
	t.Helper()
	tags := t18InputRe.FindAllString(body, -1)
	if len(tags) != 1 {
		t.Fatalf("%s: want exactly one <input name=\"site_id\">, found %d in %q", what, len(tags), body)
	}
	if m := t18ValueRe.FindStringSubmatch(tags[0]); m != nil {
		value = m[1]
	}
	return tags[0], value
}

// AUTH-097
func TestRestriction_AndOwnScope(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	priyaID, emp := w.Users[t10Priya], w.Employees["EMP-004"]
	t15Claim(t, h, webtest.Doc{Number: "EX/26-27/00008", CreatedBy: priyaID, EmployeeID: emp, ProjectID: w.Projects["PRJ-METRO"]})
	exp9 := t15Claim(t, h, webtest.Doc{Number: "EX/26-27/00009", CreatedBy: priyaID, EmployeeID: emp, ProjectID: w.Projects["PRJ-TECHPARK"]})

	priya := signedIn(t, h, t10Priya)
	rows := t15GetRows(t, priya, "priya", "/expense/list")
	if slices.Contains(rows, "EX/26-27/00009") {
		t.Fatalf("priya (restricted to PRJ-METRO) /expense/list rows = %q, must not contain her own EX/26-27/00009 on PRJ-TECHPARK", rows)
	}
	// Control: her own claim on PRJ-METRO is listed, so own scope works.
	t15WantRows(t, "priya /expense/list", rows, "EX/26-27/00008")
	t14Want404(t, "priya GET EX/26-27/00009", priya.Get("/expense/"+t14ID(exp9)))
}

// AUTH-100
func TestRestriction_SingleValueDefault(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	w.Restrict(t, t14Suresh, nil, []string{"PUNE-WH"})
	h := t14Probe(t, w)
	puneWH := w.Sites["PUNE-WH"]

	id, ok, err := w.Auth.DefaultValue(ctx, w.P(t, t14Suresh), auth.DimSite)
	if err != nil || !ok || id != puneWH {
		t.Fatalf("DefaultValue(suresh, site) with one site = (%d, %v, %v), want (%d PUNE-WH, true, nil)", id, ok, err, puneWH)
	}
	id, ok, err = w.Auth.DefaultValue(ctx, w.P(t, t14Suresh), auth.DimProject)
	if err != nil || ok || id != 0 {
		t.Fatalf("DefaultValue(suresh, project) with no project restriction = (%d, %v, %v), want (0, false, nil)", id, ok, err)
	}

	suresh := signedIn(t, h, t14Suresh)
	r := suresh.Get("/grn/new")
	wantStatus(t, "suresh GET /grn/new (one site)", r, http.StatusOK)
	tag, value := t18SiteInput(t, "suresh GET /grn/new (one site)", r.Body)
	if value != t14ID(puneWH) {
		t.Fatalf("suresh /grn/new (one site): site input %q has value %q, want PUNE-WH's id %d", tag, value, puneWH)
	}
	if !strings.Contains(r.Body, "PUNE-WH") {
		t.Fatalf("suresh /grn/new (one site): body %q does not show PUNE-WH", r.Body)
	}

	w.Restrict(t, t14Suresh, nil, []string{"PUNE-WH", "METRO-SITE"})
	id, ok, err = w.Auth.DefaultValue(ctx, w.P(t, t14Suresh), auth.DimSite)
	if err != nil || ok || id != 0 {
		t.Fatalf("DefaultValue(suresh, site) with two sites = (%d, %v, %v), want (0, false, nil)", id, ok, err)
	}
	r = suresh.Get("/grn/new")
	wantStatus(t, "suresh GET /grn/new (two sites)", r, http.StatusOK)
	tag, value = t18SiteInput(t, "suresh GET /grn/new (two sites)", r.Body)
	if value != "" {
		t.Fatalf("suresh /grn/new (two sites): site input %q has value %q, want it empty", tag, value)
	}
	if !t18RequiredRe.MatchString(tag) {
		t.Fatalf("suresh /grn/new (two sites): site input %q is not required", tag)
	}
}

// AUTH-102
func TestRestriction_MasterSelf(t *testing.T) {
	w := authtest.NewWorld(t)
	t18SiteReader(t, w, t14Suresh, t10Priya)
	h := t14Probe(t, w)

	suresh := signedIn(t, h, t14Suresh)
	t15WantRows(t, "suresh /site/list", t15GetRows(t, suresh, "suresh", "/site/list"), "PUNE-WH", "METRO-SITE")
	priya := signedIn(t, h, t10Priya)
	t15WantRows(t, "priya /project/list", t15GetRows(t, priya, "priya", "/project/list"), "PRJ-METRO")
	// Control: unrestricted, the masters list every value.
	admin := signedIn(t, h, t14Admin)
	t15WantRows(t, "admin /site/list", t15GetRows(t, admin, "admin", "/site/list"), "PUNE-WH", "METRO-SITE", "TECH-SITE")
	t15WantRows(t, "admin /project/list", t15GetRows(t, admin, "admin", "/project/list"), "PRJ-METRO", "PRJ-TECHPARK")
}

// AUTH-104
func TestRestriction_ClearLifts(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	t15GRN(t, h, "GR/26-27/00001", "PUNE-WH", w.Users[t14Suresh])
	t15GRN(t, h, "GR/26-27/00002", "TECH-SITE", w.Users[t14Admin])

	suresh := signedIn(t, h, t14Suresh)
	t15WantRows(t, "suresh /grn/list before", t15GetRows(t, suresh, "suresh", "/grn/list"), "GR/26-27/00001")

	admin := signedIn(t, h, t14Admin)
	path := "/users/" + t14ID(int64(w.Users[t14Suresh])) + "/restrictions"
	for _, code := range []string{"PUNE-WH", "METRO-SITE"} {
		wantStatus(t, "admin removes "+code+" from suresh",
			admin.Post(path, url.Values{"dimension": {"site"}, "remove": {t14ID(w.Sites[code])}}), http.StatusSeeOther)
	}
	if n := t14Int(t, w, `SELECT count(*) FROM user_restrictions WHERE user_id = ? AND dimension = 'site'`, int64(w.Users[t14Suresh])); n != 0 {
		t.Fatalf("suresh site restriction rows after clearing = %d, want 0", n)
	}

	t15WantRows(t, "suresh /grn/list after", t15GetRows(t, suresh, "suresh", "/grn/list"), "GR/26-27/00001", "GR/26-27/00002")
}

// AUTH-105
func TestRestriction_DisabledMasterKept(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	t18SiteReader(t, w, t14Suresh)
	h := t14Probe(t, w)
	grn1 := t15GRN(t, h, "GR/26-27/00001", "PUNE-WH", w.Users[t14Suresh])
	sureshID := w.Users[t14Suresh]

	labels := func(what string) map[auth.Dimension][]string {
		t.Helper()
		got, err := w.Auth.RestrictionLabels(ctx, sureshID)
		if err != nil {
			t.Fatalf("RestrictionLabels(suresh) %s: %v", what, err)
		}
		for d := range got {
			slices.Sort(got[d])
		}
		return got
	}
	// Control: nothing is marked while PUNE-WH is enabled.
	before := labels("before")
	if want := []string{"METRO-SITE", "PUNE-WH"}; !reflect.DeepEqual(before[auth.DimSite], want) {
		t.Fatalf("RestrictionLabels(suresh)[site] before disabling = %q, want %q", before[auth.DimSite], want)
	}

	w.SetMasterEnabled(t, authtest.DTSite, "PUNE-WH", false)

	suresh := signedIn(t, h, t14Suresh)
	if rows := t15GetRows(t, suresh, "suresh", "/grn/list"); !slices.Contains(rows, "GR/26-27/00001") {
		t.Fatalf("suresh /grn/list rows = %q, want GR/26-27/00001 (PUNE-WH disabled, still allowed)", rows)
	}
	wantStatus(t, "suresh GET GR/26-27/00001", suresh.Get("/grn/"+t14ID(grn1)), http.StatusOK)
	t15WantRows(t, "suresh /site/picker", t15GetRows(t, suresh, "suresh", "/site/picker?q="), "METRO-SITE")

	after := labels("after")
	if want := []string{"METRO-SITE", "PUNE-WH (disabled)"}; !reflect.DeepEqual(after[auth.DimSite], want) {
		t.Fatalf("RestrictionLabels(suresh)[site] = %q, want %q", after[auth.DimSite], want)
	}
	if got := after[auth.DimProject]; len(got) != 0 {
		t.Fatalf("RestrictionLabels(suresh)[project] = %q, want none (no project restriction)", got)
	}
}

// AUTH-111
func TestScope_ReportTotals(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	t14PO(t, h, webtest.Doc{Number: "PO/26-27/00040", State: string(doctype.StateApproved), Amount: 47200000})
	t14PO(t, h, webtest.Doc{
		Number: "PO/26-27/00041", State: string(doctype.StateApproved), Amount: 123550000,
		ProjectID: w.Projects["PRJ-TECHPARK"], SiteID: w.Sites["TECH-SITE"],
	})

	priya := signedIn(t, h, t10Priya)
	r := priya.Get("/reports/po-value")
	wantStatus(t, "priya GET /reports/po-value", r, http.StatusOK)
	t15WantLine(t, "priya /reports/po-value", r.Body, "Total: 47200000")
	// Control: an unrestricted user's total covers both POs.
	ravi := signedIn(t, h, t10Ravi)
	r = ravi.Get("/reports/po-value")
	wantStatus(t, "ravi GET /reports/po-value", r, http.StatusOK)
	t15WantLine(t, "ravi /reports/po-value", r.Body, "Total: 170750000")
}

// t18Leaks are what a denial must never show (AUTH-114).
var (
	t18LeakWords = []string{"SELECT", "goroutine", ".go:"}
	t18EmailRe   = regexp.MustCompile(`[^\s@]+@[^\s@]+\.[A-Za-z]+`)
)

func t18WantNoLeak(t *testing.T, what, text string) {
	t.Helper()
	for _, l := range t18LeakWords {
		if strings.Contains(text, l) {
			t.Errorf("%s: %q contains %q", what, text, l)
		}
	}
	if m := t18EmailRe.FindString(text); m != "" {
		t.Errorf("%s: %q contains the e-mail address %q", what, text, m)
	}
}

// t18Write sends err through Srv.WriteError on a full-page GET.
func t18Write(h *webtest.Harness, err error) (int, string) {
	rec := httptest.NewRecorder()
	h.Srv.WriteError(rec, httptest.NewRequest(http.MethodGet, "/po/list", nil), err)
	return rec.Code, html.UnescapeString(rec.Body.String())
}

// AUTH-114
func TestDenial_Messages(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	// Employee reads every claim but approves only team ones, so a visible
	// claim outside the team gives the TeamOnly denial.
	w.SetRights(t, "Employee", authtest.DTClaim, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightApprove: auth.ScopeTeam,
		auth.RightCreate: auth.ScopeOwn, auth.RightWrite: auth.ScopeOwn,
		auth.RightSubmit: auth.ScopeOwn, auth.RightPrint: auth.ScopeOwn,
	})
	h := t14Probe(t, w)
	raviPO := t14PO(t, h, webtest.Doc{CreatedBy: w.Users[t10Ravi]})
	claim := t15Claim(t, h, webtest.Doc{CreatedBy: w.Users[t14Suresh], EmployeeID: w.Employees["EMP-005"]})
	po30 := t14PO(t, h, webtest.Doc{
		Number: "PO/26-27/00030", State: string(doctype.StateApproved),
		ProjectID: w.Projects["PRJ-TECHPARK"], SiteID: w.Sites["TECH-SITE"],
	})
	grn2 := t15GRN(t, h, "GR/26-27/00002", "TECH-SITE", w.Users[t14Admin])
	deepak, priya, suresh := w.P(t, t10Deepak), w.P(t, t10Priya), w.P(t, t14Suresh)
	ref := func(dt string, id int64) *auth.RecordRef {
		r := h.Ref(t, dt, id)
		return &r
	}

	type denial struct {
		name   string
		err    error
		status int
		want   string
	}
	var cases []denial
	for _, dt := range []string{authtest.DTOrder, authtest.DTReceipt, authtest.DTItem} {
		d, _ := doctype.Lookup(dt)
		for _, r := range auth.AllRights {
			verb := string(r)
			if r == auth.RightWrite {
				verb = "change"
			}
			cases = append(cases, denial{
				name: fmt.Sprintf("deepak %s %s", r, d.Label), err: w.Auth.Can(ctx, deepak, dt, r, nil), status: http.StatusForbidden,
				want: "You don't have permission to " + verb + " " + d.Label + ". Ask your administrator for a role that allows it.",
			})
		}
	}
	cases = append(cases,
		denial{"OwnOnly", w.Auth.Can(ctx, priya, authtest.DTOrder, auth.RightDelete, ref(authtest.DTOrder, raviPO)),
			http.StatusForbidden, "You can only delete Purchase Orders you created."},
		denial{"TeamOnly", w.Auth.Can(ctx, priya, authtest.DTClaim, auth.RightApprove, ref(authtest.DTClaim, claim)),
			http.StatusForbidden, "You can only approve your own Expense Claims and those of people who report to you."},
		denial{"ValueError", w.Auth.CheckValues(ctx, suresh, authtest.DTReceipt, map[string][]int64{"site_id": {w.Sites["TECH-SITE"]}}),
			http.StatusForbidden, "You can only use Sites METRO-SITE, PUNE-WH. Choose one of your allowed sites."},
		denial{"LinkError", w.Auth.CheckLink(ctx, suresh, authtest.DTOrder, po30),
			http.StatusForbidden, "You can't use Purchase Order PO/26-27/00030. It isn't available to you."},
		denial{"ErrNotFound", w.Auth.Can(ctx, suresh, authtest.DTReceipt, auth.RightRead, ref(authtest.DTReceipt, grn2)),
			http.StatusNotFound, auth.MsgNotFound},
	)

	for _, tc := range cases {
		if tc.err == nil {
			t.Errorf("%s: err = nil, want %q", tc.name, tc.want)
			continue
		}
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("%s: message = %q, want %q", tc.name, got, tc.want)
		}
		t18WantNoLeak(t, tc.name, tc.err.Error())
		var he doctype.HTTPError
		if !errors.As(tc.err, &he) {
			t.Errorf("%s: %T does not implement doctype.HTTPError (CD-44)", tc.name, tc.err)
		} else if he.Status() != tc.status {
			t.Errorf("%s: Status() = %d, want %d", tc.name, he.Status(), tc.status)
		}
		status, body := t18Write(h, tc.err)
		if status != tc.status || !strings.Contains(body, tc.want) {
			t.Errorf("%s: WriteError = %d %q, want %d with %q", tc.name, status, body, tc.status, tc.want)
		}
		t18WantNoLeak(t, tc.name+" WriteError body", body)
	}

	// A non-auth error shows WP0's 500 page (web.ErrorFrom), never its text.
	internal := errors.New(`query "SELECT email FROM users WHERE email = 'priya@vardhan.test'": goroutine 7 [running]: service.go:42`)
	status, body := t18Write(h, internal)
	rec := httptest.NewRecorder()
	web.ErrorFrom(rec, httptest.NewRequest(http.MethodGet, "/po/list", nil), internal)
	wantBody := html.UnescapeString(rec.Body.String())
	if status != http.StatusInternalServerError || rec.Code != http.StatusInternalServerError {
		t.Errorf("non-auth error: WriteError status = %d, web.ErrorFrom status = %d, want 500", status, rec.Code)
	}
	if body != wantBody {
		t.Errorf("non-auth error: WriteError body = %q, want web.ErrorFrom's 500 page %q", body, wantBody)
	}
	if !strings.Contains(body, t10InternalMsg) {
		t.Errorf("non-auth error: body = %q, want %q", body, t10InternalMsg)
	}
	t18WantNoLeak(t, "non-auth error body", body)
}
