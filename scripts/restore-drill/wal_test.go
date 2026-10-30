package main

import (
	"context"
	osexec "os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// spillRows of spillRowSize bytes is 8 MB, past SQLite's default 2 MB page
// cache, so an open transaction's pages reach the WAL with no commit frame.
const (
	spillRows    = 2000
	spillRowSize = 4096
)

func openWAL(t *testing.T) (*db.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := d.SQL().Exec("CREATE TABLE pad (body BLOB)"); err != nil {
		t.Fatal(err)
	}
	return d, path + "-wal"
}

// holdSpilled opens a write transaction, fills it past the page cache and
// keeps it open until release is closed; done then gets the commit's result.
func holdSpilled(t *testing.T, d *db.DB) (release chan struct{}, done chan error) {
	t.Helper()
	release, done, ready := make(chan struct{}), make(chan error, 1), make(chan struct{})
	go func() {
		done <- d.Write(context.Background(), func(tx *db.Tx) error {
			for range spillRows {
				if _, err := tx.SQL().ExecContext(context.Background(), "INSERT INTO pad VALUES (?)", make([]byte, spillRowSize)); err != nil {
					close(ready)
					return err
				}
			}
			close(ready)
			<-release
			return nil
		})
	}()
	<-ready
	return release, done
}

func finish(t *testing.T, release chan struct{}, done chan error) {
	t.Helper()
	close(release)
	if err := <-done; err != nil {
		t.Error(err)
	}
}

func frames(t *testing.T, wal string) int {
	t.Helper()
	n, err := uncommittedFrames(wal)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestUncommittedFrames(t *testing.T) {
	d, wal := openWAL(t)
	if n := frames(t, wal); n != 0 {
		t.Fatalf("after commits: %d uncommitted frames, want 0", n)
	}
	release, done := holdSpilled(t, d)
	if n := frames(t, wal); n == 0 {
		t.Fatal("open spilled transaction: 0 uncommitted frames")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := frames(t, wal); n != 0 {
		t.Fatalf("after its commit: %d uncommitted frames, want 0", n)
	}
}

// A truncating checkpoint leaves an empty file; a restart leaves old frames
// behind the new header, whose salts no longer match.
func TestUncommittedFrames_OldGenerationIgnored(t *testing.T) {
	d, wal := openWAL(t)
	var a, b, c int
	if err := d.SQL().QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&a, &b, &c); err != nil {
		t.Fatal(err)
	}
	if n := frames(t, wal); n != 0 {
		t.Fatalf("empty WAL: %d uncommitted frames", n)
	}
	release, done := holdSpilled(t, d)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := d.SQL().QueryRow("PRAGMA wal_checkpoint(RESTART)").Scan(&a, &b, &c); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL().Exec("INSERT INTO pad VALUES (x'00')"); err != nil {
		t.Fatal(err)
	}
	if n := frames(t, wal); n != 0 {
		t.Fatalf("one commit over old frames: %d uncommitted frames, want 0", n)
	}
}

func TestUncommittedFrames_MissingWAL(t *testing.T) {
	if _, err := uncommittedFrames(filepath.Join(t.TempDir(), "absent-wal")); err == nil {
		t.Fatal("no error for a missing WAL")
	}
}

func sleeper(t *testing.T) *osexec.Cmd {
	t.Helper()
	cmd := osexec.CommandContext(context.Background(), "sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// ackFile holds n acknowledgements.
func ackFile(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "acks")
	writeFile(t, path, strings.Repeat("committed\n", n))
	return path
}

func TestKillInflight_KillsWithFramesOnDisk(t *testing.T) {
	d, wal := openWAL(t)
	release, done := holdSpilled(t, d)
	defer finish(t, release, done)
	cmd := sleeper(t)
	var out strings.Builder
	if err := killInflight(context.Background(), &out, ackFile(t, 2), 2, wal, cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil || !strings.Contains(err.Error(), "signal: killed") {
		t.Fatalf("process ended with %v, want SIGKILL", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out.String()))
	if err != nil || n == 0 {
		t.Fatalf("printed %q, want the uncommitted frame count", out.String())
	}
}

func TestKillInflight_NeverKillsEarly(t *testing.T) {
	for _, c := range []struct {
		name, want string
		hold       bool
		acks       int
	}{
		{"acknowledged but only commits in the WAL", "no transaction reached the WAL", false, 2},
		{"open transaction but too few acknowledgements", "1 of 2 acknowledgements", true, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, wal := openWAL(t)
			if c.hold {
				release, done := holdSpilled(t, d)
				defer finish(t, release, done)
			}
			cmd := sleeper(t)
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			var out strings.Builder
			err := killInflight(ctx, &out, ackFile(t, c.acks), 2, wal, cmd.Process.Pid)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("killInflight error = %v, want it to mention %q", err, c.want)
			}
			if err := cmd.Process.Signal(sigZero); err != nil {
				t.Fatalf("process was killed: %v", err)
			}
			state, err := osexec.CommandContext(context.Background(), "ps", "-o", "stat=", "-p", strconv.Itoa(cmd.Process.Pid)).Output()
			if err != nil || strings.HasPrefix(string(state), "T") {
				t.Fatalf("process left stopped: state %q, %v", state, err)
			}
		})
	}
}

// sigZero checks a process exists without touching it.
const sigZero = syscall.Signal(0)
