package naming

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// errEffect is the failing approval effect: the whole approval rolls back.
var errEffect = errors.New("fuzz: effect failed")

// fuzzOps is the number of lifecycle steps the fuzz input picks from.
const fuzzOps = 10

// FuzzNaming_GaplessInvariant covers NAM-037.
func FuzzNaming_GaplessInvariant(f *testing.F) {
	f.Add([]byte{0, 1, 4, 0, 1, 3, 4})
	f.Add([]byte{0, 1, 4, 7, 8, 4})
	ramp := make([]byte, 64)
	for i := range ramp {
		ramp[i] = byte(i)
	}
	f.Add(ramp)
	// Hits every op successfully: create, submit, withdraw, submit, reject,
	// revise, submit, failing approve, approve, cancel, amend, create, delete
	// draft, submit and approve the amendment, create.
	f.Add([]byte{0, 1, 9, 1, 2, 8, 1, 4, 3, 6, 7, 0, 5, 11, 13, 10})

	f.Fuzz(func(t *testing.T, ops []byte) {
		if len(ops) > 200 {
			ops = ops[:200]
		}
		h := newHarness(t)
		dates := []types.Date{day(2025, 5, 1), day(2026, 3, 31), day(2026, 4, 1), day(2026, 10, 24)}
		var ids []int64
		for i, b := range ops {
			op := int(b) % fuzzOps
			if op == 0 {
				ids = append(ids, h.create(dtPO, dates[i%len(dates)]))
				continue
			}
			if len(ids) == 0 {
				continue
			}
			// The next byte picks the document (the last op wraps to the first byte).
			id := ids[int(ops[(i+1)%len(ops)])%len(ids)]
			var err error
			switch op {
			case 1:
				err = h.submit(id, true)
			case 2:
				err = h.reject(id)
			case 3:
				err = h.approve(id)
			case 4:
				err = h.approveWithEffect(id, func(*db.Tx) error { return errEffect })
			case 5:
				err = h.deleteDraft(id)
			case 6:
				err = h.cancel(id)
			case 7:
				var child int64
				if child, err = h.amend(id); err == nil {
					ids = append(ids, child)
				}
			case 8:
				err = h.revise(id)
			case 9:
				err = h.withdraw(id)
			}
			if err != nil && !expectedFuzzErr(err) {
				t.Fatalf("op %d (%d) on doc %d: unexpected error: %v", i, op, id, err)
			}
		}
		checkInvariants(t, h)
	})
}

// expectedFuzzErr reports whether err is a refusal the random sequence can
// legitimately hit: a wrong-state transition, a deleted document, a second
// live amendment, the tenth amendment, or the failing effect. Anything else
// (a duplicate number, a missing series) is a bug.
func expectedFuzzErr(err error) bool {
	return strings.HasPrefix(err.Error(), "harness: can't ") ||
		(strings.HasPrefix(err.Error(), "load doc ") && errors.Is(err, sql.ErrNoRows)) ||
		errors.Is(err, ErrAlreadyAmended) ||
		errors.Is(err, ErrAmendmentLimit) ||
		errors.Is(err, errEffect)
}

// checkInvariants checks NAM-037's invariants from SQL alone.
func checkInvariants(t *testing.T, h *harness) {
	t.Helper()
	ctx := context.Background()
	if err := h.db.Read(ctx, func(tx *db.Tx) error {
		q := tx.SQL()

		// Each counter equals its number of root (non-amendment) ledger rows,
		// whose seq values are exactly 1..last and render as prefix + seq.
		type counter struct {
			doctype, prefix string
			last            int64
		}
		var counters []counter
		if err := queryRows(ctx, q, `SELECT doctype, prefix, last FROM naming_counters`, func(r *sql.Rows) error {
			var c counter
			if err := r.Scan(&c.doctype, &c.prefix, &c.last); err != nil {
				return err
			}
			counters = append(counters, c)
			return nil
		}); err != nil {
			return err
		}
		for _, c := range counters {
			var seqs []int64
			if err := queryRows(ctx, q,
				`SELECT seq FROM naming_issued WHERE doctype = ? AND prefix = ? AND amendment_no = 0 ORDER BY seq`,
				func(r *sql.Rows) error {
					var s int64
					if err := r.Scan(&s); err != nil {
						return err
					}
					seqs = append(seqs, s)
					return nil
				}, c.doctype, c.prefix); err != nil {
				return err
			}
			if int64(len(seqs)) != c.last {
				t.Errorf("counter (%s, %s) last = %d, but %d root numbers issued", c.doctype, c.prefix, c.last, len(seqs))
			}
			for i, s := range seqs {
				if s != int64(i+1) {
					t.Errorf("counter (%s, %s): root seqs = %v, want 1..%d", c.doctype, c.prefix, seqs, len(seqs))
					break
				}
			}
		}
		checkZero(ctx, t, q, "root ledger rows without a counter",
			`SELECT COUNT(*) FROM naming_issued i WHERE amendment_no = 0 AND NOT EXISTS (
				SELECT 1 FROM naming_counters c WHERE c.doctype = i.doctype AND c.prefix = i.prefix)`)
		checkZero(ctx, t, q, "root numbers not prefix + 5-digit seq",
			`SELECT COUNT(*) FROM naming_issued WHERE amendment_no = 0
			 AND (number != prefix || printf('%05d', seq) OR root != number)`)

		// Every number is unique across documents and the ledger.
		checkZero(ctx, t, q, "duplicate document numbers",
			`SELECT COUNT(*) FROM (SELECT number FROM nam_test_docs WHERE number IS NOT NULL
			 GROUP BY number HAVING COUNT(*) > 1)`)
		checkZero(ctx, t, q, "duplicate ledger numbers",
			`SELECT COUNT(*) FROM (SELECT number FROM naming_issued GROUP BY number HAVING COUNT(*) > 1)`)
		checkZero(ctx, t, q, "ledger numbers held by another document",
			`SELECT COUNT(*) FROM naming_issued i JOIN nam_test_docs d ON d.number = i.number
			 WHERE d.doctype != i.doctype OR d.id != i.doc_id`)

		// Each root's amendments are exactly 1..m, numbered root-k.
		checkZero(ctx, t, q, "amendment numbers not root-k",
			`SELECT COUNT(*) FROM naming_issued WHERE amendment_no > 0 AND number != root || '-' || amendment_no`)
		checkZero(ctx, t, q, "amendments without a root number",
			`SELECT COUNT(*) FROM naming_issued a WHERE amendment_no > 0 AND NOT EXISTS (
				SELECT 1 FROM naming_issued r WHERE r.amendment_no = 0 AND r.number = a.root AND r.doctype = a.doctype)`)
		checkZero(ctx, t, q, "roots whose amendment numbers are not 1..m",
			`SELECT COUNT(*) FROM (SELECT doctype, root FROM naming_issued WHERE amendment_no > 0 GROUP BY doctype, root
			 HAVING MIN(amendment_no) != 1 OR MAX(amendment_no) != COUNT(DISTINCT amendment_no)
			     OR COUNT(*) != COUNT(DISTINCT amendment_no))`)

		// Every numbered document has its own ledger row with that number,
		// root and k; every ledger row belongs to a document holding it.
		checkZero(ctx, t, q, "numbered documents without a matching ledger row",
			`SELECT COUNT(*) FROM nam_test_docs d WHERE number IS NOT NULL AND NOT EXISTS (
				SELECT 1 FROM naming_issued i WHERE i.doctype = d.doctype AND i.doc_id = d.id
				AND i.number = d.number AND i.amendment_no = d.amendment_no
				AND (d.amendment_no = 0 OR i.root = d.amendment_root))`)
		checkZero(ctx, t, q, "ledger rows whose document doesn't hold the number",
			`SELECT COUNT(*) FROM naming_issued i WHERE NOT EXISTS (
				SELECT 1 FROM nam_test_docs d WHERE d.doctype = i.doctype AND d.id = i.doc_id AND d.number = i.number)`)

		// Only approved and later states carry a number, and they always do.
		checkZero(ctx, t, q, "draft, pending or rejected documents with a number",
			`SELECT COUNT(*) FROM nam_test_docs WHERE number IS NOT NULL
			 AND state IN ('draft', 'pending_approval', 'rejected')`)
		checkZero(ctx, t, q, "approved, closed or cancelled documents without a number",
			`SELECT COUNT(*) FROM nam_test_docs WHERE number IS NULL
			 AND state IN ('approved', 'closed', 'cancelled')`)
		return nil
	}); err != nil {
		t.Fatalf("checkInvariants: %v", err)
	}
}

func queryRows(ctx context.Context, q db.DBTX, query string, scan func(*sql.Rows) error, args ...any) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func checkZero(ctx context.Context, t *testing.T, q db.DBTX, what, query string) {
	t.Helper()
	var n int
	if err := q.QueryRowContext(ctx, query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if n != 0 {
		t.Errorf("%s: %d", what, n)
	}
}
