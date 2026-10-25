package authweb_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	t13BadForm = "The form could not be read. Reload the page and try again."
	// t13NoRolesWarning is NoRolesWarning (auth/messages.go) with %s = name.
	t13NoRolesWarning = "%s has no roles and will see nothing after signing in."
)

// t13Changed is UserChangedError (auth/messages.go) for name.
func t13Changed(name string) string {
	return "Someone else changed " + name + " after you opened it. Reload it and make your changes again."
}

func t13UserExists(t *testing.T, w *authtest.World, email string) bool {
	t.Helper()
	var n int
	t13Read(t, w, "count user "+email, func(ctx context.Context, q db.DBTX) error {
		return q.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = ?`, email).Scan(&n)
	})
	return n == 1
}

// AD-1: every user-admin write without the page's version is refused with 400
// and changes nothing.
func TestUserWrites_MissingVersion400(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	admin := signedIn(t, h, t13Admin)

	userBefore := t13User(t, w, t10Priya)
	rolesBefore := w.P(t, t10Priya).Roles
	restrBefore := t13Restrictions(t, w, t10Priya)
	versionBefore := t13DBVersion(t, w, t10Priya)

	for _, tc := range []struct {
		name, path string
		form       url.Values
	}{
		{"profile", t13UserPath(w, t10Priya, ""), url.Values{"name": {"Priya Changed"}, "mobile": {userBefore.Mobile}}},
		{"roles", t13UserPath(w, t10Priya, "/roles"), url.Values{"roles": {"Employee"}}},
		{"restrictions", t13UserPath(w, t10Priya, "/restrictions"),
			url.Values{"dimension": {"site"}, "add": {strconv.FormatInt(w.Sites["PUNE-WH"], 10)}}},
		{"delete", t13UserPath(w, t10Deepak, "/delete"), url.Values{}},
		{"non-numeric version", t13UserPath(w, t10Priya, ""),
			url.Values{"name": {"Priya Changed"}, "mobile": {userBefore.Mobile}, "version": {"abc"}}},
	} {
		r := admin.Post(tc.path, tc.form)
		wantStatus(t, tc.name+" without version", r, http.StatusBadRequest)
		wantBody(t, tc.name+" without version", r, t13BadForm)
	}

	if got := t13User(t, w, t10Priya); got != userBefore {
		t.Errorf("priya after unversioned writes: %+v, want unchanged %+v", got, userBefore)
	}
	if got := w.P(t, t10Priya).Roles; !reflect.DeepEqual(got, rolesBefore) {
		t.Errorf("priya roles after unversioned writes = %q, want %q", got, rolesBefore)
	}
	if got := t13Restrictions(t, w, t10Priya); !reflect.DeepEqual(got, restrBefore) {
		t.Errorf("priya restrictions after unversioned writes = %q, want %q", got, restrBefore)
	}
	if got := t13DBVersion(t, w, t10Priya); got != versionBefore {
		t.Errorf("priya version after unversioned writes = %s, want %s", got, versionBefore)
	}
	if !t13UserExists(t, w, t10Deepak) {
		t.Errorf("deepak deleted by an unversioned delete")
	}
}

// AD-1: a second save from the same page version is refused with 409 and
// the first save is kept.
func TestUser_StaleVersion409(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	admin := signedIn(t, h, t13Admin)
	path := t13UserPath(w, t10Priya, "")
	mobile := t13User(t, w, t10Priya).Mobile

	v := t13PageVersion(t, admin, w, t10Priya)
	r := admin.Post(path, url.Values{"name": {"Priya First"}, "mobile": {mobile}, "version": {v}})
	wantStatus(t, "first save", r, http.StatusSeeOther)

	r = admin.Post(path, url.Values{"name": {"Priya Second"}, "mobile": {mobile}, "version": {v}})
	wantStatus(t, "second save at the same version", r, http.StatusConflict)
	wantBody(t, "second save at the same version", r, t13Changed("Priya First"))

	if got := t13User(t, w, t10Priya); got.Name != "Priya First" {
		t.Fatalf("priya name = %q after the stale save, want the first save's %q", got.Name, "Priya First")
	}
}

// AD-1: a restrictions change bumps the user's version, so a profile save
// from the page opened before it gets 409.
func TestRestrictions_BumpVersion(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	admin := signedIn(t, h, t13Admin)
	before := t13User(t, w, t10Priya)

	v := t13PageVersion(t, admin, w, t10Priya)
	r := admin.Post(t13UserPath(w, t10Priya, "/restrictions"), url.Values{
		"dimension": {"site"}, "add": {strconv.FormatInt(w.Sites["PUNE-WH"], 10)}, "version": {v},
	})
	wantStatus(t, "admin adds PUNE-WH to priya", r, http.StatusSeeOther)
	if got := t13DBVersion(t, w, t10Priya); got == v {
		t.Fatalf("priya version after a restrictions change = %s, want it bumped from %s", got, v)
	}

	r = admin.Post(t13UserPath(w, t10Priya, ""), url.Values{
		"name": {before.Name}, "mobile": {"+91 98200 77777"}, "version": {v},
	})
	wantStatus(t, "profile save at the pre-restrictions version", r, http.StatusConflict)
	wantBody(t, "profile save at the pre-restrictions version", r, t13Changed(before.Name))
	if got := t13User(t, w, t10Priya); got != before {
		t.Fatalf("priya after the stale profile save: %+v, want unchanged %+v", got, before)
	}
}

// CD-80: creating a user with no roles flashes NoRolesWarning onto the next
// page. No role ticked means the form carries no roles field.
func TestCreateUser_NoRolesFlash(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	admin := signedIn(t, h, t13Admin)

	const name = "Neha Rao"
	r := admin.Post("/users", url.Values{
		"email": {"neha@vardhan.test"}, "name": {name},
		"mobile": {"+91 98200 55555"}, "password": {t10Password},
	})
	wantStatus(t, "admin creates neha without roles", r, http.StatusSeeOther)
	loc := r.Header.Get("Location")
	if loc == "" {
		t.Fatalf("create user: 303 without Location")
	}

	r = admin.Get(loc)
	wantStatus(t, "GET "+loc+" after create", r, http.StatusOK)
	wantBody(t, "GET "+loc+" after create", r, fmt.Sprintf(t13NoRolesWarning, name))
}

// A roles post whose body is multipart or empty, with the CSRF token in the
// header, is refused with 400 and leaves the roles as they were.
func TestRoles_UnreadableBody400(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	admin := signedIn(t, h, t13Admin)
	tok := csrfOf(t, admin)
	path := t13UserPath(w, t10Priya, "/roles")
	hc := *h.HTTP.Client()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	rolesBefore := w.P(t, t10Priya).Roles

	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	for k, v := range map[string]string{"roles": "Employee", "version": t13PageVersion(t, admin, w, t10Priya)} {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, contentType string
		body              []byte
	}{
		{"multipart", mw.FormDataContentType(), mp.Bytes()},
		{"empty", "application/x-www-form-urlencoded", nil},
	} {
		req, err := http.NewRequest(http.MethodPost, h.HTTP.URL+path, bytes.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", tc.contentType)
		req.Header.Set("X-CSRF-Token", tok)
		req.AddCookie(admin.Cookie())
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s roles body: status = %d, want 400 (body %q)", tc.name, resp.StatusCode, body)
		}
		if got := w.P(t, t10Priya).Roles; !reflect.DeepEqual(got, rolesBefore) {
			t.Fatalf("%s roles body: priya roles = %q, want unchanged %q", tc.name, got, rolesBefore)
		}
	}
}
