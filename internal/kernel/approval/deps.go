package approval

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
	"github.com/vardhan/internaltools/internal/types"
)

// RoleDirectory tells whether a role exists and is active. Optional: AUTH
// provides it in Wave 3; without it role existence is not checked.
type RoleDirectory interface {
	RoleStatus(ctx context.Context, name string) (exists, active bool, err error)
}

// Deps are the §5 contracts Approvals reads through.
type Deps struct {
	DB        *db.DB
	Clock     clock.Clock
	Auth      auth.Service
	Info      doctype.InfoSource                         // CD-05; DOC's Engine in production
	Directory auth.Directory                             // CD-13; MST in production
	Audit     audit.Writer                               // CD-03; nil → audit.Default
	Lookup    func(name string) (*doctype.DocType, bool) // nil → doctype.Lookup
	All       func() []*doctype.DocType                  // nil → doctype.All
	Roles     RoleDirectory                              // nil → role existence not checked
}

// Approvals implements Service plus the rule-admin and read-model methods.
type Approvals struct {
	db     *db.DB
	clock  clock.Clock
	auth   auth.Service
	info   doctype.InfoSource
	dir    auth.Directory
	audit  audit.Writer
	lookup func(name string) (*doctype.DocType, bool)
	all    func() []*doctype.DocType
	roles  RoleDirectory
}

// New stores d, filling the defaults for Audit, Lookup and All.
func New(d Deps) *Approvals {
	a := &Approvals{
		db: d.DB, clock: d.Clock, auth: d.Auth, info: d.Info, dir: d.Directory,
		audit: d.Audit, lookup: d.Lookup, all: d.All, roles: d.Roles,
	}
	if a.audit == nil {
		a.audit = audit.Default
	}
	if a.lookup == nil {
		a.lookup = doctype.Lookup
	}
	if a.all == nil {
		a.all = doctype.All
	}
	return a
}

// Stub until its task: Inbox (Task 8).

func (a *Approvals) Inbox(ctx context.Context, p auth.Principal, page types.Page) (types.Result[InboxItem], error) {
	return types.Result[InboxItem]{}, errs.ErrNotImplemented
}
