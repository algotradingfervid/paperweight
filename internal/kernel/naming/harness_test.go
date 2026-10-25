package naming

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// The lifecycle harness is the DOC stand-in: it calls NAM exactly where spec
// §3.2 says DOC will. These DocTypes are plain strings; naming never imports
// fixture or doc.
const (
	dtPO  = "fixture.Order"   // Purchase Order
	dtGRN = "fixture.Receipt" // GRN (series GR/…, CD-113)
	dtEXP = "fixture.Claim"   // Expense Claim (CD-25; series EX/…, CD-115)
)

// harnessSeries is what newHarness seeds with EnsureSeries.
var harnessSeries = []struct{ doctype, label, tpl string }{
	{dtPO, "Purchase Order", "PO/{FY}/{#####}"},
	{dtGRN, "GRN", "GR/{FY}/{#####}"},
	{dtEXP, "Expense Claim", "EX/{FY}/{#####}"},
}

type harness struct {
	t     *testing.T
	db    *db.DB
	nm    *Namer
	audit *auditfake.Recorder
	roles *authfake.Fake
}

// testDoc is one row of nam_test_docs as the tests see it.
type testDoc struct {
	State       string
	Number      sql.NullString
	AmendedFrom sql.NullInt64
	AmendmentNo int
}

// harnessNow is the harness clock: the fixture instant 2026-10-24T05:00Z.
var harnessNow = time.Date(2026, time.October, 24, 5, 0, 0, 0, time.UTC)

func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessOn(t, testkit.NewDB(t))
}

// newHarnessOn builds a harness on an existing database (NAM-034 opens a
// second handle on the same file). Setup is idempotent.
func newHarnessOn(t *testing.T, d *db.DB) *harness {
	t.Helper()
	ctx := context.Background()
	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `CREATE TABLE IF NOT EXISTS nam_test_docs(
			id INTEGER PRIMARY KEY,
			doctype TEXT NOT NULL,
			state TEXT NOT NULL,
			doc_date TEXT NOT NULL,
			number TEXT UNIQUE,
			amended_from INTEGER,
			amendment_root TEXT,
			amendment_no INTEGER NOT NULL DEFAULT 0)`)
		return err
	}); err != nil {
		t.Fatalf("create nam_test_docs: %v", err)
	}
	h := &harness{t: t, db: d, audit: auditfake.New(), roles: newRoles()}
	h.nm = New(d, fixedClock(harnessNow), h.roles, h.audit)
	for _, s := range harnessSeries {
		if err := h.nm.EnsureSeries(ctx, s.doctype, s.label, s.tpl); err != nil {
			t.Fatalf("EnsureSeries(%s, %q, %q): %v", s.doctype, s.label, s.tpl, err)
		}
	}
	return h
}

func dateText(d types.Date) string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

func parseDateText(s string) (types.Date, error) {
	tm, err := time.Parse("2006-01-02", s)
	if err != nil {
		return types.Date{}, err
	}
	return types.Date{Year: tm.Year(), Month: tm.Month(), Day: tm.Day()}, nil
}

// row is the full stored document, read inside a transaction (CD-94).
type row struct {
	id            int64
	doctype       string
	state         string
	date          string
	number        sql.NullString
	amendedFrom   sql.NullInt64
	amendmentRoot sql.NullString
	amendmentNo   int
}

func loadDoc(ctx context.Context, q db.DBTX, id int64) (row, error) {
	var r row
	err := q.QueryRowContext(ctx,
		`SELECT id, doctype, state, doc_date, number, amended_from, amendment_root, amendment_no
		 FROM nam_test_docs WHERE id = ?`, id).
		Scan(&r.id, &r.doctype, &r.state, &r.date, &r.number, &r.amendedFrom, &r.amendmentRoot, &r.amendmentNo)
	if err != nil {
		return row{}, fmt.Errorf("load doc %d: %w", id, err)
	}
	return r, nil
}

func wrongState(r row, action string) error {
	return fmt.Errorf("harness: can't %s doc %d in state %s", action, r.id, r.state)
}

// transition moves a document from one state to another in one db.Write.
func (h *harness) transition(id int64, action string, from, to doctype.State) error {
	ctx := context.Background()
	return h.db.Write(ctx, func(tx *db.Tx) error {
		r, err := loadDoc(ctx, tx.SQL(), id)
		if err != nil {
			return err
		}
		if r.state != string(from) {
			return wrongState(r, action)
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE nam_test_docs SET state = ? WHERE id = ?`, string(to), id)
		return err
	})
}

// create inserts a draft. Test goroutine only.
func (h *harness) create(dt string, d types.Date) int64 {
	h.t.Helper()
	ctx := context.Background()
	var id int64
	if err := h.db.Write(ctx, func(tx *db.Tx) error {
		res, err := tx.SQL().ExecContext(ctx,
			`INSERT INTO nam_test_docs(doctype, state, doc_date) VALUES (?, ?, ?)`,
			dt, string(doctype.StateDraft), dateText(d))
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	}); err != nil {
		h.t.Fatalf("create(%s, %s): %v", dt, dateText(d), err)
	}
	return id
}

// approveTx is the approve body (spec §3.2): NAM assigns a number the first
// time only, then the effect runs, then the state changes.
func (h *harness) approveTx(ctx context.Context, tx *db.Tx, r row, effect func(*db.Tx) error) error {
	num := r.number.String
	if !r.number.Valid {
		var err error
		if r.amendmentNo > 0 {
			num, err = h.nm.AssignAmendment(ctx, tx, r.doctype, r.id, r.amendmentRoot.String, r.amendmentNo)
		} else {
			var d types.Date
			if d, err = parseDateText(r.date); err != nil {
				return err
			}
			num, err = h.nm.Assign(ctx, tx, r.doctype, r.id, d)
		}
		if err != nil {
			return err
		}
	}
	if effect != nil {
		if err := effect(tx); err != nil {
			return err
		}
	}
	_, err := tx.SQL().ExecContext(ctx, `UPDATE nam_test_docs SET state = ?, number = ? WHERE id = ?`,
		string(doctype.StateApproved), num, r.id)
	return err
}

// submit: draft → pending_approval, or straight to approved in the same
// transaction when no approval rule applies.
func (h *harness) submit(id int64, needsApproval bool) error {
	if needsApproval {
		return h.transition(id, "submit", doctype.StateDraft, doctype.StatePending)
	}
	ctx := context.Background()
	return h.db.Write(ctx, func(tx *db.Tx) error {
		r, err := loadDoc(ctx, tx.SQL(), id)
		if err != nil {
			return err
		}
		if r.state != string(doctype.StateDraft) {
			return wrongState(r, "submit")
		}
		return h.approveTx(ctx, tx, r, nil)
	})
}

func (h *harness) approve(id int64) error { return h.approveWithEffect(id, nil) }

func (h *harness) approveWithEffect(id int64, effect func(*db.Tx) error) error {
	ctx := context.Background()
	return h.db.Write(ctx, func(tx *db.Tx) error {
		r, err := loadDoc(ctx, tx.SQL(), id)
		if err != nil {
			return err
		}
		if r.state != string(doctype.StatePending) {
			return wrongState(r, "approve")
		}
		return h.approveTx(ctx, tx, r, effect)
	})
}

//nolint:unused // harness step used by later tasks' tests
func (h *harness) reject(id int64) error {
	return h.transition(id, "reject", doctype.StatePending, doctype.StateRejected)
}

//nolint:unused // harness step used by later tasks' tests
func (h *harness) revise(id int64) error {
	return h.transition(id, "revise", doctype.StateRejected, doctype.StateDraft)
}

//nolint:unused // harness step used by later tasks' tests
func (h *harness) close(id int64) error {
	return h.transition(id, "close", doctype.StateApproved, doctype.StateClosed)
}

//nolint:unused // harness step used by later tasks' tests
func (h *harness) cancel(id int64) error {
	return h.transition(id, "cancel", doctype.StateApproved, doctype.StateCancelled)
}

// deleteDraft removes a draft row; a deleted amendment no longer counts as live.
//
//nolint:unused // harness step used by later tasks' tests
func (h *harness) deleteDraft(id int64) error {
	ctx := context.Background()
	return h.db.Write(ctx, func(tx *db.Tx) error {
		r, err := loadDoc(ctx, tx.SQL(), id)
		if err != nil {
			return err
		}
		if r.state != string(doctype.StateDraft) {
			return wrongState(r, "delete")
		}
		_, err = tx.SQL().ExecContext(ctx, `DELETE FROM nam_test_docs WHERE id = ?`, id)
		return err
	})
}

// amend creates the next amendment draft of a cancelled document. It refuses
// a second live amendment with DOC-065's text (CD-27).
//
//nolint:unused // harness step used by later tasks' tests
func (h *harness) amend(id int64) (int64, error) {
	ctx := context.Background()
	var child int64
	err := h.db.Write(ctx, func(tx *db.Tx) error {
		p, err := loadDoc(ctx, tx.SQL(), id)
		if err != nil {
			return err
		}
		if p.state != string(doctype.StateCancelled) {
			return wrongState(p, "amend")
		}
		var exID int64
		var exNumber sql.NullString
		err = tx.SQL().QueryRowContext(ctx,
			`SELECT id, number FROM nam_test_docs WHERE amended_from = ? ORDER BY id LIMIT 1`, id).
			Scan(&exID, &exNumber)
		switch {
		case err == nil:
			return &Error{Kind: ErrAlreadyAmended,
				Msg: fmt.Sprintf(msgAlreadyAmended, p.number.String, Label(exNumber.String, exID))}
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		root := p.number.String
		if p.amendmentNo > 0 {
			root = p.amendmentRoot.String
		}
		res, err := tx.SQL().ExecContext(ctx,
			`INSERT INTO nam_test_docs(doctype, state, doc_date, amended_from, amendment_root, amendment_no)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			p.doctype, string(doctype.StateDraft), p.date, id, root, p.amendmentNo+1)
		if err != nil {
			return err
		}
		child, err = res.LastInsertId()
		return err
	})
	return child, err
}

// copyDoc copies a document as a fresh draft without number or amendment
// link. Test goroutine only.
//
//nolint:unused // harness step used by later tasks' tests
func (h *harness) copyDoc(id int64) int64 {
	h.t.Helper()
	ctx := context.Background()
	var cp int64
	if err := h.db.Write(ctx, func(tx *db.Tx) error {
		r, err := loadDoc(ctx, tx.SQL(), id)
		if err != nil {
			return err
		}
		res, err := tx.SQL().ExecContext(ctx,
			`INSERT INTO nam_test_docs(doctype, state, doc_date) VALUES (?, ?, ?)`,
			r.doctype, string(doctype.StateDraft), r.date)
		if err != nil {
			return err
		}
		cp, err = res.LastInsertId()
		return err
	}); err != nil {
		h.t.Fatalf("copyDoc(%d): %v", id, err)
	}
	return cp
}

// setDate changes the document date. Test goroutine only.
//
//nolint:unused // harness step used by later tasks' tests
func (h *harness) setDate(id int64, d types.Date) {
	h.t.Helper()
	ctx := context.Background()
	if err := h.db.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE nam_test_docs SET doc_date = ? WHERE id = ?`, dateText(d), id)
		return err
	}); err != nil {
		h.t.Fatalf("setDate(%d, %s): %v", id, dateText(d), err)
	}
}

// save runs the number guard and writes nothing when it refuses.
//
//nolint:unused // harness step used by later tasks' tests
func (h *harness) save(id int64, supplied *string) error {
	ctx := context.Background()
	return h.db.Write(ctx, func(tx *db.Tx) error {
		r, err := loadDoc(ctx, tx.SQL(), id)
		if err != nil {
			return err
		}
		var stored *string
		if r.number.Valid {
			stored = &r.number.String
		}
		if err := GuardNumberField(stored, supplied); err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE nam_test_docs SET doc_date = doc_date WHERE id = ?`, id)
		return err
	})
}

// doc reads a document back. Test goroutine only.
func (h *harness) doc(id int64) testDoc {
	h.t.Helper()
	ctx := context.Background()
	var r row
	if err := h.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		r, err = loadDoc(ctx, tx.SQL(), id)
		return err
	}); err != nil {
		h.t.Fatalf("doc(%d): %v", id, err)
	}
	return testDoc{State: r.state, Number: r.number, AmendedFrom: r.amendedFrom, AmendmentNo: r.amendmentNo}
}

// counter returns the last number for (doctype, prefix), 0 when no row.
// Test goroutine only.
func (h *harness) counter(dt, prefix string) int64 {
	h.t.Helper()
	ctx := context.Background()
	var last int64
	if err := h.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		last, err = readLast(ctx, tx.SQL(), dt, prefix)
		return err
	}); err != nil {
		h.t.Fatalf("counter(%s, %s): %v", dt, prefix, err)
	}
	return last
}

// preset sets a counter directly (raw upsert). Test goroutine only.
func (h *harness) preset(dt, prefix string, last int64) {
	h.t.Helper()
	ctx := context.Background()
	ts := harnessNow.Format("2006-01-02T15:04:05.000Z")
	if err := h.db.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx,
			`INSERT INTO naming_counters(doctype, prefix, last, created_at, updated_at) VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(doctype, prefix) DO UPDATE SET last = excluded.last, updated_at = excluded.updated_at`,
			dt, prefix, last, ts, ts)
		return err
	}); err != nil {
		h.t.Fatalf("preset(%s, %s, %d): %v", dt, prefix, last, err)
	}
}

// count runs a SELECT COUNT(*) query. Test goroutine only.
func (h *harness) count(query string, args ...any) int {
	h.t.Helper()
	ctx := context.Background()
	var n int
	if err := h.db.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, query, args...).Scan(&n)
	}); err != nil {
		h.t.Fatalf("count %q: %v", query, err)
	}
	return n
}
