package auth

import (
	_ "embed"
	"strings"
	"unicode/utf8"
)

// commonPasswordsText is SecLists 10k-most-common.txt (MIT, CD-18), filtered
// to entries of at least 10 runes, lower-cased, plus a few supplements.
//
//go:embed common_passwords.txt
var commonPasswordsText string

var commonPasswords = func() map[string]struct{} {
	m := make(map[string]struct{})
	for line := range strings.Lines(commonPasswordsText) {
		if line = strings.TrimRight(line, "\r\n"); line != "" {
			m[line] = struct{}{}
		}
	}
	return m
}()

// CheckPolicy returns the first policy rule password breaks, or nil. The
// checks run in order: too short, too long, common, contains the email local
// part, same as currentHash (skipped when currentHash is "").
func CheckPolicy(password, email, currentHash string) error {
	pw := NormalizePassword(password)
	switch n := utf8.RuneCountInString(pw); {
	case n < 10:
		return ErrPasswordTooShort
	case n > 256:
		return ErrPasswordTooLong
	}
	lower := strings.ToLower(pw)
	if _, ok := commonPasswords[lower]; ok {
		return ErrPasswordCommon
	}
	local, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	if utf8.RuneCountInString(local) >= 3 && strings.Contains(lower, local) {
		return ErrPasswordHasName
	}
	if currentHash != "" {
		same, _, err := VerifyPassword(currentHash, pw)
		if err != nil {
			return err
		}
		if same {
			return ErrPasswordSame
		}
	}
	return nil
}
