package purchase_test

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Helpers for P2's tests (MOD-PO P2, order Validate). Every document row is
// written through Engine.Do or the purchasetest builders; the SQL here only
// reads, except p2SetCopyNotes (see its comment).

// p2OrderDate is the fixture clock's business date (authtest.T0).
const p2OrderDate = "2026-10-24"

// p2Kit is a mstkit.Env over the purchase env, for master edits in tests.
func p2Kit(env *purchasetest.Env) mstkit.Env {
	return mstkit.Env{DB: env.DB, Auth: env.Auth, Engine: env.Engine, Auditor: audit.Default, Clock: env.Clock}
}

// p2Int parses a master id that masterID returned.
func p2Int(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("id %q: %v", s, err)
	}
	return n
}

// p2AddressID is the id of party's address in city (msttest's seed titles
// each party address by its place).
func p2AddressID(t *testing.T, env *purchasetest.Env, party, city string) string {
	t.Helper()
	var id int64
	if err := env.DB.SQL().QueryRowContext(t.Context(),
		`SELECT a.id FROM address a JOIN party p ON p.id = a.party_id WHERE p.code = ? AND a.city = ?`, party, city).Scan(&id); err != nil {
		t.Fatalf("address of %s in %s: %v", party, city, err)
	}
	return strconv.FormatInt(id, 10)
}

// p2PrimaryBilling is the id of party's primary billing address.
func p2PrimaryBilling(t *testing.T, env *purchasetest.Env, party string) string {
	t.Helper()
	var id int64
	if err := env.DB.SQL().QueryRowContext(t.Context(),
		`SELECT a.id FROM address a JOIN party p ON p.id = a.party_id WHERE p.code = ? AND a.is_primary_billing = 1`, party).Scan(&id); err != nil {
		t.Fatalf("primary billing address of %s: %v", party, err)
	}
	return strconv.FormatInt(id, 10)
}

// p2Line is one order line of qty of item in unit at price, tax code GST18
// unless taxCode is given.
func p2Line(t *testing.T, env *purchasetest.Env, item, unit, qty, price string, taxCode ...string) map[string]string {
	t.Helper()
	tc := "GST18"
	if len(taxCode) > 0 {
		tc = taxCode[0]
	}
	return map[string]string{
		"item_id": masterID(t, env.DB, "item", item), "uom_id": masterID(t, env.DB, "uom", unit),
		"qty": qty, "price": price, "tax_code_id": masterID(t, env.DB, "tax_code", tc),
	}
}

// p2RefLines are the reference PO's lines (00-fixtures.md "Reference PO"):
// MS-ANGLE-50 1,200.000 kg at ₹62.5000 and WIRE-18G 50.000 kg at ₹96.0000,
// both 18%.
func p2RefLines(t *testing.T, env *purchasetest.Env) []map[string]string {
	t.Helper()
	return []map[string]string{
		p2Line(t, env, "MS-ANGLE-50", "KGS", "1200", "62.5"),
		p2Line(t, env, "WIRE-18G", "KGS", "50", "96"),
	}
}

// p2Header is an order header for vendor, site and project (codes; "" leaves
// the field blank), dated today.
func p2Header(t *testing.T, env *purchasetest.Env, vendor, site, project string) map[string]string {
	t.Helper()
	h := map[string]string{"vendor_id": "", "site_id": "", "project_id": "", "order_date": p2OrderDate}
	if vendor != "" {
		h["vendor_id"] = masterID(t, env.DB, "party", vendor)
	}
	if site != "" {
		h["site_id"] = masterID(t, env.DB, "site", site)
	}
	if project != "" {
		h["project_id"] = masterID(t, env.DB, "project", project)
	}
	return h
}

// p2Create creates an order draft as p through the engine.
func p2Create(t *testing.T, env *purchasetest.Env, p auth.Principal, values map[string]string, lines []map[string]string) (doc.Result, error) {
	t.Helper()
	return env.Engine.Do(t.Context(), p, purchase.DocType, 0, doc.ActionSave, doc.ActionInput{Values: values, Lines: lines})
}

// p2Version is the stored version of order id.
func p2Version(t *testing.T, env *purchasetest.Env, id int64) int {
	t.Helper()
	var v int
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT version FROM purchase_order WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatalf("version of order %d: %v", id, err)
	}
	return v
}

// p2KeepLines posts each stored line of id unchanged: DOC treats nil Lines as
// delete-all (CD-256 (3)) and an id-only line as blank, so each carries its
// id and item_id.
func p2KeepLines(t *testing.T, env *purchasetest.Env, id int64) []map[string]string {
	t.Helper()
	var lines []map[string]string
	for _, l := range p2Load(t, env, id).Lines {
		lines = append(lines, map[string]string{"id": strconv.FormatInt(l.ID, 10), "item_id": p2Plain(l.Values["item_id"])})
	}
	return lines
}

// p2Save saves draft id as p at its stored version (a page opened just now).
// nil lines keep the stored lines (p2KeepLines); lines without ids replace them.
func p2Save(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64, values map[string]string, lines []map[string]string) (doc.Result, error) {
	t.Helper()
	if lines == nil {
		lines = p2KeepLines(t, env, id)
	}
	return env.Engine.Do(t.Context(), p, purchase.DocType, id, doc.ActionSave,
		doc.ActionInput{Version: p2Version(t, env, id), Values: values, Lines: lines})
}

// p2Do runs action a on order id as p at its stored version.
func p2Do(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64, a doc.Action, reason string) (doc.Result, error) {
	t.Helper()
	return env.Engine.Do(t.Context(), p, purchase.DocType, id, a, doc.ActionInput{Version: p2Version(t, env, id), Reason: reason})
}

// p2Must stops the test when an engine call it relies on fails:
// p2Must(t, what)(env.Engine.Do(…)).
func p2Must(t *testing.T, what string) func(doc.Result, error) doc.Result {
	t.Helper()
	return func(res doc.Result, err error) doc.Result {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		return res
	}
}

// p2Load reads order id as DOC stores it.
func p2Load(t *testing.T, env *purchasetest.Env, id int64) doc.Doc {
	t.Helper()
	var d doc.Doc
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		d, err = doc.LoadDoc(t.Context(), tx, purchase.DocType, id)
		return err
	}); err != nil {
		t.Fatalf("load order %d: %v", id, err)
	}
	return d
}

// p2Money formats a stored Money value; a missing one reads <nil>.
func p2Money(v any) string {
	m, ok := v.(types.Money)
	if !ok {
		return fmt.Sprintf("<%v>", v)
	}
	return types.FormatMoney(m)
}

// p2Plain formats any stored value in its plain form; nil reads <nil>.
func p2Plain(v any) string {
	switch x := v.(type) {
	case nil:
		return "<nil>"
	case types.Money:
		return x.Plain()
	case types.Qty:
		return x.Plain()
	case types.Rate:
		return x.Plain()
	case types.Percent:
		return x.Plain()
	case int64:
		return strconv.FormatInt(x, 10)
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	}
	return fmt.Sprintf("%v", v)
}

// p2Snap reads header columns of order id as text ("" for NULL).
func p2Snap(t *testing.T, env *purchasetest.Env, id int64, cols ...string) map[string]string {
	t.Helper()
	sel := make([]string, len(cols))
	for i, c := range cols {
		sel[i] = "COALESCE(CAST(" + c + " AS TEXT), '')"
	}
	vals := make([]string, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT `+strings.Join(sel, ", ")+` FROM purchase_order WHERE id = ?`, id).Scan(ptrs...); err != nil {
		t.Fatalf("read %v of order %d: %v", cols, id, err)
	}
	out := map[string]string{}
	for i, c := range cols {
		out[c] = vals[i]
	}
	return out
}

// p2LineSnap reads line columns of order id as text, in line_no order.
func p2LineSnap(t *testing.T, env *purchasetest.Env, id int64, cols ...string) []map[string]string {
	t.Helper()
	sel := make([]string, len(cols))
	for i, c := range cols {
		sel[i] = "COALESCE(CAST(" + c + " AS TEXT), '')"
	}
	rows, err := env.DB.SQL().QueryContext(t.Context(), `SELECT `+strings.Join(sel, ", ")+` FROM purchase_order_line WHERE parent_id = ? ORDER BY line_no`, id)
	if err != nil {
		t.Fatalf("read %v of order %d's lines: %v", cols, id, err)
	}
	defer func() { _ = rows.Close() }()
	var out []map[string]string
	for rows.Next() {
		vals := make([]string, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for i, c := range cols {
			m[c] = vals[i]
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// p2SetCopyNotes writes copy_notes the way P12's Copy does: copy_notes is a
// System column (DOC ignores it on input, DOC-003), so no engine call can set
// it. Like purchasetest.SetReceived it stands in for another task's write; it
// changes one column of an existing row and inserts nothing.
func p2SetCopyNotes(t *testing.T, env *purchasetest.Env, id int64, notes string) {
	t.Helper()
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE purchase_order SET copy_notes = ? WHERE id = ?`, notes, id); err != nil {
		t.Fatalf("set copy_notes of order %d: %v", id, err)
	}
}

// p2Counts are the row counts a refused write must leave unchanged.
type p2Counts struct {
	orders, lines, audits, requests, history int
}

func p2CountAll(t *testing.T, env *purchasetest.Env) p2Counts {
	t.Helper()
	return p2Counts{
		orders:   countRows(t, env.DB, "purchase_order", "1 = 1"),
		lines:    countRows(t, env.DB, "purchase_order_line", "1 = 1"),
		audits:   countRows(t, env.DB, "audit_log", "1 = 1"),
		requests: countRows(t, env.DB, "approval_requests", "1 = 1"),
		history:  countRows(t, env.DB, "approval_history", "1 = 1"),
	}
}

// p2SameCounts reports a refused write that changed a row count.
func p2SameCounts(t *testing.T, what string, before p2Counts, env *purchasetest.Env) {
	t.Helper()
	if after := p2CountAll(t, env); after != before {
		t.Errorf("%s: row counts %+v, want %+v unchanged (rolled back)", what, after, before)
	}
}

// p2WantInvalid asserts err is DOC's invalid error (422) whose field errors
// are exactly want, in any order. A want Field of "a|b" accepts either name.
func p2WantInvalid(t *testing.T, what string, err error, want ...doctype.FieldError) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) || de.Kind != doc.KindInvalid || de.Status() != 422 {
		t.Errorf("%s: err = %v, want an invalid error (422) with %+v", what, err, want)
		return
	}
	got := slices.Clone(de.Fields)
	for _, w := range want {
		i := slices.IndexFunc(got, func(g doctype.FieldError) bool {
			return g.Line == w.Line && g.Message == w.Message && slices.Contains(strings.Split(w.Field, "|"), g.Field)
		})
		if i < 0 {
			t.Errorf("%s: field errors %+v lack %+v", what, de.Fields, w)
			continue
		}
		got = slices.Delete(got, i, i+1)
	}
	if len(got) > 0 {
		t.Errorf("%s: unexpected field errors %+v (want exactly %+v)", what, got, want)
	}
}

// p2Warnings is env.Service.OrderWarnings for p, failing on an error.
func p2Warnings(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64) []string {
	t.Helper()
	w, err := env.Service.OrderWarnings(t.Context(), p, id)
	if err != nil {
		t.Fatalf("OrderWarnings(%s, %d): %v", p.Name, id, err)
	}
	return w
}
