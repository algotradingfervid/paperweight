package list_test

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

func TestList_ChildLineFilterSameRow(t *testing.T) { // NUM-124
	d := listtest.Setup(t)
	ids := listtest.SeedFixtures(t, d)
	angle, wire := ids["MS-ANGLE-50"], ids["WIRE-18G"]
	// 00002 is inserted first, so id order is not number order.
	po2 := listtest.InsertPO(t, d, listtest.PORow{Number: "PO/26-27/00002", Status: "approved"})
	po1 := listtest.InsertPO(t, d, listtest.PORow{Number: "PO/26-27/00001", Status: "approved"})
	listtest.InsertPO(t, d, listtest.PORow{Number: "PO/26-27/00003", Status: "approved"}) // no lines
	listtest.InsertPOLine(t, d, po1, angle, 5000)
	listtest.InsertPOLine(t, d, po1, wire, 50000)
	listtest.InsertPOLine(t, d, po2, angle, 50000)
	item := filter("line.item", list.OpEq, strconv.FormatInt(angle, 10))

	got, total := listtest.Run(t, d, listtest.PO(), query(item, filter("line.qty", list.OpGe, "10")), noScope)
	if !slices.Equal(got, []int64{po2}) || total != 1 {
		t.Errorf("item and qty >= 10: ids %v, total %d, want [%d], total 1", got, total, po2)
	}
	got, total = listtest.Run(t, d, listtest.PO(), query(item), noScope)
	sameIDs(t, "item alone", got, po1, po2)
	if len(got) != 2 || total != 2 {
		t.Errorf("item alone: %d rows, total %d, want 2 and 2 (each PO once)", len(got), total)
	}

	// A line filter ANDs with a header filter, and the scope still applies.
	got, _ = listtest.Run(t, d, listtest.PO(), query(item, filter("number", list.OpEq, "PO/26-27/00001")), noScope)
	sameIDs(t, "line and header filter", got, po1)
	scope := auth.Predicate{SQL: "numt_po.id = ?", Args: []any{po2}}
	got, _ = listtest.Run(t, d, listtest.PO(), query(item), scope)
	sameIDs(t, "line filter with scope", got, po2)

	// One EXISTS, every column qualified by table name, values bound.
	q, args, err := list.Build(listtest.PO(), query(item, filter("line.qty", list.OpGe, "10")), noScope)
	if err != nil {
		t.Fatal(err)
	}
	want := " WHERE EXISTS (SELECT 1 FROM numt_po_line WHERE numt_po_line.parent_id = numt_po.id AND numt_po_line.item = ? AND numt_po_line.qty >= ?)"
	if !strings.Contains(q, want) || strings.Count(q, "EXISTS") != 1 {
		t.Errorf("Build SQL = %q, want one %q", q, want)
	}
	if len(args) < 2 || args[0] != angle || args[1] != int64(10000) {
		t.Errorf("Build args = %v, want [%d 10000 …]", args, angle)
	}
}

func TestList_LineFilterOnlyOnLineFields(t *testing.T) { // NUM-124 review (supporting, no case ID)
	for _, field := range []string{
		"line.number",                   // a header field
		"line.rate",                     // not a line field
		"line.item; DROP TABLE numt_po", // injection in the name
		"line.",
		"lines.item",
	} {
		wantQueryError(t, listtest.PO(), query(filter(field, list.OpEq, "1")),
			"You can't filter Purchase Orders by '"+field+"'.")
	}
	wantQueryError(t, listtest.Receipt(), query(filter("line.item", list.OpEq, "1")),
		"You can't filter GRNs by 'line.item'.")

	dt := *listtest.PO()
	dt.Fields = slices.Clone(dt.Fields)
	for i := range dt.Fields {
		if dt.Fields[i].OnLine && dt.Fields[i].Name == "qty" {
			dt.Fields[i].Filterable = false
		}
	}
	wantQueryError(t, &dt, query(filter("line.qty", list.OpEq, "1")), "You can't filter Purchase Orders by 'line.qty'.")
	wantQueryError(t, listtest.PO(), query(filter("line.qty", list.OpContains, "1")),
		"'contains' can't be used on Qty. Use =, <, >, between.")
	wantQueryError(t, listtest.PO(), query(filter("line.item", list.OpEq, "MS-ANGLE-50")), "Choose a value for Item.")
}

func TestList_HeaderFields(t *testing.T) { // Task 15 review carry-over (supporting, no case ID)
	var names []string
	for _, f := range list.HeaderFields(listtest.PO()) {
		names = append(names, f.Name)
	}
	want := []string{"number", "party_name", "status", "date", "project_id", "branch", "grand_total",
		"qty", "rate", "gst_rate", "remarks", "created_at"}
	if !slices.Equal(names, want) {
		t.Errorf("HeaderFields(PO) = %v, want %v", names, want)
	}
	q, _, err := list.BuildAll(listtest.PO(), list.Query{}, noScope)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(q, "numt_po_line") || strings.Contains(q, "AS item") {
		t.Errorf("BuildAll selects a line field: %q", q)
	}
}

func TestList_TotalsRowAllMatching(t *testing.T) { // NUM-126
	ctx := context.Background()
	d := listtest.Setup(t)
	ids := listtest.SeedFixtures(t, d)
	metro, tech := ids["PRJ-METRO"], ids["PRJ-TECHPARK"]
	// 60 × ₹20,000.00 + ₹2,12,345.60 = ₹14,12,345.60 over 61 permitted rows.
	for i := range 61 {
		total := types.Money(2000000)
		if i == 40 {
			total = 21234560
		}
		listtest.InsertPO(t, d, listtest.PORow{Status: "approved", ProjectID: metro, GrandTotal: total})
	}
	listtest.InsertPO(t, d, listtest.PORow{Status: "approved", ProjectID: tech, GrandTotal: 99900}) // not permitted
	listtest.InsertPO(t, d, listtest.PORow{Status: "draft", ProjectID: metro, GrandTotal: 77700})   // filtered out
	a := listtest.NewAuth()
	a.Pred = auth.Predicate{SQL: "numt_po.project_id = ?", Args: []any{metro}}
	s := &list.Service{DB: d, Auth: a}
	p := testkit.Principal(t, "priya@vardhan.test")
	q := query(filter("status", list.OpEq, "approved"))
	q.PageSize = 25

	res, err := s.List(ctx, p, listtest.PO(), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 25 || res.Total != 61 {
		t.Errorf("page 1: %d rows, total %d, want 25 and 61", len(res.Rows), res.Total)
	}
	sums, err := s.Totals(ctx, p, listtest.PO(), q, "grand_total")
	if err != nil {
		t.Fatal(err)
	}
	if sums["grand_total"] != 141234560 {
		t.Errorf("Totals = %d, want 141234560", sums["grand_total"])
	}
	if got := types.FormatINR(sums["grand_total"]); got != "₹14,12,345.60" {
		t.Errorf("FormatINR = %q, want ₹14,12,345.60", got)
	}
}

func BenchmarkList_POPage(b *testing.B) { // NUM-127
	ctx := context.Background()
	d := listtest.Setup(b)
	err := d.Write(ctx, func(tx *db.Tx) error {
		for _, q := range []string{
			`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 50000)
			 INSERT INTO numt_po (id, number, party_name, status, date, grand_total)
			 SELECT i, printf('PO/26-27/%05d', i), printf('Party %d', i % 300),
			        CASE i % 3 WHEN 0 THEN 'approved' WHEN 1 THEN 'draft' ELSE 'closed' END,
			        date('2026-04-01', printf('+%d days', i % 365)), i * 100 FROM n`,
			`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 200000)
			 INSERT INTO numt_po_line (parent_id, item, qty) SELECT (i - 1) / 4 + 1, i % 50 + 1, i * 1000 FROM n`,
			"CREATE INDEX numt_po_date ON numt_po (date, id)",
			"CREATE INDEX numt_po_status_date ON numt_po (status, date, id)",
			"CREATE INDEX numt_po_party ON numt_po (party_name)", // the test PO's party column
			"CREATE INDEX numt_po_line_parent ON numt_po_line (parent_id)",
		} {
			if _, err := tx.SQL().ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	s := &list.Service{DB: d, Auth: listtest.NewAuth()}
	p := testkit.Principal(b, "ravi@vardhan.test")
	q := query(filter("status", list.OpEq, "approved"))
	ops := 0
	for b.Loop() {
		res, err := s.List(ctx, p, listtest.PO(), q)
		if err != nil {
			b.Fatal(err)
		}
		if len(res.Rows) != 25 || res.Total == 0 {
			b.Fatalf("page 1: %d rows, total %d", len(res.Rows), res.Total)
		}
		ops++
	}
	if mean := b.Elapsed() / time.Duration(ops); mean > 200*time.Millisecond {
		b.Fatalf("mean op %v, want under 200ms", mean)
	}
}
