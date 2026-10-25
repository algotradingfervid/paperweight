package files

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/files/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// GCGrace is how old an unreferenced blob or temp file must be before GC deletes it.
const GCGrace = 30 * time.Minute

// GCReport counts what one GC run did: blobs deleted, blobs kept, temp files deleted.
type GCReport struct{ Deleted, Kept, TempDeleted int }

// GC deletes blobs no attachment row uses and temp files, once their mtime is
// at least grace before the Store clock's now. Each blob is re-checked inside
// its own db.Write, so an Attach of the same bytes (which places its blob
// inside its write transaction) can never lose the blob its row points to.
// Temp-file ages compare the file's mtime with the Store's Clock, so they
// assume Clock follows the wall clock.
// A failed stat or remove on one file is logged and that file is skipped; the
// run continues and returns the first such error with the report.
func (s *Store) GC(ctx context.Context, grace time.Duration) (GCReport, error) {
	var rep GCReport
	var firstErr error
	skip := func(err error) {
		s.cfg.Logger.Error("files: gc: skipped file", "err", err)
		if firstErr == nil {
			firstErr = err
		}
	}
	cutoff := s.cfg.Clock.Now().Add(-grace)
	root := filepath.Join(s.cfg.DataDir, "files")
	dirs, err := os.ReadDir(root)
	if err != nil {
		return rep, fmt.Errorf("files: gc: read %s: %w", root, err)
	}
	for _, d := range dirs {
		if !d.IsDir() || d.Name() == "tmp" {
			continue
		}
		dir := filepath.Join(root, d.Name())
		blobs, err := os.ReadDir(dir)
		if err != nil {
			return rep, fmt.Errorf("files: gc: read %s: %w", dir, err)
		}
		for _, b := range blobs {
			path, err := BlobPath(s.cfg.DataDir, b.Name())
			if err != nil || path != filepath.Join(dir, b.Name()) {
				continue // not a blob this package wrote
			}
			if info, err := b.Info(); err == nil && info.ModTime().After(cutoff) {
				rep.Kept++ // inside the grace period: no transaction needed
				continue
			}
			fileErr, err := s.gcBlob(ctx, path, b.Name(), cutoff, &rep)
			if err != nil {
				return rep, err
			}
			if fileErr != nil {
				skip(fileErr)
			}
		}
	}
	tmps, err := os.ReadDir(s.tmp)
	if err != nil {
		return rep, fmt.Errorf("files: gc: read %s: %w", s.tmp, err)
	}
	for _, t := range tmps {
		path := filepath.Join(s.tmp, t.Name())
		st, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			skip(fmt.Errorf("files: gc: stat %s: %w", path, err))
			continue
		}
		if !st.Mode().IsRegular() || st.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			skip(fmt.Errorf("files: gc: remove %s: %w", path, err))
			continue
		}
		rep.TempDeleted++
	}
	return rep, firstErr
}

// gcBlob stats the blob, counts its rows and removes it when none use it and
// it is older than cutoff, all inside one write transaction. A failed stat or
// remove of the blob comes back as fileErr, with the transaction still
// committed; err is only set for database failures.
func (s *Store) gcBlob(ctx context.Context, path, sha string, cutoff time.Time, rep *GCReport) (fileErr, err error) {
	err = s.cfg.DB.Write(ctx, func(tx *db.Tx) error {
		fileErr = nil
		st, serr := os.Stat(path)
		if errors.Is(serr, fs.ErrNotExist) {
			return nil
		}
		if serr != nil {
			fileErr = fmt.Errorf("files: gc: stat %s: %w", path, serr)
			return nil
		}
		if st.ModTime().After(cutoff) {
			rep.Kept++
			return nil
		}
		n, err := dbq.New(tx.SQL()).CountForSHA(ctx, sha)
		if err != nil {
			return err
		}
		if n > 0 {
			rep.Kept++
			return nil
		}
		if err := os.Remove(path); err != nil {
			fileErr = fmt.Errorf("files: gc: remove %s: %w", path, err)
			return nil
		}
		rep.Deleted++
		return nil
	})
	return fileErr, err
}
