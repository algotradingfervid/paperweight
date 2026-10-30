package allmasters_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	appmigrations "github.com/vardhan/internaltools/migrations"
)

// sqlSeeded is an Env (which has the users) with the masters from testkit's
// plain SQL instead of the services.
func sqlSeeded(t *testing.T) (*msttest.Env, testkit.Fixtures) {
	t.Helper()
	env := msttest.New(t)
	fx := testkit.SeedMasters(t, env.DB)
	testkit.LinkEmployees(t, env.DB, fx)
	return env, fx
}

var createTable = regexp.MustCompile(`(?m)^CREATE TABLE (\w+)`)

// masterTables is every table the WP2 migrations (01000-01999) create, so a
// table added later is compared without changing this test.
func masterTables(t *testing.T) []string {
	t.Helper()
	files, err := fs.Glob(appmigrations.FS, "01*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	var tables []string
	for _, f := range files {
		body, err := fs.ReadFile(appmigrations.FS, f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, m := range createTable.FindAllSubmatch(body, -1) {
			tables = append(tables, string(m[1]))
		}
	}
	return tables
}

// tableRows returns table's rows as text in primary-key order, without the
// two timestamps.
func tableRows(t *testing.T, d *db.DB, table string) []string {
	t.Helper()
	ctx := context.Background()
	var out []string
	if err := d.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, `SELECT * FROM `+table+` ORDER BY 1`) //nolint:gosec // G202: names come from the migrations
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		cols, err := rows.Columns()
		if err != nil {
			return err
		}
		vals := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range vals {
			dest[i] = &vals[i]
		}
		for rows.Next() {
			if err := rows.Scan(dest...); err != nil {
				return err
			}
			var row []string
			for i, c := range cols {
				if c != "created_at" && c != "updated_at" {
					row = append(row, fmt.Sprintf("%s=%v", c, vals[i]))
				}
			}
			out = append(out, strings.Join(row, " "))
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	return out
}

// TestSeed_MatchesServiceBuiltMasters (Task 23): testkit.SeedMasters plus
// LinkEmployees writes exactly what msttest.SeedMasters writes. Only
// created_at and updated_at are ignored: Seed writes the Admin's id as
// created_by and updated_by, as the services do, so nothing is normalised.
func TestSeed_MatchesServiceBuiltMasters(t *testing.T) {
	seeded, fx := sqlSeeded(t)
	built := msttest.New(t)
	m := msttest.SeedMasters(t, built)

	tables := masterTables(t)
	for _, want := range []string{"company", "branch", "address", "item_uom", "employee", "expense_type"} {
		if !slices.Contains(tables, want) {
			t.Fatalf("masterTables = %v; want it to include %s", tables, want)
		}
	}
	for _, table := range tables {
		got, want := tableRows(t, seeded.DB, table), tableRows(t, built.DB, table)
		if !slices.Equal(got, want) {
			t.Errorf("%s differs\nSeed:\n  %s\nservices:\n  %s", table, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
		}
	}

	// The ids Seed reports are the rows' ids, for every master.
	check := func(kind, code string, got int64, ids map[string]int64) {
		t.Helper()
		if want, ok := ids[code]; !ok || got != want {
			t.Errorf("%s %s id = %d; want %d (in map: %v)", kind, code, got, want, ok)
		}
	}
	if fx.Company.ID != 1 {
		t.Errorf("Company.ID = %d; want 1", fx.Company.ID)
	}
	for _, b := range fx.Branches {
		check("branch", b.Code, b.ID, m.Branches)
		check("branch address", b.Code, b.AddressID, m.BranchAddresses)
	}
	for _, p := range fx.Parties {
		check("party", p.Code, p.ID, m.Parties)
		for _, a := range p.Addresses {
			check("address", a.Place, a.ID, m.Addresses)
		}
	}
	for _, it := range fx.Items {
		check("item", it.Code, it.ID, m.Items)
	}
	for _, u := range fx.Units {
		check("uom", u.Code, u.ID, m.UoMs)
	}
	for _, p := range fx.Projects {
		check("project", p.Code, p.ID, m.Projects)
	}
	for _, s := range fx.Sites {
		check("site", s.Code, s.ID, m.Sites)
	}
	for _, e := range fx.Employees {
		check("employee", e.Code, e.ID, m.Employees)
	}
	for code, id := range fx.ExpenseTypeIDs {
		check("expense type", code, id, m.ExpenseTypes)
	}
	if len(fx.ExpenseTypeIDs) != len(fx.ExpenseTypes) {
		t.Errorf("ExpenseTypeIDs = %v; want one id for each of %v", fx.ExpenseTypeIDs, fx.ExpenseTypes)
	}
	var taxIDs []int64
	for _, c := range fx.TaxCodes {
		taxIDs = append(taxIDs, c.ID)
	}
	var wantTax []int64
	for _, id := range m.TaxCodes {
		wantTax = append(wantTax, id)
	}
	slices.Sort(taxIDs)
	slices.Sort(wantTax)
	if !slices.Equal(taxIDs, wantTax) {
		t.Errorf("TaxCodes ids = %v; want %v", taxIDs, wantTax)
	}
	gst18 := -1
	for i, c := range fx.TaxCodes {
		if c.Category == "Taxable" && c.Percent == 18000 {
			gst18 = i
		}
	}
	if gst18 < 0 || fx.TaxCodes[gst18].ID != m.TaxCodes["GST18"] {
		t.Errorf("Taxable 18%% id wrong: index %d in %+v; want id %d", gst18, fx.TaxCodes, m.TaxCodes["GST18"])
	}
}

// TestSeed_PassesMasterValidation (Task 23): every row Seed writes survives
// its master's real update path with its own values. Each UpdateTx validates
// before it writes; a master's unchanged save has no shortcut around that.
func TestSeed_PassesMasterValidation(t *testing.T) {
	env, fx := sqlSeeded(t)
	svc := allmasters.New(env.Kit)
	ctx := context.Background()
	admin := env.Admin
	saved := 0

	if err := env.DB.Write(ctx, func(tx *db.Tx) error {
		c, ok, err := svc.Company.Company(ctx, tx)
		if err != nil {
			return fmt.Errorf("company: %w", err)
		}
		if !ok {
			return errors.New("company: no row")
		}
		if err := svc.Company.SaveCompanyTx(ctx, tx, admin, company.CompanyInput{LegalName: c.LegalName, PAN: c.PAN}, c.Version); err != nil {
			return fmt.Errorf("company: %w", err)
		}
		saved++

		addressIDs := []int64{}
		for _, b := range fx.Branches {
			r, err := svc.Company.Get(ctx, tx, b.ID)
			if err != nil {
				return fmt.Errorf("branch %s: %w", b.Code, err)
			}
			if _, err := svc.Company.UpdateTx(ctx, tx, admin, r.ID, r.Version, company.BranchInput{
				Code: r.Code, Name: r.Name, StateCode: r.StateCode, GSTIN: r.GSTIN, IsHeadOffice: r.IsHeadOffice,
			}); err != nil {
				return fmt.Errorf("branch %s: %w", b.Code, err)
			}
			addressIDs = append(addressIDs, b.AddressID)
			saved++
		}
		for _, p := range fx.Parties {
			r, err := svc.Party.Get(ctx, tx, p.ID)
			if err != nil {
				return fmt.Errorf("party %s: %w", p.Code, err)
			}
			if _, err := svc.Party.UpdateTx(ctx, tx, admin, r.ID, r.Version, party.Input{
				Code: r.Code, Name: r.Name, IsVendor: r.IsVendor, IsCustomer: r.IsCustomer, PAN: r.PAN,
				GSTCategory: r.GSTCategory, DefaultPaymentTermsID: r.DefaultPaymentTermsID,
			}); err != nil {
				return fmt.Errorf("party %s: %w", p.Code, err)
			}
			saved++
			for _, a := range p.Addresses {
				addressIDs = append(addressIDs, a.ID)
			}
		}
		for _, id := range addressIDs {
			r, err := svc.Address.Get(ctx, tx, id)
			if err != nil {
				return fmt.Errorf("address %d: %w", id, err)
			}
			if _, err := svc.Address.UpdateTx(ctx, tx, admin, r.ID, r.Version, address.Input{
				PartyID: r.PartyID, BranchID: r.BranchID, Title: r.Title, Line1: r.Line1, Line2: r.Line2, City: r.City,
				StateCode: r.StateCode, Country: r.Country, Pincode: r.Pincode, GSTIN: r.GSTIN,
				IsBilling: r.IsBilling, IsShipping: r.IsShipping, PrimaryBilling: r.PrimaryBilling, PrimaryShipping: r.PrimaryShipping,
			}); err != nil {
				return fmt.Errorf("address %s: %w", r.Title, err)
			}
			saved++
		}
		for _, it := range fx.Items {
			r, err := svc.Item.Get(ctx, tx, it.ID)
			if err != nil {
				return fmt.Errorf("item %s: %w", it.Code, err)
			}
			conv, err := svc.Item.Conversions(ctx, tx, r.ID)
			if err != nil {
				return fmt.Errorf("item %s: %w", it.Code, err)
			}
			if _, err := svc.Item.UpdateTx(ctx, tx, admin, r.ID, r.Version, item.Input{
				Code: r.Code, Name: r.Name, Type: r.Type, HSNSAC: r.HSNSAC, StockUoMID: r.StockUoMID, PurchaseUoMID: r.PurchaseUoMID,
				DefaultTaxCodeID: r.DefaultTaxCodeID, IsStock: r.IsStock, StandardRate: r.StandardRate, Conversions: conv,
			}); err != nil {
				return fmt.Errorf("item %s: %w", it.Code, err)
			}
			saved++
		}
		for _, p := range fx.Projects {
			r, err := svc.Project.Get(ctx, tx, p.ID)
			if err != nil {
				return fmt.Errorf("project %s: %w", p.Code, err)
			}
			if _, err := svc.Project.UpdateTx(ctx, tx, admin, r.ID, r.Version, project.Input{
				Code: r.Code, Name: r.Name, BranchID: r.BranchID, CustomerID: r.CustomerID, Start: r.Start, End: r.End, Status: r.Status,
			}); err != nil {
				return fmt.Errorf("project %s: %w", p.Code, err)
			}
			saved++
		}
		for _, s := range fx.Sites {
			r, err := svc.Site.Get(ctx, tx, s.ID)
			if err != nil {
				return fmt.Errorf("site %s: %w", s.Code, err)
			}
			if _, err := svc.Site.UpdateTx(ctx, tx, admin, r.ID, r.Version, site.Input{
				Code: r.Code, Name: r.Name, BranchID: r.BranchID, ProjectID: r.ProjectID, Type: r.Type,
			}); err != nil {
				return fmt.Errorf("site %s: %w", s.Code, err)
			}
			saved++
		}
		for _, e := range fx.Employees {
			r, err := svc.Employee.Get(ctx, tx, e.ID)
			if err != nil {
				return fmt.Errorf("employee %s: %w", e.Code, err)
			}
			if _, err := svc.Employee.UpdateTx(ctx, tx, admin, r.ID, r.Version, employee.Input{
				Code: r.Code, Name: r.Name, BranchID: r.BranchID, UserID: r.UserID, ManagerID: r.ManagerID,
				Joining: r.Joining, Birth: r.Birth, Relieving: r.Relieving, Status: r.Status,
			}); err != nil {
				return fmt.Errorf("employee %s: %w", e.Code, err)
			}
			saved++
		}
		for _, code := range fx.ExpenseTypes {
			r, err := svc.ExpenseType.Get(ctx, tx, fx.ExpenseTypeIDs[code])
			if err != nil {
				return fmt.Errorf("expense type %s: %w", code, err)
			}
			if _, err := svc.ExpenseType.UpdateTx(ctx, tx, admin, r.ID, r.Version, expensetype.Input{
				Code: r.Code, Name: r.Name, DefaultHSNSAC: r.DefaultHSNSAC, DefaultTaxCodeID: r.DefaultTaxCodeID,
			}); err != nil {
				return fmt.Errorf("expense type %s: %w", code, err)
			}
			saved++
		}
		return nil
	}); err != nil {
		t.Fatalf("resave: %v", err)
	}
	// Company 1, branches 2, parties 5, addresses 2+7, items 6, projects 2,
	// sites 3, employees 8, expense types 5.
	if saved != 41 {
		t.Errorf("resaved %d rows; want 41, one for every row Seed writes", saved)
	}
}
