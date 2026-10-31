package purchase_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The plan's P9 signatures (plan P9 Do); assigning the method expressions
// makes any change a compile error.
var (
	p9PendingReport func(*purchase.Service, context.Context, auth.Principal, purchase.PendingFilter, int) (purchase.PendingView, error) = (*purchase.Service).PendingReport
	p9OverdueCount  func(*purchase.Service, context.Context, auth.Principal) (int, error)                                               = (*purchase.Service).OverdueCount
)

// p9Row is the part of a purchase.PendingRow these tests compare: the 15
// columns of PO spec §13 (Expected on from §27.1), the ids and the Overdue
// flag. A zero Date is a blank date.
type p9Row struct {
	LineID, POID                 int64
	PONumber                     string
	OrderDate, RequiredBy, ExpOn string
	Vendor, Project, Site        string
	ItemCode, ItemName           string
	Ordered, Received, Pending   types.Qty
	UoM                          string
	NetRate                      types.Rate
	PendingValue                 types.Money
	Overdue                      bool
}

func p9DateText(d types.Date) string {
	if d == (types.Date{}) {
		return ""
	}
	return d.ISO()
}

func p9RowOf(r purchase.PendingRow) p9Row {
	return p9Row{
		LineID: r.LineID, POID: r.POID, PONumber: r.PONumber,
		OrderDate: p9DateText(r.OrderDate), RequiredBy: p9DateText(r.RequiredBy), ExpOn: p9DateText(r.ExpectedOn),
		Vendor: r.Vendor, Project: r.Project, Site: r.Site, ItemCode: r.ItemCode, ItemName: r.ItemName,
		Ordered: r.Ordered, Received: r.Received, Pending: r.Pending, UoM: r.UoM,
		NetRate: r.NetRate, PendingValue: r.PendingValue, Overdue: r.Overdue,
	}
}

// p9Day is the date d-m-2026.
func p9Day(t *testing.T, m time.Month, d int) types.Date {
	t.Helper()
	on, err := types.DateFromISO(fmt.Sprintf("2026-%02d-%02d", int(m), d))
	if err != nil {
		t.Fatalf("date 2026-%d-%d: %v", m, d, err)
	}
	return on
}

// p9Oct31 is 31-10-2026 10:30 IST, the clock PO-079 and PO-081 name.
var p9Oct31 = time.Date(2026, time.October, 31, 10, 30, 0, 0, types.IST)

// p9Qty parses a quantity literal.
func p9Qty(t *testing.T, s string) types.Qty {
	t.Helper()
	q, err := types.ParseQty(s)
	if err != nil {
		t.Fatalf("ParseQty(%q): %v", s, err)
	}
	return q
}

// p9Rate parses a rate literal.
func p9Rate(t *testing.T, s string) types.Rate {
	t.Helper()
	r, err := types.ParseRate(s)
	if err != nil {
		t.Fatalf("ParseRate(%q): %v", s, err)
	}
	return r
}

// p9Money parses a money literal.
func p9Money(t *testing.T, s string) types.Money {
	t.Helper()
	m, err := types.ParseMoney(s)
	if err != nil {
		t.Fatalf("ParseMoney(%q): %v", s, err)
	}
	return m
}

// p9MasterID is the id of the master row with code in table.
func p9MasterID(t *testing.T, env *purchasetest.Env, table, code string) int64 {
	t.Helper()
	var id int64
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&id); err != nil { //nolint:gosec // G202: fixed table names
		t.Fatalf("id of %s %s: %v", table, code, err)
	}
	return id
}

func p9IDText(t *testing.T, env *purchasetest.Env, table, code string) string {
	t.Helper()
	return strconv.FormatInt(p9MasterID(t, env, table, code), 10)
}

// p9Line is a line form: item and unit codes, qty, price, plus extra values.
func p9Line(t *testing.T, env *purchasetest.Env, item, uom, qty, price, hsn string, extra map[string]string) map[string]string {
	t.Helper()
	m := map[string]string{
		"item_id": p9IDText(t, env, "item", item), "uom_id": p9IDText(t, env, "uom", uom),
		"qty": qty, "price": price, "tax_code_id": p9IDText(t, env, "tax_code", "GST18"), "hsn_sac": hsn,
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// p9TechparkOrder is an approved PRJ-TECHPARK / TECH-SITE PO by Arjun
// (Purchase User, unrestricted) through Engine.Do, outside Priya's PRJ-METRO
// restriction; the builders always save as Priya, who may not create it.
// requiredBy is ISO or "".
func p9TechparkOrder(t *testing.T, env *purchasetest.Env, requiredBy string, lines ...map[string]string) doc.Doc {
	t.Helper()
	ctx := t.Context()
	values := map[string]string{
		"vendor_id":  p9IDText(t, env, "party", "SHREE"),
		"site_id":    p9IDText(t, env, "site", "TECH-SITE"),
		"project_id": p9IDText(t, env, "project", "PRJ-TECHPARK"),
		"order_date": types.Today(env.Clock).ISO(),
	}
	if requiredBy != "" {
		values["required_by"] = requiredBy
	}
	res, err := env.Engine.Do(ctx, env.Arjun, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: values, Lines: lines})
	if err != nil {
		t.Fatalf("save PRJ-TECHPARK PO as Arjun: %v", err)
	}
	res, err = env.Engine.Do(ctx, env.Arjun, purchase.DocType, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version})
	if err != nil {
		t.Fatalf("submit PRJ-TECHPARK PO as Arjun: %v", err)
	}
	if res.State != doctype.StateApproved {
		t.Fatalf("PRJ-TECHPARK PO is %s after submit, want approved (below every rule)", res.State)
	}
	return p9Load(t, env, res.ID)
}

// p9Load reads PO id as stored.
func p9Load(t *testing.T, env *purchasetest.Env, id int64) doc.Doc {
	t.Helper()
	var d doc.Doc
	err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		d, err = doc.LoadDoc(t.Context(), tx, purchase.DocType, id)
		return err
	})
	if err != nil {
		t.Fatalf("load PO %d: %v", id, err)
	}
	return d
}

// p9Report is the pending report as p with filter f, page 1, on the real
// auth.Service.
func p9Report(t *testing.T, env *purchasetest.Env, p auth.Principal, f purchase.PendingFilter) purchase.PendingView {
	t.Helper()
	v, err := p9PendingReport(env.Service, t.Context(), p, f, 1)
	if err != nil {
		t.Fatalf("PendingReport(%s, %+v): %v", p.Name, f, err)
	}
	return v
}

// p9Lines is the sorted line ids of the view's rows.
func p9Lines(v purchase.PendingView) []int64 {
	ids := make([]int64, 0, len(v.Rows))
	for _, r := range v.Rows {
		ids = append(ids, r.LineID)
	}
	slices.Sort(ids)
	return ids
}

// p9Want compares the view's line ids (any order) and its Total to want.
func p9Want(t *testing.T, what string, v purchase.PendingView, want ...int64) {
	t.Helper()
	want = slices.Clone(want)
	slices.Sort(want)
	if got := p9Lines(v); !slices.Equal(got, want) || v.Total != len(want) {
		t.Errorf("%s: rows (line ids) = %v, Total %d; want %v, Total %d", what, got, v.Total, want, len(want))
	}
}

// p9Find is the view's row for line id.
func p9Find(t *testing.T, what string, v purchase.PendingView, lineID int64) (p9Row, bool) {
	t.Helper()
	for _, r := range v.Rows {
		if r.LineID == lineID {
			return p9RowOf(r), true
		}
	}
	t.Errorf("%s: no row for line %d (rows %v)", what, lineID, p9Lines(v))
	return p9Row{}, false
}

// p9Overdue is OverdueCount as p.
func p9Overdue(t *testing.T, env *purchasetest.Env, p auth.Principal) int {
	t.Helper()
	n, err := p9OverdueCount(env.Service, t.Context(), p)
	if err != nil {
		t.Fatalf("OverdueCount(%s): %v", p.Name, err)
	}
	return n
}

// p9Terminal runs a close or cancel with a reason as p at the PO's version.
func p9Terminal(t *testing.T, env *purchasetest.Env, p auth.Principal, po doc.Doc, a doctype.Action) {
	t.Helper()
	cur := p9Load(t, env, po.ID)
	if _, err := env.Engine.Do(t.Context(), p, purchase.DocType, po.ID, a, doc.ActionInput{Version: cur.Version, Reason: "Vendor can't supply the rest"}); err != nil {
		t.Fatalf("%s PO %d as %s: %v", a, po.ID, p.Name, err)
	}
}

// PO-037 (PO spec §13, §18; AUTH-110; CD-272): the pending report has one
// row per PO line with pending above 0 on approved POs only (a fully
// received line, a draft and a cancelled PO have none), with the 15 §13
// columns: received includes already received, net rate is the line's rate
// after discount, pending value is pending × net rate. A row whose due date
// (required_by here) is before today is Overdue; one due later isn't. Rows
// go through Auth.Scope on the real auth.Service: Priya (PRJ-METRO) never
// sees the PRJ-TECHPARK PO, and her filters can't widen that. The vendor,
// project, site, item and overdue-only filters are ANDed, and OverdueCount
// counts overdue lines in the same scope.
func TestPO_PendingReport(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()

	// Built on 24-10-2026 (authtest.T0); the report is read on 31-10-2026.
	po1 := purchasetest.NewOrder(t, env,
		purchasetest.OrderHeader(map[string]string{"required_by": "28-10-2026", "earlier_po_no": "OLD-7"}),
		purchasetest.OrderLines(
			p9Line(t, env, "MS-ANGLE-50", "KGS", "100", "62.5", "7216", map[string]string{"already_received": "20"}),
			p9Line(t, env, "WIRE-18G", "KGS", "50", "96", "7217", nil),
		))
	purchasetest.SetReceived(t, env, po1.Lines[0].ID, p9Qty(t, "30"))
	purchasetest.SetReceived(t, env, po1.Lines[1].ID, p9Qty(t, "50")) // fully received: no row
	po2 := purchasetest.NewOrder(t, env,
		purchasetest.OrderHeader(map[string]string{"vendor_id": p9IDText(t, env, "party", "BHARAT"), "required_by": "05-11-2026"}),
		purchasetest.OrderLines(p9Line(t, env, "CEMENT-OPC", "BAG", "40", "350", "2523", map[string]string{"discount_pct": "10"})))
	po3 := p9TechparkOrder(t, env, "2026-10-26",
		p9Line(t, env, "MS-ANGLE-50", "KGS", "10", "62.5", "7216", nil),
		p9Line(t, env, "WIRE-18G", "KGS", "5", "96", "7217", nil))
	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	cancelled := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"required_by": "26-10-2026"}))
	p9Terminal(t, env, env.Ravi, cancelled, doctype.ActionCancel)
	env.SetNow(t, p9Oct31)

	l1, l2, t1, t2 := po1.Lines[0].ID, po2.Lines[0].ID, po3.Lines[0].ID, po3.Lines[1].ID
	all := p9Report(t, env, env.Admin, purchase.PendingFilter{})
	p9Want(t, "Admin, no filter", all, l1, l2, t1, t2)
	for _, id := range []int64{po1.Lines[1].ID, draft.Lines[0].ID, cancelled.Lines[0].ID} {
		if slices.Contains(p9Lines(all), id) {
			t.Errorf("line %d is listed; a fully received line, a draft and a cancelled PO have no rows", id)
		}
	}

	if got, ok := p9Find(t, "Admin", all, l1); ok {
		want := p9Row{
			LineID: l1, POID: po1.ID, PONumber: po1.Number,
			OrderDate: "2026-10-24", RequiredBy: "2026-10-28", ExpOn: "",
			Vendor: "Shree Steel Traders", Project: "PRJ-METRO", Site: "METRO-SITE",
			ItemCode: "MS-ANGLE-50", ItemName: "MS Angle 50x50x5",
			Ordered: p9Qty(t, "100"), Received: p9Qty(t, "50"), Pending: p9Qty(t, "50"), UoM: "KGS",
			NetRate: p9Rate(t, "62.5"), PendingValue: p9Money(t, "3125"), Overdue: true,
		}
		if got != want {
			t.Errorf("PO1 line 1 row =\n%+v\nwant (received = 20 already + 30, overdue: due 28-10 before 31-10)\n%+v", got, want)
		}
	}
	if got, ok := p9Find(t, "Admin", all, l2); ok {
		want := p9Row{
			LineID: l2, POID: po2.ID, PONumber: po2.Number,
			OrderDate: "2026-10-24", RequiredBy: "2026-11-05", ExpOn: "",
			Vendor: "Bharat Tools", Project: "PRJ-METRO", Site: "METRO-SITE",
			ItemCode: "CEMENT-OPC", ItemName: "OPC Cement 53 Grade",
			Ordered: p9Qty(t, "40"), Received: 0, Pending: p9Qty(t, "40"), UoM: "BAG",
			NetRate: p9Rate(t, "315"), PendingValue: p9Money(t, "12600"), Overdue: false,
		}
		if got != want {
			t.Errorf("PO2 line 1 row =\n%+v\nwant (net rate 350 less 10%%, due 05-11 not overdue)\n%+v", got, want)
		}
	}
	for _, id := range []int64{t1, t2} {
		if got, ok := p9Find(t, "Admin", all, id); ok && (!got.Overdue || got.Project != "PRJ-TECHPARK" || got.Site != "TECH-SITE") {
			t.Errorf("PRJ-TECHPARK line %d = %+v, want Overdue (due 26-10) on PRJ-TECHPARK / TECH-SITE", id, got)
		}
	}

	// Scope (AUTH-110), real auth.Service: Priya sees PRJ-METRO rows only,
	// and no filter brings PRJ-TECHPARK back.
	p9Want(t, "Priya, no filter", p9Report(t, env, env.Priya, purchase.PendingFilter{}), l1, l2)
	p9Want(t, "Priya, project PRJ-TECHPARK", p9Report(t, env, env.Priya, purchase.PendingFilter{ProjectID: p9MasterID(t, env, "project", "PRJ-TECHPARK")}))
	p9Want(t, "Priya, site TECH-SITE", p9Report(t, env, env.Priya, purchase.PendingFilter{SiteID: p9MasterID(t, env, "site", "TECH-SITE")}))
	p9Want(t, "Priya, overdue only", p9Report(t, env, env.Priya, purchase.PendingFilter{OverdueOnly: true}), l1)
	p9Want(t, "Priya, item MS-ANGLE-50", p9Report(t, env, env.Priya, purchase.PendingFilter{ItemID: p9MasterID(t, env, "item", "MS-ANGLE-50")}), l1)

	// Filters, each alone and combined (AND), as Admin.
	for _, c := range []struct {
		name string
		f    purchase.PendingFilter
		want []int64
	}{
		{"vendor BHARAT", purchase.PendingFilter{VendorID: p9MasterID(t, env, "party", "BHARAT")}, []int64{l2}},
		{"vendor SHREE", purchase.PendingFilter{VendorID: p9MasterID(t, env, "party", "SHREE")}, []int64{l1, t1, t2}},
		{"project PRJ-TECHPARK", purchase.PendingFilter{ProjectID: p9MasterID(t, env, "project", "PRJ-TECHPARK")}, []int64{t1, t2}},
		{"site METRO-SITE", purchase.PendingFilter{SiteID: p9MasterID(t, env, "site", "METRO-SITE")}, []int64{l1, l2}},
		{"item MS-ANGLE-50", purchase.PendingFilter{ItemID: p9MasterID(t, env, "item", "MS-ANGLE-50")}, []int64{l1, t1}},
		{"item WIRE-18G", purchase.PendingFilter{ItemID: p9MasterID(t, env, "item", "WIRE-18G")}, []int64{t2}},
		{"overdue only", purchase.PendingFilter{OverdueOnly: true}, []int64{l1, t1, t2}},
		{"overdue only and project PRJ-METRO", purchase.PendingFilter{OverdueOnly: true, ProjectID: p9MasterID(t, env, "project", "PRJ-METRO")}, []int64{l1}},
		{"vendor BHARAT and overdue only", purchase.PendingFilter{VendorID: p9MasterID(t, env, "party", "BHARAT"), OverdueOnly: true}, nil},
		{"item MS-ANGLE-50 and site TECH-SITE", purchase.PendingFilter{ItemID: p9MasterID(t, env, "item", "MS-ANGLE-50"), SiteID: p9MasterID(t, env, "site", "TECH-SITE")}, []int64{t1}},
	} {
		p9Want(t, "Admin, "+c.name, p9Report(t, env, env.Admin, c.f), c.want...)
	}

	// The Overdue deliveries count is overdue lines, in the viewer's scope.
	if n := p9Overdue(t, env, env.Admin); n != 3 {
		t.Errorf("OverdueCount(Admin) = %d, want 3 (PO1 line 1 and both PRJ-TECHPARK lines)", n)
	}
	if n := p9Overdue(t, env, env.Priya); n != 1 {
		t.Errorf("OverdueCount(Priya) = %d, want 1 (PRJ-TECHPARK is outside her scope)", n)
	}

	// No read on Purchase Order (Deepak, Employee): AUTH's refusal, no rows.
	v, err := p9PendingReport(env.Service, ctx, env.Deepak, purchase.PendingFilter{}, 1)
	var de *auth.DeniedError
	if !errors.As(err, &de) || len(v.Rows) != 0 {
		t.Errorf("PendingReport(Deepak) = %d rows, %v; want AUTH's *auth.DeniedError and no rows", len(v.Rows), err)
	}
}

// PO-079 (PO spec §13, §25, §27.5; research S44): an approved PO with 15 Bag
// pending and required_by 28-10-2026, clock at 31-10-2026, is listed and
// Overdue, and the overdue-only filter lists it. Closing it removes its row
// from the report and the filter, and the Overdue deliveries count drops by
// one (another overdue PO keeps the count above zero).
func TestPO_ClosedLeavesOverdue(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env,
		purchasetest.OrderHeader(map[string]string{"required_by": "28-10-2026"}),
		purchasetest.OrderLines(p9Line(t, env, "CEMENT-OPC", "BAG", "40", "350", "2523", nil)))
	purchasetest.SetReceived(t, env, po.Lines[0].ID, p9Qty(t, "25"))
	other := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"required_by": "27-10-2026"}))
	env.SetNow(t, p9Oct31)
	line, otherLine := po.Lines[0].ID, other.Lines[0].ID

	all := p9Report(t, env, env.Priya, purchase.PendingFilter{})
	p9Want(t, "before close", all, line, otherLine)
	if got, ok := p9Find(t, "before close", all, line); ok && (got.Pending != p9Qty(t, "15") || got.UoM != "BAG" || !got.Overdue) {
		t.Errorf("row before close = %+v, want 15 BAG pending, Overdue", got)
	}
	p9Want(t, "overdue only, before close", p9Report(t, env, env.Priya, purchase.PendingFilter{OverdueOnly: true}), line, otherLine)
	if n := p9Overdue(t, env, env.Priya); n != 2 {
		t.Errorf("OverdueCount before close = %d, want 2", n)
	}

	p9Terminal(t, env, env.Priya, po, doctype.ActionClose)

	p9Want(t, "after close", p9Report(t, env, env.Priya, purchase.PendingFilter{}), otherLine)
	p9Want(t, "overdue only, after close", p9Report(t, env, env.Priya, purchase.PendingFilter{OverdueOnly: true}), otherLine)
	if n := p9Overdue(t, env, env.Priya); n != 1 {
		t.Errorf("OverdueCount after close = %d, want 1 (dropped by one)", n)
	}
}

// PO-081 (PO spec §13, §25, §27.1; CD-272 Gap 7; research S31): the due date
// is the PO's Expected on when set, else required_by; a line with neither is
// never overdue, and a due date of today isn't overdue (before today only).
// Clock at 31-10-2026: A (required_by 28-10, no Expected on) and C (no
// required_by, Expected on 30-10) are Overdue; B (required_by 28-10,
// Expected on 05-11), D (no dates) and E (required_by 31-10) aren't. The
// overdue-only filter and OverdueCount agree. Clearing B's Expected on makes
// B overdue again (undo).
func TestPO_PendingUsesExpectedOn(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	order := func(requiredBy string) doc.Doc {
		var opts []purchasetest.OrderOpt
		if requiredBy != "" {
			opts = append(opts, purchasetest.OrderHeader(map[string]string{"required_by": requiredBy}))
		}
		return purchasetest.NewOrder(t, env, opts...)
	}
	a, b, c, d, e := order("28-10-2026"), order("28-10-2026"), order(""), order(""), order("31-10-2026")
	if err := env.Service.SetExpected(ctx, env.Arjun, b.ID, 0, p9Day(t, time.November, 5), "Vendor moved it"); err != nil {
		t.Fatalf("SetExpected(B, 05-11-2026): %v", err)
	}
	if err := env.Service.SetExpected(ctx, env.Arjun, c.ID, 0, p9Day(t, time.October, 30), ""); err != nil {
		t.Fatalf("SetExpected(C, 30-10-2026): %v", err)
	}
	env.SetNow(t, p9Oct31)
	la, lb, lc, ld, le := a.Lines[0].ID, b.Lines[0].ID, c.Lines[0].ID, d.Lines[0].ID, e.Lines[0].ID

	check := func(step string, wantOverdue map[int64]bool, wantExp map[int64]string) {
		t.Helper()
		all := p9Report(t, env, env.Priya, purchase.PendingFilter{})
		p9Want(t, step+", no filter", all, la, lb, lc, ld, le)
		var overdue []int64
		for id, name := range map[int64]string{la: "A", lb: "B", lc: "C", ld: "D", le: "E"} {
			got, ok := p9Find(t, step, all, id)
			if !ok {
				continue
			}
			if got.Overdue != wantOverdue[id] {
				t.Errorf("%s: %s Overdue = %v, want %v (row %+v)", step, name, got.Overdue, wantOverdue[id], got)
			}
			if got.ExpOn != wantExp[id] {
				t.Errorf("%s: %s Expected on = %q, want %q", step, name, got.ExpOn, wantExp[id])
			}
			if wantOverdue[id] {
				overdue = append(overdue, id)
			}
		}
		p9Want(t, step+", overdue only", p9Report(t, env, env.Priya, purchase.PendingFilter{OverdueOnly: true}), overdue...)
		if n := p9Overdue(t, env, env.Priya); n != len(overdue) {
			t.Errorf("%s: OverdueCount = %d, want %d", step, n, len(overdue))
		}
	}

	check("with B's Expected on",
		map[int64]bool{la: true, lb: false, lc: true, ld: false, le: false},
		map[int64]string{lb: "2026-11-05", lc: "2026-10-30"})
	if got, ok := p9Find(t, "C", p9Report(t, env, env.Priya, purchase.PendingFilter{}), lc); ok && got.RequiredBy != "" {
		t.Errorf("C Required by = %q, want blank", got.RequiredBy)
	}

	if err := env.Service.ClearExpected(ctx, env.Arjun, b.ID, 1); err != nil {
		t.Fatalf("ClearExpected(B, version 1): %v", err)
	}
	check("after clearing B's Expected on",
		map[int64]bool{la: true, lb: true, lc: true, ld: false, le: false},
		map[int64]string{lc: "2026-10-30"})
}
