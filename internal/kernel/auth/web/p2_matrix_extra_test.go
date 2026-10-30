package authweb_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/views"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
)

// Task 17 fix round (AD-1 rulings): the role forms through the web, versions
// and Config.Seed.

const t17xStoresChanged = "Someone else changed Stores after you opened it. Reload it and make your changes again."

func t17xVersion(t *testing.T, w *authtest.World, role string) string {
	t.Helper()
	return strconv.FormatInt(t14Int(t, w, `SELECT version FROM roles WHERE name = ?`, role), 10)
}

func t17xRolePath(t *testing.T, w *authtest.World, role, suffix string) string {
	t.Helper()
	return fmt.Sprintf("/roles/%d%s", t17RoleID(t, w, role), suffix)
}

func TestRoles_WebCreateRenameDelete(t *testing.T) {
	w := authtest.NewWorld(t)
	h := webtest.New(t, w)
	admin := signedIn(t, h, t13Admin)

	r := admin.Post("/roles", url.Values{"name": {"Quality"}})
	wantRedirect(t, "admin creates Quality", r, http.StatusSeeOther, "/roles")
	if n := t14Int(t, w, `SELECT COUNT(*) FROM roles WHERE name = 'Quality'`); n != 1 {
		t.Fatalf("roles named Quality = %d, want 1", n)
	}

	r = admin.Get("/roles")
	wantStatus(t, "admin GET /roles", r, http.StatusOK)
	if text := p1Text(p1Doc(t, "GET /roles", r).Find("main")); !strings.Contains(text, "Quality (0 users)") {
		t.Fatalf("GET /roles text %q does not list the new role", text)
	}
	rights := t17RightsPath(t, w, "Stores", authtest.DTOrder)
	r = admin.Get(rights)
	wantStatus(t, "admin GET the rights page", r, http.StatusOK)
	po := p1Doc(t, "GET the rights page", r)
	if got, want := p1Hidden(t, "rights page", p1Form(t, "rights page", po, rights), "version"), t17xVersion(t, w, "Stores"); got != want {
		t.Errorf("Purchase Order rights form version = %q, want %q", got, want)
	}
	p1Form(t, "rights page", po, "/rights/"+authtest.DTOrder+"/reset")

	r = admin.Post(t17xRolePath(t, w, "Quality", "/rename"), url.Values{"name": {"QA"}, "version": {t17xVersion(t, w, "Quality")}})
	wantRedirect(t, "admin renames Quality", r, http.StatusSeeOther, "/roles")
	if n := t14Int(t, w, `SELECT COUNT(*) FROM roles WHERE name = 'QA'`); n != 1 {
		t.Fatalf("roles named QA after rename = %d, want 1", n)
	}

	id := t17RoleID(t, w, "QA")
	r = admin.Post(t17xRolePath(t, w, "QA", "/delete"), url.Values{"version": {t17xVersion(t, w, "QA")}})
	wantRedirect(t, "admin deletes QA", r, http.StatusSeeOther, "/roles")
	if n := t14Int(t, w, `SELECT COUNT(*) FROM roles WHERE id = ?`, id); n != 0 {
		t.Fatalf("roles with QA's id after delete = %d, want 0", n)
	}
}

func TestRoles_WebNonAdminRefused(t *testing.T) {
	w := authtest.NewWorld(t)
	h := webtest.New(t, w)
	priya := signedIn(t, h, t10Priya)
	v := t17xVersion(t, w, "Stores")
	before := t17Rights(t, w, "Stores", authtest.DTOrder)

	for _, tc := range []struct {
		what string
		r    webtest.Response
	}{
		{"GET /roles", priya.Get("/roles")},
		{"POST /roles", priya.Post("/roles", url.Values{"name": {"Quality"}})},
		{"rename", priya.Post(t17xRolePath(t, w, "Stores", "/rename"), url.Values{"name": {"Warehouse"}, "version": {v}})},
		{"delete", priya.Post(t17xRolePath(t, w, "Stores", "/delete"), url.Values{"version": {v}})},
		{"rights", priya.Post(t17RightsPath(t, w, "Stores", authtest.DTOrder), url.Values{"read": {"all"}, "print": {"all"}, "version": {v}})},
		{"reset", priya.Post("/rights/"+authtest.DTOrder+"/reset", url.Values{})},
	} {
		wantStatus(t, "priya "+tc.what, tc.r, http.StatusForbidden)
		wantBody(t, "priya "+tc.what, tc.r, auth.ErrAdminOnlyRoles.Msg)
	}
	if n := t14Int(t, w, `SELECT COUNT(*) FROM roles WHERE name IN ('Quality', 'Warehouse')`); n != 0 {
		t.Errorf("roles Quality or Warehouse after refused posts = %d, want 0", n)
	}
	if n := t14Int(t, w, `SELECT COUNT(*) FROM roles WHERE name = 'Stores'`); n != 1 {
		t.Errorf("roles named Stores after refused posts = %d, want 1", n)
	}
	if got := t17Rights(t, w, "Stores", authtest.DTOrder); !reflect.DeepEqual(got, before) {
		t.Errorf("RoleRights(Stores, PO) after refused posts = %v, want %v", got, before)
	}
}

// CD-11: Team is offered only on DocTypes with an employee field.
func TestMatrix_TeamOnlyWhereOffered(t *testing.T) {
	w := authtest.NewWorld(t)
	h := webtest.New(t, w)
	admin := signedIn(t, h, t13Admin)
	const team, own = `input[type=radio][value="team"]`, `input[type=radio][value="own"]`
	po, claim := t17RightsPath(t, w, "Stores", authtest.DTOrder), t17RightsPath(t, w, "Stores", authtest.DTClaim)
	r := admin.Get(po)
	wantStatus(t, "admin GET the Purchase Order rights page", r, http.StatusOK)
	if s := p1Form(t, "Purchase Order", p1Doc(t, "Purchase Order", r), po); s.Find(team).Length() != 0 || s.Find(own).Length() == 0 {
		t.Errorf("Purchase Order rights (no employee field): want Own and no Team option, got %q", p1Text(s))
	}
	r = admin.Get(claim)
	wantStatus(t, "admin GET the Expense Claim rights page", r, http.StatusOK)
	if s := p1Form(t, "Expense Claim", p1Doc(t, "Expense Claim", r), claim); s.Find(team).Length() == 0 {
		t.Errorf("Expense Claim rights (employee field): no Team option in %q", p1Text(s))
	}
}

func TestRoles_WebVersions(t *testing.T) {
	w := authtest.NewWorld(t)
	h := webtest.New(t, w)
	admin := signedIn(t, h, t13Admin)
	rights := t17RightsPath(t, w, "Stores", authtest.DTOrder)
	stale := t17xVersion(t, w, "Stores")

	// Missing version: 400 errBadForm, nothing changes.
	for _, tc := range []struct {
		what, path string
		form       url.Values
	}{
		{"rename", t17xRolePath(t, w, "Stores", "/rename"), url.Values{"name": {"Warehouse"}}},
		{"delete", t17xRolePath(t, w, "Stores", "/delete"), url.Values{}},
		{"rights", rights, url.Values{"read": {"all"}, "print": {"all"}}},
	} {
		r := admin.Post(tc.path, tc.form)
		wantStatus(t, tc.what+" without a version", r, http.StatusBadRequest)
		wantBody(t, tc.what+" without a version", r, auth.ErrBadForm.Msg)
	}
	if n := t14Int(t, w, `SELECT COUNT(*) FROM roles WHERE name = 'Stores' AND version = ?`, stale); n != 1 {
		t.Fatalf("Stores at version %s after the refused posts = %d rows, want 1", stale, n)
	}

	// A rights save bumps the version; the page it redirects to carries it.
	r := admin.Post(rights, url.Values{"read": {"all"}, "print": {"all"}, "version": {stale}})
	wantRedirect(t, "rights save", r, http.StatusSeeOther, rights)
	current := t17xVersion(t, w, "Stores")
	if current == stale {
		t.Fatalf("Stores version after a rights save = %s, want it bumped", current)
	}
	r = admin.Get(rights)
	wantStatus(t, "the page after the rights save", r, http.StatusOK)
	form := p1Form(t, "rights save answer", p1Doc(t, "rights save answer", r), rights)
	if got := p1Hidden(t, "rights save answer", form, "version"); got != current {
		t.Errorf("rights save answer: version = %q, want %q", got, current)
	}

	// Stale version: 409, nothing changes.
	saved := t17Rights(t, w, "Stores", authtest.DTOrder)
	r = admin.Post(rights, url.Values{"read": {"all"}, "version": {stale}})
	wantStatus(t, "rights save at a stale version", r, http.StatusConflict)
	wantBody(t, "rights save at a stale version", r, t17xStoresChanged)
	if got := t17Rights(t, w, "Stores", authtest.DTOrder); !reflect.DeepEqual(got, saved) {
		t.Errorf("RoleRights(Stores, PO) after the stale save = %v, want %v", got, saved)
	}
	r = admin.Post(t17xRolePath(t, w, "Stores", "/rename"), url.Values{"name": {"Warehouse"}, "version": {stale}})
	wantStatus(t, "rename at a stale version", r, http.StatusConflict)
	wantBody(t, "rename at a stale version", r, t17xStoresChanged)
	if n := t14Int(t, w, `SELECT COUNT(*) FROM roles WHERE name = 'Stores'`); n != 1 {
		t.Errorf("roles named Stores after the stale rename = %d, want 1", n)
	}

	r = admin.Post(t17xRolePath(t, w, "Stores", "/rename"), url.Values{"name": {"Warehouse"}, "version": {current}})
	wantRedirect(t, "rename at the current version", r, http.StatusSeeOther, "/roles")
}

// The reset route seeds with Config.Seed: in production (LinkedMasterRead)
// resetting a master keeps the creators' derived read rows.
func TestMatrix_ResetUsesConfigSeed(t *testing.T) {
	w := authtest.NewWorld(t)
	read := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}
	w.SetRights(t, "Purchase User", authtest.DTSite, read) // the production matrix's derived row

	// /seeded/... is served by a second Server with Config.Seed set, behind
	// the harness's session middleware.
	var seeded http.Handler
	h := webtest.New(t, w, webtest.WithRoutes(func(r chi.Router, _ *authweb.Server) {
		r.Post("/seeded/rights/{doctype}/reset", func(rw http.ResponseWriter, req *http.Request) {
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, nil))
			req.URL.Path = strings.TrimPrefix(req.URL.Path, "/seeded")
			seeded.ServeHTTP(rw, req)
		})
	}))
	srv := authweb.New(w.Auth, auth.NewSessionStore(w.DB, w.Clock, auth.DefaultSessionConfig(false)), h.SM,
		views.Default{}, authweb.Config{Seed: auth.SeedOptions{LinkedMasterRead: true}})
	mux := chi.NewRouter()
	srv.Mount(mux)
	seeded = mux
	admin := signedIn(t, h, t13Admin)

	r := admin.Post("/seeded/rights/"+authtest.DTSite+"/reset", url.Values{})
	wantRedirect(t, "reset Site with LinkedMasterRead", r, http.StatusSeeOther, "/roles")
	if got := t17Rights(t, w, "Purchase User", authtest.DTSite); !reflect.DeepEqual(got, read) {
		t.Errorf("RoleRights(Purchase User, Site) after the production reset = %v, want %v", got, read)
	}

	r = admin.Post("/rights/"+authtest.DTSite+"/reset", url.Values{})
	wantRedirect(t, "reset Site with the default Config", r, http.StatusSeeOther, "/roles")
	if got := t17Rights(t, w, "Purchase User", authtest.DTSite); len(got) != 0 {
		t.Errorf("RoleRights(Purchase User, Site) after the test-seed reset = %v, want none", got)
	}
}
