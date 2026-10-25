package authweb_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

const t17NewDoc = "t17.NewDoc"

var t17Roles = []string{
	"Administrator", "Director", "Purchase Manager", "Purchase User", "Stores", "Masters Manager", "Employee",
}

func t17RoleID(t *testing.T, w *authtest.World, role string) int64 {
	t.Helper()
	return t14Int(t, w, `SELECT id FROM roles WHERE name = ?`, role)
}

// t17RightsPath is POST /roles/{id}/rights/{doctype} for role.
func t17RightsPath(t *testing.T, w *authtest.World, role, dt string) string {
	t.Helper()
	return fmt.Sprintf("/roles/%d/rights/%s", t17RoleID(t, w, role), dt)
}

func t17Rights(t *testing.T, w *authtest.World, role, dt string) map[auth.Right]auth.Scope {
	t.Helper()
	got, err := w.Auth.RoleRights(context.Background(), role, dt)
	if err != nil {
		t.Fatalf("RoleRights(%s, %s): %v", role, dt, err)
	}
	return got
}

// t17Defaults is dt's DefaultRights for role, typed.
func t17Defaults(dt *doctype.DocType, role string) map[auth.Right]auth.Scope {
	out := map[auth.Right]auth.Scope{}
	for r, s := range dt.DefaultRights[role] {
		out[auth.Right(r)] = auth.Scope(s)
	}
	return out
}

func t17Customised(t *testing.T, w *authtest.World, role, dt string) int64 {
	t.Helper()
	return t14Int(t, w, `SELECT rd.customised FROM role_doctypes rd JOIN roles r ON r.id = rd.role_id
		WHERE r.name = ? AND rd.doctype = ?`, role, dt)
}

// t17WantSaved accepts a successful matrix save (page or redirect).
func t17WantSaved(t *testing.T, what string, r webtest.Response) {
	t.Helper()
	if r.Status != http.StatusOK && r.Status != http.StatusSeeOther {
		t.Fatalf("%s: status = %d, want 200 or 303 (body %q)", what, r.Status, r.Body)
	}
}

// t17WantFormError checks a 400 or 422 answer that is not the not-found page.
func t17WantFormError(t *testing.T, what string, r webtest.Response, leaks ...string) {
	t.Helper()
	if r.Status != http.StatusBadRequest && r.Status != http.StatusUnprocessableEntity {
		t.Fatalf("%s: status = %d, want 400 or 422 (body %q)", what, r.Status, r.Body)
	}
	for _, l := range append([]string{auth.MsgNotFound}, leaks...) {
		if strings.Contains(r.Body, l) {
			t.Fatalf("%s: body %q contains %q", what, r.Body, l)
		}
	}
}

// AUTH-059
func TestUser_EmailChangeKeepsIdentity(t *testing.T) {
	w := authtest.NewWorld(t)
	h := webtest.New(t, w, webtest.WithProbe(), webtest.WithRoutes(func(r chi.Router, s *authweb.Server) {
		r.Method(http.MethodGet, "/", s.Self(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			_, _ = rw.Write([]byte("HOME"))
		})))
	}))
	const newEmail = "priya.shah@vardhan.test"
	priyaID := w.Users[t10Priya]
	if priyaID != 4 {
		t.Fatalf("priya's user id = %d, want 4 (fixture)", priyaID)
	}
	po := t14PO(t, h, webtest.Doc{Number: "PO/26-27/00001"})

	earlier := signedIn(t, h, t10Priya)
	wantStatus(t, "priya's earlier client GET / before", earlier.HTMX().Get("/"), http.StatusOK)

	admin := signedIn(t, h, t13Admin)
	v := t13PageVersion(t, admin, w, t10Priya)
	r := admin.Post(t13UserPath(w, t10Priya, ""), t13With(t13Form(t, w, t10Priya, url.Values{"email": {newEmail}}), v))
	wantStatus(t, "admin changes priya's email", r, http.StatusSeeOther)

	if id := t14Int(t, w, `SELECT id FROM users WHERE email = ?`, newEmail); id != 4 {
		t.Fatalf("user id of %s = %d, want 4", newEmail, id)
	}
	if n := t14Int(t, w, `SELECT count(*) FROM users WHERE email = ?`, t10Priya); n != 0 {
		t.Fatalf("users with the old email = %d, want 0", n)
	}
	if by := t14Int(t, w, `SELECT created_by FROM probe_order WHERE id = ?`, po); by != 4 {
		t.Fatalf("PO created_by = %d, want 4", by)
	}

	r = earlier.HTMX().Get("/")
	wantStatus(t, "priya's earlier client after the change", r, http.StatusUnauthorized)
	wantBody(t, "priya's earlier client after the change", r, "Your session has expired. Sign in again.")

	c := h.Client()
	r = t11SignIn(t, c, newEmail, t10Password, "")
	wantRedirect(t, "sign in with the new email", r, http.StatusSeeOther, "/")
	if n := t14Int(t, w, `SELECT count(*) FROM sessions WHERE user_id = 4`); n != 1 {
		t.Fatalf("sessions of user 4 after signing in with the new email = %d, want 1", n)
	}
	wantBody(t, "new client GET /", c.Get("/"), "HOME")
	r = c.Get("/po/" + t14ID(po))
	wantStatus(t, "new client GET the PO", r, http.StatusOK)
	wantBody(t, "new client GET the PO", r, "PO/26-27/00001")

	r = t11SignIn(t, h.Client(), t10Priya, t10Password, "")
	wantStatus(t, "sign in with the old email", r, http.StatusUnauthorized)
	wantBody(t, "sign in with the old email", r, t11Invalid)
}

// AUTH-067
func TestMatrix_SeedEditReset(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	ctx := context.Background()

	// Fresh install: the matrix is the Go-defined defaults.
	for _, dt := range doctype.All() {
		if !strings.HasPrefix(dt.Name, "authtest.") {
			continue
		}
		for _, role := range t17Roles {
			if got, want := t17Rights(t, w, role, dt.Name), t17Defaults(dt, role); !reflect.DeepEqual(got, want) {
				t.Errorf("seeded RoleRights(%s, %s) = %v, want %v", role, dt.Name, got, want)
			}
		}
	}

	t14PO(t, h, webtest.Doc{Number: "PO/26-27/00001", State: string(doctype.StateApproved)})
	priya := signedIn(t, h, t10Priya)
	r := priya.Get("/po/export.csv")
	wantStatus(t, "priya export before the edit", r, http.StatusOK)
	wantBody(t, "priya export before the edit", r, "PO/26-27/00001")

	// The admin removes Purchase User → PO → export.
	admin := signedIn(t, h, t13Admin)
	edited := map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll, auth.RightWrite: auth.ScopeAll,
		auth.RightSubmit: auth.ScopeAll, auth.RightPrint: auth.ScopeAll, auth.RightDelete: auth.ScopeOwn,
	}
	form := url.Values{}
	for rt, s := range edited {
		form.Set(string(rt), string(s))
	}
	r = admin.Post(t17RightsPath(t, w, "Purchase User", authtest.DTOrder), form)
	t17WantSaved(t, "admin removes Purchase User PO export", r)
	if got := t17Rights(t, w, "Purchase User", authtest.DTOrder); !reflect.DeepEqual(got, edited) {
		t.Fatalf("RoleRights(Purchase User, PO) after the edit = %v, want %v", got, edited)
	}
	t14Want403(t, "priya export after the edit", priya.Get("/po/export.csv"), t14Deny("export", "Purchase Order"), "PO/26-27/00001")

	// An upgrade adds a DocType: its defaults are seeded, the edited row stays.
	if _, ok := doctype.Lookup(t17NewDoc); !ok {
		doctype.Register(doctype.DocType{
			Name: t17NewDoc, Label: "New Doc", PluralLabel: "New Docs",
			DefaultRights: map[string]map[string]string{
				"Purchase User": {"read": "all", "export": "all"},
				"Stores":        {"read": "all"},
			},
		})
	}
	if err := w.Auth.SeedDefaults(ctx, auth.SeedOptions{}); err != nil {
		t.Fatalf("SeedDefaults after registering %s: %v", t17NewDoc, err)
	}
	newDT, _ := doctype.Lookup(t17NewDoc)
	for _, role := range []string{"Purchase User", "Stores"} {
		if got, want := t17Rights(t, w, role, t17NewDoc), t17Defaults(newDT, role); !reflect.DeepEqual(got, want) {
			t.Errorf("RoleRights(%s, %s) after the upgrade seed = %v, want %v", role, t17NewDoc, got, want)
		}
	}
	if got := t17Rights(t, w, "Purchase User", authtest.DTOrder); !reflect.DeepEqual(got, edited) {
		t.Fatalf("RoleRights(Purchase User, PO) after the upgrade seed = %v, want the edit %v", got, edited)
	}

	// Reset to defaults for Purchase Order only.
	w.Grant(t, "Stores", authtest.DTOrder, auth.RightPrint, auth.ScopeAll)
	grn := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}
	w.SetRights(t, "Stores", authtest.DTReceipt, grn)
	r = admin.Post("/rights/"+authtest.DTOrder+"/reset", url.Values{})
	wantStatus(t, "admin resets PO rights", r, http.StatusOK)
	orderDT, _ := doctype.Lookup(authtest.DTOrder)
	for _, role := range t17Roles {
		if got, want := t17Rights(t, w, role, authtest.DTOrder), t17Defaults(orderDT, role); !reflect.DeepEqual(got, want) {
			t.Errorf("RoleRights(%s, PO) after reset = %v, want the default %v", role, got, want)
		}
	}
	for _, role := range []string{"Purchase User", "Stores"} {
		if c := t17Customised(t, w, role, authtest.DTOrder); c != 0 {
			t.Errorf("role_doctypes(%s, PO).customised after reset = %d, want 0", role, c)
		}
	}
	if got := t17Rights(t, w, "Stores", authtest.DTReceipt); !reflect.DeepEqual(got, grn) {
		t.Errorf("RoleRights(Stores, GRN) after the PO reset = %v, want untouched %v", got, grn)
	}
	r = priya.Get("/po/export.csv")
	wantStatus(t, "priya export after reset", r, http.StatusOK)
	wantBody(t, "priya export after reset", r, "PO/26-27/00001")
}

// AUTH-068
func TestMatrix_ChangesAudited(t *testing.T) {
	w := authtest.NewWorld(t)
	h := webtest.New(t, w)
	admin := signedIn(t, h, t13Admin)
	anil := w.P(t, t13Admin)
	if anil.Name != "Anil Kapoor" {
		t.Fatalf("admin name = %q, want Anil Kapoor", anil.Name)
	}
	v := t13PageVersion(t, admin, w, t13Suresh)
	n0 := len(w.Audit.Entries())

	r := admin.Post(t17RightsPath(t, w, "Stores", authtest.DTOrder), url.Values{"read": {"all"}, "print": {"all"}})
	t17WantSaved(t, "admin grants Stores PO print", r)
	r = admin.Post(t13UserPath(w, t13Suresh, "/restrictions"), t13With(url.Values{
		"dimension": {"site"}, "add": {t14ID(w.Sites["TECH-SITE"])},
	}, v))
	wantStatus(t, "admin adds TECH-SITE to suresh", r, http.StatusSeeOther)

	es := w.Audit.Entries()[n0:]
	if len(es) != 2 {
		t.Fatalf("audit entries written = %d (%+v), want 2", len(es), es)
	}
	want := []audit.Entry{
		{
			DocType: "Role", DocID: t17RoleID(t, w, "Stores"), Action: "rights", By: anil.ID, At: authtest.T0,
			Changes: []audit.Change{{Field: "authtest.Order.print", Before: "none", After: "all", Op: "set"}},
		},
		{
			DocType: "User", DocID: int64(w.Users[t13Suresh]), Action: "update", By: anil.ID, At: authtest.T0,
			Changes: []audit.Change{{Field: "sites", Before: "[PUNE-WH, METRO-SITE]", After: "[PUNE-WH, METRO-SITE, TECH-SITE]", Op: "set"}},
		},
	}
	for i := range es {
		if got := auth.FormatTime(es[i].At); got != "2026-10-24T05:00:00.000Z" {
			t.Errorf("entry %d At = %s, want 2026-10-24T05:00:00.000Z", i, got)
		}
		es[i].At = authtest.T0
	}
	if !reflect.DeepEqual(es, want) {
		t.Errorf("audit entries =\n %+v\nwant\n %+v", es, want)
	}
}

// Carry-over: a rights key outside the 11 known rights is a clean 400/422,
// never the database's CHECK error, and changes nothing.
func TestMatrix_UnknownRightRejected(t *testing.T) {
	w := authtest.NewWorld(t)
	h := webtest.New(t, w)
	admin := signedIn(t, h, t13Admin)
	path := t17RightsPath(t, w, "Stores", authtest.DTOrder)
	before := t17Rights(t, w, "Stores", authtest.DTOrder)

	r := admin.Post(path, url.Values{"read": {"all"}, "fly": {"all"}})
	t17WantFormError(t, "rights form with key fly", r, "CHECK", "constraint", "sqlite", "SQL")
	if got := t17Rights(t, w, "Stores", authtest.DTOrder); !reflect.DeepEqual(got, before) {
		t.Fatalf("RoleRights(Stores, PO) after the refused save = %v, want unchanged %v", got, before)
	}

	r = admin.Post(path, url.Values{"read": {"all"}, "print": {"all"}})
	t17WantSaved(t, "rights form with known keys", r)
	if got, want := t17Rights(t, w, "Stores", authtest.DTOrder), (map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightPrint: auth.ScopeAll}); !reflect.DeepEqual(got, want) {
		t.Fatalf("RoleRights(Stores, PO) after a valid save = %v, want %v", got, want)
	}
}

// Carry-over: an unknown role name on user create or update is a form error
// (400/422), not the 404 page, and changes nothing.
func TestUsers_UnknownRoleIsFormError(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	admin := signedIn(t, h, t13Admin)

	r := admin.Post("/users", url.Values{
		"email": {"kiran@vardhan.test"}, "name": {"Kiran Joshi"}, "password": {"Granite-River-4417"},
		"roles": {"Employee", "Auditor"},
	})
	t17WantFormError(t, "create with role Auditor", r)
	if n := t14Int(t, w, `SELECT count(*) FROM users WHERE email = 'kiran@vardhan.test'`); n != 0 {
		t.Fatalf("users kiran after the refused create = %d, want 0", n)
	}

	before := w.P(t, t10Priya).Roles
	r = admin.Post(t13UserPath(w, t10Priya, "/roles"), url.Values{
		"roles": {"Employee", "Auditor"}, "version": {t13PageVersion(t, admin, w, t10Priya)},
	})
	t17WantFormError(t, "POST /users/{priya}/roles with role Auditor", r)
	if got := w.P(t, t10Priya).Roles; !reflect.DeepEqual(got, before) {
		t.Fatalf("priya roles after the refused roles save = %q, want %q", got, before)
	}

	v := t13PageVersion(t, admin, w, t10Priya)
	r = admin.Post(t13UserPath(w, t10Priya, ""), t13With(t13Form(t, w, t10Priya, url.Values{"roles": {"Employee", "Auditor"}}), v))
	t17WantFormError(t, "POST /users/{priya} with role Auditor", r)
	if got := w.P(t, t10Priya).Roles; !reflect.DeepEqual(got, before) {
		t.Fatalf("priya roles after the refused profile save = %q, want %q", got, before)
	}
}
