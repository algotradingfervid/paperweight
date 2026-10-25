package authweb_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	t13Admin  = "admin@vardhan.test"
	t13Suresh = "suresh@vardhan.test"

	t13AdminOnlyField  = "Only an administrator can change this."
	t13AdminOnlyRoles  = "Only an administrator can change roles."
	t13AdminOnlyAccess = "Only an administrator can change site and project access."
)

// t13Harness mounts the Task 13 routes (Srv.Mount) plus the stubs these
// tests need: GET /po/list (Right read) → "PO LIST" and GET / (Self) →
// "HOME". It does not stub /users, which Mount owns from Task 13 on.
func t13Harness(t *testing.T, w *authtest.World) *webtest.Harness {
	t.Helper()
	text := func(s string) http.HandlerFunc {
		return func(rw http.ResponseWriter, _ *http.Request) { _, _ = rw.Write([]byte(s)) }
	}
	return webtest.New(t, w, webtest.WithRoutes(func(r chi.Router, s *authweb.Server) {
		r.Method(http.MethodGet, "/po/list", s.Right(authtest.DTOrder, auth.RightRead, text("PO LIST")))
		r.Method(http.MethodGet, "/", s.Self(text("HOME")))
	}))
}

func t13UserPath(w *authtest.World, email, suffix string) string {
	return fmt.Sprintf("/users/%d%s", int64(w.Users[email]), suffix)
}

func t13Read(t *testing.T, w *authtest.World, what string, fn func(ctx context.Context, q db.DBTX) error) {
	t.Helper()
	ctx := context.Background()
	if err := w.DB.Read(ctx, func(tx *db.Tx) error { return fn(ctx, tx.SQL()) }); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

type t13Row struct {
	Email, Name, Mobile string
	Enabled             int64
}

func t13User(t *testing.T, w *authtest.World, email string) t13Row {
	t.Helper()
	var u t13Row
	t13Read(t, w, "read user "+email, func(ctx context.Context, q db.DBTX) error {
		return q.QueryRowContext(ctx, `SELECT email, name, mobile, enabled FROM users WHERE id = ?`,
			int64(w.Users[email])).Scan(&u.Email, &u.Name, &u.Mobile, &u.Enabled)
	})
	return u
}

func t13Sessions(t *testing.T, w *authtest.World, email string) int {
	t.Helper()
	var n int
	t13Read(t, w, "count sessions of "+email, func(ctx context.Context, q db.DBTX) error {
		return q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, int64(w.Users[email])).Scan(&n)
	})
	return n
}

// t13Restrictions returns email's restrictions as "dimension:value_id", sorted.
func t13Restrictions(t *testing.T, w *authtest.World, email string) []string {
	t.Helper()
	var out []string
	t13Read(t, w, "read restrictions of "+email, func(ctx context.Context, q db.DBTX) error {
		rows, err := q.QueryContext(ctx,
			`SELECT dimension, value_id FROM user_restrictions WHERE user_id = ? ORDER BY dimension, value_id`,
			int64(w.Users[email]))
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var dim string
			var id int64
			if err := rows.Scan(&dim, &id); err != nil {
				return err
			}
			out = append(out, dim+":"+strconv.FormatInt(id, 10))
		}
		return rows.Err()
	})
	return out
}

// t13Form is the profile form of email as stored: name and mobile (the
// fields POST /users/{id} always reads), plus extra.
func t13Form(t *testing.T, w *authtest.World, email string, extra url.Values) url.Values {
	t.Helper()
	u := t13User(t, w, email)
	f := url.Values{"name": {u.Name}, "mobile": {u.Mobile}}
	for k, vs := range extra {
		f[k] = vs
	}
	return f
}

var (
	t13VersionInput = regexp.MustCompile(`<input[^>]*name="version"[^>]*>`)
	t13ValueAttr    = regexp.MustCompile(`value="([0-9]+)"`)
)

// t13PageVersion GETs /users/{id} of email as c (an admin, or the user
// themselves) and returns the page's hidden version input (CD-94, AD-1).
func t13PageVersion(t *testing.T, c *webtest.Client, w *authtest.World, email string) string {
	t.Helper()
	r := c.Get(t13UserPath(w, email, ""))
	wantStatus(t, "GET user page of "+email, r, http.StatusOK)
	in := t13VersionInput.FindString(r.Body)
	m := t13ValueAttr.FindStringSubmatch(in)
	if m == nil {
		t.Fatalf("GET user page of %s: no hidden version input in %q", email, r.Body)
	}
	return m[1]
}

// t13DBVersion reads users.version of email, for a poster that may not GET
// the page.
func t13DBVersion(t *testing.T, w *authtest.World, email string) string {
	t.Helper()
	var v int64
	t13Read(t, w, "read version of "+email, func(ctx context.Context, q db.DBTX) error {
		return q.QueryRowContext(ctx, `SELECT version FROM users WHERE id = ?`, int64(w.Users[email])).Scan(&v)
	})
	return strconv.FormatInt(v, 10)
}

// t13With returns f with version set.
func t13With(f url.Values, version string) url.Values {
	f.Set("version", version)
	return f
}

func wantBody(t *testing.T, what string, r webtest.Response, want string) {
	t.Helper()
	if !strings.Contains(messageOf(r), want) {
		t.Fatalf("%s: body %q, HX-Trigger %q, want %q", what, r.Body, r.Header.Get("HX-Trigger"), want)
	}
}

// AUTH-042
func TestDisableUser_EndsSessions(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)

	a := signedIn(t, h, t10Deepak)
	b := h.Client()
	b.Login(t10Deepak, t10Password)
	wantStatus(t, "A GET / before", a.Get("/"), http.StatusOK)
	wantStatus(t, "B GET / before", b.Get("/"), http.StatusOK)
	if n := t13Sessions(t, w, t10Deepak); n != 2 {
		t.Fatalf("sessions of deepak before = %d, want 2", n)
	}

	admin := signedIn(t, h, t13Admin)
	v := t13PageVersion(t, admin, w, t10Deepak)
	r := admin.Post(t13UserPath(w, t10Deepak, ""), t13With(t13Form(t, w, t10Deepak, url.Values{"enabled": {"false"}}), v))
	wantStatus(t, "admin disables deepak", r, http.StatusSeeOther)

	if u := t13User(t, w, t10Deepak); u.Enabled != 0 {
		t.Fatalf("deepak enabled = %d after disable, want 0", u.Enabled)
	}
	if n := t13Sessions(t, w, t10Deepak); n != 0 {
		t.Fatalf("sessions of deepak after disable = %d, want 0", n)
	}
	for name, c := range map[string]*webtest.Client{"A": a, "B": b} {
		r := c.HTMX().Get("/")
		wantStatus(t, name+" HTMX GET / after disable", r, http.StatusUnauthorized)
		wantBody(t, name+" HTMX GET / after disable", r, "Your session has expired. Sign in again.")
		if strings.Contains(r.Body, "HOME") {
			t.Fatalf("%s HTMX GET / after disable leaks HOME: %q", name, r.Body)
		}
	}
}

// AUTH-057
func TestUser_SelfEditLimitedFields(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	priya := signedIn(t, h, t10Priya)
	self := t13UserPath(w, t10Priya, "")

	const mobile = "+91 98200 12345"
	r := priya.Post(self, url.Values{"name": {"Priya Shah"}, "mobile": {mobile}, "version": {t13PageVersion(t, priya, w, t10Priya)}})
	wantStatus(t, "priya edits own mobile", r, http.StatusSeeOther)
	before := t13User(t, w, t10Priya)
	if before.Mobile != mobile || before.Name != "Priya Shah" {
		t.Fatalf("priya after own edit: name %q mobile %q, want Priya Shah / %q", before.Name, before.Mobile, mobile)
	}
	rolesBefore := w.P(t, t10Priya).Roles
	if !reflect.DeepEqual(rolesBefore, []string{"Employee", "Purchase User"}) {
		t.Fatalf("priya roles = %q, want the seed's [Employee Purchase User]", rolesBefore)
	}
	restrBefore := t13Restrictions(t, w, t10Priya)
	wantRestr := []string{"project:" + strconv.FormatInt(w.Projects["PRJ-METRO"], 10)}
	if !reflect.DeepEqual(restrBefore, wantRestr) {
		t.Fatalf("priya restrictions = %q, want %q", restrBefore, wantRestr)
	}

	for _, tc := range []struct {
		name  string
		extra url.Values
	}{
		{"email", url.Values{"email": {"x@vardhan.test"}}},
		{"roles", url.Values{"roles": {"Director"}}},
		{"enabled", url.Values{"enabled": {"false"}}},
		{"sites", url.Values{"sites": {strconv.FormatInt(w.Sites["PUNE-WH"], 10)}}},
	} {
		form := url.Values{"name": {"Priya Shah"}, "mobile": {mobile}, "version": {t13PageVersion(t, priya, w, t10Priya)}}
		for k, vs := range tc.extra {
			form[k] = vs
		}
		r := priya.Post(self, form)
		wantStatus(t, "priya changes own "+tc.name, r, http.StatusForbidden)
		wantBody(t, "priya changes own "+tc.name, r, t13AdminOnlyField)

		if got := t13User(t, w, t10Priya); got != before {
			t.Fatalf("after %s attempt: user %+v, want unchanged %+v", tc.name, got, before)
		}
		if got := w.P(t, t10Priya).Roles; !reflect.DeepEqual(got, rolesBefore) {
			t.Fatalf("after %s attempt: roles %q, want %q", tc.name, got, rolesBefore)
		}
		if got := t13Restrictions(t, w, t10Priya); !reflect.DeepEqual(got, restrBefore) {
			t.Fatalf("after %s attempt: restrictions %q, want %q", tc.name, got, restrBefore)
		}
	}

	raviBefore := t13User(t, w, t10Ravi)
	r = priya.Post(t13UserPath(w, t10Ravi, ""), url.Values{"name": {raviBefore.Name}, "mobile": {mobile}, "version": {t13DBVersion(t, w, t10Ravi)}})
	wantStatus(t, "priya edits ravi's mobile", r, http.StatusForbidden)
	if got := t13User(t, w, t10Ravi); got != raviBefore {
		t.Fatalf("ravi after priya's edit: %+v, want unchanged %+v", got, raviBefore)
	}
}

// AUTH-058
func TestRoles_OnlyAdminAssigns(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	ravi := signedIn(t, h, t10Ravi)

	raviRoles := w.P(t, t10Ravi).Roles
	priyaRoles := w.P(t, t10Priya).Roles
	if !reflect.DeepEqual(raviRoles, []string{"Employee", "Purchase Manager"}) ||
		!reflect.DeepEqual(priyaRoles, []string{"Employee", "Purchase User"}) {
		t.Fatalf("seed roles: ravi %q, priya %q", raviRoles, priyaRoles)
	}

	for _, tc := range []struct {
		name, email string
		roles       []string
	}{
		{"ravi adds Administrator to ravi", t10Ravi, append(append([]string(nil), raviRoles...), "Administrator")},
		{"ravi adds Purchase Manager to priya", t10Priya, append(append([]string(nil), priyaRoles...), "Purchase Manager")},
	} {
		r := ravi.Post(t13UserPath(w, tc.email, "/roles"), url.Values{"roles": tc.roles, "version": {t13DBVersion(t, w, tc.email)}})
		wantStatus(t, tc.name, r, http.StatusForbidden)
		wantBody(t, tc.name, r, t13AdminOnlyRoles)
	}

	if got := w.P(t, t10Ravi).Roles; !reflect.DeepEqual(got, raviRoles) {
		t.Errorf("ravi roles = %q, want unchanged %q", got, raviRoles)
	}
	if got := w.P(t, t10Priya).Roles; !reflect.DeepEqual(got, priyaRoles) {
		t.Errorf("priya roles = %q, want unchanged %q", got, priyaRoles)
	}
}

// AUTH-060
func TestRoles_ChangeAppliesNextRequest(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	priya := signedIn(t, h, t10Priya)
	admin := signedIn(t, h, t13Admin)
	roles := t13UserPath(w, t10Priya, "/roles")

	r := priya.Get("/po/list")
	wantStatus(t, "priya GET /po/list before", r, http.StatusOK)
	wantBody(t, "priya GET /po/list before", r, "PO LIST")

	r = admin.Post(roles, url.Values{"roles": {"Employee"}, "version": {t13PageVersion(t, admin, w, t10Priya)}})
	wantStatus(t, "admin removes Purchase User", r, http.StatusSeeOther)
	if got := w.P(t, t10Priya).Roles; !reflect.DeepEqual(got, []string{"Employee"}) {
		t.Fatalf("priya roles after removal = %q, want [Employee]", got)
	}

	r = priya.Get("/po/list")
	wantStatus(t, "priya GET /po/list after removal", r, http.StatusForbidden)
	wantBody(t, "priya GET /po/list after removal", r, t10DenyReadPO)
	if strings.Contains(r.Body, "PO LIST") {
		t.Fatalf("priya GET /po/list after removal leaks PO LIST: %q", r.Body)
	}

	r = admin.Post(roles, url.Values{"roles": {"Employee", "Purchase User"}, "version": {t13PageVersion(t, admin, w, t10Priya)}})
	wantStatus(t, "admin restores Purchase User", r, http.StatusSeeOther)
	if got := w.P(t, t10Priya).Roles; !reflect.DeepEqual(got, []string{"Employee", "Purchase User"}) {
		t.Fatalf("priya roles after restore = %q, want [Employee Purchase User]", got)
	}

	r = priya.Get("/po/list")
	wantStatus(t, "priya GET /po/list after restore", r, http.StatusOK)
	wantBody(t, "priya GET /po/list after restore", r, "PO LIST")
}

// AUTH-103
func TestRestriction_AdminOnly(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	suresh := signedIn(t, h, t13Suresh)
	path := t13UserPath(w, t13Suresh, "/restrictions")

	before := t13Restrictions(t, w, t13Suresh)
	want := []string{
		"site:" + strconv.FormatInt(w.Sites["PUNE-WH"], 10),
		"site:" + strconv.FormatInt(w.Sites["METRO-SITE"], 10),
	}
	if w.Sites["PUNE-WH"] > w.Sites["METRO-SITE"] {
		want[0], want[1] = want[1], want[0]
	}
	if !reflect.DeepEqual(before, want) {
		t.Fatalf("suresh restrictions = %q, want the seed's %q", before, want)
	}

	for _, tc := range []struct {
		name string
		form url.Values
	}{
		{"add TECH-SITE", url.Values{"dimension": {"site"}, "add": {strconv.FormatInt(w.Sites["TECH-SITE"], 10)}}},
		{"remove PUNE-WH", url.Values{"dimension": {"site"}, "remove": {strconv.FormatInt(w.Sites["PUNE-WH"], 10)}}},
	} {
		r := suresh.Post(path, t13With(tc.form, t13PageVersion(t, suresh, w, t13Suresh)))
		wantStatus(t, "suresh "+tc.name, r, http.StatusForbidden)
		wantBody(t, "suresh "+tc.name, r, t13AdminOnlyAccess)
		if got := t13Restrictions(t, w, t13Suresh); !reflect.DeepEqual(got, before) {
			t.Fatalf("after suresh %s: restrictions %q, want unchanged %q", tc.name, got, before)
		}
	}
}
