package authweb_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Task 12 tests. The change-password form posts the fields current, next and
// confirm (ChangeOwnPassword's parameters); the admin reset posts password.
// Sign-in goes through the harness's Client.Login, which calls the same
// auth.SignIn as POST /signin (Task 11): a success answers 200 "SIGNED IN"
// there instead of /signin's 303, and a failure answers through WriteError
// with SignIn's status and message.

const (
	t12New      = "Chai-Break-Ledger-7"
	t12Confirm2 = "Chai-Break-Ledger-8"
	t12Temp     = "Temp-Godown-2026"
	t12Wrong    = "wrong-current-1"
	t12Admin    = "admin@vardhan.test"
	t12Suresh   = "suresh@vardhan.test"
	t12Expired  = "Your session has expired. Sign in again."
	t12Mismatch = "New password and confirmation do not match."
	t12BadCur   = "Current password is incorrect."
	t12NotAdmin = "You don't have permission to change other users' passwords."
	t12Signed   = "SIGNED IN"
)

func t12ChangeForm(current, next, confirm string) url.Values {
	return url.Values{"current": {current}, "next": {next}, "confirm": {confirm}}
}

// t12Hash returns email's stored password_hash.
func t12Hash(t *testing.T, w *authtest.World, email string) string {
	t.Helper()
	var h string
	if err := w.DB.SQL().QueryRowContext(context.Background(),
		`SELECT coalesce(password_hash, '') FROM users WHERE email = ?`, email).Scan(&h); err != nil {
		t.Fatalf("read password_hash of %s: %v", email, err)
	}
	return h
}

func t12MustChange(t *testing.T, w *authtest.World, email string) int {
	t.Helper()
	return t11Int(t, w, `SELECT must_change_password FROM users WHERE email = ?`, email)
}

// t12Changed asserts a successful POST /account/password: a 303 (CD-63) or a
// 200 page, never an error.
func t12Changed(t *testing.T, what string, r webtest.Response) {
	t.Helper()
	if r.Status != http.StatusSeeOther && r.Status != http.StatusOK {
		t.Fatalf("%s: status = %d, want 303 or 200 (body %q)", what, r.Status, r.Body)
	}
	if loc := r.Header.Get("Location"); strings.HasPrefix(loc, "/signin") {
		t.Fatalf("%s: Location = %q: the changing session was signed out", what, loc)
	}
}

func t12LoginOK(t *testing.T, what string, r webtest.Response) {
	t.Helper()
	wantStatus(t, what, r, http.StatusOK)
	if !strings.Contains(r.Body, t12Signed) {
		t.Fatalf("%s: body = %q, want %q", what, r.Body, t12Signed)
	}
}

func t12WantMsg(t *testing.T, what string, r webtest.Response, status int, msg string) {
	t.Helper()
	wantStatus(t, what, r, status)
	if !strings.Contains(messageOf(r), msg) {
		t.Fatalf("%s: body %q, HX-Trigger %q, want %q", what, r.Body, r.Header.Get("HX-Trigger"), msg)
	}
}

// AUTH-022
func TestPassword_OldPasswordRejectedAfterChange(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)

	page := c.Get("/account/password")
	wantStatus(t, "GET /account/password", page, http.StatusOK)
	r := c.Post("/account/password", t12ChangeForm(t10Password, t12New, t12New))
	t12Changed(t, "POST /account/password", r)

	old := h.Client().Login(t10Priya, t10Password)
	t12WantMsg(t, "sign in with the old password", old, http.StatusUnauthorized, t11Invalid)
	if strings.Contains(old.Body, t12Signed) {
		t.Fatalf("sign in with the old password succeeded: %q", old.Body)
	}

	t12LoginOK(t, "sign in with the new password", h.Client().Login(t10Priya, t12New))
}

// AUTH-030
func TestChangePassword_RequiresCurrent(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)
	before := t12Hash(t, w, t10Priya)

	r := c.Post("/account/password", t12ChangeForm(t12Wrong, t12New, t12New))
	t12WantMsg(t, "POST /account/password with a wrong current password", r, http.StatusBadRequest, t12BadCur)

	if got := t12Hash(t, w, t10Priya); got != before {
		t.Errorf("password_hash changed after a wrong current password")
	}
	if n := t11Int(t, w, `SELECT coalesce(sum(count), 0) FROM login_attempts WHERE key = ?`, auth.AccountKey(t10Priya)); n != 1 {
		t.Errorf("login_attempts count for AccountKey(priya) = %d, want 1", n)
	}
	t12LoginOK(t, "sign in with the unchanged password", h.Client().Login(t10Priya, t10Password))
}

// AUTH-031
func TestChangePassword_ConfirmMismatch(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)
	before := t12Hash(t, w, t10Priya)

	r := c.Post("/account/password", t12ChangeForm(t10Password, t12New, t12Confirm2))
	t12WantMsg(t, "POST /account/password with a mismatched confirmation", r, http.StatusBadRequest, t12Mismatch)

	if got := t12Hash(t, w, t10Priya); got != before {
		t.Errorf("password_hash changed after a mismatched confirmation")
	}
}

// AUTH-032
func TestChangePassword_EndsOtherSessions(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	a := signedIn(t, h, t10Priya)
	b := h.Client()
	t12LoginOK(t, "session B sign-in", b.Login(t10Priya, t10Password))
	wantStatus(t, "B GET / before the change", b.Get("/"), http.StatusOK)

	r := a.Post("/account/password", t12ChangeForm(t10Password, t12New, t12New))
	t12Changed(t, "A POST /account/password", r)

	home := a.Get("/")
	wantStatus(t, "A GET / after the change", home, http.StatusOK)
	if !strings.Contains(home.Body, "HOME") {
		t.Fatalf("A GET / body = %q, want HOME", home.Body)
	}

	t12WantMsg(t, "B HTMX GET / after the change", b.HTMX().Get("/"), http.StatusUnauthorized, t12Expired)
	wantRedirect(t, "B GET / after the change", b.Get("/"), http.StatusSeeOther, "/signin?next=%2F")

	if n := t11Int(t, w, `SELECT count(*) FROM sessions WHERE user_id = ?`, int64(w.Users[t10Priya])); n != 1 {
		t.Errorf("priya's sessions rows after the change = %d, want 1 (A only)", n)
	}
}

// AUTH-033  (level integration: an in-process httptest flow, so it runs in make test)
func TestAdminResetPassword_ForcesChange(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	s := signedIn(t, h, t12Suresh)
	wantStatus(t, "S GET / before the reset", s.Get("/"), http.StatusOK)
	admin := signedIn(t, h, t12Admin)
	sureshID := int64(w.Users[t12Suresh])

	reset := admin.Post(fmt.Sprintf("/users/%d/password", sureshID), url.Values{"password": {t12Temp}})
	wantStatus(t, "admin POST /users/{suresh}/password", reset, http.StatusSeeOther)
	if got := t12MustChange(t, w, t12Suresh); got != 1 {
		t.Fatalf("suresh must_change_password = %d after the reset, want 1", got)
	}
	if n := t11Int(t, w, `SELECT count(*) FROM sessions WHERE user_id = ?`, sureshID); n != 0 {
		t.Fatalf("suresh's sessions rows after the reset = %d, want 0", n)
	}

	t12WantMsg(t, "S HTMX GET / after the reset", s.HTMX().Get("/"), http.StatusUnauthorized, t12Expired)

	c := h.Client()
	t12LoginOK(t, "suresh signs in with the temporary password", c.Login(t12Suresh, t12Temp))
	wantRedirect(t, "GET / during the forced change", c.Get("/"), http.StatusSeeOther, "/account/password")
	wantRedirect(t, "GET /po/list during the forced change", c.Get("/po/list"), http.StatusSeeOther, "/account/password")

	page := c.Get("/account/password")
	wantStatus(t, "GET /account/password during the forced change", page, http.StatusOK)
	t11FormCSRF(t, "GET /account/password", page)

	r := c.Post("/account/password", t12ChangeForm(t12Temp, t12New, t12New))
	t12Changed(t, "POST /account/password during the forced change", r)
	if got := t12MustChange(t, w, t12Suresh); got != 0 {
		t.Fatalf("suresh must_change_password = %d after the change, want 0", got)
	}

	home := c.Get("/")
	wantStatus(t, "GET / after the change", home, http.StatusOK)
	if !strings.Contains(home.Body, "HOME") {
		t.Fatalf("GET / after the change body = %q, want HOME", home.Body)
	}
	t12LoginOK(t, "suresh signs in with the new password", h.Client().Login(t12Suresh, t12New))
}

// AUTH-034
func TestAdminResetPassword_AdminOnly(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t12Suresh, t10Password)
	priya := signedIn(t, h, t10Priya)
	before := t12Hash(t, w, t12Suresh)

	r := priya.Post(fmt.Sprintf("/users/%d/password", int64(w.Users[t12Suresh])), url.Values{"password": {t12Temp}})
	t12WantMsg(t, "priya POST /users/{suresh}/password", r, http.StatusForbidden, t12NotAdmin)

	if got := t12Hash(t, w, t12Suresh); got != before {
		t.Errorf("suresh's password_hash changed after priya's reset")
	}
	if got := t12MustChange(t, w, t12Suresh); got != 0 {
		t.Errorf("suresh must_change_password = %d after priya's reset, want 0", got)
	}
	t12LoginOK(t, "suresh signs in with the unchanged password", h.Client().Login(t12Suresh, t10Password))
}

// A corrupt stored hash makes ChangeOwnPassword return a plain error (not an
// HTTPError); the handler answers 500 with WP0's generic page and never shows
// the hash or the parse error (Task 7 carry-over).
func TestChangePassword_CorruptStoredHashIsInternalError(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)
	const corrupt = "$argon2id$v=19$m=19456,t=2,p=1$CORRUPTSALT$CORRUPTKEY"
	if err := w.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), `UPDATE users SET password_hash = ? WHERE email = ?`, corrupt, t10Priya)
		return err
	}); err != nil {
		t.Fatalf("corrupt priya's hash: %v", err)
	}

	r := c.Post("/account/password", t12ChangeForm(t10Password, t12New, t12New))
	t12WantMsg(t, "POST /account/password over a corrupt hash", r, http.StatusInternalServerError, t10InternalMsg)
	for _, leak := range []string{"CORRUPTSALT", "CORRUPTKEY", "argon2id", "malformed", "password hash"} {
		if strings.Contains(r.Body, leak) {
			t.Errorf("body leaks %q: %q", leak, r.Body)
		}
	}
	if got := t12Hash(t, w, t10Priya); got != corrupt {
		t.Errorf("password_hash changed over a corrupt hash")
	}
}
