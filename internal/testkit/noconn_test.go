package testkit_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// Bounds on a stall: NoSecondConn's 3 s deadline and how far past it the
// failure may land on a loaded machine.
const (
	stallMin = 3 * time.Second
	stallMax = 6 * time.Second
)

// fatalTB records Fatalf and stops the goroutine it runs on, as testing.T does.
type fatalTB struct {
	testing.TB
	msg string
}

func (f *fatalTB) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// runFatal runs NoSecondConn on a fatalTB in its own goroutine and returns the
// Fatalf message ("" when it returned) and how long it took.
func runFatal(t *testing.T, d *db.DB, fn func(ctx context.Context) error) (string, time.Duration) {
	t.Helper()
	tb := &fatalTB{TB: t}
	start := time.Now()
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_ = testkit.NoSecondConn(tb, d, fn)
	}()
	<-finished
	return tb.msg, time.Since(start)
}

// wantMaxOpen fails unless the pool's limit is want.
func wantMaxOpen(t *testing.T, d *db.DB, want int) {
	t.Helper()
	if got := d.SQL().Stats().MaxOpenConnections; got != want {
		t.Errorf("MaxOpenConnections = %d, want %d", got, want)
	}
}

// nestedRead takes a second connection inside a write: the bug NoSecondConn finds.
func nestedRead(ctx context.Context, d *db.DB) error {
	return d.Write(ctx, func(*db.Tx) error {
		return d.Read(ctx, func(*db.Tx) error { return nil })
	})
}

// CD-213: a read inside a write is reported near the deadline, and the message
// says what happened and names the test's own function; the pool limit is back.
func TestNoSecondConn_NestedReadFails(t *testing.T) {
	if db.NestCheck {
		t.Skip("nests a Read in a Write on purpose; the nestcheck gate would end the run")
	}
	d := testkit.NewDB(t)
	before := d.SQL().Stats().MaxOpenConnections

	msg, took := runFatal(t, d, func(ctx context.Context) error { return nestedRead(ctx, d) })

	if took < stallMin || took > stallMax {
		t.Errorf("failed after %s, want between %s and %s", took, stallMin, stallMax)
	}
	for _, want := range []string{
		"a second connection was requested while one was held",
		"database/sql.(*DB).conn(",
		"testkit_test.nestedRead",
		"TestNoSecondConn_NestedReadFails",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("Fatalf message lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "connectionOpener") {
		t.Errorf("Fatalf message lists the pool's own goroutines, want only the waiting ones:\n%s", msg)
	}
	wantMaxOpen(t, d, before)
}

// CD-213: fn that ignores its ctx can't hang the helper; the test still fails
// near the deadline and the pool limit is back. Close releases the waiting fn.
func TestNoSecondConn_IgnoresCtxStillFails(t *testing.T) {
	if db.NestCheck {
		t.Skip("nests a Read in a Write on purpose; the nestcheck gate would end the run")
	}
	d := testkit.NewDB(t)
	before := d.SQL().Stats().MaxOpenConnections

	bg := context.Background() // never cancelled, unlike the ctx fn is given
	msg, took := runFatal(t, d, func(context.Context) error { return nestedRead(bg, d) })

	if took < stallMin || took > stallMax {
		t.Errorf("failed after %s, want between %s and %s", took, stallMin, stallMax)
	}
	if !strings.Contains(msg, "a second connection was requested while one was held") {
		t.Errorf("Fatalf message = %q", msg)
	}
	wantMaxOpen(t, d, before)
}

// CD-213: the cut is real: while fn runs the pool holds one connection.
func TestNoSecondConn_CutsPoolToOne(t *testing.T) {
	d := testkit.NewDB(t)
	before := d.SQL().Stats().MaxOpenConnections
	if before == 1 {
		t.Fatalf("setup: pool already at 1 connection")
	}
	var during int
	err := testkit.NoSecondConn(t, d, func(context.Context) error {
		during = d.SQL().Stats().MaxOpenConnections
		return nil
	})
	if err != nil {
		t.Fatalf("NoSecondConn = %v, want nil", err)
	}
	if during != 1 {
		t.Errorf("MaxOpenConnections during fn = %d, want 1", during)
	}
	wantMaxOpen(t, d, before)
}

// CD-213: one connection is enough for a single write: nil, the row is there,
// and the limit is back.
func TestNoSecondConn_SingleWriteReturnsNil(t *testing.T) {
	d := testkit.NewDB(t)
	before := d.SQL().Stats().MaxOpenConnections
	err := testkit.NoSecondConn(t, d, func(ctx context.Context) error {
		return d.Write(ctx, func(tx *db.Tx) error {
			if _, err := tx.SQL().ExecContext(ctx, `CREATE TABLE noconn_probe (id INTEGER PRIMARY KEY)`); err != nil {
				return err
			}
			_, err := tx.SQL().ExecContext(ctx, `INSERT INTO noconn_probe (id) VALUES (1)`)
			return err
		})
	})
	if err != nil {
		t.Fatalf("NoSecondConn(single write) = %v, want nil", err)
	}
	var n int
	if err := d.SQL().QueryRowContext(t.Context(), `SELECT count(*) FROM noconn_probe`).Scan(&n); err != nil || n != 1 {
		t.Errorf("rows after the write = (%d, %v), want (1, nil)", n, err)
	}
	wantMaxOpen(t, d, before)
}

// CD-213: fn's error comes back as the same value, and the limit is back.
func TestNoSecondConn_ReturnsFnErrorUnchanged(t *testing.T) {
	d := testkit.NewDB(t)
	before := d.SQL().Stats().MaxOpenConnections
	want := errors.New("the fn error")
	got := testkit.NoSecondConn(t, d, func(context.Context) error { return want })
	if got != want { //nolint:errorlint // the same value, not a wrapped one
		t.Errorf("NoSecondConn = %v, want the error fn returned, unchanged", got)
	}
	wantMaxOpen(t, d, before)
}

// CD-213: the ctx fn gets ends when the deadline passes, so a waiting call
// gives up instead of holding the transaction.
func TestNoSecondConn_CancelsCtxOnDeadline(t *testing.T) {
	d := testkit.NewDB(t)
	got := make(chan error, 1)
	msg, _ := runFatal(t, d, func(ctx context.Context) error {
		<-ctx.Done()
		got <- ctx.Err()
		return nil
	})
	if msg == "" {
		t.Fatalf("no Fatalf, want one after the deadline")
	}
	if err := <-got; !errors.Is(err, context.Canceled) {
		t.Errorf("ctx.Err() = %v, want context.Canceled", err)
	}
}
