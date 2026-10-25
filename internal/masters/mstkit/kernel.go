// Package mstkit holds the rules every master shares (code, name, audit,
// disable, delete-if-unreferenced) and the registry wiring. This file is the
// only one in WP2 that names kernel write APIs (db.Write, audit append,
// Engine.References), so a kernel name change is fixed here.
//
// Kernel reconciliation (MST Task 1 Step 1 against main d17fb3a; Task 1b
// against main e2dd7d5, AUTH, DOC, NUM and UI merged). Every M2 contract
// matches spec §5. Non-contract names:
//
//	What                         Name on main
//	DOC engine constructor       doc.NewEngine(doc.Deps{DB, Clock, Auth, Naming, Audit, ...})
//	Naming service               naming.New(db, clock, auth roles, audit.Writer)
//	Stale-version error          &doc.Error{Kind: doc.KindStale} with DOC's msgStale
//	                             (unexported; mstkit.CheckVersion copies the wording)
//	Audit writer                 audit.Default (audit.Store), audit.Append; auditfake.New
//	AUTH service constructor     auth.New(auth.Deps{DB, Clock, Directory, Audit, Refs, Info})
//	                             (Refs and Info are the doc engine)
//	AUTH matrix grant API        (*auth.Auth).SeedDefaults, SetRights(actor, role, doctype,
//	                             map[auth.Right]auth.Scope); auth.Applies, auth.AllRights
//	CSV import flag              csvio.IsImport(ctx)
//	CSV importer                 csvio.RegisterImporter(doctype, csvio.RowFunc),
//	                             csvio.NewImporter(db, auth, clock, audit.Writer)
//	List query                   list.Build(dt, list.Query, auth.Predicate), BuildCount
//	Letterhead                   print.Letterhead.ForBranch, print.LetterheadInfo
//	Rounding helpers             types.DivRound, MulDivRound, RoundTo, Rescale,
//	                             Money.RoundRupee, Qty.Times, Rate.ToMoney,
//	                             types.StockQty
//	Formatters                   types.FormatQty, FormatQtyUoM, FormatPercent,
//	                             ParsePercent, FormatIST
//	Dates and times              types.Today(clock), types.TimestampOf(t).String()
//	UI list/form components      (for Task 22's web/ subpackages only; never imported here, CD-89)
//	                             layout.Page, layout.Main; components.Form, FilterBar,
//	                             Pagination, EmptyState, ActionBar, MoreActions, ErrorSummary,
//	                             TextInput, CodeInput, Select, Checkboxes, Textarea,
//	                             NumberInput, QtyInput, PercentInput, Card, SummaryList,
//	                             Banner, Toast, Dialog; screens.Register
//	Test kit                     testkit.NewDB, Clock, Principal, Seed, Directory
//	testkit_* users (Task 23)    internal/testkit/migrations/09001_testkit_minimal_masters.sql,
//	                             internal/testkit/seed.go, internal/testkit/testkit_test.go,
//	                             internal/fixture/claim.go, internal/kernel/doc/load_test.go
package mstkit

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Env is what every master Service needs from the kernel.
type Env struct {
	DB      *db.DB
	Auth    auth.Service
	Engine  doc.Engine
	Auditor audit.Writer
	Clock   clock.Clock
}

// Write checks that p has right r on k, then runs fn inside db.Write
// (BEGIN IMMEDIATE). It checks the kind-level right only (RecordRef nil): a
// master's UpdateTx, SetDisabledTx and DeleteTx must call Auth.Can with a
// RecordRef themselves, because the Tx twins called by importers and other
// masters never pass through Write (AUTH-102 restrictions on Project/Site).
func (e Env) Write(ctx context.Context, p auth.Principal, k Kind, r auth.Right, fn func(tx *db.Tx) error) error {
	if err := e.Auth.Can(ctx, p, k.DocType, r, nil); err != nil {
		return err
	}
	return e.DB.Write(ctx, fn)
}

// Today is the business date in Asia/Kolkata.
func (e Env) Today() types.Date { return types.Today(e.Clock) }

// References is Engine.References.
func (e Env) References(ctx context.Context, tx *db.Tx, table string, id int64, limit int) ([]doc.Reference, error) {
	return e.Engine.References(ctx, tx, table, id, limit)
}

// appendAudit writes one entry through the audit writer.
func (e Env) appendAudit(ctx context.Context, tx *db.Tx, entry audit.Entry) error {
	return e.Auditor.Append(ctx, tx, entry)
}
