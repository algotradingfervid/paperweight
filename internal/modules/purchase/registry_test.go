package purchase_test

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// lookup returns a registered purchase DocType or stops the test.
func lookup(t *testing.T, name string) *doctype.DocType {
	t.Helper()
	dt, ok := doctype.Lookup(name)
	if !ok {
		t.Fatalf("doctype.Lookup(%q): not registered", name)
	}
	return dt
}

// field returns a declared field of dt or stops the test.
func field(t *testing.T, dt *doctype.DocType, name string) doctype.Field {
	t.Helper()
	f, ok := dt.Field(name)
	if !ok {
		t.Fatalf("%s has no field %q", dt.Name, name)
	}
	return f
}

// scoped builds a role's rights row at one scope.
func scoped(scope string, rights ...string) map[string]string {
	m := map[string]string{}
	for _, r := range rights {
		m[r] = scope
	}
	return m
}

// PO-041 (PO spec §10, §20.6; CD-247, CD-253, CD-280): purchase.Order's
// DefaultRights equal the purchase.Order rows of 00-fixtures.md for every
// role, with Purchase User write, submit and delete at own and amend at all,
// and CloseRight amend. purchase.Request's rows are §20.6's without the
// approve rows, which P5 adds once M0-24 is on main (CD-280). The same rows
// reach the real auth.Service through SeedDefaults.
func TestPO_DefaultRights(t *testing.T) {
	if purchase.DocType != "purchase.Order" || purchase.RequestDocType != "purchase.Request" {
		t.Fatalf("DocType, RequestDocType = %q, %q; want purchase.Order, purchase.Request (CD-276)", purchase.DocType, purchase.RequestDocType)
	}
	purchaseUser := scoped("all", "read", "create", "amend", "print", "export")
	maps.Copy(purchaseUser, scoped("own", "write", "submit", "delete"))
	wantOrder := map[string]map[string]string{
		"Purchase User":    purchaseUser,
		"Purchase Manager": scoped("all", "read", "approve", "cancel", "print", "export"),
		"Director":         scoped("all", "read", "approve", "print", "export"),
		"Stores":           scoped("all", "read"),
	}
	wantRequest := map[string]map[string]string{
		"Employee":         scoped("own", "read", "create", "write", "submit", "cancel", "delete"),
		"Stores":           scoped("all", "read"),
		"Purchase User":    scoped("all", "read"),
		"Purchase Manager": scoped("all", "read"),
		"Director":         scoped("all", "read"),
	}

	order := lookup(t, purchase.DocType)
	if order.CloseRight != "amend" {
		t.Errorf("%s CloseRight = %q, want amend (CD-247)", order.Name, order.CloseRight)
	}
	cases := []struct {
		dt   *doctype.DocType
		want map[string]map[string]string
	}{
		{order, wantOrder},
		{lookup(t, purchase.RequestDocType), wantRequest},
	}
	for _, c := range cases {
		if !maps.EqualFunc(c.dt.DefaultRights, c.want, maps.Equal) {
			t.Errorf("%s DefaultRights = %v\nwant %v", c.dt.Name, c.dt.DefaultRights, c.want)
		}
	}

	// The real auth.Service seeds exactly these rows.
	env := purchasetest.New(t)
	for _, c := range cases {
		for role, rights := range c.want {
			got, err := env.Auth.RoleRights(t.Context(), role, c.dt.Name)
			if err != nil {
				t.Fatalf("RoleRights(%s, %s): %v", role, c.dt.Name, err)
			}
			want := map[auth.Right]auth.Scope{}
			for r, s := range rights {
				want[auth.Right(r)] = auth.Scope(s)
			}
			if !maps.Equal(got, want) {
				t.Errorf("real auth: %s on %s = %v, want %v", role, c.dt.Name, got, want)
			}
		}
	}
}

// MOD-PO P1 Do 1 (PO spec §3, §20.2; CD-243, CD-247, CD-253, CD-255,
// CD-258 (3)): both DocTypes are registered as the spec describes, with
// attachments, their CloseRights, the Request's Submitted label and the
// request-row LinkSpec with no RollUp.
func TestPurchase_Registry(t *testing.T) {
	order := lookup(t, purchase.DocType)
	request := lookup(t, purchase.RequestDocType)

	type shape struct {
		Label, PluralLabel, Table, LinesTable, Series, OwnerField string
		Submittable, Approvable, Printable, HasRoundedTotal       bool
		Attachable                                                bool
		ProjectFields, SiteFields                                 []string
		CloseRight                                                string
	}
	shapeOf := func(dt *doctype.DocType) shape {
		return shape{
			Label: dt.Label, PluralLabel: dt.PluralLabel, Table: dt.Table, LinesTable: dt.LinesTable,
			Series: dt.Series, OwnerField: dt.OwnerField,
			Submittable: dt.Submittable, Approvable: dt.Approvable, Printable: dt.Printable,
			HasRoundedTotal: dt.HasRoundedTotal, Attachable: dt.Attachable,
			ProjectFields: dt.ProjectFields, SiteFields: dt.SiteFields, CloseRight: dt.CloseRight,
		}
	}
	for _, c := range []struct {
		dt   *doctype.DocType
		want shape
	}{
		{order, shape{
			Label: "Purchase Order", PluralLabel: "Purchase Orders", Table: "purchase_order", LinesTable: "purchase_order_line",
			Series: "PO/{FY}/{#####}", OwnerField: "created_by",
			Submittable: true, Approvable: true, Printable: true, HasRoundedTotal: true, Attachable: true,
			ProjectFields: []string{"project_id"}, SiteFields: []string{"site_id"}, CloseRight: "amend",
		}},
		{request, shape{
			Label: "Purchase Request", PluralLabel: "Purchase Requests", Table: "purchase_request", LinesTable: "purchase_request_line",
			Series: "PR/{FY}/{#####}", OwnerField: "created_by",
			Submittable: true, Approvable: false, Printable: false, HasRoundedTotal: false, Attachable: true,
			ProjectFields: []string{"project_id"}, SiteFields: []string{"site_id"}, CloseRight: "approve",
		}},
	} {
		if got := shapeOf(c.dt); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s registered as %+v\nwant %+v", c.dt.Name, got, c.want)
		}
		if c.dt.Hooks == nil {
			t.Errorf("%s Hooks = nil, want the purchase hooks", c.dt.Name)
		}
	}

	// The Request's approved state reads Submitted (§20.3); the Order keeps
	// the global label.
	if got := request.StateLabels[doctype.StateApproved]; got != "Submitted" {
		t.Errorf("%s StateLabels[approved] = %q, want Submitted", request.Name, got)
	}
	if got := doctype.StateLabel(*request, doctype.StateApproved); got != "Submitted" {
		t.Errorf("StateLabel(%s, approved) = %q, want Submitted", request.Name, got)
	}
	if got, global := doctype.StateLabel(*order, doctype.StateApproved), doctype.StateLabel(doctype.DocType{}, doctype.StateApproved); got != global {
		t.Errorf("StateLabel(%s, approved) = %q, want the global %q", order.Name, got, global)
	}

	// One document link, from a PO line to a request line; no RollUp (§3.2).
	wantLinks := []doctype.LinkSpec{{Field: "request_line_id", Target: purchase.RequestDocType, OnLine: true}}
	if !slices.Equal(order.Links, wantLinks) {
		t.Errorf("%s Links = %+v, want %+v (copied_from_id has no LinkSpec)", order.Name, order.Links, wantLinks)
	}
	if len(order.RollUps) != 0 || len(request.RollUps) != 0 {
		t.Errorf("RollUps: order %+v, request %+v; want none (ordered_qty is the module's, CD-247 (4))", order.RollUps, request.RollUps)
	}
	if len(request.Links) != 0 {
		t.Errorf("%s Links = %+v, want none", request.Name, request.Links)
	}
}

// MOD-PO P1 Do 1 (PO spec §3.1, §3.2, §20.2; NUM-015; CD-247, CD-280 D7):
// the field flags. qty Positive, price, discount_pct and already_received
// NonNegative, discount_pct at most 100, §3's Required and MaxLen values, and
// System on receipt_status, order_status, ordered_qty, copy_notes and the
// totals, so amend doesn't copy them (doc/terminal.go:162).
func TestPurchase_FieldFlags(t *testing.T) {
	order := lookup(t, purchase.DocType)
	request := lookup(t, purchase.RequestDocType)
	hundred, err := types.ParsePercent("100")
	if err != nil {
		t.Fatal(err)
	}

	type flags struct {
		line                            bool
		required, positive, nonNegative bool
		maxLen                          int
		system                          bool
	}
	cases := []struct {
		dt   *doctype.DocType
		name string
		want flags
	}{
		// Order header (§3.1).
		{order, "vendor_id", flags{required: true}},
		{order, "site_id", flags{required: true}},
		{order, "project_id", flags{required: true}},
		{order, "order_date", flags{required: true}},
		{order, "vendor_quote_ref", flags{maxLen: 140}},
		{order, "terms", flags{maxLen: 2000}},
		{order, "remarks", flags{maxLen: 500}},
		{order, "earlier_po_no", flags{maxLen: 40}},
		{order, "copied_from_no", flags{maxLen: 40}},
		{order, "copy_notes", flags{system: true}},
		{order, "receipt_status", flags{system: true}},
		{order, "net_total", flags{system: true}},
		{order, "cgst_total", flags{system: true}},
		{order, "sgst_total", flags{system: true}},
		{order, "igst_total", flags{system: true}},
		{order, "grand_total", flags{system: true}},
		{order, "rounded_total", flags{system: true}},
		// Order lines (§3.2).
		{order, "item_id", flags{line: true, required: true}},
		{order, "description", flags{line: true, maxLen: 500}},
		{order, "uom_id", flags{line: true, required: true}},
		{order, "qty", flags{line: true, required: true, positive: true}},
		{order, "price", flags{line: true, nonNegative: true}},
		{order, "discount_pct", flags{line: true, nonNegative: true}},
		{order, "tax_code_id", flags{line: true, required: true}},
		{order, "already_received", flags{line: true, nonNegative: true}},
		// Request header and lines (§20.2).
		{request, "site_id", flags{required: true}},
		{request, "project_id", flags{required: true}},
		{request, "request_date", flags{required: true}},
		{request, "purpose", flags{maxLen: 500}},
		{request, "order_status", flags{system: true}},
		{request, "item_id", flags{line: true, required: true}},
		{request, "description", flags{line: true, maxLen: 500}},
		{request, "uom_id", flags{line: true, required: true}},
		{request, "qty", flags{line: true, required: true, positive: true}},
		{request, "note", flags{line: true, maxLen: 200}},
		{request, "ordered_qty", flags{line: true, system: true}},
	}
	for _, c := range cases {
		f := field(t, c.dt, c.name)
		got := flags{
			line: f.OnLine, required: f.Required, positive: f.Positive, nonNegative: f.NonNegative,
			maxLen: f.MaxLen, system: f.System,
		}
		if c.want.system && c.want.maxLen == 0 {
			got.maxLen = 0 // a System field's length isn't the spec's
		}
		if got != c.want {
			t.Errorf("%s.%s flags = %+v, want %+v", c.dt.Name, c.name, got, c.want)
		}
	}

	disc := field(t, order, "discount_pct")
	if disc.Max == nil || *disc.Max != int64(hundred) {
		t.Errorf("%s.discount_pct Max = %v, want %d (100%%, NUM-015)", order.Name, disc.Max, int64(hundred))
	}
}

// MOD-PO global constraints and P1 Files (CD-247 (1), CD-280 D7 (a)): the
// fields a module route or Copy sets on create are Fields that are not
// ReadOnly, because checkReadOnly refuses a read-only value on create.
// copied_from_id is a plain integer with no LinkSpec.
func TestPurchase_ModuleSetFieldsNotReadOnly(t *testing.T) {
	order := lookup(t, purchase.DocType)
	for _, c := range []struct {
		name string
		typ  doctype.FieldType
		line bool
	}{
		{"request_line_id", doctype.Int, true},
		{"copied_from_no", doctype.Text, false},
		{"copied_from_id", doctype.Int, false},
		{"copy_notes", doctype.Text, false},
	} {
		f := field(t, order, c.name)
		if f.ReadOnly {
			t.Errorf("%s.%s is ReadOnly, want not ReadOnly (checkReadOnly refuses it on create)", order.Name, c.name)
		}
		if f.Type != c.typ || f.OnLine != c.line {
			t.Errorf("%s.%s Type %v OnLine %v, want %v %v", order.Name, c.name, f.Type, f.OnLine, c.typ, c.line)
		}
	}
}

// MOD-PO P1 schema tests (PO spec §3, §20.2; CD-113, CD-115, CD-129): both
// series are valid templates whose numbers render at most 14 characters, and
// the registry accepts both DocTypes.
func TestPurchase_SeriesAndRegistry(t *testing.T) {
	if err := doctype.CheckRegistry(); err != nil {
		t.Errorf("CheckRegistry() = %v, want nil with both purchase DocTypes registered", err)
	}
	n := naming.New(testkit.NewDB(t), testkit.Clock(), nil, nil)
	on := types.Date{Year: 2026, Month: 10, Day: 24}
	for _, c := range []struct{ name, first string }{
		{purchase.DocType, "PO/26-27/00001"},
		{purchase.RequestDocType, "PR/26-27/00001"},
	} {
		dt := lookup(t, c.name)
		if err := n.ValidateTemplate(dt.Series); err != nil {
			t.Errorf("%s series %q: ValidateTemplate = %v, want nil", c.name, dt.Series, err)
		}
		got, err := n.PreviewTemplate(t.Context(), dt.Series, on)
		got, _, _ = strings.Cut(got, ", ") // PreviewTemplate lists the next three numbers
		if err != nil || got != c.first || len(got) > 14 {
			t.Errorf("%s series %q renders %q (%d characters), %v; want %q, at most 14", c.name, dt.Series, got, len(got), err, c.first)
		}
	}
}
