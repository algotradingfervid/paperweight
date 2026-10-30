package app_test

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// saveOrder saves change over the stored order, as a page would.
func saveOrder(t *testing.T, w *world, p auth.Principal, id int64, change func(values map[string]string, lines []map[string]string) []map[string]string) {
	t.Helper()
	ctx := context.Background()
	var rec doc.Record
	err := w.app.DB().Read(ctx, func(tx *db.Tx) (err error) {
		rec, err = w.app.Engine().Load(ctx, tx, fixture.Order, id)
		return err
	})
	if err != nil {
		t.Fatalf("load order: %v", err)
	}
	lines := change(rec.Values, rec.Lines)
	if _, err := w.app.Engine().Do(ctx, p, fixture.Order, id, doctype.ActionSave, doc.ActionInput{Version: rec.Version, Values: rec.Values, Lines: lines}); err != nil {
		t.Fatalf("save order: %v", err)
	}
}

// TestApp_HistoryWording: DOC-089. History reads in words and in the values the
// document shows: no raw actions, stored integers, JSON or nulls.
func TestApp_HistoryWording(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	ravi := w.signedIn(raviEmail)
	order, _ := receivedOrder(t, w, m)
	r := ravi.get(orderURL(order))
	if r.status != http.StatusOK {
		t.Fatalf("GET order = %d", r.status)
	}
	// Ravi can't read fixture.Receipt, so the roll-up doesn't name it (CD-248).
	for _, want := range []string{"· Priya Shah · Created", "· Priya Shah · Saved", "· Priya Shah · Submitted", "· Suresh Patil · Updated by a linked document"} {
		if !slices.ContainsFunc(historyHeads(r), func(h string) bool { return strings.HasSuffix(h, want) }) {
			t.Errorf("history heads %q, want one ending %q", historyHeads(r), want)
		}
	}
	if text := strings.Join(historyHeads(r), "\n"); strings.Contains(text, "GR/26-27/00001") {
		t.Errorf("Ravi's history heads %q name the receipt he can't read", historyHeads(r))
	}
	// Suresh reads both, so he sees the number (CD-248).
	sr := w.signedIn(sureshEmail).get(orderURL(order))
	if want := "· Suresh Patil · Updated by GR/26-27/00001"; !slices.ContainsFunc(historyHeads(sr), func(h string) bool { return strings.HasSuffix(h, want) }) {
		t.Errorf("Suresh's history heads %q, want one ending %q", historyHeads(sr), want)
	}
	// Changes on fields the DocType doesn't declare stay out (CD-248).
	for _, c := range historyChanges(r) {
		if strings.HasPrefix(c, "auto_approved") || strings.HasPrefix(c, "number") {
			t.Errorf("change row %q is on an undeclared field", c)
		}
	}
	if changes := historyChanges(r); !slices.Contains(changes, "Line 1 Received quantity: 0 → 400") {
		t.Errorf("change rows %q, want the roll-up in kilograms, not its stored integer", changes)
	}
	if got := r.doc.Find(".doc-audit .history-row").Last().Find(".history-changes li").Length(); got != 0 {
		t.Errorf("the Created row lists %d changes, want it to be the one line", got)
	}
}

// TestApp_HistoryChangeValues: every kind of value a save can change, shown as
// the document shows it.
func TestApp_HistoryChangeValues(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	priya := w.signedIn(priyaEmail)
	p := w.principal(auth.UserID(w.count(`SELECT id FROM users WHERE email = ?`, priyaEmail)))
	id := priya.newOrder(m, "PRJ-METRO", "1000.00")
	priya.addLine(m, id)
	saveOrder(t, w, p, id, func(v map[string]string, lines []map[string]string) []map[string]string {
		v["amount"], v["order_date"], v["remarks"] = "130000.00", "2026-10-25", "Deliver to Pune site"
		v["supplier_id"] = strconv.FormatInt(m.Party["BHARAT"], 10)
		lines[0]["qty"], lines[0]["rate"] = "1500.000", "70.0000"
		return append(lines, map[string]string{"item_id": strconv.FormatInt(m.Item["WIRE-18G"], 10), "qty": "50.000", "rate": "96.0000", "uom": "kg"})
	})
	saveOrder(t, w, p, id, func(v map[string]string, lines []map[string]string) []map[string]string {
		v["remarks"] = ""
		return lines[:1]
	})
	priya.act(id, "submit", nil)
	r := priya.get(orderURL(id))
	got := historyChanges(r)
	for _, want := range []string{
		"Amount: ₹1,000.00 → ₹1,30,000.00",
		"Order date: 24-10-2026 → 25-10-2026",
		"Remarks: (empty) → Deliver to Pune site",
		"Remarks: Deliver to Pune site → (empty)",
		"Supplier: SHREE → BHARAT",
		"Line 1 Quantity: 1,200 → 1,500",
		"Line 1 Rate: ₹62.50 → ₹70.00",
		"Line 2: (empty) → Item WIRE-18G, Quantity 50, Rate ₹96.00, Unit kg",
		"Line 2: Item WIRE-18G, Quantity 50, Rate ₹96.00, Unit kg → (empty)",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("change rows %q, want %q", got, want)
		}
	}
}

func historyHeads(r *response) []string {
	var out []string
	r.doc.Find(".doc-audit .history-head").Each(func(_ int, s *goquery.Selection) { out = append(out, s.Text()) })
	return out
}

func historyChanges(r *response) []string {
	var out []string
	r.doc.Find(".doc-audit .history-changes li").Each(func(_ int, s *goquery.Selection) { out = append(out, s.Text()) })
	return out
}
