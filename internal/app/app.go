// Package app is the composition root: it builds every real service, joins
// them through the cross-package adapters and serves them. Nothing else
// constructs services (WP3), and it is the only package that imports more
// than one kernel implementation package.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/views"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/company"
	companyweb "github.com/vardhan/internaltools/internal/masters/company/web"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/migrations"
)

const (
	dbFile          = "app.db"
	readHeaderLimit = 10 * time.Second
	// shutdownLimit bounds a graceful stop. It stays under the 5 s net/http
	// waits for a connection that never sent a request.
	shutdownLimit = 3 * time.Second
)

// seedOptions is what startup and a rights reset seed with (CD-10, CD-158).
var seedOptions = auth.SeedOptions{LinkedMasterRead: true}

// Config is what an App needs. Clock nil is the real clock, LogOutput nil is
// os.Stdout. Prepare runs after the migrations, for test and e2e builds that
// add their own tables; the release leaves it nil. OnListen is called with the
// bound address.
type Config struct {
	DataDir, Addr      string
	Clock              clock.Clock
	TrustedOrigins     []string
	Secure, TrustProxy bool
	Prepare            func(ctx context.Context, d *db.DB) error
	OnListen           func(addr string)
	LogOutput          io.Writer
}

// App is the whole application over one data directory.
type App struct {
	cfg       Config
	db        *db.DB
	logger    *slog.Logger
	auth      *auth.Auth
	sessions  *auth.SessionStore
	srv       *authweb.Server
	engine    doc.Engine
	approvals *approval.Approvals
	files     *files.Store
	masters   *allmasters.Services
	router    chi.Router
	screens   map[string]bool // the mounted master list paths
	installed atomic.Bool     // a user exists; once true it stays true
	jobEvery  time.Duration   // how often Run does the jobs
}

// engineRef lets AUTH, APR and FILE hold the engine, which needs them, before
// it exists.
type engineRef struct{ doc.Engine }

// infoRef is an InfoSource set once the masters it reads exist.
type infoRef struct{ doctype.InfoSource }

// directoryRef is MST's directory, which needs AUTH, before it exists.
type directoryRef struct{ auth.Directory }

// roleDirectory is AUTH's role list as APR's RoleDirectory. Roles have no
// disabled state, so a role that exists is active. APR asks after its
// Administrator check, for the signed-in user.
type roleDirectory struct{ auth *auth.Auth }

func (r roleDirectory) RoleStatus(ctx context.Context, name string) (exists, active bool, err error) {
	p, _ := auth.PrincipalFrom(ctx)
	roles, _, err := r.auth.RightsMatrix(ctx, p)
	if err != nil {
		return false, false, err
	}
	found := slices.ContainsFunc(roles, func(role auth.RoleInfo) bool { return role.Name == name })
	return found, found, nil
}

// OpenDB opens <dir>/app.db, creating the directory, and applies the
// migrations.
func OpenDB(ctx context.Context, dir string) (*db.DB, error) {
	if dir == "" {
		return nil, errors.New("--data is not set. Pass the data directory, for example --data ./data.")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create data directory %s: check the path and its permissions: %w", dir, err)
	}
	d, err := db.Open(filepath.Join(dir, dbFile))
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, d, migrations.FS, "goose_db_version"); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

// New opens the data directory, migrates, runs cfg.Prepare, seeds and builds
// every service and the router.
func New(ctx context.Context, cfg Config) (a *App, err error) {
	if cfg.Clock == nil {
		cfg.Clock = clock.Real{}
	}
	if cfg.LogOutput == nil {
		cfg.LogOutput = os.Stdout
	}
	d, err := OpenDB(ctx, cfg.DataDir)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = d.Close()
		}
	}()
	if cfg.Prepare != nil {
		if err := cfg.Prepare(ctx, d); err != nil {
			return nil, err
		}
	}

	a = &App{cfg: cfg, db: d, logger: slog.New(slog.NewJSONHandler(cfg.LogOutput, nil)), jobEvery: time.Hour}
	refs, fileInfo, dir := &engineRef{}, &infoRef{}, &directoryRef{}
	a.auth = auth.New(auth.Deps{DB: d, Clock: cfg.Clock, Directory: dir, Audit: audit.Default, Refs: refs, Info: refs})
	namer := naming.New(d, cfg.Clock, a.auth, audit.Default)
	if a.files, err = files.New(files.Config{
		DataDir: cfg.DataDir, DB: d, Auth: a.auth, Info: fileInfo, Clock: cfg.Clock, Audit: audit.Default,
		Policies: map[string]files.Policy{company.CompanyDocType: files.LogoPolicy},
		Logger:   a.logger,
	}); err != nil {
		return nil, err
	}
	a.approvals = approval.New(approval.Deps{
		DB: d, Clock: cfg.Clock, Auth: a.auth, Info: refs, Directory: dir, Audit: audit.Default, Roles: roleDirectory{a.auth},
	})
	a.engine = doc.NewEngine(doc.Deps{
		DB: d, Clock: cfg.Clock, Auth: a.auth, Naming: namer, Approval: a.approvals, Files: a.files, Audit: audit.Default,
	})
	refs.Engine = a.engine
	a.masters = allmasters.New(mstkit.Env{DB: d, Auth: a.auth, Engine: a.engine, Auditor: audit.Default, Clock: cfg.Clock})
	dir.Directory = a.masters.Directory
	fileInfo.InfoSource = companyweb.Info(a.masters.Company, a.engine)
	a.masters.Letterhead.LogoURL = companyweb.LogoURL(a.masters.Company, a.files)

	if err := a.auth.SeedDefaults(ctx, seedOptions); err != nil {
		return nil, err
	}
	for _, dt := range doctype.All() {
		if dt.Series == "" {
			continue
		}
		if err := namer.EnsureSeries(ctx, dt.Name, dt.Label, dt.Series); err != nil {
			return nil, err
		}
	}

	sessions := auth.DefaultSessionConfig(cfg.Secure)
	a.sessions = auth.NewSessionStore(d, cfg.Clock, sessions)
	sm := auth.NewSessionManager(a.sessions, sessions)
	a.srv = authweb.New(a.auth, a.sessions, sm, views.Default{}, authweb.Config{
		Secure: cfg.Secure, TrustProxy: cfg.TrustProxy, Seed: seedOptions, PickerURL: pickerURL,
	})
	a.router = a.routes(sm, namer)
	return a, nil
}

// Handler is the router; it also satisfies chi.Routes.
func (a *App) Handler() http.Handler { return a.router }

// Close closes the database.
func (a *App) Close() error { return a.db.Close() }

// DB is the database.
func (a *App) DB() *db.DB { return a.db }

// Auth is the real AUTH service.
func (a *App) Auth() *auth.Auth { return a.auth }

// Engine is the document engine.
func (a *App) Engine() doc.Engine { return a.engine }

// Approvals is the real APR service.
func (a *App) Approvals() *approval.Approvals { return a.approvals }

// Masters is every master's service.
func (a *App) Masters() *allmasters.Services { return a.masters }

// Files is the file store.
func (a *App) Files() *files.Store { return a.files }

// Run listens on cfg.Addr and serves, with the hourly jobs alongside, until
// ctx ends; it then shuts down gracefully. The kernel packages log through
// slog's default, so a running app points that at its own JSON logger too.
func (a *App) Run(ctx context.Context) error {
	slog.SetDefault(a.logger)
	l, err := net.Listen("tcp", a.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: is another program using the address? %w", a.cfg.Addr, err)
	}
	a.logger.Info("listening", "addr", l.Addr().String())
	if a.cfg.OnListen != nil {
		a.cfg.OnListen(l.Addr().String())
	}

	jobs := make(chan struct{})
	jobsCtx, stopJobs := context.WithCancel(ctx)
	defer func() {
		stopJobs()
		<-jobs
	}()
	go func() {
		defer close(jobs)
		tick := time.NewTicker(a.jobEvery)
		defer tick.Stop()
		for {
			select {
			case <-jobsCtx.Done():
				return
			case <-tick.C:
				_ = a.RunJobs(jobsCtx) // RunJobs logged each failure
			}
		}
	}()

	srv := &http.Server{Handler: a.router, ReadHeaderTimeout: readHeaderLimit}
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
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownLimit)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		a.logger.Warn("shutdown cut connections still open", "err", err)
		return srv.Close()
	}
	return nil
}

// RunJobs is one pass of the hourly jobs: unreferenced files past the grace
// period, expired sessions and expired lockouts. Every job runs; each failure
// is logged and returned joined.
func (a *App) RunJobs(ctx context.Context) error {
	var errs []error
	note := func(job string, err error) {
		if err != nil {
			a.logger.ErrorContext(ctx, "job failed", "job", job, "err", err)
			errs = append(errs, fmt.Errorf("%s: %w", job, err))
		}
	}
	_, err := a.files.GC(ctx, files.GCGrace)
	note("files gc", err)
	_, err = a.sessions.Sweep(ctx)
	note("session sweep", err)
	_, err = a.auth.SweepLockouts(ctx)
	note("lockout sweep", err)
	return errors.Join(errs...)
}

// Core is what the commands other than serve need: the migrated and seeded
// database, the real Auth and the file store, with no web stack.
type Core struct {
	Auth  *auth.Auth
	db    *db.DB
	files *files.Store
}

// OpenCore opens the data directory for a command.
func OpenCore(ctx context.Context, dir string) (c *Core, err error) {
	d, err := OpenDB(ctx, dir)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = d.Close()
		}
	}()
	a := auth.New(auth.Deps{DB: d, Clock: clock.Real{}, Audit: audit.Default})
	if err := a.SeedDefaults(ctx, seedOptions); err != nil {
		return nil, err
	}
	store, err := files.New(files.Config{DataDir: dir, DB: d, Auth: a, Clock: clock.Real{}, Audit: audit.Default})
	if err != nil {
		return nil, err
	}
	return &Core{Auth: a, db: d, files: store}, nil
}

// Close closes the database.
func (c *Core) Close() error { return c.db.Close() }

// FilesGC deletes the blobs and temp files no row uses, past the grace period.
func (c *Core) FilesGC(ctx context.Context, w io.Writer) error {
	rep, err := c.files.GC(ctx, files.GCGrace)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "Deleted %d files and %d temp files, kept %d.\n", rep.Deleted, rep.TempDeleted, rep.Kept)
	return nil
}

// FilesCheck prints what doesn't match between the rows and the blobs; any
// finding is an error, so the exit code comes from WriteCheckReport.
func (c *Core) FilesCheck(ctx context.Context, w io.Writer) error {
	rep, err := c.files.Check(ctx)
	if err != nil {
		return err
	}
	if files.WriteCheckReport(w, rep) != 0 {
		return errors.New("files check found problems. See the lines above.")
	}
	return nil
}
