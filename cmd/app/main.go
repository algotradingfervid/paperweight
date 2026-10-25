// Command app runs Paperweight: serve the web app, apply migrations or print
// the version. WP3 adds admin create, user disable and files gc (CD-18).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/migrations"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// listening, when set (by tests), is called with the address serve bound.
var listening func(addr string)

const usage = `Usage:
  app serve --data <dir> [--addr :8080]   run the web app
  app migrate --data <dir>                apply database migrations
  app version                             print the version
  app help                                show this help
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
		d, err := openDB(ctx, *data)
		if err != nil {
			return err
		}
		return d.Close()
	case "version":
		fmt.Println(version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q. Run `app help` to see the commands.", args[0])
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

// openDB opens <dir>/app.db and applies the migrations.
func openDB(ctx context.Context, dir string) (*db.DB, error) {
	if dir == "" {
		return nil, errors.New("--data is not set. Pass the data directory, for example --data ./data.")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create data directory %s: check the path and its permissions: %w", dir, err)
	}
	d, err := db.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, d, migrations.FS, "goose_db_version"); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

func serve(ctx context.Context, args []string) (err error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	data := fs.String("data", "", "data directory holding app.db")
	addr := fs.String("addr", ":8080", "address to listen on")
	if err := parse(fs, args); err != nil {
		return err
	}
	d, err := openDB(ctx, *data)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Close()) }()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	r := web.NewRouter(web.Options{Logger: logger})
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	l, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: is another program using the address? %w", *addr, err)
	}
	logger.Info("listening", "addr", l.Addr().String())
	if listening != nil {
		listening(l.Addr().String())
	}

	srv := &http.Server{Handler: r, ReadHeaderTimeout: 10 * time.Second}
	failed := make(chan error, 1)
	go func() {
		if err := srv.Serve(l); !errors.Is(err, http.ErrServerClosed) {
			failed <- fmt.Errorf("serve on %s: %w", l.Addr(), err)
		}
		close(failed)
	}()

	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		return err
	}
	return nil
}
