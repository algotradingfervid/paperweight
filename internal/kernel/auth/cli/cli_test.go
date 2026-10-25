package cli_test

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/cli"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/testkit"
)

const t11UsersExist = "Users already exist. Sign in as an administrator to add more users."

// AUTH-049
func TestBootstrap_CLIFirstAdmin(t *testing.T) {
	ctx := context.Background()
	d := testkit.NewDB(t) // migrated, no users
	rec := auditfake.New()
	a := auth.New(auth.Deps{DB: d, Clock: clock.NewFixed(authtest.T0), Directory: authfake.NewDirectory(), Audit: rec})

	args := []string{"--email", "admin@vardhan.test", "--name", "Anil Kapoor"}
	var out bytes.Buffer
	if err := cli.AdminCreate(ctx, a, args, strings.NewReader("Correct-Horse-42\nCorrect-Horse-42\n"), &out); err != nil {
		t.Fatalf("AdminCreate: %v (output %q)", err, out.String())
	}

	var (
		id          int64
		email, name string
		enabled     int
	)
	if err := d.SQL().QueryRowContext(ctx, `SELECT id, email, name, enabled FROM users`).Scan(&id, &email, &name, &enabled); err != nil {
		t.Fatalf("read the created user: %v", err)
	}
	if id != 1 || email != "admin@vardhan.test" || name != "Anil Kapoor" || enabled != 1 {
		t.Errorf("user = (%d, %q, %q, enabled %d), want (1, admin@vardhan.test, Anil Kapoor, enabled 1)", id, email, name, enabled)
	}

	rows, err := d.SQL().QueryContext(ctx, `SELECT r.name FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = 1 ORDER BY r.name`)
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatal(err)
		}
		roles = append(roles, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if !reflect.DeepEqual(roles, []string{auth.RoleAdministrator}) {
		t.Errorf("roles of user 1 = %q, want [Administrator]", roles)
	}

	res, err := a.SignIn(ctx, auth.SignInInput{Email: "admin@vardhan.test", Password: "Correct-Horse-42", IP: "127.0.0.1"})
	if err != nil || res.User != 1 {
		t.Errorf("SignIn with the entered password = (%+v, %v), want user 1", res, err)
	}

	var creates []audit.Entry
	for _, e := range rec.Entries() {
		if e.DocType == "User" && e.Action == "create" {
			creates = append(creates, e)
		}
	}
	if len(creates) != 1 {
		t.Fatalf("audit User create entries = %+v, want 1", creates)
	}
	if creates[0].By != 0 || creates[0].DocID != 1 {
		t.Errorf("audit create entry By %d DocID %d, want By 0 (system) DocID 1", creates[0].By, creates[0].DocID)
	}

	// Second run: refused with AUTH-049's wording (CD-66).
	out.Reset()
	err = cli.AdminCreate(ctx, a, args, strings.NewReader("Correct-Horse-42\nCorrect-Horse-42\n"), &out)
	if err == nil {
		t.Fatal("second AdminCreate succeeded, want an error")
	}
	if !errors.Is(err, auth.ErrUsersExist) {
		t.Errorf("second AdminCreate error = %v, want auth.ErrUsersExist", err)
	}
	if err.Error() != t11UsersExist {
		t.Errorf("second AdminCreate error text = %q, want %q", err.Error(), t11UsersExist)
	}
	if !strings.Contains(out.String(), t11UsersExist) {
		t.Errorf("second AdminCreate output = %q, want %q", out.String(), t11UsersExist)
	}
	if auth.ErrUsersExist.Msg != t11UsersExist {
		t.Errorf("ErrUsersExist text = %q, want %q", auth.ErrUsersExist.Msg, t11UsersExist)
	}
	var n int
	if err := d.SQL().QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("users after the second run = %d, want 1", n)
	}
}
