package app_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

const purchaseHome = "/modules/purchase"

// purchaseNavItem returns the text of p's nav link to the Purchase home, and
// whether there is one.
func purchaseNavItem(b *browser) (string, bool) {
	var text string
	found := false
	b.get("/approvals").doc.Find(`.nav-list a`).Each(func(_ int, s *goquery.Selection) {
		if s.AttrOr("href", "") == purchaseHome {
			text, found = strings.Join(strings.Fields(s.Text()), " "), true
		}
	})
	return text, found
}

// MOD-PO P1 Do 4 (PO spec §25): the nav has `Purchase` to /modules/purchase
// for a user with read on purchase.Order or on purchase.Request, and nobody
// else sees it. Rights are read on the real auth.Service.
func TestApp_PurchaseNav(t *testing.T) {
	w := newWorld(t)
	admin := w.seedUsers()
	ctx := context.Background()
	a := w.app.Auth()

	for role, dt := range map[string]string{"Order Reader": purchase.DocType, "Request Reader": purchase.RequestDocType} {
		if err := a.CreateRole(ctx, admin, role); err != nil {
			t.Fatalf("CreateRole(%s): %v", role, err)
		}
		if err := a.SetRights(ctx, admin, role, dt, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}); err != nil {
			t.Fatalf("SetRights(%s, %s): %v", role, dt, err)
		}
	}
	for _, u := range []auth.NewUser{
		{Email: "orders@vardhan.test", Name: "Olga Reader", Password: testPassword, Roles: []string{"Order Reader"}},
		{Email: "requests@vardhan.test", Name: "Ravi Reader", Password: testPassword, Roles: []string{"Request Reader"}},
		{Email: nobodyEmail, Name: "Nina Bose", Password: testPassword, Roles: []string{}},
	} {
		if _, _, err := a.CreateUser(ctx, admin, u); err != nil {
			t.Fatalf("CreateUser(%s): %v", u.Email, err)
		}
	}

	for _, c := range []struct {
		email string
		shown bool
	}{
		{"orders@vardhan.test", true},   // read on purchase.Order only
		{"requests@vardhan.test", true}, // read on purchase.Request only
		{deepakEmail, true},             // Employee: read (own) on purchase.Request (§20.6)
		{sureshEmail, true},             // Stores: read on both
		{adminEmail, true},
		{nobodyEmail, false}, // no roles: read on neither
	} {
		text, found := purchaseNavItem(w.signedIn(c.email))
		if found != c.shown {
			t.Errorf("%s: nav has %s = %v, want %v", c.email, purchaseHome, found, c.shown)
		}
		if found && text != "Purchase" {
			t.Errorf("%s: nav item %s reads %q, want %q", c.email, purchaseHome, text, "Purchase")
		}
	}

	// Taking the read away hides it again.
	if err := a.SetRights(ctx, admin, "Order Reader", purchase.DocType, map[auth.Right]auth.Scope{}); err != nil {
		t.Fatalf("SetRights(Order Reader, none): %v", err)
	}
	if _, found := purchaseNavItem(w.signedIn("orders@vardhan.test")); found {
		t.Errorf("orders@vardhan.test after losing read: nav still has %s", purchaseHome)
	}
}

// MOD-PO P1 Do 3 (CD-46, CD-256; review focus): every P1 module route is
// mounted behind AUTH's guard and answers 501 until its owner builds it. A
// guest is sent to sign in (a POST gets 401), and a POST without the CSRF token gets AUTH's
// refusal before the stub.
func TestApp_PurchaseRoutesStubbed(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	routes := []struct{ method, path string }{
		{http.MethodGet, "/modules/purchase"},
		{http.MethodGet, "/modules/purchase/q/to-send"},
		{http.MethodGet, "/modules/purchase/orders/1/handover"},
		{http.MethodPost, "/modules/purchase/orders/1/handover"},
		{http.MethodGet, "/modules/purchase/orders/1/send"},
		{http.MethodPost, "/modules/purchase/orders/1/send"},
		{http.MethodPost, "/modules/purchase/orders/1/send/undo"},
		{http.MethodPost, "/modules/purchase/orders/1/copy"},
		{http.MethodGet, "/modules/purchase/orders/1/expected"},
		{http.MethodPost, "/modules/purchase/orders/1/expected"},
		{http.MethodPost, "/modules/purchase/orders/1/expected/clear"},
		{http.MethodGet, "/modules/purchase/requests/1/order"},
		{http.MethodPost, "/modules/purchase/requests/1/order"},
		{http.MethodGet, "/reports/purchase/pending"},
		{http.MethodGet, "/reports/purchase/pending/filter"},
		{http.MethodGet, "/reports/purchase/pending/export.csv"},
	}
	guest := w.browser()
	b := w.signedIn(adminEmail)
	noToken := *b
	noToken.csrf = ""
	form := url.Values{"version": {"1"}}
	for _, rt := range routes {
		send := func(br *browser) *response {
			if rt.method == http.MethodPost {
				return br.post(rt.path, form)
			}
			return br.get(rt.path)
		}
		if rt.method == http.MethodPost {
			r := guest.do(rt.method, rt.path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", map[string]string{"Origin": w.srv.URL})
			if r.status != http.StatusUnauthorized || r.alert() != msgSignInRequired {
				t.Errorf("guest POST %s = %d %q, want 401 %q", rt.path, r.status, r.alert(), msgSignInRequired)
			}
		} else if r := guest.get(rt.path); r.status != http.StatusSeeOther || r.location() != "/signin?next="+url.QueryEscape(rt.path) {
			t.Errorf("guest GET %s = %d Location %q, want 303 to the sign-in page", rt.path, r.status, r.location())
		}
		if r := send(b); r.status != http.StatusNotImplemented {
			t.Errorf("signed in %s %s = %d (%q), want 501", rt.method, rt.path, r.status, r.pageText())
		}
		if rt.method == http.MethodPost {
			if r := send(&noToken); r.status != http.StatusForbidden || r.alert() != auth.ErrCSRF.Msg {
				t.Errorf("POST %s without CSRF = %d %q, want 403 %q", rt.path, r.status, r.alert(), auth.ErrCSRF.Msg)
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
		{purchase.DocType, map[string]string{"vendor_id": vendor, "site_id": site, "project_id": project, "order_date": "24-10-2026"}},
		{purchase.RequestDocType, map[string]string{"site_id": site, "project_id": project, "request_date": "24-10-2026"}},
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
