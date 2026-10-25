package auth

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
)

// Auth log events.
const (
	eventLoginSuccess = "login_success"
	eventLoginFailed  = "login_failed"
)

// logAuth writes one auth_log row: user's ID, or the email hash when user is
// 0. Never the password.
func logAuth(ctx context.Context, q *dbq.Queries, at, event string, user UserID, email string, in SignInInput) error {
	var userID, hash any
	if user != 0 {
		userID = int64(user)
	} else {
		hash = emailHash(email)
	}
	return q.AuthLogInsert(ctx, dbq.AuthLogInsertParams{
		At: at, Event: event, UserID: userID, EmailHash: hash, Ip: in.IP, UserAgent: in.UserAgent,
	})
}
