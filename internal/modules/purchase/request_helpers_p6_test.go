package purchase_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Helpers for P6's tests (MOD-PO P6, request rows and order status). Every
// document row is written through Engine.Do or the purchasetest builders;
// the only SQL write is purchasetest.SetReceived (CD-280 D7 (b)). All names
// carry p6.

// The §20.5 texts (PO spec §20.5), copied exactly. One condition each
// (CD-147).
const (
	p6MsgSite        = "Line %d: Request %s is for site %s. This order delivers to %s."                   // n, PR, request site, PO site
	p6MsgProject     = "Line %d: Request %s is for project %s. This order is for %s."                     // n, PR, request project, PO project
	p6MsgItem        = "Line %d: Request %s line %d is for %s. Remove the row to order a different item." // n, PR, m, item code
	p6MsgUoM         = "Line %d: Request %s line %d is in %s. Keep that unit."                            // n, PR, m, uom code
	p6MsgSameRow     = "Line %d: Request %s line %d is already on line %d."                               // n, PR, m, k
	p6MsgAlreadyOn   = "Line %d: Request %s line %d is already on %s."                                    // n, PR, m, PO display
	p6MsgOverRequest = "Line %d: ordering %s more than request %s asked for."                             // n, x, PR
)

// DOC's three link texts for a request row's request (doc/messages.go
// msgSourceNotApproved, msgSourceClosed, msgSourceCancelled, with DOC's line
// prefix; CD-142: the producing WP's text).
const (
	p6DocNotApproved = "Line %d: %s isn't approved yet. Pick an approved purchase request."
	p6DocClosed      = "Line %d: %s is closed. Remove the line or pick another purchase request."
	p6DocCancelled   = "Line %d: %s is cancelled. Remove the line or pick another purchase request."
)

// Reasons for the actions that need one (DOC: at least 3 characters).
const (
	p6RejectReason = "Rate is too high"
	p6CancelReason = "Vendor can't supply"
	p6CloseReason  = "Rest not needed"
)

// p6Bag is a PO line of qty Bag of CEMENT-OPC at ₹1,500.00 (18%), linked to
// request row rowID (0 = no link). 40 Bag is ₹70,800.00, above the
// ₹50,000 Purchase Manager rule.
func p6Bag(t *testing.T, env *purchasetest.Env, qty string, rowID int64) map[string]string {
	t.Helper()
	l := p2Line(t, env, "CEMENT-OPC", "BAG", qty, "1500")
	l["hsn_sac"] = "2523"
	if rowID != 0 {
		l["request_line_id"] = strconv.FormatInt(rowID, 10)
	}
	return l
}

// p6Link is line l linked to request row rowID.
func p6Link(l map[string]string, rowID int64) map[string]string {
	l["request_line_id"] = strconv.FormatInt(rowID, 10)
	return l
}

// p6Request saves a request as p with header and lines and submits it
// (straight to approved with a number). It returns the stored request.
func p6Request(t *testing.T, env *purchasetest.Env, p auth.Principal, header map[string]string, lines []map[string]string) doc.Doc {
	t.Helper()
	res := p5Must(t, "save request as "+p.Name)(p5Create(t, env, p, header, lines))
	p5Must(t, "submit request as "+p.Name)(p5Do(t, env, p, res.ID, doctype.ActionSubmit, ""))
	d := p5Load(t, env, purchase.RequestDocType, res.ID)
	if d.State != doctype.StateApproved || d.Number == "" || len(d.Lines) != len(lines) {
		t.Fatalf("request %d is %s %q with %d lines, want submitted with %d", d.ID, d.State, d.Number, len(d.Lines), len(lines))
	}
	return d
}

// p6CementRequest is PR-A as Suresh: METRO-SITE / PRJ-METRO, one row of
// qty Bag of CEMENT-OPC.
func p6CementRequest(t *testing.T, env *purchasetest.Env, qty string) doc.Doc {
	t.Helper()
	return p6Request(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"),
		[]map[string]string{p5Line(t, env, "CEMENT-OPC", "BAG", qty)})
}

// p6Order saves a PO as p (SHREE unless header says otherwise, METRO-SITE /
// PRJ-METRO) and takes it to stop: draft, pending (submitted) or approved
// (Ravi approves while it is pending). It returns the stored PO.
func p6Order(t *testing.T, env *purchasetest.Env, p auth.Principal, header map[string]string, lines []map[string]string, stop doctype.State) doc.Doc {
	t.Helper()
	if header == nil {
		header = p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO")
	}
	res := p2Must(t, "save order as "+p.Name)(p2Create(t, env, p, header, lines))
	if stop != doctype.StateDraft {
		res = p2Must(t, "submit order as "+p.Name)(p2Do(t, env, p, res.ID, doc.ActionSubmit, ""))
		if stop == doctype.StateApproved && res.State == doctype.StatePending {
			res = p2Must(t, "Ravi approves")(p2Do(t, env, env.Ravi, res.ID, doc.ActionApprove, ""))
		}
	}
	d := p2Load(t, env, res.ID)
	if d.State != stop {
		t.Fatalf("order %d is %s, want %s", d.ID, d.State, stop)
	}
	return d
}

// p6RuleFromZero gives env a purchase.Order rule from ₹0 (Purchase Manager),
// so every PO goes to pending on submit; use with WithNoRules.
func p6RuleFromZero(t testing.TB, env *purchasetest.Env) {
	t.Helper()
	if _, err := env.Approvals.CreateRule(t.Context(), env.Admin, approval.RuleInput{
		DocType: purchase.DocType, FromAmount: 0, ApproverType: approval.ApproverRole, Role: "Purchase Manager",
	}); err != nil {
		t.Fatalf("approval rule from 0: %v", err)
	}
}

// p6Q parses a quantity literal.
func p6Q(t testing.TB, s string) types.Qty {
	t.Helper()
	q, err := types.ParseQty(s)
	if err != nil {
		t.Fatalf("qty %q: %v", s, err)
	}
	return q
}

// p6Row is a request row's stored ordered_qty and its request's
// order_status.
type p6Row struct {
	ordered types.Qty
	status  string
}

func p6ReadRow(t testing.TB, env *purchasetest.Env, lineID int64) p6Row {
	t.Helper()
	var r p6Row
	if err := env.DB.SQL().QueryRowContext(t.Context(),
		`SELECT l.ordered_qty, h.order_status FROM purchase_request_line l JOIN purchase_request h ON h.id = l.parent_id WHERE l.id = ?`,
		lineID).Scan(&r.ordered, &r.status); err != nil {
		t.Fatalf("ordered_qty of request row %d: %v", lineID, err)
	}
	return r
}

// p6Facts are what RefreshOrderStatus must leave alone on a request: its
// version, updated_at, updated_by and audit entries (CD-280 D3).
type p6Facts struct {
	version   int
	updatedAt string
	updatedBy int64
	audits    int
}

func p6ReadFacts(t testing.TB, env *purchasetest.Env, dt string, id int64) p6Facts {
	t.Helper()
	q := `SELECT version, updated_at, updated_by FROM purchase_request WHERE id = ?`
	if dt == purchase.DocType {
		q = `SELECT version, updated_at, updated_by FROM purchase_order WHERE id = ?`
	}
	var f p6Facts
	if err := env.DB.SQL().QueryRowContext(t.Context(), q, id).Scan(&f.version, &f.updatedAt, &f.updatedBy); err != nil {
		t.Fatalf("facts of %s %d: %v", dt, id, err)
	}
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM audit_log WHERE doc_type = ? AND doc_id = ?`, dt, id).Scan(&f.audits); err != nil {
		t.Fatalf("audits of %s %d: %v", dt, id, err)
	}
	return f
}

// p6Pending is purchase.PendingToOrder for rows, in a read tx.
func p6Pending(t testing.TB, env *purchasetest.Env, rows ...int64) map[int64]types.Qty {
	t.Helper()
	var m map[int64]types.Qty
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		m, err = purchase.PendingToOrder(t.Context(), tx, rows)
		return err
	}); err != nil {
		t.Fatalf("PendingToOrder(%v): %v", rows, err)
	}
	return m
}

// p6HeldBy is purchase.HeldBy for request id, in a read tx.
func p6HeldBy(t testing.TB, env *purchasetest.Env, id int64) []purchase.Holder {
	t.Helper()
	var hs []purchase.Holder
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		hs, err = purchase.HeldBy(t.Context(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("HeldBy(%d): %v", id, err)
	}
	return hs
}

// p6Held is what draft, pending and rejected POs hold of row (HeldBy's
// lines in those states, §20.2).
func p6Held(t testing.TB, env *purchasetest.Env, requestID, row int64) types.Qty {
	t.Helper()
	var held types.Qty
	for _, h := range p6HeldBy(t, env, requestID) {
		if h.RequestLineID != row {
			continue
		}
		switch h.State {
		case doctype.StateDraft, doctype.StatePending, doctype.StateRejected:
			held += h.Qty
		}
	}
	return held
}

// p6Linked is purchase.LinkedOrders for request id, in a read tx.
func p6Linked(t testing.TB, env *purchasetest.Env, id int64) []purchase.LinkedOrder {
	t.Helper()
	var los []purchase.LinkedOrder
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		los, err = purchase.LinkedOrders(t.Context(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("LinkedOrders(%d): %v", id, err)
	}
	return los
}

// p6Refresh runs purchase.RefreshOrderStatus for ids inside one db.Write.
func p6Refresh(t testing.TB, env *purchasetest.Env, ids ...int64) error {
	t.Helper()
	return env.DB.Write(t.Context(), func(tx *db.Tx) error {
		return purchase.RefreshOrderStatus(t.Context(), tx, ids)
	})
}

// p6Label is DOC's display of a document: its number, or Draft {id}.
func p6Label(d doc.Doc) string { return naming.Label(d.Number, d.ID) }

// p6Walk is one row of a quantity walk table: after step, request row's
// ordered_qty, held and pending to order, and the request's order_status.
type p6Walk struct {
	step                   string
	do                     func(t *testing.T)
	ordered, held, pending string
	status                 string
}

// p6RunWalk runs each step and asserts its row for request req's row, and
// that no step bumped the request's version or added an audit entry to it
// (CD-280 D3) unless the step itself acted on the request (actsOnRequest).
func p6RunWalk(t *testing.T, env *purchasetest.Env, req doc.Doc, row int64, steps []p6Walk, actsOnRequest map[string]bool) {
	t.Helper()
	for _, s := range steps {
		before := p6ReadFacts(t, env, purchase.RequestDocType, req.ID)
		s.do(t)
		got := p6ReadRow(t, env, row)
		held := p6Held(t, env, req.ID, row)
		pending := p6Pending(t, env, row)[row]
		if got.ordered != p6Q(t, s.ordered) || held != p6Q(t, s.held) || pending != p6Q(t, s.pending) || got.status != s.status {
			t.Errorf("%s: ordered %s held %s pending %s status %q; want %s, %s, %s, %q", s.step,
				got.ordered.Plain(), held.Plain(), pending.Plain(), got.status, s.ordered, s.held, s.pending, s.status)
		}
		if !actsOnRequest[s.step] {
			if after := p6ReadFacts(t, env, purchase.RequestDocType, req.ID); after != before {
				t.Errorf("%s: request facts %+v, want %+v unchanged (plain UPDATEs, no audit; CD-280 D3)", s.step, after, before)
			}
		}
	}
}

// p6Fmt is fmt.Sprintf, short for the message tables.
func p6Fmt(format string, a ...any) string { return fmt.Sprintf(format, a...) }
