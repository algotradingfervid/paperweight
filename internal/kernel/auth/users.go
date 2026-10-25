package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"slices"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// NormalizeEmail trims and lower-cases an email.
func NormalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ValidateEmail accepts a bare address whose domain contains a dot.
func ValidateEmail(s string) error {
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s {
		return ErrEmailInvalid
	}
	if _, domain, _ := strings.Cut(s, "@"); !strings.Contains(domain, ".") {
		return ErrEmailInvalid
	}
	return nil
}

// NewUser is the input to CreateUser. Nil Roles means [Employee].
type NewUser struct {
	Email, Name, Mobile, Password string
	Roles                         []string
}

// UserPatch lists the fields UpdateUser changes; nil fields stay as they are.
// Version is the user's version the page was opened at; when set, UpdateUser
// refuses to save over a newer one (CD-94).
type UserPatch struct {
	Email, Name, Mobile *string
	Enabled             *bool
	Roles               *[]string
	Projects, Sites     *[]int64
	Version             *int64
}

func isAdmin(p Principal) bool { return slices.Contains(p.Roles, RoleAdministrator) }

// byArg is the created_by/updated_by value for actor (NULL = system).
func byArg(actor UserID) any {
	if actor == 0 {
		return nil
	}
	return int64(actor)
}

func getUser(ctx context.Context, q *dbq.Queries, id UserID) (dbq.GetUserRow, error) {
	u, err := q.GetUser(ctx, int64(id))
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// checkAdminsLeft returns ErrLastAdmin when no enabled user other than except
// holds the Administrator role.
func checkAdminsLeft(ctx context.Context, q *dbq.Queries, except UserID) error {
	n, err := q.CountEnabledAdmins(ctx, int64(except))
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLastAdmin
	}
	return nil
}

// resolveRoles looks up names (any case) and returns the roles sorted and
// unique by their stored names; an unknown name is ErrBadForm.
func resolveRoles(ctx context.Context, q *dbq.Queries, names []string) ([]dbq.RoleByNameRow, error) {
	roles := make([]dbq.RoleByNameRow, 0, len(names))
	for _, name := range names {
		r, err := q.RoleByName(ctx, name)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrBadForm // a role the form never offered
		}
		if err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	slices.SortFunc(roles, func(a, b dbq.RoleByNameRow) int { return strings.Compare(a.Name, b.Name) })
	return slices.CompactFunc(roles, func(a, b dbq.RoleByNameRow) bool { return a.ID == b.ID }), nil
}

func roleNames(roles []dbq.RoleByNameRow) string {
	s := make([]string, len(roles))
	for i, r := range roles {
		s[i] = r.Name
	}
	return strings.Join(s, ", ")
}

// setRoles replaces u's roles.
func setRoles(ctx context.Context, q *dbq.Queries, u UserID, roles []dbq.RoleByNameRow) error {
	if err := q.DeleteUserRoles(ctx, int64(u)); err != nil {
		return err
	}
	for _, r := range roles {
		if err := q.InsertUserRole(ctx, dbq.InsertUserRoleParams{UserID: int64(u), RoleID: r.ID}); err != nil {
			return err
		}
	}
	return nil
}

// setRestrictions replaces u's values on dimension d.
func setRestrictions(ctx context.Context, q *dbq.Queries, u UserID, d Dimension, ids []int64) error {
	if err := q.DeleteUserRestrictions(ctx, dbq.DeleteUserRestrictionsParams{UserID: int64(u), Dimension: string(d)}); err != nil {
		return err
	}
	for _, id := range ids {
		if err := q.InsertUserRestriction(ctx, dbq.InsertUserRestrictionParams{UserID: int64(u), Dimension: string(d), ValueID: id}); err != nil {
			return err
		}
	}
	return nil
}

func sortedIDs(ids []int64) []int64 {
	out := slices.Clone(ids)
	slices.Sort(out)
	return slices.Compact(out)
}

// CreateUser adds a user. Only an Administrator may do it. The warnings are
// shown after saving.
func (a *Auth) CreateUser(ctx context.Context, actor Principal, in NewUser) (UserID, []string, error) {
	if !isAdmin(actor) {
		return 0, nil, ErrAdminOnlyField
	}
	email := NormalizeEmail(in.Email)
	if err := ValidateEmail(email); err != nil {
		return 0, nil, err
	}
	if err := CheckPolicy(in.Password, email, ""); err != nil {
		return 0, nil, err
	}
	hash, err := Hash(in.Password)
	if err != nil {
		return 0, nil, err
	}
	roles := in.Roles
	if roles == nil {
		roles = []string{RoleEmployee}
	}
	var warnings []string
	if len(roles) == 0 {
		warnings = append(warnings, NoRolesWarning(in.Name))
	}
	var id UserID
	err = a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		_, err := q.UserIDByEmail(ctx, email)
		if err == nil {
			return EmailTakenError(email)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := a.clock.Now()
		got, err := q.InsertUser(ctx, dbq.InsertUserParams{
			Email: email, Name: in.Name, Mobile: in.Mobile, PasswordHash: hash,
			At: FormatTime(now), By: byArg(actor.ID),
		})
		if err != nil {
			return err
		}
		id = UserID(got)
		stored, err := resolveRoles(ctx, q, roles)
		if err != nil {
			return err
		}
		if err := setRoles(ctx, q, id, stored); err != nil {
			return err
		}
		return a.audit.Append(ctx, tx, audit.Entry{
			DocType: "User", DocID: got, Action: "create",
			Changes: []audit.Change{
				{Field: "email", After: email, Op: "set"},
				{Field: "name", After: in.Name, Op: "set"},
				{Field: "mobile", After: in.Mobile, Op: "set"},
				{Field: "roles", After: roleNames(stored), Op: "set"},
			},
			By: actor.ID, At: now,
		})
	})
	if err != nil {
		return 0, nil, err
	}
	return id, warnings, nil
}

// UpdateUser applies p to user id and returns warnings to show after saving.
// A non-admin may only change their own name and mobile. Nobody may disable
// themselves or remove their own Administrator role, and at least one
// enabled Administrator must remain.
func (a *Auth) UpdateUser(ctx context.Context, actor Principal, id UserID, p UserPatch) ([]string, error) {
	admin := isAdmin(actor)
	if !admin && (actor.ID != id || p.Email != nil || p.Enabled != nil || p.Roles != nil || p.Projects != nil || p.Sites != nil) {
		return nil, ErrAdminOnlyField
	}
	if actor.ID == id && p.Enabled != nil && !*p.Enabled {
		return nil, ErrCannotDisableSelf
	}
	if actor.ID == id && p.Roles != nil && !slices.ContainsFunc(*p.Roles, func(r string) bool {
		return strings.EqualFold(r, RoleAdministrator)
	}) {
		return nil, ErrCannotDemoteSelf
	}
	var email string
	if p.Email != nil {
		email = NormalizeEmail(*p.Email)
		if err := ValidateEmail(email); err != nil {
			return nil, err
		}
	}
	var warnings []string
	err := a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		u, err := getUser(ctx, q, id)
		if err != nil {
			return err
		}
		if p.Version != nil && u.Version != *p.Version {
			return UserChangedError(u.Name)
		}
		var changes []audit.Change
		set := func(field, before, after string) {
			if before != after {
				changes = append(changes, audit.Change{Field: field, Before: before, After: after, Op: "set"})
			}
		}
		next := u
		if p.Email != nil {
			next.Email = email
		}
		if p.Name != nil {
			next.Name = *p.Name
		}
		if p.Mobile != nil {
			next.Mobile = *p.Mobile
		}
		if p.Enabled != nil {
			next.Enabled = 0
			if *p.Enabled {
				next.Enabled = 1
			}
		}
		set("email", u.Email, next.Email)
		set("name", u.Name, next.Name)
		set("mobile", u.Mobile, next.Mobile)
		set("enabled", strconv.FormatBool(u.Enabled == 1), strconv.FormatBool(next.Enabled == 1))
		if next.Email != u.Email {
			other, err := q.UserIDByEmail(ctx, next.Email)
			if err == nil && other != int64(id) {
				return EmailTakenError(next.Email)
			}
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		permChanged := false
		if p.Roles != nil {
			before, err := q.ListUserRoles(ctx, int64(id))
			if err != nil {
				return err
			}
			after, err := resolveRoles(ctx, q, *p.Roles)
			if err != nil {
				return err
			}
			if b, af := strings.Join(before, ", "), roleNames(after); b != af {
				set("roles", b, af)
				if err := setRoles(ctx, q, id, after); err != nil {
					return err
				}
				permChanged = true
			}
			if len(after) == 0 {
				warnings = append(warnings, NoRolesWarning(next.Name))
			}
		}
		for _, r := range []struct {
			dim Dimension
			ids *[]int64
		}{{DimProject, p.Projects}, {DimSite, p.Sites}} {
			if r.ids == nil {
				continue
			}
			before, err := q.ListUserRestrictions(ctx, dbq.ListUserRestrictionsParams{UserID: int64(id), Dimension: string(r.dim)})
			if err != nil {
				return err
			}
			after := sortedIDs(*r.ids)
			if slices.Equal(before, after) {
				continue
			}
			b, err := a.codesText(ctx, tx, r.dim, before)
			if err != nil {
				return err
			}
			af, err := a.codesText(ctx, tx, r.dim, after)
			if err != nil {
				return err
			}
			set(string(r.dim)+"s", b, af)
			if err := setRestrictions(ctx, q, id, r.dim, after); err != nil {
				return err
			}
			permChanged = true
		}
		if len(changes) == 0 {
			return nil
		}
		now := a.clock.Now()
		if err := q.UpdateUserFields(ctx, dbq.UpdateUserFieldsParams{
			Email: next.Email, Name: next.Name, Mobile: next.Mobile, Enabled: next.Enabled,
			At: FormatTime(now), By: byArg(actor.ID), ID: int64(id),
		}); err != nil {
			return err
		}
		if err := checkAdminsLeft(ctx, q, 0); err != nil {
			return err
		}
		if next.Enabled == 0 || next.Email != u.Email {
			if err := q.DeleteUserSessions(ctx, int64(id)); err != nil {
				return err
			}
		}
		if permChanged {
			if err := q.BumpPermVersion(ctx); err != nil {
				return err
			}
		}
		return a.audit.Append(ctx, tx, audit.Entry{
			DocType: "User", DocID: int64(id), Action: "update", Changes: changes,
			By: actor.ID, At: now,
		})
	})
	if err != nil {
		return nil, err
	}
	return warnings, nil
}

// DeleteUser deletes a user who has never signed in and is referenced by
// nothing; otherwise the user should be disabled (CD-19). Only an
// Administrator may do it, and at least one enabled Administrator must remain.
// When version is set, a user changed since then is not deleted (CD-94).
func (a *Auth) DeleteUser(ctx context.Context, actor Principal, id UserID, version *int64) error {
	if !isAdmin(actor) {
		return ErrAdminOnlyField
	}
	return a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		u, err := getUser(ctx, q, id)
		if err != nil {
			return err
		}
		if version != nil && u.Version != *version {
			return UserChangedError(u.Name)
		}
		if err := checkAdminsLeft(ctx, q, id); err != nil {
			return err
		}
		referenced := u.SignedIn == 1
		if !referenced {
			_, referenced, err = a.refs.IsReferenced(ctx, tx, "users", int64(id))
			if err != nil {
				return err
			}
		}
		for _, count := range []func(context.Context, int64) (int64, error){q.CountUserAuditRefs, q.CountRoleAuditRefs} {
			if referenced {
				break
			}
			n, err := count(ctx, int64(id))
			if err != nil {
				return err
			}
			referenced = n > 0
		}
		if referenced {
			return ReferencedUserError(u.Name)
		}
		if err := q.DeleteUserRow(ctx, int64(id)); err != nil {
			return err
		}
		return a.audit.Append(ctx, tx, audit.Entry{
			DocType: "User", DocID: int64(id), Action: string(doctype.ActionDelete),
			By: actor.ID, At: a.clock.Now(),
		})
	})
}

// DisableUser disables u and ends their sessions, as the system (Service).
func (a *Auth) DisableUser(ctx context.Context, tx *db.Tx, u UserID, reason string) error {
	q := dbq.New(tx.SQL())
	if _, err := getUser(ctx, q, u); err != nil {
		return err
	}
	now := a.clock.Now()
	if err := q.SetUserEnabled(ctx, dbq.SetUserEnabledParams{Enabled: 0, At: FormatTime(now), ID: int64(u)}); err != nil {
		return err
	}
	if err := checkAdminsLeft(ctx, q, 0); err != nil {
		return err
	}
	if err := q.DeleteUserSessions(ctx, int64(u)); err != nil {
		return err
	}
	return a.audit.Append(ctx, tx, audit.Entry{
		DocType: "User", DocID: int64(u), Action: "disable", Reason: reason, At: now,
	})
}

// EnableUser enables u, as the system (Service, CD-12).
func (a *Auth) EnableUser(ctx context.Context, tx *db.Tx, u UserID) error {
	q := dbq.New(tx.SQL())
	if _, err := getUser(ctx, q, u); err != nil {
		return err
	}
	now := a.clock.Now()
	if err := q.SetUserEnabled(ctx, dbq.SetUserEnabledParams{Enabled: 1, At: FormatTime(now), ID: int64(u)}); err != nil {
		return err
	}
	return a.audit.Append(ctx, tx, audit.Entry{
		DocType: "User", DocID: int64(u), Action: "enable", At: now,
	})
}

// readUser loads user id in a read transaction.
func (a *Auth) readUser(ctx context.Context, id UserID) (dbq.GetUserRow, error) {
	var u dbq.GetUserRow
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		u, err = getUser(ctx, dbq.New(tx.SQL()), id)
		return err
	})
	return u, err
}

// DisplayName is u's name, with " (disabled)" for a disabled user.
func (a *Auth) DisplayName(ctx context.Context, u UserID) (string, error) {
	row, err := a.readUser(ctx, u)
	if err != nil {
		return "", err
	}
	if row.Enabled == 0 {
		return row.Name + " (disabled)", nil
	}
	return row.Name, nil
}

// EnabledUsers lists the enabled users in ascending ID order.
func (a *Auth) EnabledUsers(ctx context.Context) ([]UserID, error) {
	var out []UserID
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		ids, err := dbq.New(tx.SQL()).ListEnabledUserIDs(ctx)
		for _, id := range ids {
			out = append(out, UserID(id))
		}
		return err
	})
	return out, err
}

// ResetPassword sets a temporary password the target must change at their
// next sign-in, and ends their sessions. Only an Administrator may do it.
func (a *Auth) ResetPassword(ctx context.Context, actor Principal, target UserID, temp string) error {
	if !isAdmin(actor) {
		return ErrNotAdminPassword
	}
	u, err := a.readUser(ctx, target)
	if err != nil {
		return err
	}
	if err := CheckPolicy(temp, u.Email, ""); err != nil {
		return err
	}
	hash, err := Hash(temp)
	if err != nil {
		return err
	}
	return a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		row, err := getUser(ctx, q, target)
		if err != nil {
			return err
		}
		if err := CheckPolicy(temp, row.Email, ""); err != nil {
			return err
		}
		now := a.clock.Now()
		if err := q.SetUserPassword(ctx, dbq.SetUserPasswordParams{
			PasswordHash: hash, MustChange: 1, At: FormatTime(now), By: byArg(actor.ID), ID: int64(target),
		}); err != nil {
			return err
		}
		if err := q.DeleteUserSessions(ctx, int64(target)); err != nil {
			return err
		}
		return a.audit.Append(ctx, tx, audit.Entry{
			DocType: "User", DocID: int64(target), Action: "reset_password",
			By: actor.ID, At: now,
		})
	})
}

// ChangeOwnPassword changes u's password and ends u's other sessions; the
// session whose token hash is keepTokenHash stays signed in.
func (a *Auth) ChangeOwnPassword(ctx context.Context, u UserID, current, next, confirm, keepTokenHash string) error {
	if next != confirm {
		return ErrPasswordMismatch
	}
	row, err := a.readUser(ctx, u)
	if err != nil {
		return err
	}
	verify := a.verify
	if verify == nil {
		verify = VerifyPassword
	}
	ok := false
	if row.PasswordHash != "" {
		if ok, _, err = verify(row.PasswordHash, current); err != nil {
			return err
		}
	}
	if !ok {
		if err := a.countAccountFailure(ctx, row.Email); err != nil {
			return err
		}
		return ErrCurrentPasswordWrong
	}
	if err := CheckPolicy(next, row.Email, row.PasswordHash); err != nil {
		return err
	}
	hash, err := Hash(next)
	if err != nil {
		return err
	}
	return a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		now := a.clock.Now()
		// The hash verified above must still be the stored one, or a password
		// reset committed in between would be undone.
		n, err := q.ChangeUserPassword(ctx, dbq.ChangeUserPasswordParams{
			PasswordHash: hash, At: FormatTime(now), By: int64(u), ID: int64(u), OldHash: row.PasswordHash,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrCurrentPasswordWrong
		}
		if err := q.DeleteUserSessionsExcept(ctx, dbq.DeleteUserSessionsExceptParams{UserID: int64(u), KeepTokenHash: keepTokenHash}); err != nil {
			return err
		}
		return a.audit.Append(ctx, tx, audit.Entry{
			DocType: "User", DocID: int64(u), Action: "change_password",
			By: u, At: now,
		})
	})
}

// DisableUserByEmail disables the user with email (as the system) and ends
// their sessions; an unknown email is ErrNotFound.
func (a *Auth) DisableUserByEmail(ctx context.Context, email, reason string) error {
	return a.db.Write(ctx, func(tx *db.Tx) error {
		id, err := dbq.New(tx.SQL()).UserIDByEmail(ctx, NormalizeEmail(email))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return a.DisableUser(ctx, tx, UserID(id), reason)
	})
}
