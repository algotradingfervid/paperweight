package goodsreceipt_test

import (
	"cmp"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN G2 test helpers. Every GRN here is made and changed through
// Engine.Do (create, save, step save, submit, amend), never by inserting
// rows, so deleting a call site in validateReceipt or submitFields fails a
// test (CD-272 research §2 pattern 2).

// G2's texts, copied from GRN spec §4 and the plan's G2 section (CD-264 (8):
// line texts start "Line {n}: "); the total text is CD-308 as CD-311 amends
// it (lowercase name with its article, as expense built it).
const (
	g2MsgChoosePO     = "Choose a purchase order."
	g2MsgDeleteDraft  = "Delete this draft to receive against another order."
	g2MsgFuture       = "Receipt date can't be in the future."
	g2MsgChallanAfter = "Challan date can't be after the receipt date."
	g2MsgChallanNo    = "Challan or invoice no. is required for goods."
	g2MsgChallanKind  = "Choose Delivery challan or Tax invoice."
	g2MsgTotal        = "The total is too large. The most a goods receipt can be is ₹10,00,00,00,000.00. Split it into more than one goods receipt."
)

// g2RefPO is the reference PO (00-fixtures.md, grn.md): raised by Priya for
// SHREE on METRO-SITE / PRJ-METRO and approved, line 1 MS-ANGLE-50
// 1,200.000 kg at ₹62.5000, line 2 WIRE-18G 50.000 kg at ₹96.0000. It is the
// first PO of each test, so it is PO/26-27/00001.
func g2RefPO(t *testing.T, env *grntest.Env) doc.Doc {
	t.Helper()
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(
		map[string]string{"qty": "1200", "price": "62.5"},
		map[string]string{"item_id": g1MasterID(t, env.DB, "item", "WIRE-18G"), "qty": "50", "price": "96", "hsn_sac": "7217"},
	))
	if po.Number != "PO/26-27/00001" || len(po.Lines) != 2 {
		t.Fatalf("reference PO = %q with %d lines, want PO/26-27/00001 with 2", po.Number, len(po.Lines))
	}
	return po
}

// g2ServicePO is an approved PO by Priya whose only line is SVC-CRANE, 8
// hours at ₹1,500.00.
func g2ServicePO(t *testing.T, env *grntest.Env) doc.Doc {
	t.Helper()
	return purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(map[string]string{
		"item_id": g1MasterID(t, env.DB, "item", "SVC-CRANE"), "uom_id": g1MasterID(t, env.DB, "uom", "HRS"),
		"qty": "8", "price": "1500", "hsn_sac": "997313",
	}))
}

// g2Header is the GRN header step 1 and step 2 post for po: the PO, its
// site and project, today's receipt date and a delivery challan. Tests
// change or delete keys to make the case.
func g2Header(t *testing.T, env *grntest.Env, po doc.Doc) map[string]string {
	t.Helper()
	return map[string]string{
		"purchase_order_id": strconv.FormatInt(po.ID, 10),
		"site_id":           g1Int(t, po.Header["site_id"]),
		"project_id":        g1Int(t, po.Header["project_id"]),
		"receipt_date":      types.Today(env.Clock).ISO(),
		"challan_kind":      "Delivery challan",
		"challan_no":        "DC-1042",
	}
}

// g2Line is one posted GRN line on PO line poLine. A blank rejected posts
// neither rejected_qty nor reject_reason.
func g2Line(poLine doc.Line, received, rejected, reason string) map[string]string {
	m := map[string]string{"po_line_id": strconv.FormatInt(poLine.ID, 10), "received_qty": received}
	if rejected != "" {
		m["rejected_qty"], m["reject_reason"] = rejected, reason
	}
	return m
}

// g2Create creates a GRN as p through the engine.
func g2Create(t *testing.T, env *grntest.Env, p auth.Principal, header map[string]string, lines ...map[string]string) (doc.Result, error) {
	t.Helper()
	return env.Engine.Do(t.Context(), p, goodsreceipt.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: header, Lines: lines})
}

// g2MustCreate is g2Create that stops the test on any error.
func g2MustCreate(t *testing.T, env *grntest.Env, p auth.Principal, header map[string]string, lines ...map[string]string) doc.Result {
	t.Helper()
	res, err := g2Create(t, env, p, header, lines...)
	if err != nil {
		t.Fatalf("create GRN as %s: %v %+v", p.Name, err, g1FieldErrors(err))
	}
	return res
}

// g2Save saves draft id as p at its stored version with header and lines;
// only, when given, makes it a step save (CD-205, CD-207).
func g2Save(t *testing.T, env *grntest.Env, p auth.Principal, id int64, header map[string]string, lines []map[string]string, only ...string) (doc.Result, error) {
	t.Helper()
	in := doc.ActionInput{Version: env.Load(t, goodsreceipt.DocType, id).Version, Values: header, Lines: lines}
	if len(only) > 0 {
		in.Only = only
	}
	return env.Engine.Do(t.Context(), p, goodsreceipt.DocType, id, doctype.ActionSave, in)
}

// g2Submit submits GRN id as p at its stored version.
func g2Submit(t *testing.T, env *grntest.Env, p auth.Principal, id int64) (doc.Result, error) {
	t.Helper()
	return env.Engine.Do(t.Context(), p, goodsreceipt.DocType, id, doctype.ActionSubmit,
		doc.ActionInput{Version: env.Load(t, goodsreceipt.DocType, id).Version})
}

// g2StoredLines are draft id's stored lines as the page posts them back
// (with their ids, so they are kept), with change[n] merged over line n
// (1-based).
func g2StoredLines(t *testing.T, env *grntest.Env, id int64, change map[int]map[string]string) []map[string]string {
	t.Helper()
	var out []map[string]string
	for i, l := range env.Load(t, goodsreceipt.DocType, id).Lines {
		m := map[string]string{"id": strconv.FormatInt(l.ID, 10)}
		for _, f := range []string{"po_line_id", "received_qty", "rejected_qty", "reject_reason"} {
			m[f] = g2Form(t, l.Values[f])
		}
		for k, v := range change[i+1] {
			m[k] = v
		}
		out = append(out, m)
	}
	return out
}

// g2Form is a stored value as a form value.
func g2Form(t *testing.T, v any) string {
	t.Helper()
	switch v := v.(type) {
	case nil:
		return ""
	case int64:
		return strconv.FormatInt(v, 10)
	case types.Qty:
		return v.Plain()
	case string:
		return v
	}
	t.Fatalf("stored value %v (%T) has no form value here", v, v)
	return ""
}

// g2SortErrs sorts field errors by line, field, then message.
func g2SortErrs(errs []doctype.FieldError) []doctype.FieldError {
	out := slices.Clone(errs)
	slices.SortFunc(out, func(a, b doctype.FieldError) int {
		return cmp.Or(cmp.Compare(a.Line, b.Line), cmp.Compare(a.Field, b.Field), cmp.Compare(a.Message, b.Message))
	})
	return out
}

// g2WantInvalid asserts err is the engine's 422 with exactly the field
// errors want (in any order): each text, field key and line number.
func g2WantInvalid(t *testing.T, what string, err error, want ...doctype.FieldError) {
	t.Helper()
	var he doctype.HTTPError
	if err == nil || !errors.As(err, &he) {
		t.Errorf("%s: err = %v, want a 422 with %+v", what, err, want)
		return
	}
	if he.Status() != 422 {
		t.Errorf("%s: status %d (%v), want 422", what, he.Status(), err)
	}
	if got := g2SortErrs(he.FieldErrors()); !slices.Equal(got, g2SortErrs(want)) {
		t.Errorf("%s: field errors\n got %+v\nwant %+v", what, got, g2SortErrs(want))
	}
}

// g2WantProblems asserts the step save committed (err nil) and its
// Result.Problems hold each of want: the failing check ran and its error
// was out of the step's scope, so the save kept Validate's writes (CD-302).
func g2WantProblems(t *testing.T, what string, res doc.Result, err error, want ...doctype.FieldError) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v %+v, want the step save committed with problems %+v", what, err, g1FieldErrors(err), want)
	}
	for _, w := range want {
		if !slices.Contains(res.Problems, w) {
			t.Errorf("%s: problems %+v lack %+v", what, res.Problems, w)
		}
	}
}

// g2Query runs q and returns every row's columns as text ("" for NULL).
func g2Query(t *testing.T, d *db.DB, q string, args ...any) [][]string {
	t.Helper()
	rows, err := d.SQL().QueryContext(t.Context(), q, args...)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = v.String
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// g2Same reports whether a stored derived value matches what it is derived
// from: equal, or both cleared (NULL, empty text or 0 where the source is NULL or
// blank).
func g2Same(got, want string) bool {
	if want == "" {
		return got == "" || got == "0"
	}
	return got == want
}

// g2Int parses a stored integer ("" is 0).
func g2Int(t *testing.T, s string) int64 {
	t.Helper()
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("stored integer %q: %v", s, err)
	}
	return n
}

// g2HeaderPairs are each derived header column of the GRN and what it is
// derived from on the stored PO (purchase_order_id): GRN spec §3.1, plan G2
// Do 1 "Copies from the PO".
var g2HeaderPairs = [][2]string{
	{"g.vendor_id", "o.vendor_id"},
	{"g.site_id", "o.site_id"},
	{"g.project_id", "o.project_id"},
	{"g.snap_vendor_name", "o.snap_vendor_name"},
	{"g.snap_vendor_gstin", "o.snap_vendor_gstin"},
	{"g.snap_vendor_address", "o.snap_vendor_address"},
	{"g.snap_po_number", "o.number"},
	{"g.snap_po_date", "o.order_date"},
	{"g.snap_site_name", "o.snap_site_name"},
	{"g.snap_project_code", "pr.code"},
	{"g.snap_branch_id", "o.branch_id"},
}

// g2LinePairs are each derived line column and what it is derived from on
// the line's own PO line (po_line_id) and its item (spec §3.2; snap_rate is
// the PO line's net rate; snap_is_service from the item master's type).
var g2LinePairs = [][2]string{
	{"l.uom_id", "pl.uom_id"},
	{"l.snap_item_code", "pl.snap_item_code"},
	{"l.snap_item_name", "pl.snap_item_name"},
	{"l.snap_hsn_sac", "pl.hsn_sac"},
	{"l.snap_uom_code", "pl.snap_uom_code"},
	{"l.snap_rate", "pl.rate"},
	{"l.snap_is_service", "CASE i.item_type WHEN 'Service' THEN 1 WHEN 'Goods' THEN 0 END"},
}

// g2CheckDerived asserts GRN id holds no stale derived value (CD-302,
// CD-303): vendor_id, site_id, project_id and the header snapshots match
// the stored purchase_order_id's PO (cleared when it is blank); each line's
// uom_id and snapshots match its own PO line; accepted_qty is received −
// rejected, or 0 when rejected is above received; accepted_value is the sum
// over the stored lines of accepted × snap_rate (gstcalc.LineAmount's
// rounding), or cleared when that sum is above types.MaxMoney (CD-308).
func g2CheckDerived(t *testing.T, what string, env *grntest.Env, id int64) {
	t.Helper()
	q := "SELECT "
	for i, p := range g2HeaderPairs {
		if i > 0 {
			q += ", "
		}
		q += p[0] + ", " + p[1]
	}
	q += `, g.accepted_value FROM goods_receipt g
		LEFT JOIN purchase_order o ON o.id = g.purchase_order_id
		LEFT JOIN project pr ON pr.id = o.project_id WHERE g.id = ?`
	rows := g2Query(t, env.DB, q, id)
	if len(rows) != 1 {
		t.Fatalf("%s: GRN %d not found", what, id)
	}
	h := rows[0]
	for i, p := range g2HeaderPairs {
		if got, want := h[2*i], h[2*i+1]; !g2Same(got, want) {
			t.Errorf("%s: %s = %q, want %q from the stored PO (%s)", what, p[0], got, want, p[1])
		}
	}
	storedValue := h[len(h)-1]

	lq := "SELECT l.line_no, COALESCE(l.received_qty, 0), COALESCE(l.rejected_qty, 0), l.accepted_qty"
	for _, p := range g2LinePairs {
		lq += ", " + p[0] + ", " + p[1]
	}
	lq += ` FROM goods_receipt_line l
		LEFT JOIN purchase_order_line pl ON pl.id = l.po_line_id
		LEFT JOIN item i ON i.id = pl.item_id WHERE l.parent_id = ? ORDER BY l.line_no`
	var sum int64
	over := false
	for _, l := range g2Query(t, env.DB, lq, id) {
		for i, p := range g2LinePairs {
			if got, want := l[4+2*i], l[5+2*i]; !g2Same(got, want) {
				t.Errorf("%s: line %s %s = %q, want %q from its own PO line (%s)", what, l[0], p[0], got, want, p[1])
			}
		}
		received, rejected := g2Int(t, l[1]), g2Int(t, l[2])
		accepted := received - rejected
		if rejected > received {
			accepted = 0
		}
		if l[3] != strconv.FormatInt(accepted, 10) {
			t.Errorf("%s: line %s accepted_qty = %q, want %d (received %d − rejected %d, 0 when rejected is more)",
				what, l[0], l[3], accepted, received, rejected)
		}
		rateCol := 4 + 2*slices.IndexFunc(g2LinePairs, func(p [2]string) bool { return p[0] == "l.snap_rate" })
		amount, err := types.Qty(accepted).Times(types.Rate(g2Int(t, l[rateCol])))
		if err != nil {
			over = true // a line amount past the maximum puts the total over it (CD-308)
		}
		sum += int64(amount)
	}
	if over || sum > int64(types.MaxMoney) {
		if !g2Same(storedValue, "") {
			t.Errorf("%s: accepted_value = %q, want cleared: the lines sum to %d paise, above the money maximum (CD-308)", what, storedValue, sum)
		}
		return
	}
	if storedValue != strconv.FormatInt(sum, 10) {
		t.Errorf("%s: accepted_value = %q, want %d paise, the sum of the stored lines", what, storedValue, sum)
	}
}

// g2Column reads one column of GRN id's header as text.
func g2Column(t *testing.T, env *grntest.Env, id int64, col string) string {
	t.Helper()
	rows := g2Query(t, env.DB, `SELECT `+col+` FROM goods_receipt WHERE id = ?`, id) //nolint:gosec // G202: fixed column names
	if len(rows) != 1 {
		t.Fatalf("GRN %d not found", id)
	}
	return rows[0][0]
}

// g2LineColumn reads one column of GRN id's lines, in line order.
func g2LineColumn(t *testing.T, env *grntest.Env, id int64, col string) []string {
	t.Helper()
	var out []string
	for _, r := range g2Query(t, env.DB, `SELECT `+col+` FROM goods_receipt_line WHERE parent_id = ? ORDER BY line_no`, id) { //nolint:gosec // G202: fixed column names
		out = append(out, r[0])
	}
	return out
}
