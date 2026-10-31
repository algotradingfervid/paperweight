package purchase_test

import (
	"database/sql"
	"errors"
	"regexp"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// schemaTS is a stored timestamp at authtest.T0.
const schemaTS = "2026-10-24T05:00:00.000Z"

var autoincrementID = regexp.MustCompile(`(?is)\bid\s+INTEGER\s+PRIMARY\s+KEY\s+AUTOINCREMENT\b`)

// tableSQL is a table's CREATE statement, or "" when it doesn't exist.
func tableSQL(t *testing.T, d *db.DB, table string) string {
	t.Helper()
	var s string
	err := d.SQL().QueryRowContext(t.Context(), `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("read the schema of %s: %v", table, err)
	}
	return s
}

// countRows counts table's rows matching where (args bound).
func countRows(t *testing.T, d *db.DB, table, where string, args ...any) int {
	t.Helper()
	var n int
	if err := d.SQL().QueryRowContext(t.Context(), `SELECT count(*) FROM `+table+` WHERE `+where, args...).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// MOD-PO P1 schema tests (PO spec §3, §20.2; CD-156): every purchase id is
// INTEGER PRIMARY KEY AUTOINCREMENT, so a deleted newest id is never reused.
func TestPurchaseSchema_Autoincrement(t *testing.T) {
	d := testkit.NewDB(t)
	for _, table := range []string{"purchase_order", "purchase_order_line", "purchase_request", "purchase_request_line"} {
		s := tableSQL(t, d, table)
		if s == "" {
			t.Errorf("table %s doesn't exist", table)
			continue
		}
		if !autoincrementID.MatchString(s) {
			t.Errorf("%s: id is not INTEGER PRIMARY KEY AUTOINCREMENT (CD-156):\n%s", table, s)
		}
	}

	// Through the real path: delete the newest draft, and the next draft gets
	// a higher id.
	env := purchasetest.New(t)
	purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	newest := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	if _, err := env.Engine.Do(t.Context(), env.Priya, purchase.DocType, newest.ID, doctype.ActionDelete, doc.ActionInput{Version: newest.Version}); err != nil {
		t.Fatalf("delete draft %d as Priya: %v", newest.ID, err)
	}
	if next := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft()); next.ID <= newest.ID {
		t.Errorf("draft after deleting id %d got id %d, want > %d", newest.ID, next.ID, newest.ID)
	}
}

// MOD-PO P1 schema tests (PO spec §3, §20.2, §23, §27.1, §27.2; CD-161): line
// tables have parent_id ON DELETE CASCADE, and so do purchase_order_sent,
// purchase_order_expected and purchase_order_rest on po_id. Deleting a draft
// through the engine removes its lines and its rows in the three tables.
func TestPurchaseSchema_Cascade(t *testing.T) {
	d := testkit.NewDB(t)
	for _, c := range []struct{ table, from, parent string }{
		{"purchase_order_line", "parent_id", "purchase_order"},
		{"purchase_request_line", "parent_id", "purchase_request"},
		{"purchase_order_sent", "po_id", "purchase_order"},
		{"purchase_order_expected", "po_id", "purchase_order"},
		{"purchase_order_rest", "po_id", "purchase_order"},
	} {
		rows, err := d.SQL().QueryContext(t.Context(), `SELECT "table", "from", "to", on_delete FROM pragma_foreign_key_list(?)`, c.table)
		if err != nil {
			t.Fatalf("foreign keys of %s: %v", c.table, err)
		}
		found := false
		for rows.Next() {
			var parent, from, to, onDelete string
			if err := rows.Scan(&parent, &from, &to, &onDelete); err != nil {
				t.Fatal(err)
			}
			if parent == c.parent && from == c.from && to == "id" && onDelete == "CASCADE" {
				found = true
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		if !found {
			t.Errorf("%s.%s: no REFERENCES %s (id) ON DELETE CASCADE", c.table, c.from, c.parent)
		}
	}

	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	keep := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	pr := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())
	if len(po.Lines) == 0 || len(keep.Lines) == 0 || len(pr.Lines) == 0 {
		t.Fatalf("builder drafts have %d, %d and %d lines, want at least 1 each", len(po.Lines), len(keep.Lines), len(pr.Lines))
	}
	on := types.Date{Year: 2026, Month: 10, Day: 24}
	// The side records exist only for approved POs in real use; the schema
	// test writes them with SQL to see the cascade (plan: schema tests only).
	if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
		for _, id := range []int64{po.ID, keep.ID} {
			if _, err := tx.SQL().ExecContext(t.Context(),
				`INSERT INTO purchase_order_sent (po_id, sent_on, sent_via, note, sent_by, sent_at, version) VALUES (?, ?, 'email', '', ?, ?, 1)`,
				id, on, env.Priya.ID, schemaTS); err != nil {
				return err
			}
			if _, err := tx.SQL().ExecContext(t.Context(),
				`INSERT INTO purchase_order_expected (po_id, expected_on, note, set_by, set_at, version) VALUES (?, ?, '', ?, ?, 1)`,
				id, on, env.Priya.ID, schemaTS); err != nil {
				return err
			}
			if _, err := tx.SQL().ExecContext(t.Context(),
				`INSERT INTO purchase_order_rest (po_id, grn_id, created_by, created_at) VALUES (?, 7, ?, ?)`,
				id, env.Suresh.ID, schemaTS); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("insert the side rows: %v", err)
	}

	if _, err := env.Engine.Do(t.Context(), env.Priya, purchase.DocType, po.ID, doctype.ActionDelete, doc.ActionInput{Version: po.Version}); err != nil {
		t.Fatalf("delete PO draft %d as Priya: %v", po.ID, err)
	}
	if _, err := env.Engine.Do(t.Context(), env.Suresh, purchase.RequestDocType, pr.ID, doctype.ActionDelete, doc.ActionInput{Version: pr.Version}); err != nil {
		t.Fatalf("delete request draft %d as Suresh: %v", pr.ID, err)
	}
	for _, c := range []struct {
		table, col string
		id         int64
		want       int
	}{
		{"purchase_order_line", "parent_id", po.ID, 0},
		{"purchase_order_sent", "po_id", po.ID, 0},
		{"purchase_order_expected", "po_id", po.ID, 0},
		{"purchase_order_rest", "po_id", po.ID, 0},
		{"purchase_request_line", "parent_id", pr.ID, 0},
		// The other draft keeps everything.
		{"purchase_order_line", "parent_id", keep.ID, len(keep.Lines)},
		{"purchase_order_sent", "po_id", keep.ID, 1},
		{"purchase_order_expected", "po_id", keep.ID, 1},
		{"purchase_order_rest", "po_id", keep.ID, 1},
	} {
		if got := countRows(t, env.DB, c.table, c.col+` = ?`, c.id); got != c.want {
			t.Errorf("%s rows for %d after deleting draft %d = %d, want %d", c.table, c.id, po.ID, got, c.want)
		}
	}
}

// MOD-PO P1 schema tests (PO spec §3.2, §20.2; CD-161): both line tables have
// parent_id and line_no, and the builder's lines are numbered from 1.
func TestPurchaseSchema_LineNo(t *testing.T) {
	d := testkit.NewDB(t)
	for _, table := range []string{"purchase_order_line", "purchase_request_line"} {
		cols := map[string]bool{}
		rows, err := d.SQL().QueryContext(t.Context(), `SELECT name, "notnull" FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatalf("columns of %s: %v", table, err)
		}
		for rows.Next() {
			var name string
			var notNull bool
			if err := rows.Scan(&name, &notNull); err != nil {
				t.Fatal(err)
			}
			cols[name] = notNull
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		for _, col := range []string{"parent_id", "line_no"} {
			if notNull, ok := cols[col]; !ok || !notNull {
				t.Errorf("%s.%s: present %v, NOT NULL %v; want a NOT NULL column", table, col, ok, notNull)
			}
		}
	}

	env := purchasetest.New(t)
	two := []map[string]string{{}, {}} // two default lines
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(two...))
	pr := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft(), purchasetest.RequestLines(two...))
	for _, c := range []struct {
		what  string
		lines []doc.Line
	}{{"order", po.Lines}, {"request", pr.Lines}} {
		if len(c.lines) != 2 || c.lines[0].LineNo != 1 || c.lines[1].LineNo != 2 {
			t.Errorf("%s lines = %+v, want two, line_no 1 and 2", c.what, c.lines)
		}
	}
}
