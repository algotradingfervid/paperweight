package fixture_test

import (
	"context"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Supporting test for M0-40c (no DOC case ID): fixture.Quote is the TestOnly
// five-cell editable line with a money input (Item, Quantity, Rate, Amount,
// Description), its tables match its fields, and its Amount and Date hooks
// read its lines and header.
func TestFixture_Quote(t *testing.T) {
	d := newFixtureDB(t)
	dt := mustLookup(t, fixture.Quote)
	if !dt.TestOnly || !dt.IsDocument() || dt.Series != "QT/{FY}/{#####}" {
		t.Errorf("fixture.Quote TestOnly %v, document %v, Series %q; want a TestOnly document on QT/{FY}/{#####}",
			dt.TestOnly, dt.IsDocument(), dt.Series)
	}
	if got, want := fieldNames(dt.LineFields()), []string{"item_id", "qty", "rate", "amount", "description"}; !slices.Equal(got, want) {
		t.Errorf("fixture.Quote line fields = %q, want %q", got, want)
	}
	for table, fields := range map[string][]string{
		dt.Table:      slices.Concat(headerConv, fieldNames(dt.HeaderFields())),
		dt.LinesTable: slices.Concat(lineConv, fieldNames(dt.LineFields())),
	} {
		cols := tableColumns(t, d, table)
		for _, c := range fields {
			if _, ok := cols[c]; !ok {
				t.Errorf("%s lacks column %q", table, c)
			}
		}
	}

	const ts = "2026-10-24T05:00:00.000Z"
	err := d.Write(t.Context(), func(tx *db.Tx) error {
		ctx := context.Background()
		var id int64
		if err := tx.SQL().QueryRowContext(ctx,
			`INSERT INTO fixture_quote (state, version, created_at, created_by, updated_at, updated_by, quote_date)
			 VALUES ('draft', 1, ?, 7, ?, 7, '2026-10-24') RETURNING id`, ts, ts).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.SQL().ExecContext(ctx,
			`INSERT INTO fixture_quote_line (parent_id, line_no, amount) VALUES (?, 1, 125000), (?, 2, 48050)`, id, id); err != nil {
			return err
		}
		if amt, err := dt.Hooks.Amount(ctx, tx, id); err != nil || amt != types.Money(173050) {
			t.Errorf("Quote Amount = %d, %v; want 173050", amt, err)
		}
		if got, err := dt.Hooks.Date(ctx, tx, id); err != nil || got != (types.Date{Year: 2026, Month: 10, Day: 24}) {
			t.Errorf("Quote Date = %+v, %v; want 2026-10-24", got, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("d.Write: %v", err)
	}
}
