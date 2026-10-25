package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// SignInInput is one sign-in attempt.
type SignInInput struct{ Email, Password, IP, UserAgent string }

// SignInResult is the signed-in user.
type SignInResult struct {
	User               UserID
	MustChangePassword bool
}

// SignIn checks in's credentials, counting failures towards the account and
// IP lockouts and writing the auth log. The email is trimmed and lower-cased;
// the password is never trimmed.
func (a *Auth) SignIn(ctx context.Context, in SignInInput) (SignInResult, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || in.Password == "" {
		return SignInResult{}, ErrLoginIncomplete
	}
	if len(in.Password) > 256*utf8.UTFMax || utf8.RuneCountInString(NormalizePassword(in.Password)) > 256 {
		return SignInResult{}, ErrPasswordTooLong
	}
	if blocked, err := a.locked(ctx, IPKey(in.IP), IPLimit); err != nil {
		return SignInResult{}, err
	} else if blocked {
		return SignInResult{}, ErrIPBlocked
	}
	if locked, err := a.locked(ctx, AccountKey(email), AccountLimit); err != nil {
		return SignInResult{}, err
	} else if locked {
		return SignInResult{}, ErrAccountLocked
	}

	var u dbq.AuthLogGetSignInUserRow
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		u, err = dbq.New(tx.SQL()).AuthLogGetSignInUser(ctx, email)
		return err
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SignInResult{}, err
	}
	hash := u.PasswordHash
	if hash == "" {
		hash = DummyHash
	}
	verify := a.verify
	if verify == nil {
		verify = VerifyPassword
	}
	ok, rehash, err := verify(hash, in.Password)
	if err != nil {
		return SignInResult{}, err
	}
	user := UserID(u.ID)
	if !ok || u.PasswordHash == "" || u.Enabled == 0 {
		if err := a.signInFailed(ctx, email, user, in); err != nil {
			return SignInResult{}, err
		}
		if !ok || u.PasswordHash == "" {
			return SignInResult{}, ErrLoginInvalid
		}
		return SignInResult{}, ErrAccountDisabled
	}

	var newHash string
	if rehash {
		if newHash, err = Hash(in.Password); err != nil {
			return SignInResult{}, err
		}
	}
	var cur dbq.AuthLogGetSignInUserRow
	err = a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		// Re-check inside the transaction: a disable, reset or own change
		// since the read above wins over this sign-in (CD-94).
		var err error
		cur, err = q.AuthLogGetSignInUser(ctx, email)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err != nil || cur.ID != u.ID || cur.PasswordHash != u.PasswordHash {
			return ErrLoginInvalid
		}
		if cur.Enabled == 0 {
			return ErrAccountDisabled
		}
		at := FormatTime(a.clock.Now())
		if err := q.LockoutDelete(ctx, AccountKey(email)); err != nil {
			return err
		}
		if err := q.AuthLogSetLastLogin(ctx, dbq.AuthLogSetLastLoginParams{At: at, ID: u.ID}); err != nil {
			return err
		}
		if newHash != "" {
			if err := q.AuthLogSetPasswordHash(ctx, dbq.AuthLogSetPasswordHashParams{PasswordHash: newHash, ID: u.ID, OldHash: u.PasswordHash}); err != nil {
				return err
			}
		}
		return logAuth(ctx, q, at, eventLoginSuccess, user, email, in)
	})
	if errors.Is(err, ErrLoginInvalid) || errors.Is(err, ErrAccountDisabled) {
		if ferr := a.signInFailed(ctx, email, user, in); ferr != nil {
			return SignInResult{}, ferr
		}
		return SignInResult{}, err
	}
	if err != nil {
		return SignInResult{}, err
	}
	return SignInResult{User: user, MustChangePassword: cur.MustChangePassword != 0}, nil
}

// signInFailed counts a failure for email's account and in's IP and writes
// login_failed (user 0 = unknown).
func (a *Auth) signInFailed(ctx context.Context, email string, user UserID, in SignInInput) error {
	if err := a.countAccountFailure(ctx, email); err != nil {
		return err
	}
	if err := a.recordFailure(ctx, IPKey(in.IP)); err != nil {
		return err
	}
	return a.db.Write(ctx, func(tx *db.Tx) error {
		return logAuth(ctx, dbq.New(tx.SQL()), FormatTime(a.clock.Now()), eventLoginFailed, user, email, in)
	})
}
