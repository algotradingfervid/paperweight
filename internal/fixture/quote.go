package fixture

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// quoteDocType is the five-cell editable line with a money input (M0-40c):
// Item, Quantity, Rate, Amount and Description. Its amount input sits in a
// narrow track beside the rate and quantity ones, the line grid's widest row.
func quoteDocType() doctype.DocType {
	return doctype.DocType{
		Name: Quote, Label: "Quotation", PluralLabel: "Quotations",
		Table: "fixture_quote", LinesTable: "fixture_quote_line", Series: "QT/{FY}/{#####}",
		TestOnly:   true, // CD-291
		OwnerField: "created_by",
		DefaultRights: map[string]map[string]string{
			"Purchase User": rights("all", "read", "create", "write"),
		},
		Fields: []doctype.Field{
			{Name: "quote_date", Label: "Quote date", Type: doctype.Date, Required: true, ListPriority: 1},
			{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500},
			// The state convention column, for list filters (CD-144, CD-162).
			{Name: "state", Label: "State", Type: doctype.Select, ReadOnly: true, Filterable: true, Options: []string{
				string(doctype.StateDraft), string(doctype.StatePending), string(doctype.StateApproved),
				string(doctype.StateClosed), string(doctype.StateRejected), string(doctype.StateCancelled),
			}},

			{Name: "item_id", Label: "Item", Type: doctype.Link, Link: Item, Required: true, OnLine: true, Step: "lines"},
			{Name: "qty", Label: "Quantity", Type: doctype.Qty, Required: true, Positive: true, OnLine: true, Step: "lines"},
			{Name: "rate", Label: "Rate", Type: doctype.Rate, NonNegative: true, OnLine: true, Step: "lines"},
			{Name: "amount", Label: "Amount", Type: doctype.Money, Required: true, Positive: true, OnLine: true, Step: "lines"},
			{Name: "description", Label: "Description", Type: doctype.Text, OnLine: true, Step: "lines"},
		},
		Hooks: quoteHooks{hookBase{"Quote"}},
	}
}

type quoteHooks struct{ hookBase }

func (h quoteHooks) Validate(ctx context.Context, _ *db.Tx, _ int64) []doctype.FieldError {
	return h.validate(ctx)
}

func (h quoteHooks) BeforeSubmit(ctx context.Context, _ *db.Tx, _ int64) error {
	return h.enter(ctx, "BeforeSubmit")
}

func (h quoteHooks) ApplyEffects(ctx context.Context, _ *db.Tx, _ int64) error {
	return h.enter(ctx, "ApplyEffects")
}

func (h quoteHooks) ReverseEffects(ctx context.Context, _ *db.Tx, _ int64) error {
	return h.enter(ctx, "ReverseEffects")
}

// Amount is the sum of the line amounts.
func (quoteHooks) Amount(ctx context.Context, tx *db.Tx, id int64) (types.Money, error) {
	var amt types.Money
	err := tx.SQL().QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount), 0) FROM fixture_quote_line WHERE parent_id = ?`, id).Scan(&amt)
	return amt, err
}

func (quoteHooks) Date(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	return readDate(ctx, tx, `SELECT quote_date FROM fixture_quote WHERE id = ?`, id)
}
