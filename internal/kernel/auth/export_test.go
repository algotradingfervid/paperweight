package auth

import (
	"context"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// Grant exposes the unexported grant, in its own read, for tests in package auth_test.
var Grant = (*Auth).grantInRead

func (a *Auth) grantInRead(ctx context.Context, roles []string, doctypeName string, r Right) (Scope, error) {
	var s Scope
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		s, err = a.grant(ctx, tx, roles, doctypeName, r)
		return err
	})
	return s, err
}

// RecordFailure exposes the unexported recordFailure for tests in package auth_test.
var RecordFailure = (*Auth).recordFailure
