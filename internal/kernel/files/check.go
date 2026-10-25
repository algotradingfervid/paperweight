package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/vardhan/internaltools/internal/kernel/files/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// CheckReport is what Check found: rows whose blob is missing, blobs no row
// uses, and blobs whose bytes no longer hash to their name (SHA-256 hex).
type CheckReport struct {
	MissingBlobs []Meta
	Orphans      []string
	Corrupt      []string
}

// Check compares the attachment rows with the blobs on disk and changes
// nothing (FILE-049). A blob can be both an orphan and corrupt.
func (s *Store) Check(ctx context.Context) (CheckReport, error) {
	var rep CheckReport
	var rows []dbq.Attachment
	err := s.cfg.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		rows, err = dbq.New(tx.SQL()).ListAll(ctx)
		return err
	})
	if err != nil {
		return rep, err
	}
	used := map[string]bool{}
	for _, r := range rows {
		used[r.Sha256] = true
		path, err := BlobPath(s.cfg.DataDir, r.Sha256)
		if err != nil {
			return rep, fmt.Errorf("files: check: attachment %d: %w", r.ID, err)
		}
		_, err = os.Stat(path)
		if err == nil {
			continue
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return rep, fmt.Errorf("files: check: %w", err)
		}
		m, err := metaFrom(r)
		if err != nil {
			return rep, err
		}
		rep.MissingBlobs = append(rep.MissingBlobs, m)
	}
	root := filepath.Join(s.cfg.DataDir, "files")
	dirs, err := os.ReadDir(root)
	if err != nil {
		return rep, fmt.Errorf("files: check: read %s: %w", root, err)
	}
	for _, d := range dirs {
		if !d.IsDir() || d.Name() == "tmp" {
			continue
		}
		dir := filepath.Join(root, d.Name())
		blobs, err := os.ReadDir(dir)
		if err != nil {
			return rep, fmt.Errorf("files: check: read %s: %w", dir, err)
		}
		for _, b := range blobs {
			path, err := BlobPath(s.cfg.DataDir, b.Name())
			if err != nil || path != filepath.Join(dir, b.Name()) {
				continue // not a blob this package wrote
			}
			sum, err := hashFile(path)
			if err != nil {
				return rep, fmt.Errorf("files: check: %w", err)
			}
			if sum != b.Name() {
				rep.Corrupt = append(rep.Corrupt, b.Name())
			}
			if !used[b.Name()] {
				rep.Orphans = append(rep.Orphans, b.Name())
			}
		}
	}
	return rep, nil
}

// hashFile returns the SHA-256 hex of the file at path.
func hashFile(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path is a blob path built by BlobPath
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

// WriteCheckReport prints r, one finding per line, and returns the exit code
// for `app files check`: 1 when there is any finding, else 0.
func WriteCheckReport(w io.Writer, r CheckReport) (exitCode int) {
	for _, m := range r.MissingBlobs {
		_, _ = fmt.Fprintf(w, "missing blob: attachment %d %q %s\n", m.ID, m.Name, m.SHA256)
	}
	for _, sha := range r.Orphans {
		_, _ = fmt.Fprintf(w, "orphan blob: %s\n", sha)
	}
	for _, sha := range r.Corrupt {
		_, _ = fmt.Fprintf(w, "corrupt blob: %s\n", sha)
	}
	if len(r.MissingBlobs)+len(r.Orphans)+len(r.Corrupt) > 0 {
		return 1
	}
	_, _ = fmt.Fprintln(w, "files check: no problems found")
	return 0
}
