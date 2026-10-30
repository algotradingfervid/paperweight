package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// spoolPrefix is files/tmp, uploads in progress (FILE store): never backed up,
// so the source's copy is left out and a restored one is a sync bug.
const spoolPrefix = "tmp/"

// verify checks the restored data directory against the crashed source and
// the writer's committed count, printing one line per passed check. It stops
// at the first failure.
func verify(ctx context.Context, w io.Writer, source, restored string, committed int, countSQL string) error {
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format+"\n", a...) }
	if committed <= 0 {
		return errors.New("nothing committed: the writer acknowledged no approvals")
	}
	src, err := db.Open(filepath.Join(source, "app.db"))
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := db.Open(filepath.Join(restored, "app.db"))
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()

	if err := integrity(ctx, dst.SQL()); err != nil {
		return err
	}
	say("integrity ok")

	srcSum, err := dbSHA256(ctx, src)
	if err != nil {
		return fmt.Errorf("source: %w", err)
	}
	dstSum, err := dbSHA256(ctx, dst)
	if err != nil {
		return fmt.Errorf("restored: %w", err)
	}
	if srcSum != dstSum {
		return fmt.Errorf("database checksum differs: source %s, restored %s", srcSum, dstSum)
	}
	say("database sha256 ok")

	n, err := compareFiles(filepath.Join(source, "files"), filepath.Join(restored, "files"))
	if err != nil {
		return err
	}
	say("files %d/%d sha256 ok", n, n)

	var approved int
	// countSQL is the drill script's own constant.
	if err := dst.SQL().QueryRowContext(ctx, countSQL).Scan(&approved); err != nil { //nolint:gosec // see above
		return fmt.Errorf("count approvals: %w", err)
	}
	if approved != committed {
		return fmt.Errorf("approvals %d/%d: restored count differs from committed", approved, committed)
	}
	say("approvals %d/%d", approved, committed)
	return nil
}

func integrity(ctx context.Context, q *sql.DB) error {
	rows, err := q.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return fmt.Errorf("integrity check: %w", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	if len(lines) != 1 || lines[0] != "ok" {
		return fmt.Errorf("integrity check: %s", strings.Join(lines, "; "))
	}
	return nil
}

// dbSHA256 folds the WAL into the main file, then hashes the file. A restore
// rebuilds the same pages, so spec section 10 item 4 can be taken literally:
// identical bytes, Litestream's own tables included.
func dbSHA256(ctx context.Context, d *db.DB) (string, error) {
	var busy, logged, moved int
	row := d.SQL().QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	if err := row.Scan(&busy, &logged, &moved); err != nil {
		return "", fmt.Errorf("checkpoint: %w", err)
	}
	if busy != 0 {
		return "", errors.New("checkpoint: database busy")
	}
	return fileSHA256(d.Path())
}

// compareFiles returns how many files match, or an error naming each path that
// is changed, missing or extra in the restored tree.
func compareFiles(source, restored string) (int, error) {
	src, err := treeSums(source)
	if err != nil {
		return 0, err
	}
	dst, err := treeSums(restored)
	if err != nil {
		return 0, err
	}
	maps.DeleteFunc(src, func(path, _ string) bool { return strings.HasPrefix(path, spoolPrefix) })
	var bad []string
	for path, sum := range src {
		switch got, ok := dst[path]; {
		case !ok:
			bad = append(bad, path+" missing")
		case got != sum:
			bad = append(bad, path+" changed")
		}
	}
	for path := range dst {
		if _, ok := src[path]; !ok {
			bad = append(bad, path+" extra")
		}
	}
	if len(bad) > 0 {
		slices.Sort(bad)
		return 0, fmt.Errorf("files differ: %s", strings.Join(bad, ", "))
	}
	if len(src) == 0 {
		return 0, errors.New("no files to compare")
	}
	return len(src), nil
}

// treeSums maps each file under root (slash path relative to root) to its
// SHA-256. A root that doesn't exist is an empty tree.
func treeSums(root string) (map[string]string, error) {
	sums := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error { //nolint:gosec // the drill's own temp dir
		if errors.Is(err, fs.ErrNotExist) && path == root {
			return nil
		}
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum, err := fileSHA256(path)
		if err != nil {
			return err
		}
		sums[filepath.ToSlash(rel)] = sum
		return nil
	})
	return sums, err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // paths from the drill's own temp tree
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
