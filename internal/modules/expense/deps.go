package expense

import (
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Deps are what the hooks need beyond their transaction. Hooks get only
// (ctx, tx, id), so they reach Deps through wiring, per database (CD-284).
type Deps struct {
	Clock        clock.Clock
	Auth         auth.Service
	Directory    auth.Directory
	Files        *files.Store
	ExpenseTypes *expensetype.Service
}

// wiring holds each database's Deps. Every hook reads it with Get(tx) first
// and returns its error, so parallel worlds in one binary each get their own.
var wiring modkit.Wiring[Deps]

// Wire sets the Deps the hooks use on d, replacing any earlier ones.
// internal/app wires its database in app.New; expensetest wires each test
// database.
func Wire(d *db.DB, v Deps) { wiring.Wire(d, v) }

// Service is what acts for a principal outside the document engine (Mark as
// paid, reports, warnings). Each task adds its methods in its own file.
type Service struct {
	db   *db.DB
	deps Deps
}

// New returns the Service over d with v.
func New(d *db.DB, v Deps) *Service { return &Service{db: d, deps: v} }
