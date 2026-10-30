package fixture

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

func orderDocType() doctype.DocType {
	return doctype.DocType{
		Name: Order, Label: "Purchase Order", PluralLabel: "Purchase Orders",
		Table: "fixture_order", LinesTable: "fixture_order_line", Series: "PO/{FY}/{#####}",
		Submittable: true, Approvable: true, Printable: true,
		OwnerField:    "created_by",
		ProjectFields: []string{"project_id"},
		SearchFields:  []string{"remarks"}, // NUM-125: orders have no party name column
		DefaultRights: map[string]map[string]string{
			"Purchase Manager": rights("all", "read", "approve", "cancel", "amend", "print", "export"),
			"Purchase User": func() map[string]string {
				m := rights("all", "read", "create", "write", "submit", "print", "export")
				m["delete"] = "own"
				return m
			}(),
			"Stores":   rights("all", "read"),
			"Director": rights("all", "read", "approve", "print", "export"),
		},
		Fields: []doctype.Field{
			{Name: "supplier_id", Label: "Supplier", Type: doctype.Link, Link: Party, Required: true, ListPriority: 1},
			{Name: "project_id", Label: "Project", Type: doctype.Link, Link: Project, ListPriority: 5},
			{Name: "order_date", Label: "Order date", Type: doctype.Date, Required: true, ListPriority: 4},
			{Name: "amount", Label: "Amount", Type: doctype.Money, Sortable: true, ListPriority: 2},
			{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500},
			{Name: "credit_days", Label: "Credit days", Type: doctype.Int, Min: i64(0), Max: i64(365),
				Hint: "Days the supplier gives you to pay."},
			{Name: "supplier_ref", Label: "Supplier ref", Type: doctype.Text, MaxLen: 140},
			{Name: "priority", Label: "Priority", Type: doctype.Text, Default: "normal"},
			{Name: "discount_pct", Label: "Discount", Type: doctype.Percent, Default: "0"},
			// The state convention column, for list filters (CD-144, CD-162).
			{Name: "state", Label: "State", Type: doctype.Select, ReadOnly: true, Filterable: true, Options: []string{
				string(doctype.StateDraft), string(doctype.StatePending), string(doctype.StateApproved),
				string(doctype.StateClosed), string(doctype.StateRejected), string(doctype.StateCancelled),
			}},

			{Name: "item_id", Label: "Item", Type: doctype.Link, Link: Item, Required: true, OnLine: true, Step: "lines"},
			{Name: "qty", Label: "Quantity", Type: doctype.Qty, Required: true, Positive: true, OnLine: true, Step: "lines"},
			{Name: "rate", Label: "Rate", Type: doctype.Rate, NonNegative: true, OnLine: true, Step: "lines"},
			{Name: "uom", Label: "Unit", Type: doctype.Text, Default: "Nos", OnLine: true, Step: "lines"},
			{Name: "description", Label: "Description", Type: doctype.Text, OnLine: true, Step: "lines"},
			{Name: "received_qty", Label: "Received quantity", Type: doctype.Qty, RollUp: true, OnLine: true, Step: "lines"},
		},
		Hooks: orderHooks{hookBase{"Order"}},
	}
}

type orderHooks struct{ hookBase }

func (h orderHooks) Validate(ctx context.Context, _ *db.Tx, _ int64) []doctype.FieldError {
	return h.validate(ctx)
}

func (h orderHooks) BeforeSubmit(ctx context.Context, _ *db.Tx, _ int64) error {
	return h.enter(ctx, "BeforeSubmit")
}

func (h orderHooks) ApplyEffects(ctx context.Context, _ *db.Tx, _ int64) error {
	return h.enter(ctx, "ApplyEffects")
}

func (h orderHooks) ReverseEffects(ctx context.Context, _ *db.Tx, _ int64) error {
	return h.enter(ctx, "ReverseEffects")
}

func (orderHooks) Amount(ctx context.Context, tx *db.Tx, id int64) (types.Money, error) {
	var amt types.Money
	err := tx.SQL().QueryRowContext(ctx, `SELECT COALESCE(amount, 0) FROM fixture_order WHERE id = ?`, id).Scan(&amt)
	return amt, err
}

func (orderHooks) Date(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	return readDate(ctx, tx, `SELECT order_date FROM fixture_order WHERE id = ?`, id)
}
