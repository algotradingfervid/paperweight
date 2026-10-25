package fixture_test

import (
	"context"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// Supporting tests for WP1-DOC Task 5 (no DOC case ID).

var (
	headerConv = []string{"id", "number", "state", "state_reason", "amended_from_id", "amendment_root_id",
		"amendment_no", "version", "created_at", "created_by", "updated_at", "updated_by"}
	lineConv   = []string{"id", "parent_id", "line_no"}
	masterConv = []string{"id", "code", "name", "disabled", "blocked", "warning",
		"version", "created_at", "created_by", "updated_at", "updated_by"}
)

// newFixtureDB returns a testkit database with the fixture migrations applied.
func newFixtureDB(t *testing.T) *db.DB {
	t.Helper()
	d := testkit.NewDB(t)
	if err := db.Migrate(t.Context(), d, fixture.MigrationsFS, "goose_fixture"); err != nil {
		t.Fatalf("db.Migrate(fixture.MigrationsFS, goose_fixture): %v", err)
	}
	return d
}

// tableColumns returns column name -> declared type from PRAGMA table_info.
func tableColumns(t *testing.T, d *db.DB, table string) map[string]string {
	t.Helper()
	rows, err := d.SQL().QueryContext(t.Context(), `SELECT name, type FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	defer rows.Close()
	cols := map[string]string{}
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			t.Fatalf("scan table_info(%s): %v", table, err)
		}
		cols[name] = typ
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table_info(%s): %v", table, err)
	}
	if len(cols) == 0 {
		t.Fatalf("table %s does not exist", table)
	}
	return cols
}

func mustLookup(t *testing.T, name string) *doctype.DocType {
	t.Helper()
	dt, ok := doctype.Lookup(name)
	if !ok {
		t.Fatalf("doctype.Lookup(%q) not registered", name)
	}
	return dt
}

func fieldNames(fs []doctype.Field) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Name)
	}
	return out
}

func TestFixture_SchemaMatchesRegistry(t *testing.T) {
	d := newFixtureDB(t)

	if err := doctype.CheckRegistry(); err != nil {
		t.Fatalf("doctype.CheckRegistry() = %v, want nil", err)
	}

	documents := []string{fixture.Order, fixture.Receipt, fixture.Claim}
	masters := []string{fixture.Party, fixture.Item, fixture.Project}
	if got, want := slices.Concat(documents, masters),
		[]string{"fixture.Order", "fixture.Receipt", "fixture.Claim", "fixture.Party", "fixture.Item", "fixture.Project"}; !slices.Equal(got, want) {
		t.Errorf("DocType name consts = %q, want %q", got, want)
	}

	// Every convention column and every Field column at its level.
	checkLevel := func(dt *doctype.DocType, table string, conv []string, fields, otherLevel []doctype.Field) {
		t.Helper()
		cols := tableColumns(t, d, table)
		for _, c := range conv {
			if _, ok := cols[c]; !ok {
				t.Errorf("%s: table %s lacks convention column %q", dt.Name, table, c)
			}
		}
		own := map[string]bool{}
		for _, f := range fields {
			own[f.Column] = true
			if _, ok := cols[f.Column]; !ok {
				t.Errorf("%s: table %s lacks column %q for field %q", dt.Name, table, f.Column, f.Name)
			}
		}
		for _, f := range otherLevel {
			if own[f.Column] || slices.Contains(conv, f.Column) {
				continue
			}
			if _, ok := cols[f.Column]; ok {
				t.Errorf("%s: table %s has column %q of a field on the other level", dt.Name, table, f.Column)
			}
		}
	}
	for _, name := range documents {
		dt := mustLookup(t, name)
		if dt.Table == "" || dt.LinesTable == "" {
			t.Errorf("%s: Table %q, LinesTable %q; want both set", name, dt.Table, dt.LinesTable)
			continue
		}
		checkLevel(dt, dt.Table, headerConv, dt.HeaderFields(), dt.LineFields())
		checkLevel(dt, dt.LinesTable, lineConv, dt.LineFields(), dt.HeaderFields())
	}
	for _, name := range masters {
		dt := mustLookup(t, name)
		checkLevel(dt, dt.Table, masterConv, dt.Fields, nil)
	}
	wantTables := map[string]string{
		fixture.Order: "fixture_order", fixture.Receipt: "fixture_receipt", fixture.Claim: "fixture_claim",
		fixture.Party: "fixture_party", fixture.Item: "fixture_item", fixture.Project: "fixture_project",
	}
	for name, table := range wantTables {
		if dt := mustLookup(t, name); dt.Table != table {
			t.Errorf("%s: Table = %q, want %q", name, dt.Table, table)
		}
	}
	effect := tableColumns(t, d, "fixture_effect_log")
	for _, c := range []string{"id", "receipt_id", "order_line_id", "action"} {
		if _, ok := effect[c]; !ok {
			t.Errorf("fixture_effect_log lacks column %q", c)
		}
	}

	// Declared types only INTEGER, TEXT, BLOB (CD-34), on every fixture table.
	rows, err := d.SQL().QueryContext(t.Context(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name LIKE 'fixture\_%' ESCAPE '\'`)
	if err != nil {
		t.Fatalf("list fixture tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		tables = append(tables, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list fixture tables: %v", err)
	}
	_ = rows.Close()
	for _, want := range []string{"fixture_order", "fixture_order_line", "fixture_receipt", "fixture_receipt_line",
		"fixture_claim", "fixture_claim_line", "fixture_party", "fixture_item", "fixture_project", "fixture_effect_log"} {
		if !slices.Contains(tables, want) {
			t.Errorf("table %s missing; fixture tables = %q", want, tables)
		}
	}
	for _, table := range tables {
		for col, typ := range tableColumns(t, d, table) {
			if typ != "INTEGER" && typ != "TEXT" && typ != "BLOB" {
				t.Errorf("%s.%s declared type %q, want INTEGER, TEXT or BLOB (CD-34)", table, col, typ)
			}
		}
	}

	// CD-42 extras.
	order := mustLookup(t, fixture.Order)
	for name, want := range map[string]int{"supplier_id": 1, "amount": 2} {
		f, ok := order.Field(name)
		if !ok {
			t.Errorf("fixture.Order has no field %q", name)
			continue
		}
		if f.ListPriority != want {
			t.Errorf("fixture.Order %s ListPriority = %d, want %d", name, f.ListPriority, want)
		}
	}
	wantLines := map[string][]string{
		fixture.Order:   {"item_id", "qty", "rate", "uom", "description", "received_qty"},
		fixture.Receipt: {"order_line_id", "qty"},
		fixture.Claim:   {"expense_type", "description", "amount"},
	}
	for _, name := range documents {
		dt := mustLookup(t, name)
		lines := dt.LineFields()
		if got := fieldNames(lines); !slices.Equal(got, wantLines[name]) {
			t.Errorf("%s line fields = %q, want %q", name, got, wantLines[name])
		}
		for _, f := range lines {
			if f.Step != "lines" {
				t.Errorf("%s line field %s Step = %q, want \"lines\"", name, f.Name, f.Step)
			}
		}
	}
}

func lookupMaster(t *testing.T, d *db.DB, name string, id int64) (doctype.MasterInfo, bool) {
	t.Helper()
	dt := mustLookup(t, name)
	if dt.Master == nil {
		t.Fatalf("%s: Master is nil", name)
	}
	var (
		info  doctype.MasterInfo
		found bool
	)
	err := d.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		info, found, err = dt.Master.Lookup(context.Background(), tx, id)
		return err
	})
	if err != nil {
		t.Fatalf("%s Lookup(%d): %v", name, id, err)
	}
	return info, found
}

func TestFixture_MasterLookups(t *testing.T) {
	d := newFixtureDB(t)
	m := fixture.SeedMasters(t, d)

	for set, codes := range map[string]struct {
		got  map[string]int64
		want []string
	}{
		"Party":   {m.Party, []string{"SHREE", "BHARAT", "RAJKOT", "COROM", "LOCAL"}},
		"Item":    {m.Item, []string{"MS-ANGLE-50", "BOLT-M12", "WIRE-18G", "CABLE-4C", "CEMENT-OPC", "SVC-CRANE"}},
		"Project": {m.Project, []string{"PRJ-METRO", "PRJ-TECHPARK"}},
	} {
		if len(codes.got) != len(codes.want) {
			t.Errorf("Masters.%s has %d entries, want %d", set, len(codes.got), len(codes.want))
		}
		for _, c := range codes.want {
			if codes.got[c] == 0 {
				t.Errorf("Masters.%s[%q] = 0, want its id", set, c)
			}
		}
	}

	check := func(name string, id int64, want doctype.MasterInfo) {
		t.Helper()
		got, found := lookupMaster(t, d, name, id)
		if !found {
			t.Errorf("%s Lookup(%d) found = false, want true", name, id)
		}
		if got != want {
			t.Errorf("%s Lookup(%d) = %+v, want %+v", name, id, got, want)
		}
	}

	check(fixture.Party, m.Party["RAJKOT"], doctype.MasterInfo{Code: "RAJKOT", Name: "Rajkot Castings"})
	check(fixture.Item, m.Item["CEMENT-OPC"], doctype.MasterInfo{Code: "CEMENT-OPC", Name: "OPC Cement 53 Grade"})
	if got, _ := lookupMaster(t, d, fixture.Project, m.Project["PRJ-METRO"]); got.Code != "PRJ-METRO" {
		t.Errorf("Project Lookup(PRJ-METRO).Code = %q, want PRJ-METRO", got.Code)
	}

	fixture.SetDisabled(t, d, "fixture_party", "RAJKOT", true)
	check(fixture.Party, m.Party["RAJKOT"], doctype.MasterInfo{Code: "RAJKOT", Name: "Rajkot Castings", Disabled: true})
	fixture.SetDisabled(t, d, "fixture_party", "RAJKOT", false)
	check(fixture.Party, m.Party["RAJKOT"], doctype.MasterInfo{Code: "RAJKOT", Name: "Rajkot Castings"})

	const shreeBlocked = "Shree Steel Traders is on hold. Ask accounts before ordering."
	const wireBlocked = "Binding Wire 18G is blocked for purchase."
	fixture.SetBlocked(t, d, "fixture_party", "SHREE", shreeBlocked)
	fixture.SetBlocked(t, d, "fixture_item", "WIRE-18G", wireBlocked)
	check(fixture.Party, m.Party["SHREE"], doctype.MasterInfo{Code: "SHREE", Name: "Shree Steel Traders", Blocked: shreeBlocked})
	check(fixture.Item, m.Item["WIRE-18G"], doctype.MasterInfo{Code: "WIRE-18G", Name: "Binding Wire 18G", Blocked: wireBlocked})

	const bharatWarn = "Bharat Tools asks for advance payment on new orders."
	const boltWarn = "Hex Bolt M12x60 is low in stock."
	fixture.SetWarning(t, d, "fixture_party", "BHARAT", bharatWarn)
	fixture.SetWarning(t, d, "fixture_item", "BOLT-M12", boltWarn)
	check(fixture.Party, m.Party["BHARAT"], doctype.MasterInfo{Code: "BHARAT", Name: "Bharat Tools", Warning: bharatWarn})
	check(fixture.Item, m.Item["BOLT-M12"], doctype.MasterInfo{Code: "BOLT-M12", Name: "Hex Bolt M12x60", Warning: boltWarn})

	for _, name := range []string{fixture.Party, fixture.Item, fixture.Project} {
		if got, found := lookupMaster(t, d, name, 9999); found {
			t.Errorf("%s Lookup(9999) = %+v, found true; want found false", name, got)
		}
	}
}

func TestFixture_ClaimAmountIsSumOfLines(t *testing.T) {
	d := newFixtureDB(t)
	fx := testkit.Seed(t, d)
	var empID int64
	for _, e := range fx.Employees {
		if e.Code == "EMP-007" {
			empID = e.ID
		}
	}
	if empID == 0 {
		t.Fatal("testkit.Seed: no id for EMP-007")
	}

	claim := mustLookup(t, fixture.Claim)
	if claim.Hooks == nil {
		t.Fatal("fixture.Claim Hooks is nil")
	}
	const ts = "2026-10-24T05:00:00.000Z"
	insertClaim := func(tx *db.Tx, total int64) (int64, error) {
		var id int64
		err := tx.SQL().QueryRowContext(t.Context(),
			`INSERT INTO fixture_claim (state, version, created_at, created_by, updated_at, updated_by, employee_id, claim_date, total)
			 VALUES ('draft', 1, ?, 7, ?, 7, ?, '2026-10-24', ?) RETURNING id`, ts, ts, empID, total).Scan(&id)
		return id, err
	}
	readTotal := func(tx *db.Tx, id int64) (int64, error) {
		var total int64
		err := tx.SQL().QueryRowContext(t.Context(), `SELECT total FROM fixture_claim WHERE id = ?`, id).Scan(&total)
		return total, err
	}

	err := d.Write(t.Context(), func(tx *db.Tx) error {
		ctx := context.Background()

		// Lines TRAVEL 1,250.00 and FOOD 480.50.
		id, err := insertClaim(tx, 0)
		if err != nil {
			return err
		}
		if _, err := tx.SQL().ExecContext(ctx,
			`INSERT INTO fixture_claim_line (parent_id, line_no, expense_type, amount) VALUES (?, 1, 'TRAVEL', 125000), (?, 2, 'FOOD', 48050)`,
			id, id); err != nil {
			return err
		}
		amt, err := claim.Hooks.Amount(ctx, tx, id)
		if err != nil {
			t.Errorf("Claim Amount: %v", err)
		}
		if amt != types.Money(173050) {
			t.Errorf("Claim Amount = %d, want 173050", amt)
		}
		if fe := claim.Hooks.Validate(ctx, tx, id); fe != nil {
			t.Errorf("Claim Validate = %v, want nil", fe)
		}
		total, err := readTotal(tx, id)
		if err != nil {
			return err
		}
		if total != 173050 {
			t.Errorf("claim total after Validate = %d, want 173050", total)
		}

		// No lines: amount 0, Validate sets total 0.
		empty, err := insertClaim(tx, 999)
		if err != nil {
			return err
		}
		amt, err = claim.Hooks.Amount(ctx, tx, empty)
		if err != nil {
			t.Errorf("Claim Amount (no lines): %v", err)
		}
		if amt != 0 {
			t.Errorf("Claim Amount (no lines) = %d, want 0", amt)
		}
		_ = claim.Hooks.Validate(ctx, tx, empty)
		total, err = readTotal(tx, empty)
		if err != nil {
			return err
		}
		if total != 0 {
			t.Errorf("claim total after Validate (no lines) = %d, want 0", total)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("d.Write: %v", err)
	}

	if f, ok := claim.Field("total"); !ok || !f.ReadOnly {
		t.Errorf("fixture.Claim field total: found %v, ReadOnly %v; want ReadOnly (CD-79)", ok, f.ReadOnly)
	}
	if !mustLookup(t, fixture.Receipt).Printable {
		t.Error("fixture.Receipt Printable = false, want true (CD-82)")
	}
	if claim.Label != "Expense Claim" {
		t.Errorf("fixture.Claim Label = %q, want \"Expense Claim\"", claim.Label)
	}
	if claim.Series != "EXP/{FY}/{#####}" {
		t.Errorf("fixture.Claim Series = %q, want \"EXP/{FY}/{#####}\"", claim.Series)
	}
	if !claim.Approvable {
		t.Error("fixture.Claim Approvable = false, want true")
	}
	if claim.EmployeeField != "employee_id" {
		t.Errorf("fixture.Claim EmployeeField = %q, want \"employee_id\"", claim.EmployeeField)
	}
}
