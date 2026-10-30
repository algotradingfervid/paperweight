package testkit_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite" // driver "sqlite" for the raw second connection

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// Expected values below are copied from docs/tests/core/00-fixtures.md.

// fakeTB records Fatal/Error calls instead of failing the real test. Fatal*
// and FailNow stop the calling goroutine with runtime.Goexit, as testing.T does,
// so the code under test must run in its own goroutine (see runWithFake).
type fakeTB struct {
	testing.TB // the real *testing.T for everything not overridden

	mu      sync.Mutex
	fatal   bool
	failed  bool
	message string
}

func (f *fakeTB) record(fatal bool, msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = true
	if fatal {
		f.fatal = true
	}
	if f.message == "" {
		f.message = msg
	}
}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.record(true, fmt.Sprintf(format, args...))
	runtime.Goexit()
}

func (f *fakeTB) Fatal(args ...any) {
	f.record(true, fmt.Sprint(args...))
	runtime.Goexit()
}

func (f *fakeTB) FailNow() {
	f.record(true, "FailNow")
	runtime.Goexit()
}

func (f *fakeTB) Errorf(format string, args ...any) { f.record(false, fmt.Sprintf(format, args...)) }
func (f *fakeTB) Error(args ...any)                 { f.record(false, fmt.Sprint(args...)) }
func (f *fakeTB) Fail()                             { f.record(false, "Fail") }

func (f *fakeTB) Failed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failed
}

// runWithFake runs fn(fake) in its own goroutine and reports whether fn
// returned normally (false when Fatal*/FailNow stopped it).
func runWithFake(t *testing.T, fn func(tb testing.TB)) (fake *fakeTB, returned bool) {
	t.Helper()
	fake = &fakeTB{TB: t}
	done := make(chan bool)
	go func() {
		ok := false
		defer func() { done <- ok }()
		fn(fake)
		ok = true
	}()
	return fake, <-done
}

func tableExists(t *testing.T, conn *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := conn.QueryRowContext(t.Context(),
		`SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		t.Fatalf("query sqlite_schema for %q: %v", name, err)
	}
	return n == 1
}

type column struct{ Name, Type string }

func tableColumns(t *testing.T, conn *sql.DB, table string) []column {
	t.Helper()
	rows, err := conn.QueryContext(t.Context(), `SELECT name, type FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	var cols []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.Name, &c.Type); err != nil {
			t.Fatalf("scan table_info(%s): %v", table, err)
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info(%s) rows: %v", table, err)
	}
	return cols
}

// columnTypeViolations is the CD-34 / CD-71 rule: every table in sqlite_schema
// declares only INTEGER, TEXT or BLOB, except tables named sqlite_* or goose_*.
// It returns the violations as "table.column TYPE" and the skipped table names.
func columnTypeViolations(t *testing.T, conn *sql.DB) (violations, skipped []string) {
	t.Helper()
	rows, err := conn.QueryContext(t.Context(), `SELECT name FROM sqlite_schema WHERE type = 'table' ORDER BY name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list tables rows: %v", err)
	}
	_ = rows.Close()

	allowed := map[string]bool{"INTEGER": true, "TEXT": true, "BLOB": true}
	for _, name := range tables {
		if strings.HasPrefix(name, "sqlite_") || strings.HasPrefix(name, "goose_") {
			skipped = append(skipped, name)
			continue
		}
		for _, c := range tableColumns(t, conn, name) {
			if !allowed[strings.ToUpper(c.Type)] {
				violations = append(violations, name+"."+c.Name+" "+c.Type)
			}
		}
	}
	return violations, skipped
}

func TestTestkit_NewDBMigrated(t *testing.T) {
	d := testkit.NewDB(t)
	if d == nil {
		t.Fatal("NewDB returned nil")
	}
	conn := d.SQL()
	for _, name := range []string{"goose_db_version", "app_meta", "company", "branch", "party", "address", "item", "project", "site", "employee", "expense_type"} {
		if !tableExists(t, conn, name) {
			t.Errorf("table %q does not exist after NewDB", name)
		}
	}
	if got := filepath.Base(d.Path()); got != "app.db" {
		t.Errorf("NewDB file name = %q, want %q", got, "app.db")
	}
	var bt int
	if err := conn.QueryRowContext(t.Context(), `PRAGMA busy_timeout`).Scan(&bt); err != nil {
		t.Fatalf("PRAGMA busy_timeout: %v", err)
	}
	if bt != 5000 {
		t.Errorf("busy_timeout = %d, want 5000 (db.Open default)", bt)
	}
}

func TestTestkit_NewDBCleansUp(t *testing.T) {
	var (
		d   *db.DB
		dir string
	)
	t.Run("inner", func(st *testing.T) {
		d = testkit.NewDB(st)
		if d == nil {
			st.Fatal("NewDB returned nil")
		}
		dir = filepath.Dir(d.Path())
		if _, err := os.Stat(dir); err != nil {
			st.Fatalf("temp dir %q should exist during the subtest: %v", dir, err)
		}
	})
	if d == nil || dir == "" {
		t.Fatal("subtest did not create a database")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("after the subtest, os.Stat(%q) error = %v, want not-exist", dir, err)
	}
	if err := d.SQL().PingContext(t.Context()); err == nil {
		t.Error("after the subtest, the database still accepts Ping; NewDB must close it in t.Cleanup")
	}
}

func TestTestkit_DBPathMatchesNewDB(t *testing.T) {
	d := testkit.NewDB(t)
	p := testkit.DBPath(t)
	if p != d.Path() {
		t.Errorf("DBPath(t) = %q, want NewDB's path %q", p, d.Path())
	}
	raw, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatalf("sql.Open(sqlite, %q): %v", p, err)
	}
	defer func() { _ = raw.Close() }()
	var v string
	if err := raw.QueryRowContext(t.Context(), `SELECT value FROM app_meta WHERE key = 'schema_conventions'`).Scan(&v); err != nil {
		t.Fatalf("raw connection cannot read app_meta: %v", err)
	}
	if v != "1" {
		t.Errorf("app_meta schema_conventions = %q, want %q", v, "1")
	}

	t.Run("without NewDB calls Fatalf", func(t *testing.T) {
		fake, returned := runWithFake(t, func(tb testing.TB) { _ = testkit.DBPath(tb) })
		if returned {
			t.Error("DBPath on a t without NewDB returned normally, want t.Fatalf")
		}
		if !fake.fatal {
			t.Error("DBPath on a t without NewDB did not call Fatalf")
		}
		if fake.message == "" {
			t.Error("DBPath's Fatalf message is empty; it should say NewDB(t) was not called first")
		}
	})
}

func TestTestkit_SeedMatchesFixturesDoc(t *testing.T) {
	d := testkit.NewDB(t)
	fx := testkit.Seed(t, d)

	type user struct {
		Email, Name string
		Roles       []string
		Emp         string
		Disabled    bool
	}
	wantUsers := []user{
		{"admin@vardhan.test", "Anil Kapoor", []string{"Administrator"}, "EMP-001", false},
		{"meena@vardhan.test", "Meena Iyer", []string{"Director"}, "EMP-002", false},
		{"ravi@vardhan.test", "Ravi Kulkarni", []string{"Purchase Manager", "Employee"}, "EMP-003", false},
		{"priya@vardhan.test", "Priya Shah", []string{"Purchase User", "Employee"}, "EMP-004", false},
		{"suresh@vardhan.test", "Suresh Patil", []string{"Stores", "Employee"}, "EMP-005", false},
		{"kavya@vardhan.test", "Kavya Rao", []string{"Masters Manager", "Employee"}, "EMP-006", false},
		{"deepak@vardhan.test", "Deepak Nair", []string{"Employee"}, "EMP-007", false},
		{"old@vardhan.test", "Former Staff", []string{"Employee"}, "EMP-008", true},
	}
	if len(fx.Users) != 8 {
		t.Fatalf("len(Fixtures.Users) = %d, want 8", len(fx.Users))
	}
	for i, w := range wantUsers {
		u := fx.Users[i]
		if int64(u.ID) != int64(i+1) {
			t.Errorf("Users[%d].ID = %d, want %d (IDs 1-8 in table order)", i, u.ID, i+1)
		}
		if u.Email != w.Email || u.Name != w.Name || u.EmployeeCode != w.Emp || u.Disabled != w.Disabled {
			t.Errorf("Users[%d] = {%q %q emp %q disabled %v}, want {%q %q emp %q disabled %v}",
				i, u.Email, u.Name, u.EmployeeCode, u.Disabled, w.Email, w.Name, w.Emp, w.Disabled)
		}
		if !reflect.DeepEqual(u.Roles, w.Roles) {
			t.Errorf("Users[%d] (%s) roles = %q, want %q", i, w.Email, u.Roles, w.Roles)
		}
	}
	// Priya's roles, as the plan names them.
	if got, want := fx.Users[3].Roles, []string{"Purchase User", "Employee"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Priya's roles = %q, want %q", got, want)
	}

	// Employees in Fixtures carry the database ids and the managers from the doc.
	wantManager := map[string]string{
		"EMP-001": "", "EMP-002": "", "EMP-003": "EMP-002", "EMP-004": "EMP-003",
		"EMP-005": "EMP-003", "EMP-006": "EMP-002", "EMP-007": "EMP-004", "EMP-008": "",
	}
	if len(fx.Employees) != 8 {
		t.Fatalf("len(Fixtures.Employees) = %d, want 8", len(fx.Employees))
	}
	byCode := map[string]int{}
	for i, e := range fx.Employees {
		byCode[e.Code] = i
	}
	for i, w := range wantUsers {
		idx, ok := byCode[w.Emp]
		if !ok {
			t.Errorf("Fixtures.Employees has no %s", w.Emp)
			continue
		}
		e := fx.Employees[idx]
		if e.ManagerCode != wantManager[w.Emp] {
			t.Errorf("%s ManagerCode = %q, want %q", w.Emp, e.ManagerCode, wantManager[w.Emp])
		}
		if e.UserID != fx.Users[i].ID {
			t.Errorf("%s UserID = %d, want %d (%s)", w.Emp, e.UserID, fx.Users[i].ID, w.Email)
		}
		if e.ID == 0 {
			t.Errorf("%s ID = 0, want the database id filled in by Seed", w.Emp)
		}
	}
	if idx, ok := byCode["EMP-008"]; ok && fx.Employees[idx].Status != "Left" {
		t.Errorf("EMP-008 Status = %q, want %q", fx.Employees[idx].Status, "Left")
	}

	// The database rows agree: Ravi's EMP-003 reports to EMP-002, Deepak's EMP-007 to EMP-004.
	conn := d.SQL()
	managerOf := func(code string) string {
		t.Helper()
		var m sql.NullString
		err := conn.QueryRowContext(t.Context(), `SELECT m.code FROM employee e
			LEFT JOIN employee m ON m.id = e.manager_id WHERE e.code = ?`, code).Scan(&m)
		if err != nil {
			t.Fatalf("query manager of %s: %v", code, err)
		}
		return m.String
	}
	if got := managerOf("EMP-003"); got != "EMP-002" {
		t.Errorf("employee EMP-003 manager = %q, want EMP-002", got)
	}
	if got := managerOf("EMP-007"); got != "EMP-004" {
		t.Errorf("employee EMP-007 manager = %q, want EMP-004", got)
	}
	for _, code := range []string{"EMP-003", "EMP-007"} {
		var dbID int64
		var dbUser int64
		if err := conn.QueryRowContext(t.Context(), `SELECT id, user_id FROM employee WHERE code = ?`, code).Scan(&dbID, &dbUser); err != nil {
			t.Fatalf("query %s: %v", code, err)
		}
		if idx, ok := byCode[code]; ok {
			if fx.Employees[idx].ID != dbID {
				t.Errorf("Fixtures %s ID = %d, database id = %d", code, fx.Employees[idx].ID, dbID)
			}
			if int64(fx.Employees[idx].UserID) != dbUser {
				t.Errorf("Fixtures %s UserID = %d, database user_id = %d", code, fx.Employees[idx].UserID, dbUser)
			}
		}
	}
	var name string
	if err := conn.QueryRowContext(t.Context(), `SELECT name FROM project WHERE code = 'PRJ-METRO'`).Scan(&name); err != nil {
		t.Errorf("project PRJ-METRO: %v", err)
	} else if name != "Pune Metro Depot" {
		t.Errorf("PRJ-METRO name = %q, want %q", name, "Pune Metro Depot")
	}

	// Rights: Employee -> fixture.Claim is read, create, write, submit, print (own)
	// and read, approve (team), like Expense Claim (CD-127); scopes are own/team/all.
	if len(fx.Rights) == 0 {
		t.Fatal("Fixtures.Rights is empty")
	}
	wantClaim := map[string][]string{
		"own":  {"read", "create", "write", "submit", "print"},
		"team": {"read", "approve"},
	}
	var claimRows int
	for _, r := range fx.Rights {
		if r.Scope != "own" && r.Scope != "team" && r.Scope != "all" {
			t.Errorf("rights row %s/%s scope = %q, want one of own, team, all", r.Role, r.DocType, r.Scope)
		}
		if r.Role == "Employee" && r.DocType == "fixture.Claim" {
			claimRows++
			if !reflect.DeepEqual(r.Rights, wantClaim[r.Scope]) {
				t.Errorf("Employee -> fixture.Claim row = %q scope %q, want %q", r.Rights, r.Scope, wantClaim[r.Scope])
			}
		}
	}
	if claimRows != 2 {
		t.Errorf("Employee -> fixture.Claim rights rows = %d, want 2", claimRows)
	}
}

func TestTestkit_DirectoryFromFixtures(t *testing.T) {
	d := testkit.NewDB(t)
	fx := testkit.Seed(t, d)
	dir := testkit.Directory(fx)
	if dir == nil {
		t.Fatal("Directory returned nil")
	}
	ctx := context.Background()

	emp := func(code string) int64 {
		t.Helper()
		for _, e := range fx.Employees {
			if e.Code == code {
				return e.ID
			}
		}
		t.Fatalf("Fixtures.Employees has no %s", code)
		return 0
	}
	var priya = fx.Users[3]
	if priya.Email != "priya@vardhan.test" {
		t.Fatalf("Users[3] = %q, want priya@vardhan.test", priya.Email)
	}

	reports, err := dir.DirectReports(ctx, nil, emp("EMP-004"))
	if err != nil {
		t.Fatalf("DirectReports(EMP-004): %v", err)
	}
	if want := []int64{emp("EMP-007")}; !reflect.DeepEqual(reports, want) {
		t.Errorf("DirectReports(Priya's EMP-004) = %v, want %v (Deepak's EMP-007)", reports, want)
	}

	chain, err := dir.ManagerChain(ctx, nil, emp("EMP-007"))
	if err != nil {
		t.Fatalf("ManagerChain(EMP-007): %v", err)
	}
	if want := []int64{emp("EMP-004"), emp("EMP-003"), emp("EMP-002")}; !reflect.DeepEqual(chain, want) {
		t.Errorf("ManagerChain(Deepak's EMP-007) = %v, want %v (EMP-004, EMP-003, EMP-002)", chain, want)
	}

	uid, ok, err := dir.UserForEmployee(ctx, nil, emp("EMP-004"))
	if err != nil || !ok || uid != priya.ID {
		t.Errorf("UserForEmployee(EMP-004) = (%d, %v, %v), want (%d, true, nil)", uid, ok, err, priya.ID)
	}
	eid, ok, err := dir.EmployeeForUser(ctx, nil, priya.ID)
	if err != nil || !ok || eid != emp("EMP-004") {
		t.Errorf("EmployeeForUser(Priya) = (%d, %v, %v), want (%d, true, nil)", eid, ok, err, emp("EMP-004"))
	}

	// EMP-008 has status Left, so the directory treats it as inactive (CD-54).
	if uid, ok, err := dir.UserForEmployee(ctx, nil, emp("EMP-008")); err != nil || ok || uid != 0 {
		t.Errorf("UserForEmployee(EMP-008, Left) = (%d, %v, %v), want (0, false, nil)", uid, ok, err)
	}

	// Project and site codes come from fx (database ids filled in by Seed).
	conn := d.SQL()
	var prjID, siteID int64
	if err := conn.QueryRowContext(t.Context(), `SELECT id FROM project WHERE code = 'PRJ-METRO'`).Scan(&prjID); err != nil {
		t.Fatalf("PRJ-METRO id: %v", err)
	}
	if err := conn.QueryRowContext(t.Context(), `SELECT id FROM site WHERE code = 'METRO-SITE'`).Scan(&siteID); err != nil {
		t.Fatalf("METRO-SITE id: %v", err)
	}
	pc, err := dir.ProjectCodes(ctx, nil, []int64{prjID})
	if err != nil || pc[prjID] != "PRJ-METRO" {
		t.Errorf("ProjectCodes([%d]) = (%v, %v), want {%d: PRJ-METRO}", prjID, pc, err, prjID)
	}
	sc, err := dir.SiteCodes(ctx, nil, []int64{siteID})
	if err != nil || sc[siteID] != "METRO-SITE" {
		t.Errorf("SiteCodes([%d]) = (%v, %v), want {%d: METRO-SITE}", siteID, sc, err, siteID)
	}
}

// TestTestkit_NoStandInTables (Task 23): the Wave 1 stand-ins are gone, with
// the migration set that made them.
func TestTestkit_NoStandInTables(t *testing.T) {
	conn := testkit.NewDB(t).SQL()
	for _, name := range []string{"testkit_employee", "testkit_project", "testkit_site", "goose_testkit"} {
		if tableExists(t, conn, name) {
			t.Errorf("table %q exists; Seed writes the real master tables", name)
		}
	}
}

// insertUsers adds the fixture users, which employee.user_id references.
func insertUsers(t *testing.T, d *db.DB, fx testkit.Fixtures) {
	t.Helper()
	if err := d.Write(t.Context(), func(tx *db.Tx) error {
		for _, u := range fx.Users {
			if _, err := tx.SQL().ExecContext(t.Context(),
				`INSERT INTO users (id, email, name, enabled, created_at, updated_at) VALUES (?, ?, ?, 1, 'x', 'x')`,
				int64(u.ID), u.Email, u.Name); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("insert users: %v", err)
	}
}

// TestTestkit_SeedMastersLeavesEmployeesUnlinked (CD-199 item 4): SeedMasters
// works with no users; the link comes from LinkEmployees, once they exist.
func TestTestkit_SeedMastersLeavesEmployeesUnlinked(t *testing.T) {
	d := testkit.NewDB(t)
	fx := testkit.SeedMasters(t, d)
	linked := func() int {
		t.Helper()
		var n int
		if err := d.SQL().QueryRowContext(t.Context(), `SELECT count(user_id) FROM employee`).Scan(&n); err != nil {
			t.Fatalf("count linked employees: %v", err)
		}
		return n
	}
	if n := linked(); n != 0 {
		t.Errorf("after SeedMasters, %d employees have a user_id; want 0", n)
	}

	// Without users the FK refuses the link, and that fails the test.
	fake, returned := runWithFake(t, func(tb testing.TB) { testkit.LinkEmployees(tb, d, fx) })
	if returned || !fake.fatal {
		t.Errorf("LinkEmployees with no users: returned %v, fatal %v; want t.Fatalf", returned, fake.fatal)
	}

	insertUsers(t, d, fx)
	testkit.LinkEmployees(t, d, fx)
	if n := linked(); n != len(fx.Employees) {
		t.Errorf("after LinkEmployees, %d employees have a user_id; want %d", n, len(fx.Employees))
	}
}

// TestTestkit_LinkEmployeesFailsOnUnknownEmployee: an id Seed didn't produce
// updates nothing, and that must not pass silently.
func TestTestkit_LinkEmployeesFailsOnUnknownEmployee(t *testing.T) {
	d := testkit.NewDB(t)
	fx := testkit.SeedMasters(t, d)
	insertUsers(t, d, fx)
	fx.Employees[0].ID = 9999
	fake, returned := runWithFake(t, func(tb testing.TB) { testkit.LinkEmployees(tb, d, fx) })
	if returned || !fake.fatal || !strings.Contains(fake.message, "EMP-001") {
		t.Errorf("LinkEmployees on unknown id: returned %v, fatal %v, message %q; want Fatalf naming EMP-001", returned, fake.fatal, fake.message)
	}
}

func TestTestkit_ColumnTypesRuleSkipsGoose(t *testing.T) {
	d := testkit.NewDB(t)
	conn := d.SQL()

	violations, skipped := columnTypeViolations(t, conn)
	if len(violations) != 0 {
		t.Errorf("column-type rule violations on a fresh NewDB: %q", violations)
	}
	if !slices.Contains(skipped, "goose_db_version") {
		t.Errorf("skip list %q does not include goose_db_version", skipped)
	}
	// The skip matters: goose's own table declares a non-allowed type (tstamp TIMESTAMP).
	var gooseBad bool
	for _, c := range tableColumns(t, conn, "goose_db_version") {
		if c.Type != "INTEGER" && c.Type != "TEXT" && c.Type != "BLOB" {
			gooseBad = true
		}
	}
	if !gooseBad {
		t.Error("goose_db_version declares only allowed types; expected goose's TIMESTAMP column, so the skip would be untested")
	}

	// The rule must catch a non-goose table that declares TIMESTAMP, and still skip goose_*.
	if err := d.Write(t.Context(), func(tx *db.Tx) error {
		if _, err := tx.SQL().ExecContext(t.Context(), `CREATE TABLE stray_events (id INTEGER, at TIMESTAMP)`); err != nil {
			return err
		}
		_, err := tx.SQL().ExecContext(t.Context(), `CREATE TABLE goose_extra (id INTEGER, at TIMESTAMP)`)
		return err
	}); err != nil {
		t.Fatalf("create test tables: %v", err)
	}
	violations, skipped = columnTypeViolations(t, conn)
	if want := []string{"stray_events.at TIMESTAMP"}; !reflect.DeepEqual(violations, want) {
		t.Errorf("violations after adding stray_events = %q, want %q", violations, want)
	}
	if !slices.Contains(skipped, "goose_extra") {
		t.Errorf("skip list %q does not include goose_extra", skipped)
	}
}

func TestTestkit_PrincipalUnknownEmailFails(t *testing.T) {
	fake, returned := runWithFake(t, func(tb testing.TB) {
		_ = testkit.Principal(tb, "nobody@vardhan.test")
	})
	if returned {
		t.Error("Principal with an unknown email returned normally, want t.Fatalf")
	}
	if !fake.fatal {
		t.Error("Principal with an unknown email did not call Fatalf")
	}
	if !strings.Contains(fake.message, "nobody@vardhan.test") {
		t.Errorf("Fatalf message %q does not name the unknown email", fake.message)
	}

	// A known email works without failing and comes from Fixtures.Users.
	p := testkit.Principal(t, "priya@vardhan.test")
	if int64(p.ID) != 4 || p.Name != "Priya Shah" || !reflect.DeepEqual(p.Roles, []string{"Employee", "Purchase User"}) {
		t.Errorf("Principal(priya) = %+v, want {ID:4 Name:Priya Shah Roles:[Employee Purchase User]}", p)
	}
}

func TestTestkit_ClockIsIST1030(t *testing.T) {
	c := testkit.Clock()
	if c == nil {
		t.Fatal("Clock returned nil")
	}
	if got, want := c.Now(), time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("Clock().Now() = %v, want %v", got, want)
	}
	if got := testkit.Clock().Now().In(clock.IST).Format("02-01-2006 15:04"); got != "24-10-2026 10:30" {
		t.Errorf("Clock in IST = %q, want %q", got, "24-10-2026 10:30")
	}
}

// TestTestkit_SeedUsesAuthTables (CD-67, CD-91): after Seed, the real AUTH
// reads the fixture users, roles and rights from its own tables.
func TestTestkit_SeedUsesAuthTables(t *testing.T) {
	d := testkit.NewDB(t)
	ctx := t.Context()
	var before int64
	if err := d.SQL().QueryRowContext(ctx, `SELECT perm_version FROM auth_meta`).Scan(&before); err != nil {
		t.Fatalf("perm_version before Seed: %v", err)
	}
	fx := testkit.Seed(t, d)
	a := auth.New(auth.Deps{DB: d, Clock: testkit.Clock()})

	for _, u := range fx.Users {
		var (
			info auth.UserInfo
			ok   bool
		)
		if err := d.Read(ctx, func(tx *db.Tx) error {
			var err error
			info, ok, err = a.User(ctx, tx, u.ID)
			return err
		}); err != nil {
			t.Fatalf("User(%d): %v", u.ID, err)
		}
		if !ok {
			t.Errorf("User(%d) (%s) not found", u.ID, u.Email)
			continue
		}
		if info.Email != u.Email || info.Name != u.Name || info.Enabled == u.Disabled {
			t.Errorf("User(%d) = %+v, want %q %q disabled %v", u.ID, info, u.Email, u.Name, u.Disabled)
		}
		p, state, err := a.LoadPrincipal(ctx, u.ID)
		if err != nil {
			t.Fatalf("LoadPrincipal(%s): %v", u.Email, err)
		}
		if want := testkit.Principal(t, u.Email); !reflect.DeepEqual(p, want) {
			t.Errorf("LoadPrincipal(%s) = %+v, testkit.Principal = %+v", u.Email, p, want)
		}
		if state.Email != u.Email || state.Enabled == u.Disabled {
			t.Errorf("LoadPrincipal(%s) state = %+v, disabled %v", u.Email, state, u.Disabled)
		}
	}

	// Each row, with a right held at two scopes kept at the wider one: AUTH
	// stores one scope per role, DocType and right.
	type row struct{ role, doctype string }
	want := map[row]map[auth.Right]auth.Scope{}
	for _, r := range fx.Rights {
		k := row{r.Role, r.DocType}
		if want[k] == nil {
			want[k] = map[auth.Right]auth.Scope{}
		}
		for _, w := range r.Rights {
			want[k][auth.Right(w)] = auth.Wider(want[k][auth.Right(w)], auth.Scope(r.Scope))
		}
	}
	for k, rights := range want {
		got, err := a.RoleRights(ctx, k.role, k.doctype)
		if err != nil {
			t.Fatalf("RoleRights(%s, %s): %v", k.role, k.doctype, err)
		}
		if !reflect.DeepEqual(got, rights) {
			t.Errorf("RoleRights(%s, %s) = %v, want %v", k.role, k.doctype, got, rights)
		}
	}
	claim, err := a.RoleRights(ctx, "Employee", "fixture.Claim")
	if err != nil {
		t.Fatalf("RoleRights(Employee, fixture.Claim): %v", err)
	}
	wantClaim := map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeTeam, auth.RightApprove: auth.ScopeTeam,
		auth.RightCreate: auth.ScopeOwn, auth.RightWrite: auth.ScopeOwn, auth.RightSubmit: auth.ScopeOwn, auth.RightPrint: auth.ScopeOwn,
	}
	if !reflect.DeepEqual(claim, wantClaim) {
		t.Errorf("Employee -> fixture.Claim = %v, want %v", claim, wantClaim)
	}
	admin, err := a.RoleRights(ctx, "Administrator", "*")
	if err != nil {
		t.Fatalf("RoleRights(Administrator, *): %v", err)
	}
	if len(admin) != len(auth.AllRights) {
		t.Errorf("Administrator -> * = %v, want all %d rights", admin, len(auth.AllRights))
	}
	for r, s := range admin {
		if s != auth.ScopeAll {
			t.Errorf("Administrator -> * %s scope = %q, want all", r, s)
		}
	}

	// AUTH caches the matrix by perm_version, so the rows must move it.
	var after int64
	if err := d.SQL().QueryRowContext(ctx, `SELECT perm_version FROM auth_meta`).Scan(&after); err != nil {
		t.Fatalf("perm_version after Seed: %v", err)
	}
	if after <= before {
		t.Errorf("perm_version = %d after Seed, was %d; want it bumped", after, before)
	}

	// CD-199 item 4: every employee links to the fixture user with that id.
	for _, e := range fx.Employees {
		var user int64
		if err := d.SQL().QueryRowContext(ctx, `SELECT user_id FROM employee WHERE id = ?`, e.ID).Scan(&user); err != nil {
			t.Fatalf("employee %s user_id: %v", e.Code, err)
		}
		if user != int64(e.UserID) {
			t.Errorf("employee %s user_id = %d, want %d", e.Code, user, e.UserID)
		}
	}
}
