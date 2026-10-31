package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// purchaseHome is the Purchase nav entry's link (plan P1 Do 4).
const purchaseHome = "/modules/purchase"

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
