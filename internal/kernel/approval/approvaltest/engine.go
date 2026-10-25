package approvaltest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files/filesfake"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The engine driver (Task 12b): Env's actions run the real doc.Engine over
// internal/fixture's DocTypes with the real naming.Service, filesfake and
// audit.Default. APR_DRIVER=ref selects the reference driver in env.go.

// useEngine reports whether APR_DRIVER selects the engine driver.
func useEngine(t testing.TB) bool {
	t.Helper()
	switch v := os.Getenv("APR_DRIVER"); v {
	case "engine":
		return true
	case "", "ref":
		// Engine becomes the default once the DOC/AUTH deviations in
		// .work/status/apr-12b.md are settled (AD-3 escalation).
		return false
	default:
		t.Fatalf("APR_DRIVER=%q: want engine or ref", v)
		return false
	}
}

// testDate is the business date of every engine document (00-fixtures).
const testDate = "2026-10-24"

// setupEngine adds the fixture tables, masters and number series to the
// harness database. It runs before approval.New, which takes the engine as
// its InfoSource.
func (e *Env) setupEngine(t testing.TB) {
	t.Helper()
	ctx := context.Background()
	MustOK(t, db.Migrate(ctx, e.DB, fixture.MigrationsFS, "goose_fixture"))
	fixture.SeedMasters(t, e.DB)
	e.Audit = dbAudit{e.DB}
	e.namer = naming.New(e.DB, e.Clock, e.Auth, e.Audit)
	for _, name := range []string{Order, Receipt, Claim} {
		dt, _ := doctype.Lookup(name)
		MustOK(t, e.namer.EnsureSeries(ctx, dt.Name, dt.Label, dt.Series))
	}
	// The InfoSource needs no APR calls; the acting engine is built per call
	// with e.A (or a wrapper of it), which needs the InfoSource first.
	e.eng = e.engine(nil)
}

// engine returns a doc.Engine on the harness's services with svc as APR.
func (e *Env) engine(svc approval.Service) doc.Engine {
	return doc.NewEngine(doc.Deps{
		DB: e.DB, Clock: e.Clock, Auth: e.Auth, Naming: e.namer, Approval: svc,
		Files: filesfake.New(e.Clock), Audit: e.Audit,
	})
}

// failAfterRecord is APR with an error after a successful Record, so the
// engine's transaction rolls back (FailAfterRecord).
type failAfterRecord struct {
	approval.Service
	err error
}

func (f failAfterRecord) Record(ctx context.Context, tx *db.Tx, dt string, id int64, a doctype.Action, p auth.Principal, comment string) error {
	if err := f.Service.Record(ctx, tx, dt, id, a, p, comment); err != nil {
		return err
	}
	return f.err
}

// do runs action a through doc.Engine.Do, posting the document's currently
// loaded version (CD-94). The fixture hooks report their ApplyEffects and
// ReverseEffects calls to a probe; after a committed action they are added
// to the document's row in aprtest_effects, the test-only effects table on
// the fixture DocTypes (CD-142).
func (e *Env) do(p auth.Principal, d Doc, a doctype.Action, in doc.ActionInput, failAfter error) error {
	svc := approval.Service(e.A)
	if failAfter != nil {
		svc = failAfterRecord{e.A, failAfter}
	}
	in.Version = int(e.Version(e.t, d))
	probe := &fixture.Probe{}
	ctx := fixture.WithProbe(context.Background(), probe)
	if _, err := e.engine(svc).Do(ctx, p, d.Type, d.ID, a, in); err != nil {
		return err
	}
	n := 0
	for _, h := range probe.Hooks() {
		switch h {
		case "ApplyEffects":
			n++
		case "ReverseEffects":
			n--
		}
	}
	if n != 0 {
		// Upsert: the first effect of a document inserts its row, later ones add to it.
		e.exec(e.t, `INSERT INTO aprtest_effects (doctype, id, n) VALUES (?, ?, ?)
			ON CONFLICT (doctype, id) DO UPDATE SET n = n + excluded.n`, d.Type, d.ID, n)
	}
	return nil
}

// engineSave loads the document as form values (CD-43), applies changes and
// saves it.
func (e *Env) engineSave(p auth.Principal, d Doc, changes map[string]string) error {
	var rec doc.Record
	MustOK(e.t, e.DB.Read(context.Background(), func(tx *db.Tx) (err error) {
		rec, err = e.eng.Load(context.Background(), tx, d.Type, d.ID)
		return err
	}))
	for k, v := range changes {
		rest, ok := strings.CutPrefix(k, "lines.")
		if !ok {
			rec.Values[k] = v
			continue
		}
		pos, field, _ := strings.Cut(rest, ".")
		n, err := strconv.Atoi(pos)
		if err != nil || n < 1 || n > len(rec.Lines) {
			e.t.Fatalf("approvaltest: Save change %q: no line %s", k, pos)
		}
		rec.Lines[n-1][field] = v
	}
	return e.do(p, d, doctype.ActionSave, doc.ActionInput{Values: rec.Values, Lines: rec.Lines}, nil)
}

// engineOffered is every action that DOC's rights check, CheckApprover and
// guard table allow p on the document (checks after DOC's version check, such
// as amend's limit, are not probed),
// asked of the engine itself (CD-142): each action is posted with a version
// that can never match, so DOC runs its rights check, CheckApprover and its
// guard table, then refuses as stale before writing anything. A stale
// refusal means rights and guard allowed the action.
func (e *Env) engineOffered(p auth.Principal, d Doc) []doctype.Action {
	var out []doctype.Action
	for _, a := range doctype.Actions() {
		_, err := e.engine(e.A).Do(context.Background(), p, d.Type, d.ID, a, doc.ActionInput{Version: -1})
		var de *doc.Error
		if errors.As(err, &de) && de.Kind == doc.KindStale {
			out = append(out, a)
		}
	}
	return out
}

// engineVersion is the document's stored version.
func (e *Env) engineVersion(t testing.TB, d Doc) int64 {
	t.Helper()
	var x doc.Doc
	MustOK(t, e.DB.Read(context.Background(), func(tx *db.Tx) (err error) {
		x, err = doc.LoadDoc(context.Background(), tx, d.Type, d.ID)
		return err
	}))
	return int64(x.Version)
}

// newEngineDoc writes a fixture document with id and one valid line straight
// into its tables at version 1, as DOC's own tests set up rows: a save can't
// choose the id or the state. An Order gets supplier s.party (Shree Steel
// Traders by default), project s.project and amount; a Claim one line of
// amount; a Receipt one line on an approved order, and no amount (fixture.Receipt
// has none).
func (e *Env) newEngineDoc(t testing.TB, dt string, id int64, amount types.Money, creator auth.Principal, s docSpec, emp int64) {
	t.Helper()
	now := types.TimestampOf(e.Clock.Now()).String()
	by := int64(creator.ID)
	ctx := context.Background()
	MustOK(t, e.DB.Write(ctx, func(tx *db.Tx) error {
		ex := func(q string, args ...any) error {
			_, err := tx.SQL().ExecContext(ctx, q, args...)
			return err
		}
		switch dt {
		case Order:
			return e.insertOrder(ctx, tx, ex, id, s, amount, now, by)
		case Claim:
			if err := ex(`INSERT INTO fixture_claim (id, number, state, created_at, created_by, updated_at, updated_by, employee_id, claim_date, total)
				VALUES (?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, s.number, string(s.state), now, by, now, by, emp, testDate, int64(amount)); err != nil {
				return err
			}
			return ex(`INSERT INTO fixture_claim_line (parent_id, line_no, expense_type, amount) VALUES (?, 1, 'Travel', ?)`, id, int64(amount))
		case Receipt:
			// The order the receipt line points to; ids far from the tests' own.
			src := 100_000 + id
			if err := e.insertOrder(ctx, tx, ex, src, docSpec{state: doctype.StateApproved, number: fmt.Sprintf("PO/SRC/%d", src)}, amount, now, by); err != nil {
				return err
			}
			if err := ex(`INSERT INTO fixture_receipt (id, number, state, created_at, created_by, updated_at, updated_by, receipt_date)
				VALUES (?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?)`,
				id, s.number, string(s.state), now, by, now, by, testDate); err != nil {
				return err
			}
			return ex(`INSERT INTO fixture_receipt_line (parent_id, line_no, order_line_id, qty)
				SELECT ?, 1, id, 1000 FROM fixture_order_line WHERE parent_id = ?`, id, src)
		}
		return fmt.Errorf("approvaltest: no engine document type %s", dt)
	}))
}

func (e *Env) insertOrder(ctx context.Context, tx *db.Tx, ex func(string, ...any) error, id int64, s docSpec, amount types.Money, now string, by int64) error {
	supplier, err := partyID(ctx, tx, s.party, now)
	if err != nil {
		return err
	}
	if err := ex(`INSERT INTO fixture_order (id, number, state, created_at, created_by, updated_at, updated_by,
			supplier_id, project_id, order_date, amount, priority)
		VALUES (?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, NULLIF(?, 0), ?, ?, 'normal')`,
		id, s.number, string(s.state), now, by, now, by, supplier, s.project, testDate, int64(amount)); err != nil {
		return err
	}
	return ex(`INSERT INTO fixture_order_line (parent_id, line_no, item_id, qty, rate, uom)
		SELECT ?, 1, id, 1000, ?, 'Nos' FROM fixture_item WHERE code = 'MS-ANGLE-50'`, id, int64(amount)*100)
}

// partyID is the fixture party named name (Shree Steel Traders when empty),
// added with the name as its code when the fixtures have none.
func partyID(ctx context.Context, tx *db.Tx, name, now string) (int64, error) {
	if name == "" {
		name = "Shree Steel Traders"
	}
	var id int64
	err := tx.SQL().QueryRowContext(ctx, `SELECT id FROM fixture_party WHERE name = ?`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.SQL().QueryRowContext(ctx, `INSERT INTO fixture_party (code, name, created_at, created_by, updated_at, updated_by)
			VALUES (?, ?, ?, 1, ?, 1) RETURNING id`, name, name, now, now).Scan(&id)
	}
	return id, err
}

// engineSetAmount changes the stored amount as a save would: the Order's
// amount, or the Claim's one line and total. A Receipt has no amount.
func (e *Env) engineSetAmount(t testing.TB, d Doc, m types.Money) {
	t.Helper()
	switch d.Type {
	case Order:
		e.exec(t, `UPDATE fixture_order SET amount = ? WHERE id = ?`, int64(m), d.ID)
	case Claim:
		e.exec(t, `UPDATE fixture_claim_line SET amount = ? WHERE parent_id = ?`, int64(m), d.ID)
		e.exec(t, `UPDATE fixture_claim SET total = ? WHERE id = ?`, int64(m), d.ID)
	}
}

// engineCounter is the last number NAM issued under prefix (0 when none).
func (e *Env) engineCounter(t testing.TB, prefix string) int {
	t.Helper()
	var n int
	MustOK(t, e.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(),
			`SELECT COALESCE(MAX(last), 0) FROM naming_counters WHERE prefix = ?`, prefix).Scan(&n)
	}))
	return n
}

// dbAudit is audit.Default with Entries read back from audit_log, so
// entries roll back with their transaction like every other engine write.
type dbAudit struct{ db *db.DB }

func (a dbAudit) Append(ctx context.Context, tx *db.Tx, en audit.Entry) error {
	return audit.Default.Append(ctx, tx, en)
}

// Entries is every entry in audit_log order, read through audit.ForDoc.
func (a dbAudit) Entries() []audit.Entry {
	type key struct {
		dt string
		id int64
	}
	var out []audit.Entry
	err := a.db.Read(context.Background(), func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(context.Background(), `SELECT doc_type, doc_id FROM audit_log ORDER BY id`)
		if err != nil {
			return err
		}
		var order []key
		for rows.Next() {
			var k key
			if err := rows.Scan(&k.dt, &k.id); err != nil {
				_ = rows.Close()
				return err
			}
			order = append(order, k)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		byDoc := map[key][]audit.Entry{}
		for _, k := range order {
			if _, ok := byDoc[k]; ok {
				continue
			}
			if byDoc[k], err = audit.ForDoc(context.Background(), tx, k.dt, k.id); err != nil {
				return err
			}
		}
		for _, k := range order {
			out = append(out, byDoc[k][0])
			byDoc[k] = slices.Delete(byDoc[k], 0, 1)
		}
		return nil
	})
	if err != nil {
		panic(fmt.Sprintf("approvaltest: read audit_log: %v", err))
	}
	return out
}
