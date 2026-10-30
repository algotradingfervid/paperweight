// Package cli holds AUTH's command-line commands (CD-18); cmd/app wires them.
package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// AdminCreate creates the first Administrator from --email and --name, reading
// the password twice from in (without echo when in is a terminal).
func AdminCreate(ctx context.Context, a *auth.Auth, args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("admin create", flag.ContinueOnError)
	fs.SetOutput(out)
	email := fs.String("email", "", "the administrator's email")
	name := fs.String("name", "", "the administrator's name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	read := passwordReader(in, out)
	pw, err := read("Password: ")
	if err != nil {
		return err
	}
	confirm, err := read("Confirm password: ")
	if err != nil {
		return err
	}
	if pw != confirm {
		return auth.ErrPasswordMismatch
	}
	id, err := a.CreateFirstAdmin(ctx, *email, *name, pw)
	if err != nil {
		if errors.Is(err, auth.ErrUsersExist) {
			_, _ = fmt.Fprintln(out, err.Error())
		}
		return err
	}
	_, _ = fmt.Fprintf(out, "Created administrator %s (user %d).\n", auth.NormalizeEmail(*email), id)
	return nil
}

// AdminResetPassword gives the Administrator named by --email a temporary
// password, reading it twice from in like AdminCreate, so a sole administrator
// who forgets a password recovers without SQL (CD-228).
func AdminResetPassword(ctx context.Context, a *auth.Auth, args []string, in io.Reader, out io.Writer) error {
	fs := flag.NewFlagSet("admin reset-password", flag.ContinueOnError)
	fs.SetOutput(out)
	email := fs.String("email", "", "the administrator's email")
	if err := fs.Parse(args); err != nil {
		return err
	}
	read := passwordReader(in, out)
	pw, err := read("Temporary password: ")
	if err != nil {
		return err
	}
	confirm, err := read("Confirm password: ")
	if err != nil {
		return err
	}
	if pw != confirm {
		return auth.ErrPasswordMismatch
	}
	if err := a.ResetAdminPassword(ctx, *email, pw); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "Set a temporary password for %s. It must be changed at the next sign-in.\n", auth.NormalizeEmail(*email))
	return nil
}

// passwordReader returns a prompt-and-read function: term.ReadPassword when
// in is a terminal, else one line at a time (the line ending removed, the
// password itself never trimmed).
func passwordReader(in io.Reader, out io.Writer) func(prompt string) (string, error) {
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return func(prompt string) (string, error) {
			_, _ = fmt.Fprint(out, prompt)
			b, err := term.ReadPassword(int(f.Fd()))
			_, _ = fmt.Fprintln(out)
			return string(b), err
		}
	}
	br := bufio.NewReader(in)
	return func(string) (string, error) {
		line, err := br.ReadString('\n')
		if err != nil && (!errors.Is(err, io.EOF) || line == "") {
			return "", err
		}
		line = strings.TrimSuffix(line, "\n")
		return strings.TrimSuffix(line, "\r"), nil
	}
}

// UserDisable disables the user whose email is the one positional argument.
func UserDisable(ctx context.Context, a *auth.Auth, args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: user disable <email>")
	}
	if err := a.DisableUserByEmail(ctx, args[0], "disabled from command line"); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "Disabled %s.\n", auth.NormalizeEmail(args[0]))
	return nil
}
