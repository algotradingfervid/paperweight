// Package listtest holds the test-only DocTypes, tables, fixtures and auth
// fake shared by the list and csvio tests (WP1-NUM Task 8).
package listtest

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DocType names; registered with the package prefix (CD-65).
const (
	NamePO       = "listtest.PO"
	NameReceipt  = "listtest.Receipt"
	NameParty    = "listtest.Party"
	NameItem     = "listtest.Item"
	NameUoM      = "listtest.UoM"
	NameProject  = "listtest.Project"
	NameSite     = "listtest.Site"
	NameExpense  = "listtest.Expense"
	NameEmployee = "listtest.Employee"
)

func init() {
	for _, dt := range docTypes() {
		doctype.Register(dt)
	}
}

func docTypes() []doctype.DocType {
	f := func(name, label string, typ doctype.FieldType) doctype.Field {
		return doctype.Field{Name: name, Label: label, Type: typ}
	}
	filterable := func(fs ...doctype.Field) []doctype.Field {
		for i := range fs {
			fs[i].Filterable = true
		}
		return fs
	}
	importable := func(fs ...doctype.Field) []doctype.Field {
		for i := range fs {
			fs[i].Importable = true
		}
		return fs
	}
	sortable := func(fd doctype.Field) doctype.Field { fd.Sortable = true; return fd }
	required := func(fd doctype.Field) doctype.Field { fd.Required = true; return fd }
	prio := func(fd doctype.Field, p int) doctype.Field { fd.ListPriority = p; return fd }
	link := func(fd doctype.Field, target string) doctype.Field { fd.Link = target; return fd }
	onLine := func(fd doctype.Field) doctype.Field { fd.OnLine = true; return fd }
	column := func(fd doctype.Field, c string) doctype.Field { fd.Column = c; return fd }
	nameAsc := []doctype.SortKey{{Field: "name"}}
	dateDesc := []doctype.SortKey{{Field: "date", Desc: true}}

	status := prio(f("status", "Status", doctype.Select), 3)
	status.Options = []string{"draft", "pending_approval", "approved", "rejected", "closed", "cancelled"}
	itemType := required(f("item_type", "Type", doctype.Select))
	itemType.Options = []string{"Goods", "Service"}

	return []doctype.DocType{
		{
			Name: NamePO, Label: "Purchase Order", PluralLabel: "Purchase Orders", Table: "numt_po", LinesTable: "numt_po_line",
			Fields: filterable(
				prio(sortable(f("number", "Number", doctype.Text)), 1),
				prio(sortable(f("party_name", "Party", doctype.Text)), 5),
				status,
				prio(sortable(f("date", "Date", doctype.Date)), 4),
				link(f("project_id", "Project", doctype.Link), NameProject),
				f("branch", "Branch", doctype.Text),
				prio(sortable(f("grand_total", "Grand Total", doctype.Money)), 2),
				f("qty", "Qty", doctype.Qty),
				f("rate", "Rate", doctype.Rate),
				f("gst_rate", "GST", doctype.Percent),
				f("remarks", "Remarks", doctype.Text),
				f("created_at", "Created", doctype.Timestamp),
				onLine(link(f("item", "Item", doctype.Link), NameItem)),
				onLine(column(f("line_qty", "Qty", doctype.Qty), "qty")),
			),
			SearchFields: []string{"number", "party_name"},
			DefaultSort:  dateDesc,
			Hooks:        noHooks{},
		},
		{
			Name: NameParty, Label: "Party", PluralLabel: "Parties", Table: "numt_party",
			Fields: importable(filterable(
				prio(sortable(required(f("code", "Code", doctype.Text))), 4),
				prio(sortable(required(f("name", "Name", doctype.Text))), 1),
				f("gstin", "GSTIN", doctype.Text),
				f("state", "State", doctype.Text),
				f("pin", "PIN", doctype.Text),
				f("phone", "Phone", doctype.Text),
				f("remarks", "Remarks", doctype.Text),
				prio(f("disabled", "Disabled", doctype.Check), 3),
			)...),
			SearchFields: []string{"code", "name"},
			DefaultSort:  nameAsc,
			Master:       TableLookup("numt_party"),
		},
		{
			Name: NameItem, Label: "Item", PluralLabel: "Items", Table: "numt_item",
			Fields: importable(
				prio(required(f("code", "Code", doctype.Text)), 4),
				prio(required(f("name", "Name", doctype.Text)), 1),
				itemType,
				link(required(f("uom", "UoM", doctype.Link)), NameUoM),
				f("hsn_sac", "HSN/SAC", doctype.Text),
				f("tax_code", "Tax Code", doctype.Text),
				f("standard_rate", "Standard Rate", doctype.Rate),
				prio(f("disabled", "Disabled", doctype.Check), 3),
			),
			SearchFields: []string{"code", "name"},
			DefaultSort:  nameAsc,
			Master:       TableLookup("numt_item"),
		},
		{
			Name: NameUoM, Label: "UoM", PluralLabel: "UoMs", Table: "numt_uom",
			Fields: []doctype.Field{
				prio(f("code", "Code", doctype.Text), 4),
				prio(f("name", "Name", doctype.Text), 1),
				prio(f("disabled", "Disabled", doctype.Check), 3),
			},
			SearchFields: []string{"code", "name"},
			DefaultSort:  nameAsc,
			Master:       TableLookup("numt_uom"),
		},
		{
			Name: NameProject, Label: "Project", PluralLabel: "Projects", Table: "numt_project",
			Fields: []doctype.Field{
				prio(f("code", "Code", doctype.Text), 4),
				prio(f("name", "Name", doctype.Text), 1),
			},
			SearchFields: []string{"code", "name"},
			DefaultSort:  nameAsc,
			Master:       TableLookup("numt_project"),
		},
		{
			Name: NameReceipt, Label: "GRN", PluralLabel: "GRNs", Table: "numt_grn",
			Fields: filterable(
				sortable(f("number", "Number", doctype.Text)),
				sortable(link(f("site_id", "Site", doctype.Link), NameSite)),
				sortable(f("date", "Date", doctype.Date)),
			),
			SearchFields: []string{"number"},
			DefaultSort:  dateDesc,
			Hooks:        noHooks{},
		},
		{
			Name: NameSite, Label: "Site", PluralLabel: "Sites", Table: "numt_site",
			Fields: []doctype.Field{
				prio(f("code", "Code", doctype.Text), 4),
				prio(f("name", "Name", doctype.Text), 1),
			},
			SearchFields: []string{"code", "name"},
			DefaultSort:  nameAsc,
			Master:       TableLookup("numt_site"),
		},
		{
			Name: NameExpense, Label: "Expense Claim", PluralLabel: "Expense Claims", Table: "numt_expense",
			Fields: filterable(
				sortable(f("date", "Date", doctype.Date)),
				sortable(link(f("project_id", "Project", doctype.Link), NameProject)),
				sortable(f("amount", "Amount", doctype.Money)),
			),
			DefaultSort: dateDesc,
			Hooks:       noHooks{},
		},
		{
			Name: NameEmployee, Label: "Employee", PluralLabel: "Employees", Table: "numt_employee",
			Fields: importable(
				prio(required(f("code", "Code", doctype.Text)), 4),
				prio(required(f("name", "Name", doctype.Text)), 1),
				f("date_of_joining", "Date of Joining", doctype.Date),
			),
			SearchFields: []string{"code", "name"},
			DefaultSort:  nameAsc,
			Master:       TableLookup("numt_employee"),
		},
	}
}

func mustLookup(name string) *doctype.DocType {
	dt, ok := doctype.Lookup(name)
	if !ok {
		panic("listtest: DocType not registered: " + name)
	}
	return dt
}

func PO() *doctype.DocType       { return mustLookup(NamePO) }
func Receipt() *doctype.DocType  { return mustLookup(NameReceipt) }
func Party() *doctype.DocType    { return mustLookup(NameParty) }
func Item() *doctype.DocType     { return mustLookup(NameItem) }
func UoM() *doctype.DocType      { return mustLookup(NameUoM) }
func Project() *doctype.DocType  { return mustLookup(NameProject) }
func Site() *doctype.DocType     { return mustLookup(NameSite) }
func Expense() *doctype.DocType  { return mustLookup(NameExpense) }
func Employee() *doctype.DocType { return mustLookup(NameEmployee) }

// TableLookup is a MasterLookup over a numt_ master table (CD-21). Every
// master table has code, name and disabled columns.
func TableLookup(table string) doctype.MasterLookup { return tableLookup(table) }

type tableLookup string

func (t tableLookup) Lookup(ctx context.Context, tx *db.Tx, id int64) (doctype.MasterInfo, bool, error) {
	var mi doctype.MasterInfo
	err := tx.SQL().QueryRowContext(ctx,
		"SELECT code, name, disabled FROM "+string(t)+" WHERE id = ?", id).
		Scan(&mi.Code, &mi.Name, &mi.Disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return doctype.MasterInfo{}, false, nil
	}
	if err != nil {
		return doctype.MasterInfo{}, false, err
	}
	return mi, true, nil
}

// noHooks is the no-op Hooks of the listtest documents.
type noHooks struct{}

func (noHooks) Validate(context.Context, *db.Tx, int64) []doctype.FieldError { return nil }
func (noHooks) BeforeSubmit(context.Context, *db.Tx, int64) error            { return nil }
func (noHooks) ApplyEffects(context.Context, *db.Tx, int64) error            { return nil }
func (noHooks) ReverseEffects(context.Context, *db.Tx, int64) error          { return nil }
func (noHooks) Amount(context.Context, *db.Tx, int64) (types.Money, error)   { return 0, nil }
func (noHooks) Date(context.Context, *db.Tx, int64) (types.Date, error) {
	return types.Date{}, nil
}
