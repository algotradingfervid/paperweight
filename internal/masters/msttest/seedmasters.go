package msttest

import (
	"context"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Masters holds the ids SeedMasters created, by code. Addresses is keyed by
// place (party addresses), BranchAddresses by branch code.
type Masters struct {
	Branches, Parties, Addresses, BranchAddresses map[string]int64
	UoMs, TaxCodes, Items                         map[string]int64
	Projects, Sites, Employees, ExpenseTypes      map[string]int64
}

// The values 00-fixtures.md leaves open (M9).
var (
	places = map[string]struct {
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
	branchPlaces = map[string]struct{ city, pincode string }{
		"PUNE": {"Pune", "411019"},
		"BLR":  {"Bengaluru", "560066"},
	}
	projectBranches = map[string]string{"PRJ-METRO": "PUNE", "PRJ-TECHPARK": "BLR"} // MST-106
	unitCodes       = map[string]string{"kg": "KGS", "Nos": "NOS", "m": "MTR", "Box": "BOX", "Bag": "BAG", "Hour": "HRS", "Drum": "DRUM"}
	expenseNames    = map[string]string{"TRAVEL": "Travel", "FOOD": "Food", "FUEL": "Fuel", "LODGING": "Lodging", "SITE-MISC": "Site miscellaneous"}
	joiningDate     = types.Date{Year: 2024, Month: time.April, Day: 1}
	relievingDate   = types.Date{Year: 2026, Month: time.March, Day: 31}
)

const (
	defaultTaxCode = "GST18"
	addressLine1   = "Main Road"
)

// SeedMasters creates every canonical master (00-fixtures.md plus M9) through
// the services, as Admin, then applies the fixtures' user restrictions with
// the real ids. It fails the test when an employee, project or site id differs
// from e.Fx, which AUTH's Directory fake was built from.
func SeedMasters(t testing.TB, e *Env) Masters {
	t.Helper()
	ctx := context.Background()
	m := Masters{
		Branches: map[string]int64{}, Parties: map[string]int64{}, Addresses: map[string]int64{}, BranchAddresses: map[string]int64{},
		Items: map[string]int64{}, Projects: map[string]int64{}, Sites: map[string]int64{}, Employees: map[string]int64{}, ExpenseTypes: map[string]int64{},
	}
	if err := e.DB.Write(ctx, func(tx *db.Tx) error {
		s := seeder{ctx: ctx, tx: tx, e: e, m: &m}
		for _, step := range []func() error{s.company, s.parties, s.catalogue, s.organisation} {
			if err := step(); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("msttest.SeedMasters: %v", err)
	}
	for _, p := range e.Fx.Projects {
		checkID(t, p.Code, m.Projects[p.Code], p.ID)
	}
	for _, s := range e.Fx.Sites {
		checkID(t, s.Code, m.Sites[s.Code], s.ID)
	}
	for _, emp := range e.Fx.Employees {
		checkID(t, emp.Code, m.Employees[emp.Code], emp.ID)
	}
	restrict(t, e.auth, e.Admin, e.Fx)
	return m
}

func checkID(t testing.TB, code string, got, want int64) {
	t.Helper()
	if got != want {
		t.Fatalf("msttest.SeedMasters: %s has id %d, want %d from Fx", code, got, want)
	}
}

// seeder writes the masters inside one transaction.
type seeder struct {
	ctx context.Context
	tx  *db.Tx
	e   *Env
	m   *Masters
}

func (s seeder) company() error {
	svc := company.New(s.e.Kit)
	addresses := address.New(s.e.Kit)
	c := s.e.Fx.Company
	if err := svc.SaveCompanyTx(s.ctx, s.tx, s.e.Admin, company.CompanyInput{LegalName: c.LegalName, PAN: c.PAN}, 0); err != nil {
		return err
	}
	for _, b := range s.e.Fx.Branches {
		saved, err := svc.CreateTx(s.ctx, s.tx, s.e.Admin, company.BranchInput{
			Code: b.Code, Name: b.Name, StateCode: b.StateCode, GSTIN: b.GSTIN, IsHeadOffice: b.HeadOffice,
		})
		if err != nil {
			return err
		}
		s.m.Branches[b.Code] = saved.ID
		place := branchPlaces[b.Code]
		saved, err = addresses.CreateTx(s.ctx, s.tx, s.e.Admin, address.Input{
			BranchID: saved.ID, Title: b.Name, Line1: addressLine1, City: place.city, StateCode: b.StateCode,
			Pincode: place.pincode, GSTIN: b.GSTIN, IsBilling: true, PrimaryBilling: true,
		})
		if err != nil {
			return err
		}
		s.m.BranchAddresses[b.Code] = saved.ID
	}
	return nil
}

func (s seeder) parties() error {
	parties := party.New(s.e.Kit)
	addresses := address.New(s.e.Kit)
	for _, p := range s.e.Fx.Parties {
		saved, err := parties.CreateTx(s.ctx, s.tx, s.e.Admin, party.Input{
			Code: p.Code, Name: p.Name, IsVendor: true, PAN: p.PAN, GSTCategory: gstin.Category(p.GSTCategory),
		})
		if err != nil {
			return err
		}
		s.m.Parties[p.Code] = saved.ID
		for _, a := range p.Addresses {
			pl := places[a.Place]
			created, err := addresses.CreateTx(s.ctx, s.tx, s.e.Admin, address.Input{
				PartyID: saved.ID, Title: a.Place, Line1: addressLine1, City: a.Place, StateCode: pl.state, Pincode: pl.pincode,
				GSTIN: a.GSTIN, IsBilling: !pl.shipping, IsShipping: pl.shipping,
			})
			if err != nil {
				return err
			}
			s.m.Addresses[a.Place] = created.ID
		}
	}
	return nil
}

func (s seeder) catalogue() error {
	var err error
	if s.m.UoMs, err = seededCodes(s.ctx, s.tx, "uom"); err != nil {
		return err
	}
	if s.m.TaxCodes, err = seededCodes(s.ctx, s.tx, "tax_code"); err != nil {
		return err
	}
	items := item.New(s.e.Kit)
	for _, it := range s.e.Fx.Items {
		in := item.Input{
			Code: it.Code, Name: it.Name, Type: item.ItemType(it.Type), HSNSAC: it.HSN,
			StockUoMID: s.m.UoMs[unitCodes[it.StockUoM]], DefaultTaxCodeID: s.m.TaxCodes[defaultTaxCode], IsStock: it.Type == string(item.TypeGoods),
		}
		if it.OtherUoM != "" {
			in.Conversions = []item.Conversion{{UoMID: s.m.UoMs[unitCodes[it.OtherUoM]], Factor: types.Factor(it.OtherFactor) * uom.FactorOne}}
		}
		saved, err := items.CreateTx(s.ctx, s.tx, s.e.Admin, in)
		if err != nil {
			return err
		}
		s.m.Items[it.Code] = saved.ID
	}
	return nil
}

// seededCodes reads the rows a migration seeded in table (uom, tax_code).
func seededCodes(ctx context.Context, tx *db.Tx, table string) (map[string]int64, error) {
	rows, err := tx.SQL().QueryContext(ctx, `SELECT code, id FROM `+table) //nolint:gosec // G202: fixed table names
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int64{}
	for rows.Next() {
		var code string
		var id int64
		if err := rows.Scan(&code, &id); err != nil {
			return nil, err
		}
		out[code] = id
	}
	return out, rows.Err()
}

func (s seeder) organisation() error {
	projects := project.New(s.e.Kit)
	for _, p := range s.e.Fx.Projects {
		saved, err := projects.CreateTx(s.ctx, s.tx, s.e.Admin, project.Input{Code: p.Code, Name: p.Name, BranchID: s.m.Branches[projectBranches[p.Code]]})
		if err != nil {
			return err
		}
		s.m.Projects[p.Code] = saved.ID
	}
	sites := site.New(s.e.Kit)
	for _, st := range s.e.Fx.Sites {
		// The fixtures doc gives sites no name, so the code is the name.
		saved, err := sites.CreateTx(s.ctx, s.tx, s.e.Admin, site.Input{
			Code: st.Code, Name: st.Code, BranchID: s.m.Branches[st.BranchCode], ProjectID: s.m.Projects[st.ProjectCode], Type: site.SiteType(st.Kind),
		})
		if err != nil {
			return err
		}
		s.m.Sites[st.Code] = saved.ID
	}
	if err := s.employees(); err != nil {
		return err
	}
	expenses := expensetype.New(s.e.Kit)
	for _, code := range s.e.Fx.ExpenseTypes {
		saved, err := expenses.CreateTx(s.ctx, s.tx, s.e.Admin, expensetype.Input{Code: code, Name: expenseNames[code]})
		if err != nil {
			return err
		}
		s.m.ExpenseTypes[code] = saved.ID
	}
	return nil
}

// employees creates the employees in fixture order: managers come first.
func (s seeder) employees() error {
	names := map[string]string{}
	for _, u := range s.e.Fx.Users {
		names[u.EmployeeCode] = u.Name
	}
	employees := employee.New(s.e.Kit)
	for _, f := range s.e.Fx.Employees {
		in := employee.Input{
			Code: f.Code, Name: names[f.Code], BranchID: s.m.Branches["PUNE"], UserID: f.UserID,
			ManagerID: s.m.Employees[f.ManagerCode], Joining: joiningDate, Status: employee.Status(f.Status),
		}
		if in.Status == employee.Left {
			in.Relieving = relievingDate
		}
		saved, err := employees.CreateTx(s.ctx, s.tx, s.e.Admin, in)
		if err != nil {
			return err
		}
		s.m.Employees[f.Code] = saved.ID
	}
	return nil
}
