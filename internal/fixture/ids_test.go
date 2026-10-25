package fixture_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// Supporting test for CD-156 (no DOC case ID): deleting the newest row never
// hands its id to the next insert.

const idsTS = "2026-10-24T05:00:00.000Z"

const (
	idsHeader = `(state, version, created_at, created_by, updated_at, updated_by, remarks) VALUES ('draft', 1, '` + idsTS + `', 7, '` + idsTS + `', 7, ?)`
	idsMaster = `(code, name, created_at, created_by, updated_at, updated_by) VALUES ('C' || ?, 'N', '` + idsTS + `', 7, '` + idsTS + `', 7)`
)

func TestFixture_IdsNeverReused(t *testing.T) {
	cases := []struct {
		table  string
		setup  string // inserts parent id 1 (argument "parent"), or "" for none
		insert string // takes one argument: a value unique per insert
	}{
		{"fixture_party", "", `INSERT INTO fixture_party ` + idsMaster},
		{"fixture_item", "", `INSERT INTO fixture_item ` + idsMaster},
		{"fixture_project", "", `INSERT INTO fixture_project ` + idsMaster},
		{"fixture_order", "", `INSERT INTO fixture_order ` + idsHeader},
		{"fixture_order_line", `INSERT INTO fixture_order ` + idsHeader,
			`INSERT INTO fixture_order_line (parent_id, line_no) VALUES (1, ?)`},
		{"fixture_receipt", "", `INSERT INTO fixture_receipt ` + idsHeader},
		{"fixture_receipt_line", `INSERT INTO fixture_receipt ` + idsHeader,
			`INSERT INTO fixture_receipt_line (parent_id, line_no) VALUES (1, ?)`},
		{"fixture_effect_log", "", `INSERT INTO fixture_effect_log (receipt_id, action) VALUES (?, 'apply')`},
		{"fixture_claim", "", `INSERT INTO fixture_claim ` + idsHeader},
		{"fixture_claim_line", `INSERT INTO fixture_claim ` + idsHeader,
			`INSERT INTO fixture_claim_line (parent_id, line_no) VALUES (1, ?)`},
	}
	for _, c := range cases {
		t.Run(c.table, func(t *testing.T) {
			d := newFixtureDB(t)
			n := 0
			insert := func() int64 {
				t.Helper()
				n++
				var id int64
				err := d.Write(t.Context(), func(tx *db.Tx) error {
					res, err := tx.SQL().ExecContext(t.Context(), c.insert, n)
					if err != nil {
						return err
					}
					id, err = res.LastInsertId()
					return err
				})
				if err != nil {
					t.Fatalf("insert into %s: %v", c.table, err)
				}
				return id
			}
			if c.setup != "" {
				if err := d.Write(t.Context(), func(tx *db.Tx) error {
					_, err := tx.SQL().ExecContext(t.Context(), c.setup, "parent")
					return err
				}); err != nil {
					t.Fatalf("setup for %s: %v", c.table, err)
				}
			}

			insert()
			newest := insert()
			if err := d.Write(t.Context(), func(tx *db.Tx) error {
				_, err := tx.SQL().ExecContext(t.Context(), `DELETE FROM `+c.table+` WHERE id = ?`, newest)
				return err
			}); err != nil {
				t.Fatalf("delete %s id %d: %v", c.table, newest, err)
			}
			if got := insert(); got <= newest {
				t.Errorf("%s: id after deleting newest id %d = %d, want > %d", c.table, newest, got, newest)
			}
		})
	}
}
