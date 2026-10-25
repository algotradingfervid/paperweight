package auth

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// ReferenceChecker reports whether a row is referenced by any record, and by
// which (doc.Engine satisfies it).
type ReferenceChecker interface {
	IsReferenced(ctx context.Context, tx *db.Tx, table string, id int64) (string, bool, error)
}

// VerifyFunc checks password against an encoded hash; rehash is true when the
// hash should be re-encoded with the current parameters.
type VerifyFunc func(encoded, password string) (ok, rehash bool, err error)

// Deps are the collaborators Auth needs.
type Deps struct {
	DB        *db.DB
	Clock     clock.Clock
	Directory Directory    // CD-13
	Audit     audit.Writer // CD-03
	Refs      ReferenceChecker
	Info      doctype.InfoSource // CD-05
	Verify    VerifyFunc         // nil = VerifyPassword
}

// Auth implements Service plus the user, matrix and sign-in operations.
type Auth struct {
	db     *db.DB
	clock  clock.Clock
	dir    Directory
	audit  audit.Writer
	refs   ReferenceChecker
	info   doctype.InfoSource
	verify VerifyFunc
}

// New builds an Auth from d.
func New(d Deps) *Auth {
	return &Auth{
		db:     d.DB,
		clock:  d.Clock,
		dir:    d.Directory,
		audit:  d.Audit,
		refs:   d.Refs,
		info:   d.Info,
		verify: d.Verify,
	}
}
