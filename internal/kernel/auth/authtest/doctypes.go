// Package authtest is AUTH's test support: the probe DocTypes and tables,
// an InfoSource over them, a ReferenceChecker and the canonical World.
package authtest

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Test-only DocTypes (CD-65); they stand in for fixture.Order and friends.
const (
	DTOrder   = "authtest.Order"
	DTReceipt = "authtest.Receipt"
	DTClaim   = "authtest.Claim"
	DTItem    = "authtest.Item"
	DTParty   = "authtest.Party"
	DTSite    = "authtest.Site"
	DTProject = "authtest.Project"
)

// NopHooks is the probe documents' Hooks: every method returns zero values.
type NopHooks struct{}

func (NopHooks) Validate(context.Context, *db.Tx, int64) []doctype.FieldError { return nil }
func (NopHooks) BeforeSubmit(context.Context, *db.Tx, int64) error            { return nil }
func (NopHooks) ApplyEffects(context.Context, *db.Tx, int64) error            { return nil }
func (NopHooks) ReverseEffects(context.Context, *db.Tx, int64) error          { return nil }
func (NopHooks) Amount(context.Context, *db.Tx, int64) (types.Money, error)   { return 0, nil }
func (NopHooks) Date(context.Context, *db.Tx, int64) (types.Date, error)      { return types.Date{}, nil }

// masterLookup is a doctype.MasterLookup over a probe master table.
type masterLookup struct{ table string }

func (m masterLookup) Lookup(ctx context.Context, tx *db.Tx, id int64) (doctype.MasterInfo, bool, error) {
	var info doctype.MasterInfo
	var enabled int64
	err := tx.SQL().QueryRowContext(ctx,
		`SELECT code, name, enabled FROM `+m.table+` WHERE id = ?`, id). //nolint:gosec // G202: table is a probe constant
		Scan(&info.Code, &info.Name, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return doctype.MasterInfo{}, false, nil
	}
	if err != nil {
		return doctype.MasterInfo{}, false, err
	}
	info.Disabled = enabled == 0
	return info, true, nil
}

// all lists rights at scope all, for DefaultRights.
func all(rights ...string) map[string]string {
	m := make(map[string]string, len(rights))
	for _, r := range rights {
		m[r] = "all"
	}
	return m
}

// docFields are the probe document columns as fields.
func docFields() []doctype.Field {
	return []doctype.Field{
		{Name: "number", Label: "Number", Column: "number", Type: doctype.Text, ReadOnly: true},
		{Name: "employee_id", Label: "Employee", Column: "employee_id", Type: doctype.Int},
		{Name: "project_id", Label: "Project", Column: "project_id", Type: doctype.Link, Link: DTProject},
		{Name: "site_id", Label: "Site", Column: "site_id", Type: doctype.Link, Link: DTSite},
		{Name: "amount", Label: "Amount", Column: "amount", Type: doctype.Money, ReadOnly: true}, // computed (CD-79)
		{Name: "delivery_date", Label: "Delivery date", Column: "delivery_date", Type: doctype.Date},
		{Name: "line_project_id", Label: "Line project", Column: "line_project_id", Type: doctype.Link, Link: DTProject, OnLine: true},
		{Name: "line_site_id", Label: "Line site", Column: "line_site_id", Type: doctype.Link, Link: DTSite, OnLine: true},
	}
}

// masterFields are the probe master columns as fields.
func masterFields() []doctype.Field {
	return []doctype.Field{
		{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, Importable: true},
		{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true},
		{Name: "enabled", Label: "Enabled", Column: "enabled", Type: doctype.Check},
	}
}

// masterRights is the fixture's "every master" row.
func masterRights() map[string]map[string]string {
	return map[string]map[string]string{
		"Masters Manager": all("read", "create", "write", "delete", "export", "import"),
	}
}

// docTypes returns the probe DocTypes with the seeded rights matrix of
// 00-fixtures.md (plan "Fixture mapping"). Administrator has no rows.
func docTypes() []doctype.DocType {
	orderPU := all("read", "create", "write", "submit", "print", "export")
	orderPU["delete"] = "own"
	claimEmp := map[string]string{
		"read": "team", "approve": "team",
		"create": "own", "write": "own", "submit": "own", "print": "own",
	}
	master := func(name, label, plural, table string) doctype.DocType {
		return doctype.DocType{
			Name: name, Label: label, PluralLabel: plural, Table: table,
			Importable: true, Fields: masterFields(), OwnerField: "created_by",
			DefaultRights: masterRights(), Master: masterLookup{table},
		}
	}
	site := master(DTSite, "Site", "Sites", "probe_site")
	site.SiteFields = []string{"id"}
	project := master(DTProject, "Project", "Projects", "probe_project")
	project.ProjectFields = []string{"id"}
	return []doctype.DocType{
		{
			Name: DTOrder, Label: "Purchase Order", PluralLabel: "Purchase Orders",
			Table: "probe_order", LinesTable: "probe_order_line",
			Submittable: true, Approvable: true, Fields: docFields(), OwnerField: "created_by",
			ProjectFields: []string{"project_id"}, SiteFields: []string{"site_id", "lines.line_site_id"},
			DefaultRights: map[string]map[string]string{
				"Purchase User":    orderPU,
				"Purchase Manager": all("read", "approve", "cancel", "amend", "print", "export"),
				"Director":         all("read", "approve", "print", "export"),
				"Stores":           all("read"),
			},
			Hooks: NopHooks{},
		},
		{
			Name: DTReceipt, Label: "GRN", PluralLabel: "GRNs",
			Table: "probe_receipt", LinesTable: "probe_receipt_line",
			Submittable: true, Fields: docFields(), OwnerField: "created_by",
			SiteFields: []string{"site_id", "lines.line_site_id"},
			DefaultRights: map[string]map[string]string{
				"Stores": all("read", "create", "write", "submit", "print"),
			},
			Hooks: NopHooks{},
		},
		{
			Name: DTClaim, Label: "Expense Claim", PluralLabel: "Expense Claims",
			Table: "probe_claim", LinesTable: "probe_claim_line",
			Submittable: true, Approvable: true, Fields: docFields(), OwnerField: "created_by",
			EmployeeField: "employee_id", ProjectFields: []string{"project_id"},
			DefaultRights: map[string]map[string]string{
				"Director": all("read", "approve", "print", "export"),
				"Employee": claimEmp,
			},
			Hooks: NopHooks{},
		},
		master(DTItem, "Item", "Items", "probe_item"),
		master(DTParty, "Party", "Parties", "probe_party"),
		site,
		project,
	}
}

var registerOnce sync.Once

// RegisterDocTypes registers the probe DocTypes once, skipping names that
// are already registered.
func RegisterDocTypes() {
	registerOnce.Do(func() {
		for _, dt := range docTypes() {
			if _, ok := doctype.Lookup(dt.Name); !ok {
				doctype.Register(dt)
			}
		}
	})
}
