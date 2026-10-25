package auth_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// Malformed or hostile stored hashes must be rejected before argon2 runs.
func TestVerifyPassword_RejectsMalformedHash(t *testing.T) {
	const salt, key = "YqqjLzOWCdaYdZKRSLAQNw", "/hjJP8Fhvslp6OKwaCevhesZ6A+GuRBLReYIy1wwoUc"
	cases := map[string]string{
		"oversized m":         "$argon2id$v=19$m=4294967295,t=2,p=1$" + salt + "$" + key,
		"oversized t":         "$argon2id$v=19$m=19456,t=4294967295,p=1$" + salt + "$" + key,
		"zero p":              "$argon2id$v=19$m=19456,t=2,p=0$" + salt + "$" + key,
		"p over 8 bits":       "$argon2id$v=19$m=19456,t=2,p=257$" + salt + "$" + key,
		"junk after version":  "$argon2id$v=19junk$m=19456,t=2,p=1$" + salt + "$" + key,
		"junk after params":   "$argon2id$v=19$m=19456,t=2,p=1junk$" + salt + "$" + key,
		"extra param":         "$argon2id$v=19$m=19456,t=2,p=1,x=1$" + salt + "$" + key,
		"params out of order": "$argon2id$v=19$t=2,m=19456,p=1$" + salt + "$" + key,
		"empty salt":          "$argon2id$v=19$m=19456,t=2,p=1$$" + key,
		"short salt":          "$argon2id$v=19$m=19456,t=2,p=1$AAAA$" + key,
		"empty key":           "$argon2id$v=19$m=19456,t=2,p=1$" + salt + "$",
		"bad base64 salt":     "$argon2id$v=19$m=19456,t=2,p=1$!!!!$" + key,
		"too few segments":    "$argon2id$v=19$m=19456,t=2,p=1$" + salt,
		"too many segments":   "$argon2id$v=19$m=19456,t=2,p=1$" + salt + "$" + key + "$x",
		"wrong algorithm":     "$argon2i$v=19$m=19456,t=2,p=1$" + salt + "$" + key,
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			ok, _, err := auth.VerifyPassword(h, "Correct-Horse-42")
			if ok || err == nil {
				t.Errorf("VerifyPassword(%q) = %v, %v; want false, error", h, ok, err)
			}
		})
	}
}

func TestCheckPolicy_MalformedCurrentHashIsError(t *testing.T) {
	if err := auth.CheckPolicy("Chai-Break-Ledger-7", "priya@vardhan.test", "$argon2id$junk"); err == nil {
		t.Error("CheckPolicy with a malformed current hash = nil, want an error")
	}
}
