package fixture

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

func receiptDocType() doctype.DocType {
	return doctype.DocType{
		Name: Receipt, Label: "Goods Receipt", PluralLabel: "Goods Receipts",
		Table: "fixture_receipt", LinesTable: "fixture_receipt_line", Series: "GR/{FY}/{#####}", // CD-113
		Submittable: true, Approvable: true, Printable: true, // CD-82
		OwnerField: "created_by",
		DefaultRights: map[string]map[string]string{
			"Stores": rights("all", "read", "create", "write", "submit", "print"),
		},
		Fields: []doctype.Field{
			{Name: "receipt_date", Label: "Receipt date", Type: doctype.Date, Required: true, ListPriority: 4},
			{Name: "remarks", Label: "Remarks", Type: doctype.Text},

			{Name: "order_line_id", Label: "Purchase order line", Type: doctype.Int, Required: true, OnLine: true, Step: "lines"},
			{Name: "qty", Label: "Quantity", Type: doctype.Qty, Required: true, Positive: true, OnLine: true, Step: "lines"},
		},
		Links:   []doctype.LinkSpec{{Field: "order_line_id", Target: Order, OnLine: true}},
		RollUps: []doctype.RollUp{{TargetDocType: Order, TargetField: "received_qty", SourceField: "qty", JoinField: "order_line_id"}},
		Hooks:   receiptHooks{hookBase{"Receipt"}},
	}
}

type receiptHooks struct{ hookBase }

func (h receiptHooks) Validate(ctx context.Context, _ *db.Tx, _ int64) []doctype.FieldError {
	return h.validate(ctx)
}

// BeforeSubmit refuses, when Probe.ForbidOverReceipt is set, a line that would
// take its order line past the ordered quantity, counting approved and closed
// receipts.
func (h receiptHooks) BeforeSubmit(ctx context.Context, tx *db.Tx, id int64) error {
	if err := h.enter(ctx, "BeforeSubmit"); err != nil {
		return err
	}
	if !ProbeFrom(ctx).forbidOverReceipt() {
		return nil
	}
	rows, err := tx.SQL().QueryContext(ctx, `
		SELECT rl.line_no, rl.qty, COALESCE(ol.qty, 0), ol.line_no, o.id, COALESCE(o.number, ''),
		       (SELECT COALESCE(SUM(x.qty), 0) FROM fixture_receipt_line x JOIN fixture_receipt r ON r.id = x.parent_id
		         WHERE x.order_line_id = rl.order_line_id AND r.state IN ('approved', 'closed') AND r.id <> rl.parent_id)
		FROM fixture_receipt_line rl
		JOIN fixture_order_line ol ON ol.id = rl.order_line_id
		JOIN fixture_order o ON o.id = ol.parent_id
		WHERE rl.parent_id = ?
		ORDER BY rl.line_no`, id)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			lineNo, qty, ordered, orderLineNo, orderID, received int64
			number                                               string
		)
		if err := rows.Scan(&lineNo, &qty, &ordered, &orderLineNo, &orderID, &number, &received); err != nil {
			return err
		}
		if received+qty > ordered {
			return doc.Refused(fmt.Sprintf("Line %d: receiving %s would take %s line %d to %s of %s. Reduce the quantity.",
				lineNo, plainQty(qty), naming.Label(number, orderID), orderLineNo, plainQty(received+qty), plainQty(ordered)))
		}
	}
	return rows.Err()
}

// ApplyEffects records the order line's received_qty at apply time in
// Probe.Seen, then writes one "apply" effect log row per line.
func (h receiptHooks) ApplyEffects(ctx context.Context, tx *db.Tx, id int64) error {
	if err := h.enter(ctx, "ApplyEffects"); err != nil {
		return err
	}
	if p := ProbeFrom(ctx); p != nil {
		var received int64
		err := tx.SQL().QueryRowContext(ctx, `
			SELECT ol.received_qty FROM fixture_receipt_line rl JOIN fixture_order_line ol ON ol.id = rl.order_line_id
			WHERE rl.parent_id = ? AND rl.line_no = 1`, id).Scan(&received)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		p.see("received_qty_at_apply", received)
	}
	return h.effects(ctx, tx, id, "ApplyEffects", "apply")
}

// ReverseEffects writes one "reverse" effect log row per line.
func (h receiptHooks) ReverseEffects(ctx context.Context, tx *db.Tx, id int64) error {
	if err := h.enter(ctx, "ReverseEffects"); err != nil {
		return err
	}
	return h.effects(ctx, tx, id, "ReverseEffects", "reverse")
}

// effects writes the effect log rows. With Probe.FailAfterWrite[hook] it
// instead adds line 1's qty to its order line's received_qty and returns the
// error, so a test can see the write roll back.
func (receiptHooks) effects(ctx context.Context, tx *db.Tx, id int64, hook, action string) error {
	if err := ProbeFrom(ctx).failAfterWrite(hook); err != nil {
		if _, werr := tx.SQL().ExecContext(ctx, `
			UPDATE fixture_order_line SET received_qty = received_qty + rl.qty
			FROM fixture_receipt_line rl WHERE rl.parent_id = ? AND rl.line_no = 1 AND fixture_order_line.id = rl.order_line_id`,
			id); werr != nil {
			return werr
		}
		return err
	}
	_, err := tx.SQL().ExecContext(ctx, `
		INSERT INTO fixture_effect_log (receipt_id, order_line_id, action)
		SELECT parent_id, order_line_id, ? FROM fixture_receipt_line WHERE parent_id = ? ORDER BY line_no`,
		action, id)
	return err
}

// Amount is the sum of each line's qty × its order line's rate (CD-157).
func (receiptHooks) Amount(ctx context.Context, tx *db.Tx, id int64) (types.Money, error) {
	rows, err := tx.SQL().QueryContext(ctx, `
		SELECT COALESCE(rl.qty, 0), COALESCE(ol.rate, 0)
		FROM fixture_receipt_line rl JOIN fixture_order_line ol ON ol.id = rl.order_line_id
		WHERE rl.parent_id = ?`, id)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var total types.Money
	for rows.Next() {
		var (
			qty  types.Qty
			rate types.Rate
		)
		if err := rows.Scan(&qty, &rate); err != nil {
			return 0, err
		}
		amt, err := qty.Times(rate)
		if err != nil {
			return 0, err
		}
		total += amt
	}
	return total, rows.Err()
}

func (receiptHooks) Date(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	return readDate(ctx, tx, `SELECT receipt_date FROM fixture_receipt WHERE id = ?`, id)
}
