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
//	Test kit                     testkit.NewDB, Clock, Principal, Seed, LinkEmployees, Directory
package mstkit

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
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
// master's UpdateTx, SetDisabledTx and DeleteTx must call CanRowTx themselves,
// because the Tx twins called by importers and other masters never pass
// through Write (AUTH-102 restrictions on Project/Site).
func (e Env) Write(ctx context.Context, p auth.Principal, k Kind, r auth.Right, fn func(tx *db.Tx) error) error {
	if err := e.Auth.Can(ctx, p, k.DocType, r, nil); err != nil {
		return err
	}
	return e.DB.Write(ctx, fn)
}

// CreatedBy reads who created row id of k, inside tx. It is 0, which is nobody's
// user, for a row seeded by a migration (created_by NULL), so such a row is
// outside every own scope. A missing row is 0 too: AUTH then refuses as it
// does for any row it can't place, and doesn't tell a user with no right
// whether the id exists.
func CreatedBy(ctx context.Context, tx *db.Tx, k Kind, id int64) (auth.UserID, error) {
	var by sql.NullInt64
	err := tx.SQL().QueryRowContext(ctx, "SELECT created_by FROM "+k.Table+" WHERE id = ?", id).Scan(&by) //nolint:gosec // G202: Table comes from each master's package-level Kind, never from a request
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	return auth.UserID(by.Int64), nil
}

// CanRowTx is Auth.CanTx on rec, a stored row of k, with rec.CreatedBy read
// inside tx, so own scope decides as the list does (CD-223, CD-213). A create
// has no row: it passes a nil RecordRef to Auth.CanTx, where the right alone
// decides.
func (e Env) CanRowTx(ctx context.Context, tx *db.Tx, p auth.Principal, k Kind, r auth.Right, rec *auth.RecordRef) error {
	by, err := CreatedBy(ctx, tx, k, rec.ID)
	if err != nil {
		return err
	}
	rec.CreatedBy = by
	return e.Auth.CanTx(ctx, tx, p, k.DocType, r, rec)
}

// CanRow is CanRowTx in one read, for a screen that holds no transaction.
func (e Env) CanRow(ctx context.Context, p auth.Principal, k Kind, r auth.Right, rec *auth.RecordRef) error {
	return e.DB.Read(ctx, func(tx *db.Tx) error { return e.CanRowTx(ctx, tx, p, k, r, rec) })
}

// CanLinkTx checks that p may read row id of k, the target of a link p posted
// to another master, inside tx (CD-227). A row p can't read, and any row when p
// has no read right on k, is doctype.ErrNotFound, which a master answers with
// its not-found text, so a hidden target looks like an unknown id. It runs ahead
// of every message that describes the target. A master whose ProjectFields or
// SiteFields is {"id"} is its own dimension value (CD-64).
func (e Env) CanLinkTx(ctx context.Context, tx *db.Tx, p auth.Principal, k Kind, id int64) error {
	rec := &auth.RecordRef{DocType: k.DocType, ID: id}
	if dt, ok := doctype.Lookup(k.DocType); ok {
		if slices.Equal(dt.ProjectFields, []string{"id"}) {
			rec.ProjectIDs = []int64{id}
		}
		if slices.Equal(dt.SiteFields, []string{"id"}) {
			rec.SiteIDs = []int64{id}
		}
	}
	err := e.CanRowTx(ctx, tx, p, k, auth.RightRead, rec)
	if errors.Is(err, auth.ErrForbidden) {
		return doctype.ErrNotFound
	}
	return err
}

// CanLinkTargetTx is CanLinkTx for a registered DocType: CSV link cells and
// posted form values name their target that way (CD-232, CD-233).
func (e Env) CanLinkTargetTx(ctx context.Context, tx *db.Tx, p auth.Principal, target *doctype.DocType, id int64) error {
	return e.CanLinkTx(ctx, tx, p, Kind{DocType: target.Name, Label: target.Label, Table: target.Table}, id)
}

// LinkedTx reads the target of a link p posted with get, after CanLinkTx.
func LinkedTx[T any](ctx context.Context, tx *db.Tx, e Env, p auth.Principal, k Kind, id int64,
	get func(context.Context, *db.Tx, int64) (T, error)) (T, error) {
	if err := e.CanLinkTx(ctx, tx, p, k, id); err != nil {
		var zero T
		return zero, err
	}
	return get(ctx, tx, id)
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
