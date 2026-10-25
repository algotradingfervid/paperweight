package approvaltest

import (
	"context"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Fixture DocType names (00-fixtures.md).
const (
	Order      = "fixture.Order"
	Receipt    = "fixture.Receipt"
	Claim      = "fixture.Claim"
	ItemMaster = "Item"
)

// noMasters marks Item as a master; the harness never looks masters up.
type noMasters struct{}

func (noMasters) Lookup(ctx context.Context, tx *db.Tx, id int64) (doctype.MasterInfo, bool, error) {
	return doctype.MasterInfo{}, false, nil
}

// docTypes are test descriptors, sorted by name like doctype.All. They are
// never passed to doctype.Register: internal/fixture registers the real ones,
// and duplicates panic.
var docTypes = []*doctype.DocType{
	{Name: Order, Label: "Purchase Order", Table: docsTable, Submittable: true, Approvable: true},
	{Name: Claim, Label: "Expense Claim", Table: docsTable, Submittable: true, Approvable: true, EmployeeField: "employee_id"},
	{Name: Receipt, Label: "Goods Receipt Note", Table: docsTable, Submittable: true, Approvable: true},
	{Name: ItemMaster, Label: "Item", Master: noMasters{}},
}

// DocTypes returns the test descriptors (Deps.All in the harness).
func DocTypes() []*doctype.DocType { return slices.Clone(docTypes) }

// Lookup finds a test descriptor by name (Deps.Lookup in the harness).
func Lookup(name string) (*doctype.DocType, bool) {
	for _, dt := range docTypes {
		if dt.Name == name {
			return dt, true
		}
	}
	return nil, false
}
