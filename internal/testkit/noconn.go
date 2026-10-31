package testkit

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/platform/db"
)

var (
	// noConnDeadline is how long fn may run before NoSecondConn looks for a
	// goroutine waiting for a connection.
	noConnDeadline = 3 * time.Second
	// noConnCap is how long fn may run when none is waiting: a slow fn on a
	// loaded machine passes, a hung one still fails.
	noConnCap = 2 * time.Minute
)

const (
	// noConnRecheck is how often the waiters are looked for after the deadline.
	noConnRecheck = time.Second
	// noConnFrame is the frame of a goroutine waiting for a pool connection;
	// the "(" leaves out the pool's own connectionOpener and connectionCleaner.
	noConnFrame = "database/sql.(*DB).conn("
)

// NoSecondConn runs fn with d's pool cut to one connection and returns fn's
// error unchanged (CD-213). A transaction that asks for a second connection
// while holding the only one never gets it, so once noConnDeadline has passed
// and a goroutine is waiting in the pool, the test fails with the waiters'
// stacks, which name the call site. With no waiter fn may run on until
// noConnCap (M0-51). fn runs in its own goroutine, so it must not call t.Fatal; it
// should pass the ctx it gets down, but NoSecondConn returns even if it doesn't.
func NoSecondConn(t testing.TB, d *db.DB, fn func(ctx context.Context) error) error {
	t.Helper()
	pool := d.SQL()
	defer pool.SetMaxOpenConns(pool.Stats().MaxOpenConnections)
	pool.SetMaxOpenConns(1)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- fn(ctx) }()

	hardCap := time.After(noConnCap)
	select {
	case err := <-done:
		return err
	case <-time.After(noConnDeadline):
	}
	tick := time.NewTicker(noConnRecheck)
	defer tick.Stop()
	for {
		if stacks := waitingStacks(); stacks != "" { // before cancel, which frees the waiters
			cancel()
			t.Fatalf("testkit.NoSecondConn: a second connection was requested while one was held: fn had not returned after %s; goroutines waiting for a connection:\n%s",
				noConnDeadline, stacks)
			return nil
		}
		select {
		case err := <-done:
			return err
		case <-tick.C:
		case <-hardCap:
			cancel()
			t.Fatalf("testkit.NoSecondConn: fn had not returned after %s; no goroutine was waiting for a connection", noConnCap)
			return nil
		}
	}
}

// waitingStacks returns the stacks of all goroutines waiting for a connection.
func waitingStacks() string {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	var waiting []string
	for s := range bytes.SplitSeq(buf, []byte("\n\n")) {
		if bytes.Contains(s, []byte(noConnFrame)) {
			waiting = append(waiting, string(s))
		}
	}
	return strings.Join(waiting, "\n\n")
}
