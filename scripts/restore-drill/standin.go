package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// Temporary. This stand-in does what the e2e-tag app will do in the drill:
// real committed WAL writes through db.Write, plus one file for each. Delete
// this file and the standin-writer command when run_writer in
// scripts/restore-drill.sh drives the e2e-tag app.

// spillRows of spillRowSize bytes is 8 MB, past SQLite's default 2 MB page
// cache, so the open transaction's pages reach the WAL with no commit frame.
const (
	spillRows    = 2000
	spillRowSize = 4096
)

func initDB(data string) error {
	if err := os.MkdirAll(data, 0o700); err != nil { //nolint:gosec // the drill's own temp dir
		return err
	}
	d, err := db.Open(filepath.Join(data, "app.db"))
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	_, err = d.SQL().Exec(`CREATE TABLE IF NOT EXISTS drill_approvals (
		id INTEGER PRIMARY KEY,
		state TEXT NOT NULL,
		blob_hash TEXT NOT NULL,
		pad BLOB
	)`)
	return err
}

// standInWriter commits n rows, acknowledging each on ack only after its commit,
// with one file each and one half-written spool file. Then it opens a
// transaction it never commits and keeps writing to it until ctx ends or the
// process is killed; "inflight" on ack means its pages have reached the WAL.
func standInWriter(ctx context.Context, ack io.Writer, data string, n int, pause time.Duration) error {
	d, err := db.Open(filepath.Join(data, "app.db"))
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()

	if err := writeBlob(filepath.Join(data, "files", "tmp", "up-drill"), []byte("half an upload")); err != nil {
		return err
	}
	for i := 1; i <= n; i++ {
		body := fmt.Appendf(nil, "approval %d\n", i)
		sum := sha256.Sum256(body)
		hash := hex.EncodeToString(sum[:])
		if err := writeBlob(filepath.Join(data, "files", hash[:2], hash), body); err != nil {
			return err
		}
		err := d.Write(ctx, func(tx *db.Tx) error {
			_, err := tx.SQL().ExecContext(ctx,
				"INSERT INTO drill_approvals (id, state, blob_hash) VALUES (?, 'approved', ?)", i, hash)
			return err
		})
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(ack, "committed %d\n", i); err != nil {
			return err
		}
		time.Sleep(pause)
	}

	pad := make([]byte, spillRowSize)
	return d.Write(ctx, func(tx *db.Tx) error {
		for i := n + 1; ; i++ {
			if _, err := tx.SQL().ExecContext(ctx,
				"INSERT INTO drill_approvals (id, state, blob_hash, pad) VALUES (?, 'approved', 'inflight', ?)", i, pad); err != nil {
				return err
			}
			if i == n+spillRows {
				if _, err := fmt.Fprintln(ack, "inflight"); err != nil {
					return err
				}
			}
		}
	})
}

func writeBlob(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { //nolint:gosec // the drill's own temp dir
		return err
	}
	return os.WriteFile(path, body, 0o600) //nolint:gosec // the drill's own temp dir
}
