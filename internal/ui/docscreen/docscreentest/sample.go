// Package docscreentest holds docscreen's test doubles: the sample DocType
// and its masters, an in-memory Lister and Extras, a Reader and a seed
// helper over docfake.
package docscreentest

import (
	"context"
	"sync"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Names of the test DocTypes (CD-65 prefix).
const (
	Sample   = "docscreentest.Sample"
	Supplier = "docscreentest.Supplier"
	Item     = "docscreentest.Item"
)

var (
	once    sync.Once
	masters = map[string]*masterLookup{Supplier: newMasterLookup(), Item: newMasterLookup()}
)

// RegisterSample registers the sample DocType and its two masters, once.
func RegisterSample() {
	once.Do(func() {
		doctype.Register(doctype.DocType{Name: Supplier, Label: "Supplier", PluralLabel: "Suppliers", Table: "docscreentest_supplier", Master: masters[Supplier]})
		doctype.Register(doctype.DocType{Name: Item, Label: "Item", PluralLabel: "Items", Table: "docscreentest_item", Master: masters[Item]})
		doctype.Register(doctype.DocType{
			Name: Sample, Label: "Sample Order", PluralLabel: "Sample Orders",
			Table: "docscreentest_sample", LinesTable: "docscreentest_sample_line",
			Submittable: true, Approvable: true, Printable: true,
			Fields: []doctype.Field{
				{Name: "supplier_id", Label: "Supplier", Type: doctype.Link, Link: Supplier, Required: true},
				{Name: "order_date", Label: "Order date", Type: doctype.Date, Required: true, Sortable: true, ListPriority: 4},
				{Name: "priority", Label: "Priority", Type: doctype.Select, Options: []string{"Normal", "Urgent"}},
				{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 1000, Hint: "Shown on the printed order."},
				{Name: "amount", Label: "Amount", Type: doctype.Money, Sortable: true, ListPriority: 2, ReadOnly: true},
				{Name: "item_id", Label: "Item", Type: doctype.Link, Link: Item, Required: true, OnLine: true},
				{Name: "qty", Label: "Quantity", Type: doctype.Qty, Required: true, OnLine: true},
				{Name: "rate", Label: "Rate", Type: doctype.Rate, OnLine: true},
			},
		})
	})
}

// SetMaster stores what the master lookup of Supplier or Item returns for id.
func SetMaster(name string, id int64, mi doctype.MasterInfo) {
	m := masters[name]
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[id] = mi
}

type masterLookup struct {
	mu   sync.Mutex
	rows map[int64]doctype.MasterInfo
}

func newMasterLookup() *masterLookup {
	return &masterLookup{rows: map[int64]doctype.MasterInfo{}}
}

func (m *masterLookup) Lookup(_ context.Context, _ *db.Tx, id int64) (doctype.MasterInfo, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mi, ok := m.rows[id]
	return mi, ok, nil
}
