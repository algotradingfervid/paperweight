package files

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	errDiskFull   = errors.New("no space left on device")
	errClientGone = errors.New("client went away")
)

// failingFile fails every write, like a temp file on a full disk.
type failingFile struct{ closed bool }

func (f *failingFile) Write(p []byte) (int, error) { return 0, errDiskFull }
func (f *failingFile) Close() error                { f.closed = true; return nil }

// failingReader fails like a client body that breaks off.
type failingReader struct{}

func (failingReader) Read(p []byte) (int, error) { return 0, errClientGone }

func newSpoolStore(t *testing.T, log *bytes.Buffer) *Store {
	t.Helper()
	s, err := New(Config{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(log, nil))})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestSpool_TempWriteFailureIsStorage(t *testing.T) {
	var log bytes.Buffer
	s := newSpoolStore(t, &log)
	f := &failingFile{}
	path := filepath.Join(s.tmp, "up-wp1-file-3")

	_, err := s.spoolTo(f, path, bytes.NewReader([]byte("%PDF-1.7\n")), MaxFileBytes)
	if k := KindOf(err); k != KindStorage {
		t.Fatalf("KindOf(%v) = %v, want KindStorage", err, k)
	}
	if err.Error() != msgStorage {
		t.Errorf("message = %q, want %q", err.Error(), msgStorage)
	}
	if !errors.Is(err, errDiskFull) {
		t.Errorf("error %v does not wrap the write error", err)
	}
	if !f.closed {
		t.Error("temp file not closed after the failed write")
	}
	for _, want := range []string{"op=spool", "path=" + path, "err=" + `"no space left on device"`} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log %q lacks %q", log.String(), want)
		}
	}
}

func TestSpool_ClientReadFailureIsNotStorage(t *testing.T) {
	var log bytes.Buffer
	s := newSpoolStore(t, &log)

	_, err := s.Spool(failingReader{}, MaxFileBytes)
	if !errors.Is(err, errClientGone) {
		t.Fatalf("Spool error = %v, want it to wrap the read error", err)
	}
	if k := KindOf(err); k != 0 {
		t.Errorf("KindOf = %v, want 0 (a client read error is not a files error)", k)
	}
	if log.Len() != 0 {
		t.Errorf("read error logged as a storage failure: %q", log.String())
	}
	ents, err := os.ReadDir(s.tmp)
	if err != nil {
		t.Fatalf("read files/tmp: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("files/tmp has %d entries, want empty", len(ents))
	}
}
