package docscreentest

import (
	"context"
	"sync"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Hooked is the test DocType the module hook tests run on (hooks spec §6),
// on a fixture database: a header step, a terms step and lines whose first
// Link is a project, not the item.
const Hooked = "docscreentest.Hooked"

// HookedSchema creates Hooked's two tables.
var HookedSchema = []string{
	`CREATE TABLE docscreentest_hooked (id INTEGER PRIMARY KEY AUTOINCREMENT, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER, amendment_no INTEGER NOT NULL DEFAULT 0,
		version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
		supplier_id INTEGER, site_id INTEGER, ref TEXT NOT NULL DEFAULT '', remarks TEXT NOT NULL DEFAULT '')`,
	`CREATE TABLE docscreentest_hooked_line (id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL,
		project_id INTEGER, item_id INTEGER, qty INTEGER, note TEXT NOT NULL DEFAULT '')`,
}

var hookedOnce sync.Once

// RegisterHooked registers Hooked, once. Its links name the fixture masters
// and the Site master by string, so this package imports neither
// internal/fixture nor internal/masters/site.
func RegisterHooked() {
	hookedOnce.Do(func() {
		doctype.Register(doctype.DocType{
			Name: Hooked, Label: "Hooked Order", PluralLabel: "Hooked Orders",
			Table: "docscreentest_hooked", LinesTable: "docscreentest_hooked_line",
			Submittable: true, Hooks: hookedHooks{}, SiteFields: []string{"site_id"},
			Fields: []doctype.Field{
				{Name: "supplier_id", Label: "Supplier", Type: doctype.Link, Link: "fixture.Party", Required: true},
				{Name: "site_id", Label: "Site", Type: doctype.Link, Link: "Site"},
				{Name: "ref", Label: "Reference", Type: doctype.Text, MaxLen: 12, Step: "terms"},
				{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500, Step: "terms"},
				{Name: "project_id", Label: "Project", Type: doctype.Link, Link: "fixture.Project", OnLine: true},
				{Name: "item_id", Label: "Item", Type: doctype.Link, Link: "fixture.Item", Required: true, OnLine: true},
				{Name: "qty", Label: "Quantity", Type: doctype.Qty, Positive: true, OnLine: true},
				{Name: "note", Label: "Note", Type: doctype.Text, OnLine: true},
			},
		})
	})
}

// hookedHooks are Hooked's Hooks: the date is fixed, the rest are no-ops.
type hookedHooks struct{}

func (hookedHooks) Validate(context.Context, *db.Tx, int64) []doctype.FieldError { return nil }
func (hookedHooks) BeforeSubmit(context.Context, *db.Tx, int64) error            { return nil }
func (hookedHooks) ApplyEffects(context.Context, *db.Tx, int64) error            { return nil }
func (hookedHooks) ReverseEffects(context.Context, *db.Tx, int64) error          { return nil }
func (hookedHooks) Amount(context.Context, *db.Tx, int64) (types.Money, error)   { return 0, nil }

func (hookedHooks) Date(context.Context, *db.Tx, int64) (types.Date, error) {
	return types.Date{Year: 2026, Month: time.October, Day: 24}, nil
}
