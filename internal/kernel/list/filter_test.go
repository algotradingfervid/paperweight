package list_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

var noScope = auth.Predicate{}

func date(t *testing.T, iso string) types.Date {
	t.Helper()
	d, err := types.DateFromISO(iso)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func filter(field string, op list.Op, values ...string) list.Filter {
	return list.Filter{Field: field, Op: op, Values: values}
}

func query(fs ...list.Filter) list.Query { return list.Query{Filters: fs} }

// sameIDs compares id sets ignoring order.
func sameIDs(t *testing.T, what string, got []int64, want ...int64) {
	t.Helper()
	g, w := slices.Clone(got), slices.Clone(want)
	slices.Sort(g)
	slices.Sort(w)
	if !slices.Equal(g, w) {
		t.Errorf("%s: ids = %v, want %v", what, g, w)
	}
}

// wantQueryError checks that Build rejects q with msg as a 422 QueryError.
func wantQueryError(t *testing.T, dt *doctype.DocType, q list.Query, msg string) {
	t.Helper()
	_, _, err := list.Build(dt, q, noScope)
	if err == nil {
		t.Fatalf("Build(%+v) = nil error, want %q", q.Filters, msg)
	}
	if err.Error() != msg {
		t.Errorf("Build(%+v) error = %q, want %q", q.Filters, err.Error(), msg)
	}
	if !errors.Is(err, list.ErrBadQuery) {
		t.Errorf("errors.Is(%v, ErrBadQuery) = false", err)
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != 422 || he.FieldErrors() != nil {
		t.Errorf("error %v is not a 422 HTTPError with no field errors", err)
	}
	if _, _, err := list.BuildCount(dt, q, noScope); err == nil || err.Error() != msg {
		t.Errorf("BuildCount error = %v, want %q", err, msg)
	}
}

// seedStatusPOs inserts 3 draft, 2 pending_approval (number NULL) and 5
// approved POs numbered PO/26-27/00001…00005.
func seedStatusPOs(t *testing.T, d *db.DB) (unnumbered, approved []int64) {
	for range 3 {
		unnumbered = append(unnumbered, listtest.InsertPO(t, d, listtest.PORow{Status: "draft"}))
	}
	for range 2 {
		unnumbered = append(unnumbered, listtest.InsertPO(t, d, listtest.PORow{Status: "pending_approval"}))
	}
	for i := 1; i <= 5; i++ {
		approved = append(approved, listtest.InsertPO(t, d, listtest.PORow{
			Status: "approved", Number: fmt.Sprintf("PO/26-27/%05d", i),
		}))
	}
	return unnumbered, approved
}

func TestList_FilterEquals(t *testing.T) { // NUM-100
	d := listtest.Setup(t)
	unnumbered, approved := seedStatusPOs(t, d)

	ids, total := listtest.Run(t, d, listtest.PO(), query(filter("status", list.OpEq, "approved")), noScope)
	sameIDs(t, "status = approved", ids, approved...)
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}

	// Unfiltered: unnumbered rows display as "Draft <id>" (CD-59).
	q, args, err := list.Build(listtest.PO(), list.Query{}, noScope)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[int64]string{}
	ctx := context.Background()
	err = d.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, q, args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		cols, _ := rows.Columns()
		for rows.Next() {
			var id int64
			var number sql.NullString
			dest := []any{&id}
			for _, c := range cols[1:] {
				if c == "number" {
					dest = append(dest, &number)
				} else {
					dest = append(dest, new(any))
				}
			}
			if err := rows.Scan(dest...); err != nil {
				return err
			}
			labels[id] = naming.Label(number.String, id)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range unnumbered {
		if want := fmt.Sprintf("Draft %d", id); labels[id] != want {
			t.Errorf("label of %d = %q, want %q", id, labels[id], want)
		}
	}
	if labels[approved[0]] != "PO/26-27/00001" {
		t.Errorf("label of first approved = %q, want PO/26-27/00001", labels[approved[0]])
	}
}

func TestList_NotEqualsIncludesEmpty(t *testing.T) { // NUM-101
	d := listtest.Setup(t)
	ids := listtest.SeedFixtures(t, d)
	var want []int64
	for range 4 {
		want = append(want, listtest.InsertExpense(t, d, listtest.ExpenseRow{ProjectID: ids["PRJ-METRO"], Amount: 100}))
	}
	for range 2 {
		listtest.InsertExpense(t, d, listtest.ExpenseRow{ProjectID: ids["PRJ-TECHPARK"], Amount: 100})
	}
	for range 3 {
		want = append(want, listtest.InsertExpense(t, d, listtest.ExpenseRow{Amount: 100}))
	}
	got, total := listtest.Run(t, d, listtest.Expense(),
		query(filter("project_id", list.OpNe, fmt.Sprint(ids["PRJ-TECHPARK"]))), noScope)
	sameIDs(t, "project_id != TECHPARK", got, want...)
	if total != 7 {
		t.Errorf("total = %d, want 7", total)
	}
}

func TestList_ContainsLiteral(t *testing.T) { // NUM-102
	d := listtest.Setup(t)
	ids := listtest.SeedFixtures(t, d)
	steelco := listtest.InsertParty(t, d, listtest.PartyRow{Code: "STEELCO", Name: "STEELCO Traders"})
	ab := listtest.InsertParty(t, d, listtest.PartyRow{Code: "AB1", Name: "a_b"})
	listtest.InsertParty(t, d, listtest.PartyRow{Code: "AXB", Name: "axb"})
	advance := listtest.InsertParty(t, d, listtest.PartyRow{Code: "ADV", Name: "Advance Co", Remarks: "50% advance"})
	listtest.InsertParty(t, d, listtest.PartyRow{Code: "UNITS", Name: "Units Co", Remarks: "500 units"})

	got, _ := listtest.Run(t, d, listtest.Party(), query(filter("name", list.OpContains, "steel")), noScope)
	sameIDs(t, "name contains steel", got, ids["SHREE"], steelco)
	got, _ = listtest.Run(t, d, listtest.Party(), query(filter("remarks", list.OpContains, "50%")), noScope)
	sameIDs(t, "remarks contains 50%", got, advance)
	got, _ = listtest.Run(t, d, listtest.Party(), query(filter("name", list.OpContains, "a_b")), noScope)
	sameIDs(t, "name contains a_b", got, ab)
}

func TestList_InNotIn(t *testing.T) { // NUM-103
	d := listtest.Setup(t)
	unnumbered, _ := seedStatusPOs(t, d)
	got, total := listtest.Run(t, d, listtest.PO(),
		query(filter("status", list.OpIn, "draft", "pending_approval")), noScope)
	sameIDs(t, "status in [draft, pending_approval]", got, unnumbered...)
	if total != 5 {
		t.Errorf("in total = %d, want 5", total)
	}
	if _, total := listtest.Run(t, d, listtest.PO(), query(filter("status", list.OpIn)), noScope); total != 0 {
		t.Errorf("in [] total = %d, want 0", total)
	}
	if _, total := listtest.Run(t, d, listtest.PO(), query(filter("status", list.OpNotIn)), noScope); total != 10 {
		t.Errorf("not in [] total = %d, want 10", total)
	}

	listtest.SeedFixtures(t, d)
	shah := listtest.InsertParty(t, d, listtest.PartyRow{Code: "SHAHRAO", Name: "Shah, Rao & Co"})
	got, _ = listtest.Run(t, d, listtest.Party(), query(filter("name", list.OpIn, "Shah, Rao & Co")), noScope)
	sameIDs(t, "name in [Shah, Rao & Co]", got, shah)
}

func TestList_DateBetween(t *testing.T) { // NUM-104
	d := listtest.Setup(t)
	var po []int64
	for _, iso := range []string{"2026-03-31", "2026-04-01", "2026-04-30", "2026-05-01"} {
		po = append(po, listtest.InsertPO(t, d, listtest.PORow{Date: date(t, iso)}))
	}
	got, _ := listtest.Run(t, d, listtest.PO(), query(filter("date", list.OpBetween, "01-04-2026", "30-04-2026")), noScope)
	sameIDs(t, "date between", got, po[1], po[2])

	wantQueryError(t, listtest.PO(), query(filter("date", list.OpBetween, "30-04-2026", "01-04-2026")),
		"The start date must be on or before the end date.")
	wantQueryError(t, listtest.PO(), query(filter("date", list.OpBetween, "01-04-2026")),
		"Choose both a start and an end date.")
}

func seedCreatedPOs(t *testing.T, d *db.DB) []int64 {
	var po []int64
	for _, ts := range []string{"2026-03-31T18:15:00.000Z", "2026-03-31T18:45:00.000Z", "2026-04-01T18:29:59.000Z"} {
		po = append(po, listtest.InsertPO(t, d, listtest.PORow{CreatedAt: ts}))
	}
	return po
}

func TestList_TimestampDayRangeIST(t *testing.T) { // NUM-105
	d := listtest.Setup(t)
	po := seedCreatedPOs(t, d)
	q := query(filter("created_at", list.OpBetween, "01-04-2026", "01-04-2026"))
	got, _ := listtest.Run(t, d, listtest.PO(), q, noScope)
	sameIDs(t, "created between 01-04-2026 and 01-04-2026", got, po[1], po[2])
	_, args, err := list.Build(listtest.PO(), q, noScope)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2026-03-31T18:30:00.000Z", "2026-04-01T18:30:00.000Z"} {
		if !slices.Contains(args, any(want)) {
			t.Errorf("args %v do not contain %q", args, want)
		}
	}
}

func TestList_TimestampEqualsWholeISTDay(t *testing.T) { // Review Focus 3
	d := listtest.Setup(t)
	po := seedCreatedPOs(t, d)
	got, _ := listtest.Run(t, d, listtest.PO(), query(filter("created_at", list.OpEq, "01-04-2026")), noScope)
	sameIDs(t, "created = 01-04-2026", got, po[1], po[2])
}

func TestList_MoneyComparison(t *testing.T) { // NUM-106
	d := listtest.Setup(t)
	var po []int64
	for _, m := range []types.Money{9999999, 10000000, 10000001} {
		po = append(po, listtest.InsertPO(t, d, listtest.PORow{GrandTotal: m}))
	}
	got, _ := listtest.Run(t, d, listtest.PO(), query(filter("grand_total", list.OpGe, "1,00,000")), noScope)
	sameIDs(t, "grand_total >= 1,00,000", got, po[1], po[2])
	wantQueryError(t, listtest.PO(), query(filter("grand_total", list.OpGe, "1,00,000.005")),
		"Amount can have at most 2 decimal places.")
}

func TestList_InValuesParsedPerType(t *testing.T) { // Review Focus 5
	d := listtest.Setup(t)
	var po []int64
	for _, m := range []types.Money{9999999, 10000000, 10000001} {
		po = append(po, listtest.InsertPO(t, d, listtest.PORow{GrandTotal: m}))
	}
	got, total := listtest.Run(t, d, listtest.PO(), query(filter("grand_total", list.OpIn, "1,00,000", "99,999.99")), noScope)
	sameIDs(t, "grand_total in", got, po[0], po[1])
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	wantQueryError(t, listtest.PO(), query(filter("grand_total", list.OpIn, "1,00,000", "abc")),
		`"abc" is not a valid amount. Write it like 1,234.50 or 1234.50.`)
}

func TestList_IsEmpty(t *testing.T) { // NUM-107
	d := listtest.Setup(t)
	ids := listtest.SeedFixtures(t, d)
	null := listtest.InsertParty(t, d, listtest.PartyRow{Code: "UNREG1", Name: "Unregistered One"})
	blank := listtest.Insert(t, d, "numt_party", map[string]any{"code": "UNREG2", "name": "Unregistered Two", "gstin": ""})

	got, _ := listtest.Run(t, d, listtest.Party(), query(filter("gstin", list.OpIsEmpty)), noScope)
	sameIDs(t, "gstin is empty", got, ids["LOCAL"], null, blank)
	got, _ = listtest.Run(t, d, listtest.Party(), query(filter("gstin", list.OpIsNotEmpty)), noScope)
	sameIDs(t, "gstin is not empty", got, ids["SHREE"], ids["BHARAT"], ids["RAJKOT"], ids["COROM"])

	listtest.InsertExpense(t, d, listtest.ExpenseRow{Amount: 0})
	if got, total := listtest.Run(t, d, listtest.Expense(), query(filter("amount", list.OpIsEmpty)), noScope); total != 0 || len(got) != 0 {
		t.Errorf("amount is empty: %v rows, total %d, want none", got, total)
	}
}

func TestList_FiltersAndSearchAreANDed(t *testing.T) { // NUM-108
	d := listtest.Setup(t)
	var want []int64
	for i := range 10 {
		r := listtest.PORow{Status: "approved", PartyName: "Bharat Tools"}
		if i < 3 {
			r.PartyName = "Shree Steel Traders"
		}
		id := listtest.InsertPO(t, d, r)
		if i < 3 {
			want = append(want, id)
		}
	}
	for i := range 5 {
		r := listtest.PORow{Status: "draft", PartyName: "Rajkot Castings"}
		if i < 2 {
			r.PartyName = "Shree Steel Traders"
		}
		listtest.InsertPO(t, d, r)
	}
	q := query(filter("status", list.OpEq, "approved"))
	q.Search = "shree"
	got, total := listtest.Run(t, d, listtest.PO(), q, noScope)
	sameIDs(t, "approved + shree", got, want...)
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
}

func TestList_RejectUnknownFieldOrOperator(t *testing.T) { // NUM-109
	wantQueryError(t, listtest.PO(), query(filter("password_hash", list.OpEq, "x")),
		"You can't filter Purchase Orders by 'password_hash'.")
	wantQueryError(t, listtest.PO(), query(filter("grand_total; drop table party", list.OpEq, "1")),
		"You can't filter Purchase Orders by 'grand_total; drop table party'.")
	wantQueryError(t, listtest.PO(), query(filter("grand_total", list.OpContains, "5")),
		"'contains' can't be used on Grand Total. Use =, <, >, between.")
}

func TestList_NoSQLInjection(t *testing.T) { // NUM-110
	d := listtest.Setup(t)
	listtest.SeedFixtures(t, d)
	q := list.Query{Search: `'; DROP TABLE party;--`}
	if got, total := listtest.Run(t, d, listtest.Party(), q, noScope); total != 0 || len(got) != 0 {
		t.Errorf("injection search: %v, total %d, want none", got, total)
	}
	if got, total := listtest.Run(t, d, listtest.Party(), query(filter("name", list.OpEq, `x' OR '1'='1`)), noScope); total != 0 || len(got) != 0 {
		t.Errorf("injection filter: %v, total %d, want none", got, total)
	}
	if _, total := listtest.Run(t, d, listtest.Party(), list.Query{}, noScope); total != 5 {
		t.Errorf("numt_party has %d rows after the attempts, want 5", total)
	}
}

func TestList_BuildQualifiesWithTableName(t *testing.T) { // CD-16 (no case ID)
	scope := auth.Predicate{SQL: "numt_po.project_id = ?", Args: []any{7}}
	q, args, err := list.Build(listtest.PO(), list.Query{}, scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"FROM numt_po WHERE", "(numt_po.project_id = ?)"} {
		if !strings.Contains(q, want) {
			t.Errorf("Build SQL %q lacks %q", q, want)
		}
	}
	for _, bad := range []string{" AS t", " t."} {
		if strings.Contains(q, bad) {
			t.Errorf("Build SQL %q contains alias %q", q, bad)
		}
	}
	// Build ends with the LIMIT and OFFSET args; the scope args come just before.
	if len(args) < 3 || args[len(args)-3] != 7 {
		t.Errorf("Build args = %v, want the scope arg 7 before LIMIT/OFFSET", args)
	}
	cq, cargs, err := list.BuildCount(listtest.PO(), list.Query{}, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cq, "SELECT COUNT(*) FROM numt_po WHERE (numt_po.project_id = ?)") {
		t.Errorf("BuildCount SQL = %q", cq)
	}
	if len(cargs) == 0 || cargs[len(cargs)-1] != 7 {
		t.Errorf("BuildCount args = %v, want to end with 7", cargs)
	}

	q, _, err = list.Build(listtest.PO(), list.Query{}, auth.Predicate{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, "WHERE") {
		t.Errorf("Build with an empty predicate added a condition: %q", q)
	}
}

func TestList_ColumnsByPriority(t *testing.T) { // CD-42 (supporting, no case ID)
	var names []string
	for _, f := range list.Columns(listtest.PO()) {
		names = append(names, f.Name)
	}
	if want := []string{"number", "grand_total", "status", "date", "party_name"}; !slices.Equal(names, want) {
		t.Errorf("Columns(PO) = %v, want %v", names, want)
	}
}
