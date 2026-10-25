package fixture

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

func claimDocType() doctype.DocType {
	own := rights("own", "read", "create", "write", "submit", "print")
	own["read"] = "team" // team includes own (CD-11)
	own["approve"] = "team"
	return doctype.DocType{
		Name: Claim, Label: "Expense Claim", PluralLabel: "Expense Claims",
		Table: "fixture_claim", LinesTable: "fixture_claim_line", Series: "EX/{FY}/{#####}",
		Submittable: true, Approvable: true, Printable: true,
		OwnerField:    "created_by",
		EmployeeField: "employee_id",
		DefaultRights: map[string]map[string]string{
			"Employee": own,
			"Director": rights("all", "read", "approve", "print", "export"),
		},
		Fields: []doctype.Field{
			// A testkit_employee id; no master link in Wave 1.
			{Name: "employee_id", Label: "Employee", Type: doctype.Int, Required: true, ListPriority: 1},
			{Name: "claim_date", Label: "Claim date", Type: doctype.Date, Required: true, ListPriority: 4},
			{Name: "total", Label: "Total", Type: doctype.Money, ReadOnly: true, ListPriority: 2}, // CD-79
			{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500},
			// The state convention column, for list filters (CD-144, CD-162).
			{Name: "state", Label: "State", Type: doctype.Select, ReadOnly: true, Filterable: true, Options: []string{
				string(doctype.StateDraft), string(doctype.StatePending), string(doctype.StateApproved),
				string(doctype.StateClosed), string(doctype.StateRejected), string(doctype.StateCancelled),
			}},

			{Name: "expense_type", Label: "Expense type", Type: doctype.Text, Required: true, OnLine: true, Step: "lines"},
			{Name: "description", Label: "Description", Type: doctype.Text, OnLine: true, Step: "lines"},
			{Name: "amount", Label: "Amount", Type: doctype.Money, Required: true, Positive: true, OnLine: true, Step: "lines"},
		},
		Hooks: claimHooks{hookBase{"Claim"}},
	}
}

type claimHooks struct{ hookBase }

// Validate sets total to the sum of the line amounts (CD-79), then returns
// Probe.ValidateErrors. A failed update panics; the engine recovers hook
// panics into a plain error and rolls back (Review Focus 3).
func (h claimHooks) Validate(ctx context.Context, tx *db.Tx, id int64) []doctype.FieldError {
	if err := h.enter(ctx, "Validate"); err != nil {
		return []doctype.FieldError{{Message: err.Error()}} // form-level, see hookBase.validate
	}
	if _, err := tx.SQL().ExecContext(ctx,
		`UPDATE fixture_claim SET total = (SELECT COALESCE(SUM(amount), 0) FROM fixture_claim_line WHERE parent_id = ?) WHERE id = ?`,
		id, id); err != nil {
		panic(fmt.Sprintf("fixture: Claim.Validate: set total of claim %d: %v", id, err))
	}
	return ProbeFrom(ctx).validateErrors()
}

func (h claimHooks) BeforeSubmit(ctx context.Context, _ *db.Tx, _ int64) error {
	return h.enter(ctx, "BeforeSubmit")
}

func (h claimHooks) ApplyEffects(ctx context.Context, _ *db.Tx, _ int64) error {
	return h.enter(ctx, "ApplyEffects")
}

func (h claimHooks) ReverseEffects(ctx context.Context, _ *db.Tx, _ int64) error {
	return h.enter(ctx, "ReverseEffects")
}

func (claimHooks) Amount(ctx context.Context, tx *db.Tx, id int64) (types.Money, error) {
	var amt types.Money
	err := tx.SQL().QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount), 0) FROM fixture_claim_line WHERE parent_id = ?`, id).Scan(&amt)
	return amt, err
}

func (claimHooks) Date(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	return readDate(ctx, tx, `SELECT claim_date FROM fixture_claim WHERE id = ?`, id)
}
