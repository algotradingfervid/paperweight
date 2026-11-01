package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// TestApp_GuardRefusedProblemPage: the admin guard's 403 on a full-page GET
// is the problem page in the shell with the guard's text exactly (UI v2
// §5.14, E6b); htmx keeps the CD-348 notice, a POST its plain page, and a
// guest is still sent to sign in.
func TestApp_GuardRefusedProblemPage(t *testing.T) {
	const (
		path           = "/users"
		problemHeading = "You don't have access to this"
		shellLink      = `nav[aria-label="Main"] a[href="/approvals"]`
	)
	msg := auth.ErrAdminOnlyUsers.Msg
	w := newWorld(t)
	w.seedUsers()
	b := w.signedIn(deepakEmail)

	r := b.get(path)
	h1 := strings.TrimSpace(r.doc.Find("h1").First().Text())
	body := r.doc.Find(".problem-message").Text()
	if r.status != http.StatusForbidden || h1 != problemHeading || body != msg {
		t.Errorf("non-admin GET %s = %d h1 %q body %q, want 403 %q with %q", path, r.status, h1, body, problemHeading, msg)
	}
	if r.doc.Find(shellLink).Length() == 0 {
		t.Errorf("non-admin GET %s has no shell navigation", path)
	}

	hx := b.do(http.MethodGet, path, nil, "", map[string]string{"HX-Request": "true"})
	if got := noticeText(t, hx.header.Get("HX-Trigger")); hx.status != http.StatusForbidden || got != msg || hx.body != "" {
		t.Errorf("htmx GET %s = %d notice %q body %q, want 403 notice %q, no body", path, hx.status, got, hx.body, msg)
	}

	post := b.post(path, url.Values{})
	if post.status != http.StatusForbidden || post.alert() != msg || post.doc.Find(".problem-message").Length() != 0 || post.doc.Find(shellLink).Length() != 0 {
		t.Errorf("non-admin POST %s = %d alert %q, want the plain 403 page %q", path, post.status, post.alert(), msg)
	}

	guest := w.browser().get(path)
	if want := "/signin?next=" + url.QueryEscape(path); guest.status != http.StatusSeeOther || guest.location() != want {
		t.Errorf("guest GET %s = %d Location %q, want 303 %q", path, guest.status, guest.location(), want)
	}
}

// problem is the problem page's kernel text, which a full-page GET 403 or 404
// shows in place of the bare page's alert (E6b).
func (r *response) problem() string { return r.doc.Find(".problem-message").Text() }

// TestApp_SecureSetupProblemPage: the public host's closed /setup (CD-228)
// is the 404 problem page with the kernel text, in the shell for a signed-in
// user; a POST keeps the plain page (E6b).
func TestApp_SecureSetupProblemPage(t *testing.T) {
	const (
		problemHeading = "We can't open this page"
		shellLink      = `nav[aria-label="Main"] a[href="/approvals"]`
	)
	w := newWorld(t, func(c *app.Config) { c.Secure = true })
	w.seedUsers()
	isProblem := func(r *response) bool {
		return r.status == http.StatusNotFound && strings.TrimSpace(r.doc.Find("h1").First().Text()) == problemHeading && r.problem() == auth.MsgNotFound
	}

	guest := w.browser()
	if r := guest.get("/setup"); !isProblem(r) || r.doc.Find(shellLink).Length() != 0 {
		t.Errorf("guest GET /setup = %d %q, want the 404 problem page with %q and no navigation", r.status, r.pageText(), auth.MsgNotFound)
	}
	b := w.signedIn(adminEmail)
	if r := b.get("/setup"); !isProblem(r) || r.doc.Find(shellLink).Length() == 0 {
		t.Errorf("signed-in GET /setup = %d %q, want the 404 problem page with %q in the shell", r.status, r.pageText(), auth.MsgNotFound)
	}
	if r := b.post("/setup", url.Values{}); r.status != http.StatusNotFound || r.alert() != auth.MsgNotFound || r.doc.Find(".problem-message").Length() != 0 {
		t.Errorf("POST /setup = %d alert %q, want the plain 404 page %q", r.status, r.alert(), auth.MsgNotFound)
	}
}
