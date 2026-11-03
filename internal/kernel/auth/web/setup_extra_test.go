package authweb_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
)

// After setup, a POST /setup with a weak password gets 409 setup-done, not
// the password policy's 422.
func TestSetup_DoneBeforePolicy(t *testing.T) {
	w := t11EmptyWorld(t)
	h := webtest.New(t, w)
	if _, err := w.Auth.CreateFirstAdmin(context.Background(), "admin@vardhan.test", "Anil Kapoor", t10Password); err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}

	c := h.Client()
	page := c.Get("/signin") // the guest session and its token
	wantStatus(t, "GET /signin", page, http.StatusOK)
	r := c.NoCSRF().Post("/setup", url.Values{
		"email": {"meena@vardhan.test"}, "name": {"Meena Iyer"}, "password": {"short"},
		"csrf_token": {t11FormCSRF(t, "GET /signin", page)},
	})
	wantStatus(t, "POST /setup after setup", r, http.StatusConflict)
	if !strings.Contains(r.Body, auth.ErrSetupDone.Msg) {
		t.Errorf("body %q, want %q", r.Body, auth.ErrSetupDone.Msg)
	}
	if n := t11Int(t, w, `SELECT count(*) FROM users`); n != 1 {
		t.Errorf("users = %d, want 1", n)
	}
}

// A blank first-administrator name stays on the form, on the Name field, with
// the email kept and nobody created.
func TestSetup_BlankNameRefused(t *testing.T) {
	w := t11EmptyWorld(t)
	h := webtest.New(t, w)
	c := h.Client()
	page := c.Get("/setup")
	wantStatus(t, "GET /setup", page, http.StatusOK)
	r := c.NoCSRF().Post("/setup", url.Values{
		"email": {"admin@vardhan.test"}, "name": {"   "}, "password": {t10Password},
		"csrf_token": {t11FormCSRF(t, "GET /setup", page)},
	})
	wantStatus(t, "POST /setup with a blank name", r, http.StatusBadRequest)
	doc := p1Doc(t, "blank name", r)
	if got := doc.Find(".error-summary li a").AttrOr("href", ""); got != "#f-name" {
		t.Errorf("summary link = %q, want #f-name", got)
	}
	if got := p1Text(doc.Find("#f-name-msg")); !strings.Contains(got, auth.ErrNameRequired.Msg) {
		t.Errorf("name message = %q", got)
	}
	if got := doc.Find(`input[name=email]`).AttrOr("value", ""); got != "admin@vardhan.test" {
		t.Errorf("email keeps %q", got)
	}
	if n := t11Int(t, w, `SELECT count(*) FROM users`); n != 0 {
		t.Errorf("users = %d, want 0", n)
	}
}
