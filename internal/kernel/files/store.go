package files

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
)

// Config is what a Store needs. Audit nil means audit.Default; a DocType
// missing from Policies gets DefaultPolicy.
type Config struct {
	DataDir  string
	DB       *db.DB
	Auth     auth.Service
	Info     doctype.InfoSource
	Clock    clock.Clock
	Audit    audit.Writer
	Policies map[string]Policy
	Logger   *slog.Logger
}

// Store keeps attachment rows in the database and their bytes under
// <DataDir>/files, one blob per SHA-256 (CD-29).
type Store struct {
	cfg Config
	tmp string // <DataDir>/files/tmp
}

var _ Service = (*Store)(nil)

// New creates <DataDir>/files and <DataDir>/files/tmp (mode 0700) and returns the Store.
func New(cfg Config) (*Store, error) {
	if cfg.Audit == nil {
		cfg.Audit = audit.Default
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	tmp := filepath.Join(cfg.DataDir, "files", "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil, fmt.Errorf("files: create %s: check the data directory is writable: %w", tmp, err)
	}
	return &Store{cfg: cfg, tmp: tmp}, nil
}

func (s *Store) policy(doctype string) Policy {
	if p, ok := s.cfg.Policies[doctype]; ok {
		return p
	}
	return DefaultPolicy
}

// refFrom is the RecordRef permission checks use: always built from Info, never
// from the caller's RecordRef beyond DocType and ID.
func refFrom(info doctype.Info) auth.RecordRef { //nolint:unused // used by Task 4's rule checks
	return auth.RecordRef{
		DocType:    info.DocType,
		ID:         info.ID,
		CreatedBy:  info.CreatedBy,
		EmployeeID: info.EmployeeID,
		ProjectIDs: info.ProjectIDs,
		SiteIDs:    info.SiteIDs,
	}
}

// Remove is not built yet (WP1-FILE Task 5).
func (s *Store) Remove(ctx context.Context, tx *db.Tx, p auth.Principal, id int64) error {
	return errs.ErrNotImplemented
}

// Open is not built yet (WP1-FILE Task 5).
func (s *Store) Open(ctx context.Context, p auth.Principal, id int64) (io.ReadCloser, Meta, error) {
	return nil, Meta{}, errs.ErrNotImplemented
}

// List is not built yet (WP1-FILE Task 5).
func (s *Store) List(ctx context.Context, p auth.Principal, parent auth.RecordRef) ([]Meta, error) {
	return nil, errs.ErrNotImplemented
}

// CopyAll is not built yet (WP1-FILE Task 5).
func (s *Store) CopyAll(ctx context.Context, tx *db.Tx, from, to auth.RecordRef) error {
	return errs.ErrNotImplemented
}

// DeleteAll is not built yet (WP1-FILE Task 5).
func (s *Store) DeleteAll(ctx context.Context, tx *db.Tx, parent auth.RecordRef) error {
	return errs.ErrNotImplemented
}
