package list_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

func rowIDs(rows []list.Row) []int64 {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func TestList_CountIndependentOfPage(t *testing.T) { // NUM-118
	d := listtest.Setup(t)
	for range 61 {
		listtest.InsertPO(t, d, listtest.PORow{Status: "approved"})
	}
	for range 5 {
		listtest.InsertPO(t, d, listtest.PORow{Status: "draft"})
	}
	s := &list.Service{DB: d, Auth: listtest.NewAuth()}
	q := query(filter("status", list.OpEq, "approved"))
	q.Page, q.PageSize = 2, 25
	res, err := s.List(context.Background(), testkit.Principal(t, "ravi@vardhan.test"), listtest.PO(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 25 || res.Total != 61 {
		t.Errorf("page 2: %d rows, total %d, want 25 rows, total 61", len(res.Rows), res.Total)
	}
	if got := list.ShowingText(2, 25, 61); got != "Showing 26–50 of 61" {
		t.Errorf("ShowingText(2, 25, 61) = %q", got)
	}
}

func TestList_PermissionPredicateAlwaysApplied(t *testing.T) { // NUM-121
	ctx := context.Background()
	d := listtest.Setup(t)
	ids := listtest.SeedFixtures(t, d)
	metro, tech := ids["PRJ-METRO"], ids["PRJ-TECHPARK"]
	var metroPOs []int64
	var metroSum types.Money
	for i := range 20 {
		r := listtest.PORow{Status: "approved", Number: fmt.Sprintf("PO/26-27/%05d", i+1),
			GrandTotal: types.Money(100000 * (i + 1)), ProjectID: tech}
		if i < 8 {
			r.ProjectID = metro
			metroSum += r.GrandTotal
		}
		id := listtest.InsertPO(t, d, r)
		if i < 8 {
			metroPOs = append(metroPOs, id)
		}
	}
	priya := testkit.Principal(t, "priya@vardhan.test")
	a := listtest.NewAuth()
	a.Pred = auth.Predicate{SQL: "numt_po.project_id = ?", Args: []any{metro}}
	s := &list.Service{DB: d, Auth: a}

	res, err := s.List(ctx, priya, listtest.PO(), list.Query{Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	sameIDs(t, "Priya's POs", rowIDs(res.Rows), metroPOs...)
	if res.Total != 8 {
		t.Errorf("total = %d, want 8", res.Total)
	}
	res, err = s.List(ctx, priya, listtest.PO(), list.Query{Search: "PO/", Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	sameIDs(t, "search PO/", rowIDs(res.Rows), metroPOs...)
	q := query(filter("project_id", list.OpEq, fmt.Sprint(tech)))
	q.Page = 1
	res, err = s.List(ctx, priya, listtest.PO(), q)
	if err != nil || len(res.Rows) != 0 || res.Total != 0 {
		t.Errorf("filter TECHPARK: %d rows, total %d, err %v; want none, nil", len(res.Rows), res.Total, err)
	}
	if n, err := s.Count(ctx, priya, listtest.PO(), list.Query{}); err != nil || n != 8 {
		t.Errorf("Count = %d, %v; want 8", n, err)
	}
	totals, err := s.Totals(ctx, priya, listtest.PO(), list.Query{}, "grand_total")
	if err != nil {
		t.Fatal(err)
	}
	if totals["grand_total"] != metroSum {
		t.Errorf("Totals grand_total = %d, want %d", totals["grand_total"], metroSum)
	}

	suresh := testkit.Principal(t, "suresh@vardhan.test")
	pune := listtest.InsertReceipt(t, d, listtest.ReceiptRow{SiteID: ids["PUNE-WH"]})
	msite := listtest.InsertReceipt(t, d, listtest.ReceiptRow{SiteID: ids["METRO-SITE"]})
	listtest.InsertReceipt(t, d, listtest.ReceiptRow{SiteID: ids["TECH-SITE"]})
	a = listtest.NewAuth()
	a.Pred = auth.Predicate{SQL: "numt_grn.site_id IN (?, ?)", Args: []any{ids["PUNE-WH"], ids["METRO-SITE"]}}
	s = &list.Service{DB: d, Auth: a}
	res, err = s.List(ctx, suresh, listtest.Receipt(), list.Query{Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	sameIDs(t, "Suresh's GRNs", rowIDs(res.Rows), pune, msite)
}

func TestList_NoReadRight(t *testing.T) { // NUM-122
	ctx := context.Background()
	d := listtest.Setup(t)
	deepak := testkit.Principal(t, "deepak@vardhan.test")
	const msg = "You don't have permission to read Purchase Order. Ask your administrator for a role that allows it."
	check := func(what string, call func(s *list.Service) error) {
		t.Helper()
		a := listtest.NewAuth()
		a.Deny = map[auth.Right]bool{"read": true}
		err := call(&list.Service{DB: d, Auth: a})
		if !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("%s: error %v is not auth.ErrForbidden", what, err)
		}
		want := fmt.Errorf("%w: %s", auth.ErrForbidden, msg).Error()
		if err != nil && err.Error() != want {
			t.Errorf("%s: error = %q, want %q", what, err.Error(), want)
		}
		if !slices.Equal(a.Calls, []auth.Right{"read"}) {
			t.Errorf("%s: auth calls = %v, want [read] (no Scope)", what, a.Calls)
		}
	}
	check("List", func(s *list.Service) error {
		_, err := s.List(ctx, deepak, listtest.PO(), list.Query{Page: 1})
		return err
	})
	check("Count", func(s *list.Service) error {
		_, err := s.Count(ctx, deepak, listtest.PO(), list.Query{})
		return err
	})
}

func TestList_DisabledHiddenInPickers(t *testing.T) { // NUM-123
	ctx := context.Background()
	d := listtest.Setup(t)
	listtest.SeedFixtures(t, d)
	off1 := listtest.InsertParty(t, d, listtest.PartyRow{Code: "OLD1", Name: "Old One", Disabled: true})
	off2 := listtest.InsertParty(t, d, listtest.PartyRow{Code: "OLD2", Name: "Old Two", Disabled: true})
	s := &list.Service{DB: d, Auth: listtest.NewAuth()}
	p := testkit.Principal(t, "ravi@vardhan.test")

	res, err := s.Picker(ctx, p, listtest.Party(), "", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 5 || res.Total != 5 {
		t.Errorf("Picker: %d rows, total %d, want 5", len(res.Rows), res.Total)
	}
	res, err = s.List(ctx, p, listtest.Party(), list.Query{Filters: list.DefaultFilters(listtest.Party()), Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 5 {
		t.Errorf("default filter: %d rows, want 5", len(res.Rows))
	}
	res, err = s.List(ctx, p, listtest.Party(), list.Query{Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 7 {
		t.Errorf("show disabled: %d rows, want 7", len(res.Rows))
	}
	var disabled []int64
	for _, r := range res.Rows {
		if r.Values["disabled"] == true {
			disabled = append(disabled, r.ID)
		}
	}
	sameIDs(t, "rows marked disabled", disabled, off1, off2)
	if list.DefaultFilters(listtest.PO()) != nil {
		t.Errorf("DefaultFilters(PO) = %v, want nil", list.DefaultFilters(listtest.PO()))
	}
}

func TestList_ScanRowTypes(t *testing.T) { // Task 9 Row, ScanRow, BuildAll (supporting, no case ID)
	ctx := context.Background()
	d := listtest.Setup(t)
	full := listtest.InsertPO(t, d, listtest.PORow{
		Number: "PO/26-27/00001", Status: "approved", Date: date(t, "2026-04-01"), ProjectID: 3,
		GrandTotal: 123456, Qty: 5000, Rate: 625000, GSTRate: 18000, CreatedAt: "2026-04-01T04:30:00.000Z",
	})
	empty := listtest.InsertPO(t, d, listtest.PORow{})
	for range 28 {
		listtest.InsertPO(t, d, listtest.PORow{})
	}

	q, args, err := list.BuildAll(listtest.PO(), list.Query{Page: 0, PageSize: 1000}, noScope)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 0 {
		t.Errorf("BuildAll args = %v, want none (no LIMIT/OFFSET)", args)
	}
	got := map[int64]list.Row{}
	err = d.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, q, args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			r, err := list.ScanRow(listtest.PO(), rows)
			if err != nil {
				return err
			}
			got[r.ID] = r
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 30 {
		t.Errorf("BuildAll returned %d rows, want 30", len(got))
	}
	ts, _ := types.ParseTimestamp("2026-04-01T04:30:00.000Z")
	want := map[string]any{
		"number": "PO/26-27/00001", "status": "approved", "date": date(t, "2026-04-01"), "project_id": int64(3),
		"grand_total": types.Money(123456), "qty": types.Qty(5000), "rate": types.Rate(625000),
		"gst_rate": types.Percent(18000), "created_at": ts,
		"party_name": nil, "branch": nil, "remarks": nil,
	}
	for k, w := range want {
		if g := got[full].Values[k]; g != w {
			t.Errorf("Values[%q] = %#v, want %#v", k, g, w)
		}
	}
	if v := got[empty].Values["date"]; v != nil {
		t.Errorf("NULL date = %#v, want nil", v)
	}
}
