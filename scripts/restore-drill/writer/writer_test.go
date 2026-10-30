//go:build e2e

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// testPad keeps a heavy order quick; the drill uses defaultPad.
const (
	testPad   = 64
	testSmall = 4
	testAcks  = testSmall + 2 // into the heavy orders

	stepLimit   = 30 * time.Second
	replicaHold = 300 * time.Millisecond // far longer than one small approval
)

// ackChecker is the writer's output. At each line it reads the database on its
// own connection, which sees only committed rows, so a print before the commit
// fails here.
type ackChecker struct {
	t      *testing.T
	data   string
	cancel context.CancelFunc

	mu   sync.Mutex
	ids  []int64
	errs []string
}

func (a *ackChecker) Write(p []byte) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var n int
	var id int64
	if _, err := fmt.Sscanf(string(p), "committed %d %d\n", &n, &id); err != nil {
		a.errs = append(a.errs, fmt.Sprintf("bad line %q: %v", p, err))
		return len(p), nil
	}
	d, err := db.Open(filepath.Join(a.data, "app.db"))
	if err != nil {
		a.errs = append(a.errs, err.Error())
		return len(p), nil
	}
	defer func() { _ = d.Close() }()
	var state string
	if err := d.SQL().QueryRow("SELECT state FROM fixture_order WHERE id = ?", id).Scan(&state); err != nil {
		a.errs = append(a.errs, fmt.Sprintf("order %d: %v", id, err))
	} else if state != "approved" {
		a.errs = append(a.errs, fmt.Sprintf("order %d printed in state %q", id, state))
	}
	a.ids = append(a.ids, id)
	if len(a.ids) == testAcks {
		a.cancel()
	}
	return len(p), nil
}

func TestWrite_RealApprovalsPrintedAfterCommit(t *testing.T) {
	data := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	acks := &ackChecker{t: t, data: data, cancel: cancel}

	err := write(ctx, acks, data, data, testSmall, testPad)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("write = %v, want it to run until cancelled", err)
	}
	if len(acks.errs) > 0 {
		t.Fatalf("acknowledgements: %s", strings.Join(acks.errs, "; "))
	}

	d, err := db.Open(filepath.Join(data, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	q := d.SQL()
	var approved []int64
	rows, err := q.Query("SELECT id FROM fixture_order WHERE state = 'approved' ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		approved = append(approved, id)
	}
	if !slices.Equal(approved, acks.ids) {
		t.Fatalf("approved %v, printed %v", approved, acks.ids)
	}

	// Each approval went through the engine: submitted by the requester and
	// approved by a different user, with an approval request behind it.
	if n := count(t, q, `SELECT count(*) FROM audit_log a JOIN audit_log b ON a.doc_id = b.doc_id
		WHERE a.doc_type = 'fixture.Order' AND b.doc_type = 'fixture.Order'
		AND a.action = 'submit' AND b.action = 'approve' AND a.user_id != b.user_id`); n != len(approved) {
		t.Fatalf("%d submit and approve pairs by two users, want %d", n, len(approved))
	}

	// Attachments are real uploads: a row and a blob with that hash.
	hashes := map[string]bool{}
	files := filepath.Join(data, "files")
	spooled := 0
	err = filepath.WalkDir(files, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		if filepath.Base(filepath.Dir(path)) == "tmp" {
			spooled++
		} else {
			hashes[filepath.Base(path)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if spooled != 1 {
		t.Fatalf("%d files in files/tmp, want the 1 half-finished upload", spooled)
	}
	var want []string
	for i := attachEvery; i <= len(approved); i += attachEvery {
		want = append(want, strconv.Itoa(i))
	}
	if n := count(t, q, "SELECT count(*) FROM attachment"); n != len(want) || len(hashes) != len(want) {
		t.Fatalf("%d attachment rows, %d blobs, want %d of each", n, len(hashes), len(want))
	}

	// Orders past n are the heavy ones.
	last := approved[len(approved)-1]
	if n := count(t, q, fmt.Sprintf("SELECT count(*) FROM fixture_order_line WHERE parent_id = %d", last)); n != heavyLines {
		t.Fatalf("heavy order has %d lines, want %d", n, heavyLines)
	}
	if n := count(t, q, fmt.Sprintf("SELECT count(*) FROM fixture_order_line WHERE parent_id = %d", approved[0])); n != 1 {
		t.Fatalf("small order has %d lines, want 1", n)
	}
}

// firstAck reports the writer's first line.
type firstAck struct {
	once sync.Once
	seen chan struct{}
}

func (f *firstAck) Write(p []byte) (int, error) {
	f.once.Do(func() { close(f.seen) })
	return len(p), nil
}

// Setup has its own limit, so the hold starts only once write has reached the
// wait: the half-finished upload is setup's last step.
func TestWrite_WaitsForTheReplica(t *testing.T) {
	data := t.TempDir()
	replica := filepath.Join(data, "replica")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ack := &firstAck{seen: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- write(ctx, ack, data, replica, testSmall, testPad) }()

	setup := time.After(stepLimit)
	for {
		spooled, err := filepath.Glob(filepath.Join(data, "files", "tmp", "up-*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(spooled) > 0 {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("write ended during setup: %v", err)
		case <-setup:
			t.Fatal("setup didn't finish")
		case <-time.After(time.Millisecond):
		}
	}

	select {
	case <-ack.seen:
		t.Fatal("approved an order before the replica existed")
	case err := <-done:
		t.Fatalf("write ended during the wait: %v", err)
	case <-time.After(replicaHold):
	}
	d, err := db.Open(filepath.Join(data, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if n := count(t, d.SQL(), "SELECT count(*) FROM fixture_order"); n != 0 {
		t.Fatalf("%d orders before the replica", n)
	}

	if err := os.Mkdir(replica, 0o700); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ack.seen:
	case err := <-done:
		t.Fatalf("write ended after the replica appeared: %v", err)
	case <-time.After(stepLimit):
		t.Fatal("no approval after the replica appeared")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("write = %v, want it to run until cancelled", err)
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
