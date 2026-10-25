package auth_test

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// t6Exec runs one statement in a write transaction.
func t6Exec(t *testing.T, w *authtest.World, query string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := w.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, query, args...)
		return err
	}); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// t6Null maps 0 to NULL.
func t6Null(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// t6InsertDoc inserts a probe document row into table (0 = NULL).
func t6InsertDoc(t *testing.T, w *authtest.World, table string, id int64, createdBy auth.UserID, employee, project, site int64) {
	t.Helper()
	t6Exec(t, w,
		`INSERT INTO `+table+` (id, state, created_by, created_at, employee_id, project_id, site_id) VALUES (?, ?, ?, ?, ?, ?, ?)`, //nolint:gosec // G202: table is a probe constant
		id, string(doctype.StateDraft), int64(createdBy), auth.FormatTime(authtest.T0), t6Null(employee), t6Null(project), t6Null(site))
}

// t6InsertLine inserts a probe line row into table (0 = NULL).
func t6InsertLine(t *testing.T, w *authtest.World, table string, id, parent, lineProject, lineSite int64) {
	t.Helper()
	t6Exec(t, w,
		`INSERT INTO `+table+` (id, parent_id, line_project_id, line_site_id) VALUES (?, ?, ?, ?)`, //nolint:gosec // G202: table is a probe constant
		id, parent, t6Null(lineProject), t6Null(lineSite))
}

// t6Visible runs pred against table and returns the visible ids in order;
// an invalid predicate fails the test.
func t6Visible(t *testing.T, w *authtest.World, table string, pred auth.Predicate) []int64 {
	t.Helper()
	ctx := context.Background()
	q := `SELECT id FROM ` + table //nolint:gosec // G202: table is a probe constant
	if pred.SQL != "" {
		q += ` WHERE ` + pred.SQL
	}
	q += ` ORDER BY id`
	var ids []int64
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, q, pred.Args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("run predicate %q %v: %v", pred.SQL, pred.Args, err)
	}
	return ids
}

// t6Ints returns pred's args as int64s; a non-integer arg fails the test.
func t6Ints(t *testing.T, args []any) []int64 {
	t.Helper()
	out := make([]int64, 0, len(args))
	for _, a := range args {
		v := reflect.ValueOf(a)
		switch v.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			out = append(out, v.Int())
		default:
			t.Errorf("predicate arg %v (%T) is not an integer", a, a)
		}
	}
	return out
}

// t6Scope calls Scope and fails the test on an error.
func t6Scope(t *testing.T, w *authtest.World, email, dt string, r auth.Right) auth.Predicate {
	t.Helper()
	pred, err := w.Auth.Scope(context.Background(), w.P(t, email), dt, r)
	if err != nil {
		t.Fatalf("Scope(%s, %s, %s) error = %v", email, dt, r, err)
	}
	return pred
}

// t6WantContains checks that pred.SQL contains each part.
func t6WantContains(t *testing.T, what string, pred auth.Predicate, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(pred.SQL, p) {
			t.Errorf("%s: SQL %q does not contain %q", what, pred.SQL, p)
		}
	}
}

// t6WantArgs checks that pred's args contain each of want.
func t6WantArgs(t *testing.T, what string, pred auth.Predicate, want ...int64) {
	t.Helper()
	got := t6Ints(t, pred.Args)
	for _, v := range want {
		if !slices.Contains(got, v) {
			t.Errorf("%s: args %v do not contain %d", what, pred.Args, v)
		}
	}
}

// AUTH-089
func TestOwnScope_AllWins(t *testing.T) {
	w := authtest.NewWorld(t)
	deepak, priya, ravi := w.Users["deepak@vardhan.test"], w.Users["priya@vardhan.test"], w.Users["ravi@vardhan.test"]
	t6InsertDoc(t, w, "probe_claim", 1, deepak, 0, 0, 0)                     // Deepak's own
	t6InsertDoc(t, w, "probe_claim", 2, priya, w.Employees["EMP-007"], 0, 0) // for Deepak's employee
	t6InsertDoc(t, w, "probe_claim", 3, priya, w.Employees["EMP-004"], 0, 0) // someone else's
	t6InsertDoc(t, w, "probe_claim", 4, ravi, w.Employees["EMP-003"], 0, 0)  // someone else's

	checkOwnFilter := func(what string) {
		t.Helper()
		pred := t6Scope(t, w, "deepak@vardhan.test", authtest.DTClaim, auth.RightRead)
		t6WantContains(t, what, pred, `"probe_claim"."created_by" = ?`, `"probe_claim"."employee_id" = ?`)
		t6WantArgs(t, what, pred, int64(deepak), w.Employees["EMP-007"])
		if got, want := t6Visible(t, w, "probe_claim", pred), []int64{1, 2}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: visible claims = %v, want %v", what, got, want)
		}
	}
	checkOwnFilter("Employee only")

	w.SetRoles(t, "deepak@vardhan.test", "Employee", "Director")
	pred := t6Scope(t, w, "deepak@vardhan.test", authtest.DTClaim, auth.RightRead)
	if pred.SQL != "" || len(pred.Args) != 0 {
		t.Errorf("Employee+Director: Scope = {SQL %q, Args %v}, want no filter", pred.SQL, pred.Args)
	}
	if got, want := t6Visible(t, w, "probe_claim", pred), []int64{1, 2, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("Employee+Director: visible claims = %v, want %v", got, want)
	}

	w.SetRoles(t, "deepak@vardhan.test", "Employee")
	checkOwnFilter("Director removed")
}

// AUTH-095
func TestRestriction_DimensionsAnd(t *testing.T) {
	w := authtest.NewWorld(t)
	w.Restrict(t, "suresh@vardhan.test", []string{"PRJ-METRO"}, []string{"PUNE-WH", "METRO-SITE"})
	priya := w.Users["priya@vardhan.test"]
	metro, techpark := w.Projects["PRJ-METRO"], w.Projects["PRJ-TECHPARK"]
	puneWH, metroSite, techSite := w.Sites["PUNE-WH"], w.Sites["METRO-SITE"], w.Sites["TECH-SITE"]

	t6InsertDoc(t, w, "probe_order", 1, priya, 0, metro, puneWH)    // visible
	t6InsertDoc(t, w, "probe_order", 2, priya, 0, metro, techSite)  // hidden: site
	t6InsertDoc(t, w, "probe_order", 3, priya, 0, techpark, puneWH) // hidden: project
	t6InsertDoc(t, w, "probe_order", 4, priya, 0, metro, metroSite) // visible
	t6InsertDoc(t, w, "probe_order", 5, priya, 0, metro, puneWH)    // hidden: line site
	t6InsertLine(t, w, "probe_order_line", 51, 5, 0, techSite)
	t6InsertDoc(t, w, "probe_order", 6, priya, 0, metro, puneWH) // visible: line site allowed
	t6InsertLine(t, w, "probe_order_line", 61, 6, 0, metroSite)
	t6InsertDoc(t, w, "probe_order", 7, priya, 0, metro, 0)  // hidden: strict, no site anywhere
	t6InsertDoc(t, w, "probe_order", 8, priya, 0, 0, puneWH) // hidden: strict, no project

	pred := t6Scope(t, w, "suresh@vardhan.test", authtest.DTOrder, auth.RightRead)
	t6WantContains(t, "Suresh PO scope", pred, `"probe_order"."project_id" IN (?)`, `"probe_order"."site_id" IN (?,?)`)
	if !strings.Contains(pred.SQL, " AND ") {
		t.Errorf("Suresh PO scope: SQL %q does not join clauses with AND", pred.SQL)
	}
	t6WantArgs(t, "Suresh PO scope", pred, metro, puneWH, metroSite)
	if got, want := t6Visible(t, w, "probe_order", pred), []int64{1, 4, 6}; !reflect.DeepEqual(got, want) {
		t.Errorf("Suresh visible POs = %v, want %v (PRJ-METRO at TECH-SITE and PRJ-TECHPARK at PUNE-WH hidden)", got, want)
	}

	// Ravi (Purchase Manager, read all, unrestricted) has no filter and sees every PO.
	rp := t6Scope(t, w, "ravi@vardhan.test", authtest.DTOrder, auth.RightRead)
	if rp.SQL != "" || len(rp.Args) != 0 {
		t.Errorf("Ravi PO scope = {SQL %q, Args %v}, want no filter", rp.SQL, rp.Args)
	}
}

func TestScope_AdminEmptyAndNoRightForbidden(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	admin := w.P(t, "admin@vardhan.test")

	var checked int
	for _, name := range []string{
		authtest.DTOrder, authtest.DTReceipt, authtest.DTClaim, authtest.DTItem,
		authtest.DTParty, authtest.DTSite, authtest.DTProject,
	} {
		dt, ok := doctype.Lookup(name)
		if !ok {
			t.Fatalf("DocType %s not registered", name)
		}
		for _, r := range auth.AllRights {
			if !auth.Applies(dt, r) {
				continue
			}
			pred, err := w.Auth.Scope(ctx, admin, name, r)
			if err != nil || !reflect.DeepEqual(pred, auth.Predicate{}) {
				t.Errorf("admin Scope(%s, %s) = (%+v, %v), want (Predicate{}, nil)", name, r, pred, err)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no admin Scope calls checked")
	}

	deepak := w.P(t, "deepak@vardhan.test")
	_, err := w.Auth.Scope(ctx, deepak, authtest.DTOrder, auth.RightRead)
	t5WantDenied(t, "deepak Scope(PO, read)", err, auth.RightRead, false, false,
		"You don't have permission to read Purchase Order. Ask your administrator for a role that allows it.")

	// The grant for r is missing although read is held.
	ravi := w.P(t, "ravi@vardhan.test")
	_, err = w.Auth.Scope(ctx, ravi, authtest.DTOrder, auth.RightCreate)
	t5WantDenied(t, "ravi Scope(PO, create)", err, auth.RightCreate, false, false,
		"You don't have permission to create Purchase Order. Ask your administrator for a role that allows it.")
}

func TestScope_TeamClause(t *testing.T) {
	w := authtest.NewWorld(t)
	metro := w.Projects["PRJ-METRO"] // Priya is restricted to PRJ-METRO
	t6InsertDoc(t, w, "probe_claim", 1, w.Users["priya@vardhan.test"], w.Employees["EMP-004"], metro, 0)
	t6InsertDoc(t, w, "probe_claim", 2, w.Users["suresh@vardhan.test"], w.Employees["EMP-005"], metro, 0)
	t6InsertDoc(t, w, "probe_claim", 3, w.Users["deepak@vardhan.test"], w.Employees["EMP-007"], metro, 0)
	t6InsertDoc(t, w, "probe_claim", 4, w.Users["ravi@vardhan.test"], 0, metro, 0)
	t6InsertDoc(t, w, "probe_claim", 5, w.Users["kavya@vardhan.test"], w.Employees["EMP-006"], metro, 0)

	pp := t6Scope(t, w, "priya@vardhan.test", authtest.DTClaim, auth.RightRead)
	t6WantContains(t, "Priya", pp, `"probe_claim"."employee_id" IN (?)`)
	t6WantArgs(t, "Priya", pp, w.Employees["EMP-007"])
	if got, want := t6Visible(t, w, "probe_claim", pp), []int64{1, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("Priya visible claims = %v, want %v", got, want)
	}

	rp := t6Scope(t, w, "ravi@vardhan.test", authtest.DTClaim, auth.RightRead)
	t6WantContains(t, "Ravi", rp, `"probe_claim"."employee_id" IN (?,?)`)
	t6WantArgs(t, "Ravi", rp, w.Employees["EMP-004"], w.Employees["EMP-005"])
	if got, want := t6Visible(t, w, "probe_claim", rp), []int64{1, 2, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("Ravi visible claims = %v, want %v", got, want)
	}

	dp := t6Scope(t, w, "deepak@vardhan.test", authtest.DTClaim, auth.RightRead)
	if strings.Contains(dp.SQL, `"probe_claim"."employee_id" IN (`) {
		t.Errorf("Deepak (no reports): SQL %q has a team IN clause", dp.SQL)
	}
	t6WantContains(t, "Deepak", dp, `"probe_claim"."created_by" = ?`)
	if got, want := t6Visible(t, w, "probe_claim", dp), []int64{3}; !reflect.DeepEqual(got, want) {
		t.Errorf("Deepak visible claims = %v, want %v", got, want)
	}

	// Suresh leaves: read on the next check, no perm_version bump needed.
	w.Dir.SetActive(w.Employees["EMP-005"], false)
	rp = t6Scope(t, w, "ravi@vardhan.test", authtest.DTClaim, auth.RightRead)
	t6WantContains(t, "Ravi after EMP-005 left", rp, `"probe_claim"."employee_id" IN (?)`)
	t6WantArgs(t, "Ravi after EMP-005 left", rp, w.Employees["EMP-004"])
	if slices.Contains(t6Ints(t, rp.Args), w.Employees["EMP-005"]) {
		t.Errorf("Ravi after EMP-005 left: args %v still contain EMP-005 (%d)", rp.Args, w.Employees["EMP-005"])
	}
	if got, want := t6Visible(t, w, "probe_claim", rp), []int64{1, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("Ravi visible claims after EMP-005 left = %v, want %v", got, want)
	}
}
