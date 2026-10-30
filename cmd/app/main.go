// Command app runs Paperweight: serve the web app, apply migrations, create or
// recover an administrator, disable a user, look after the attachment files or
// print the version (CD-18, CD-228).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/kernel/auth/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// listening, when set (by tests), is called with the address serve bound.
var listening func(addr string)

const usage = `Usage:
  app serve --data <dir> [--addr :8080] [--secure] [--trust-proxy]
            [--trusted-origin <origin>]...          run the web app
  app migrate --data <dir>                          apply database migrations
  app admin create --data <dir> --email <e> --name <n>
                                                    create the first administrator
  app admin reset-password --data <dir> --email <e> set an administrator's temporary password
  app user disable --data <dir> --email <e>         disable a user
  app files gc --data <dir>                         delete unused attachment files
  app files check --data <dir>                      compare attachment rows and files
  app version                                       print the version
  app help                                          show this help
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		stop()
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return errors.New("no command given. Run `app help` to see the commands.")
	}
	switch args[0] {
	case "serve":
		return serve(ctx, args[1:])
	case "migrate":
		fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
		data := fs.String("data", "", "data directory holding app.db")
		if err := parse(fs, args[1:]); err != nil {
			return err
		}
		d, err := app.OpenDB(ctx, *data)
		if err != nil {
			return err
		}
		return d.Close()
	case "admin", "user", "files":
		return subcommand(ctx, args[0], args[1:])
	case "version":
		fmt.Println(version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		return unknown(args[0])
	}
}

// parse parses flags and rejects positional arguments.
func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q. Run `app help` to see the commands.", fs.Arg(0))
	}
	return nil
}

// subcommand runs `app <group> <name>`. Each needs the data directory; the
// password prompts of the admin commands read standard input.
func subcommand(ctx context.Context, group string, args []string) (err error) {
	if len(args) == 0 {
		return unknown(group)
	}
	name := group + " " + args[0]
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	data := fs.String("data", "", "data directory holding app.db")
	email := fs.String("email", "", "the user's email")
	adminName := fs.String("name", "", "the administrator's name")
	if err := parse(fs, args[1:]); err != nil {
		return err
	}
	switch name {
	case "admin create", "admin reset-password", "user disable", "files gc", "files check":
	default:
		return unknown(name)
	}
	c, err := app.OpenCore(ctx, *data)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, c.Close()) }()
	switch name {
	case "admin create":
		return cli.AdminCreate(ctx, c.Auth, []string{"--email", *email, "--name", *adminName}, os.Stdin, os.Stdout)
	case "admin reset-password":
		return cli.AdminResetPassword(ctx, c.Auth, []string{"--email", *email}, os.Stdin, os.Stdout)
	case "user disable":
		return cli.UserDisable(ctx, c.Auth, []string{*email}, os.Stdout)
	case "files gc":
		return c.FilesGC(ctx, os.Stdout)
	default:
		return c.FilesCheck(ctx, os.Stdout)
	}
}

func unknown(name string) error {
	return fmt.Errorf("unknown command %q. Run `app help` to see the commands.", name)
}

func serve(ctx context.Context, args []string) (err error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	data := fs.String("data", "", "data directory holding app.db")
	addr := fs.String("addr", ":8080", "address to listen on")
	secure := fs.Bool("secure", false, "served over HTTPS on a public host: Secure cookies, HSTS, /setup closed")
	trustProxy := fs.Bool("trust-proxy", false, "take the client IP from X-Forwarded-For")
	var origins []string
	fs.Func("trusted-origin", "an origin allowed to post here (repeatable)", func(v string) error {
		origin := strings.TrimSpace(v)
		if err := http.NewCrossOriginProtection().AddTrustedOrigin(origin); err != nil {
			return fmt.Errorf("%q isn't an origin: use the form https://host[:port]", v)
		}
		origins = append(origins, origin)
		return nil
	})
	if err := parse(fs, args); err != nil {
		return err
	}
	a, err := app.New(ctx, app.Config{
		DataDir: *data, Addr: *addr, Secure: *secure, TrustProxy: *trustProxy, TrustedOrigins: origins, OnListen: listening,
	})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.Close()) }()
	return a.Run(ctx)
}
