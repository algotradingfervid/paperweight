package gstcalc_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// storedLine and storedPO are the rows of the test-only tables numt_po_line and numt_po.
type storedLine struct {
	ID               int64
	Amount           types.Money
	GSTRate          types.Percent
	CGST, SGST, IGST types.Money
}

type storedPO struct {
	SupplierState, BranchState                        string
	Net, CGST, SGST, IGST, Grand, Rounded, Adjustment types.Money
	Lines                                             []storedLine
}

// createRecomputeTables creates the test-only draft tables (INTEGER/TEXT only, CD-34).
func createRecomputeTables(t *testing.T, d *db.DB) {
	t.Helper()
	ctx := t.Context()
	err := d.Write(ctx, func(tx *db.Tx) error {
		if _, err := tx.SQL().ExecContext(ctx, `CREATE TABLE numt_po (
			id INTEGER PRIMARY KEY,
			supplier_state TEXT NOT NULL,
			branch_state TEXT NOT NULL,
			net INTEGER NOT NULL DEFAULT 0,
			cgst INTEGER NOT NULL DEFAULT 0,
			sgst INTEGER NOT NULL DEFAULT 0,
			igst INTEGER NOT NULL DEFAULT 0,
			grand INTEGER NOT NULL DEFAULT 0,
			rounded INTEGER NOT NULL DEFAULT 0,
			adjustment INTEGER NOT NULL DEFAULT 0)`); err != nil {
			return err
		}
		_, err := tx.SQL().ExecContext(ctx, `CREATE TABLE numt_po_line (
			id INTEGER PRIMARY KEY,
			po_id INTEGER NOT NULL REFERENCES numt_po(id),
			amount INTEGER NOT NULL,
			gst_rate INTEGER NOT NULL,
			cgst INTEGER NOT NULL DEFAULT 0,
			sgst INTEGER NOT NULL DEFAULT 0,
			igst INTEGER NOT NULL DEFAULT 0)`)
		return err
	})
	if err != nil {
		t.Fatalf("creating test tables: %v", err)
	}
}

// insertDraft stores a draft with its lines (taxes not yet worked out) and returns its id.
func insertDraft(t *testing.T, d *db.DB, supplierState, branchState string, lines []gstcalc.Line) int64 {
	t.Helper()
	ctx := t.Context()
	var id int64
	err := d.Write(ctx, func(tx *db.Tx) error {
		res, err := tx.SQL().ExecContext(ctx,
			`INSERT INTO numt_po (supplier_state, branch_state) VALUES (?, ?)`, supplierState, branchState)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		for _, l := range lines {
			if _, err := tx.SQL().ExecContext(ctx,
				`INSERT INTO numt_po_line (po_id, amount, gst_rate) VALUES (?, ?, ?)`, id, l.Amount, l.GSTRate); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inserting draft: %v", err)
	}
	return id
}

// loadPO reads the draft and its lines (in id order) inside tx.
func loadPO(ctx context.Context, tx *db.Tx, id int64) (storedPO, error) {
	var po storedPO
	err := tx.SQL().QueryRowContext(ctx,
		`SELECT supplier_state, branch_state, net, cgst, sgst, igst, grand, rounded, adjustment
		 FROM numt_po WHERE id = ?`, id).
		Scan(&po.SupplierState, &po.BranchState, &po.Net, &po.CGST, &po.SGST, &po.IGST, &po.Grand, &po.Rounded, &po.Adjustment)
	if err != nil {
		return po, err
	}
	rows, err := tx.SQL().QueryContext(ctx,
		`SELECT id, amount, gst_rate, cgst, sgst, igst FROM numt_po_line WHERE po_id = ? ORDER BY id`, id)
	if err != nil {
		return po, err
	}
	for rows.Next() {
		var l storedLine
		if err := rows.Scan(&l.ID, &l.Amount, &l.GSTRate, &l.CGST, &l.SGST, &l.IGST); err != nil {
			_ = rows.Close()
			return po, err
		}
		po.Lines = append(po.Lines, l)
	}
	if err := rows.Close(); err != nil {
		return po, err
	}
	return po, rows.Err()
}

// recompute works the taxes out again from the stored states and lines and saves them,
// reading and writing inside one db.Write (CD-94).
func recompute(t *testing.T, d *db.DB, id int64) {
	t.Helper()
	ctx := t.Context()
	err := d.Write(ctx, func(tx *db.Tx) error {
		po, err := loadPO(ctx, tx, id)
		if err != nil {
			return err
		}
		inter := gstcalc.IsInterState(po.SupplierState, po.BranchState)
		lines := make([]gstcalc.Line, len(po.Lines))
		for i, l := range po.Lines {
			lines[i] = gstcalc.Line{Amount: l.Amount, GSTRate: l.GSTRate, InterState: inter}
		}
		tot := gstcalc.Totals(lines, true)
		if len(tot.Lines) != len(po.Lines) {
			t.Errorf("Totals returned %d lines, want %d", len(tot.Lines), len(po.Lines))
			return nil
		}
		for i, lt := range tot.Lines {
			if _, err := tx.SQL().ExecContext(ctx,
				`UPDATE numt_po_line SET cgst = ?, sgst = ?, igst = ? WHERE id = ?`,
				lt.CGST, lt.SGST, lt.IGST, po.Lines[i].ID); err != nil {
				return err
			}
		}
		_, err = tx.SQL().ExecContext(ctx,
			`UPDATE numt_po SET net = ?, cgst = ?, sgst = ?, igst = ?, grand = ?, rounded = ?, adjustment = ? WHERE id = ?`,
			tot.Net, tot.CGST, tot.SGST, tot.IGST, tot.Grand, tot.Rounded, tot.Adjustment, id)
		return err
	})
	if err != nil {
		t.Fatalf("recompute: %v", err)
	}
}

func readPO(t *testing.T, d *db.DB, id int64) storedPO {
	t.Helper()
	ctx := t.Context()
	var po storedPO
	err := d.Read(ctx, func(tx *db.Tx) error {
		var err error
		po, err = loadPO(ctx, tx, id)
		return err
	})
	if err != nil {
		t.Fatalf("reading draft: %v", err)
	}
	return po
}

// NUM-026: NUM proves the recomputed values are identical; "DOC records no
// audit change" needs DOC's real engine and is WP3's (CD-40).
func TestTotals_RecomputeIsStable(t *testing.T) { // NUM-026
	d := testkit.NewDB(t)
	createRecomputeTables(t, d)

	supplier, err := gstcalc.SupplierState("27AABCS1429B1ZU", "") // SHREE
	mustState(t, `SupplierState("27AABCS1429B1ZU", "")`, supplier, err, "27")
	branch, err := gstcalc.BranchState("Pune Works", "27AAFCV2449G1Z7", "") // PUNE
	mustState(t, `BranchState("Pune Works", "27AAFCV2449G1Z7", "")`, branch, err, "27")

	// The reference PO, stored and saved with its Totals(…, true).
	id := insertDraft(t, d, supplier, branch, []gstcalc.Line{
		{Amount: 7500000, GSTRate: 18000},
		{Amount: 480000, GSTRate: 18000},
	})
	recompute(t, d, id)
	stored := readPO(t, d, id)
	if stored.Net != 7980000 || stored.CGST != 718200 || stored.SGST != 718200 || stored.IGST != 0 ||
		stored.Grand != 9416400 || stored.Rounded != 9416400 || stored.Adjustment != 0 {
		t.Fatalf("stored reference PO = %+v; want Net 7980000, CGST 718200, SGST 718200, IGST 0, Grand 9416400, Rounded 9416400, Adjustment 0", stored)
	}

	for pass := 1; pass <= 2; pass++ {
		recompute(t, d, id)
		if got := readPO(t, d, id); !reflect.DeepEqual(stored, got) {
			t.Errorf("recompute %d changed the draft:\n got  %+v\n want %+v", pass, got, stored)
		}
	}
}

func TestGST_RecomputeOnPartyChange(t *testing.T) { // NUM-041
	d := testkit.NewDB(t)
	ctx := t.Context()
	createRecomputeTables(t, d)

	// Draft from SHREE (27) to PUNE (27): ₹1,000.00 at 18%.
	id := insertDraft(t, d, "27", "27", []gstcalc.Line{{Amount: 100000, GSTRate: 18000}})
	recompute(t, d, id)
	before := readPO(t, d, id)
	if len(before.Lines) != 1 {
		t.Fatalf("draft has %d lines, want 1", len(before.Lines))
	}
	if l := before.Lines[0]; l.CGST != 9000 || l.SGST != 9000 || l.IGST != 0 {
		t.Fatalf("before change line CGST/SGST/IGST = %d/%d/%d, want 9000/9000/0", l.CGST, l.SGST, l.IGST)
	}
	if before.Grand != 118000 {
		t.Fatalf("before change Grand = %d, want 118000", before.Grand)
	}

	// The supplier changes to BHARAT (29) and the draft is saved.
	err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE numt_po SET supplier_state = ? WHERE id = ?`, "29", id)
		return err
	})
	if err != nil {
		t.Fatalf("changing supplier: %v", err)
	}
	recompute(t, d, id)

	after := readPO(t, d, id)
	if len(after.Lines) != 1 {
		t.Fatalf("draft has %d lines after change, want 1", len(after.Lines))
	}
	if l := after.Lines[0]; l.IGST != 18000 || l.CGST != 0 || l.SGST != 0 {
		t.Errorf("after change line CGST/SGST/IGST = %d/%d/%d, want 0/0/18000", l.CGST, l.SGST, l.IGST)
	}
	if after.IGST != 18000 || after.CGST != 0 || after.SGST != 0 {
		t.Errorf("after change header CGST/SGST/IGST = %d/%d/%d, want 0/0/18000", after.CGST, after.SGST, after.IGST)
	}
	if after.Grand != 118000 {
		t.Errorf("after change Grand = %d, want 118000 (unchanged)", after.Grand)
	}
}
