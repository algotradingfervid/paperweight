package goodsreceipt_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// g1TS is a stored timestamp at authtest.T0.
const g1TS = "2026-10-24T05:00:00.000Z"

var g1Autoincrement = regexp.MustCompile(`(?is)\bid\s+INTEGER\s+PRIMARY\s+KEY\s+AUTOINCREMENT\b`)

// g1DocType returns the registered goodsreceipt.Receipt or stops the test.
func g1DocType(t *testing.T) *doctype.DocType {
	t.Helper()
	if goodsreceipt.DocType != "goodsreceipt.Receipt" {
		t.Fatalf("goodsreceipt.DocType = %q, want goodsreceipt.Receipt (GRN spec §3)", goodsreceipt.DocType)
	}
	dt, ok := doctype.Lookup(goodsreceipt.DocType)
	if !ok {
		t.Fatalf("doctype.Lookup(%q): not registered", goodsreceipt.DocType)
	}
	return dt
}

// g1Field returns a declared field of the GRN or stops the test.
func g1Field(t *testing.T, dt *doctype.DocType, name string) doctype.Field {
	t.Helper()
	f, ok := dt.Field(name)
	if !ok {
		t.Fatalf("%s has no field %q", dt.Name, name)
	}
	return f
}

// g1TableSQL is a table's CREATE statement, or "" when it doesn't exist.
func g1TableSQL(t *testing.T, d *db.DB, table string) string {
	t.Helper()
	var s string
	err := d.SQL().QueryRowContext(t.Context(), `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&s)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("read the schema of %s: %v", table, err)
	}
	return s
}

// g1Columns maps each column of table to whether it is NOT NULL.
func g1Columns(t *testing.T, d *db.DB, table string) map[string]bool {
	t.Helper()
	cols := map[string]bool{}
	rows, err := d.SQL().QueryContext(t.Context(), `SELECT name, "notnull" FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("columns of %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		var notNull bool
		if err := rows.Scan(&name, &notNull); err != nil {
			t.Fatal(err)
		}
		cols[name] = notNull
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return cols
}

// g1HasFK reports whether table has a foreign key from column to parent(id)
// with the given ON DELETE action ("" accepts any).
func g1HasFK(t *testing.T, d *db.DB, table, column, parent, onDelete string) bool {
	t.Helper()
	rows, err := d.SQL().QueryContext(t.Context(), `SELECT "table", "from", "to", on_delete FROM pragma_foreign_key_list(?)`, table)
	if err != nil {
		t.Fatalf("foreign keys of %s: %v", table, err)
	}
	defer func() { _ = rows.Close() }()
	found := false
	for rows.Next() {
		var p, from, to, del string
		if err := rows.Scan(&p, &from, &to, &del); err != nil {
			t.Fatal(err)
		}
		if p == parent && from == column && to == "id" && (onDelete == "" || del == onDelete) {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}

// g1Count counts table's rows matching where (args bound).
func g1Count(t *testing.T, d *db.DB, table, where string, args ...any) int {
	t.Helper()
	var n int
	if err := d.SQL().QueryRowContext(t.Context(), `SELECT count(*) FROM `+table+` WHERE `+where, args...).Scan(&n); err != nil { //nolint:gosec // G202: fixed table names
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// g1MasterID is the id, as a form value, of the master row with code in table.
func g1MasterID(t *testing.T, d *db.DB, table, code string) string {
	t.Helper()
	var id int64
	if err := d.SQL().QueryRowContext(t.Context(), `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&id); err != nil { //nolint:gosec // G202: fixed table names
		t.Fatalf("id of %s %s: %v", table, code, err)
	}
	return strconv.FormatInt(id, 10)
}

// g1Int is a stored Link or Int value as a form value.
func g1Int(t *testing.T, v any) string {
	t.Helper()
	n, ok := v.(int64)
	if !ok {
		t.Fatalf("stored value %v (%T), want an int64", v, v)
	}
	return strconv.FormatInt(n, 10)
}

// g1Line is one GRN line to post: the index of the PO line it receives
// against, and its quantities as form values.
type g1Line struct {
	poLine             int
	received, rejected string
	reason             string
}

// g1Receipt saves a GRN as Suresh through the engine on po, with the posted
// site and project copied from the PO (GRN spec §3.1), today's receipt date,
// a delivery challan and one line per l, then submits it when stop is
// approved (no approval rule in grntest, so submit approves). It returns the
// stored document. The values are valid under the whole spec (§3.1, §3.2,
// §4), so later tasks' Validate and BeforeSubmit accept them too.
func g1Receipt(t *testing.T, env *grntest.Env, po doc.Doc, stop doctype.State, lines ...g1Line) doc.Doc {
	t.Helper()
	ctx := context.Background()
	in := doc.ActionInput{Values: map[string]string{
		"purchase_order_id": strconv.FormatInt(po.ID, 10),
		"site_id":           g1Int(t, po.Header["site_id"]),
		"project_id":        g1Int(t, po.Header["project_id"]),
		"receipt_date":      types.Today(env.Clock).ISO(),
		"challan_kind":      "Delivery challan",
		"challan_no":        "DC-1042",
	}}
	for _, l := range lines {
		m := map[string]string{
			"po_line_id":   strconv.FormatInt(po.Lines[l.poLine].ID, 10),
			"received_qty": l.received,
		}
		if l.rejected != "" {
			m["rejected_qty"] = l.rejected
			m["reject_reason"] = l.reason
		}
		in.Lines = append(in.Lines, m)
	}
	res, err := env.Engine.Do(ctx, env.Suresh, goodsreceipt.DocType, 0, doctype.ActionSave, in)
	if err != nil {
		t.Fatalf("save GRN as Suresh: %v %+v", err, g1FieldErrors(err))
	}
	if stop == doctype.StateApproved {
		res, err = env.Engine.Do(ctx, env.Suresh, goodsreceipt.DocType, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version})
		if err != nil {
			t.Fatalf("submit GRN %d as Suresh: %v %+v", res.ID, err, g1FieldErrors(err))
		}
	}
	if res.State != stop {
		t.Fatalf("GRN %d is %s, want %s", res.ID, res.State, stop)
	}
	return g1Load(t, env, res.ID)
}

// g1Load reads a stored GRN.
func g1Load(t *testing.T, env *grntest.Env, id int64) doc.Doc {
	t.Helper()
	var out doc.Doc
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		out, err = doc.LoadDoc(t.Context(), tx, goodsreceipt.DocType, id)
		return err
	}); err != nil {
		t.Fatalf("load GRN %d: %v", id, err)
	}
	return out
}

// g1FieldErrors are err's field errors, for a failure message.
func g1FieldErrors(err error) []doctype.FieldError {
	var he doctype.HTTPError
	if errors.As(err, &he) {
		return he.FieldErrors()
	}
	return nil
}

// g1TwoLinePO is an approved PO by Priya on METRO-SITE / PRJ-METRO with two
// lines: 100 kg MS-ANGLE-50 and 100 m CABLE-4C.
func g1TwoLinePO(t *testing.T, env *grntest.Env) doc.Doc {
	t.Helper()
	return purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(
		map[string]string{},
		map[string]string{"item_id": g1MasterID(t, env.DB, "item", "CABLE-4C"), "uom_id": g1MasterID(t, env.DB, "uom", "MTR"), "qty": "100"},
	))
}

// MOD-GRN G1 schema tests (GRN spec §3, §3.3; CD-156): goods_receipt,
// goods_receipt_line and grn_bill ids are INTEGER PRIMARY KEY AUTOINCREMENT,
// so a deleted newest id is never handed out again: through the engine for
// a GRN draft (Stores deletes its own), and for a bill row.
func TestGRN_Schema_Autoincrement(t *testing.T) {
	d := testkit.NewDB(t)
	for _, table := range []string{"goods_receipt", "goods_receipt_line", "grn_bill"} {
		s := g1TableSQL(t, d, table)
		if s == "" {
			t.Errorf("table %s doesn't exist", table)
			continue
		}
		if !g1Autoincrement.MatchString(s) {
			t.Errorf("%s: id is not INTEGER PRIMARY KEY AUTOINCREMENT (CD-156):\n%s", table, s)
		}
	}

	env := grntest.New(t)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved())
	first := g1Receipt(t, env, po, doctype.StateDraft, g1Line{poLine: 0, received: "10"})
	newest := g1Receipt(t, env, po, doctype.StateDraft, g1Line{poLine: 0, received: "10"})
	if _, err := env.Engine.Do(t.Context(), env.Suresh, goodsreceipt.DocType, newest.ID, doctype.ActionDelete, doc.ActionInput{Version: newest.Version}); err != nil {
		t.Fatalf("delete GRN draft %d as Suresh: %v", newest.ID, err)
	}
	if next := g1Receipt(t, env, po, doctype.StateDraft, g1Line{poLine: 0, received: "10"}); next.ID <= newest.ID {
		t.Errorf("GRN draft after deleting id %d got id %d, want > %d (CD-156)", newest.ID, next.ID, newest.ID)
	}

	// grn_bill: the schema test writes the row with SQL (bill details are
	// G7's); a re-inserted bill for the same GRN gets a new id.
	insertBill := func() int64 {
		t.Helper()
		var id int64
		if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
			return tx.SQL().QueryRowContext(t.Context(),
				`INSERT INTO grn_bill (grn_id, invoice_no, invoice_date, invoice_amount, version, created_at, created_by, updated_at, updated_by)
				 VALUES (?, 'INV-77', '2026-10-24', 0, 1, ?, ?, ?, ?) RETURNING id`,
				first.ID, g1TS, env.Suresh.ID, g1TS, env.Suresh.ID).Scan(&id)
		}); err != nil {
			t.Fatalf("insert grn_bill: %v", err)
		}
		return id
	}
	bill := insertBill()
	if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(), `DELETE FROM grn_bill WHERE id = ?`, bill)
		return err
	}); err != nil {
		t.Fatalf("delete grn_bill %d: %v", bill, err)
	}
	if again := insertBill(); again <= bill {
		t.Errorf("grn_bill after deleting id %d got id %d, want > %d (CD-156)", bill, again, bill)
	}
}

// MOD-GRN G1 schema tests (GRN spec §3; CD-161): goods_receipt_line has
// NOT NULL parent_id REFERENCES goods_receipt (id) ON DELETE CASCADE and
// NOT NULL line_no. Lines saved through the engine are numbered from 1, and
// deleting a draft removes its lines and leaves another draft's.
func TestGRN_Schema_LinesCascadeAndLineNo(t *testing.T) {
	d := testkit.NewDB(t)
	if !g1HasFK(t, d, "goods_receipt_line", "parent_id", "goods_receipt", "CASCADE") {
		t.Errorf("goods_receipt_line.parent_id: no REFERENCES goods_receipt (id) ON DELETE CASCADE (CD-161)")
	}
	cols := g1Columns(t, d, "goods_receipt_line")
	for _, col := range []string{"parent_id", "line_no"} {
		if notNull, ok := cols[col]; !ok || !notNull {
			t.Errorf("goods_receipt_line.%s: present %v, NOT NULL %v; want a NOT NULL column (CD-161)", col, ok, notNull)
		}
	}

	env := grntest.New(t)
	po := g1TwoLinePO(t, env)
	grn := g1Receipt(t, env, po, doctype.StateDraft, g1Line{poLine: 0, received: "10"}, g1Line{poLine: 1, received: "20"})
	keep := g1Receipt(t, env, po, doctype.StateDraft, g1Line{poLine: 0, received: "5"})
	if len(grn.Lines) != 2 || grn.Lines[0].LineNo != 1 || grn.Lines[1].LineNo != 2 {
		t.Fatalf("GRN lines = %+v, want two, line_no 1 and 2", grn.Lines)
	}
	if _, err := env.Engine.Do(t.Context(), env.Suresh, goodsreceipt.DocType, grn.ID, doctype.ActionDelete, doc.ActionInput{Version: grn.Version}); err != nil {
		t.Fatalf("delete GRN draft %d as Suresh: %v", grn.ID, err)
	}
	if got := g1Count(t, env.DB, "goods_receipt_line", "parent_id = ?", grn.ID); got != 0 {
		t.Errorf("goods_receipt_line rows of deleted draft %d = %d, want 0 (ON DELETE CASCADE)", grn.ID, got)
	}
	if got := g1Count(t, env.DB, "goods_receipt_line", "parent_id = ?", keep.ID); got != 1 {
		t.Errorf("goods_receipt_line rows of the other draft %d = %d, want 1", keep.ID, got)
	}
}

// MOD-GRN G1 schema tests (GRN spec §3.3; CD-234, CD-156): grn_bill has one
// row per GRN: grn_id is NOT NULL, UNIQUE and REFERENCES goods_receipt (id),
// and the table carries version and the audit columns. A second bill row for
// the same GRN is refused by the database.
func TestGRN_Schema_BillUnique(t *testing.T) {
	d := testkit.NewDB(t)
	cols := g1Columns(t, d, "grn_bill")
	for _, col := range []string{"id", "grn_id", "invoice_no", "invoice_date", "invoice_amount", "version", "created_at", "created_by", "updated_at", "updated_by"} {
		if _, ok := cols[col]; !ok {
			t.Errorf("grn_bill has no column %s (GRN plan G1 Files)", col)
		}
	}
	if !cols["grn_id"] {
		t.Errorf("grn_bill.grn_id is nullable, want NOT NULL")
	}
	if !g1HasFK(t, d, "grn_bill", "grn_id", "goods_receipt", "") {
		t.Errorf("grn_bill.grn_id: no REFERENCES goods_receipt (id)")
	}

	// A unique index whose only column is grn_id.
	unique := false
	rows, err := d.SQL().QueryContext(t.Context(), `SELECT il.name FROM pragma_index_list('grn_bill') il WHERE il."unique" = 1`)
	if err != nil {
		t.Fatalf("indexes of grn_bill: %v", err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	for _, n := range names {
		var cs []string
		r, err := d.SQL().QueryContext(t.Context(), `SELECT name FROM pragma_index_info(?)`, n)
		if err != nil {
			t.Fatalf("columns of index %s: %v", n, err)
		}
		for r.Next() {
			var c string
			if err := r.Scan(&c); err != nil {
				t.Fatal(err)
			}
			cs = append(cs, c)
		}
		_ = r.Close()
		if slices.Equal(cs, []string{"grn_id"}) {
			unique = true
		}
	}
	if !unique {
		t.Errorf("grn_bill: no UNIQUE index on grn_id alone (unique indexes %v)", names)
	}

	// Through the database: a second row for one GRN is refused.
	env := grntest.New(t)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved())
	grn := g1Receipt(t, env, po, doctype.StateApproved, g1Line{poLine: 0, received: "10"})
	insert := func(no string) error {
		return env.DB.Write(t.Context(), func(tx *db.Tx) error {
			_, err := tx.SQL().ExecContext(t.Context(),
				`INSERT INTO grn_bill (grn_id, invoice_no, invoice_date, invoice_amount, version, created_at, created_by, updated_at, updated_by)
				 VALUES (?, ?, '2026-10-24', 0, 1, ?, ?, ?, ?)`,
				grn.ID, no, g1TS, env.Suresh.ID, g1TS, env.Suresh.ID)
			return err
		})
	}
	if err := insert("INV-1"); err != nil {
		t.Fatalf("first grn_bill row: %v", err)
	}
	if err := insert("INV-2"); err == nil {
		t.Errorf("second grn_bill row for GRN %d: err = nil, want a UNIQUE refusal (GRN spec §3.3)", grn.ID)
	}
	if got := g1Count(t, env.DB, "grn_bill", "grn_id = ?", grn.ID); got != 1 {
		t.Errorf("grn_bill rows for GRN %d = %d, want 1", grn.ID, got)
	}
}

// MOD-GRN G1 schema tests (GRN spec §6; CD-113; 00-fixtures.md Numbering):
// the series is GR/{FY}/{#####}, a valid template whose first number in FY
// 2026-27 is GR/26-27/00001, 14 characters, and the first approved GRN in
// grntest gets exactly that number.
func TestGRN_Schema_SeriesWidth(t *testing.T) {
	dt := g1DocType(t)
	if dt.Series != "GR/{FY}/{#####}" {
		t.Errorf("Series = %q, want GR/{FY}/{#####} (GRN spec §6)", dt.Series)
	}
	n := naming.New(testkit.NewDB(t), testkit.Clock(), nil, nil)
	if err := n.ValidateTemplate(dt.Series); err != nil {
		t.Errorf("series %q: ValidateTemplate = %v, want nil", dt.Series, err)
	}
	got, err := n.PreviewTemplate(t.Context(), dt.Series, types.Date{Year: 2026, Month: 10, Day: 24})
	got, _, _ = strings.Cut(got, ", ") // PreviewTemplate lists the next three numbers
	if err != nil || got != "GR/26-27/00001" || len(got) != 14 {
		t.Errorf("series %q renders %q (%d characters), %v; want GR/26-27/00001, 14 characters (CD-113)", dt.Series, got, len(got), err)
	}

	env := grntest.New(t)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved())
	grn := g1Receipt(t, env, po, doctype.StateApproved, g1Line{poLine: 0, received: "10"})
	if grn.Number != "GR/26-27/00001" {
		t.Errorf("first approved GRN number = %q, want GR/26-27/00001", grn.Number)
	}
}

// MOD-GRN G1 Do 1 (GRN spec §3; CD-156, CD-161, CD-243, CD-255, CD-277 (1)):
// doctype.CheckRegistry accepts the registry with goodsreceipt.Receipt in it,
// and the DocType is registered as spec §3 describes: attachments on, close
// not offered (CloseRight doctype.CloseNone), hooks set.
func TestGRN_Registry_Accepts(t *testing.T) {
	dt := g1DocType(t)
	if err := doctype.CheckRegistry(); err != nil {
		t.Errorf("CheckRegistry() = %v, want nil with goodsreceipt.Receipt registered", err)
	}
	type shape struct {
		Label, PluralLabel, Table, LinesTable, Series, OwnerField string
		Submittable, Approvable, Printable, Attachable            bool
		ProjectFields, SiteFields                                 []string
		CloseRight                                                string
	}
	got := shape{
		Label: dt.Label, PluralLabel: dt.PluralLabel, Table: dt.Table, LinesTable: dt.LinesTable,
		Series: dt.Series, OwnerField: dt.OwnerField,
		Submittable: dt.Submittable, Approvable: dt.Approvable, Printable: dt.Printable, Attachable: dt.Attachable,
		ProjectFields: dt.ProjectFields, SiteFields: dt.SiteFields, CloseRight: dt.CloseRight,
	}
	want := shape{
		Label: "Goods Receipt", PluralLabel: "Goods Receipts", Table: "goods_receipt", LinesTable: "goods_receipt_line",
		Series: "GR/{FY}/{#####}", OwnerField: "created_by",
		Submittable: true, Approvable: true, Printable: true, Attachable: true,
		ProjectFields: []string{"project_id"}, SiteFields: []string{"site_id"}, CloseRight: doctype.CloseNone,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s registered as %+v\nwant %+v", dt.Name, got, want)
	}
	if dt.Hooks == nil {
		t.Errorf("%s Hooks = nil, want the goodsreceipt hooks", dt.Name)
	}
}

// MOD-GRN Global constraints "Posted, not System" (GRN spec §3.1, §3.2;
// CD-247 (1)): site_id and project_id are plain Int header fields and
// po_line_id a plain Int line field, none ReadOnly or System, because create
// checks rights on the posted header and checkReadOnly refuses a posted
// read-only value on create. purchase_order_id is a Required header Link to
// purchase.Order with no Step, as is receipt_date's Date (both on details).
func TestGRN_Fields_PostedNotReadOnly(t *testing.T) {
	dt := g1DocType(t)
	for _, c := range []struct {
		name string
		line bool
	}{
		{"site_id", false},
		{"project_id", false},
		{"po_line_id", true},
	} {
		f := g1Field(t, dt, c.name)
		if f.Type != doctype.Int || f.OnLine != c.line || f.ReadOnly || f.System {
			t.Errorf("%s: Type %v OnLine %v ReadOnly %v System %v; want Int, OnLine %v, not ReadOnly, not System (CD-247 (1))",
				c.name, f.Type, f.OnLine, f.ReadOnly, f.System, c.line)
		}
	}

	po := g1Field(t, dt, "purchase_order_id")
	if po.Type != doctype.Link || po.Link != purchase.DocType || !po.Required || po.OnLine || po.Step != "" || po.ReadOnly || po.System {
		t.Errorf("purchase_order_id = %+v; want a Required header Link to %s, no Step, not ReadOnly or System (GRN spec §3.1)", po, purchase.DocType)
	}
	rd := g1Field(t, dt, "receipt_date")
	if rd.Type != doctype.Date || rd.OnLine || rd.Step != "" {
		t.Errorf("receipt_date Type %v OnLine %v Step %q; want a header Date with no Step (GRN plan G1 Do 1)", rd.Type, rd.OnLine, rd.Step)
	}
}

// MOD-GRN Global constraints "System and ReadOnly" and "ReadOnly computed"
// (GRN spec §3.1, §3.2; CD-79, CD-144): vendor_id (Link Party) and line
// uom_id (Link UoM) are System and ReadOnly, copied in Validate; the
// computed accepted_value (header Money) and accepted_qty (line Qty) are
// ReadOnly; the convention columns number and state are ReadOnly fields.
func TestGRN_Fields_SystemReadOnly(t *testing.T) {
	dt := g1DocType(t)
	for _, c := range []struct {
		name string
		link string
		line bool
	}{
		{"vendor_id", party.DocType, false},
		{"uom_id", uom.DocType, true},
	} {
		f := g1Field(t, dt, c.name)
		if f.Type != doctype.Link || f.Link != c.link || f.OnLine != c.line || !f.System || !f.ReadOnly {
			t.Errorf("%s: Type %v Link %q OnLine %v System %v ReadOnly %v; want Link %q, OnLine %v, System and ReadOnly (GRN spec §3.1, §3.2)",
				c.name, f.Type, f.Link, f.OnLine, f.System, f.ReadOnly, c.link, c.line)
		}
	}
	for _, c := range []struct {
		name string
		typ  doctype.FieldType
		line bool
	}{
		{"accepted_value", doctype.Money, false},
		{"accepted_qty", doctype.Qty, true},
	} {
		f := g1Field(t, dt, c.name)
		if f.Type != c.typ || f.OnLine != c.line || !f.ReadOnly {
			t.Errorf("%s: Type %v OnLine %v ReadOnly %v; want Type %v, OnLine %v, ReadOnly (computed by Validate)", c.name, f.Type, f.OnLine, f.ReadOnly, c.typ, c.line)
		}
	}
	for _, name := range []string{"number", "state"} {
		if f := g1Field(t, dt, name); !f.ReadOnly || f.OnLine {
			t.Errorf("%s: ReadOnly %v OnLine %v; want a ReadOnly header field (CD-144)", name, f.ReadOnly, f.OnLine)
		}
	}
}

// MOD-GRN G1 Do 1 (GRN spec §3.1, §3.2; CD-175, CD-282 Q7): the delivery
// step's fields carry Step "delivery"; challan_kind is a Select whose
// Options are exactly the display texts; rest_not_coming is a Check; the
// MaxLen values; received_qty Required and Positive, rejected_qty
// NonNegative with default 0, reject_reason a line Text.
func TestGRN_Fields_DeliveryStep(t *testing.T) {
	dt := g1DocType(t)
	for _, name := range []string{"challan_kind", "challan_no", "challan_date", "vehicle_no", "transporter", "eway_bill_no", "rest_not_coming", "remarks"} {
		if f := g1Field(t, dt, name); f.Step != "delivery" || f.OnLine {
			t.Errorf("%s: Step %q OnLine %v; want header Step delivery (CD-282 Q7)", name, f.Step, f.OnLine)
		}
	}
	kind := g1Field(t, dt, "challan_kind")
	if kind.Type != doctype.Select || !slices.Equal(kind.Options, []string{"Delivery challan", "Tax invoice"}) {
		t.Errorf("challan_kind Type %v Options %q; want Select with exactly [Delivery challan Tax invoice] (CD-175)", kind.Type, kind.Options)
	}
	if f := g1Field(t, dt, "rest_not_coming"); f.Type != doctype.Check {
		t.Errorf("rest_not_coming Type %v, want Check", f.Type)
	}
	if f := g1Field(t, dt, "challan_date"); f.Type != doctype.Date {
		t.Errorf("challan_date Type %v, want Date", f.Type)
	}
	for _, c := range []struct {
		name string
		max  int
		line bool
	}{
		{"challan_no", 40, false},
		{"vehicle_no", 20, false},
		{"transporter", 140, false},
		{"eway_bill_no", 20, false},
		{"remarks", 500, false},
		{"reject_reason", 200, true},
	} {
		if f := g1Field(t, dt, c.name); f.Type != doctype.Text || f.MaxLen != c.max || f.OnLine != c.line {
			t.Errorf("%s: Type %v MaxLen %d OnLine %v; want Text, MaxLen %d, OnLine %v (GRN plan G1 Do 1)", c.name, f.Type, f.MaxLen, f.OnLine, c.max, c.line)
		}
	}
	if f := g1Field(t, dt, "received_qty"); f.Type != doctype.Qty || !f.OnLine || !f.Required || !f.Positive || f.ReadOnly {
		t.Errorf("received_qty = %+v; want a Required, Positive line Qty", f)
	}
	if f := g1Field(t, dt, "rejected_qty"); f.Type != doctype.Qty || !f.OnLine || !f.NonNegative || f.Default != "0" || f.ReadOnly {
		t.Errorf("rejected_qty = %+v; want a NonNegative line Qty with Default 0", f)
	}
}

// MOD-GRN G1 Do 1 (GRN spec §3; CD-70): two document links, the header
// purchase_order_id to purchase.Order (so CD-70 runs CheckLinkTx, AUTH-101)
// and the line po_line_id to purchase.Order lines (DOC-017, DOC-038,
// DOC-052), and one RollUp: the PO line's received_qty is the sum of
// accepted_qty joined on po_line_id.
func TestGRN_Links_RollUp(t *testing.T) {
	dt := g1DocType(t)
	want := []doctype.LinkSpec{
		{Field: "purchase_order_id", Target: purchase.DocType},
		{Field: "po_line_id", Target: purchase.DocType, OnLine: true},
	}
	got := slices.Clone(dt.Links)
	byField := func(a, b doctype.LinkSpec) int { return strings.Compare(a.Field, b.Field) }
	slices.SortFunc(got, byField)
	slices.SortFunc(want, byField)
	if !slices.Equal(got, want) {
		t.Errorf("Links = %+v, want %+v (GRN spec §3)", dt.Links, want)
	}
	wantRollUps := []doctype.RollUp{{TargetDocType: purchase.DocType, TargetField: "received_qty", SourceField: "accepted_qty", JoinField: "po_line_id"}}
	if !slices.Equal(dt.RollUps, wantRollUps) {
		t.Errorf("RollUps = %+v, want %+v (GRN spec §3)", dt.RollUps, wantRollUps)
	}
}

// MOD-GRN G1 Files (GRN spec §3.1, §3.2; CD-279 (5), CD-303): vendor_id,
// purchase_order_id, line uom_id and every snapshot column are nullable,
// because create inserts before Validate runs and System fields are left out
// of the insert. The header and line snapshot columns are the plan's,
// snap_is_service included; there is no snap_po_id (dropped by CD-303).
func TestGRN_Schema_Nullable(t *testing.T) {
	d := testkit.NewDB(t)
	for _, c := range []struct {
		table string
		cols  []string
	}{
		{"goods_receipt", []string{
			"vendor_id", "purchase_order_id",
			"snap_vendor_name", "snap_vendor_gstin", "snap_vendor_address", "snap_po_number", "snap_po_date",
			"snap_site_name", "snap_project_code", "snap_branch_id",
		}},
		{"goods_receipt_line", []string{
			"uom_id",
			"snap_item_code", "snap_item_name", "snap_hsn_sac", "snap_uom_code", "snap_rate", "snap_is_service",
		}},
	} {
		got := g1Columns(t, d, c.table)
		for _, col := range c.cols {
			notNull, ok := got[col]
			if !ok {
				t.Errorf("%s has no column %s", c.table, col)
				continue
			}
			if notNull {
				t.Errorf("%s.%s is NOT NULL, want nullable (create inserts before Validate)", c.table, col)
			}
		}
	}
	if _, ok := g1Columns(t, d, "goods_receipt")["snap_po_id"]; ok {
		t.Errorf("goods_receipt has snap_po_id, want none (CD-303)")
	}
}

// MOD-GRN G1 Do 1 (GRN spec §3.2; 06-masters.md MST-092): init calls
// uom.RegisterQtyUse for received_qty and for rejected_qty on
// goods_receipt_line (parent_id, uom_id). One approved GRN holds a
// fractional received quantity in KGS (rejected 0) and a whole received but
// fractional rejected quantity in MTR, so turning whole-number on is refused
// for KGS only through the received_qty entry and for MTR only through the
// rejected_qty entry, and each UoM is left as it was.
func TestGRN_QtyUse_BothColumns(t *testing.T) {
	env := grntest.New(t)
	ctx := t.Context()
	svc := uom.New(mstkit.Env{DB: env.DB, Auth: env.Auth, Engine: env.Engine, Auditor: audit.Default, Clock: env.Clock})
	get := func(code string) uom.UoM {
		t.Helper()
		id, err := strconv.ParseInt(g1MasterID(t, env.DB, "uom", code), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		var u uom.UoM
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			u, err = svc.Get(ctx, tx, id)
			return err
		}); err != nil {
			t.Fatalf("get UoM %s: %v", code, err)
		}
		return u
	}

	po := g1TwoLinePO(t, env) // 100 KGS (whole) and 100 MTR (whole)
	grn := g1Receipt(t, env, po, doctype.StateApproved,
		g1Line{poLine: 0, received: "2.5"},
		g1Line{poLine: 1, received: "3", rejected: "0.5", reason: "Cut short"})
	if grn.Number != "GR/26-27/00001" {
		t.Fatalf("GRN number = %q, want GR/26-27/00001", grn.Number)
	}
	// uom_id is G2's copy from the PO line (System; Validate is a stub in
	// G1). Set it to the PO line's unit as Validate will, only where it is
	// still blank, so this test keeps passing once G2 fills it.
	if err := env.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE goods_receipt_line
			SET uom_id = (SELECT pl.uom_id FROM purchase_order_line pl WHERE pl.id = goods_receipt_line.po_line_id)
			WHERE parent_id = ? AND uom_id IS NULL`, grn.ID)
		return err
	}); err != nil {
		t.Fatalf("copy the PO lines' units: %v", err)
	}

	for _, c := range []struct{ code, msg string }{
		{"KGS", "KGS is used with fractional quantities (e.g. GR/26-27/00001). Create a new UoM instead."},
		{"MTR", "MTR is used with fractional quantities (e.g. GR/26-27/00001). Create a new UoM instead."},
	} {
		before := get(c.code)
		if before.WholeNumber {
			t.Fatalf("%s is whole-number before the test", c.code)
		}
		_, err := svc.Update(ctx, env.Admin, before.ID, before.Version,
			uom.Input{Code: before.Code, Name: before.Name, UQC: before.UQC, WholeNumber: true})
		want := doctype.FieldError{Field: "whole_number", Message: c.msg}
		var ve *mstkit.ValidationError
		if !errors.As(err, &ve) || len(ve.Fields) != 1 || ve.Fields[0] != want {
			t.Errorf("turn whole-number on for %s: err = %v; want %+v (MST-092)", c.code, err, want)
		}
		if after := get(c.code); after != before {
			t.Errorf("%s after the refusal = %+v, want unchanged %+v", c.code, after, before)
		}
	}
}
