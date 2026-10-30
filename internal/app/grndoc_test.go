package app_test

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// grnType is a document with Site links in its header and lines, on the real
// Site and Project masters: no fixture DocType has a Site (fixture.Receipt has
// neither dimension, fixture.Order a Project only), and AUTH's Site cases need
// one. It lives here because fixture can't link to a real master.
const (
	grnType = "apptest.GRN"
	grnPath = "/doc/" + grnType
)

const grnTables = `
CREATE TABLE IF NOT EXISTS apptest_grn (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    number            TEXT UNIQUE,
    state             TEXT NOT NULL DEFAULT 'draft',
    state_reason      TEXT NOT NULL DEFAULT '',
    amended_from_id   INTEGER,
    amendment_root_id INTEGER,
    amendment_no      INTEGER NOT NULL DEFAULT 0,
    version           INTEGER NOT NULL DEFAULT 1,
    created_at        TEXT NOT NULL,
    created_by        INTEGER NOT NULL,
    updated_at        TEXT NOT NULL,
    updated_by        INTEGER NOT NULL,
    site_id           INTEGER,
    project_id        INTEGER,
    receipt_date      TEXT,
    ref               TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS apptest_grn_line (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id    INTEGER NOT NULL REFERENCES apptest_grn (id) ON DELETE CASCADE,
    line_no      INTEGER NOT NULL,
    line_site_id INTEGER,
    qty          INTEGER
);`

func init() {
	doctype.Register(doctype.DocType{
		Name: grnType, Label: "GRN", PluralLabel: "GRNs",
		Table: "apptest_grn", LinesTable: "apptest_grn_line", Series: "GN/{FY}/{#####}",
		Submittable: true, OwnerField: "created_by",
		ProjectFields: []string{"project_id"},
		SiteFields:    []string{"site_id", "lines.line_site_id"},
		SearchFields:  []string{"ref"},
		DefaultRights: map[string]map[string]string{
			"Stores":           {"read": "all", "create": "all", "write": "all", "submit": "all"},
			"Purchase Manager": {"read": "all"},
			"Purchase User":    {"read": "own"},
		},
		Fields: []doctype.Field{
			{Name: "site_id", Label: "Site", Type: doctype.Link, Link: "Site", Required: true, Filterable: true, Sortable: true, ListPriority: 1},
			{Name: "project_id", Label: "Project", Type: doctype.Link, Link: "Project", ListPriority: 2},
			{Name: "receipt_date", Label: "Receipt date", Type: doctype.Date, Sortable: true, ListPriority: 3},
			{Name: "ref", Label: "Reference", Type: doctype.Text},

			{Name: "line_site_id", Label: "Delivery site", Type: doctype.Link, Link: "Site", OnLine: true, Step: "lines"},
			{Name: "qty", Label: "Quantity", Type: doctype.Qty, OnLine: true, Step: "lines"},
		},
		Hooks: grnHooks{},
	})
}

// grnHooks does nothing: these cases are about who reaches the document.
type grnHooks struct{}

func (grnHooks) Validate(context.Context, *db.Tx, int64) []doctype.FieldError { return nil }
func (grnHooks) BeforeSubmit(context.Context, *db.Tx, int64) error            { return nil }
func (grnHooks) ApplyEffects(context.Context, *db.Tx, int64) error            { return nil }
func (grnHooks) ReverseEffects(context.Context, *db.Tx, int64) error          { return nil }
func (grnHooks) Amount(context.Context, *db.Tx, int64) (types.Money, error)   { return 0, nil }
func (grnHooks) Date(context.Context, *db.Tx, int64) (types.Date, error) {
	return types.Date{}, nil
}

// migrateGRN adds grnType's tables.
func migrateGRN(ctx context.Context, d *db.DB) error {
	return d.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, grnTables)
		return err
	})
}
