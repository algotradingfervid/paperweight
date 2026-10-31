package goodsreceipt

import (
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Deps are what the hooks and services need beyond their transaction. Hooks
// get only (ctx, tx, id), so they reach Deps through wiring, per database
// (CD-284).
type Deps struct {
	DB    *db.DB
	Clock clock.Clock
	Auth  auth.Service
	Audit audit.Writer
	Files *files.Store
}

// wiring holds each database's Deps. Every hook reads it with Get(tx) first
// and returns its error, so parallel worlds in one binary each get their own.
var wiring modkit.Wiring[Deps]

// Wire sets the Deps the hooks use on d, replacing any earlier ones.
// internal/app wires its database in app.New; grntest wires each test
// database.
func Wire(d *db.DB, v Deps) { wiring.Wire(d, v) }

// Service is what acts for a principal outside the document engine (the PO
// picker, bill details, reports). Each task adds its methods in its own file.
type Service struct {
	d Deps //nolint:unused // read by the methods G6, G7 and G9 build
}

// New returns the Service over v.
func New(v Deps) *Service { return &Service{d: v} }
