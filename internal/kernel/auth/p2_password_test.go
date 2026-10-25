package auth_test

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// Task 16 (P2) password cases at service level.

// t16OldHash returns a PHC argon2id hash of pw made with the outdated
// parameters m=8192, t=1, p=1 (16-byte salt, 32-byte key).
func t16OldHash(t *testing.T, pw string) string {
	t.Helper()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	key := argon2.IDKey([]byte(pw), salt, 1, 8192, 1, 32)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=19$m=8192,t=1,p=1$%s$%s", enc.EncodeToString(salt), enc.EncodeToString(key))
}

// AUTH-023
func TestPassword_RehashOnLogin(t *testing.T) {
	const email = "priya@vardhan.test"
	w := authtest.NewWorld(t)
	old := t16OldHash(t, t8Password)
	raceExec(t, w, `UPDATE users SET password_hash = ? WHERE email = ?`, old, email)

	if _, err := t8SignIn(w.Auth, email, t8Wrong); !errors.Is(err, auth.ErrLoginInvalid) {
		t.Fatalf("SignIn(wrong) error = %v, want ErrLoginInvalid", err)
	}
	if _, hash := raceUser(t, w, email); hash != old {
		t.Errorf("after a failed sign-in: password_hash = %q, want the old hash %q unchanged", hash, old)
	}

	got, err := t8SignIn(w.Auth, email, t8Password)
	if err != nil {
		t.Fatalf("SignIn(correct) against the old hash: %v", err)
	}
	if want := w.Users[email]; got.User != want {
		t.Errorf("SignIn(correct).User = %d, want %d", got.User, want)
	}
	_, hash := raceUser(t, w, email)
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("after a successful sign-in: password_hash = %q, want prefix $argon2id$v=19$m=19456,t=2,p=1$", hash)
	}
	ok, rehash, err := auth.VerifyPassword(hash, t8Password)
	if err != nil || !ok || rehash {
		t.Errorf("VerifyPassword(new hash, correct) = ok %v, rehash %v, err %v; want true, false, nil", ok, rehash, err)
	}
	if _, err := t8SignIn(w.Auth, email, t8Password); err != nil {
		t.Errorf("SignIn(correct) against the upgraded hash: %v", err)
	}
}

// AUTH-024
func TestPassword_UnicodeNFC(t *testing.T) {
	const email = "priya@vardhan.test"
	const nfd = "Café-Ledger-43" // e + combining acute accent
	const nfc = "Café-Ledger-43"  // precomposed é
	if nfd == nfc {
		t.Fatal("test setup: the NFD and NFC forms are the same string")
	}
	w := authtest.NewWorld(t)
	w.SetPassword(t, email, nfd)
	priya := w.Users[email]

	for _, tc := range []struct{ what, pw string }{{"NFC", nfc}, {"NFD", nfd}} {
		got, err := t8SignIn(w.Auth, email, tc.pw)
		if err != nil {
			t.Errorf("SignIn(%s form) error = %v, want success", tc.what, err)
			continue
		}
		if got.User != priya {
			t.Errorf("SignIn(%s form).User = %d, want %d", tc.what, got.User, priya)
		}
	}

	_, err := t8SignIn(w.Auth, email, "Cafe-Ledger-43")
	if !errors.Is(err, auth.ErrLoginInvalid) {
		t.Fatalf("SignIn(%q) error = %v, want ErrLoginInvalid", "Cafe-Ledger-43", err)
	}
	if got := t8Describe(err); got.Status != http.StatusUnauthorized {
		t.Errorf("SignIn(%q) status = %d, want 401", "Cafe-Ledger-43", got.Status)
	}
}

// AUTH-028
func TestPasswordPolicy_NotContainingEmail(t *testing.T) {
	checkPolicyErr(t, "Priya-2026-Secure", priyaEmail, "", auth.ErrPasswordHasName)
	if got, want := auth.ErrPasswordHasName.Error(), "Password must not contain your name or email."; got != want {
		t.Errorf("ErrPasswordHasName text = %q, want %q", got, want)
	}
}
