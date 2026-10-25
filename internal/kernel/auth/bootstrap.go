package auth

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// SetupDone reports whether any user exists, so first-admin setup is closed.
func (a *Auth) SetupDone(ctx context.Context) (bool, error) {
	var done bool
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		done, err = dbq.New(tx.SQL()).UsersExist(ctx)
		return err
	})
	return done, err
}

// CreateFirstAdmin creates the first user, an enabled Administrator, as the
// system. Once any user exists it returns ErrUsersExist, checked before the
// input is validated and again inside the write (CD-94).
func (a *Auth) CreateFirstAdmin(ctx context.Context, email, name, password string) (UserID, error) {
	if done, err := a.SetupDone(ctx); err != nil {
		return 0, err
	} else if done {
		return 0, ErrUsersExist
	}
	email = NormalizeEmail(email)
	if err := ValidateEmail(email); err != nil {
		return 0, err
	}
	if err := CheckPolicy(password, email, ""); err != nil {
		return 0, err
	}
	hash, err := Hash(password)
	if err != nil {
		return 0, err
	}
	var id UserID
	err = a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		if exist, err := q.UsersExist(ctx); err != nil {
			return err
		} else if exist {
			return ErrUsersExist
		}
		now := a.clock.Now()
		got, err := q.InsertUser(ctx, dbq.InsertUserParams{
			Email: email, Name: name, PasswordHash: hash, At: FormatTime(now), By: byArg(0),
		})
		if err != nil {
			return err
		}
		id = UserID(got)
		roles, err := resolveRoles(ctx, q, []string{RoleAdministrator})
		if err != nil {
			return err
		}
		if err := setRoles(ctx, q, id, roles); err != nil {
			return err
		}
		return a.audit.Append(ctx, tx, audit.Entry{
			DocType: "User", DocID: got, Action: "create",
			Changes: []audit.Change{
				{Field: "email", After: email, Op: "set"},
				{Field: "name", After: name, Op: "set"},
				{Field: "roles", After: roleNames(roles), Op: "set"},
			},
			By: 0, At: now,
		})
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}
