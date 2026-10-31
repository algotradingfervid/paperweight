package purchase_test

import (
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// P35 (P17's e2e findings): a PO created from a request starts with today's
// IST business date as its order date (PO spec §3.1 "default today"), and a
// line with an item whose price stays blank after defaults warns as ₹0
// (CD-337 (4)).

// TestRequest_CreatePOOrderDateToday runs at 18:31 UTC on 24-10-2026, 00:01
// IST on 25-10-2026: the order date is the IST date, not the UTC one.
func TestRequest_CreatePOOrderDateToday(t *testing.T) {
	env := purchasetest.New(t)
	req := p13Request(t, env, p13Row(t, env, "MS-ANGLE-50", "KGS", "10", "North columns"))
	env.SetNow(t, time.Date(2026, 10, 24, 18, 31, 0, 0, time.UTC))

	id, err := p13Create(t, env, env.Priya, req.ID, p5Version(t, env, req.ID), "SHREE")
	if err != nil {
		t.Fatalf("CreateFromRequest as Priya: %v %+v", err, p5Fields(err))
	}
	po := p5Load(t, env, purchase.DocType, id)
	if got := p5Plain(po.Header["order_date"]); got != "2026-10-25" {
		t.Errorf("order_date = %s, want today's IST date 2026-10-25", got)
	}
}

// TestPO_BlankPriceWarning: a line with an item and no price after defaults
// (no last price, no standard rate) warns as ₹0; ₹0.01 and a line with no
// item warn nothing.
func TestPO_BlankPriceWarning(t *testing.T) {
	env := purchasetest.New(t)
	angle := func(price string) map[string]string { return p2Line(t, env, "MS-ANGLE-50", "KGS", "10", price) }

	for _, c := range []struct {
		what     string
		lines    []map[string]string
		warnings []string
	}{
		{"blank on line 2", []map[string]string{angle("62.5"), angle("")}, []string{"Price is ₹0 on line 2."}},
		{"0.01", []map[string]string{angle("0.01")}, nil},
	} {
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(c.lines...))
		if got := p2Warnings(t, env, env.Priya, po.ID); !slices.Equal(got, c.warnings) {
			t.Errorf("%s: OrderWarnings = %q, want %q", c.what, got, c.warnings)
		}
	}

	// A step save keeps a line with no item (and no price) on the draft.
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(angle("62.5")))
	noItem := angle("")
	noItem["item_id"] = ""
	_, err := p2Step(t, env, env.Priya, po.ID, []string{"remarks"}, nil, []map[string]string{angle("62.5"), noItem})
	if err != nil {
		t.Fatalf("step save with an item-less line: %v %+v", err, p5Fields(err))
	}
	if n := countRows(t, env.DB, "purchase_order_line", "item_id IS NULL AND price IS NULL"); n != 1 {
		t.Fatalf("item-less blank-price lines = %d, want 1", n)
	}
	if got := p2Warnings(t, env, env.Priya, po.ID); len(got) != 0 {
		t.Errorf("item-less line: OrderWarnings = %q, want none", got)
	}
}

// TestPO_BlankOrderDateIsToday (AD-13 ruling on P35): Validate fills a blank
// order date on a draft with today's IST date, as for a request's date, at
// 18:31 UTC on 24-10-2026 (00:01 IST on 25-10-2026); a picked date is kept;
// so a PO saved with a blank date submits without "Order date is required.".
// A non-draft is never filled: a pending PO whose date is blank still gets
// the refusal on approve.
func TestPO_BlankOrderDateIsToday(t *testing.T) {
	env := purchasetest.New(t)
	env.SetNow(t, time.Date(2026, 10, 24, 18, 31, 0, 0, time.UTC))
	header := func(date string) map[string]string {
		return map[string]string{"vendor_id": masterID(t, env.DB, "party", "SHREE"), "site_id": masterID(t, env.DB, "site", "METRO-SITE"),
			"project_id": masterID(t, env.DB, "project", "PRJ-METRO"), "order_date": date}
	}
	angle := []map[string]string{p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "62.5")}

	for _, c := range []struct{ what, posted, want string }{
		{"blank", "", "2026-10-25"},
		{"picked", "2026-10-20", "2026-10-20"},
	} {
		res, err := p2Create(t, env, env.Priya, header(c.posted), angle)
		if err != nil {
			t.Fatalf("%s save: %v %+v", c.what, err, p5Fields(err))
		}
		if got := p5Plain(p2Load(t, env, res.ID).Header["order_date"]); got != c.want {
			t.Errorf("%s: order_date = %s, want %s", c.what, got, c.want)
		}
	}

	// CD-299: filling B's blank date writes only B: A keeps its picked date.
	a, err := p2Create(t, env, env.Priya, header("2026-10-20"), angle)
	if err != nil {
		t.Fatalf("save A: %v %+v", err, p5Fields(err))
	}
	if _, err := p2Create(t, env, env.Priya, header(""), angle); err != nil {
		t.Fatalf("save B: %v %+v", err, p5Fields(err))
	}
	if got := p5Plain(p2Load(t, env, a.ID).Header["order_date"]); got != "2026-10-20" {
		t.Errorf("A after B's save: order_date = %s, want 2026-10-20", got)
	}

	po := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), purchasetest.OrderLines(p2RefLines(t, env)...),
		purchasetest.OrderHeader(map[string]string{"order_date": ""}))
	if got := p5Plain(po.Header["order_date"]); po.State != doctype.StatePending || got != "2026-10-25" {
		t.Fatalf("blank-date PO after submit: %s with order_date %s, want pending on 2026-10-25", po.State, got)
	}

	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE purchase_order SET order_date = NULL WHERE id = ?`, po.ID); err != nil {
		t.Fatalf("blank the pending PO's date: %v", err)
	}
	_, err = p2Do(t, env, env.Ravi, po.ID, doc.ActionApprove, "")
	p2WantInvalid(t, "approve a pending PO with no date", err, doctype.FieldError{Field: "order_date", Message: "Order date is required."})
}
