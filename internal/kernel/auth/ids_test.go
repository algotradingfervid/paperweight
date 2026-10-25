package auth_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// TestIDs_NeverReused checks CD-156: deleting the newest row of an AUTH
// table whose id is routed or referenced never hands its id to the next row.
func TestIDs_NeverReused(t *testing.T) {
	const now = `strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`
	cases := []struct {
		table  string
		insert string // one row, with unique values so it can run twice
	}{
		{"users", `INSERT INTO users (email, name, created_at, updated_at)
			VALUES ('ids-' || lower(hex(randomblob(4))) || '@example.com', 'Ids', ` + now + `, ` + now + `)`},
		{"roles", `INSERT INTO roles (name, name_key, created_at, updated_at)
			VALUES ('Ids', 'ids-' || hex(randomblob(4)), ` + now + `, ` + now + `)`},
		{"sessions", `INSERT INTO sessions (token_hash, data, created_at, last_seen_at)
			VALUES (hex(randomblob(16)), x'', ` + now + `, ` + now + `)`},
	}
	for _, c := range cases {
		t.Run(c.table, func(t *testing.T) {
			d := testkit.NewDB(t)
			ctx := context.Background()
			insert := func() int64 {
				var id int64
				if err := d.Write(ctx, func(tx *db.Tx) error {
					res, err := tx.SQL().ExecContext(ctx, c.insert)
					if err != nil {
						return err
					}
					id, err = res.LastInsertId()
					return err
				}); err != nil {
					t.Fatalf("insert %s: %v", c.table, err)
				}
				return id
			}
			deleted := insert()
			if err := d.Write(ctx, func(tx *db.Tx) error {
				_, err := tx.SQL().ExecContext(ctx, `DELETE FROM `+c.table+` WHERE id = ?`, deleted)
				return err
			}); err != nil {
				t.Fatalf("delete %s %d: %v", c.table, deleted, err)
			}
			if next := insert(); next == deleted {
				t.Errorf("%s: id %d reused after deleting the newest row (CD-156)", c.table, next)
			}
		})
	}
}
