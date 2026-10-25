package doctype

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Supporting tests for WP1-DOC Task 1 (no DOC case ID). Each test works on a
// fresh newRegistry(), never on the package-level registry.

type nopHooks struct{}

func (nopHooks) Validate(context.Context, *db.Tx, int64) []FieldError       { return nil }
func (nopHooks) BeforeSubmit(context.Context, *db.Tx, int64) error          { return nil }
func (nopHooks) ApplyEffects(context.Context, *db.Tx, int64) error          { return nil }
func (nopHooks) ReverseEffects(context.Context, *db.Tx, int64) error        { return nil }
func (nopHooks) Amount(context.Context, *db.Tx, int64) (types.Money, error) { return 0, nil }
func (nopHooks) Date(context.Context, *db.Tx, int64) (types.Date, error) {
	var d types.Date
	return d, nil
}

type nopMaster struct{}

func (nopMaster) Lookup(context.Context, *db.Tx, int64) (MasterInfo, bool, error) {
	return MasterInfo{}, false, nil
}

// testOrder is a valid document DocType; each bad case mutates one copy of it.
func testOrder() DocType {
	return DocType{
		Name:        "t.Order",
		Label:       "Purchase Order",
		PluralLabel: "Purchase Orders",
		Table:       "t_order",
		LinesTable:  "t_order_lines",
		Submittable: true,
		Approvable:  true,
		Fields: []Field{
			{Name: "supplier_id", Label: "Supplier", Type: Link, Link: "t.Party", Required: true, ListPriority: 1},
			{Name: "credit_days", Label: "Credit days", Type: Int, Hint: "Days the supplier gives you to pay."},
			{Name: "priority", Label: "Priority", Type: Select, Options: []string{"normal", "urgent"}},
			{Name: "total", Label: "Total", Type: Money, ReadOnly: true},
			{Name: "qty", Label: "Qty", Type: Qty, OnLine: true, Step: "lines"},
			{Name: "received_qty", Label: "Received qty", Type: Qty, OnLine: true, Step: "lines", RollUp: true, EditableAfterSubmit: true},
			{Name: "posted_at", Label: "Posted at", Type: Timestamp, System: true, EditableAfterSubmit: true},
		},
		Hooks: nopHooks{},
	}
}

func testParty() DocType {
	return DocType{
		Name:   "t.Party",
		Label:  "Party",
		Table:  "t_party",
		Fields: []Field{{Name: "code", Label: "Code", Type: Text}},
		Master: nopMaster{},
	}
}

// mustPanic runs register and returns the recovered panic value as text; it
// fails the test if register does not panic.
func mustPanic(t *testing.T, name string, register func()) string {
	t.Helper()
	var got any
	func() {
		defer func() { got = recover() }()
		register()
	}()
	if got == nil {
		t.Errorf("%s: Register did not panic", name)
		return ""
	}
	return fmt.Sprint(got)
}

func mustNotPanic(t *testing.T, name string, register func()) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("%s: Register panicked: %v", name, p)
		}
	}()
	register()
}

func fieldNames(fs []Field) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Name)
	}
	return out
}

func TestRegistry_RegisterAndLookup(t *testing.T) {
	r := newRegistry()
	r.Register(testOrder())
	r.Register(testParty())
	r.Register(DocType{Name: "t.Claim", Label: "Expense Claim", Table: "t_claim"})

	if _, ok := r.Lookup("t.Missing"); ok {
		t.Errorf(`Lookup("t.Missing") ok = true, want false`)
	}
	dt, ok := r.Lookup("t.Order")
	if !ok || dt == nil {
		t.Fatalf(`Lookup("t.Order") = %v, %v; want the DocType`, dt, ok)
	}
	if dt.Table != "t_order" {
		t.Errorf("Table = %q, want %q", dt.Table, "t_order")
	}
	if dt.Label != "Purchase Order" || dt.PluralLabel != "Purchase Orders" {
		t.Errorf("Label, PluralLabel = %q, %q; want %q, %q", dt.Label, dt.PluralLabel, "Purchase Order", "Purchase Orders")
	}

	var names []string
	for _, d := range r.All() {
		names = append(names, d.Name)
	}
	if want := []string{"t.Claim", "t.Order", "t.Party"}; !slices.Equal(names, want) {
		t.Errorf("All() names = %v, want %v", names, want)
	}

	qty, ok := dt.Field("qty")
	if !ok {
		t.Fatalf(`Field("qty") ok = false`)
	}
	if qty.Column != "qty" {
		t.Errorf(`Field("qty").Column = %q, want "qty" (defaults to Name)`, qty.Column)
	}
	if qty.Step != "lines" {
		t.Errorf(`Field("qty").Step = %q, want "lines"`, qty.Step)
	}
	if _, ok := dt.Field("nope"); ok {
		t.Errorf(`Field("nope") ok = true, want false`)
	}
	sup, _ := dt.Field("supplier_id")
	if sup.ListPriority != 1 {
		t.Errorf("supplier_id ListPriority = %d, want 1", sup.ListPriority)
	}
	if sup.Column != "supplier_id" {
		t.Errorf(`supplier_id Column = %q, want "supplier_id"`, sup.Column)
	}
	cd, _ := dt.Field("credit_days")
	if cd.Hint != "Days the supplier gives you to pay." {
		t.Errorf("credit_days Hint = %q, want %q", cd.Hint, "Days the supplier gives you to pay.")
	}
	pr, _ := dt.Field("priority")
	if want := []string{"normal", "urgent"}; !slices.Equal(pr.Options, want) {
		t.Errorf("priority Options = %v, want %v", pr.Options, want)
	}

	if got, want := fieldNames(dt.HeaderFields()), []string{"supplier_id", "credit_days", "priority", "total"}; !slices.Equal(got, want) {
		t.Errorf("HeaderFields() = %v, want %v", got, want)
	}
	if got, want := fieldNames(dt.LineFields()), []string{"qty", "received_qty", "posted_at"}; !slices.Equal(got, want) {
		t.Errorf("LineFields() = %v, want %v", got, want)
	}
}

func TestRegistry_PanicsOnDuplicate(t *testing.T) {
	r := newRegistry()
	r.Register(testOrder())
	got := mustPanic(t, "second t.Order", func() { r.Register(testOrder()) })
	if want := `doctype: DocType "t.Order" registered twice`; got != want {
		t.Errorf("panic = %q, want %q", got, want)
	}
}

func TestRegistry_RejectsBadIdentifiers(t *testing.T) {
	mustNotPanic(t, "valid base", func() { newRegistry().Register(testOrder()) })

	cases := []struct {
		name   string
		mutate func(*DocType)
	}{
		{"Table t-order", func(d *DocType) { d.Table = "t-order" }},
		{"Column qty;drop", func(d *DocType) { d.Fields[4].Column = "qty;drop" }},
		{"LinesTable X", func(d *DocType) { d.LinesTable = "X" }},
	}
	for _, c := range cases {
		dt := testOrder()
		c.mutate(&dt)
		mustPanic(t, c.name, func() { newRegistry().Register(dt) })
	}
}

func TestRegistry_RejectsBadFields(t *testing.T) {
	mustNotPanic(t, "valid base", func() { newRegistry().Register(testOrder()) })

	cases := []struct {
		name  string
		field Field
	}{
		{"Link without Link target", Field{Name: "party_id", Label: "Party", Type: Link, Link: ""}},
		{"Select without Options", Field{Name: "kind", Label: "Kind", Type: Select}},
		{"Options on a non-Select field", Field{Name: "note", Label: "Note", Type: Text, Options: []string{"a", "b"}}},
		{"ListPriority < 0", Field{Name: "note", Label: "Note", Type: Text, ListPriority: -1}},
		{"MaxLen < 0", Field{Name: "note", Label: "Note", Type: Text, MaxLen: -1}},
		{`Step "Lines 2"`, Field{Name: "note", Label: "Note", Type: Text, Step: "Lines 2"}},
		{"EditableAfterSubmit without RollUp/System", Field{Name: "note", Label: "Note", Type: Text, EditableAfterSubmit: true}},
		{"ReadOnly with Required", Field{Name: "grand_total", Label: "Grand total", Type: Money, ReadOnly: true, Required: true}},
		{"ReadOnly with Default", Field{Name: "grand_total", Label: "Grand total", Type: Money, ReadOnly: true, Default: "0.00"}},
		{"duplicate field name", Field{Name: "credit_days", Label: "Credit days again", Type: Int}},
	}
	for _, c := range cases {
		dt := testOrder()
		dt.Fields = append(dt.Fields, c.field)
		mustPanic(t, c.name, func() { newRegistry().Register(dt) })
	}
}

func TestRegistry_MasterRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*DocType)
	}{
		{"Master with Hooks", func(d *DocType) { d.Hooks = nopHooks{} }},
		{"Master with Submittable", func(d *DocType) { d.Submittable = true }},
		{"Master with Approvable", func(d *DocType) { d.Approvable = true }},
	}
	for _, c := range cases {
		dt := testParty()
		c.mutate(&dt)
		mustPanic(t, c.name, func() { newRegistry().Register(dt) })
	}

	r := newRegistry()
	mustNotPanic(t, "master t.Party", func() { r.Register(testParty()) })
	mustNotPanic(t, "plain t.Plain", func() {
		r.Register(DocType{Name: "t.Plain", Label: "Plain", Table: "t_plain", Fields: []Field{{Name: "title", Label: "Title", Type: Text}}})
	})
	mustNotPanic(t, "document t.Order", func() { r.Register(testOrder()) })

	party, ok := r.Lookup("t.Party")
	if !ok {
		t.Fatalf("t.Party not registered")
	}
	if !party.IsMaster() || party.IsDocument() {
		t.Errorf("t.Party IsMaster, IsDocument = %v, %v; want true, false", party.IsMaster(), party.IsDocument())
	}
	plain, ok := r.Lookup("t.Plain")
	if !ok {
		t.Fatalf("t.Plain not registered")
	}
	if plain.IsMaster() || plain.IsDocument() {
		t.Errorf("t.Plain IsMaster, IsDocument = %v, %v; want false, false", plain.IsMaster(), plain.IsDocument())
	}
	order, ok := r.Lookup("t.Order")
	if !ok {
		t.Fatalf("t.Order not registered")
	}
	if order.IsMaster() || !order.IsDocument() {
		t.Errorf("t.Order IsMaster, IsDocument = %v, %v; want false, true", order.IsMaster(), order.IsDocument())
	}
}

func TestRegistry_CheckResolvesRefs(t *testing.T) {
	// Minimal target with no outgoing references.
	target := func(withReceived bool) DocType {
		d := DocType{
			Name: "t.Order", Label: "Purchase Order", Table: "t_order", LinesTable: "t_order_lines",
			Fields: []Field{{Name: "qty", Label: "Qty", Type: Qty, OnLine: true}},
			Hooks:  nopHooks{},
		}
		if withReceived {
			d.Fields = append(d.Fields, Field{Name: "received_qty", Label: "Received qty", Type: Qty, OnLine: true, RollUp: true, EditableAfterSubmit: true})
		}
		return d
	}
	receipt := func(links []LinkSpec, rollups []RollUp) DocType {
		return DocType{
			Name: "t.Receipt", Label: "Goods Receipt", Table: "t_receipt", LinesTable: "t_receipt_lines",
			Fields: []Field{
				{Name: "order_line_id", Label: "Order line", Type: Int, OnLine: true},
				{Name: "qty", Label: "Qty", Type: Qty, OnLine: true},
			},
			Links:   links,
			RollUps: rollups,
			Hooks:   nopHooks{},
		}
	}
	rollUp := RollUp{TargetDocType: "t.Order", TargetField: "received_qty", SourceField: "qty", JoinField: "order_line_id"}
	linkSpec := LinkSpec{Field: "order_line_id", Target: "t.Order", OnLine: true}
	withRights := func(owner, employee string, project, site []string) DocType {
		return DocType{
			Name: "t.Claim", Label: "Expense Claim", Table: "t_claim", LinesTable: "t_claim_lines",
			Fields: []Field{
				{Name: "owner_id", Label: "Owner", Type: Int},
				{Name: "employee_id", Label: "Employee", Type: Int},
				{Name: "project_id", Label: "Project", Type: Int},
				{Name: "site_id", Label: "Site", Type: Int, OnLine: true},
			},
			OwnerField: owner, EmployeeField: employee, ProjectFields: project, SiteFields: site,
			Hooks: nopHooks{},
		}
	}
	goodClaim := withRights("owner_id", "employee_id", []string{"project_id"}, []string{"lines.site_id"})

	cases := []struct {
		name string
		bad  []DocType
		want string // the error must name this
		good []DocType
	}{
		{
			name: "Field.Link to an unknown DocType",
			bad: []DocType{{Name: "t.Note", Label: "Note", Table: "t_note",
				Fields: []Field{{Name: "party_id", Label: "Party", Type: Link, Link: "t.Party"}}}},
			want: "t.Party",
			good: []DocType{{Name: "t.Note", Label: "Note", Table: "t_note",
				Fields: []Field{{Name: "party_id", Label: "Party", Type: Link, Link: "t.Party"}}}, testParty()},
		},
		{
			name: "RollUp to an unknown DocType",
			bad:  []DocType{receipt(nil, []RollUp{rollUp})},
			want: "t.Order",
			good: []DocType{receipt(nil, []RollUp{rollUp}), target(true)},
		},
		{
			name: "RollUp to an unknown target field",
			bad:  []DocType{receipt(nil, []RollUp{rollUp}), target(false)},
			want: "received_qty",
			good: []DocType{receipt(nil, []RollUp{rollUp}), target(true)},
		},
		{
			name: "LinkSpec to an unknown DocType",
			bad:  []DocType{receipt([]LinkSpec{linkSpec}, nil)},
			want: "t.Order",
			good: []DocType{receipt([]LinkSpec{linkSpec}, nil), target(false)},
		},
		{
			name: "OwnerField naming no column",
			bad:  []DocType{withRights("maker_id", "employee_id", []string{"project_id"}, []string{"lines.site_id"})},
			want: "maker_id",
			good: []DocType{goodClaim},
		},
		{
			name: "EmployeeField naming no column",
			bad:  []DocType{withRights("owner_id", "staff_id", []string{"project_id"}, []string{"lines.site_id"})},
			want: "staff_id",
			good: []DocType{goodClaim},
		},
		{
			name: "ProjectFields entry naming no column",
			bad:  []DocType{withRights("owner_id", "employee_id", []string{"job_id"}, []string{"lines.site_id"})},
			want: "job_id",
			good: []DocType{goodClaim},
		},
		{
			name: "SiteFields entry naming no column",
			bad:  []DocType{withRights("owner_id", "employee_id", []string{"project_id"}, []string{"lines.yard_id"})},
			want: "yard_id",
			good: []DocType{goodClaim},
		},
	}
	for _, c := range cases {
		bad := newRegistry()
		mustNotPanic(t, c.name+" (register)", func() {
			for _, d := range c.bad {
				bad.Register(d)
			}
		})
		err := bad.Check()
		if err == nil {
			t.Errorf("%s: Check() = nil, want an error naming %q", c.name, c.want)
		} else if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: Check() = %q, want it to name %q", c.name, err, c.want)
		}

		good := newRegistry()
		mustNotPanic(t, c.name+" (register fixed)", func() {
			for _, d := range c.good {
				good.Register(d)
			}
		})
		if err := good.Check(); err != nil {
			t.Errorf("%s: after the fix, Check() = %v, want nil", c.name, err)
		}
	}
}

func TestStateLabel(t *testing.T) {
	cases := map[State]string{
		StateDraft:     "Draft",
		StatePending:   "Waiting for approval",
		StateApproved:  "Approved",
		StateClosed:    "Closed",
		StateRejected:  "Rejected",
		StateCancelled: "Cancelled",
	}
	for s, want := range cases {
		if got := StateLabel(s); got != want {
			t.Errorf("StateLabel(%q) = %q, want %q", s, got, want)
		}
	}
}
