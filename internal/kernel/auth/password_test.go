package auth_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

const argon2idPrefix = "$argon2id$v=19$m=19456,t=2,p=1$"

// AUTH-020
func TestPassword_StoredAsArgon2id(t *testing.T) {
	const pw = "Correct-Horse-42"
	h, err := auth.Hash(pw)
	if err != nil {
		t.Fatalf("Hash(%q) error: %v", pw, err)
	}
	if !strings.HasPrefix(h, argon2idPrefix) {
		t.Errorf("Hash(%q) = %q, want prefix %q", pw, h, argon2idPrefix)
	}
	if strings.Contains(h, pw) {
		t.Errorf("Hash(%q) = %q contains the password text", pw, h)
	}
	ok, rehash, err := auth.VerifyPassword(h, pw)
	if !ok || rehash || err != nil {
		t.Errorf("VerifyPassword(h, %q) = %v, %v, %v; want true, false, nil", pw, ok, rehash, err)
	}
	if ok, _, _ := auth.VerifyPassword(h, "Correct-Horse-43"); ok {
		t.Errorf("VerifyPassword(h, %q) = true, want false", "Correct-Horse-43")
	}
}

// AUTH-021
func TestPassword_UniqueSalt(t *testing.T) {
	const pw = "Correct-Horse-42"
	h1, err := auth.Hash(pw)
	if err != nil {
		t.Fatalf("first Hash(%q) error: %v", pw, err)
	}
	h2, err := auth.Hash(pw)
	if err != nil {
		t.Fatalf("second Hash(%q) error: %v", pw, err)
	}
	if h1 == h2 {
		t.Errorf("two Hash(%q) calls returned the same hash %q", pw, h1)
	}
	for i, h := range []string{h1, h2} {
		if ok, _, err := auth.VerifyPassword(h, pw); !ok || err != nil {
			t.Errorf("hash %d: VerifyPassword(%q, %q) = %v, %v; want true, nil", i+1, h, pw, ok, err)
		}
	}
}
