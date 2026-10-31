package purchase_test

import (
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// PO-037 (PO spec §13, §27.1; UX §5.19; CD-299): the pending report's order
// is the earliest due date first (Expected on ahead of required_by), lines
// with no due date last, then line number within a PO. Pages hold
// purchase.PendingPageSize rows, Total counts every page, and a page below 1
// is every row (the CSV).
func TestPO_PendingReportOrderAndPages(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	line := func() map[string]string { return p9Line(t, env, "WIRE-18G", "KGS", "1", "1", "7217", nil) }
	order := func(requiredBy string, lines int) []int64 {
		opts := []purchasetest.OrderOpt{}
		if requiredBy != "" {
			opts = append(opts, purchasetest.OrderHeader(map[string]string{"required_by": requiredBy}))
		}
		ls := make([]map[string]string, lines)
		for i := range ls {
			ls[i] = line()
		}
		po := purchasetest.NewOrder(t, env, append(opts, purchasetest.OrderLines(ls...))...)
		ids := make([]int64, len(po.Lines))
		for i, l := range po.Lines {
			ids[i] = l.ID
		}
		return ids
	}
	// Built out of due order: none, 05-11, 28-10 with two lines, and 06-11
	// moved to 27-10 by Expected on.
	none := order("", 1)
	nov5 := order("05-11-2026", 1)
	oct28 := order("28-10-2026", 2)
	moved := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"required_by": "06-11-2026"}))
	if err := env.Service.SetExpected(ctx, env.Arjun, moved.ID, 0, p9Day(t, time.October, 27), ""); err != nil {
		t.Fatalf("SetExpected: %v", err)
	}

	want := []int64{moved.Lines[0].ID, oct28[0], oct28[1], nov5[0], none[0]}
	v := p9Report(t, env, env.Priya, purchase.PendingFilter{})
	var got []int64
	for _, r := range v.Rows {
		got = append(got, r.LineID)
	}
	if !slices.Equal(got, want) {
		t.Errorf("row order = %v, want %v (due 27-10, 28-10 lines 1 and 2, 05-11, no due date)", got, want)
	}

	big := order("", purchase.PendingPageSize+1)
	n := len(want) + len(big)
	for _, c := range []struct {
		page, rows int
	}{{1, purchase.PendingPageSize}, {2, n - purchase.PendingPageSize}, {3, 0}, {0, n}} {
		v, err := env.Service.PendingReport(ctx, env.Priya, purchase.PendingFilter{}, c.page)
		if err != nil {
			t.Fatalf("PendingReport page %d: %v", c.page, err)
		}
		if len(v.Rows) != c.rows || v.Total != n {
			t.Errorf("page %d = %d rows, Total %d; want %d rows, Total %d", c.page, len(v.Rows), v.Total, c.rows, n)
		}
	}
	v2, err := env.Service.PendingReport(ctx, env.Priya, purchase.PendingFilter{}, 2)
	if err != nil {
		t.Fatalf("PendingReport page 2: %v", err)
	}
	if len(v2.Rows) > 0 && v2.Rows[len(v2.Rows)-1].LineID != big[len(big)-1] {
		t.Errorf("page 2 ends with line %d, want the last line %d", v2.Rows[len(v2.Rows)-1].LineID, big[len(big)-1])
	}
}
