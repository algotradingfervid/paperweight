package fixture

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// seedAt is the fixed test clock (00-fixtures), used for seeded rows.
const seedAt = "2026-10-24T05:00:00.000Z"

// masterLookup reads a stand-in master table (CD-21).
type masterLookup struct{ table string }

func (m masterLookup) Lookup(ctx context.Context, tx *db.Tx, id int64) (doctype.MasterInfo, bool, error) {
	var info doctype.MasterInfo
	err := tx.SQL().QueryRowContext(ctx,
		`SELECT code, name, disabled, blocked, warning FROM `+m.table+` WHERE id = ?`, id).
		Scan(&info.Code, &info.Name, &info.Disabled, &info.Blocked, &info.Warning)
	if errors.Is(err, sql.ErrNoRows) {
		return doctype.MasterInfo{}, false, nil
	}
	if err != nil {
		return doctype.MasterInfo{}, false, err
	}
	return info, true, nil
}

func masterDocTypes() []doctype.DocType {
	master := func(name, label, plural, table string) doctype.DocType {
		return doctype.DocType{
			Name: name, Label: label, PluralLabel: plural, Table: table, Importable: true,
			Fields: []doctype.Field{
				{Name: "code", Label: "Code", Type: doctype.Text, Required: true, ListPriority: 1},
				{Name: "name", Label: "Name", Type: doctype.Text, Required: true, ListPriority: 4},
			},
			DefaultRights: map[string]map[string]string{
				"Masters Manager": rights("all", "read", "create", "write", "delete", "export", "import"),
			},
			Master: masterLookup{table: table},
		}
	}
	return []doctype.DocType{
		master(Party, "Party", "Parties", "fixture_party"),
		master(Item, "Item", "Items", "fixture_item"),
		master(Project, "Project", "Projects", "fixture_project"),
	}
}

// Masters maps each seeded master code to its id.
type Masters struct {
	Party, Item, Project map[string]int64
}

// SeedMasters inserts the 00-fixtures parties, items and projects.
func SeedMasters(t testing.TB, d *db.DB) Masters {
	t.Helper()
	sets := []struct {
		table string
		rows  [][2]string
	}{
		{"fixture_party", [][2]string{
			{"SHREE", "Shree Steel Traders"}, {"BHARAT", "Bharat Tools"}, {"RAJKOT", "Rajkot Castings"},
			{"COROM", "Coromandel Cables"}, {"LOCAL", "Ganesh Hardware"},
		}},
		{"fixture_item", [][2]string{
			{"MS-ANGLE-50", "MS Angle 50x50x5"}, {"BOLT-M12", "Hex Bolt M12x60"}, {"WIRE-18G", "Binding Wire 18G"},
			{"CABLE-4C", "Armoured Cable 4 core 16 sq mm"}, {"CEMENT-OPC", "OPC Cement 53 Grade"}, {"SVC-CRANE", "Crane hire"},
		}},
		{"fixture_project", [][2]string{
			{"PRJ-METRO", "Pune Metro Depot"}, {"PRJ-TECHPARK", "Whitefield Tech Park, Bengaluru"},
		}},
	}
	ids := make([]map[string]int64, len(sets))
	err := d.Write(context.Background(), func(tx *db.Tx) error {
		for i, s := range sets {
			ids[i] = map[string]int64{}
			for _, r := range s.rows {
				var id int64
				if err := tx.SQL().QueryRowContext(context.Background(),
					`INSERT INTO `+s.table+` (code, name, created_at, created_by, updated_at, updated_by)
					 VALUES (?, ?, ?, 1, ?, 1) RETURNING id`, r[0], r[1], seedAt, seedAt).Scan(&id); err != nil {
					return err
				}
				ids[i][r[0]] = id
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("fixture.SeedMasters: %v", err)
	}
	return Masters{Party: ids[0], Item: ids[1], Project: ids[2]}
}

// SetDisabled sets the disabled flag of the master code in table.
func SetDisabled(t testing.TB, d *db.DB, table, code string, disabled bool) {
	t.Helper()
	setMaster(t, d, table, code, "disabled", disabled)
}

// SetBlocked sets the blocked reason of the master code in table ("" clears it).
func SetBlocked(t testing.TB, d *db.DB, table, code, reason string) {
	t.Helper()
	setMaster(t, d, table, code, "blocked", reason)
}

// SetWarning sets the warning text of the master code in table ("" clears it).
func SetWarning(t testing.TB, d *db.DB, table, code, text string) {
	t.Helper()
	setMaster(t, d, table, code, "warning", text)
}

func setMaster(t testing.TB, d *db.DB, table, code, column string, v any) {
	t.Helper()
	if !masterTables[table] {
		t.Fatalf("fixture: %q is not a stand-in master table", table)
	}
	err := d.Write(context.Background(), func(tx *db.Tx) error {
		res, err := tx.SQL().ExecContext(context.Background(),
			`UPDATE `+table+` SET `+column+` = ? WHERE code = ?`, v, code)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n != 1 {
			return fmt.Errorf("no row with code %q", code)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("fixture: set %s.%s for %s: %v", table, column, code, err)
	}
}

var masterTables = map[string]bool{"fixture_party": true, "fixture_item": true, "fixture_project": true}
