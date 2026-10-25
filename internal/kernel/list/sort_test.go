package list_test

import (
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/types"
)

// wantErr checks that err is a 422 QueryError with msg.
func wantErr(t *testing.T, what string, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: nil error, want %q", what, msg)
	}
	if err.Error() != msg {
		t.Errorf("%s: error = %q, want %q", what, err.Error(), msg)
	}
	var he doctype.HTTPError
	if !errors.Is(err, list.ErrBadQuery) || !errors.As(err, &he) || he.Status() != 422 {
		t.Errorf("%s: error %v is not a 422 QueryError", what, err)
	}
}

// sorted is a page-1 query sorted by keys.
func sorted(keys ...doctype.SortKey) list.Query { return list.Query{Sort: keys, Page: 1} }

func asc(field string) doctype.SortKey  { return doctype.SortKey{Field: field} }
func desc(field string) doctype.SortKey { return doctype.SortKey{Field: field, Desc: true} }

func orderIs(t *testing.T, what string, got []int64, want ...int64) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s: ids = %v, want %v", what, got, want)
	}
}

func TestList_SortWhitelist(t *testing.T) { // NUM-111
	for s, msg := range map[string]string{
		"sleep(1) asc":         "You can't sort Purchase Orders by 'sleep(1)'.",
		"grand_total sideways": "You can't sort Purchase Orders by 'grand_total sideways'.",
		"remarks asc":          "You can't sort Purchase Orders by 'remarks'.",
	} {
		_, err := list.ParseSort(listtest.PO(), s)
		wantErr(t, "ParseSort "+s, err, msg)
	}
	_, _, err := list.Build(listtest.PO(), sorted(asc("remarks")), noScope)
	wantErr(t, "Build sort remarks", err, "You can't sort Purchase Orders by 'remarks'.")

	keys, err := list.ParseSort(listtest.PO(), "grand_total desc")
	if err != nil {
		t.Fatal(err)
	}
	if want := []doctype.SortKey{desc("grand_total")}; !slices.Equal(keys, want) {
		t.Fatalf("ParseSort(grand_total desc) = %v, want %v", keys, want)
	}
	d := listtest.Setup(t)
	a := listtest.InsertPO(t, d, listtest.PORow{GrandTotal: 500})
	b := listtest.InsertPO(t, d, listtest.PORow{GrandTotal: 900})
	c := listtest.InsertPO(t, d, listtest.PORow{GrandTotal: 100})
	got, _ := listtest.Run(t, d, listtest.PO(), sorted(keys...), noScope)
	orderIs(t, "grand_total desc", got, b, a, c)
}

func TestList_DefaultSort(t *testing.T) { // NUM-112
	d := listtest.Setup(t)
	p1 := listtest.InsertPO(t, d, listtest.PORow{Date: date(t, "2026-04-01")})
	p2 := listtest.InsertPO(t, d, listtest.PORow{Date: date(t, "2026-04-02")})
	p3 := listtest.InsertPO(t, d, listtest.PORow{Date: date(t, "2026-04-01")})
	got, _ := listtest.Run(t, d, listtest.PO(), sorted(), noScope)
	orderIs(t, "PO default sort", got, p2, p3, p1)

	bharat := listtest.InsertParty(t, d, listtest.PartyRow{Code: "B", Name: "bharat"})
	apex1 := listtest.InsertParty(t, d, listtest.PartyRow{Code: "A1", Name: "Apex"})
	apex2 := listtest.InsertParty(t, d, listtest.PartyRow{Code: "A2", Name: "apex"})
	got, _ = listtest.Run(t, d, listtest.Party(), sorted(), noScope)
	orderIs(t, "party default sort", got, apex1, apex2, bharat)
}

func TestList_StablePagination(t *testing.T) { // NUM-113
	d := listtest.Setup(t)
	for range 60 {
		listtest.InsertPO(t, d, listtest.PORow{Date: date(t, "2026-04-01")})
	}
	seen := map[int64]bool{}
	var page2 []int64
	for page, want := range map[int]int{1: 25, 2: 25, 3: 10} {
		q := list.Query{Sort: []doctype.SortKey{asc("date")}, Page: page, PageSize: 25}
		got, _ := listtest.Run(t, d, listtest.PO(), q, noScope)
		if len(got) != want {
			t.Errorf("page %d: %d rows, want %d", page, len(got), want)
		}
		for _, id := range got {
			if seen[id] {
				t.Errorf("id %d appears on more than one page", id)
			}
			seen[id] = true
		}
		if page == 2 {
			page2 = got
		}
	}
	if len(seen) != 60 {
		t.Errorf("pages hold %d distinct ids, want 60", len(seen))
	}
	again, _ := listtest.Run(t, d, listtest.PO(), list.Query{Sort: []doctype.SortKey{asc("date")}, Page: 2, PageSize: 25}, noScope)
	orderIs(t, "page 2 again", again, page2...)
}

func TestList_SortByValue(t *testing.T) { // NUM-114
	d := listtest.Setup(t)
	var po []int64
	for _, m := range []types.Money{999900, 10000000, 2500000} {
		po = append(po, listtest.InsertPO(t, d, listtest.PORow{GrandTotal: m}))
	}
	got, _ := listtest.Run(t, d, listtest.PO(), sorted(asc("grand_total")), noScope)
	orderIs(t, "grand_total asc", got, po[0], po[2], po[1])

	jan := listtest.InsertExpense(t, d, listtest.ExpenseRow{Date: date(t, "2026-01-02"), Amount: 1})
	dec := listtest.InsertExpense(t, d, listtest.ExpenseRow{Date: date(t, "2025-12-31"), Amount: 1})
	got, _ = listtest.Run(t, d, listtest.Expense(), sorted(asc("date")), noScope)
	orderIs(t, "date asc", got, dec, jan)
}

func TestList_EmptyLast(t *testing.T) { // NUM-115
	d := listtest.Setup(t)
	ids := listtest.SeedFixtures(t, d)
	tech := listtest.InsertExpense(t, d, listtest.ExpenseRow{ProjectID: ids["PRJ-TECHPARK"], Amount: 1})
	none := listtest.InsertExpense(t, d, listtest.ExpenseRow{Amount: 1})
	metro := listtest.InsertExpense(t, d, listtest.ExpenseRow{ProjectID: ids["PRJ-METRO"], Amount: 1})
	// The newest project sorts first by name, so id order differs from name order.
	andheri := listtest.InsertExpense(t, d, listtest.ExpenseRow{
		ProjectID: listtest.Insert(t, d, "numt_project", map[string]any{"code": "PRJ-ANDHERI", "name": "Andheri Yard"}),
		Amount:    1,
	})
	got, _ := listtest.Run(t, d, listtest.Expense(), sorted(asc("project_id")), noScope)
	orderIs(t, "project asc", got, andheri, metro, tech, none)
	got, _ = listtest.Run(t, d, listtest.Expense(), sorted(desc("project_id")), noScope)
	orderIs(t, "project desc", got, tech, metro, andheri, none)

	// Empty text is last too, both ways.
	a := listtest.InsertPO(t, d, listtest.PORow{PartyName: "Apex"})
	blank := listtest.Insert(t, d, "numt_po", map[string]any{"party_name": ""})
	null := listtest.InsertPO(t, d, listtest.PORow{})
	b := listtest.InsertPO(t, d, listtest.PORow{PartyName: "Bharat"})
	got, _ = listtest.Run(t, d, listtest.PO(), sorted(asc("party_name")), noScope)
	orderIs(t, "party asc", got[:2], a, b)
	sameIDs(t, "party asc empties", got[2:], blank, null)
	got, _ = listtest.Run(t, d, listtest.PO(), sorted(desc("party_name")), noScope)
	orderIs(t, "party desc", got[:2], b, a)
	sameIDs(t, "party desc empties", got[2:], blank, null)
}

func TestList_TextSortNoCase(t *testing.T) { // NUM-116
	d := listtest.Setup(t)
	bharat := listtest.InsertParty(t, d, listtest.PartyRow{Code: "BHARAT", Name: "Bharat Tools"})
	apex := listtest.InsertParty(t, d, listtest.PartyRow{Code: "APEX", Name: "apex Fabricators"})
	chennai := listtest.InsertParty(t, d, listtest.PartyRow{Code: "CHENNAI", Name: "chennai Traders"})
	got, _ := listtest.Run(t, d, listtest.Party(), sorted(asc("name")), noScope)
	orderIs(t, "name asc", got, apex, bharat, chennai)
}

func TestList_PageSizeRules(t *testing.T) { // NUM-117
	limit := func(q list.Query) (any, any) {
		t.Helper()
		_, args, err := list.Build(listtest.PO(), q, noScope)
		if err != nil {
			t.Fatalf("Build(page %d, size %d): %v", q.Page, q.PageSize, err)
		}
		return args[len(args)-2], args[len(args)-1]
	}
	if l, o := limit(list.Query{Page: 1}); l != 25 || o != 0 {
		t.Errorf("size 0: LIMIT %v OFFSET %v, want 25 0", l, o)
	}
	for _, size := range []int{50, 100, 500} {
		if l, o := limit(list.Query{Page: 3, PageSize: size}); l != size || o != 2*size {
			t.Errorf("size %d page 3: LIMIT %v OFFSET %v", size, l, o)
		}
	}
	_, _, err := list.Build(listtest.PO(), list.Query{Page: 1, PageSize: 1000}, noScope)
	wantErr(t, "size 1000", err, "Page size must be 25, 50, 100 or 500.")
	_, _, err = list.Build(listtest.PO(), list.Query{Page: 0, PageSize: 25}, noScope)
	wantErr(t, "page 0", err, "Page must be 1 or more.")
	_, _, err = list.Build(listtest.PO(), list.Query{Page: math.MaxInt/25 + 2, PageSize: 25}, noScope)
	if err == nil {
		t.Error("page whose OFFSET overflows: nil error")
	}

	d := listtest.Setup(t)
	for range 3 {
		listtest.InsertPO(t, d, listtest.PORow{})
	}
	got, total := listtest.Run(t, d, listtest.PO(), list.Query{Page: 2, PageSize: 25}, noScope)
	if len(got) != 0 || total != 3 {
		t.Errorf("page past the end: %d rows, total %d, want 0 rows, total 3", len(got), total)
	}
	if list.NoMoreResults != "No more results." {
		t.Errorf("NoMoreResults = %q", list.NoMoreResults)
	}
	if !slices.Equal(list.PageSizes, []int{25, 50, 100, 500}) {
		t.Errorf("PageSizes = %v", list.PageSizes)
	}
}

func TestList_DefaultSortSkipsLineFields(t *testing.T) { // Task 9 review (supporting, no case ID)
	dt := *listtest.PO()
	dt.Fields = append(slices.Clone(dt.Fields), doctype.Field{
		Name: "item", Label: "Item", Type: doctype.Text, Sortable: true, OnLine: true,
	})
	dt.DefaultSort = []doctype.SortKey{asc("item")}
	q, _, err := list.Build(&dt, list.Query{Page: 1}, noScope)
	if err == nil {
		t.Errorf("default sort on a line field: nil error, query %q", q)
	}
}
