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

const (
	// noConnDeadline is how long fn may run before NoSecondConn calls it stuck.
	noConnDeadline = 3 * time.Second
	// noConnFrame is the frame of a goroutine waiting for a pool connection;
	// the "(" leaves out the pool's own connectionOpener and connectionCleaner.
	noConnFrame = "database/sql.(*DB).conn("
)

// NoSecondConn runs fn with d's pool cut to one connection and returns fn's
// error unchanged (CD-213). A transaction that asks for a second connection
// while holding the only one never gets it, so after noConnDeadline the test
// fails with the stacks of the goroutines waiting in the pool, which name the
// call site. fn runs in its own goroutine, so it must not call t.Fatal; it
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

	select {
	case err := <-done:
		return err
	case <-time.After(noConnDeadline):
	}
	stacks := waitingStacks() // before cancel, which frees the waiters
	cancel()
	t.Fatalf("testkit.NoSecondConn: a second connection was requested while one was held: fn had not returned after %s; goroutines waiting for a connection:\n%s",
		noConnDeadline, stacks)
	return nil
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
