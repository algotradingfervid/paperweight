package authtest_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// TestWorld_Seed checks the canonical World from 00-fixtures.md: users and
// their IDs, roles, restrictions, the disabled user, the seeded rights matrix,
// reporting lines and the probe Info source.
func TestWorld_Seed(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)

	// 8 users, IDs 1–8 in fixture table order.
	wantIDs := map[string]auth.UserID{
		"admin@vardhan.test":  1,
		"meena@vardhan.test":  2,
		"ravi@vardhan.test":   3,
		"priya@vardhan.test":  4,
		"suresh@vardhan.test": 5,
		"kavya@vardhan.test":  6,
		"deepak@vardhan.test": 7,
		"old@vardhan.test":    8,
	}
	if !reflect.DeepEqual(w.Users, wantIDs) {
		t.Errorf("w.Users = %v, want %v", w.Users, wantIDs)
	}
	var nUsers int
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&nUsers)
	}); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if nUsers != 8 {
		t.Errorf("users table has %d rows, want 8", nUsers)
	}

	// Priya's roles, sorted.
	if got, want := w.P(t, "priya@vardhan.test").Roles, []string{"Employee", "Purchase User"}; !reflect.DeepEqual(got, want) {
		t.Errorf("priya roles = %q, want %q", got, want)
	}

	// Suresh is restricted to sites PUNE-WH and METRO-SITE.
	var sureshSites []int64
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx,
			`SELECT value_id FROM user_restrictions WHERE user_id = ? AND dimension = 'site' ORDER BY value_id`,
			int64(w.Users["suresh@vardhan.test"]))
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			sureshSites = append(sureshSites, id)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("read suresh restrictions: %v", err)
	}
	wantSites := []int64{w.Sites["PUNE-WH"], w.Sites["METRO-SITE"]}
	slices.Sort(wantSites)
	if wantSites[0] == 0 || wantSites[1] == 0 {
		t.Errorf("w.Sites missing PUNE-WH or METRO-SITE: %v", w.Sites)
	}
	if !reflect.DeepEqual(sureshSites, wantSites) {
		t.Errorf("suresh site restrictions = %v, want %v (PUNE-WH, METRO-SITE)", sureshSites, wantSites)
	}

	// old@ is disabled.
	var oldEnabled int64 = -1
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT enabled FROM users WHERE id = ?`,
			int64(w.Users["old@vardhan.test"])).Scan(&oldEnabled)
	}); err != nil {
		t.Fatalf("read old@ enabled: %v", err)
	}
	if oldEnabled != 0 {
		t.Errorf("old@vardhan.test enabled = %d, want 0 (disabled)", oldEnabled)
	}

	// Seeded rights matrix.
	pu, err := w.Auth.RoleRights(ctx, "Purchase User", authtest.DTOrder)
	if err != nil {
		t.Fatalf("RoleRights(Purchase User, DTOrder): %v", err)
	}
	if got := pu[auth.RightDelete]; got != auth.ScopeOwn {
		t.Errorf("Purchase User delete on %s = %q, want %q", authtest.DTOrder, got, auth.ScopeOwn)
	}
	emp, err := w.Auth.RoleRights(ctx, "Employee", authtest.DTClaim)
	if err != nil {
		t.Fatalf("RoleRights(Employee, DTClaim): %v", err)
	}
	wantEmp := map[auth.Right]auth.Scope{
		auth.RightRead:    auth.ScopeTeam,
		auth.RightApprove: auth.ScopeTeam,
		auth.RightCreate:  auth.ScopeOwn,
		auth.RightWrite:   auth.ScopeOwn,
		auth.RightSubmit:  auth.ScopeOwn,
		auth.RightPrint:   auth.ScopeOwn,
	}
	if !reflect.DeepEqual(emp, wantEmp) {
		t.Errorf("Employee rights on %s = %v, want %v", authtest.DTClaim, emp, wantEmp)
	}

	// Reporting lines.
	e := w.Employees
	for _, code := range []string{"EMP-002", "EMP-003", "EMP-004", "EMP-005", "EMP-007"} {
		if e[code] == 0 {
			t.Fatalf("w.Employees[%q] missing: %v", code, e)
		}
	}
	if got, err := w.Dir.DirectReports(ctx, nil, e["EMP-004"]); err != nil || !reflect.DeepEqual(got, []int64{e["EMP-007"]}) {
		t.Errorf("DirectReports(EMP-004) = %v, %v; want [EMP-007=%d]", got, err, e["EMP-007"])
	}
	if got, err := w.Dir.DirectReports(ctx, nil, e["EMP-003"]); err != nil || !reflect.DeepEqual(got, []int64{e["EMP-004"], e["EMP-005"]}) {
		t.Errorf("DirectReports(EMP-003) = %v, %v; want [EMP-004=%d EMP-005=%d]", got, err, e["EMP-004"], e["EMP-005"])
	}
	if got, err := w.Dir.ManagerChain(ctx, nil, e["EMP-007"]); err != nil || !reflect.DeepEqual(got, []int64{e["EMP-004"], e["EMP-003"], e["EMP-002"]}) {
		t.Errorf("ManagerChain(EMP-007) = %v, %v; want [EMP-004=%d EMP-003=%d EMP-002=%d]", got, err, e["EMP-004"], e["EMP-003"], e["EMP-002"])
	}

	// A new probe order with no number displays as "Draft <id>".
	var id int64
	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		res, err := tx.SQL().ExecContext(ctx,
			`INSERT INTO probe_order (state, created_by, created_at) VALUES (?, ?, ?)`,
			string(doctype.StateDraft), int64(w.Users["priya@vardhan.test"]), auth.FormatTime(authtest.T0))
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	}); err != nil {
		t.Fatalf("insert probe_order row: %v", err)
	}
	var info doctype.Info
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		info, err = w.Info.Info(ctx, tx, authtest.DTOrder, id)
		return err
	}); err != nil {
		t.Fatalf("Info(DTOrder, %d): %v", id, err)
	}
	if want := fmt.Sprintf("Draft %d", id); info.Display != want {
		t.Errorf("Info(DTOrder, %d).Display = %q, want %q", id, info.Display, want)
	}
}
