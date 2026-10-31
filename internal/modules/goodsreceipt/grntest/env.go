// Package grntest is goodsreceipt's test environment: purchasetest's world
// (a testkit database with the canonical seed, the real AUTH, DOC, APR and
// naming, comments and purchase.Service on one fixed clock) with no approval
// rule for purchase.Order or goodsreceipt.Receipt, so both approve on submit
// (00-fixtures.md, GRN test seed; CD-279 (8)), plus the real files.Store and
// goodsreceipt's Wiring for that database. POs come from
// purchasetest.NewOrder; the builders here go through Engine.Do, never
// insert document rows (research §2 pattern 2). Later lanes add their
// helpers in grntest/<task>.go.
package grntest

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// Env is one test's GRN world. Files is a real files.Store for the module's
// own code (Deps.Files), but the engine is purchasetest's, built with
// filesfake: DOC's DeleteAll (draft delete) and CopyAll (amend) run against
// the fake, so a test that attaches through Files and then deletes or amends
// must not rely on the real store seeing those calls.
type Env struct {
	*purchasetest.Env
	Files   *files.Store
	Deps    goodsreceipt.Deps
	Service *goodsreceipt.Service
}

// New builds the world on purchasetest.New(t, WithNoRules()) and wires
// goodsreceipt's hooks for its database only (CD-284).
func New(t testing.TB) *Env {
	t.Helper()
	pe := purchasetest.New(t, purchasetest.WithNoRules())
	store, err := files.New(files.Config{
		DataDir: testkit.DataDir(t), DB: pe.DB, Auth: pe.Auth, Info: pe.Engine, Clock: pe.Clock, Audit: audit.Default,
	})
	if err != nil {
		t.Fatalf("grntest.New: files.New: %v", err)
	}
	dt, _ := doctype.Lookup(goodsreceipt.DocType)
	if err := naming.New(pe.DB, pe.Clock, pe.Auth, audit.Default).EnsureSeries(context.Background(), dt.Name, dt.Label, dt.Series); err != nil {
		t.Fatalf("grntest.New: EnsureSeries(%s): %v", dt.Name, err)
	}
	deps := goodsreceipt.Deps{DB: pe.DB, Clock: pe.Clock, Auth: pe.Auth, Audit: audit.Default, Files: store}
	goodsreceipt.Wire(pe.DB, deps)
	return &Env{Env: pe, Files: store, Deps: deps, Service: goodsreceipt.New(deps)}
}

// Line is one GRN line to post: the PO line it receives against and its
// quantities as form values. A blank Rejected posts neither Rejected nor
// Reason.
type Line struct {
	POLineID                   int64
	Received, Rejected, Reason string
}

// GRN saves a receipt as as on PO poID through Engine.Do, with the PO's site
// and project posted (spec §3.1), today's receipt date, a delivery challan
// and one line per l; then submits it unless stop is draft. With no approval
// rule, submit approves. It returns the stored document.
func (e *Env) GRN(t testing.TB, as auth.Principal, poID int64, lines []Line, stop doctype.State) doc.Doc {
	t.Helper()
	po := e.Load(t, purchase.DocType, poID)
	in := doc.ActionInput{Values: map[string]string{
		"purchase_order_id": strconv.FormatInt(poID, 10),
		"site_id":           formInt(po.Header["site_id"]),
		"project_id":        formInt(po.Header["project_id"]),
		"receipt_date":      types.Today(e.Clock).ISO(),
		"challan_kind":      "Delivery challan",
		"challan_no":        "DC-1042",
	}}
	for _, l := range lines {
		m := map[string]string{"po_line_id": strconv.FormatInt(l.POLineID, 10), "received_qty": l.Received}
		if l.Rejected != "" {
			m["rejected_qty"], m["reject_reason"] = l.Rejected, l.Reason
		}
		in.Lines = append(in.Lines, m)
	}
	res, err := e.Engine.Do(context.Background(), as, goodsreceipt.DocType, 0, doctype.ActionSave, in)
	if err != nil {
		t.Fatalf("grntest: save GRN on PO %d as %s: %v %+v", poID, as.Name, err, fieldErrors(err))
	}
	if stop != doctype.StateDraft {
		res = e.do(t, as, goodsreceipt.DocType, res.ID, res.Version, doctype.ActionSubmit, "")
	}
	if res.State != stop {
		t.Fatalf("grntest: GRN %d is %s, want %s", res.ID, res.State, stop)
	}
	return e.Load(t, goodsreceipt.DocType, res.ID)
}

// Cancel cancels GRN id as as with reason and returns the stored GRN.
func (e *Env) Cancel(t testing.TB, as auth.Principal, id int64, reason string) doc.Doc {
	t.Helper()
	return e.act(t, as, goodsreceipt.DocType, id, doctype.ActionCancel, reason)
}

// Amend amends GRN id as as and returns the new draft.
func (e *Env) Amend(t testing.TB, as auth.Principal, id int64) doc.Doc {
	t.Helper()
	return e.act(t, as, goodsreceipt.DocType, id, doctype.ActionAmend, "")
}

// ClosePO closes PO id as as with reason and returns the stored PO.
func (e *Env) ClosePO(t testing.TB, as auth.Principal, id int64, reason string) doc.Doc {
	t.Helper()
	return e.act(t, as, purchase.DocType, id, doctype.ActionClose, reason)
}

// CancelPO cancels PO id as as with reason and returns the stored PO.
func (e *Env) CancelPO(t testing.TB, as auth.Principal, id int64, reason string) doc.Doc {
	t.Helper()
	return e.act(t, as, purchase.DocType, id, doctype.ActionCancel, reason)
}

// Lock sets the period lock to upTo through modkit's settings, as the
// Administrator (PO spec §14.2).
func (e *Env) Lock(t testing.TB, upTo types.Date) {
	t.Helper()
	ctx := context.Background()
	kit := modkit.New(modkit.Deps{DB: e.DB, Auth: e.Auth, Audit: audit.Default, Clock: e.Clock})
	s, err := kit.GetSettings(ctx, e.Admin)
	if err != nil {
		t.Fatalf("grntest: read the business settings: %v", err)
	}
	in := modkit.SettingsInput{LockedUpTo: sql.Null[types.Date]{V: upTo, Valid: true}, DefaultPOTerms: s.DefaultPOTerms}
	if err := kit.SaveSettings(ctx, e.Admin, s.Version, in); err != nil {
		t.Fatalf("grntest: lock up to %s: %v", upTo.ISO(), err)
	}
}

// SecondStores creates a second non-admin Stores user restricted to
// METRO-SITE through the real AUTH (GRN-015) and returns the principal.
func (e *Env) SecondStores(t testing.TB) auth.Principal {
	t.Helper()
	ctx := context.Background()
	id, _, err := e.Auth.CreateUser(ctx, e.Admin, auth.NewUser{ //nolint:gosec // G101: a test user's password
		Email: "gate2@vardhan.test", Name: "Gate Two", Password: "Receive the trucks 42", Roles: []string{"Stores"},
	})
	if err != nil {
		t.Fatalf("grntest: create the second Stores user: %v", err)
	}
	site, err := strconv.ParseInt(e.masterID(t, "site", "METRO-SITE"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Auth.ChangeRestrictions(ctx, e.Admin, id, auth.DimSite, []int64{site}, nil, nil); err != nil {
		t.Fatalf("grntest: restrict the second Stores user: %v", err)
	}
	p, _, err := e.Auth.LoadPrincipal(ctx, id)
	if err != nil {
		t.Fatalf("grntest: load the second Stores user: %v", err)
	}
	return p
}

// Counts are row counts for rollback checks.
type Counts struct {
	Receipts, Lines, Bills, Audit int
}

// RowCounts counts the rows in goods_receipt, goods_receipt_line, grn_bill
// and the audit log.
func (e *Env) RowCounts(t testing.TB) Counts {
	t.Helper()
	var c Counts
	if err := e.DB.SQL().QueryRowContext(context.Background(), `SELECT
		(SELECT count(*) FROM goods_receipt), (SELECT count(*) FROM goods_receipt_line),
		(SELECT count(*) FROM grn_bill), (SELECT count(*) FROM audit_log)`).Scan(&c.Receipts, &c.Lines, &c.Bills, &c.Audit); err != nil {
		t.Fatalf("grntest: row counts: %v", err)
	}
	return c
}

// POAs creates a PO as as through Engine.Do, for a PO purchasetest.NewOrder
// can't make: NewOrder always raises it as Priya, who is restricted to
// PRJ-METRO, and a PO on another site is raised by admin (00-fixtures.md).
// purchasetest's options can't be read outside it, so header and lines are
// form values merged over NewOrder's defaults (vendor SHREE, METRO-SITE /
// PRJ-METRO, ordered today; one line of 100 kg MS-ANGLE-50 at ₹62.50, GST18;
// no lines means one default line). stop is NewOrder's: draft saves, pending
// submits, approved also approves as each eligible fixture user until the PO
// is approved. It returns the stored PO.
func POAs(t testing.TB, env *Env, as auth.Principal, stop doctype.State, header map[string]string, lines ...map[string]string) doc.Doc {
	t.Helper()
	ctx := context.Background()
	in := doc.ActionInput{Values: map[string]string{
		"vendor_id":  env.masterID(t, "party", "SHREE"),
		"site_id":    env.masterID(t, "site", "METRO-SITE"),
		"project_id": env.masterID(t, "project", "PRJ-METRO"),
		"order_date": types.Today(env.Clock).ISO(),
	}}
	maps.Copy(in.Values, header)
	if len(lines) == 0 {
		lines = []map[string]string{{}}
	}
	for _, l := range lines {
		m := map[string]string{
			"item_id": env.masterID(t, "item", "MS-ANGLE-50"), "uom_id": env.masterID(t, "uom", "KGS"),
			"qty": "100", "price": "62.5", "tax_code_id": env.masterID(t, "tax_code", "GST18"), "hsn_sac": "7216",
		}
		maps.Copy(m, l)
		in.Lines = append(in.Lines, m)
	}
	res, err := env.Engine.Do(ctx, as, purchase.DocType, 0, doctype.ActionSave, in)
	if err != nil {
		t.Fatalf("grntest: save PO as %s: %v %+v", as.Name, err, fieldErrors(err))
	}
	if stop != doctype.StateDraft {
		res = env.do(t, as, purchase.DocType, res.ID, res.Version, doctype.ActionSubmit, "")
		for stop == doctype.StateApproved && res.State == doctype.StatePending {
			res = env.do(t, env.approver(t, res.ID), purchase.DocType, res.ID, res.Version, doctype.ActionApprove, "")
		}
	}
	if res.State != stop {
		t.Fatalf("grntest: PO %d is %s, want %s", res.ID, res.State, stop)
	}
	return env.Load(t, purchase.DocType, res.ID)
}

// approver is the first fixture user APR lets approve PO id now.
func (e *Env) approver(t testing.TB, id int64) auth.Principal {
	t.Helper()
	ctx := context.Background()
	var ids []types.UserID
	if err := e.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		ids, err = e.Approvals.EligibleApprovers(ctx, tx, purchase.DocType, id)
		return err
	}); err != nil {
		t.Fatalf("grntest: approvers of PO %d: %v", id, err)
	}
	for _, p := range []auth.Principal{e.Admin, e.Meena, e.Ravi, e.Priya, e.Suresh, e.Kavya, e.Deepak, e.Arjun} {
		if slices.Contains(ids, p.ID) {
			return p
		}
	}
	t.Fatalf("grntest: no fixture user may approve PO %d (eligible %v)", id, ids)
	return auth.Principal{}
}

// Load reads a stored document of DocType dt.
func (e *Env) Load(t testing.TB, dt string, id int64) doc.Doc {
	t.Helper()
	var out doc.Doc
	if err := e.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		out, err = doc.LoadDoc(context.Background(), tx, dt, id)
		return err
	}); err != nil {
		t.Fatalf("grntest: load %s %d: %v", dt, id, err)
	}
	return out
}

// act runs action a with reason on document id at its stored version and
// returns the document the action left (the new draft for amend).
func (e *Env) act(t testing.TB, as auth.Principal, dt string, id int64, a doctype.Action, reason string) doc.Doc {
	t.Helper()
	res := e.do(t, as, dt, id, e.Load(t, dt, id).Version, a, reason)
	return e.Load(t, dt, res.ID)
}

// do runs action a on document id as as at version.
func (e *Env) do(t testing.TB, as auth.Principal, dt string, id int64, version int, a doctype.Action, reason string) doc.Result {
	t.Helper()
	res, err := e.Engine.Do(context.Background(), as, dt, id, a, doc.ActionInput{Version: version, Reason: reason})
	if err != nil {
		t.Fatalf("grntest: %s %s %d as %s: %v %+v", a, dt, id, as.Name, err, fieldErrors(err))
	}
	return res
}

// masterID is the id, as a form value, of the master row with code in table.
func (e *Env) masterID(t testing.TB, table, code string) string {
	t.Helper()
	var id int64
	if err := e.DB.SQL().QueryRowContext(context.Background(), `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&id); err != nil { //nolint:gosec // G202: fixed table names
		t.Fatalf("grntest: id of %s %s: %v", table, code, err)
	}
	return strconv.FormatInt(id, 10)
}

// formInt is a stored Int or Link value as a form value; blank when unset.
func formInt(v any) string {
	if n, ok := v.(int64); ok {
		return strconv.FormatInt(n, 10)
	}
	return ""
}

// fieldErrors are err's field errors, for a builder's failure message.
func fieldErrors(err error) []doctype.FieldError {
	var he doctype.HTTPError
	if errors.As(err, &he) {
		return he.FieldErrors()
	}
	return nil
}
