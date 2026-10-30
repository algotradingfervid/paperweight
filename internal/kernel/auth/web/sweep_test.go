package authweb_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	authscreens "github.com/vardhan/internaltools/internal/kernel/auth/web/screens"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/ui/screens"
)

// The role screens in the overflow and axe sweep (design-system §10, CD-219).

// p8Skip is every GET route of the harness that isn't an AUTH screen, and
// /setup, which only an empty install shows, so the e2e sweep covers it there.
// Uncovered matches by prefix, so nothing here may be a prefix of /roles or
// /users: TestScreens_NewRoleRouteIsUncovered fails if one is.
var p8Skip = slices.Concat(screens.DefaultSkip, []string{"/_test", "/setup"})

// p8Plain are the variants a GET alone shows: what on the page proves it.
var p8Plain = map[string]struct{ sel, text string }{
	"roles":              {"main h1", "Roles"},
	"role":               {"main .card", "Change"},
	"role administrator": {"main p", p6AdminLine},
	"rights not shown":   {"main p", "Not shown because they don't apply to Item"},
	"rights team":        {`main input[name=read][value=team]`, ""},
	"copy rights":        {`main input[name=source_id]`, ""},
	"sign in":            {`form input[name=password]`, ""},
	"password":           {"main h1", "Change password"},
	"sessions":           {"main h1", "Sessions"},
	"users":              {"main h1", "Users"},
	"user":               {"main h1", ""},
	"user access":        {"main h1", ""},
	"who can":            {"main h1", "Who can"},
	"who can results":    {"main", "Administrator"},
}

// p8IDs is the fixture's Stores and Administrator roles, Item (a master, so
// submit doesn't apply) and Expense Claim (the one with an employee field).
func p8IDs(t *testing.T, w *authtest.World) authscreens.SweepIDs {
	t.Helper()
	return authscreens.SweepIDs{
		Role: t17RoleID(t, w, p6Stores), Admin: t17RoleID(t, w, auth.RoleAdministrator), User: 1,
		NotShown: authtest.DTItem, Team: authtest.DTClaim,
	}
}

var p8Once sync.Once

// p8Register registers the role screens once: Register refuses a repeat, also
// under -count=2. Uncovered reads only their routes.
func p8Register(t *testing.T, w *authtest.World) {
	t.Helper()
	list := authscreens.Screens(p8IDs(t, w), nil)
	p8Once.Do(func() { screens.Register(list...) })
}

func TestScreens_CoverEveryRoleRoute(t *testing.T) {
	w, h, _ := p6World(t)
	p8Register(t, w)
	if missing := screens.Uncovered(h.Router, p8Skip); len(missing) != 0 {
		t.Errorf("routes with no screen: %q", missing)
	}
}

// The harness router plus routes under /roles: the skip list must not let any
// of them through, so a new role page fails here and not at WP3.
func TestScreens_NewRoleRouteIsUncovered(t *testing.T) {
	added := []string{"/roles/new-page", "/roles/{id}/new-page", "/roles/{id}/rights/{doctype}/new-page"}
	w := authtest.NewWorld(t)
	h := webtest.New(t, w, webtest.WithRoutes(func(r chi.Router, _ *authweb.Server) {
		for _, route := range added {
			r.Method(http.MethodGet, route, http.NotFoundHandler())
		}
	}))
	p8Register(t, w)
	got := screens.Uncovered(h.Router, p8Skip)
	slices.Sort(got)
	slices.Sort(added)
	if !slices.Equal(got, added) {
		t.Errorf("Uncovered = %q, want the new routes %q", got, added)
	}
}

// Every variant opens as the fixture admin, or signed out for the sign-in page,
// on the route it says it covers, and the ones with no Setup show the state they
// are named for.
func TestScreens_VariantsOpenAsAdmin(t *testing.T) {
	w, h, admin := p6World(t)
	list := authscreens.Screens(p8IDs(t, w), nil)
	if len(list) != len(p8Plain)+len(p8Stateful) {
		t.Errorf("%d screens, want %d", len(list), len(p8Plain)+len(p8Stateful))
	}
	for _, s := range list {
		client, as := admin, t13Admin
		if s.Route == "/signin" {
			client, as = h.Client(), ""
		}
		if s.As != as {
			t.Errorf("%s: As = %q, want %q", s.Name, s.As, as)
		}
		path, _, _ := strings.Cut(s.URL, "?")
		if got := h.Router.Find(chi.NewRouteContext(), http.MethodGet, path); got != s.Route {
			t.Errorf("%s: %s is route %q, screen says %q", s.Name, s.URL, got, s.Route)
		}
		doc := p6Get(t, client, s.URL)
		want, plain := p8Plain[s.Name]
		if !plain {
			if s.Setup == nil {
				t.Errorf("%s: no Setup and not a known plain variant", s.Name)
			}
			continue
		}
		if s.Setup != nil {
			t.Errorf("%s: a plain variant has a Setup", s.Name)
		}
		found := doc.Find(want.sel)
		if found.Length() == 0 || !strings.Contains(p1Text(found), want.text) {
			t.Errorf("%s: %s = %q, want it to hold %q", s.Name, want.sel, p1Text(found), want.text)
		}
	}
}

// p8Stateful are the variants whose Setup makes the state. act does over HTTP
// what the Setup's script does in the browser, and returns the page it ends on;
// text is what that page says. The e2e build runs the scripts themselves
// (sweep_browser_test.go).
var p8Stateful = map[string]struct {
	act    func(t *testing.T, w *authtest.World, admin *webtest.Client, path string) webtest.Response
	status int
	text   string
}{
	"rights read notice": {func(t *testing.T, w *authtest.World, admin *webtest.Client, path string) webtest.Response {
		r := admin.Post(path, t13With(url.Values{"create": {"own"}}, t17RoleVersion(t, w, p6Stores)))
		wantRedirect(t, "save without read", r, http.StatusSeeOther, path)
		return admin.Get(path)
	}, http.StatusOK, p6ReadAdded},
	"rights 409": {func(t *testing.T, w *authtest.World, admin *webtest.Client, path string) webtest.Response {
		v, err := strconv.ParseInt(t17RoleVersion(t, w, p6Stores), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return admin.Post(path, t13With(url.Values{"read": {"own"}}, strconv.FormatInt(v-1, 10)))
	}, http.StatusConflict, "Reload"},
	"copy rights 422": {func(t *testing.T, w *authtest.World, admin *webtest.Client, path string) webtest.Response {
		return admin.Post(path, p1Post(t17RoleVersion(t, w, p6Stores), ""))
	}, http.StatusUnprocessableEntity, auth.ErrCopySourceRequired.Msg},
	"sign in refused": {func(t *testing.T, w *authtest.World, admin *webtest.Client, path string) webtest.Response {
		return admin.Post(path, url.Values{"email": {"nobody@vardhan.test"}, "password": {"wrong"}})
	}, http.StatusUnauthorized, "Invalid email or password."},
}

// p8Screen is the variant called name, which must have a Setup.
func p8Screen(t *testing.T, name string, ids authscreens.SweepIDs, submit func(context.Context, string, string) error) screens.Screen {
	t.Helper()
	list := authscreens.Screens(ids, submit)
	i := slices.IndexFunc(list, func(s screens.Screen) bool { return s.Name == name })
	if i < 0 {
		t.Fatalf("no screen named %q", name)
	}
	if list[i].Setup == nil {
		t.Fatalf("%s: no Setup", name)
	}
	return list[i]
}

// Each Setup asks the browser to wait for a selector; that selector must find
// the state on the page the server really answers with.
func TestScreens_SetupWaitsForItsState(t *testing.T) {
	for name, tc := range p8Stateful {
		t.Run(name, func(t *testing.T) {
			w, _, admin := p6World(t)
			var waits []string
			s := p8Screen(t, name, p8IDs(t, w), func(_ context.Context, js, wait string) error {
				if js == "" {
					t.Error("Submit got no script")
				}
				waits = append(waits, wait)
				return nil
			})
			if err := s.Setup(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(waits) != 1 {
				t.Fatalf("Setup called Submit %d times, want 1", len(waits))
			}
			r := tc.act(t, w, admin, s.URL)
			wantStatus(t, "the state's page", r, tc.status)
			found := p1Doc(t, "the state's page", r).Find(waits[0])
			if found.Length() != 1 || !strings.Contains(p1Text(found), tc.text) {
				t.Errorf("%s = %q (%d found), want one holding %q", waits[0], p1Text(found), found.Length(), tc.text)
			}
		})
	}
}
