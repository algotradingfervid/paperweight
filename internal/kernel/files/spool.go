package files

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
)

// headWriter keeps the first SniffBytes bytes written to it.
type headWriter struct{ b []byte }

func (w *headWriter) Write(p []byte) (int, error) {
	if n := SniffBytes - len(w.b); n > 0 {
		w.b = append(w.b, p[:min(n, len(p))]...)
	}
	return len(p), nil
}

// Spool copies r to a temp file under files/tmp, hashing it on the way. It
// runs outside any transaction (CD-29); Attach only consumes the result. More
// than limit bytes is KindTooLarge and leaves no temp file.
func (s *Store) Spool(r io.Reader, limit int64) (*Spooled, error) {
	f, err := os.CreateTemp(s.tmp, "up-*")
	if err != nil {
		return nil, s.storageErr("spool", s.tmp, err)
	}
	sp, err := s.spoolTo(f, f.Name(), r, limit)
	if err != nil {
		_ = os.Remove(f.Name())
		return nil, err
	}
	return sp, nil
}

// fileWriter remembers the first error writing the temp file, so it can be
// told apart from an error reading the client's body.
type fileWriter struct {
	w   io.Writer
	err error
}

func (fw *fileWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if err != nil && fw.err == nil {
		fw.err = err
	}
	return n, err
}

// spoolTo copies r into f (the temp file at path) and closes f. A failed write
// to f is KindStorage; a failed read of r is returned wrapped, as is.
func (s *Store) spoolTo(f io.WriteCloser, path string, r io.Reader, limit int64) (*Spooled, error) {
	h := sha256.New()
	head := &headWriter{}
	fw := &fileWriter{w: f}
	n, err := io.Copy(io.MultiWriter(fw, h, head), io.LimitReader(r, limit+1))
	switch {
	case fw.err != nil:
		err = s.storageErr("spool", path, fw.err)
	case err != nil:
		err = fmt.Errorf("files: spool upload: %w", err)
	}
	if cerr := f.Close(); err == nil && cerr != nil {
		err = s.storageErr("spool", path, cerr)
	}
	if err == nil && n > limit {
		err = &Error{Kind: KindTooLarge, Code: http.StatusRequestEntityTooLarge, Message: msgTooLarge}
	}
	if err != nil {
		return nil, err
	}
	return &Spooled{path: path, size: n, sha256: hex.EncodeToString(h.Sum(nil)), head: head.b}, nil
}

// SHA256 is the lower-case hex SHA-256 of the spooled bytes.
func (f *Spooled) SHA256() string { return f.sha256 }

// Size is the number of spooled bytes.
func (f *Spooled) Size() int64 { return f.size }

// Head is the first min(Size, SniffBytes) bytes.
func (f *Spooled) Head() []byte { return f.head }

// Cleanup removes the temp file if it is still there; calling it again is harmless.
func (f *Spooled) Cleanup() error {
	if err := os.Remove(f.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
