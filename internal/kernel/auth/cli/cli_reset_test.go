package cli_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/cli"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/testkit"
)

// CD-228, CD-231: --email and the password typed twice reach the reset.
func TestAdminResetPassword(t *testing.T) {
	ctx := context.Background()
	const email, first, temp = "admin@vardhan.test", "Correct-Horse-42", "Temporary-Horse-77"
	a := auth.New(auth.Deps{DB: testkit.NewDB(t), Clock: clock.NewFixed(authtest.T0), Directory: authfake.NewDirectory(), Audit: auditfake.New()})
	if _, err := a.CreateFirstAdmin(ctx, email, "Anil Kapoor", first); err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}
	args := []string{"--email", email}

	var out bytes.Buffer
	err := cli.AdminResetPassword(ctx, a, args, strings.NewReader(temp+"\nsomething else\n"), &out)
	if !errors.Is(err, auth.ErrPasswordMismatch) {
		t.Errorf("mismatch = %v, want auth.ErrPasswordMismatch", err)
	}
	if err := cli.AdminResetPassword(ctx, a, []string{"--no-such-flag"}, strings.NewReader(""), &out); err == nil {
		t.Error("an unknown flag = nil, want an error")
	}
	if err := cli.AdminResetPassword(ctx, a, args, strings.NewReader(""), &out); err == nil {
		t.Error("no password typed = nil, want an error")
	}
	if err := cli.AdminResetPassword(ctx, a, args, strings.NewReader(temp+"\n"), &out); err == nil {
		t.Error("no confirmation typed = nil, want an error")
	}

	out.Reset()
	if err := cli.AdminResetPassword(ctx, a, args, strings.NewReader(temp+"\n"+temp+"\n"), &out); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !strings.Contains(out.String(), email) {
		t.Errorf("output = %q, want it to name %s", out.String(), email)
	}
	res, err := a.SignIn(ctx, auth.SignInInput{Email: email, Password: temp, IP: "127.0.0.1"})
	if err != nil || !res.MustChangePassword {
		t.Errorf("sign-in with the temporary password = %+v, %v, want must-change", res, err)
	}
}
