package authweb_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// Task 20: the access page takes the flash once; it is gone on the next GET
// (the page is rendered before the session is saved, CD-160).
func TestAccessEditor_FlashShownOnce(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	admin := signedIn(t, h, t13Admin)
	page := t13UserPath(w, t13Suresh, "/access")
	const flash = "Site and project access for Suresh Patil saved."

	r := admin.Post(t13UserPath(w, t13Suresh, "/restrictions"), t13With(url.Values{
		"dimension": {"site"}, "add": {strconv.FormatInt(w.Sites["TECH-SITE"], 10)},
	}, t13DBVersion(t, w, t13Suresh)))
	wantRedirect(t, "admin adds TECH-SITE", r, http.StatusSeeOther, page)
	if body := t20Page(t, admin, "first GET "+page, page); !strings.Contains(body, flash) {
		t.Fatalf("first GET %s: body lacks the flash %q: %q", page, flash, body)
	}
	if body := t20Page(t, admin, "second GET "+page, page); strings.Contains(body, flash) {
		t.Fatalf("second GET %s: the flash %q is shown again: %q", page, flash, body)
	}
}

// Task 20: an Administrator's view of a user links the access page; a user
// viewing themselves (who can't open it) gets no link.
func TestUserPage_AccessLink(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t13Harness(t, w)
	path := t13UserPath(w, t13Suresh, "")
	link := `href="` + t13UserPath(w, t13Suresh, "/access") + `"`

	r := signedIn(t, h, t13Admin).Get(path)
	wantStatus(t, "admin GET "+path, r, http.StatusOK)
	if !strings.Contains(r.Body, link) || !strings.Contains(r.Body, "Site and project access") {
		t.Fatalf("admin GET %s: body lacks the \"Site and project access\" link %s: %q", path, link, r.Body)
	}
	r = signedIn(t, h, t13Suresh).Get(path)
	wantStatus(t, "suresh GET "+path, r, http.StatusOK)
	if strings.Contains(r.Body, link) {
		t.Fatalf("suresh GET %s: body links the access page, which only an administrator may open: %q", path, r.Body)
	}
}
