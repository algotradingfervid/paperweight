package testkit

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The values 00-fixtures.md leaves open (plan decision M9). msttest.SeedMasters
// holds the same ones; TestSeed_MatchesServiceBuiltMasters ties them together.
var (
	seedPlaces = map[string]struct {
		pincode, state string
		shipping       bool // shipping address, else billing
	}{
		"Mumbai":    {pincode: "400001", state: "27"},
		"Thane":     {pincode: "400601", state: "27", shipping: true},
		"Delhi":     {pincode: "110001", state: "07", shipping: true},
		"Bengaluru": {pincode: "560001", state: "29"},
		"Rajkot":    {pincode: "360001", state: "24"},
		"Chennai":   {pincode: "600001", state: "33"},
		"Pune":      {pincode: "411001", state: "27"},
	}
	seedBranchPlaces = map[string]struct{ city, pincode string }{
		"PUNE": {"Pune", "411019"},
		"BLR":  {"Bengaluru", "560066"},
	}
	seedProjectBranches = map[string]string{"PRJ-METRO": "PUNE", "PRJ-TECHPARK": "BLR"} // MST-106
	seedUnitCodes       = map[string]string{"kg": "KGS", "Nos": "NOS", "m": "MTR", "Box": "BOX", "Bag": "BAG", "Hour": "HRS", "Drum": "DRUM"}
	seedExpenseNames    = map[string]string{"TRAVEL": "Travel", "FOOD": "Food", "FUEL": "Fuel", "LODGING": "Lodging", "SITE-MISC": "Site miscellaneous"}
)

const (
	seedAddressLine1  = "Main Road"
	seedJoiningDate   = "2024-04-01"
	seedRelievingDate = "2026-03-31"
	seedBranch        = "PUNE" // every employee's branch
	seedTaxCode       = "GST18"
	seedAdmin         = 1             // the fixtures' Administrator; created_by has no FK
	factorOne         = 1_000_000_000 // types.Factor is at 1e-9 scale
)

// Seed inserts, with plain SQL in foreign-key order, exactly the master rows
// msttest.SeedMasters creates, and returns the fixtures with their database
// ids filled in. Employees get user_id NULL: Seed creates no users, rights or
// sessions (AUTH owns those), so a caller with users then calls LinkEmployees
// (CD-199). The UoMs and tax codes are the migrations' rows.
func Seed(t testing.TB, d *db.DB) Fixtures {
	t.Helper()
	fx := canonical()
	ctx := context.Background()
	err := d.Write(ctx, func(tx *db.Tx) error {
		s := seeder{ctx: ctx, q: tx.SQL(), fx: &fx, at: types.TimestampOf(Clock().Now()).String(), branches: map[string]int64{}}
		for _, step := range []func() error{s.company, s.parties, s.catalogue, s.projects, s.employees, s.expenseTypes} {
			if err := step(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("testkit.Seed: insert masters: %v", err)
	}
	return fx
}

// LinkEmployees sets each fixture employee's user_id. Call it once the
// fixture users exist: employee.user_id references users.
func LinkEmployees(t testing.TB, d *db.DB, fx Fixtures) {
	t.Helper()
	ctx := context.Background()
	err := d.Write(ctx, func(tx *db.Tx) error {
		for _, e := range fx.Employees {
			res, err := tx.SQL().ExecContext(ctx, `UPDATE employee SET user_id = ? WHERE id = ?`, int64(e.UserID), e.ID)
			if err != nil {
				return fmt.Errorf("%s: %w", e.Code, err)
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return fmt.Errorf("%s: no employee with id %d", e.Code, e.ID)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("testkit.LinkEmployees: %v", err)
	}
}

// seeder writes the masters inside one transaction.
type seeder struct {
	ctx      context.Context
	q        db.DBTX
	fx       *Fixtures
	at       string
	branches map[string]int64
}

// insert adds a row with the audit columns the services would write, as the
// Administrator, and returns its id.
func (s seeder) insert(table, columns string, values ...any) (int64, error) {
	marks := strings.Repeat("?, ", len(values))
	args := append(values, s.at, seedAdmin, s.at, seedAdmin)
	var id int64
	err := s.q.QueryRowContext(s.ctx, `INSERT INTO `+table+` (`+columns+`, created_at, created_by, updated_at, updated_by)
		VALUES (`+marks+`?, ?, ?, ?) RETURNING id`, args...).Scan(&id) //nolint:gosec // G202: fixed table and column names
	if err != nil {
		return 0, fmt.Errorf("%s: %w", table, err)
	}
	return id, nil
}

// optional is NULL for a zero id.
func optional(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

func (s seeder) company() error {
	c := &s.fx.Company
	var err error
	if c.ID, err = s.insert("company", "name, pan", c.LegalName, c.PAN); err != nil {
		return err
	}
	for i := range s.fx.Branches {
		b := &s.fx.Branches[i]
		if b.ID, err = s.insert("branch", "code, name, state_code, gstin, is_head_office",
			b.Code, b.Name, b.StateCode, b.GSTIN, b.HeadOffice); err != nil {
			return err
		}
		s.branches[b.Code] = b.ID
		place := seedBranchPlaces[b.Code]
		if b.AddressID, err = s.insert("address",
			"branch_id, name, title, line1, city, state_code, pincode, gstin, is_billing, is_primary_billing",
			b.ID, b.Name, b.Name, seedAddressLine1, place.city, b.StateCode, place.pincode, b.GSTIN, true, true); err != nil {
			return err
		}
	}
	return nil
}

// parties adds the parties and their addresses. A party's first billing and
// first shipping address are primary, as address.Service makes them.
func (s seeder) parties() error {
	for i := range s.fx.Parties {
		p := &s.fx.Parties[i]
		var err error
		if p.ID, err = s.insert("party", "code, name, is_vendor, pan, gst_category", p.Code, p.Name, true, p.PAN, p.GSTCategory); err != nil {
			return err
		}
		var hasBilling, hasShipping bool
		for j := range p.Addresses {
			a := &p.Addresses[j]
			pl := seedPlaces[a.Place]
			primaryBilling := !pl.shipping && !hasBilling
			primaryShipping := pl.shipping && !hasShipping
			hasBilling = hasBilling || !pl.shipping
			hasShipping = hasShipping || pl.shipping
			if a.ID, err = s.insert("address",
				"party_id, name, title, line1, city, state_code, pincode, gstin, is_billing, is_shipping, is_primary_billing, is_primary_shipping",
				p.ID, a.Place, a.Place, seedAddressLine1, a.Place, pl.state, pl.pincode, a.GSTIN,
				!pl.shipping, pl.shipping, primaryBilling, primaryShipping); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s seeder) catalogue() error {
	uoms := map[string]int64{}
	for i := range s.fx.Units {
		u := &s.fx.Units[i]
		if err := s.q.QueryRowContext(s.ctx, `SELECT id FROM uom WHERE code = ?`, u.Code).Scan(&u.ID); err != nil {
			return fmt.Errorf("uom %s: %w", u.Code, err)
		}
		uoms[u.Code] = u.ID
	}
	for i := range s.fx.TaxCodes {
		c := &s.fx.TaxCodes[i]
		if err := s.q.QueryRowContext(s.ctx, `SELECT id FROM tax_code WHERE treatment = ? AND rate = ?`,
			c.Category, int64(c.Percent)).Scan(&c.ID); err != nil {
			return fmt.Errorf("tax code %s %d: %w", c.Category, c.Percent, err)
		}
	}
	var taxCode int64
	if err := s.q.QueryRowContext(s.ctx, `SELECT id FROM tax_code WHERE code = ?`, seedTaxCode).Scan(&taxCode); err != nil {
		return fmt.Errorf("tax code %s: %w", seedTaxCode, err)
	}
	for i := range s.fx.Items {
		it := &s.fx.Items[i]
		stock := uoms[seedUnitCodes[it.StockUoM]]
		var err error
		if it.ID, err = s.insert("item",
			"code, name, item_type, hsn_sac, stock_uom_id, purchase_uom_id, default_tax_code_id, is_stock",
			it.Code, it.Name, it.Type, it.HSN, stock, stock, taxCode, it.Type == "Goods"); err != nil {
			return err
		}
		// The stock UoM's own row comes after the conversions, as in item.Service.
		line := 1
		if it.OtherUoM != "" {
			if err := s.itemUoM(it.ID, line, uoms[seedUnitCodes[it.OtherUoM]], it.OtherFactor*factorOne); err != nil {
				return err
			}
			line++
		}
		if err := s.itemUoM(it.ID, line, stock, factorOne); err != nil {
			return err
		}
	}
	return nil
}

func (s seeder) itemUoM(item int64, line int, uom, factor int64) error {
	_, err := s.q.ExecContext(s.ctx, `INSERT INTO item_uom (parent_id, line_no, uom_id, factor) VALUES (?, ?, ?, ?)`, item, line, uom, factor)
	return err
}

func (s seeder) projects() error {
	ids := map[string]int64{}
	for i := range s.fx.Projects {
		p := &s.fx.Projects[i]
		var err error
		if p.ID, err = s.insert("project", "code, name, branch_id", p.Code, p.Name, s.branches[seedProjectBranches[p.Code]]); err != nil {
			return err
		}
		ids[p.Code] = p.ID
	}
	for i := range s.fx.Sites {
		st := &s.fx.Sites[i]
		st.ProjectID = ids[st.ProjectCode]
		var err error
		// The fixtures doc gives sites no name, so the code is the name.
		if st.ID, err = s.insert("site", "code, name, branch_id, project_id, type",
			st.Code, st.Code, s.branches[st.BranchCode], optional(st.ProjectID), st.Kind); err != nil {
			return err
		}
	}
	return nil
}

// employees adds the employees in fixture order: managers come first.
func (s seeder) employees() error {
	names := map[string]string{}
	for _, u := range s.fx.Users {
		names[u.EmployeeCode] = u.Name
	}
	ids := map[string]int64{}
	for i := range s.fx.Employees {
		e := &s.fx.Employees[i]
		left := e.Status == "Left"
		var relieving sql.NullString
		if left {
			relieving = sql.NullString{String: seedRelievingDate, Valid: true}
		}
		var err error
		if e.ID, err = s.insert("employee", "code, name, branch_id, manager_id, joining_date, relieving_date, status, disabled",
			e.Code, names[e.Code], s.branches[seedBranch], optional(ids[e.ManagerCode]), seedJoiningDate, relieving, e.Status, left); err != nil {
			return err
		}
		ids[e.Code] = e.ID
	}
	return nil
}

func (s seeder) expenseTypes() error {
	s.fx.ExpenseTypeIDs = map[string]int64{}
	for _, code := range s.fx.ExpenseTypes {
		id, err := s.insert("expense_type", "code, name", code, seedExpenseNames[code])
		if err != nil {
			return err
		}
		s.fx.ExpenseTypeIDs[code] = id
	}
	return nil
}
