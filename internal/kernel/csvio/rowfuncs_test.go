package csvio

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// rowCall is one recorded call of a test RowFunc.
type rowCall struct {
	DocType  string
	Tx       *db.Tx
	IsImport bool
	Row      map[string]string
	Parties  int // numt_party rows when a party call started
}

// calls records every test RowFunc call; tests reset it.
var calls []rowCall

func init() {
	RegisterImporter(listtest.NameParty, partyRow)
	RegisterImporter(listtest.NameItem, itemRow)
	RegisterImporter(listtest.NameUoM, uomRow)
	RegisterImporter(listtest.NameEmployee, employeeRow)
	doctype.Register(doctype.DocType{
		Name: nameCategory, Label: "Category", PluralLabel: "Categories", Table: "numt_csvio_category",
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Type: doctype.Text, Required: true, Importable: true},
			{Name: "name", Label: "Name", Type: doctype.Text, Importable: true},
			{Name: "parent", Label: "Parent", Type: doctype.Link, Link: nameCategory, Importable: true},
		},
		Master: listtest.TableLookup("numt_csvio_category"),
	})
	RegisterImporter(nameCategory, categoryRow)
}

// nameCategory is a csvio-only master whose Parent links to its own DocType.
const nameCategory = "csviotest.Category"

const categoryTable = "CREATE TABLE numt_csvio_category (id INTEGER PRIMARY KEY, code TEXT, name TEXT, " +
	"parent INTEGER, disabled INTEGER NOT NULL DEFAULT 0, created_at TEXT, updated_at TEXT, created_by INTEGER, updated_by INTEGER)"

func record(ctx context.Context, name string, tx *db.Tx, row map[string]string) {
	calls = append(calls, rowCall{DocType: name, Tx: tx, IsImport: IsImport(ctx), Row: row})
}

// parseCheck reads a Check cell already accepted by csvio.
func parseCheck(s string) bool {
	switch strings.ToLower(s) {
	case "yes", "true", "1":
		return true
	}
	return false
}

func stamp() string { return types.TimestampOf(testkit.Clock().Now()).String() }

func insert(ctx context.Context, tx *db.Tx, p auth.Principal, table string, cols []string, vals ...any) (int64, error) {
	cols = append(cols, "created_at", "updated_at", "created_by", "updated_by")
	vals = append(vals, stamp(), stamp(), p.ID, p.ID)
	q := "INSERT INTO " + table + " (" + strings.Join(cols, ", ") + ") VALUES (" +
		strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + ")"
	res, err := tx.SQL().ExecContext(ctx, q, vals...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// partyRow stands in for MST's party importer, including its GSTIN texts
// (MST-030, MST-031; CD-38). It inserts before checking the GSTIN, so a
// row with a field error leaves an insert that only csvio's per-row
// savepoint removes.
func partyRow(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	record(ctx, listtest.NameParty, tx, row)
	if err := tx.SQL().QueryRowContext(ctx, "SELECT count(*) FROM numt_party").Scan(&calls[len(calls)-1].Parties); err != nil {
		return 0, nil, err
	}
	id, err := insert(ctx, tx, p, "numt_party",
		[]string{"code", "name", "gstin", "state", "pin", "phone", "remarks", "disabled"},
		row["code"], row["name"], row["gstin"], row["state"], row["pin"], row["phone"], row["remarks"], parseCheck(row["disabled"]))
	if err != nil {
		return 0, nil, err
	}
	g := row["gstin"]
	switch {
	case g != "" && len(g) != 15:
		return 0, []doctype.FieldError{{Field: "gstin", Message: "GSTIN " + g + " must have 15 characters."}}, nil
	case g == "27AABCS1429B1Z5":
		return 0, []doctype.FieldError{{Field: "gstin", Message: "GSTIN " + g + " is not valid: the last character doesn't match. Check for typing mistakes."}}, nil
	}
	return id, nil, nil
}

func itemRow(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	record(ctx, listtest.NameItem, tx, row)
	uom, err := strconv.ParseInt(row["uom"], 10, 64)
	if err != nil {
		return 0, nil, fmt.Errorf("item row: uom %q is not an id: %w", row["uom"], err)
	}
	var code string
	if err := tx.SQL().QueryRowContext(ctx, "SELECT code FROM numt_uom WHERE id = ?", uom).Scan(&code); err != nil {
		return 0, nil, fmt.Errorf("item row: uom id %d: %w", uom, err)
	}
	var rate types.Rate
	if s := row["standard_rate"]; s != "" {
		if rate, err = types.ParseRate(s); err != nil {
			return 0, nil, err
		}
	}
	id, err := insert(ctx, tx, p, "numt_item",
		[]string{"code", "name", "item_type", "uom", "hsn_sac", "tax_code", "standard_rate", "disabled"},
		row["code"], row["name"], row["item_type"], uom, row["hsn_sac"], row["tax_code"], int64(rate), parseCheck(row["disabled"]))
	return id, nil, err
}

func uomRow(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	record(ctx, listtest.NameUoM, tx, row)
	id, err := insert(ctx, tx, p, "numt_uom", []string{"code", "name", "disabled"},
		row["code"], row["name"], parseCheck(row["disabled"]))
	return id, nil, err
}

func employeeRow(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	record(ctx, listtest.NameEmployee, tx, row)
	var doj any
	if s := row["date_of_joining"]; s != "" {
		d, err := types.ParseDate(s)
		if err != nil {
			return 0, nil, err
		}
		doj = d.ISO()
	}
	id, err := insert(ctx, tx, p, "numt_employee", []string{"code", "name", "date_of_joining"},
		row["code"], row["name"], doj)
	return id, nil, err
}

func categoryRow(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	record(ctx, nameCategory, tx, row)
	var parent any
	if row["parent"] != "" {
		parent = row["parent"]
	}
	id, err := insert(ctx, tx, p, "numt_csvio_category", []string{"code", "name", "parent"}, row["code"], row["name"], parent)
	return id, nil, err
}
