package auth_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	t7Admin  = "admin@vardhan.test"
	t7Nisha  = "nisha@vardhan.test"
	t7Kiran  = "kiran@vardhan.test"
	t7Deepak = "deepak@vardhan.test"
)

// t7WantProblem asserts err is want by errors.Is and has want's exact text.
func t7WantProblem(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("%s: err = %v, want %v", what, err, want)
		return
	}
	if err.Error() != want.Error() {
		t.Errorf("%s: text = %q, want %q", what, err.Error(), want.Error())
	}
}

// t7WantText asserts err has the exact user-facing text msg and status 409.
func t7WantText(t *testing.T, what string, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: err = nil, want %q", what, msg)
		return
	}
	if err.Error() != msg {
		t.Errorf("%s: text = %q, want %q", what, err.Error(), msg)
	}
	var p *auth.Problem
	if !errors.As(err, &p) {
		t.Errorf("%s: err %T is not *auth.Problem", what, err)
	} else if p.Status() != 409 {
		t.Errorf("%s: status = %d, want 409", what, p.Status())
	}
}

// t7Strings runs a query whose single column is text and returns the rows.
func t7Strings(t *testing.T, w *authtest.World, query string, args ...any) []string {
	t.Helper()
	ctx := context.Background()
	var out []string
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return out
}

// t7Count runs a COUNT query.
func t7Count(t *testing.T, w *authtest.World, query string, args ...any) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, query, args...).Scan(&n)
	}); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

// t7Exec runs one statement in a write transaction.
func t7Exec(t *testing.T, w *authtest.World, query string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, query, args...)
		return err
	}); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// t7Snapshot is every users, user_roles and sessions row, as text.
func t7Snapshot(t *testing.T, w *authtest.World) []string {
	t.Helper()
	var out []string
	out = append(out, t7Strings(t, w, `SELECT 'u|' || id || '|' || email || '|' || name || '|' || mobile || '|' ||
		ifnull(password_hash, '') || '|' || must_change_password || '|' || enabled || '|' ||
		ifnull(last_login_at, '') || '|' || version || '|' || created_at || '|' || ifnull(created_by, '') || '|' ||
		updated_at || '|' || ifnull(updated_by, '') FROM users ORDER BY id`)...)
	out = append(out, t7Strings(t, w, `SELECT 'r|' || user_id || '|' || role_id FROM user_roles ORDER BY user_id, role_id`)...)
	out = append(out, t7Strings(t, w, `SELECT 's|' || id || '|' || token_hash || '|' || ifnull(user_id, '') FROM sessions ORDER BY id`)...)
	return out
}

func t7Enabled(t *testing.T, w *authtest.World, u auth.UserID) bool {
	t.Helper()
	return t7Count(t, w, `SELECT enabled FROM users WHERE id = ?`, int64(u)) == 1
}

func t7Sessions(t *testing.T, w *authtest.World, u auth.UserID) int {
	t.Helper()
	return t7Count(t, w, `SELECT COUNT(*) FROM sessions WHERE user_id = ?`, int64(u))
}

func t7AddSession(t *testing.T, w *authtest.World, u auth.UserID, tokenHash string) {
	t.Helper()
	at := auth.FormatTime(w.Clock.Now())
	t7Exec(t, w, `INSERT INTO sessions (token_hash, user_id, data, created_at, last_seen_at) VALUES (?, ?, x'00', ?, ?)`,
		tokenHash, int64(u), at, at)
}

// t7EnabledAdmins counts enabled users holding the Administrator role.
func t7EnabledAdmins(t *testing.T, w *authtest.World) int {
	t.Helper()
	return t7Count(t, w, `SELECT COUNT(*) FROM users u JOIN user_roles ur ON ur.user_id = u.id
		JOIN roles r ON r.id = ur.role_id WHERE u.enabled = 1 AND r.name = 'Administrator'`)
}

// AUTH-051
func TestUser_EmailValidUnique(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	before := t7Snapshot(t, w)

	// A password that passes the policy, so only the email can be at fault.
	const accepted = "Lantern-Orbit-73"
	_, _, err := w.Auth.CreateUser(ctx, admin, auth.NewUser{Email: "PRIYA@vardhan.test", Name: "X", Password: accepted})
	const taken = "A user with email priya@vardhan.test already exists."
	t7WantText(t, "CreateUser(PRIYA@vardhan.test)", err, taken)
	if got := auth.EmailTakenError("priya@vardhan.test").Error(); got != taken {
		t.Errorf("EmailTakenError text = %q, want %q", got, taken)
	}

	_, _, err = w.Auth.CreateUser(ctx, admin, auth.NewUser{Email: "priya@", Name: "X", Password: accepted})
	t7WantProblem(t, "CreateUser(priya@)", err, auth.ErrEmailInvalid)
	if got, want := auth.ErrEmailInvalid.Error(), "Enter a valid email address, such as name@company.in."; got != want {
		t.Errorf("ErrEmailInvalid text = %q, want %q", got, want)
	}

	if got := t7Snapshot(t, w); !reflect.DeepEqual(got, before) {
		t.Errorf("rejected creates changed the database:\n got %q\nwant %q", got, before)
	}
	if n := len(w.Audit.Entries()); n != 0 {
		t.Errorf("rejected creates appended %d audit entries, want 0", n)
	}

	if got, want := auth.NormalizeEmail("  PRIYA@Vardhan.Test\t"), "priya@vardhan.test"; got != want {
		t.Errorf("NormalizeEmail = %q, want %q", got, want)
	}
	if err := auth.ValidateEmail("priya@vardhan.test"); err != nil {
		t.Errorf("ValidateEmail(priya@vardhan.test) = %v, want nil", err)
	}
	for _, bad := range []string{"priya@", "priya", "priya@localhost", "Priya Shah <priya@vardhan.test>"} {
		t7WantProblem(t, fmt.Sprintf("ValidateEmail(%q)", bad), auth.ValidateEmail(bad), auth.ErrEmailInvalid)
	}
}

// AUTH-053
func TestUser_CannotDisableOrDemoteSelf(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	before := t7Snapshot(t, w)

	off := false
	_, err := w.Auth.UpdateUser(ctx, admin, admin.ID, auth.UserPatch{Enabled: &off})
	t7WantProblem(t, "UpdateUser(self, Enabled=false)", err, auth.ErrCannotDisableSelf)
	if got, want := auth.ErrCannotDisableSelf.Error(), "You can't disable your own account. Ask another administrator."; got != want {
		t.Errorf("ErrCannotDisableSelf text = %q, want %q", got, want)
	}

	roles := []string{"Employee"}
	_, err = w.Auth.UpdateUser(ctx, admin, admin.ID, auth.UserPatch{Roles: &roles})
	t7WantProblem(t, "UpdateUser(self, Roles=[Employee])", err, auth.ErrCannotDemoteSelf)
	if got, want := auth.ErrCannotDemoteSelf.Error(), "You can't remove your own Administrator role. Ask another administrator."; got != want {
		t.Errorf("ErrCannotDemoteSelf text = %q, want %q", got, want)
	}

	if got := t7Snapshot(t, w); !reflect.DeepEqual(got, before) {
		t.Errorf("refused updates changed the database:\n got %q\nwant %q", got, before)
	}
	if got, want := w.P(t, t7Admin).Roles, []string{"Administrator"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Anil's roles = %q, want %q", got, want)
	}
}

// AUTH-054
func TestUser_AtLeastOneAdmin(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	anil := w.Users[t7Admin]
	nisha := w.AddUser(t, t7Nisha, "Nisha Menon", "Administrator")
	nishaP := w.P(t, t7Nisha)
	t7AddSession(t, w, nisha, "nisha-token-hash")

	none := []string{}
	if _, err := w.Auth.UpdateUser(ctx, nishaP, anil, auth.UserPatch{Roles: &none}); err != nil {
		t.Fatalf("UpdateUser(nisha removes Anil's roles) = %v, want nil", err)
	}
	if got := w.P(t, t7Admin).Roles; len(got) != 0 {
		t.Fatalf("Anil's roles after removal = %q, want none", got)
	}
	if n := t7EnabledAdmins(t, w); n != 1 {
		t.Fatalf("enabled Administrators = %d, want 1 (Nisha)", n)
	}

	before := t7Snapshot(t, w)
	audits := len(w.Audit.Entries())
	unchanged := func(what string) {
		t.Helper()
		if got := t7Snapshot(t, w); !reflect.DeepEqual(got, before) {
			t.Errorf("%s changed the database:\n got %q\nwant %q", what, got, before)
		}
		if !t7Enabled(t, w, nisha) {
			t.Errorf("after %s: Nisha is disabled, want enabled", what)
		}
		if got, want := w.P(t, t7Nisha).Roles, []string{"Administrator"}; !reflect.DeepEqual(got, want) {
			t.Errorf("after %s: Nisha's roles = %q, want %q", what, got, want)
		}
		if n := len(w.Audit.Entries()); n != audits {
			t.Errorf("after %s: %d audit entries, want %d (nothing appended)", what, n, audits)
		}
	}

	// Service / CLI path (`app user disable nisha@vardhan.test`).
	err := w.DB.Write(ctx, func(tx *db.Tx) error {
		return w.Auth.DisableUser(ctx, tx, nisha, "left")
	})
	t7WantProblem(t, "DisableUser(nisha)", err, auth.ErrLastAdmin)
	unchanged("DisableUser(nisha)")

	// Nisha is the only enabled Administrator: the last-admin rule refuses the
	// delete with its own message (AUTH-054), even though Nisha has changed
	// Anil's user record above.
	err = w.Auth.DeleteUser(ctx, nishaP, nisha)
	t7WantProblem(t, "DeleteUser(nisha)", err, auth.ErrLastAdmin)
	unchanged("DeleteUser(nisha)")

	employee := []string{"Employee"}
	_, err = w.Auth.UpdateUser(ctx, nishaP, nisha, auth.UserPatch{Roles: &employee})
	t7WantProblem(t, "UpdateUser(nisha demotes self)", err, auth.ErrCannotDemoteSelf)
	unchanged("UpdateUser(nisha demotes self)")

	if got, want := auth.ErrLastAdmin.Error(), "At least one enabled user must have the Administrator role."; got != want {
		t.Errorf("ErrLastAdmin text = %q, want %q", got, want)
	}
}

// AUTH-055
func TestUser_DeleteOnlyIfUnreferenced(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	priya := w.Users[priyaEmail]

	w.Refs.Add("users", int64(priya), "authtest.Order PO/26-27/00001")
	err := w.Auth.DeleteUser(ctx, admin, priya)
	const msg = "Priya Shah has created or changed records, so they can't be deleted. Disable them instead."
	t7WantText(t, "DeleteUser(priya)", err, msg)
	if got := auth.ReferencedUserError("Priya Shah").Error(); got != msg {
		t.Errorf("ReferencedUserError text = %q, want %q", got, msg)
	}
	if n := t7Count(t, w, `SELECT COUNT(*) FROM users WHERE id = ?`, int64(priya)); n != 1 {
		t.Errorf("Priya's row count after refused delete = %d, want 1", n)
	}

	// Users referenced only from users/roles audit columns or by a sign-in
	// (users.created_by/updated_by have no ON DELETE): the refusal is
	// ReferencedUserError, never a raw foreign-key error.
	refs := []struct {
		email, name, setup string
		onRole             bool
	}{
		{"arjun@vardhan.test", "Arjun Rao", `UPDATE users SET updated_by = ? WHERE email = 'deepak@vardhan.test'`, false},
		{"lata@vardhan.test", "Lata Verma", `UPDATE users SET created_by = ? WHERE email = 'meena@vardhan.test'`, false},
		{"mohan@vardhan.test", "Mohan Das", `UPDATE roles SET updated_by = ? WHERE name = 'Director'`, true},
		{"farah@vardhan.test", "Farah Khan", `UPDATE roles SET created_by = ? WHERE name = 'Stores'`, true},
	}
	for _, r := range refs {
		u := w.AddUser(t, r.email, r.name, "Employee")
		t7Exec(t, w, r.setup, int64(u))
		before := t7Snapshot(t, w)
		err := w.Auth.DeleteUser(ctx, admin, u)
		t7WantText(t, "DeleteUser("+r.email+")", err, auth.ReferencedUserError(r.name).Error())
		if got := t7Snapshot(t, w); !reflect.DeepEqual(got, before) {
			t.Errorf("refused DeleteUser(%s) changed the database:\n got %q\nwant %q", r.email, got, before)
		}
	}

	signedIn := w.AddUser(t, "vikram@vardhan.test", "Vikram Sethi", "Employee")
	t7Exec(t, w, `UPDATE users SET last_login_at = ? WHERE id = ?`, auth.FormatTime(w.Clock.Now()), int64(signedIn))
	err = w.Auth.DeleteUser(ctx, admin, signedIn)
	t7WantText(t, "DeleteUser(vikram, signed in once)", err, auth.ReferencedUserError("Vikram Sethi").Error())
	if n := t7Count(t, w, `SELECT COUNT(*) FROM users WHERE id = ?`, int64(signedIn)); n != 1 {
		t.Errorf("Vikram's row count after refused delete = %d, want 1", n)
	}

	kiran := w.AddUser(t, t7Kiran, "Kiran Joshi", "Employee")
	if err := w.Auth.DeleteUser(ctx, admin, kiran); err != nil {
		t.Fatalf("DeleteUser(kiran) = %v, want nil", err)
	}
	if n := t7Count(t, w, `SELECT COUNT(*) FROM users WHERE id = ?`, int64(kiran)); n != 0 {
		t.Errorf("Kiran's users rows after delete = %d, want 0", n)
	}
	if n := t7Count(t, w, `SELECT COUNT(*) FROM user_roles WHERE user_id = ?`, int64(kiran)); n != 0 {
		t.Errorf("Kiran's user_roles rows after delete = %d, want 0", n)
	}
}

// Review Focus 3
func TestUser_AtLeastOneAdmin_Concurrent(t *testing.T) {
	for round := range 5 {
		w := authtest.NewWorld(t)
		ctx := context.Background()
		anil := w.Users[t7Admin]
		nisha := w.AddUser(t, t7Nisha, "Nisha Menon", "Administrator")
		anilP, nishaP := w.P(t, t7Admin), w.P(t, t7Nisha)

		var (
			start                       = make(chan struct{})
			wg                          sync.WaitGroup
			anilDemotesNisha, nishaDemo error
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			none := []string{}
			<-start
			_, anilDemotesNisha = w.Auth.UpdateUser(ctx, anilP, nisha, auth.UserPatch{Roles: &none})
		}()
		go func() {
			defer wg.Done()
			none := []string{}
			<-start
			_, nishaDemo = w.Auth.UpdateUser(ctx, nishaP, anil, auth.UserPatch{Roles: &none})
		}()
		close(start)
		wg.Wait()

		var winner, loser string
		switch {
		case anilDemotesNisha == nil && errors.Is(nishaDemo, auth.ErrLastAdmin):
			winner, loser = t7Admin, t7Nisha
		case nishaDemo == nil && errors.Is(anilDemotesNisha, auth.ErrLastAdmin):
			winner, loser = t7Nisha, t7Admin
		default:
			t.Fatalf("round %d: anil demotes nisha = %v, nisha demotes anil = %v; want exactly one nil and one ErrLastAdmin",
				round, anilDemotesNisha, nishaDemo)
		}
		if n := t7EnabledAdmins(t, w); n != 1 {
			t.Errorf("round %d: enabled Administrators = %d, want 1", round, n)
		}
		if got, want := w.P(t, winner).Roles, []string{"Administrator"}; !reflect.DeepEqual(got, want) {
			t.Errorf("round %d: %s roles = %q, want %q", round, winner, got, want)
		}
		if got := w.P(t, loser).Roles; len(got) != 0 {
			t.Errorf("round %d: %s roles = %q, want none", round, loser, got)
		}
	}
}

func TestService_DisableEnableUser(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	deepak := w.Users[t7Deepak]
	priya := w.Users[priyaEmail]
	t7AddSession(t, w, deepak, "deepak-token-hash-1")
	t7AddSession(t, w, deepak, "deepak-token-hash-2")
	t7AddSession(t, w, priya, "priya-token-hash")

	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		return w.Auth.DisableUser(ctx, tx, deepak, "left")
	}); err != nil {
		t.Fatalf("DisableUser(deepak) = %v, want nil", err)
	}
	if t7Enabled(t, w, deepak) {
		t.Errorf("Deepak enabled after DisableUser, want disabled")
	}
	if n := t7Sessions(t, w, deepak); n != 0 {
		t.Errorf("Deepak's sessions after DisableUser = %d, want 0", n)
	}
	if n := t7Sessions(t, w, priya); n != 1 {
		t.Errorf("Priya's sessions after disabling Deepak = %d, want 1", n)
	}
	entries := w.Audit.For("User", int64(deepak))
	if len(entries) != 1 {
		t.Fatalf("audit entries for User %d after DisableUser = %+v, want 1", deepak, entries)
	}
	checkEntry := func(e audit.Entry, action, reason string) {
		t.Helper()
		if e.DocType != "User" || e.DocID != int64(deepak) || e.Action != action || e.Reason != reason {
			t.Errorf("audit entry = {DocType:%q DocID:%d Action:%q Reason:%q}, want {User %d %q %q}",
				e.DocType, e.DocID, e.Action, e.Reason, deepak, action, reason)
		}
		if !e.At.Equal(authtest.T0) {
			t.Errorf("audit entry %q At = %v, want %v (the clock)", action, e.At, authtest.T0)
		}
	}
	checkEntry(entries[0], "disable", "left")

	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		return w.Auth.EnableUser(ctx, tx, deepak)
	}); err != nil {
		t.Fatalf("EnableUser(deepak) = %v, want nil", err)
	}
	if !t7Enabled(t, w, deepak) {
		t.Errorf("Deepak disabled after EnableUser, want enabled")
	}
	entries = w.Audit.For("User", int64(deepak))
	if len(entries) != 2 {
		t.Fatalf("audit entries for User %d after EnableUser = %+v, want 2", deepak, entries)
	}
	checkEntry(entries[1], "enable", "")

	audits := len(w.Audit.Entries())
	err := w.DB.Write(ctx, func(tx *db.Tx) error {
		return w.Auth.EnableUser(ctx, tx, 999)
	})
	if !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("EnableUser(999) = %v, want ErrNotFound", err)
	}
	if n := len(w.Audit.Entries()); n != audits {
		t.Errorf("EnableUser(999) appended audit entries: %d, want %d", n, audits)
	}
}
