package authweb_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// Task 16 review: the negative control for AUTH-016 over HTTP. A non-admin's
// POST /users/{id}/unlock is refused and the lockout stays.
func TestLockout_NonAdminUnlockRefused(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t10Priya, t10Password)
	priya := w.Users[t10Priya]
	ctx := context.Background()

	for i := 1; i <= auth.AccountLimit; i++ {
		if _, err := w.Auth.SignIn(ctx, auth.SignInInput{Email: t10Priya, Password: t11Wrong, IP: "198.51.100.4", UserAgent: "auth-16"}); err == nil {
			t.Fatalf("wrong password %d signed in", i)
		}
	}
	key := auth.AccountKey(t10Priya)
	if n := t11Int(t, w, `SELECT count(*) FROM login_attempts WHERE key = ?`, key); n != 1 {
		t.Fatalf("login_attempts rows for priya's account before unlock = %d, want 1", n)
	}
	count := t11Int(t, w, `SELECT count FROM login_attempts WHERE key = ?`, key)

	suresh := signedIn(t, h, t12Suresh)
	r := suresh.Post(fmt.Sprintf("/users/%d/unlock", int64(priya)), url.Values{})
	t12WantMsg(t, "suresh POST /users/{priya}/unlock", r, http.StatusForbidden, "Only an administrator can change this.")

	if n := t11Int(t, w, `SELECT count(*) FROM login_attempts WHERE key = ?`, key); n != 1 {
		t.Errorf("login_attempts rows for priya's account after suresh's unlock = %d, want 1", n)
	}
	if got := t11Int(t, w, `SELECT count FROM login_attempts WHERE key = ?`, key); got != count {
		t.Errorf("login_attempts count for priya's account after suresh's unlock = %d, want %d", got, count)
	}
	locked := t11SignIn(t, h.Client(), t10Priya, t10Password, "")
	wantStatus(t, "priya's correct password after suresh's unlock", locked, http.StatusTooManyRequests)
}
