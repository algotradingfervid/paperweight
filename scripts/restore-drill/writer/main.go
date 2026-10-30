//go:build e2e

// Command writer is the process the restore drill crashes. It opens the real
// app with the fixture DocTypes, creates a requester and an approver through
// AUTH's admin API, and runs fixture.Order approvals through the engine until
// it is killed, printing "committed <n> <order id>" to stdout only after each
// approval's commit. Past --n the orders are heavy enough that their save
// spills to the WAL, so the drill can kill it inside an open transaction.
//
// It starts approving only once --replica exists: a writer at full speed
// starves Litestream's first write. Litestream retries and recovers, but one
// stopped before the retry has replicated nothing.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	// limit stops a writer the drill failed to kill.
	limit = 2 * time.Minute
	// replicaPoll is how often the writer looks for the replica.
	replicaPoll = 10 * time.Millisecond
	// attachEvery is how often an approval carries an attachment.
	attachEvery = 4
	// heavyLines is the engine's line limit. At defaultPad bytes of line text
	// a save is 32 MB, well past SQLite's default 2 MB page cache, so its pages
	// sit in the WAL with no commit frame for as long as it runs.
	heavyLines = 500
	defaultPad = 64 << 10

	password   = "Correct-Horse-42"
	orderDate  = "2026-10-24"
	orderTotal = "94164.00"
)

// seedTB is what fixture.SeedMasters needs from a test.
type seedTB struct{ testing.TB }

func (seedTB) Helper() {}

func (seedTB) Fatalf(format string, args ...any) { panic(fmt.Sprintf(format, args...)) }

func main() {
	data := flag.String("data", "", "data directory")
	n := flag.Int("n", 0, "approvals to run before the orders turn heavy")
	replica := flag.String("replica", "", "path that exists once Litestream has synced")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	if err := write(ctx, os.Stdout, *data, *replica, *n, defaultPad); err != nil {
		fmt.Fprintln(os.Stderr, "writer:", err)
		os.Exit(1)
	}
}

// run is one writer's world.
type run struct {
	app                 *app.App
	requester, approver auth.Principal
	masters             fixture.Masters
}

// write sets up, waits for replica, then approves orders until ctx ends. Orders
// past n carry heavyLines lines of pad bytes each.
func write(ctx context.Context, ack io.Writer, data, replica string, n, pad int) (err error) {
	a, err := app.New(ctx, app.Config{
		DataDir: data, Addr: "127.0.0.1:0", LogOutput: os.Stderr,
		Prepare: func(ctx context.Context, d *db.DB) error {
			return db.Migrate(ctx, d, fixture.MigrationsFS, "goose_fixture")
		},
	})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.Close()) }()
	r, err := newRun(ctx, a)
	if err != nil {
		return err
	}
	// An upload still in progress at the crash: never backed up.
	if _, err := a.Files().Spool(strings.NewReader("half an upload"), files.MaxFileBytes); err != nil {
		return err
	}
	if err := awaitPath(ctx, replica); err != nil {
		return err
	}
	for i := 1; ; i++ {
		lines, text := 1, ""
		if i > n {
			lines, text = heavyLines, strings.Repeat("x", pad)
		}
		id, err := r.approve(ctx, i, lines, text)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(ack, "committed %d %d\n", i, id); err != nil {
			return err
		}
	}
}

func awaitPath(ctx context.Context, path string) error {
	for {
		_, err := os.Stat(path)
		if err == nil {
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for %s: %w", path, ctx.Err())
		case <-time.After(replicaPoll):
		}
	}
}

func newRun(ctx context.Context, a *app.App) (*run, error) {
	r := &run{app: a}
	adminID, err := a.Auth().CreateFirstAdmin(ctx, "admin@drill.test", "Drill Admin", password)
	if err != nil {
		return nil, err
	}
	admin, _, err := a.Auth().LoadPrincipal(ctx, adminID)
	if err != nil {
		return nil, err
	}
	users := []struct {
		email, name, role string
		into              *auth.Principal
	}{
		{"requester@drill.test", "Drill Requester", "Purchase User", &r.requester},
		{"approver@drill.test", "Drill Approver", "Purchase Manager", &r.approver},
	}
	for _, u := range users {
		id, _, err := a.Auth().CreateUser(ctx, admin, auth.NewUser{Email: u.email, Name: u.name, Password: password, Roles: []string{u.role}})
		if err != nil {
			return nil, err
		}
		if *u.into, _, err = a.Auth().LoadPrincipal(ctx, id); err != nil {
			return nil, err
		}
	}
	// The rule form's request carries the principal; the role check reads it.
	_, err = a.Approvals().CreateRule(auth.WithPrincipal(ctx, admin), admin, approval.RuleInput{
		DocType: fixture.Order, ApproverType: approval.ApproverRole, Role: "Purchase Manager",
	})
	if err != nil {
		return nil, err
	}
	r.masters = fixture.SeedMasters(seedTB{}, a.DB())
	return r, nil
}

// approve takes one order from a new draft to approved, each step its own
// commit, and returns its id.
func (r *run) approve(ctx context.Context, i, lines int, text string) (int64, error) {
	in := doc.ActionInput{Values: map[string]string{
		"supplier_id": strconv.FormatInt(r.masters.Party["SHREE"], 10),
		"project_id":  strconv.FormatInt(r.masters.Project["PRJ-METRO"], 10),
		"order_date":  orderDate, "amount": orderTotal,
	}}
	for range lines {
		in.Lines = append(in.Lines, map[string]string{
			"item_id": strconv.FormatInt(r.masters.Item["MS-ANGLE-50"], 10), "qty": "10", "rate": "62.5000", "description": text,
		})
	}
	engine := r.app.Engine()
	saved, err := engine.Do(ctx, r.requester, fixture.Order, 0, doc.ActionSave, in)
	if err != nil {
		return 0, err
	}
	if i%attachEvery == 0 {
		if err := r.attach(ctx, saved.ID, i); err != nil {
			return 0, err
		}
	}
	submitted, err := engine.Do(ctx, r.requester, fixture.Order, saved.ID, doc.ActionSubmit, doc.ActionInput{Version: saved.Version})
	if err != nil {
		return 0, err
	}
	if submitted.State != doc.StatePending {
		return 0, fmt.Errorf("order %d is %s after submit, want it waiting for approval", saved.ID, submitted.State)
	}
	if _, err := engine.Do(ctx, r.approver, fixture.Order, saved.ID, doc.ActionApprove, doc.ActionInput{Version: submitted.Version}); err != nil {
		return 0, err
	}
	return saved.ID, nil
}

// attach uploads a file unique to approval i the way the upload handler does:
// spool outside the transaction, attach inside it.
func (r *run) attach(ctx context.Context, id int64, i int) error {
	f, err := r.app.Files().Spool(strings.NewReader(fmt.Sprintf("%%PDF-1.4\n%% approval %d\n", i)), files.MaxFileBytes)
	if err != nil {
		return err
	}
	return r.app.DB().Write(ctx, func(tx *db.Tx) error {
		_, err := r.app.Files().Attach(ctx, tx, r.requester, auth.RecordRef{DocType: fixture.Order, ID: id}, "quote.pdf", f)
		return err
	})
}
