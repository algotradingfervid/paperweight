package auth_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

const (
	t8Password = "Correct-Horse-42"
	t8Wrong    = "Correct-Horse-43"
	t8IP       = "203.0.113.7"
	t8UA       = "Mozilla/5.0 (auth-8 test)"
)

// t8Start is 2026-10-24 10:00:00 IST, the clock start where times matter.
var t8Start = time.Date(2026, 10, 24, 4, 30, 0, 0, time.UTC)

// t8Spy wraps auth.VerifyPassword and records every encoded hash it is given.
type t8Spy struct {
	mu      sync.Mutex
	encoded []string
}

func (s *t8Spy) verify(encoded, password string) (ok, rehash bool, err error) {
	s.mu.Lock()
	s.encoded = append(s.encoded, encoded)
	s.mu.Unlock()
	return auth.VerifyPassword(encoded, password)
}

func (s *t8Spy) calls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.encoded...)
}

func (s *t8Spy) reset() {
	s.mu.Lock()
	s.encoded = nil
	s.mu.Unlock()
}

// t8SpyAuth returns an Auth over w's collaborators whose Deps.Verify is a spy.
func t8SpyAuth(w *authtest.World) (*auth.Auth, *t8Spy) {
	spy := &t8Spy{}
	a := auth.New(auth.Deps{
		DB: w.DB, Clock: w.Clock, Directory: w.Dir, Audit: w.Audit, Refs: w.Refs, Info: w.Info,
		Verify: spy.verify,
	})
	return a, spy
}

func t8SignIn(a *auth.Auth, email, password string) (auth.SignInResult, error) {
	return a.SignIn(context.Background(), auth.SignInInput{Email: email, Password: password, IP: t8IP, UserAgent: t8UA})
}

// t8Count returns the number of rows in table.
func t8Count(t *testing.T, w *authtest.World, table string) int {
	t.Helper()
	var n int
	if err := w.DB.SQL().QueryRowContext(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil { //nolint:gosec // G202: table is a test constant
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// AUTH-002
func TestLogin_EmailCaseAndSpaceInsensitive(t *testing.T) {
	w := authtest.NewWorld(t)
	w.SetPassword(t, "priya@vardhan.test", t8Password)
	got, err := t8SignIn(w.Auth, "  Priya@Vardhan.TEST ", t8Password)
	if err != nil {
		t.Fatalf("SignIn(%q) error: %v", "  Priya@Vardhan.TEST ", err)
	}
	if want := w.Users["priya@vardhan.test"]; got.User != want {
		t.Errorf("SignIn(%q).User = %d, want %d (priya)", "  Priya@Vardhan.TEST ", got.User, want)
	}
}

// AUTH-005
func TestLogin_UnknownUserSpendsHashTime(t *testing.T) {
	w := authtest.NewWorld(t)
	w.AddUser(t, "kiran@vardhan.test", "Kiran Joshi", "Employee") // no password: NULL hash
	a, spy := t8SpyAuth(w)
	for _, email := range []string{"nobody@vardhan.test", "kiran@vardhan.test"} {
		spy.reset()
		_, err := t8SignIn(a, email, t8Password)
		if !errors.Is(err, auth.ErrLoginInvalid) {
			t.Errorf("SignIn(%q) error = %v, want ErrLoginInvalid", email, err)
		}
		calls := spy.calls()
		if len(calls) != 1 {
			t.Errorf("SignIn(%q): Verify called %d times, want exactly 1", email, len(calls))
			continue
		}
		if calls[0] != auth.DummyHash {
			t.Errorf("SignIn(%q): Verify encoded = %q, want auth.DummyHash", email, calls[0])
		}
	}
}

// AUTH-006
func TestLogin_IncompleteDetails(t *testing.T) {
	w := authtest.NewWorld(t)
	w.SetPassword(t, "priya@vardhan.test", t8Password)
	a, spy := t8SpyAuth(w)
	cases := []struct{ email, password string }{
		{"", t8Password},
		{"   ", t8Password},
		{"priya@vardhan.test", ""},
		{"", ""},
	}
	for _, c := range cases {
		_, err := t8SignIn(a, c.email, c.password)
		if !errors.Is(err, auth.ErrLoginIncomplete) {
			t.Errorf("SignIn(%q, %q) error = %v, want ErrLoginIncomplete", c.email, c.password, err)
		}
	}
	if n := len(spy.calls()); n != 0 {
		t.Errorf("Verify called %d times, want 0", n)
	}
	for _, table := range []string{"login_attempts", "auth_log"} {
		if n := t8Count(t, w, table); n != 0 {
			t.Errorf("%s has %d rows, want 0", table, n)
		}
	}
}

// AUTH-008
func TestLogin_DisabledUserWrongPasswordGeneric(t *testing.T) {
	w := authtest.NewWorld(t)
	w.SetPassword(t, "old@vardhan.test", t8Password)
	_, err := t8SignIn(w.Auth, "old@vardhan.test", t8Wrong)
	if !errors.Is(err, auth.ErrLoginInvalid) {
		t.Errorf("SignIn(old, wrong) error = %v, want ErrLoginInvalid", err)
	}
	if errors.Is(err, auth.ErrAccountDisabled) {
		t.Errorf("SignIn(old, wrong) revealed the disabled status: %v", err)
	}
}

// AUTH-017
func TestLogin_OversizedPasswordSameError(t *testing.T) {
	w := authtest.NewWorld(t)
	w.SetPassword(t, "priya@vardhan.test", t8Password)
	a, spy := t8SpyAuth(w)
	long := strings.Repeat("a", 257)
	for _, email := range []string{"priya@vardhan.test", "nobody@vardhan.test"} {
		_, err := t8SignIn(a, email, long)
		if !errors.Is(err, auth.ErrPasswordTooLong) {
			t.Errorf("SignIn(%q, 257 runes) error = %v, want ErrPasswordTooLong", email, err)
		}
	}
	if n := len(spy.calls()); n != 0 {
		t.Errorf("Verify called %d times, want 0", n)
	}
}

// Review Focus 5
func TestLogin_PasswordNotTrimmed(t *testing.T) {
	w := authtest.NewWorld(t)
	w.SetPassword(t, "priya@vardhan.test", t8Password)
	if _, err := t8SignIn(w.Auth, "priya@vardhan.test", " "+t8Password); !errors.Is(err, auth.ErrLoginInvalid) {
		t.Errorf("SignIn(priya, %q) error = %v, want ErrLoginInvalid", " "+t8Password, err)
	}
	got, err := t8SignIn(w.Auth, "priya@vardhan.test\t", t8Password)
	if err != nil {
		t.Fatalf("SignIn(%q, correct) error: %v", "priya@vardhan.test\t", err)
	}
	if want := w.Users["priya@vardhan.test"]; got.User != want {
		t.Errorf("SignIn(%q).User = %d, want %d (priya)", "priya@vardhan.test\t", got.User, want)
	}
}
