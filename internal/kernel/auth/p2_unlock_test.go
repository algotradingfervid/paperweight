package auth_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// Task 16 review: the negative control for AUTH-016. A non-admin cannot
// clear an account lockout.
func TestUnlock_NonAdminRefused(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	w.SetPassword(t, t13Priya, "Correct-Horse-42")
	w.Clock.Set(authtest.T0)
	priya := w.Users[t13Priya]

	for i := 1; i <= auth.AccountLimit; i++ {
		if _, err := w.Auth.SignIn(ctx, auth.SignInInput{Email: t13Priya, Password: "Wrong-Horse-43", IP: "198.51.100.4", UserAgent: "auth-16"}); err == nil {
			t.Fatalf("wrong password %d signed in", i)
		}
	}
	const attempts = `SELECT key || '|' || count || '|' || first_failure_at || '|' || expires_at FROM login_attempts ORDER BY key`
	before := t7Strings(t, w, attempts)
	if n := t7Count(t, w, `SELECT count(*) FROM login_attempts WHERE key = ?`, auth.AccountKey(t13Priya)); n != 1 {
		t.Fatalf("login_attempts rows for priya's account before unlock = %d, want 1", n)
	}
	audits := len(w.Audit.Entries())

	for _, actor := range []string{t13Suresh, t13Priya} {
		err := w.Auth.Unlock(ctx, w.P(t, actor), priya)
		t7WantProblem(t, actor+" unlocks priya", err, auth.ErrAdminOnlyField)
		if n := t7Count(t, w, `SELECT count(*) FROM login_attempts WHERE key = ?`, auth.AccountKey(t13Priya)); n != 1 {
			t.Errorf("login_attempts rows for priya's account after %s's unlock = %d, want 1", actor, n)
		}
		if got := t7Strings(t, w, attempts); !reflect.DeepEqual(got, before) {
			t.Errorf("login_attempts after %s's unlock = %v, want unchanged %v", actor, got, before)
		}
		if es := w.Audit.Entries(); len(es) != audits {
			t.Errorf("audit entries after %s's unlock = %+v, want %d (none added)", actor, es, audits)
		}
	}
}
