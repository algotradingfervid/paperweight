package auth_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// TestMigrations_AuthTablesAndRoles checks the AUTH schema (migrations
// 00100–00103): the seeded roles, the tables, and two column constraints.
func TestMigrations_AuthTablesAndRoles(t *testing.T) {
	d := testkit.NewDB(t)
	ctx := context.Background()

	// Seeded roles, in id order; only Administrator is is_admin.
	type role struct {
		name    string
		isAdmin int
	}
	var roles []role
	err := d.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, `SELECT name, is_admin FROM roles ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r role
			if err := rows.Scan(&r.name, &r.isAdmin); err != nil {
				return err
			}
			roles = append(roles, r)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read roles: %v", err)
	}
	wantRoles := []role{
		{"Administrator", 1},
		{"Director", 0},
		{"Purchase Manager", 0},
		{"Purchase User", 0},
		{"Stores", 0},
		{"Masters Manager", 0},
		{"Employee", 0},
	}
	if !slices.Equal(roles, wantRoles) {
		t.Errorf("roles (name, is_admin) ORDER BY id = %v, want %v", roles, wantRoles)
	}

	// Every AUTH table exists.
	var tables []string
	err = d.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table'`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				return err
			}
			tables = append(tables, n)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read tables: %v", err)
	}
	for _, want := range []string{
		"users", "roles", "user_roles", "role_doctypes", "role_rights",
		"user_restrictions", "auth_meta", "sessions", "login_attempts", "auth_log",
	} {
		if !slices.Contains(tables, want) {
			t.Errorf("table %q missing; tables = %v", want, tables)
		}
	}

	exec := func(q string, args ...any) error {
		return d.Write(ctx, func(tx *db.Tx) error {
			_, err := tx.SQL().ExecContext(ctx, q, args...)
			return err
		})
	}

	// login_attempts.expires_at is NOT NULL.
	err = exec(`INSERT INTO login_attempts(key, count, first_failure_at, expires_at) VALUES ('k', 1, 'x', NULL)`)
	if err == nil || !strings.Contains(err.Error(), "NOT NULL") {
		t.Errorf("insert login_attempts with NULL expires_at: err = %v, want an error containing \"NOT NULL\"", err)
	}

	// role_rights.scope accepts 'team' (CD-11) and rejects anything outside own|team|all.
	var employeeID int64
	err = d.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT id FROM roles WHERE name = 'Employee'`).Scan(&employeeID)
	})
	if err != nil {
		t.Fatalf("read Employee role id: %v", err)
	}
	if err := exec(`INSERT INTO role_doctypes(role_id, doctype) VALUES (?, 'authtest.Claim')`, employeeID); err != nil {
		t.Fatalf("insert role_doctypes: %v", err)
	}
	if err := exec(`INSERT INTO role_rights(role_id, doctype, right_name, scope) VALUES (?, 'authtest.Claim', 'read', 'team')`, employeeID); err != nil {
		t.Errorf("insert role_rights with scope 'team': %v, want success", err)
	}
	err = exec(`INSERT INTO role_rights(role_id, doctype, right_name, scope) VALUES (?, 'authtest.Claim', 'approve', 'some')`, employeeID)
	if err == nil || !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("insert role_rights with scope 'some': err = %v, want an error containing \"CHECK\"", err)
	}
}
