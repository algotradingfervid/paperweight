package main

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// standInCountSQL is COUNT_SQL in scripts/restore-drill.sh.
const standInCountSQL = "SELECT count(*) FROM drill_approvals WHERE state = 'approved'"

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// The writer's in-flight transaction ends by context here; in the drill it
// ends by SIGKILL. Either way only the acknowledged rows may be in the table.
func TestStandInWriter_InflightTransactionNeverCommits(t *testing.T) {
	data := t.TempDir()
	if err := initDB(data); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ack safeBuffer
	done := make(chan error, 1)
	go func() { done <- standInWriter(ctx, &ack, data, 5, 0) }()

	deadline := time.After(30 * time.Second)
	for !strings.Contains(ack.String(), "inflight\n") {
		select {
		case <-deadline:
			t.Fatalf("no inflight marker; acks: %q", ack.String())
		case err := <-done:
			t.Fatalf("writer stopped early: %v", err)
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("writer returned nil after cancel")
	}

	if want := "committed 5\n"; !strings.Contains(ack.String(), want) {
		t.Fatalf("acks %q miss %q", ack.String(), want)
	}
	d, err := db.Open(filepath.Join(data, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if n := count(t, d.SQL(), standInCountSQL); n != 5 {
		t.Fatalf("rows = %d, want 5", n)
	}
	sums, err := treeSums(filepath.Join(data, "files"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 6 {
		t.Fatalf("files = %d, want 5 blobs and the spool file", len(sums))
	}
}

func count(t *testing.T, q *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := q.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
