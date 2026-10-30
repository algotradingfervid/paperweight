package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/ui"
)

const (
	claimPath = "/doc/" + fixture.Claim

	empPriya  = "EMP-004"
	empSuresh = "EMP-005"
	empDeepak = "EMP-007"
	empFormer = "EMP-008"
)

func claimURL(id int64) string { return claimPath + "/" + idText(id) }
func grnURL(id int64) string   { return grnPath + "/" + idText(id) }

// wantOrderExport checks that every row of the order export is on project.
func wantOrderExport(t *testing.T, what string, b *browser, project string) {
	t.Helper()
	rows := csvRows(t, what, b.get(orderPath+"/export.csv"))
	if len(rows) != 1 {
		t.Fatalf("%s = %v, want 1 row", what, rows)
	}
	if rows[0][1] != project {
		t.Errorf("%s: row %v, want project %s", what, rows[0], project)
	}
}

func (c *authCase) restrict(id auth.UserID, dim auth.Dimension, add ...int64) {
	c.t.Helper()
	if err := c.app.Auth().ChangeRestrictions(context.Background(), c.admin, id, dim, add, nil, nil); err != nil {
		c.t.Fatalf("restrict user %d: %v", id, err)
	}
}

func (c *authCase) setRoles(id auth.UserID, roles ...string) {
	c.t.Helper()
	if _, err := c.app.Auth().UpdateUser(context.Background(), c.admin, id, auth.UserPatch{Roles: &roles}); err != nil {
		c.t.Fatalf("roles of user %d: %v", id, err)
	}
}

func (c *authCase) exec(q string, args ...any) {
	c.t.Helper()
	if _, err := c.app.DB().SQL().ExecContext(context.Background(), q, args...); err != nil {
		c.t.Fatalf("%q: %v", q, err)
	}
}

// AUTH-085. The real list shows each row's amount and has no grand total.
func caseOwnListFiltered(t *testing.T, c *authCase) {
	mine := c.claim(row{number: "EX/26-27/00001", state: "approved", by: deepakID, employee: c.employee(empDeepak), amt: 120000})
	c.claim(row{number: "EX/26-27/00002", state: "approved", by: sureshID, employee: c.employee(empSuresh), amt: 50000})
	r := c.as(deepakEmail).get(claimPath)
	wantListed(t, "Deepak's list", r, claimPath, mine)
	wantPage(t, "Deepak's list", r, []string{"1 expense claim", "1,200.00"}, []string{"500.00"})
}

// AUTH-086. A hidden document's print page is the same 404; the real router has
// no PDF route (AUTH-079).
func caseOwnOthersNotFound(t *testing.T, c *authCase) {
	mine := c.claim(row{number: "EX/26-27/00001", state: "approved", by: deepakID, employee: c.employee(empDeepak)})
	other := c.claim(row{number: "EX/26-27/00002", state: "approved", by: sureshID, employee: c.employee(empSuresh)})
	deepak := c.as(deepakEmail)
	wantStatus(t, "Deepak opens his own", deepak.get(claimURL(mine)), http.StatusOK)
	for _, path := range []string{claimURL(other), "/print/" + fixture.Claim + "/" + idText(other)} {
		want404(t, "Deepak GET "+path, deepak.get(path), "EX/26-27/00002")
	}
}

// AUTH-087.
func caseOwnWriteDelete(t *testing.T, c *authCase) {
	metro := c.m.Project["PRJ-METRO"]
	mine := c.order(row{by: priyaID, project: metro})
	ravis := c.order(row{by: raviID, project: metro})
	priya := c.as(priyaEmail)
	want403(t, "Priya deletes Ravi's", priya.post(orderURL(ravis)+"/delete", url.Values{"version": {c.version(ravis)}}),
		"You can only delete Purchase Orders you created.")
	if n := c.count(`SELECT count(*) FROM fixture_order WHERE id = ?`, ravis); n != 1 {
		t.Fatalf("Ravi's draft rows = %d, want 1", n)
	}
	wantStatus(t, "Priya deletes her own", priya.post(orderURL(mine)+"/delete", url.Values{"version": {c.version(mine)}}), http.StatusSeeOther)
	if n := c.count(`SELECT count(*) FROM fixture_order WHERE id = ?`, mine); n != 0 {
		t.Fatalf("Priya's draft rows = %d, want 0", n)
	}
	wantStatus(t, "Priya creates", priya.post(orderPath+"/new", url.Values{
		"supplier_id": {idText(c.m.Party["SHREE"])}, "project_id": {idText(metro)},
		"order_date": {"24-10-2026"}, "amount": {"1.00"}, "priority": {"normal"}, "discount_pct": {"0"},
	}), http.StatusSeeOther)
}

// AUTH-088.
func caseOwnLinkedEmployee(t *testing.T, c *authCase) {
	id := c.claim(row{number: "EX/26-27/00003", state: "approved", by: adminID, employee: c.employee(empDeepak)})
	deepak := c.as(deepakEmail)
	wantListed(t, "Deepak's list", deepak.get(claimPath), claimPath, id)
	wantPage(t, "Deepak opens it", deepak.get(claimURL(id)), []string{"EX/26-27/00003"}, nil)
}

// AUTH-089.
func caseOwnAllWins(t *testing.T, c *authCase) {
	id := c.claim(row{number: "EX/26-27/00002", state: "approved", by: sureshID, employee: c.employee(empSuresh)})
	deepak := c.as(deepakEmail)
	wantListed(t, "Deepak's list, Employee only", deepak.get(claimPath), claimPath)
	c.setRoles(deepakID, "Employee", "Director")
	wantListed(t, "Deepak's list with Director", deepak.get(claimPath), claimPath, id)
	c.setRoles(deepakID, "Employee")
	wantListed(t, "Deepak's list, Director gone", deepak.get(claimPath), claimPath)
}

// AUTH-090.
func caseOwnExportFiltered(t *testing.T, c *authCase) {
	c.setRights("Employee", fixture.Claim, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeTeam, auth.RightApprove: auth.ScopeTeam, auth.RightCreate: auth.ScopeOwn, auth.RightWrite: auth.ScopeOwn,
		auth.RightSubmit: auth.ScopeOwn, auth.RightPrint: auth.ScopeOwn, auth.RightExport: auth.ScopeOwn,
	})
	deepakEmp := c.employee(empDeepak)
	first := c.claim(row{number: "EX/26-27/00001", state: "approved", by: deepakID, employee: deepakEmp})
	c.claim(row{number: "EX/26-27/00002", state: "approved", by: sureshID, employee: c.employee(empSuresh)})
	third := c.claim(row{number: "EX/26-27/00003", state: "approved", by: adminID, employee: deepakEmp})
	deepak := c.as(deepakEmail)
	rows := csvRows(t, "Deepak's export", deepak.get(claimPath+"/export.csv"))
	if len(rows) != 2 {
		t.Fatalf("export = %v, want Deepak's 2 rows", rows)
	}
	for _, row := range rows {
		if row[0] != idText(deepakEmp) {
			t.Errorf("export row %v is another employee's", row)
		}
	}
	wantListed(t, "Deepak's list", deepak.get(claimPath), claimPath, first, third)
}

// AUTH-091.
func caseRestrictionSiteList(t *testing.T, c *authCase) {
	shown := c.grn(row{number: "GR/26-27/00001", state: "approved", site: c.site("PUNE-WH")})
	hidden := c.grn(row{number: "GR/26-27/00002", state: "approved", by: adminID, site: c.site("TECH-SITE")})
	suresh := c.as(sureshEmail)
	r := suresh.get(grnPath)
	wantListed(t, "Suresh's list", r, grnPath, shown)
	wantPage(t, "Suresh's list", r, []string{"1 grn"}, nil)
	want404(t, "Suresh opens the other", suresh.get(grnURL(hidden)), "GR/26-27/00002")
}

// AUTH-092.
func caseRestrictionProjectList(t *testing.T, c *authCase) {
	shown := c.order(row{number: "PO/26-27/00020", state: "approved", project: c.m.Project["PRJ-METRO"]})
	hidden := c.order(row{number: "PO/26-27/00021", state: "approved", project: c.m.Project["PRJ-TECHPARK"]})
	priya := c.as(priyaEmail)
	wantListed(t, "Priya's list", priya.get(orderPath), orderPath, shown)
	wantOrderExport(t, "Priya's export", priya, "PRJ-METRO")
	want404(t, "Priya opens the other", priya.get(orderURL(hidden)), "PO/26-27/00021")
}

// AUTH-093.
func caseRestrictionStrictEmpty(t *testing.T, c *authCase) {
	id := c.order(row{number: "PO/26-27/00022", state: "approved"})
	wantListed(t, "Priya's list", c.as(priyaEmail).get(orderPath), orderPath)
	wantListed(t, "Ravi's list", c.as(raviEmail).get(orderPath), orderPath, id)
}

// AUTH-094.
func caseRestrictionChildLinks(t *testing.T, c *authCase) {
	id := c.grn(row{number: "GR/26-27/00023", state: "approved", site: c.site("PUNE-WH"), lineSites: []int64{c.site("TECH-SITE")}})
	wantStatus(t, "Ravi opens it", c.as(raviEmail).get(grnURL(id)), http.StatusOK)
	want404(t, "Suresh opens it", c.as(sureshEmail).get(grnURL(id)), "GR/26-27/00023")
	wantListed(t, "Suresh's list", c.as(sureshEmail).get(grnPath), grnPath)
}

// AUTH-095.
func caseRestrictionDimensionsAnd(t *testing.T, c *authCase) {
	metro, tech := c.m.Project["PRJ-METRO"], c.m.Project["PRJ-TECHPARK"]
	c.restrict(sureshID, auth.DimProject, metro)
	shown := c.grn(row{number: "GR/26-27/00001", state: "approved", project: metro, site: c.site("PUNE-WH")})
	hidden := c.grn(row{number: "GR/26-27/00002", state: "approved", project: metro, site: c.site("TECH-SITE")})
	c.grn(row{number: "GR/26-27/00003", state: "approved", project: tech, site: c.site("PUNE-WH")})
	suresh := c.as(sureshEmail)
	wantListed(t, "Suresh's list", suresh.get(grnPath), grnPath, shown)
	want404(t, "Suresh opens the hidden one", suresh.get(grnURL(hidden)))
}

// AUTH-096.
func caseRestrictionDoesNotGrant(t *testing.T, c *authCase) {
	c.restrict(deepakID, auth.DimSite, c.site("PUNE-WH"))
	id := c.grn(row{number: "GR/26-27/00001", state: "approved", site: c.site("PUNE-WH")})
	want403(t, "Deepak opens it", c.as(deepakEmail).get(grnURL(id)), deny("read", "GRN"), "GR/26-27/00001")
}

// AUTH-097: fixture.Claim has no Project, so Priya's own scope here is on the
// GRN (read: own), restricted the same way.
func caseRestrictionAndOwnScope(t *testing.T, c *authCase) {
	metro, tech := c.m.Project["PRJ-METRO"], c.m.Project["PRJ-TECHPARK"]
	own := c.grn(row{number: "GR/26-27/00008", state: "approved", by: priyaID, project: metro})
	c.grn(row{number: "GR/26-27/00009", state: "approved", by: priyaID, project: tech})
	c.grn(row{number: "GR/26-27/00010", state: "approved", by: raviID, project: metro})
	wantListed(t, "Priya's list", c.as(priyaEmail).get(grnPath), grnPath, own)
}

// AUTH-098, on the real masters' pickers.
func caseRestrictionPicker(t *testing.T, c *authCase) {
	suresh := c.as(sureshEmail)
	wantPage(t, "Suresh's Site picker", suresh.get("/pick/Site?q="), []string{"PUNE-WH", "METRO-SITE"}, []string{"TECH-SITE"})
	wantPage(t, "Suresh's Project picker", suresh.get("/pick/Project?q="), []string{"PRJ-METRO", "PRJ-TECHPARK"}, nil)
}

// AUTH-099.
func caseRestrictionSaveDisallowed(t *testing.T, c *authCase) {
	r := c.as(sureshEmail).post(grnPath+"/new", url.Values{"site_id": {idText(c.site("TECH-SITE"))}, "receipt_date": {"24-10-2026"}})
	want403(t, "Suresh posts TECH-SITE", r, "You can only use Sites METRO-SITE, PUNE-WH. Choose one of your allowed sites.")
	if n := c.count(`SELECT count(*) FROM apptest_grn`); n != 0 {
		t.Errorf("GRNs after the refused post = %d, want 0", n)
	}
}

// AUTH-100.
func caseRestrictionSingleDefault(t *testing.T, c *authCase) {
	suresh := c.as(sureshEmail)
	siteInput := func() *response {
		t.Helper()
		r := suresh.get(grnPath + "/new")
		wantStatus(t, "Suresh GET new", r, http.StatusOK)
		return r
	}
	if v, _ := siteInput().doc.Find(`input[name="site_id"]`).First().Attr("value"); v != "" {
		t.Errorf("with two Sites the form prefilled %q, want it empty", v)
	}
	if err := c.app.Auth().ChangeRestrictions(context.Background(), c.admin, sureshID, auth.DimSite, nil, []int64{c.site("METRO-SITE")}, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := siteInput().doc.Find(`input[name="site_id"]`).First().Attr("value"); v != idText(c.site("PUNE-WH")) {
		t.Errorf("with one Site the form prefilled %q, want PUNE-WH's id", v)
	}
}

// AUTH-101, on fixture.Receipt's real link to an order line. Suresh's PO rights
// have no Site to restrict, so his view of the PO is cut by Project.
func caseRestrictionLinkedDocument(t *testing.T, c *authCase) {
	c.restrict(sureshID, auth.DimProject, c.m.Project["PRJ-METRO"])
	po := c.order(row{number: "PO/26-27/00030", state: "approved", project: c.m.Project["PRJ-TECHPARK"]})
	c.exec(`INSERT INTO fixture_order_line (parent_id, line_no, item_id, qty, rate) VALUES (?, 1, ?, 5000, 100)`, po, c.m.Item["BOLT-M12"])
	line := c.text(`SELECT CAST(id AS TEXT) FROM fixture_order_line WHERE parent_id = ?`, po)
	receipt := c.insert("fixture_receipt", row{by: sureshID}, "receipt_date", "2026-10-24")

	suresh := c.as(sureshEmail)
	r := suresh.post("/doc/"+fixture.Receipt+"/"+idText(receipt)+"/lines/new", url.Values{
		"version": {c.text(`SELECT CAST(version AS TEXT) FROM fixture_receipt WHERE id = ?`, receipt)}, "order_line_id": {line}, "qty": {"1.000"},
	})
	want403(t, "Suresh links the hidden PO", r, "You can't use Purchase Order PO/26-27/00030. It isn't available to you.")
	if n := c.count(`SELECT count(*) FROM fixture_receipt_line`); n != 0 {
		t.Errorf("receipt lines after the refused post = %d, want 0", n)
	}
	wantListed(t, "Suresh's PO list", suresh.get(orderPath), orderPath)
	wantListed(t, "Ravi's PO list", c.as(raviEmail).get(orderPath), orderPath, po)
}

// AUTH-102.
func caseRestrictionMasterSelf(t *testing.T, c *authCase) {
	wantPage(t, "Suresh's Sites", c.as(sureshEmail).get("/m/site"), []string{"PUNE-WH", "METRO-SITE"}, []string{"TECH-SITE"})
	wantPage(t, "Priya's Projects", c.as(priyaEmail).get("/m/project"), []string{"PRJ-METRO"}, []string{"PRJ-TECHPARK"})
}

func (c *authCase) userVersion(id auth.UserID) string {
	c.t.Helper()
	return c.text(`SELECT CAST(version AS TEXT) FROM users WHERE id = ?`, int64(id))
}

// AUTH-103.
func caseRestrictionAdminOnly(t *testing.T, c *authCase) {
	path := "/users/" + idText(int64(sureshID)) + "/restrictions"
	suresh := c.as(sureshEmail)
	const msg = "Only an administrator can change site and project access."
	before := c.count(`SELECT count(*) FROM user_restrictions WHERE user_id = ?`, int64(sureshID))
	for _, f := range []url.Values{
		{"dimension": {"site"}, "add": {idText(c.site("TECH-SITE"))}},
		{"dimension": {"site"}, "remove": {idText(c.site("PUNE-WH"))}},
	} {
		f.Set("version", c.userVersion(sureshID))
		want403(t, "Suresh changes his Sites", suresh.post(path, f), msg)
	}
	if n := c.count(`SELECT count(*) FROM user_restrictions WHERE user_id = ?`, int64(sureshID)); n != before {
		t.Errorf("Suresh's restrictions = %d, want %d", n, before)
	}
}

// AUTH-104.
func caseRestrictionClearLifts(t *testing.T, c *authCase) {
	id := c.grn(row{number: "GR/26-27/00002", state: "approved", by: adminID, site: c.site("TECH-SITE")})
	suresh := c.as(sureshEmail)
	wantListed(t, "Suresh's list before", suresh.get(grnPath), grnPath)
	r := c.as(adminEmail).post("/users/"+idText(int64(sureshID))+"/restrictions", url.Values{
		"dimension": {"site"}, "version": {c.userVersion(sureshID)},
		"remove": {idText(c.site("PUNE-WH")), idText(c.site("METRO-SITE"))},
	})
	if r.status >= http.StatusBadRequest {
		t.Fatalf("clearing the list = %d (%q)", r.status, r.pageText())
	}
	wantListed(t, "Suresh's next list", suresh.get(grnPath), grnPath, id)
}

// AUTH-105.
func caseRestrictionDisabledKept(t *testing.T, c *authCase) {
	pune := c.site("PUNE-WH")
	id := c.grn(row{number: "GR/26-27/00001", state: "approved", site: pune})
	r := c.as(adminEmail).post("/m/site/"+idText(pune)+"/disable", url.Values{
		"version": {c.text(`SELECT CAST(version AS TEXT) FROM site WHERE id = ?`, pune)},
	})
	if r.status >= http.StatusBadRequest {
		t.Fatalf("disable PUNE-WH = %d (%q)", r.status, r.pageText())
	}
	suresh := c.as(sureshEmail)
	wantListed(t, "Suresh's list", suresh.get(grnPath), grnPath, id)
	wantPage(t, "Suresh's Site picker", suresh.get("/pick/Site?q="), []string{"METRO-SITE"}, []string{"PUNE-WH"})
	wantPage(t, "the admin's access page", c.as(adminEmail).get("/users/"+idText(int64(sureshID))+"/access"), []string{"PUNE-WH (disabled)"}, nil)
}

// AUTH-106: on fixture.Order, whose Project restriction every channel can
// reach (list, count, search, print, export, every GET route on the record).
// The real router has no document picker and no JSON channel.
func caseScopeAllChannels(t *testing.T, c *authCase) {
	const hidden, shown = "PO/26-27/00002", "PO/26-27/00001"
	seen := c.order(row{number: shown, state: "approved", project: c.m.Project["PRJ-METRO"]})
	id := c.order(row{number: hidden, state: "approved", project: c.m.Project["PRJ-TECHPARK"]})
	priya := c.as(priyaEmail)
	r := priya.get(orderPath)
	wantListed(t, "list", r, orderPath, seen)
	wantPage(t, "count", r, []string{"1 purchase order"}, nil)
	// The search box echoes the query, so a hit is a link to the record.
	// fixture.Order declares no search fields; the GRN does, behind Suresh's
	// Site restriction.
	suresh := c.as(sureshEmail)
	grnShown := c.grn(row{number: "GR/26-27/00001", state: "approved", site: c.site("PUNE-WH")})
	grnHidden := c.grn(row{number: "GR/26-27/00002", state: "approved", by: adminID, site: c.site("TECH-SITE")})
	wantPage(t, "search", suresh.get("/search?q=GR/26-27/00002"), nil, []string{`href="` + grnURL(grnHidden) + `"`})
	wantPage(t, "search control", suresh.get("/search?q=GR/26-27/00001"), []string{`href="` + grnURL(grnShown) + `"`}, nil)
	wantOrderExport(t, "export", priya, "PRJ-METRO")

	var paths []string
	err := chi.Walk(c.routes(), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		onRecord := strings.Contains(route, "{id}") && (strings.HasPrefix(route, "/doc/") || strings.HasPrefix(route, "/print/"))
		if method != http.MethodGet || !onRecord {
			return nil
		}
		paths = append(paths, fillRoute(route, func(param string) string {
			switch param {
			case "doctype":
				return fixture.Order
			case "id":
				return idText(id)
			case "step":
				return "details"
			case "no":
				return "1"
			}
			return "reject"
		}))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{orderURL(id), "/print/" + fixture.Order + "/" + idText(id)} {
		if !slices.Contains(paths, want) {
			t.Fatalf("GET routes on a record = %q, want %s", paths, want)
		}
	}
	for _, p := range paths {
		want404(t, "Priya GET "+p, priya.get(p), hidden)
	}
}

// AUTH-107: AUTH's inventory over the real router. TestApp_CheckRoutesClean and
// TestApp_RouteGuards hold the same rule; this is the id's own row.
func caseRoutesDeclarePermission(t *testing.T, c *authCase) {
	if problems := authweb.CheckRoutes(c.routes()); len(problems) != 0 {
		t.Errorf("CheckRoutes found %d problems:\n%s", len(problems), strings.Join(problems, "\n"))
	}
}

// AUTH-108.
func caseScopeIDGuessing(t *testing.T, c *authCase) {
	own := []int64{}
	for i, u := range []struct {
		by  auth.UserID
		emp string
	}{{sureshID, empSuresh}, {deepakID, empDeepak}, {priyaID, empPriya}, {deepakID, empDeepak}, {raviID, "EMP-003"}} {
		id := c.claim(row{state: "approved", number: "EX/26-27/0000" + strconv.Itoa(i+1), by: u.by, employee: c.employee(u.emp)})
		if u.by == deepakID {
			own = append(own, id)
		}
	}
	deepak := c.as(deepakEmail)
	var first string
	var firstID int64
	for id := int64(1); id <= 50; id++ {
		r := deepak.get(claimURL(id))
		if slices.Contains(own, id) {
			wantStatus(t, "Deepak opens his own "+idText(id), r, http.StatusOK)
			continue
		}
		want404(t, "Deepak GET "+claimURL(id), r)
		body := strings.ReplaceAll(r.body, deepak.csrf, "{csrf}")
		body = strings.ReplaceAll(body, claimURL(id), "{path}")
		if firstID == 0 {
			first, firstID = body, id
			continue
		}
		if body != first {
			t.Fatalf("the 404 of %d differs from the 404 of %d:\n%s\n---\n%s", id, firstID, body, first)
		}
	}
}

// AUTH-109, through FILE's real upload and download.
func caseScopeAttachments(t *testing.T, c *authCase) {
	po := c.order(row{number: "PO/26-27/00002", project: c.m.Project["PRJ-TECHPARK"], by: raviID})
	content := filestest.JPEG(1000)
	admin := c.as(adminEmail)
	r := admin.upload("/attachments/"+fixture.Order+"/"+idText(po), "invoice_8812.jpg", content, nil)
	wantStatus(t, "admin uploads", r, http.StatusCreated)
	var up struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(r.body), &up); err != nil {
		t.Fatalf("upload answer %q: %v", r.body, err)
	}
	file := "/files/" + idText(up.ID)
	wantRefused(t, "Priya downloads", c.as(priyaEmail).get(file), http.StatusNotFound, files.MsgNotFound, string(content))
	want403(t, "Deepak downloads", c.as(deepakEmail).get(file), deny("read", "Purchase Order"), string(content))
	got := admin.get(file)
	wantStatus(t, "admin downloads", got, http.StatusOK)
	if got.body != string(content) {
		t.Errorf("admin's download is %d bytes, want the %d uploaded", len(got.body), len(content))
	}
}

// AUTH-110.
func caseScopeFilters(t *testing.T, c *authCase) {
	shown := c.grn(row{number: "GR/26-27/00001", state: "approved", by: adminID, site: c.site("PUNE-WH")})
	hidden := grnURL(c.grn(row{number: "GR/26-27/00002", state: "approved", by: adminID, site: c.site("TECH-SITE")}))
	suresh := c.as(sureshEmail)
	wantListed(t, "?f=site TECH-SITE", suresh.get(grnPath+"?f=site_id:in:"+idText(c.site("TECH-SITE"))), grnPath)
	wantListed(t, "sort by site", suresh.get(grnPath+"?sort=site_id.asc"), grnPath, shown)
	// created_by isn't a declared filter: the query is refused, so it can't
	// widen anything either.
	r := suresh.get(grnPath + "?f=created_by:in:" + idText(int64(adminID)))
	if strings.Contains(r.body, hidden) {
		t.Errorf("?f=created_by shows %s: %q", hidden, r.pageText())
	}
}

// AUTH-111: the core has no report screen, so the totals Priya can reach are
// the export's rows: only her Project's amount is in them.
func caseScopeReportTotals(t *testing.T, c *authCase) {
	c.order(row{number: "PO/26-27/00001", state: "approved", project: c.m.Project["PRJ-METRO"], amt: 47200000})
	c.order(row{number: "PO/26-27/00002", state: "approved", project: c.m.Project["PRJ-TECHPARK"], amt: 123550000})
	wantOrderExport(t, "Priya's export", c.as(priyaEmail), "PRJ-METRO")
	wantPage(t, "Priya's export", c.as(priyaEmail).get(orderPath+"/export.csv"), []string{"472000.00"}, []string{"1235500.00"})
}

// AUTH-112: the real router has no JSON read endpoint, so nothing can differ
// from the screen. The table runs the screen against Can for 7 users × 8 GRNs,
// and a JSON read route added later fails this test until it joins the table.
func caseScopeAPIEqualsUI(t *testing.T, c *authCase) {
	metro, tech := c.m.Project["PRJ-METRO"], c.m.Project["PRJ-TECHPARK"]
	pune, metroSite, techSite := c.site("PUNE-WH"), c.site("METRO-SITE"), c.site("TECH-SITE")
	var grns []auth.RecordRef
	for _, r := range []row{
		{project: metro, site: metroSite, by: priyaID},
		{project: metro, site: pune, by: raviID},
		{project: metro, site: techSite, by: priyaID},
		{project: tech, site: techSite, by: raviID},
		{project: tech, site: pune, by: priyaID},
		{site: metroSite, by: raviID},
		{project: metro, site: pune, by: priyaID, lineSites: []int64{techSite}},
		{project: tech, site: metroSite, by: raviID, lineSites: []int64{metroSite}},
	} {
		r.state = "approved"
		ref := auth.RecordRef{DocType: grnType, ID: c.grn(r), CreatedBy: r.by, SiteIDs: append([]int64{r.site}, r.lineSites...)}
		if r.project != 0 {
			ref.ProjectIDs = []int64{r.project}
		}
		grns = append(grns, ref)
	}
	users := []struct {
		email string
		id    auth.UserID
	}{{adminEmail, adminID}, {meenaEmail, 2}, {raviEmail, raviID}, {priyaEmail, priyaID}, {sureshEmail, sureshID}, {kavyaEmail, 6}, {deepakEmail, deepakID}}
	seen := map[int]bool{}
	ctx := context.Background()
	for _, u := range users {
		b := c.as(u.email)
		p := c.principal(u.id)
		for _, ref := range grns {
			want := http.StatusOK
			err := c.app.Auth().Can(ctx, p, grnType, auth.RightRead, &ref)
			switch {
			case errors.Is(err, auth.ErrNotFound):
				want = http.StatusNotFound
			case errors.Is(err, auth.ErrForbidden):
				want = http.StatusForbidden
			case err != nil:
				t.Fatal(err)
			}
			got := b.get(grnURL(ref.ID)).status
			if got != want {
				t.Errorf("%s GRN %d: page %d, Can says %d", u.email, ref.ID, got, want)
			}
			seen[got] = true
		}
	}
	for _, s := range []int{http.StatusOK, http.StatusForbidden, http.StatusNotFound} {
		if !seen[s] {
			t.Errorf("no (user, GRN) pair answered %d; the table must cover allow, 403 and 404", s)
		}
	}
	err := chi.Walk(c.routes(), func(_, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/api/") {
			t.Errorf("the router has %s: compare it with the screen here", route)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// AUTH-115. TestApp_AllRoutesRequireSignIn walks every route; these are the
// case's own paths.
func caseGuestPublicRoutes(t *testing.T, c *authCase) {
	guest := c.browser()
	for _, path := range []string{"/", orderPath, "/users"} {
		r := guest.get(path)
		want := "/signin?next=" + url.QueryEscape(path)
		if r.status != http.StatusSeeOther || r.location() != want {
			t.Errorf("guest GET %s = %d to %q, want 303 to %q", path, r.status, r.location(), want)
		}
	}
	r := guest.do(http.MethodGet, orderPath, nil, "", map[string]string{"HX-Request": "true"})
	if got := noticeText(t, r.header.Get("HX-Trigger")); r.status != http.StatusUnauthorized || got != msgSignInRequired {
		t.Errorf("guest htmx GET = %d %q, want 401 %q", r.status, got, msgSignInRequired)
	}
	// The real router has no /api/ route, so the case's API path has nothing
	// to reach (AUTH-112).
	wantStatus(t, "guest GET /api/po/1", guest.get("/api/po/1"), http.StatusNotFound)
	wantStatus(t, "guest GET static", guest.get(ui.CSSURL), http.StatusOK)
}

// newClaim is an Expense Claim through the real forms, submitted, by a user
// who holds create and submit on it.
func (c *authCase) newClaim(b *browser, emp string) int64 {
	c.t.Helper()
	before := c.count(`SELECT count(*) FROM fixture_claim`)
	r := b.post(claimPath+"/new", url.Values{"employee_id": {idText(c.employee(emp))}, "claim_date": {"24-10-2026"}})
	wantStatus(c.t, "new claim", r, http.StatusSeeOther)
	if n := c.count(`SELECT count(*) FROM fixture_claim`); n != before+1 {
		c.t.Fatalf("claims after the post = %d, want %d", n, before+1)
	}
	id := int64(c.count(`SELECT max(id) FROM fixture_claim`))
	r = b.post(claimURL(id)+"/lines/new", url.Values{
		"version":      {c.text(`SELECT CAST(version AS TEXT) FROM fixture_claim WHERE id = ?`, id)},
		"expense_type": {"TRAVEL"}, "amount": {"1200.00"},
	})
	if r.status >= http.StatusBadRequest {
		c.t.Fatalf("claim line = %d (%q)", r.status, r.pageText())
	}
	r = b.post(claimURL(id)+"/submit", url.Values{"version": {c.text(`SELECT CAST(version AS TEXT) FROM fixture_claim WHERE id = ?`, id)}})
	if r.status >= http.StatusBadRequest {
		c.t.Fatalf("claim submit = %d (%q)", r.status, r.pageText())
	}
	if got := c.claimState(id); got != "pending_approval" {
		c.t.Fatalf("claim state after submit = %q, want pending_approval", got)
	}
	return id
}

func (c *authCase) claimState(id int64) string {
	c.t.Helper()
	return c.text(`SELECT state FROM fixture_claim WHERE id = ?`, id)
}

// managerRule sends every Expense Claim to the reporter's manager.
func (c *authCase) managerRule() {
	c.t.Helper()
	r := c.as(adminEmail).post("/admin/approval-rules", url.Values{
		"doctype": {fixture.Claim}, "from_amount": {"0.00"}, "approver_type": {"reporting_manager"}, "fallback_role": {"Director"},
	})
	if r.status >= http.StatusBadRequest {
		c.t.Fatalf("POST /admin/approval-rules = %d (%q)", r.status, r.pageText())
	}
}

// AUTH-116 end to end: doc.Engine, APR's CheckApprover and MST's Directory.
func caseTeamDirectReport(t *testing.T, c *authCase) {
	c.managerRule()
	deepak, suresh, priya := c.as(deepakEmail), c.as(sureshEmail), c.as(priyaEmail)
	mine := c.newClaim(deepak, empDeepak)
	other := c.newClaim(suresh, empSuresh)

	wantListed(t, "Priya's list", priya.get(claimPath), claimPath, mine)
	wantStatus(t, "Priya opens Deepak's claim", priya.get(claimURL(mine)), http.StatusOK)
	want404(t, "Priya opens Suresh's claim", priya.get(claimURL(other)))

	got, err := c.app.Auth().UsersWithRight(context.Background(), fixture.Claim, auth.RightApprove, &auth.RecordRef{
		DocType: fixture.Claim, ID: mine, CreatedBy: deepakID, EmployeeID: c.employee(empDeepak),
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []auth.UserID{adminID, 2, priyaID, deepakID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("UsersWithRight(approve) = %v, want Anil, Meena, Priya, Deepak %v", got, want)
	}

	// AUTH lists Deepak; APR's self-approval rule keeps him out.
	form := func() url.Values {
		return url.Values{"version": {c.text(`SELECT CAST(version AS TEXT) FROM fixture_claim WHERE id = ?`, mine)}}
	}
	want403(t, "Deepak approves his own claim", deepak.post(claimURL(mine)+"/approve", form()), "Only Priya Shah or a user with the Director role can approve")
	c.setRoles(deepakID, "Employee", "Director")
	want403(t, "Deepak, a Director, approves his own claim", deepak.post(claimURL(mine)+"/approve", form()), "You can't approve your own Expense Claim.")
	if got := c.claimState(mine); got != "pending_approval" {
		t.Fatalf("state after Deepak's tries = %q, want pending_approval", got)
	}
	wantStatus(t, "Priya approves", priya.post(claimURL(mine)+"/approve", form()), http.StatusSeeOther)
	if got := c.claimState(mine); got != "approved" {
		t.Errorf("state after Priya's approve = %q, want approved", got)
	}
}

// AUTH-117.
func caseTeamIndirectReport(t *testing.T, c *authCase) {
	c.managerRule()
	id := c.newClaim(c.as(deepakEmail), empDeepak)
	form := url.Values{"version": {c.text(`SELECT CAST(version AS TEXT) FROM fixture_claim WHERE id = ?`, id)}}
	notFound := func(who string, b *browser) {
		t.Helper()
		wantListed(t, who+"'s list", b.get(claimPath), claimPath)
		want404(t, who+" opens the claim", b.get(claimURL(id)))
		want404(t, who+" approves the claim", b.post(claimURL(id)+"/approve", form))
		if got := c.claimState(id); got != "pending_approval" {
			t.Fatalf("state after %s's try = %q, want pending_approval", who, got)
		}
	}
	notFound("Ravi", c.as(raviEmail))
	meena := c.as(meenaEmail)
	c.setRoles(2, "Employee")
	notFound("Meena (Employee only)", meena)
	c.setRoles(2, "Director")
	wantStatus(t, "Meena (Director) opens the claim", meena.get(claimURL(id)), http.StatusOK)
}

// AUTH-118. Team membership is read on every check: no sign-out, and no
// perm_version bump.
func caseTeamLeftEmployee(t *testing.T, c *authCase) {
	c.exec(`UPDATE employee SET manager_id = ? WHERE id = ?`, c.employee(empPriya), c.employee(empFormer))
	formers := c.claim(row{number: "EX/26-27/00042", state: "approved", by: adminID, employee: c.employee(empFormer)})
	sureshs := c.claim(row{number: "EX/26-27/00043", state: "approved", by: sureshID, employee: c.employee(empSuresh)})

	priya := c.as(priyaEmail)
	wantListed(t, "Priya's list", priya.get(claimPath), claimPath)
	want404(t, "Priya opens the leaver's claim", priya.get(claimURL(formers)))

	ravi := c.as(raviEmail)
	wantStatus(t, "Ravi opens Suresh's claim", ravi.get(claimURL(sureshs)), http.StatusOK)
	wantListed(t, "Ravi's list", ravi.get(claimPath), claimPath, sureshs)

	permVersion := c.count(`SELECT perm_version FROM auth_meta WHERE id = 1`)
	setLeft := func(left bool) {
		t.Helper()
		status := "Active"
		if left {
			status = "Left"
		}
		c.exec(`UPDATE employee SET status = ?, disabled = ? WHERE id = ?`, status, left, c.employee(empSuresh))
		if got := c.count(`SELECT perm_version FROM auth_meta WHERE id = 1`); got != permVersion {
			t.Fatalf("perm_version = %d, want %d", got, permVersion)
		}
	}
	setLeft(true)
	want404(t, "Ravi opens the claim after Suresh left", ravi.get(claimURL(sureshs)))
	wantListed(t, "Ravi's list after Suresh left", ravi.get(claimPath), claimPath)
	setLeft(false)
	wantStatus(t, "Ravi opens the claim after Suresh is back", ravi.get(claimURL(sureshs)), http.StatusOK)
	wantListed(t, "Ravi's list after Suresh is back", ravi.get(claimPath), claimPath, sureshs)
}
