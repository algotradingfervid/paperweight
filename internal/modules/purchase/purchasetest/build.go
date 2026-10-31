package purchasetest

import (
	"context"
	"errors"
	"maps"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Spec is what a builder creates: the header and line form values, and the
// state it stops at. Options are functions over it, so later tasks add theirs
// in purchasetest/<task>.go.
type Spec struct {
	stop   doctype.State
	header map[string]string   // form values, merged over the defaults
	line   map[string]string   // the default line
	lines  []map[string]string // one map per line, merged over the default line
}

// OrderOpt changes the PO NewOrder builds.
type OrderOpt func(*Spec)

// RequestOpt changes the request NewRequest builds.
type RequestOpt func(*Spec)

// StopAtDraft stops after the first save. NewOrder and NewRequest both take it.
func StopAtDraft() func(*Spec) { return func(s *Spec) { s.stop = doctype.StateDraft } }

// StopAtPending stops after submit, which must leave the document pending.
func StopAtPending() func(*Spec) { return func(s *Spec) { s.stop = doctype.StatePending } }

// StopAtApproved submits and approves as each eligible fixture user until the
// document is approved (the default).
func StopAtApproved() func(*Spec) { return func(s *Spec) { s.stop = doctype.StateApproved } }

// OrderLines replaces the default line with one line per map, each merged
// over the default line (an empty map is one default line).
func OrderLines(lines ...map[string]string) OrderOpt { return func(s *Spec) { s.lines = lines } }

// RequestLines replaces the default line with one line per map, each merged
// over the default line (an empty map is one default line).
func RequestLines(lines ...map[string]string) RequestOpt { return func(s *Spec) { s.lines = lines } }

// OrderHeader merges values over the default header.
func OrderHeader(values map[string]string) OrderOpt {
	return func(s *Spec) { maps.Copy(s.header, values) }
}

// RequestHeader merges values over the default header.
func RequestHeader(values map[string]string) RequestOpt {
	return func(s *Spec) { maps.Copy(s.header, values) }
}

// NewOrder creates a PO by Priya through Engine.Do: by default vendor SHREE,
// METRO-SITE / PRJ-METRO, ordered today, one line of 100 kg MS-ANGLE-50 at
// ₹62.50, and approved. It returns the stored document.
func NewOrder(t testing.TB, env *Env, opts ...OrderOpt) doc.Doc {
	t.Helper()
	s := Spec{
		stop: doctype.StateApproved,
		header: map[string]string{
			"vendor_id":  env.id(t, "party", "SHREE"),
			"site_id":    env.id(t, "site", "METRO-SITE"),
			"project_id": env.id(t, "project", "PRJ-METRO"),
			"order_date": types.FormatDate(types.Today(env.Clock)),
		},
		line: map[string]string{
			"item_id": env.id(t, "item", "MS-ANGLE-50"), "uom_id": env.id(t, "uom", "KGS"),
			"qty": "100", "price": "62.5", "tax_code_id": env.id(t, "tax_code", "GST18"), "hsn_sac": "7216",
		},
	}
	for _, o := range opts {
		o(&s)
	}
	return build(t, env, purchase.DocType, env.Priya, s)
}

// NewRequest creates a request by Suresh (PR-A) through Engine.Do: by
// default METRO-SITE / PRJ-METRO, requested today, one line of 100 kg
// MS-ANGLE-50, and submitted. It returns the stored document.
func NewRequest(t testing.TB, env *Env, opts ...RequestOpt) doc.Doc {
	t.Helper()
	s := Spec{
		stop: doctype.StateApproved,
		header: map[string]string{
			"site_id":      env.id(t, "site", "METRO-SITE"),
			"project_id":   env.id(t, "project", "PRJ-METRO"),
			"request_date": types.FormatDate(types.Today(env.Clock)),
		},
		line: map[string]string{"item_id": env.id(t, "item", "MS-ANGLE-50"), "uom_id": env.id(t, "uom", "KGS"), "qty": "100"},
	}
	for _, o := range opts {
		o(&s)
	}
	return build(t, env, purchase.RequestDocType, env.Suresh, s)
}

// build saves the document as owner, then submits and approves it as far as
// s.stop, and loads what is stored.
func build(t testing.TB, env *Env, dt string, owner auth.Principal, s Spec) doc.Doc {
	t.Helper()
	ctx := context.Background()
	in := doc.ActionInput{Values: plainDates(dt, s.header)}
	if s.lines == nil {
		s.lines = []map[string]string{{}}
	}
	for _, l := range s.lines {
		m := maps.Clone(s.line)
		maps.Copy(m, l)
		in.Lines = append(in.Lines, plainDates(dt, m))
	}
	res, err := env.Engine.Do(ctx, owner, dt, 0, doctype.ActionSave, in)
	if err != nil {
		t.Fatalf("purchasetest: save %s as %s: %v %+v", dt, owner.Name, err, fieldErrors(err))
	}
	if s.stop != doctype.StateDraft {
		res = do(t, env, owner, dt, res, doctype.ActionSubmit)
		for s.stop == doctype.StateApproved && res.State == doctype.StatePending {
			res = do(t, env, env.approver(t, dt, res.ID), dt, res, doctype.ActionApprove)
		}
	}
	if res.State != s.stop {
		t.Fatalf("purchasetest: %s %d is %s, want %s", dt, res.ID, res.State, s.stop)
	}
	var out doc.Doc
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		out, err = doc.LoadDoc(ctx, tx, dt, res.ID)
		return err
	}); err != nil {
		t.Fatalf("purchasetest: load %s %d: %v", dt, res.ID, err)
	}
	return out
}

// plainDates turns m's dates from the form's day-first text ("31-03-2026")
// into the engine's plain ISO values; anything ParseDate refuses is left for
// the engine to refuse.
func plainDates(dt string, m map[string]string) map[string]string {
	d, _ := doctype.Lookup(dt)
	for _, f := range d.Fields {
		if v, ok := m[f.Name]; ok && f.Type == doctype.Date {
			if on, err := types.ParseDate(v); err == nil {
				m[f.Name] = on.ISO()
			}
		}
	}
	return m
}

// do runs action a on the document as p at its current version.
func do(t testing.TB, env *Env, p auth.Principal, dt string, res doc.Result, a doctype.Action) doc.Result {
	t.Helper()
	next, err := env.Engine.Do(context.Background(), p, dt, res.ID, a, doc.ActionInput{Version: res.Version})
	if err != nil {
		t.Fatalf("purchasetest: %s %s %d as %s: %v %+v", a, dt, res.ID, p.Name, err, fieldErrors(err))
	}
	return next
}

// fieldErrors are err's field errors, for a builder's failure message.
func fieldErrors(err error) []doctype.FieldError {
	var he doctype.HTTPError
	if errors.As(err, &he) {
		return he.FieldErrors()
	}
	return nil
}

// approver is the first fixture user APR lets approve the open request.
func (e *Env) approver(t testing.TB, dt string, id int64) auth.Principal {
	t.Helper()
	ctx := context.Background()
	var ids []types.UserID
	if err := e.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		ids, err = e.Approvals.EligibleApprovers(ctx, tx, dt, id)
		return err
	}); err != nil {
		t.Fatalf("purchasetest: approvers of %s %d: %v", dt, id, err)
	}
	for _, uid := range ids {
		for _, p := range []auth.Principal{e.Admin, e.Meena, e.Ravi, e.Priya, e.Suresh, e.Kavya, e.Deepak, e.Arjun} {
			if p.ID == uid {
				return p
			}
		}
	}
	t.Fatalf("purchasetest: no fixture user may approve %s %d (eligible %v)", dt, id, ids)
	return auth.Principal{}
}

// id is the id, as a form value, of the master row with code in table.
func (e *Env) id(t testing.TB, table, code string) string {
	t.Helper()
	var id int64
	if err := e.DB.SQL().QueryRowContext(context.Background(), `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&id); err != nil { //nolint:gosec // G202: fixed table names
		t.Fatalf("purchasetest: id of %s %s: %v", table, code, err)
	}
	return strconv.FormatInt(id, 10)
}

// SetReceived sets one PO line's received_qty with one UPDATE, leaving the
// version alone. It stands in for the GRN roll-up (CD-280 D7 (b)) and is the
// one exception to "no SQL writes" in the builders; GRN-014 and GRN-040 test
// the real path.
func SetReceived(t testing.TB, env *Env, lineID int64, qty types.Qty) {
	t.Helper()
	err := env.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), `UPDATE purchase_order_line SET received_qty = ? WHERE id = ?`, qty, lineID)
		return err
	})
	if err != nil {
		t.Fatalf("purchasetest: set received_qty of line %d: %v", lineID, err)
	}
}
