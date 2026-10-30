package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/db"
)

const testApprovedSQL = "SELECT id FROM approvals"

// seed builds a data dir with app.db (three rows) and two files.
func seed(t *testing.T, dir string) {
	t.Helper()
	d, err := db.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	for _, q := range []string{
		"CREATE TABLE approvals (id INTEGER PRIMARY KEY, note TEXT NOT NULL)",
		"CREATE INDEX approvals_note ON approvals (note)",
		"INSERT INTO approvals VALUES (1, 'a'), (2, 'b'), (3, 'c')",
	} {
		if _, err := d.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(dir, "files", "ab", "one"), "one")
	writeFile(t, filepath.Join(dir, "files", "cd", "two"), "two")
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exec(t *testing.T, dir, q string) {
	t.Helper()
	d, err := db.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.SQL().Exec(q); err != nil {
		t.Fatal(err)
	}
}

func pair(t *testing.T) (source, restored string) {
	t.Helper()
	source, restored = t.TempDir(), t.TempDir()
	seed(t, source)
	seed(t, restored)
	return source, restored
}

// run verifies with acknowledgements for ids 1 to committed.
func run(source, restored string, committed int) (string, error) {
	var acked []int64
	for id := 1; id <= committed; id++ {
		acked = append(acked, int64(id))
	}
	var out bytes.Buffer
	err := verify(context.Background(), &out, source, restored, acked, testApprovedSQL)
	return out.String(), err
}

func TestVerify_AllMatch(t *testing.T) {
	source, restored := pair(t)
	out, err := run(source, restored, 3)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	for _, want := range []string{"integrity ok\n", "database sha256 ok\n", "files 2/2 sha256 ok\n", "approvals 3/3\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q misses %q", out, want)
		}
	}
}

func TestVerify_IgnoresSpoolDir(t *testing.T) {
	source, restored := pair(t)
	writeFile(t, filepath.Join(source, "files", "tmp", "up-1"), "half an upload")
	if _, err := run(source, restored, 3); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerify_RestoredSpoolFileRefused(t *testing.T) {
	source, restored := pair(t)
	writeFile(t, filepath.Join(restored, "files", "tmp", "up-1"), "half an upload")
	if _, err := run(source, restored, 3); err == nil || !strings.Contains(err.Error(), "tmp/up-1 extra") {
		t.Fatalf("verify error = %v, want tmp/up-1 extra", err)
	}
}

func TestVerify_Refuses(t *testing.T) {
	cases := []struct {
		name      string
		committed int
		breakIt   func(t *testing.T, source, restored string)
		want      string
	}{
		{"count short of committed", 4, nil, "approvals 3/4"},
		{"two more than acknowledged", 1, nil, "approvals 1/1: 2 more"},
		{"row lost", 3, func(t *testing.T, _, restored string) {
			exec(t, restored, "DELETE FROM approvals WHERE id = 3")
		}, "database checksum differs"},
		{"row changed, count same", 3, func(t *testing.T, _, restored string) {
			exec(t, restored, "UPDATE approvals SET note = 'x' WHERE id = 2")
		}, "database checksum differs"},
		{"schema differs", 3, func(t *testing.T, _, restored string) {
			exec(t, restored, "DROP INDEX approvals_note")
		}, "database checksum differs"},
		{"file changed", 3, func(t *testing.T, _, restored string) {
			writeFile(t, filepath.Join(restored, "files", "ab", "one"), "ONE")
		}, "ab/one"},
		{"file missing", 3, func(t *testing.T, _, restored string) {
			if err := os.Remove(filepath.Join(restored, "files", "cd", "two")); err != nil {
				t.Fatal(err)
			}
		}, "cd/two"},
		{"file extra", 3, func(t *testing.T, _, restored string) {
			writeFile(t, filepath.Join(restored, "files", "ef", "three"), "three")
		}, "ef/three"},
		{"no files at all", 3, func(t *testing.T, source, restored string) {
			for _, dir := range []string{source, restored} {
				if err := os.RemoveAll(filepath.Join(dir, "files")); err != nil {
					t.Fatal(err)
				}
			}
		}, "no files"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			source, restored := pair(t)
			if c.breakIt != nil {
				c.breakIt(t, source, restored)
			}
			if _, err := run(source, restored, c.committed); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("verify error = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

// A kill between a commit and its print leaves one approval nobody heard of.
func TestVerify_OneUnacknowledgedApprovalInBothDatabases(t *testing.T) {
	source, restored := pair(t)
	out, err := run(source, restored, 2)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !strings.HasSuffix(out, "1 more approval committed but not printed, also in the source\napprovals 2/2\n") {
		t.Fatalf("output = %q", out)
	}
}

func TestCheckApprovals(t *testing.T) {
	cases := []struct {
		name                    string
		acked, source, restored []int64
		wantExtra               int
		wantErr                 string
	}{
		{name: "exact", acked: []int64{1, 2}, source: []int64{1, 2}, restored: []int64{1, 2}},
		{name: "one committed past the last print", acked: []int64{1, 2}, source: []int64{1, 2, 3}, restored: []int64{1, 2, 3}, wantExtra: 1},
		{name: "acknowledged row lost", acked: []int64{1, 2}, source: []int64{1, 2}, restored: []int64{1}, wantErr: "approvals 1/2"},
		{name: "same count, wrong rows", acked: []int64{1, 2}, source: []int64{1, 3}, restored: []int64{1, 3}, wantErr: "approvals 1/2"},
		{name: "two past the last print", acked: []int64{1}, source: []int64{1, 2, 3}, restored: []int64{1, 2, 3}, wantErr: "approvals 1/1: 2 more"},
		{name: "extra row not in the source", acked: []int64{1}, source: []int64{1}, restored: []int64{1, 9}, wantErr: "approved ids differ"},
		{name: "unprinted commit missing from restored", acked: []int64{1}, source: []int64{1, 2}, restored: []int64{1}, wantErr: "approved ids differ"},
		{name: "same ids in another order", acked: []int64{1, 2}, source: []int64{2, 1}, restored: []int64{1, 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			extra, err := checkApprovals(c.acked, c.source, c.restored)
			if c.wantErr == "" {
				if err != nil || extra != c.wantExtra {
					t.Fatalf("checkApprovals = %d, %v; want %d, nil", extra, err, c.wantExtra)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("checkApprovals error = %v, want it to mention %q", err, c.wantErr)
			}
		})
	}
}

func TestReadAcks(t *testing.T) {
	cases := []struct {
		name, body string
		want       []int64
		wantErr    string
	}{
		{name: "in order", body: "committed 1 7\ncommitted 2 9\n", want: []int64{7, 9}},
		{name: "none", body: ""},
		{name: "other text", body: "committed 1 7\ninflight\n", wantErr: "inflight"},
		{name: "gap in the sequence", body: "committed 1 7\ncommitted 3 9\n", wantErr: "committed 3 9"},
		{name: "same document twice", body: "committed 1 7\ncommitted 2 7\n", wantErr: "committed 2 7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "acks")
			writeFile(t, path, c.body)
			got, err := readAcks(path)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("readAcks error = %v, want it to mention %q", err, c.wantErr)
				}
				return
			}
			if err != nil || !slices.Equal(got, c.want) {
				t.Fatalf("readAcks = %v, %v; want %v", got, err, c.want)
			}
		})
	}
}

func TestVerify_ZeroCommittedRefused(t *testing.T) {
	source, restored := pair(t)
	exec(t, source, "DELETE FROM approvals")
	exec(t, restored, "DELETE FROM approvals")
	if _, err := run(source, restored, 0); err == nil || !strings.Contains(err.Error(), "nothing committed") {
		t.Fatalf("verify error = %v, want nothing committed", err)
	}
}

func TestVerify_CorruptDatabaseRefused(t *testing.T) {
	source, restored := pair(t)
	path := filepath.Join(restored, "app.db")
	// Checkpoint so the rows are in the main file, then overwrite the pages
	// after the schema page.
	exec(t, restored, "PRAGMA wal_checkpoint(TRUNCATE)")
	f, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(bytes.Repeat([]byte{0xff}, 4096), 4096); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := run(source, restored, 3)
	if err == nil {
		t.Fatalf("verify passed a corrupt database: %q", out)
	}
	if strings.Contains(out, "integrity ok") {
		t.Fatalf("printed integrity ok for a corrupt database: %q", out)
	}
}
