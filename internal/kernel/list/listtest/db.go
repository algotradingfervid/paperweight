package listtest

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// Setup returns a migrated test database with a numt_ table for every
// listtest DocType (columns INTEGER or TEXT only, CD-34).
func Setup(t testing.TB) *db.DB {
	t.Helper()
	d := testkit.NewDB(t)
	for _, dt := range docTypes() {
		mustExec(t, d, createTable(dt))
	}
	return d
}

func createTable(dt doctype.DocType) string {
	cols := []string{"id INTEGER PRIMARY KEY"}
	have := map[string]bool{}
	for _, f := range dt.Fields {
		typ := "INTEGER"
		switch f.Type {
		case doctype.Text, doctype.Select, doctype.Date, doctype.Timestamp:
			typ = "TEXT"
		}
		cols = append(cols, f.Name+" "+typ)
		have[f.Name] = true
	}
	if dt.Master != nil && !have["disabled"] {
		cols = append(cols, "disabled INTEGER NOT NULL DEFAULT 0")
	}
	cols = append(cols, "version INTEGER NOT NULL DEFAULT 1")
	if !have["created_at"] {
		cols = append(cols, "created_at TEXT")
	}
	cols = append(cols, "created_by INTEGER", "updated_at TEXT", "updated_by INTEGER")
	return "CREATE TABLE " + dt.Table + " (" + strings.Join(cols, ", ") + ")"
}

func mustExec(t testing.TB, d *db.DB, q string, args ...any) {
	t.Helper()
	err := d.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), q, args...)
		return err
	})
	if err != nil {
		t.Fatalf("listtest: %s: %v", q, err)
	}
}

// Insert adds one row to table; a nil value stores NULL. It returns the id.
func Insert(t testing.TB, d *db.DB, table string, row map[string]any) int64 {
	t.Helper()
	cols := make([]string, 0, len(row))
	for c := range row {
		cols = append(cols, c)
	}
	slices.Sort(cols)
	args := make([]any, len(cols))
	for i, c := range cols {
		args[i] = row[c]
	}
	q := "INSERT INTO " + table + " (" + strings.Join(cols, ", ") + ") VALUES (" +
		strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ") + ")"
	var id int64
	err := d.Write(context.Background(), func(tx *db.Tx) error {
		res, err := tx.SQL().ExecContext(context.Background(), q, args...)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		t.Fatalf("listtest: insert into %s: %v", table, err)
	}
	return id
}

// nullText stores "" as NULL.
func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullID stores 0 as NULL.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// nullDate stores the zero Date as NULL.
func nullDate(d types.Date) any {
	if d == (types.Date{}) {
		return nil
	}
	return d.ISO()
}

// PORow is one numt_po row; empty text, zero ids and zero dates are NULL.
type PORow struct {
	Number, PartyName, Status, Branch, Remarks string
	Date                                       types.Date
	ProjectID                                  int64
	GrandTotal                                 types.Money
	Qty                                        types.Qty
	Rate                                       types.Rate
	GSTRate                                    types.Percent
	CreatedAt                                  string // "2026-03-31T18:15:00.000Z"
}

func InsertPO(t testing.TB, d *db.DB, r PORow) int64 {
	t.Helper()
	return Insert(t, d, "numt_po", map[string]any{
		"number": nullText(r.Number), "party_name": nullText(r.PartyName), "status": nullText(r.Status),
		"date": nullDate(r.Date), "project_id": nullID(r.ProjectID), "branch": nullText(r.Branch),
		"grand_total": int64(r.GrandTotal), "qty": int64(r.Qty), "rate": int64(r.Rate),
		"gst_rate": int64(r.GSTRate), "remarks": nullText(r.Remarks), "created_at": nullText(r.CreatedAt),
	})
}

// PartyRow is one numt_party row; empty text is NULL.
type PartyRow struct {
	Code, Name, GSTIN, State, PIN, Phone, Remarks string
	Disabled                                      bool
}

func InsertParty(t testing.TB, d *db.DB, r PartyRow) int64 {
	t.Helper()
	return Insert(t, d, "numt_party", map[string]any{
		"code": r.Code, "name": r.Name, "gstin": nullText(r.GSTIN), "state": nullText(r.State),
		"pin": nullText(r.PIN), "phone": nullText(r.Phone), "remarks": nullText(r.Remarks), "disabled": r.Disabled,
	})
}

// ItemRow is one numt_item row; empty text is NULL.
type ItemRow struct {
	Code, Name, ItemType, HSNSAC, TaxCode string
	UoM                                   int64
	StandardRate                          types.Rate
	Disabled                              bool
}

func InsertItem(t testing.TB, d *db.DB, r ItemRow) int64 {
	t.Helper()
	return Insert(t, d, "numt_item", map[string]any{
		"code": r.Code, "name": r.Name, "item_type": nullText(r.ItemType), "uom": nullID(r.UoM),
		"hsn_sac": nullText(r.HSNSAC), "tax_code": nullText(r.TaxCode),
		"standard_rate": int64(r.StandardRate), "disabled": r.Disabled,
	})
}

// ExpenseRow is one numt_expense row; a zero project or date is NULL.
type ExpenseRow struct {
	Date      types.Date
	ProjectID int64
	Amount    types.Money
}

func InsertExpense(t testing.TB, d *db.DB, r ExpenseRow) int64 {
	t.Helper()
	return Insert(t, d, "numt_expense", map[string]any{
		"date": nullDate(r.Date), "project_id": nullID(r.ProjectID), "amount": int64(r.Amount),
	})
}

// ReceiptRow is one numt_grn row; empty text, zero ids and dates are NULL.
type ReceiptRow struct {
	Number string
	SiteID int64
	Date   types.Date
}

func InsertReceipt(t testing.TB, d *db.DB, r ReceiptRow) int64 {
	t.Helper()
	return Insert(t, d, "numt_grn", map[string]any{
		"number": nullText(r.Number), "site_id": nullID(r.SiteID), "date": nullDate(r.Date),
	})
}

// IDs maps a fixture code (party, project, site, UoM label, item) to its id.
type IDs map[string]int64

// placeState is the state of each fixture party's primary address.
var placeState = map[string]string{
	"Mumbai": "Maharashtra", "Bengaluru": "Karnataka", "Rajkot": "Gujarat",
	"Chennai": "Tamil Nadu", "Pune": "Maharashtra",
}

// SeedFixtures inserts the 00-fixtures.md parties, projects, sites, UoMs and
// items into the numt_ tables.
func SeedFixtures(t testing.TB, d *db.DB) IDs {
	t.Helper()
	fx := testkit.Seed(t, d)
	ids := IDs{}
	for _, p := range fx.Parties {
		a := p.Addresses[0]
		for _, x := range p.Addresses {
			if x.PrimaryBilling {
				a = x
			}
		}
		ids[p.Code] = InsertParty(t, d, PartyRow{Code: p.Code, Name: p.Name, GSTIN: a.GSTIN, State: placeState[a.Place]})
	}
	for _, p := range fx.Projects {
		ids[p.Code] = Insert(t, d, "numt_project", map[string]any{"code": p.Code, "name": p.Name})
	}
	for _, s := range fx.Sites {
		ids[s.Code] = Insert(t, d, "numt_site", map[string]any{"code": s.Code, "name": s.Code})
	}
	for _, u := range []string{"kg", "Nos", "Box", "m", "Drum", "Bag", "Hour"} {
		ids[u] = Insert(t, d, "numt_uom", map[string]any{"code": u, "name": u})
	}
	for _, it := range fx.Items {
		ids[it.Code] = InsertItem(t, d, ItemRow{
			Code: it.Code, Name: it.Name, ItemType: it.Type, UoM: ids[it.StockUoM], HSNSAC: it.HSN, TaxCode: it.GST,
		})
	}
	return ids
}

// Run executes Build and BuildCount and returns the page's ids and the total.
// A zero q.Page lists page 1.
func Run(t testing.TB, d *db.DB, dt *doctype.DocType, q list.Query, scope auth.Predicate) (ids []int64, total int) {
	t.Helper()
	q.Page = max(q.Page, 1)
	query, args, err := list.Build(dt, q, scope)
	if err != nil {
		t.Fatalf("listtest.Run: Build: %v", err)
	}
	countQ, countArgs, err := list.BuildCount(dt, q, scope)
	if err != nil {
		t.Fatalf("listtest.Run: BuildCount: %v", err)
	}
	ctx := context.Background()
	err = d.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		cols, err := rows.Columns()
		if err != nil {
			return err
		}
		var id int64
		dest := []any{&id}
		for range cols[1:] {
			dest = append(dest, new(any))
		}
		for rows.Next() {
			if err := rows.Scan(dest...); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return tx.SQL().QueryRowContext(ctx, countQ, countArgs...).Scan(&total)
	})
	if err != nil {
		t.Fatalf("listtest.Run: %s: %v", query, err)
	}
	return ids, total
}
