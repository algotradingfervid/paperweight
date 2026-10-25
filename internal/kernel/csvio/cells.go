package csvio

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// checkCell checks a non-empty cell of a field that isn't a Link and returns
// the user message, or "" when the cell is fine.
func checkCell(f *doctype.Field, cell string) string {
	var err error
	switch f.Type {
	case doctype.Money:
		_, err = types.ParseMoney(cell)
	case doctype.Qty:
		_, err = types.ParseQty(cell)
	case doctype.Rate:
		_, err = types.ParseRate(cell)
	case doctype.Percent:
		_, err = types.ParsePercent(cell)
	case doctype.Factor:
		_, err = types.ParseFactor(cell)
	case doctype.Date:
		_, err = types.ParseDate(cell)
	case doctype.Check:
		switch strings.ToLower(cell) {
		case "yes", "no", "true", "false", "1", "0":
		default:
			return `"` + cell + `" is not Yes or No.`
		}
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// resolveLink finds the active record whose code is cell in f's target
// DocType. It returns the record id, or a user message when the code is
// missing or disabled.
func resolveLink(ctx context.Context, tx *db.Tx, f *doctype.Field, cell string) (int64, string, error) {
	target, ok := doctype.Lookup(f.Link)
	if !ok {
		return 0, "", fmt.Errorf("csvio: field %s links to unknown DocType %q", f.Name, f.Link)
	}
	var id int64
	err := tx.SQL().QueryRowContext(ctx,
		"SELECT "+target.Table+".id FROM "+target.Table+" WHERE "+target.Table+".code = ?", cell).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		s, err := suggestCode(ctx, tx, target.Table, cell)
		if err != nil {
			return 0, "", err
		}
		msg := f.Label + " '" + cell + "' not found."
		if s != "" {
			msg += " Use an existing " + f.Label + " code such as " + s + "."
		}
		return 0, msg, nil
	}
	if err != nil {
		return 0, "", err
	}
	if target.Master != nil {
		mi, _, err := target.Master.Lookup(ctx, tx, id)
		if err != nil {
			return 0, "", err
		}
		if mi.Disabled {
			return 0, f.Label + " '" + cell + "' is disabled. Enable it or use another " + f.Label + ".", nil
		}
	}
	return id, "", nil
}

// suggestCode returns the active code sharing the longest case-insensitive
// prefix with cell (ties: alphabetical), or "" when there is none.
// It reads the table's raw disabled column, not the MasterLookup: every
// master table has one (CD-21).
func suggestCode(ctx context.Context, tx *db.Tx, table, cell string) (string, error) {
	rows, err := tx.SQL().QueryContext(ctx, "SELECT "+table+".code FROM "+table+" WHERE "+table+".disabled = 0")
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	best, bestLen := "", -1
	want := []rune(strings.ToLower(cell))
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return "", err
		}
		n := 0
		for _, r := range strings.ToLower(code) {
			if n >= len(want) || want[n] != r {
				break
			}
			n++
		}
		if n > bestLen || (n == bestLen && code < best) {
			best, bestLen = code, n
		}
	}
	return best, rows.Err()
}
