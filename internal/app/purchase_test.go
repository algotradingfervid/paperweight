package app_test

import (
	"context"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-PO P1 Do 3 (CD-46, CD-256; AD-13 ruling on the route tests): every
// purchase module route is mounted behind AUTH's guard. A signed-in
// Administrator, on a real approved PO and submitted request, gets neither
// 404 nor 405, and no handler panics (500). A guest is sent to sign in: a GET
// is redirected (303) and a POST gets 401. A POST without the CSRF token gets
// AUTH's 403. What each route answers is its owner's test.
func TestApp_PurchaseRoutesStubbed(t *testing.T) {
	w := newWorld(t)
	admin, _, _ := w.canonical()
	ctx := context.Background()
	id := func(table, code string) string {
		t.Helper()
		return w.text(`SELECT CAST(id AS TEXT) FROM `+table+` WHERE code = ?`, code)
	}
	header := map[string]string{"site_id": id("site", "METRO-SITE"), "project_id": id("project", "PRJ-METRO")}
	line := map[string]string{"item_id": id("item", "MS-ANGLE-50"), "uom_id": id("uom", "KGS"), "qty": "100"}
	approved := func(dt string, values, line map[string]string) int64 {
		t.Helper()
		res, err := w.app.Engine().Do(ctx, admin, dt, 0, doctype.ActionSave, doc.ActionInput{Values: values, Lines: []map[string]string{line}})
		if err != nil {
			t.Fatalf("save %s as admin: %v", dt, err)
		}
		res, err = w.app.Engine().Do(ctx, admin, dt, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version})
		if err != nil || res.State != doctype.StateApproved {
			t.Fatalf("submit %s %d as admin = %s, %v; want approved", dt, res.ID, res.State, err)
		}
		return res.ID
	}
	poValues := map[string]string{"vendor_id": id("party", "SHREE"), "order_date": "2026-10-24"}
	maps.Copy(poValues, header)
	poLine := map[string]string{"price": "62.5", "tax_code_id": id("tax_code", "GST18"), "hsn_sac": "7216"}
	maps.Copy(poLine, line)
	prValues := map[string]string{"request_date": "2026-10-24"}
	maps.Copy(prValues, header)
	po := strconv.FormatInt(approved(purchase.DocType, poValues, poLine), 10)
	pr := strconv.FormatInt(approved(purchase.RequestDocType, prValues, line), 10)

	routes := []struct{ method, path string }{
		{http.MethodGet, "/modules/purchase"},
		{http.MethodGet, "/modules/purchase/q/to-send"},
		{http.MethodGet, "/modules/purchase/orders/" + po + "/handover"},
		{http.MethodPost, "/modules/purchase/orders/" + po + "/handover"},
		{http.MethodGet, "/modules/purchase/orders/" + po + "/send"},
		{http.MethodPost, "/modules/purchase/orders/" + po + "/send"},
		{http.MethodPost, "/modules/purchase/orders/" + po + "/send/undo"},
		{http.MethodPost, "/modules/purchase/orders/" + po + "/copy"},
		{http.MethodGet, "/modules/purchase/orders/" + po + "/expected"},
		{http.MethodPost, "/modules/purchase/orders/" + po + "/expected"},
		{http.MethodPost, "/modules/purchase/orders/" + po + "/expected/clear"},
		{http.MethodGet, "/modules/purchase/requests/" + pr + "/order"},
		{http.MethodPost, "/modules/purchase/requests/" + pr + "/order"},
		{http.MethodGet, "/reports/purchase/pending"},
		{http.MethodGet, "/reports/purchase/pending/filter"},
		{http.MethodGet, "/reports/purchase/pending/export.csv"},
	}
	guest := w.browser()
	b := w.signedIn(adminEmail)
	noToken := *b
	noToken.csrf = ""
	form := url.Values{"version": {w.text(`SELECT CAST(version AS TEXT) FROM purchase_order WHERE id = ?`, po)}}
	for _, rt := range routes {
		send := func(br *browser) *response {
			if rt.method == http.MethodPost {
				return br.post(rt.path, form)
			}
			return br.get(rt.path)
		}
		if rt.method == http.MethodPost {
			r := guest.do(rt.method, rt.path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", map[string]string{"Origin": w.srv.URL})
			if r.status != http.StatusUnauthorized || !strings.Contains(r.pageText(), msgSignInRequired) {
				t.Errorf("guest POST %s = %d %q, want 401 %q", rt.path, r.status, r.pageText(), msgSignInRequired)
			}
		} else if r := guest.get(rt.path); r.status != http.StatusSeeOther || r.location() != "/signin?next="+url.QueryEscape(rt.path) {
			t.Errorf("guest GET %s = %d Location %q, want 303 to the sign-in page", rt.path, r.status, r.location())
		}
		switch r := send(b); r.status {
		case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusInternalServerError:
			t.Errorf("signed in %s %s = %d (%q), want the route mounted (not 404, 405 or a 500 panic)", rt.method, rt.path, r.status, r.pageText())
		}
		if rt.method == http.MethodPost {
			if r := send(&noToken); r.status != http.StatusForbidden || !strings.Contains(r.pageText(), auth.ErrCSRFSignIn.Msg) {
				t.Errorf("POST %s without CSRF = %d %q, want 403 %q", rt.path, r.status, r.pageText(), auth.ErrCSRFSignIn.Msg)
			}
		}
	}
}

// MOD-PO P1 "Service shape" (CD-284): app.New wires purchase's hook
// dependencies for its own database, so the Date hooks run on the app's
// documents and return order_date and request_date.
func TestApp_PurchaseWired(t *testing.T) {
	w := newWorld(t)
	admin, fx, _ := w.canonical()
	ctx := context.Background()
	var vendor, site, project string
	for _, p := range fx.Parties {
		if p.Code == "SHREE" {
			vendor = strconv.FormatInt(p.ID, 10)
		}
	}
	for _, s := range fx.Sites {
		if s.Code == "METRO-SITE" {
			site = strconv.FormatInt(s.ID, 10)
		}
	}
	for _, p := range fx.Projects {
		if p.Code == "PRJ-METRO" {
			project = strconv.FormatInt(p.ID, 10)
		}
	}
	if vendor == "" || site == "" || project == "" {
		t.Fatalf("fixture ids: vendor %q site %q project %q, want all set", vendor, site, project)
	}

	want := types.Date{Year: 2026, Month: 10, Day: 24}
	for _, c := range []struct {
		dt     string
		values map[string]string
	}{
		{purchase.DocType, map[string]string{"vendor_id": vendor, "site_id": site, "project_id": project, "order_date": "2026-10-24"}},
		{purchase.RequestDocType, map[string]string{"site_id": site, "project_id": project, "request_date": "2026-10-24"}},
	} {
		res, err := w.app.Engine().Do(ctx, admin, c.dt, 0, doctype.ActionSave, doc.ActionInput{Values: c.values})
		if err != nil {
			t.Fatalf("create %s draft as admin: %v", c.dt, err)
		}
		dt, ok := doctype.Lookup(c.dt)
		if !ok {
			t.Fatalf("%s not registered", c.dt)
		}
		if err := w.app.DB().Read(ctx, func(tx *db.Tx) error {
			got, err := dt.Hooks.Date(ctx, tx, res.ID)
			if err != nil || got != want {
				t.Errorf("%s Date on the app's database = %v, %v; want %v, nil (wired by app.New)", c.dt, got, err, want)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}
