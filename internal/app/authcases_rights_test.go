package app_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

const (
	msgOnlyManager = "Only a user with the Purchase Manager role can approve"
	stepDetails    = "/edit/details"
)

func (c *authCase) setRights(role, dt string, rights map[auth.Right]auth.Scope) {
	c.t.Helper()
	if err := c.app.Auth().SetRights(context.Background(), c.admin, role, dt, rights); err != nil {
		c.t.Fatalf("SetRights(%s, %s): %v", role, dt, err)
	}
}

func (c *authCase) orders() int {
	c.t.Helper()
	return c.count(`SELECT count(*) FROM fixture_order`)
}

// AUTH-064: every page of a DocType without read says so, and the menu doesn't
// offer it.
func caseNoRoleNoAccess(t *testing.T, c *authCase) {
	po := c.order(row{number: "PO/26-27/00001", state: "approved", project: c.m.Project["PRJ-METRO"]})
	c.grn(row{number: "GR/26-27/00001", state: "approved", site: c.site("PUNE-WH")})
	deepak := c.as(deepakEmail)
	for _, path := range []string{orderPath, orderURL(po), orderPath + "/export.csv"} {
		want403(t, "Deepak GET "+path, deepak.get(path), deny("read", "Purchase Order"), "PO/26-27/00001")
	}
	want403(t, "Deepak GET "+grnPath, deepak.get(grnPath), deny("read", "GRN"), "GR/26-27/00001")

	home := deepak.get("/approvals")
	wantStatus(t, "Deepak GET "+"/approvals", home, http.StatusOK)
	for _, href := range []string{orderPath, grnPath} {
		if n := home.doc.Find(`nav a[href="` + href + `"]`).Length(); n != 0 {
			t.Errorf("the menu offers Deepak %s", href)
		}
	}
}

// AUTH-070, the doc.Engine half: AUTH lets the Administrator through and DOC
// still refuses, in DOC's words. A line post on the real page is sent back to
// the document without the words, so they are asserted on the engine.
func caseAdminFollowsLifecycle(t *testing.T, c *authCase) {
	po := c.order(row{number: "PO/26-27/00003", state: "approved", project: c.m.Project["PRJ-METRO"]})
	ver := c.version(po)
	version, err := strconv.Atoi(ver)
	if err != nil {
		t.Fatal(err)
	}
	const locked = "PO/26-27/00003 is approved and can't be edited. Cancel and amend it to make changes."
	const noDelete = "Approved documents can't be deleted. Cancel it instead."
	admin := c.principal(adminID)
	line := doc.ActionInput{Version: version, Only: []string{"lines"}, Lines: []map[string]string{{
		"item_id": idText(c.m.Item["MS-ANGLE-50"]), "qty": "1.000", "rate": "1.0000", "uom": "kg",
	}}}
	ctx := context.Background()
	_, err = c.app.Engine().Do(ctx, admin, fixture.Order, po, doctype.ActionSave, line)
	if err == nil || err.Error() != locked {
		t.Errorf("engine save of a line = %v, want %q", err, locked)
	}
	_, err = c.app.Engine().Do(ctx, admin, fixture.Order, po, doctype.ActionDelete, doc.ActionInput{Version: version})
	if err == nil || err.Error() != noDelete {
		t.Errorf("engine delete = %v, want %q", err, noDelete)
	}

	b := c.as(adminEmail)
	r := b.post(orderURL(po)+"/lines/new", withVersion(ver, url.Values{
		"item_id": {idText(c.m.Item["MS-ANGLE-50"])}, "qty": {"1.000"}, "rate": {"1.0000"}, "uom": {"kg"},
	}))
	if r.status != http.StatusSeeOther || r.location() != orderURL(po) {
		t.Errorf("admin adds a line = %d to %q, want 303 to the document", r.status, r.location())
	}
	wantRefused(t, "admin edits the header", b.post(orderURL(po)+stepDetails, c.details(po, "after approval")), http.StatusConflict, locked)
	wantRefused(t, "admin deletes it", b.post(orderURL(po)+"/delete", url.Values{"version": {ver}}), http.StatusConflict, noDelete)
	if got := c.state(po); got != "approved" {
		t.Errorf("state = %q, want approved", got)
	}
	if got := c.version(po); got != ver {
		t.Errorf("version = %s, want %s: a refused edit must change nothing", got, ver)
	}
	if n := c.count(`SELECT count(*) FROM fixture_order_line WHERE parent_id = ?`, po); n != 0 {
		t.Errorf("lines on the approved order = %d, want 0", n)
	}
}

// AUTH-071.
func caseRead(t *testing.T, c *authCase) {
	po := c.order(row{number: "PO/26-27/00001", state: "approved", project: c.m.Project["PRJ-METRO"]})
	ravi := c.as(raviEmail)
	wantListed(t, "Ravi's list", ravi.get(orderPath), orderPath, po)
	wantPage(t, "Ravi opens it", ravi.get(orderURL(po)), []string{"PO/26-27/00001"}, nil)
	c.setRights("Purchase Manager", fixture.Order, map[auth.Right]auth.Scope{})
	for _, path := range []string{orderPath, orderURL(po)} {
		want403(t, "Ravi GET "+path+" with read removed", ravi.get(path), deny("read", "Purchase Order"), "PO/26-27/00001")
	}
}

// AUTH-072.
func caseCreate(t *testing.T, c *authCase) {
	form := url.Values{
		"supplier_id": {idText(c.m.Party["SHREE"])}, "project_id": {idText(c.m.Project["PRJ-METRO"])},
		"order_date": {"24-10-2026"}, "amount": {"1000.00"}, "priority": {"normal"}, "discount_pct": {"0"},
	}
	msg := deny("create", "Purchase Order")
	ravi := c.as(raviEmail)
	want403(t, "Ravi GET new", ravi.get(orderPath+"/new"), msg)
	want403(t, "Ravi POST new", ravi.post(orderPath+"/new", form), msg)
	if n := c.orders(); n != 0 {
		t.Fatalf("orders after Ravi's post = %d, want 0", n)
	}
	wantPage(t, "Ravi's list", ravi.get(orderPath), nil, []string{orderPath + "/new"})

	priya := c.as(priyaEmail)
	wantPage(t, "Priya's list", priya.get(orderPath), []string{orderPath + "/new"}, nil)
	wantStatus(t, "Priya GET new", priya.get(orderPath+"/new"), http.StatusOK)
	wantStatus(t, "Priya POST new", priya.post(orderPath+"/new", form), http.StatusSeeOther)
	if n := c.count(`SELECT count(*) FROM fixture_order WHERE created_by = ?`, int64(priyaID)); n != 1 {
		t.Fatalf("orders created by Priya = %d, want 1", n)
	}
}

// AUTH-073.
func caseWrite(t *testing.T, c *authCase) {
	metro := c.m.Project["PRJ-METRO"]
	draft := c.order(row{project: metro})
	rejected := c.order(row{state: "rejected", project: metro})
	msg := deny("change", "Purchase Order")
	ravi := c.as(raviEmail)
	ver := c.version(draft)
	want403(t, "Ravi saves the draft", ravi.post(orderURL(draft)+stepDetails, c.details(draft, "changed")), msg)
	if got := c.text(`SELECT remarks FROM fixture_order WHERE id = ?`, draft); got != "" {
		t.Errorf("remarks after Ravi's save = %q, want none", got)
	}
	if got := c.version(draft); got != ver {
		t.Errorf("version after Ravi's save = %s, want %s", got, ver)
	}
	want403(t, "Ravi revises", ravi.post(orderURL(rejected)+"/revise", url.Values{"version": {c.version(rejected)}}), msg)
	if got := c.state(rejected); got != "rejected" {
		t.Errorf("state after Ravi's revise = %q, want rejected", got)
	}
}

// AUTH-074: submit, withdraw and close all need submit, which write doesn't
// bring.
func caseSubmit(t *testing.T, c *authCase) {
	c.setRights(roleClerk, fixture.Order, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll, auth.RightWrite: auth.ScopeAll,
	})
	metro := c.m.Project["PRJ-METRO"]
	draft := c.order(row{project: metro})
	approved := c.order(row{number: "PO/26-27/00009", state: "approved", project: metro})
	priya := c.as(priyaEmail)
	pending := c.pendingOrder(c.as(adminEmail), priya, c.m)

	clerk := c.as(clerkEmail)
	wantStatus(t, "clerk saves the draft", clerk.post(orderURL(draft)+stepDetails, c.details(draft, "by the clerk")), http.StatusSeeOther)
	if got := c.text(`SELECT remarks FROM fixture_order WHERE id = ?`, draft); got != "by the clerk" {
		t.Errorf("remarks after the clerk's save = %q", got)
	}
	msg := deny("submit", "Purchase Order")
	for _, tc := range []struct {
		action string
		id     int64
		state  string
		extra  url.Values
	}{
		{"submit", draft, "draft", url.Values{}},
		{"withdraw", pending, "pending_approval", url.Values{}},
		{"close", approved, "approved", url.Values{"reason": {"Done"}}},
	} {
		want403(t, "clerk "+tc.action, clerk.post(orderURL(tc.id)+"/"+tc.action, withVersion(c.version(tc.id), tc.extra)), msg)
		if got := c.state(tc.id); got != tc.state {
			t.Errorf("state after the clerk's %s = %q, want %q", tc.action, got, tc.state)
		}
	}
	wantStatus(t, "Priya withdraws", priya.post(orderURL(pending)+"/withdraw", url.Values{"version": {c.version(pending)}}), http.StatusSeeOther)
	if got := c.state(pending); got != "draft" {
		t.Errorf("state after Priya's withdraw = %q, want draft", got)
	}
}

// AUTH-075: through the app DOC doesn't ask Can(approve); APR's CheckApprover
// answers (CD-52), so Priya gets 403 with APR's message, not AUTH's.
func caseApprove(t *testing.T, c *authCase) {
	priya := c.as(priyaEmail)
	po := c.pendingOrder(c.as(adminEmail), priya, c.m)
	want403(t, "Priya approves", priya.post(orderURL(po)+"/approve", url.Values{"version": {c.version(po)}}), msgOnlyManager, deny("approve", "Purchase Order"))
	want403(t, "Priya rejects", priya.post(orderURL(po)+"/reject", url.Values{"version": {c.version(po)}, "reason": {"No"}}), msgOnlyManager, deny("approve", "Purchase Order"))
	if got := c.state(po); got != "pending_approval" {
		t.Fatalf("state after Priya's tries = %q, want pending_approval", got)
	}
	wantStatus(t, "Ravi approves", c.as(raviEmail).post(orderURL(po)+"/approve", url.Values{"version": {c.version(po)}}), http.StatusSeeOther)
	if got := c.state(po); got != "approved" {
		t.Errorf("state after Ravi's approve = %q, want approved", got)
	}
}

// AUTH-076.
func caseCancel(t *testing.T, c *authCase) {
	po := c.order(row{number: "PO/26-27/00007", state: "approved", project: c.m.Project["PRJ-METRO"]})
	form := func() url.Values { return url.Values{"version": {c.version(po)}, "reason": {"Wrong supplier"}} }
	want403(t, "Priya cancels", c.as(priyaEmail).post(orderURL(po)+"/cancel", form()), deny("cancel", "Purchase Order"))
	if got := c.state(po); got != "approved" {
		t.Fatalf("state after Priya's cancel = %q, want approved", got)
	}
	wantStatus(t, "Ravi cancels", c.as(raviEmail).post(orderURL(po)+"/cancel", form()), http.StatusSeeOther)
	if got := c.state(po); got != "cancelled" {
		t.Errorf("state after Ravi's cancel = %q, want cancelled", got)
	}
}

// AUTH-077.
func caseAmend(t *testing.T, c *authCase) {
	po := c.order(row{number: "PO/26-27/00007", state: "cancelled", project: c.m.Project["PRJ-METRO"]})
	form := url.Values{"version": {c.version(po)}}
	want403(t, "Priya amends", c.as(priyaEmail).post(orderURL(po)+"/amend", form), deny("amend", "Purchase Order"))
	if n := c.orders(); n != 1 {
		t.Fatalf("orders after Priya's amend = %d, want 1", n)
	}
	wantStatus(t, "Ravi amends", c.as(raviEmail).post(orderURL(po)+"/amend", form), http.StatusSeeOther)
	if n := c.count(`SELECT count(*) FROM fixture_order WHERE amended_from_id = ? AND state = 'draft' AND number IS NULL AND created_by = ?`, po, int64(raviID)); n != 1 {
		t.Errorf("Ravi's numberless draft amending the order: %d rows, want 1", n)
	}
	if got := c.state(po); got != "cancelled" {
		t.Errorf("the original's state = %q, want cancelled", got)
	}
}

// AUTH-078.
func caseDelete(t *testing.T, c *authCase) {
	metro := c.m.Project["PRJ-METRO"]
	a := c.order(row{by: raviID, project: metro})
	b := c.order(row{by: priyaID, project: metro})
	want403(t, "Ravi deletes", c.as(raviEmail).post(orderURL(a)+"/delete", url.Values{"version": {c.version(a)}}), deny("delete", "Purchase Order"))
	if n := c.count(`SELECT count(*) FROM fixture_order WHERE id = ?`, a); n != 1 {
		t.Fatalf("Ravi's draft rows after his delete = %d, want 1", n)
	}
	wantStatus(t, "Priya deletes", c.as(priyaEmail).post(orderURL(b)+"/delete", url.Values{"version": {c.version(b)}}), http.StatusSeeOther)
	if n := c.count(`SELECT count(*) FROM fixture_order WHERE id = ?`, b); n != 0 {
		t.Fatalf("Priya's draft rows after her delete = %d, want 0", n)
	}
}

// AUTH-079. The real router has one print page and no PDF route: the browser
// makes the PDF from it (FILE-055), so the probe's /pdf has no counterpart.
func casePrint(t *testing.T, c *authCase) {
	c.setRights(roleViewer, fixture.Order, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll})
	mine := c.order(row{number: "PO/26-27/00001", state: "approved", project: c.m.Project["PRJ-METRO"]})
	other := c.order(row{number: "PO/26-27/00002", state: "approved", project: c.m.Project["PRJ-TECHPARK"]})

	viewer := c.as(viewEmail)
	want403(t, "viewer prints", viewer.get(printURL(mine)), deny("print", "Purchase Order"))
	wantPage(t, "viewer's page", viewer.get(orderURL(mine)), nil, []string{printURL(mine)})

	priya := c.as(priyaEmail)
	wantPage(t, "Priya prints", priya.get(printURL(mine)), []string{"PO/26-27/00001"}, nil)
	want404(t, "Priya prints outside her project", priya.get(printURL(other)), "PO/26-27/00002")
}

// AUTH-080.
func caseExport(t *testing.T, c *authCase) {
	c.order(row{number: "PO/26-27/00001", state: "approved", project: c.m.Project["PRJ-METRO"]})
	path := orderPath + "/export.csv"
	suresh := c.as(sureshEmail)
	want403(t, "Suresh exports", suresh.get(path), deny("export", "Purchase Order"), "PO/26-27/00001")
	wantPage(t, "Suresh's list", suresh.get(orderPath), nil, []string{path})
	if rows := csvRows(t, "Priya exports", c.as(priyaEmail).get(path)); len(rows) != 1 {
		t.Errorf("Priya's export = %v, want the one order", rows)
	}
}

// AUTH-081, through the real import wizard.
func caseImport(t *testing.T, c *authCase) {
	const item = "Item"
	c.setRights("Purchase User", item, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll})
	before := c.count(`SELECT count(*) FROM item`)
	priya := c.as(priyaEmail)
	msg := deny("import", "Item")
	want403(t, "Priya chooses Item", priya.post("/m/import/choose", url.Values{"doctype": {item}}), msg)

	c.setRights("Purchase User", item, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightImport: auth.ScopeAll})
	wantStatus(t, "Priya chooses Item", priya.post("/m/import/choose", url.Values{"doctype": {item}}), http.StatusSeeOther)
	r := priya.upload("/m/import/upload", "items.csv", []byte(itemsCSV), nil)
	wantStatus(t, "Priya uploads", r, http.StatusSeeOther)
	r = priya.get("/m/import/check")
	if text := r.pageText(); !strings.Contains(text, "you don't have permission to create Item.") || !strings.Contains(text, "Row 2") {
		t.Fatalf("check page = %d %q, want row 2's create message", r.status, text)
	}
	if n := c.count(`SELECT count(*) FROM item`); n != before {
		t.Errorf("items after the refused import = %d, want %d", n, before)
	}
}

const itemsCSV = "Code,Name,Type,UoM\nBOLT-M16,Hex Bolt M16x60,Goods,NOS\n"

// AUTH-082.
func caseLinesFollowParent(t *testing.T, c *authCase) {
	metro := c.order(row{project: c.m.Project["PRJ-METRO"]})
	tech := c.order(row{project: c.m.Project["PRJ-TECHPARK"]})
	for _, id := range []int64{metro, tech} {
		if _, err := c.app.DB().SQL().ExecContext(context.Background(),
			`INSERT INTO fixture_order_line (parent_id, line_no, item_id, qty, rate) VALUES (?, 1, ?, 1000, 100)`, id, c.m.Item["BOLT-M12"]); err != nil {
			t.Fatal(err)
		}
	}
	line := func(id int64) url.Values {
		return url.Values{
			"version": {c.version(id)}, "item_id": {idText(c.m.Item["WIRE-18G"])}, "qty": {"1.000"}, "rate": {"1.0000"}, "uom": {"kg"},
		}
	}
	lines := func() int {
		return c.count(`SELECT count(*) FROM fixture_order_line`)
	}
	ravi := c.as(raviEmail)
	msg := deny("change", "Purchase Order")
	want403(t, "Ravi adds a line", ravi.post(orderURL(metro)+"/lines/new", line(metro)), msg)
	want403(t, "Ravi removes a line", ravi.post(orderURL(metro)+"/lines/1/remove", url.Values{"version": {c.version(metro)}}), msg)
	priya := c.as(priyaEmail)
	want404(t, "Priya adds to a hidden order", priya.post(orderURL(tech)+"/lines/new", line(tech)))
	want404(t, "Priya removes from a hidden order", priya.post(orderURL(tech)+"/lines/1/remove", url.Values{"version": {c.version(tech)}}))
	if n := lines(); n != 2 {
		t.Errorf("lines after the refused posts = %d, want 2", n)
	}
}

// AUTH-083 on the real form: the guard strips both fields and DOC sets them.
func caseOwnerNotClientSettable(t *testing.T, c *authCase) {
	priya := c.as(priyaEmail)
	r := priya.post(orderPath+"/new", url.Values{
		"supplier_id": {idText(c.m.Party["SHREE"])}, "project_id": {idText(c.m.Project["PRJ-METRO"])},
		"order_date": {"24-10-2026"}, "amount": {"1000.00"}, "priority": {"normal"}, "discount_pct": {"0"},
		"created_by": {idText(int64(raviID))}, "created_at": {"2026-04-01T00:00:00Z"},
	})
	wantStatus(t, "Priya creates", r, http.StatusSeeOther)
	id := c.onlyOrder()
	check := func(when string) {
		t.Helper()
		if got := c.text(`SELECT CAST(created_by AS TEXT) FROM fixture_order WHERE id = ?`, id); got != idText(int64(priyaID)) {
			t.Errorf("created_by %s = %s, want Priya's", when, got)
		}
		if got, want := c.text(`SELECT created_at FROM fixture_order WHERE id = ?`, id), "2026-10-24T05:00:00.000Z"; got != want {
			t.Errorf("created_at %s = %q, want the server time %q", when, got, want)
		}
	}
	check("after create")
	form := c.details(id, "edited")
	form.Set("created_by", idText(int64(raviID)))
	wantStatus(t, "Priya edits", priya.post(orderURL(id)+stepDetails, form), http.StatusSeeOther)
	if got := c.text(`SELECT remarks FROM fixture_order WHERE id = ?`, id); got != "edited" {
		t.Fatalf("remarks = %q: the save didn't run", got)
	}
	check("after the edit")
}

func (c *authCase) onlyOrder() int64 {
	c.t.Helper()
	var id int64
	if err := c.app.DB().SQL().QueryRowContext(context.Background(), `SELECT id FROM fixture_order`).Scan(&id); err != nil {
		c.t.Fatal(err)
	}
	return id
}
